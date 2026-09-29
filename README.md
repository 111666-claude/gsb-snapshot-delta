# snapshot-delta

快照差量同步：每拍只发变化的实体，丢包后发关键帧兜底，只用 Go 标准库。

```
go test ./...
go run ./cmd/snapshot-delta --sample repeat
go run ./cmd/snapshot-delta --sample removed
go run ./cmd/snapshot-delta --sample resync
```

## 口径（README 为准）

- **每拍都要刷新基线**：跟上一拍的快照比，`X`/`Y`/`HP` 有变化的实体进 `Changed`，
  上一拍有、这一拍没有的实体进 `Removed`，完全没变化的一拍 `Changed` 与 `Removed` 都为空。
  `--sample repeat`（实体 a 先从 0 动到 5，第三拍与第二拍一模一样）按口径是
  `sizes=1,1,0`，现在是 `sizes=2,1,1`——第三拍还在重复发已经同步过的位置。
- **删除要报**：实体消失时进 `Removed`，客户端要把它删掉。`--sample removed`
  （第二拍实体 b 不见了）按口径是 `changed=0 removed=1`，现在是 `changed=0 removed=0`，
  客户端会一直留着这个幽灵实体。
- **关键帧兜底**：客户端调 `RequestKeyframe` 之后的下一次 `Encode` 必须是关键帧：
  `Keyframe=true`、`Changed` 含全部当前实体、`Removed` 为空，并把基线重置。
  `--sample resync`（客户端丢包后要求重同步）按口径是 `keyframe=true changed=2`，
  现在是 `keyframe=false changed=1`。
- 不变量：把连续几拍的差量依次 `Apply` 到客户端状态上，结果必须逐项等于服务器当前快照
  （ID、坐标、血量全都要一致）；同一拍重复编码第二次必须是空差量；`Changed` 按 ID 升序。
- 规模：单拍 5 万个实体一次 O(n) 编完，内存 O(实体数)；不许每拍全量广播，
  也不许把每拍的完整快照都留在内存里。

## 现在的行为

- 基线只在第一拍建立，之后永不更新，于是同一个变化会被反复发（`--sample repeat` 的第三拍）。
- 只遍历当前快照，从不比较基线里多出来的实体，`Removed` 永远是空的。
- `RequestKeyframe` 只是把 `wantKeyframe` 立起来，`Encode` 没读过这个字段。

## 输出

```
sizes=2,1,0
changed=0 removed=1
keyframe=true changed=2
```

## 目录

```
delta.go                差量编码与贴差量
cmd/snapshot-delta      命令行入口
delta_test.go           go test 用例
```
