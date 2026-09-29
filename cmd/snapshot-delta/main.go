// Command snapshot-delta 跑快照差量样例。
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"example.com/snapdelta"
)

func at(x float64) []snapdelta.Entity {
	return []snapdelta.Entity{{ID: "a", X: x, HP: 100}, {ID: "b", X: 0, HP: 100}}
}

// Run 执行一次命令行调用，返回退出码。
func Run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("snapshot-delta", flag.ContinueOnError)
	flags.SetOutput(stderr)
	sample := flags.String("sample", "repeat", "repeat / removed / resync")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	encoder := snapdelta.NewEncoder()
	switch *sample {
	case "repeat":
		// 三拍：实体 a 先动一次，第三拍跟第二拍完全一样。
		sizes := make([]int, 0, 3)
		for tick, entities := range [][]snapdelta.Entity{at(0), at(5), at(5)} {
			delta := encoder.Encode(int64(tick+1), entities)
			sizes = append(sizes, len(delta.Changed)+len(delta.Removed))
		}
		fmt.Fprintf(stdout, "sizes=%d,%d,%d\n", sizes[0], sizes[1], sizes[2])
	case "removed":
		// 第一拍两个实体，第二拍 b 被销毁。
		encoder.Encode(1, at(0))
		delta := encoder.Encode(2, []snapdelta.Entity{{ID: "a", X: 0, HP: 100}})
		fmt.Fprintf(stdout, "changed=%d removed=%d\n", len(delta.Changed), len(delta.Removed))
	case "resync":
		// 客户端丢包，客户端状态为空，服务器要求下一拍发关键帧。
		encoder.Encode(1, at(0))
		encoder.Encode(2, at(5))
		encoder.RequestKeyframe()
		delta := encoder.Encode(3, at(5))
		fmt.Fprintf(stdout, "keyframe=%v changed=%d\n", delta.Keyframe, len(delta.Changed))
	default:
		fmt.Fprintln(stderr, "需要 --sample repeat|removed|resync")
		return 2
	}
	return 0
}

func main() {
	os.Exit(Run(os.Args[1:], os.Stdout, os.Stderr))
}
