// Package snapdelta 是快照差量同步：每拍只发变化的实体，丢包后发关键帧兜底。
// 缺陷：发完差量不更新基线，删除也没人报，客户端请求关键帧更是没人理。
package snapdelta

import "sort"

// Entity 是一个同步实体。
type Entity struct {
	ID string
	X  float64
	Y  float64
	HP int
}

// Delta 是一拍的差量。
type Delta struct {
	Tick     int64
	Keyframe bool
	Changed  []Entity
	Removed  []string
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
// 缺陷：基线只在第一拍建立，之后永不更新；删除不进 Removed；关键帧请求被忽略。
func (e *Encoder) Encode(tick int64, current []Entity) Delta {
	delta := Delta{Tick: tick}
	if !e.haveBaseline {
		e.baseline = index(current)
		e.haveBaseline = true
		delta.Keyframe = true
		delta.Changed = append(delta.Changed, current...)
		return delta
	}
	for _, entity := range current {
		previous, ok := e.baseline[entity.ID]
		if !ok || previous != entity {
			delta.Changed = append(delta.Changed, entity)
		}
	}
	sort.Slice(delta.Changed, func(i, j int) bool { return delta.Changed[i].ID < delta.Changed[j].ID })
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
