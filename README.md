# snapshot-delta

快照差量同步：带宽预算内按优先级发差量、丢包后有界收敛、关键帧兜底，只用 Go 标准库。

```
go test ./...
go run ./cmd/snapshot-delta --sample repeat
go run ./cmd/snapshot-delta --sample removed
go run ./cmd/snapshot-delta --sample resync
go run ./cmd/snapshot-delta --sample bandwidth
```

## 口径（README 为准）

- **每拍刷新基线**：与"上一次已经同步出去的状态"比较，坐标或血量有变化的实体进 `Changed`，
  上次有、这次没有的实体进 `Removed`，完全没变化的一拍两项都为空。
- **带宽预算**：`Changed` 与 `Removed` 加起来每拍最多 `MaxPerTick` 条；按 `Priority` 升序
  （同优先级按 `ID` 升序）选取，这一拍没发出去的留到下一拍，条数写进 `Deferred`。
- **关键帧兜底**：`RequestKeyframe` 之后的下一次 `Encode` 必须发全量
  （`Keyframe=true`、`Changed` 含全部实体、`Removed` 为空），不受带宽限制，并重置基线与积压。
- **收敛**：把连续几拍的差量依次 `Apply` 到客户端状态上，结果必须逐项等于服务器当前快照
  （ID、坐标、血量全都要一致）；丢包之后按 `MaxPerTick` 计算，`ConvergeTicks` 拍内必须追上。
- **顺序**：`Changed` 与 `Removed` 都要按 ID 升序输出。
- **规模**：单拍 5 万个实体一次 O(n) 编完，内存 O(实体数)；不许每拍全量广播。

## 输出契约（不改格式）

```
sizes=2,1,0
changed=0 removed=1
keyframe=true changed=2
changed=5,2,2,1 deferred=3,1,0,0
```

## 常量

```
MaxPerTick    = 2
ConvergeTicks = 3
```

## 目录

```
delta.go                差量编码与贴差量
cmd/snapshot-delta      命令行入口
delta_test.go           go test 用例
```
