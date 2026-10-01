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

// Delta 是一拍的差量。Deferred 是本拍之后仍压在收敛窗口之外的积压条数。
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
	baseline map[string]Entity
	// wantKeyframe 为真时下一次 Encode 发全量；第一拍也按全量处理。
	wantKeyframe bool
	// lastTick/tickEncoded 保证同一拍重复编码第二次起拿到空差量。
	lastTick    int64
	tickEncoded bool
}

// NewEncoder 建一个编码器。
func NewEncoder() *Encoder {
	return &Encoder{wantKeyframe: true}
}

// RequestKeyframe 是客户端丢包后要求重同步：下一次 Encode 发全量。
func (e *Encoder) RequestKeyframe() {
	e.wantKeyframe = true
}

// Encode 编出一拍的差量：
//   - 与 baseline 比较，坐标或血量变化的实体进 Changed，消失的进 Removed，
//     两者都没有时返回空差量；
//   - Changed 与 Removed 合计每拍最多 MaxPerTick 条，按 Priority 升序
//     （同优先级按 ID 升序）选取，没发出去的留下一拍，Deferred 记录本拍之后
//     仍超出剩余收敛窗口（ConvergeTicks-1 拍）容量的条数；
//   - 第一拍或请求过关键帧时发全量：Keyframe=true、Changed 含全部实体、
//     Removed 为空，不受带宽限制，并重置基线与积压；
//   - 同一拍重复编码，第二次起返回空差量。
func (e *Encoder) Encode(tick int64, current []Entity) Delta {
	delta := Delta{Tick: tick}
	if e.tickEncoded && e.lastTick == tick {
		return delta
	}
	e.lastTick = tick
	e.tickEncoded = true

	changed, removed := e.diff(current)
	outstanding := len(changed) + len(removed)

	if e.wantKeyframe {
		e.wantKeyframe = false
		delta.Keyframe = true
		delta.Changed = cloneSortedByID(current)
		// 全量已经把 outstanding 条一次发完，这里只保留信息性的积压口径。
		delta.Deferred = overflow(outstanding, MaxPerTick)
		e.baseline = indexEntities(current)
		return delta
	}

	if outstanding == 0 {
		return delta
	}

	candidates := make([]pendingItem, 0, outstanding)
	for id, entity := range changed {
		candidates = append(candidates, pendingItem{id: id, entity: entity, removed: false})
	}
	for id, entity := range removed {
		candidates = append(candidates, pendingItem{id: id, entity: entity, removed: true})
	}

	delta.Deferred = overflow(outstanding, (ConvergeTicks-1)*MaxPerTick)
	for _, item := range selectByPriority(candidates, MaxPerTick) {
		if item.removed {
			delta.Removed = append(delta.Removed, item.id)
			delete(e.baseline, item.id)
		} else {
			delta.Changed = append(delta.Changed, item.entity)
			e.baseline[item.id] = item.entity
		}
	}
	sortEntitiesByID(delta.Changed)
	sort.Strings(delta.Removed)
	return delta
}

// diff 只按 README 口径比对 ID、坐标、血量；Priority 仅用于带宽选取时的排序。
func (e *Encoder) diff(current []Entity) (changed map[string]Entity, removed map[string]Entity) {
	currentIndex := indexEntities(current)
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

// pendingItem 是一条待发的差量；removed 时 entity 取基线旧实体，借它的 Priority 排序。
type pendingItem struct {
	id      string
	entity  Entity
	removed bool
}

func lessPending(a, b pendingItem) bool {
	if a.entity.Priority != b.entity.Priority {
		return a.entity.Priority < b.entity.Priority
	}
	return a.id < b.id
}

// selectByPriority 在 O(n*limit) 时间、O(limit) 额外空间内选出优先级最高的 limit 条。
// limit 是常量 MaxPerTick，因此整体 O(n)，结果按 Priority、ID 升序。
func selectByPriority(items []pendingItem, limit int) []pendingItem {
	best := make([]pendingItem, 0, min(limit, len(items)))
	for _, item := range items {
		if len(best) == limit {
			if !lessPending(item, best[limit-1]) {
				continue
			}
			best = best[:limit-1]
		}
		position := sort.Search(len(best), func(i int) bool { return lessPending(item, best[i]) })
		best = append(best, pendingItem{})
		copy(best[position+1:], best[position:])
		best[position] = item
	}
	return best
}

// Apply 是客户端侧：把差量贴到本地状态上。
func Apply(local []Entity, delta Delta) []Entity {
	state := indexEntities(local)
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
	sortEntitiesByID(out)
	return out
}

func indexEntities(entities []Entity) map[string]Entity {
	indexed := make(map[string]Entity, len(entities))
	for _, entity := range entities {
		indexed[entity.ID] = entity
	}
	return indexed
}

func cloneSortedByID(entities []Entity) []Entity {
	out := append([]Entity(nil), entities...)
	sortEntitiesByID(out)
	return out
}

func sortEntitiesByID(entities []Entity) {
	sort.Slice(entities, func(i, j int) bool { return entities[i].ID < entities[j].ID })
}

func overflow(total, capacity int) int {
	if total > capacity {
		return total - capacity
	}
	return 0
}
