// Package snapdelta 是快照差量同步：带宽预算内按优先级发差量、丢包后有界收敛、关键帧兜底。
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
	// baseline 是“上一次已经同步出去的状态”，每拍只随实际发出的条目推进。
	baseline     map[string]Entity
	haveBaseline bool
	wantKeyframe bool
	lastTick     int64
	tickEncoded  bool
}

// NewEncoder 建一个编码器。
func NewEncoder() *Encoder {
	return &Encoder{}
}

// RequestKeyframe 是客户端丢包后要求重同步。
func (e *Encoder) RequestKeyframe() {
	e.wantKeyframe = true
}

// Encode 编出一拍的差量：
//   - 与 baseline 对比，坐标或血量变化进 Changed，消失的实体进 Removed，
//     完全没变化时两项都为空；
//   - Changed 与 Removed 合计每拍最多 MaxPerTick 条，按 Priority 升序
//     （同优先级按 ID 升序）选取，没发出去的留下一拍（baseline 不推进）；
//   - Deferred 记录本拍之后仍占满收敛窗口的积压条数：普通拍为
//     max(0, 未同步条数-(ConvergeTicks-1)*MaxPerTick)；关键帧拍只记信息性的
//     max(0, 未同步条数-MaxPerTick)；
//   - 请求过关键帧（或第一拍）时发全量：Keyframe=true、Changed 含全部实体、
//     Removed 为空，不受带宽限制，并重置基线与积压；
//   - 同一拍重复编码第二次起返回空差量。
func (e *Encoder) Encode(tick int64, current []Entity) Delta {
	delta := Delta{Tick: tick}
	if e.tickEncoded && e.lastTick == tick {
		return delta
	}
	e.lastTick = tick
	e.tickEncoded = true

	if !e.haveBaseline {
		e.baseline = make(map[string]Entity)
		e.haveBaseline = true
	}

	changed, removed := e.diff(current)
	outstanding := len(changed) + len(removed)

	if e.wantKeyframe {
		e.wantKeyframe = false
		delta.Keyframe = true
		delta.Deferred = overflow(outstanding, MaxPerTick)
		delta.Changed = cloneSortedByID(current)
		e.baseline = index(current)
		return delta
	}

	if outstanding == 0 {
		return delta
	}

	items := make([]pendingItem, 0, outstanding)
	for id, entity := range changed {
		items = append(items, pendingItem{id: id, entity: entity, kind: pendingChanged})
	}
	for id, entity := range removed {
		items = append(items, pendingItem{id: id, entity: entity, kind: pendingRemoved})
	}

	selected := selectHighestPriority(items, MaxPerTick)
	delta.Deferred = overflow(outstanding, (ConvergeTicks-1)*MaxPerTick)
	for _, item := range selected {
		if item.kind == pendingChanged {
			delta.Changed = append(delta.Changed, item.entity)
			e.baseline[item.id] = item.entity
		} else {
			delta.Removed = append(delta.Removed, item.id)
			delete(e.baseline, item.id)
		}
	}
	sort.Slice(delta.Changed, func(i, j int) bool { return delta.Changed[i].ID < delta.Changed[j].ID })
	sort.Strings(delta.Removed)
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

// diff 只按 README 口径同步 ID、坐标、血量；Priority 仅用于选取排序。
func (e *Encoder) diff(current []Entity) (changed map[string]Entity, removed map[string]Entity) {
	currentIndex := index(current)
	changed = make(map[string]Entity, len(currentIndex))
	for id, entity := range currentIndex {
		previous, ok := e.baseline[id]
		if !ok || previous.X != entity.X || previous.Y != entity.Y || previous.HP != entity.HP {
			changed[id] = entity
		}
	}
	removed = make(map[string]Entity)
	for id, entity := range e.baseline {
		if _, ok := currentIndex[id]; !ok {
			removed[id] = entity
		}
	}
	return changed, removed
}

type pendingKind int

const (
	pendingChanged pendingKind = iota
	pendingRemoved
)

type pendingItem struct {
	id     string
	entity Entity // Changed 时是当前实体，Removed 时是基线里的旧实体（取其 Priority）
	kind   pendingKind
}

func lessPending(a, b pendingItem) bool {
	if a.entity.Priority != b.entity.Priority {
		return a.entity.Priority < b.entity.Priority
	}
	return a.id < b.id
}

// selectHighestPriority 在 O(n*limit) 时间、O(limit) 额外空间内选出优先级最高的
// limit 条（limit 为常量 MaxPerTick，即整体 O(n)），结果按优先级、ID 升序。
func selectHighestPriority(items []pendingItem, limit int) []pendingItem {
	if limit <= 0 || len(items) == 0 {
		return nil
	}
	best := make([]pendingItem, 0, min(limit, len(items)))
	for _, item := range items {
		if len(best) == limit {
			if !lessPending(item, best[limit-1]) {
				continue
			}
			best = best[:limit-1]
		}
		insertAt := sort.Search(len(best), func(i int) bool { return lessPending(item, best[i]) })
		best = append(best, pendingItem{})
		copy(best[insertAt+1:], best[insertAt:])
		best[insertAt] = item
	}
	return best
}

func cloneSortedByID(entities []Entity) []Entity {
	out := append([]Entity(nil), entities...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func overflow(total, capacity int) int {
	if total > capacity {
		return total - capacity
	}
	return 0
}
