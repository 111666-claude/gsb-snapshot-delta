package snapdelta

import "testing"

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
