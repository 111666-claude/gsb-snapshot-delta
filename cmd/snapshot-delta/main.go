// Command snapshot-delta 跑快照差量样例。
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"example.com/snapdelta"
)

func world(x float64) []snapdelta.Entity {
	return []snapdelta.Entity{
		{ID: "a", X: x, HP: 100},
		{ID: "b", X: 0, HP: 100},
	}
}

func team(x float64) []snapdelta.Entity {
	out := make([]snapdelta.Entity, 0, 5)
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		out = append(out, snapdelta.Entity{ID: id, X: x, HP: 100})
	}
	return out
}

func joinInts(values []int) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprint(value))
	}
	return strings.Join(parts, ",")
}

// Run 执行一次命令行调用，返回退出码。
func Run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("snapshot-delta", flag.ContinueOnError)
	flags.SetOutput(stderr)
	sample := flags.String("sample", "repeat", "repeat / removed / resync / bandwidth")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	encoder := snapdelta.NewEncoder()
	switch *sample {
	case "repeat":
		// 三拍：实体先动一次，第三拍与第二拍完全一样。
		sizes := make([]int, 0, 3)
		for tick, entities := range [][]snapdelta.Entity{world(0), world(5), world(5)} {
			delta := encoder.Encode(int64(tick+1), entities)
			sizes = append(sizes, len(delta.Changed)+len(delta.Removed))
		}
		fmt.Fprintf(stdout, "sizes=%s\n", joinInts(sizes))
	case "removed":
		// 第一拍两个实体，第二拍 b 被销毁。
		encoder.Encode(1, world(0))
		delta := encoder.Encode(2, []snapdelta.Entity{{ID: "a", X: 0, HP: 100}})
		fmt.Fprintf(stdout, "changed=%d removed=%d\n", len(delta.Changed), len(delta.Removed))
	case "resync":
		// 客户端丢包后要求下一拍发关键帧。
		encoder.Encode(1, world(0))
		encoder.Encode(2, world(5))
		encoder.RequestKeyframe()
		delta := encoder.Encode(3, world(5))
		fmt.Fprintf(stdout, "keyframe=%v changed=%d\n", delta.Keyframe, len(delta.Changed))
	case "bandwidth":
		// 第一拍五个实体，之后三拍五条变更全都要同步。
		changed := make([]int, 0, 4)
		deferred := make([]int, 0, 3)
		for tick := 1; tick <= 4; tick++ {
			entities := team(0)
			if tick > 1 {
				entities = team(5)
			}
			delta := encoder.Encode(int64(tick), entities)
			changed = append(changed, len(delta.Changed)+len(delta.Removed))
			deferred = append(deferred, delta.Deferred)
		}
		fmt.Fprintf(stdout, "changed=%s deferred=%s\n", joinInts(changed), joinInts(deferred))
	default:
		fmt.Fprintln(stderr, "需要 --sample repeat|removed|resync|bandwidth")
		return 2
	}
	return 0
}

func main() {
	os.Exit(Run(os.Args[1:], os.Stdout, os.Stderr))
}
