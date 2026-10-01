package snapdelta

import (
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"testing"
	"time"
)

func reflectDeepEqualDelta(a, b Delta) bool {
	return reflect.DeepEqual(a, b)
}

func TestFirstEncodeIsKeyframe(t *testing.T) {
	encoder := NewEncoder()
	delta := encoder.Encode(1, []Entity{{ID: "a", X: 1}, {ID: "b", X: 2}})
	if !delta.Keyframe || len(delta.Changed) != 2 {
		t.Fatalf("第一拍应该是两个实体的关键帧：%+v", delta)
	}
}

func TestUnchangedTickSendsNothing(t *testing.T) {
	encoder := NewEncoder()
	entities := []Entity{{ID: "a", X: 1}}
	encoder.Encode(1, entities)
	if delta := encoder.Encode(2, entities); !delta.Empty() {
		t.Fatalf("没有变化的拍不该发东西：%+v", delta)
	}
}

func TestApplyUpdatesAndRemoves(t *testing.T) {
	local := []Entity{{ID: "a", X: 1}, {ID: "b", X: 2}}
	next := Apply(local, Delta{
		Changed: []Entity{{ID: "a", X: 9}},
		Removed: []string{"b"},
	})
	if len(next) != 1 || next[0].ID != "a" || next[0].X != 9 {
		t.Fatalf("贴差量之后的状态不对：%+v", next)
	}
}

func TestApplyKeyframeReplacesState(t *testing.T) {
	local := []Entity{{ID: "old", X: 1}}
	next := Apply(local, Delta{Keyframe: true, Changed: []Entity{{ID: "new", X: 2}}})
	if len(next) != 1 || next[0].ID != "new" {
		t.Fatalf("关键帧应该整体替换状态：%+v", next)
	}
}

func TestConstants(t *testing.T) {
	if MaxPerTick != 2 || ConvergeTicks != 3 {
		t.Fatal("差量同步常量被改了")
	}
}

// 以下是补的测试：基线刷新、带宽预算、删除、关键帧、收敛与对照自证。

func snapshotEqual(t *testing.T, got, want []Entity, msg string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s：长度 %d != %d（got=%v want=%v）", msg, len(got), len(want), got, want)
	}
	gotIndex := indexEntities(got)
	for _, entity := range want {
		if gotIndex[entity.ID] != entity {
			t.Fatalf("%s：实体 %s 不一致（got=%v want=%v）", msg, entity.ID, gotIndex[entity.ID], entity)
		}
	}
}

func assertIDsSorted(t *testing.T, delta Delta) {
	t.Helper()
	for i := 1; i < len(delta.Changed); i++ {
		if delta.Changed[i-1].ID >= delta.Changed[i].ID {
			t.Fatalf("Changed 没有按 ID 升序：%v", delta.Changed)
		}
	}
	for i := 1; i < len(delta.Removed); i++ {
		if delta.Removed[i-1] >= delta.Removed[i] {
			t.Fatalf("Removed 没有按 ID 升序：%v", delta.Removed)
		}
	}
}

func TestBaselineRefreshesEveryTick(t *testing.T) {
	encoder := NewEncoder()
	first := []Entity{{ID: "a", X: 1, HP: 10}, {ID: "b", X: 2, HP: 10}}
	second := []Entity{{ID: "a", X: 9, HP: 10}, {ID: "b", X: 2, HP: 10}}
	encoder.Encode(1, first)
	moved := encoder.Encode(2, second)
	if len(moved.Changed) != 1 || moved.Changed[0].ID != "a" {
		t.Fatalf("第二拍只有 a 该进 Changed：%+v", moved)
	}
	still := encoder.Encode(3, second)
	if !still.Empty() || still.Deferred != 0 {
		t.Fatalf("基线刷新后重复拍应为空差量：%+v", still)
	}
	hpOnly := encoder.Encode(4, []Entity{{ID: "a", X: 9, HP: 8}, {ID: "b", X: 2, HP: 10}})
	if len(hpOnly.Changed) != 1 || hpOnly.Changed[0].ID != "a" || hpOnly.Changed[0].HP != 8 {
		t.Fatalf("只有血量变化时 a 该进 Changed：%+v", hpOnly)
	}
	// Priority 单独变化不触发同步。
	reprioritized := encoder.Encode(5, []Entity{{ID: "a", X: 9, HP: 8, Priority: 5}, {ID: "b", X: 2, HP: 10}})
	if !reprioritized.Empty() {
		t.Fatalf("只有 Priority 变化不该同步：%+v", reprioritized)
	}
}

func TestRemovedEntities(t *testing.T) {
	encoder := NewEncoder()
	encoder.Encode(1, []Entity{{ID: "a"}, {ID: "b"}, {ID: "c"}})
	delta := encoder.Encode(2, []Entity{{ID: "a"}})
	if len(delta.Changed) != 0 || len(delta.Removed) != MaxPerTick {
		t.Fatalf("删除受带宽限制，这一拍应删 2 个：%+v", delta)
	}
	if delta.Removed[0] != "b" || delta.Removed[1] != "c" {
		t.Fatalf("Removed 应按 ID 升序：%v", delta.Removed)
	}
	delta = encoder.Encode(3, []Entity{{ID: "a"}})
	if !delta.Empty() {
		t.Fatalf("删除发完后基线已推进，不应再有积压：%+v", delta)
	}
}

func TestBandwidthBudgetPicksByPriority(t *testing.T) {
	encoder := NewEncoder()
	encoder.Encode(1, nil)
	delta := encoder.Encode(2, []Entity{
		{ID: "id-low", X: 1, Priority: 9},
		{ID: "id-hi", X: 1, Priority: 0},
		{ID: "id-mid", X: 1, Priority: 5},
	})
	if len(delta.Changed) != MaxPerTick || delta.Deferred != 0 {
		t.Fatalf("3 条变化每拍 2 条，未超收敛窗口 Deferred=0：%+v", delta)
	}
	if delta.Changed[0].ID != "id-hi" || delta.Changed[1].ID != "id-mid" {
		t.Fatalf("应先按 Priority 升序选取：%v", delta.Changed)
	}
	assertIDsSorted(t, delta)

	next := encoder.Encode(3, []Entity{
		{ID: "id-low", X: 1, Priority: 9},
		{ID: "id-hi", X: 1, Priority: 0},
		{ID: "id-mid", X: 1, Priority: 5},
	})
	if len(next.Changed) != 1 || next.Changed[0].ID != "id-low" {
		t.Fatalf("没发出去的应留到下一拍：%+v", next)
	}
}

func TestBandwidthSampleShape(t *testing.T) {
	encoder := NewEncoder()
	team := func(x float64) []Entity {
		out := make([]Entity, 0, 5)
		for _, id := range []string{"a", "b", "c", "d", "e"} {
			out = append(out, Entity{ID: id, X: x, HP: 100})
		}
		return out
	}
	first := encoder.Encode(1, team(0))
	if !first.Keyframe || len(first.Changed) != 5 || first.Deferred != 3 {
		t.Fatalf("第一拍全量关键帧，Deferred 口径为超出预算的 3：%+v", first)
	}
	wantSizes := []int{2, 2, 1}
	wantDeferred := []int{1, 0, 0}
	for tick := 2; tick <= 4; tick++ {
		delta := encoder.Encode(int64(tick), team(5))
		got := len(delta.Changed) + len(delta.Removed)
		if got != wantSizes[tick-2] || delta.Deferred != wantDeferred[tick-2] {
			t.Fatalf("第 %d 拍：发 %d 条 Deferred=%d，口径为 %d/%d",
				tick, got, delta.Deferred, wantSizes[tick-2], wantDeferred[tick-2])
		}
	}
}

func TestKeyframeResyncResetsBaselineAndBacklog(t *testing.T) {
	encoder := NewEncoder()
	encoder.Encode(1, []Entity{{ID: "a", X: 1}, {ID: "b", X: 2}})
	encoder.Encode(2, []Entity{{ID: "a", X: 3}, {ID: "b", X: 4}, {ID: "c", X: 5}})
	encoder.RequestKeyframe()
	current := []Entity{{ID: "a", X: 3}, {ID: "b", X: 4}, {ID: "c", X: 5}}
	delta := encoder.Encode(3, current)
	if !delta.Keyframe || len(delta.Changed) != 3 || len(delta.Removed) != 0 {
		t.Fatalf("关键帧拍必须全量、Removed 为空、不受带宽限制：%+v", delta)
	}
	assertIDsSorted(t, delta)
	if next := encoder.Encode(4, current); !next.Empty() {
		t.Fatalf("关键帧重置基线后下一拍应为空：%+v", next)
	}
}

func TestRepeatedEncodeSameTickIsEmpty(t *testing.T) {
	encoder := NewEncoder()
	encoder.Encode(1, []Entity{{ID: "a"}})
	first := encoder.Encode(2, []Entity{{ID: "a", X: 7}})
	if first.Empty() {
		t.Fatal("同拍第一次编码应正常出差量")
	}
	second := encoder.Encode(2, []Entity{{ID: "a", X: 7}})
	if !second.Empty() || second.Keyframe || second.Deferred != 0 {
		t.Fatalf("同拍第二次编码必须是空差量：%+v", second)
	}
}

func entitiesMatch(a, b []Entity) bool {
	if len(a) != len(b) {
		return false
	}
	indexed := indexEntities(a)
	for _, entity := range b {
		if indexed[entity.ID] != entity {
			return false
		}
	}
	return true
}

func TestConvergesWithinWindowAfterLoss(t *testing.T) {
	server := []Entity{
		{ID: "a", X: 1}, {ID: "b", X: 1}, {ID: "c", X: 1},
		{ID: "d", X: 1}, {ID: "e", X: 1},
	}

	// 带宽积压：5 条变化超过 MaxPerTick，ConvergeTicks 拍内必须全部送达。
	encoder := NewEncoder()
	client := Apply(nil, encoder.Encode(1, server))
	server = []Entity{
		{ID: "a", X: 9}, {ID: "b", X: 9}, {ID: "c", X: 9},
		{ID: "d", X: 9}, {ID: "e", X: 9},
	}
	converged := false
	for tick := int64(2); tick <= 1+ConvergeTicks; tick++ {
		client = Apply(client, encoder.Encode(tick, server))
		if entitiesMatch(client, server) {
			converged = true
			break
		}
	}
	if !converged {
		t.Fatalf("积压应在 %d 拍内收敛：%v", ConvergeTicks, client)
	}

	// 丢包：已发出的差量丢了，客户端请求关键帧后下一拍必须一拍收敛。
	encoder = NewEncoder()
	client = Apply(nil, encoder.Encode(1, server))
	moved := []Entity{
		{ID: "a", X: 5}, {ID: "b", X: 5}, {ID: "c", X: 5},
		{ID: "d", X: 5}, {ID: "e", X: 5},
	}
	encoder.Encode(2, moved) // 这一拍丢包，客户端不 Apply
	encoder.RequestKeyframe()
	client = Apply(client, encoder.Encode(3, moved))
	if !entitiesMatch(client, moved) {
		t.Fatalf("丢包重同步后客户端必须一拍收敛：%v", client)
	}
}

func TestLargeSnapshotIsLinear(t *testing.T) {
	encoder := NewEncoder()
	const n = 50000
	current := make([]Entity, n)
	for i := range current {
		current[i] = Entity{ID: fmt.Sprintf("entity-%06d", i), HP: i}
	}
	start := time.Now()
	delta := encoder.Encode(1, current)
	if !delta.Keyframe || len(delta.Changed) != n {
		t.Fatalf("5 万实体应一次全量编码：keyframe=%v changed=%d", delta.Keyframe, len(delta.Changed))
	}
	moved := current
	moved[0].X = 42
	next := encoder.Encode(2, moved)
	if len(next.Changed) != 1 || next.Changed[0].ID != moved[0].ID {
		t.Fatalf("下一拍只应有一个变化：%+v", next)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("5 万实体两拍编码超过 5 秒，不是 O(n)")
	}
}

// referenceEncoder 是测试里独立写的“基线对比 + 带宽预算 + 关键帧”参考实现，
// 与生产实现刻意不同：候选全量排序后再截取 MaxPerTick 条。
type referenceEncoder struct {
	baseline map[string]Entity
	have     bool
	wantKF   bool
	lastTick int64
	encoded  bool
}

type referenceItem struct {
	id      string
	entity  Entity
	removed bool
}

func (r *referenceEncoder) encode(tick int64, current []Entity) Delta {
	delta := Delta{Tick: tick}
	if r.encoded && r.lastTick == tick {
		return delta
	}
	r.lastTick, r.encoded = tick, true

	currentIndex := indexEntities(current)
	var items []referenceItem
	for id, entity := range currentIndex {
		previous, ok := r.baseline[id]
		if !ok || previous.X != entity.X || previous.Y != entity.Y || previous.HP != entity.HP {
			items = append(items, referenceItem{id: id, entity: entity})
		}
	}
	for id, entity := range r.baseline {
		if _, ok := currentIndex[id]; !ok {
			items = append(items, referenceItem{id: id, entity: entity, removed: true})
		}
	}
	outstanding := len(items)

	if !r.have || r.wantKF {
		r.have, r.wantKF = true, false
		delta.Keyframe = true
		delta.Deferred = overflow(outstanding, MaxPerTick)
		delta.Changed = append(delta.Changed, current...)
		sortEntitiesByID(delta.Changed)
		r.baseline = indexEntities(current)
		return delta
	}
	if outstanding == 0 {
		return delta
	}
	delta.Deferred = overflow(outstanding, (ConvergeTicks-1)*MaxPerTick)
	sort.Slice(items, func(i, j int) bool {
		if items[i].entity.Priority != items[j].entity.Priority {
			return items[i].entity.Priority < items[j].entity.Priority
		}
		return items[i].id < items[j].id
	})
	for _, item := range items[:min(MaxPerTick, len(items))] {
		if item.removed {
			delta.Removed = append(delta.Removed, item.id)
			delete(r.baseline, item.id)
		} else {
			delta.Changed = append(delta.Changed, item.entity)
			r.baseline[item.id] = item.entity
		}
	}
	sortEntitiesByID(delta.Changed)
	sort.Strings(delta.Removed)
	return delta
}

func (r *referenceEncoder) requestKeyframe() { r.wantKF = true }

func TestRandomDifferentialAgainstReference(t *testing.T) {
	const universe = 12
	for seed := int64(0); seed < 200; seed++ {
		rng := rand.New(rand.NewSource(seed))
		producer := NewEncoder()
		reference := &referenceEncoder{}
		server := make(map[string]Entity)
		var clientA, clientB []Entity
		resync := false

		for tick := int64(1); tick <= 40; tick++ {
			for op := 0; op < 1+rng.Intn(3); op++ {
				id := fmt.Sprintf("e%02d", rng.Intn(universe))
				switch rng.Intn(3) {
				case 0: // 新增/复活，带随机优先级
					server[id] = Entity{
						ID: id, X: rng.Float64() * 10, Y: rng.Float64() * 10,
						HP: rng.Intn(200), Priority: rng.Intn(5),
					}
				case 1: // 修改
					if entity, ok := server[id]; ok {
						entity.X += rng.Float64() * 2
						entity.HP = rng.Intn(200)
						server[id] = entity
					}
				case 2: // 删除
					delete(server, id)
				}
			}
			current := make([]Entity, 0, len(server))
			for _, entity := range server {
				current = append(current, entity)
			}
			rng.Shuffle(len(current), func(i, j int) { current[i], current[j] = current[j], current[i] })

			if resync {
				producer.RequestKeyframe()
				reference.requestKeyframe()
				resync = false
			}
			got := producer.Encode(tick, current)
			want := reference.encode(tick, current)
			if !reflectDeepEqualDelta(got, want) {
				t.Fatalf("seed=%d tick=%d 差量不一致\ngot =%+v\nwant=%+v", seed, tick, got, want)
			}
			// 第二拍起随机丢包；丢了就请求下一拍重同步，其余拍正常贴差量。
			dropped := tick > 1 && rng.Intn(4) == 0
			if dropped {
				resync = true
			} else {
				clientA = Apply(clientA, got)
				clientB = Apply(clientB, want)
				if !entitiesMatch(clientA, clientB) {
					t.Fatalf("seed=%d tick=%d 两个客户端状态分叉", seed, tick)
				}
			}
			if got.Keyframe && !dropped {
				wantSnapshot := make([]Entity, 0, len(server))
				for _, entity := range server {
					wantSnapshot = append(wantSnapshot, entity)
				}
				if !entitiesMatch(clientA, wantSnapshot) {
					t.Fatalf("seed=%d tick=%d 关键帧后客户端未等于服务器快照", seed, tick)
				}
			}
		}
		// 结尾强制一次重同步，最终状态必须逐项等于服务器当前快照。
		producer.RequestKeyframe()
		reference.requestKeyframe()
		current := make([]Entity, 0, len(server))
		for _, entity := range server {
			current = append(current, entity)
		}
		got := producer.Encode(41, current)
		want := reference.encode(41, current)
		if !reflectDeepEqualDelta(got, want) {
			t.Fatalf("seed=%d 收尾关键帧差量不一致\ngot =%+v\nwant=%+v", seed, got, want)
		}
		clientA = Apply(clientA, got)
		clientB = Apply(clientB, want)
		final := make([]Entity, 0, len(server))
		for _, entity := range server {
			final = append(final, entity)
		}
		if !entitiesMatch(clientA, final) || !entitiesMatch(clientB, final) {
			t.Fatalf("seed=%d 最终客户端状态与服务器快照不一致", seed)
		}
	}
}
