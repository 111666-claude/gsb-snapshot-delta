// Package snapdelta 是快照差量同步：带宽预算内按优先级发差量、丢包后有界收敛、关键帧兜底。
// 缺陷：基线只在第一拍建立、不发 Removed、关键帧请求没人理、每拍不限带宽、积压不维护。
package snapdelta

import "sort"

// MaxPerTick 是每拍最多能发的实体数（带宽预算）。
const MaxPerTick = 2

// ConvergeTicks 是丢包后允许的收敛拍数。
const ConvergeTicks = 3

// Entity 是一个同步实体。
type Entity struct {
	ID       string
	X        float64
	Y        float64
	HP       int
	Priority int // 越小越重要
}

// Delta 是一拍的差量。Deferred 是本拍因带宽没发出去的积压条数。
type Delta struct {
	Tick     int64
	Keyframe bool
	Changed  []Entity
	Removed  []string
	Deferred int
}

// Empty 判断这一拍有没有内容要发。
func (d Delta) Empty() bool {
	return len(d.Changed) == 0 && len(d.Removed) == 0
}

// Encoder 是服务器侧的差量编码器。
type Encoder struct {
	baseline     map[string]Entity
	haveBaseline bool
	wantKeyframe bool
}

// NewEncoder 建一个编码器。
func NewEncoder() *Encoder {
	return &Encoder{}
}

// RequestKeyframe 是客户端丢包后要求重同步。
// 缺陷：只是把标志立起来，Encode 从来没看过它。
func (e *Encoder) RequestKeyframe() {
	e.wantKeyframe = true
}

// Encode 编出一拍的差量。
// 缺陷：基线只在第一拍建立、之后永不更新；删除不进 Removed；
// 关键帧请求被忽略；每拍想发多少发多少，没有带宽预算也没有积压记账。
func (e *Encoder) Encode(tick int64, current []Entity) Delta {
	delta := Delta{Tick: tick}
	if !e.haveBaseline {
		e.baseline = index(current)
		e.haveBaseline = true
		delta.Keyframe = true
		delta.Changed = append(delta.Changed, current...)
		sortEntities(delta.Changed)
		return delta
	}
	for _, entity := range current {
		previous, ok := e.baseline[entity.ID]
		if !ok || previous != entity {
			delta.Changed = append(delta.Changed, entity)
		}
	}
	sortEntities(delta.Changed)
	return delta
}

// Apply 是客户端侧：把差量贴到本地状态上。
func Apply(local []Entity, delta Delta) []Entity {
	state := index(local)
	if delta.Keyframe {
		state = map[string]Entity{}
	}
	for _, entity := range delta.Changed {
		state[entity.ID] = entity
	}
	for _, id := range delta.Removed {
		delete(state, id)
	}
	out := make([]Entity, 0, len(state))
	for _, entity := range state {
		out = append(out, entity)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func index(entities []Entity) map[string]Entity {
	indexed := make(map[string]Entity, len(entities))
	for _, entity := range entities {
		indexed[entity.ID] = entity
	}
	return indexed
}

func sortEntities(entities []Entity) {
	sort.Slice(entities, func(i, j int) bool {
		if entities[i].Priority != entities[j].Priority {
			return entities[i].Priority < entities[j].Priority
		}
		return entities[i].ID < entities[j].ID
	})
}
