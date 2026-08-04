# API DTO 抽离与转换重构 —— 实施规格

> 上游设计:`docs/api-refactor/api-types-and-conversion-refactor.md`(方案论证与依赖证据)
> 本文只记**已拍板的决策**与**可执行的迁移清单**,不重复论证。
>
> 基线:`upstream/master` @ `b967fbf0`。分支:`refactor/api-dto`。

---

## 1. 目标

1. **分层修复**:`thorclient` 不再传递依赖 `chain/state/muxdb/runtime/txpool/comm/bft/logdb`。
2. **DTO 独立成包**:REST wire model 迁入 `api/dto`,为 JSON-RPC 子系统腾出 `api/` 命名空间。
3. **转换代码标准化**:keyed 字面量、统一命名、消除同构重复类型、泛型 `MapSlice` 削样板。

wire 冻结:REST 响应 JSON 的字段名/顺序/`omitempty`/编码类型**逐字节不变**。

---

## 2. 已定决策

| 决策 | 结论 | 理由 |
|---|---|---|
| DTO 包名 | `api/dto` | 中立命名。JSON-RPC 的 model **未来可能也放进这个包**(届时评估),故不用 `api/restmodel`/`api/rest` 这类 REST 专属名 |
| 共用 converter 落点 | 新包 `api/convert` | `ConvertRange` 被 events+transfers 共用、`ConvertClause` 被 transactions+blocks 共用;放 owner handler 会制造 handler 互 import 与成环风险 |
| 执行阶段 | 0–5 全做 + B+(logdb 清零) | — |
| `TransferCriteria` json tag | **加** `json:"txOrigin"`/`sender`/`recipient` | 现状 `logdb.TransferCriteria` 无 tag,靠 `encoding/json` 大小写不敏感匹配才接受 OpenAPI 声明的 lowerCamel(`api/doc/thor.yaml:2288`)。加 tag 后仍兼容 `TxOrigin` 写法(不敏感匹配 tag),且与文档对齐 |
| 命名统一方向 | **去掉** `JSON` 前缀 | 包名 `dto` 已表明是 wire 模型,前缀冗余;与现有无前缀的 `Receipt`/`Clause`/`FilteredEvent` 一致 |
| Go API 破坏 | **接受**,阶段 5 删除全部 alias、顶层 `api` 包消失 | 按 semver 属 major(v2→v3),需在 PR/release note 明确标注 breaking change |
| wire 基线 | 入库 golden 文件 | 长期回归保护,对后续 JSON-RPC 工作同样有用 |
| 交付粒度 | **待定**(默认单 PR、按阶段+域分 commit,每个 commit 可编译可测) | review 时确认 |

---

## 3. 终局包结构

```
api/dto/          纯 wire model。依赖仅:thor, tx, block, math, hexutil, fmt, time, encoding/json
api/convert/      跨域共用 converter:ConvertRange(+emptyRange)、ConvertClause、MapSlice
api/accounts/     handler + 域专属 converter
api/blocks/       同上
api/transactions/ 同上
api/events/       同上
api/transfers/    同上
api/node/         同上 + Network 接口
api/subscriptions/同上
api/debug/ api/fees/ api/admin/   handler(无 converter)
api/jsonrpc/      (后续)JSON-RPC 子系统
```

顶层 `api` 包(`api/*_types.go`)在阶段 5 后**不再存在**——该目录下除子包外只有这些文件。

依赖方向单向无环:`thor/tx/block` ← `api/dto` ← {`api/convert`, handler 子包, `thorclient/*`};server-only 包只被 handler 引用。

---

## 4. 类型迁移清单(60 个类型)

| 源文件 | 迁入 `api/dto` 的类型 | 备注 |
|---|---|---|
| `account_types.go` | `Account`, `CallData`, `GetCodeResult`, `GetStorageResult`, `GetRawStorageResponse`, `CallResult`, `BatchCallData`, `BatchCallResults` | — |
| `admin_types.go` | `LogStatus`, `ToggleStatus`, `HealthStatus`, `LogLevelRequest`, `LogLevelResponse` | 依赖 `time` |
| `blocks_types.go` | `BlockSummary`, `RawBlockSummary`, `CollapsedBlock`, `EmbeddedTx`, `ExpandedBlock` | 均去 `JSON` 前缀;`JSONClause` **删除**(死代码);`JSONEvent`/`JSONTransfer`/`JSONOutput` **合并**至 common 三兄弟 |
| `common_types.go` | `Event`, `Transfer`, `Clause`, `Clauses`, `LogMeta` | — |
| `debug_types.go` | `TraceClauseOption`, `TraceCallOption`, `StorageRangeOption`, `StorageRangeResult`, `StorageMap`, `StorageEntry` | 依赖 `encoding/json` |
| `events_types.go` | `FilteredEvent`, `TopicSet`, `EventCriteria`, `Options`(+`Validate`), `EventFilter`, `RangeType`+两常量, `Range`(+`Validate`), **新增本地 `Order`** | `Order` 为 `string` 底层类型 + `ASC`/`DESC` 常量,取代 `logdb.Order` |
| `fees_types.go` | `FeesHistory`, `FeesPriority` | — |
| `node_types.go` | `Status`, `PeerStats` | `Network` 接口不是 DTO → 随 converter 进 `api/node` |
| `subscriptions_types.go` | `BlockMessage`, `TransferMessage`, `EventMessage`, `SubscriptionEventFilter`(+`Match`), `SubscriptionTransferFilter`(+`Match`), `BeatMessage`, `Beat2Message`, `PendingTxIDMessage` | `Match` 只依赖 `tx`(domain),留在 dto 合规 |
| `transactions_types.go` | `RawTx`(+`Decode`), `RawTransaction`, `TxMeta`, `ReceiptMeta`, `Receipt`, `Output`, `SendTxResult` | `Decode` 只依赖 `tx`,留在 dto 合规 |
| `transfers_types.go` | `FilteredTransfer`, `TransferFilter`, **新增本地 `TransferCriteria`** | 取代 `logdb.TransferCriteria` |
| `api/transactions/transactions_converter.go` | `Transaction` | 归位:DTO 进 `api/dto`,`ConvertTransaction` 留 `api/transactions` |

---

## 5. Converter 落点(14 个)

| converter | 现位置 | 依赖 | 落点 | 改名 |
|---|---|---|---|---|
| `ConvertRange` + `emptyRange` | `events_types.go:167` | chain, logdb, block | **api/convert** | — |
| `ConvertClause` | `common_types.go:54` | tx | **api/convert** | — |
| `MapSlice[A,B]` | 新增 | — | **api/convert** | — |
| `ConvertCallResultWithInputGas` | `account_types.go:53` | runtime | api/accounts | — |
| `BuildJSONBlockSummary` | `blocks_types.go:102` | chain | api/blocks | → `ConvertBlockSummary` |
| `buildJSONOutput` | `blocks_types.go:128` | tx | api/blocks | → `convertOutput` |
| `BuildJSONEmbeddedTxs` | `blocks_types.go:155` | tx | api/blocks | → `ConvertEmbeddedTxs` |
| `ConvertEvent` | `events_types.go:28` | logdb | api/events | — |
| `ConvertEventFilter` | `events_types.go:97` | logdb, chain | api/events | — |
| `ConvertTransfer` | `transfers_types.go:29` | logdb | api/transfers | — |
| `ConvertPeersStats` | `node_types.go:31` | comm | api/node | — |
| `ConvertBlock` | `subscriptions_types.go:40` | chain | api/subscriptions | — |
| `ConvertSubscriptionTransfer` | `subscriptions_types.go:83` | block, tx | api/subscriptions | — |
| `ConvertSubscriptionEvent` | `subscriptions_types.go:114` | block, tx | api/subscriptions | — |
| `ConvertReceipt` | `transactions_types.go:73` | block, tx | api/transactions | — |
| `ConvertTransaction` | `api/transactions/transactions_converter.go:38` | chain | 原地不动 | — |

新增:`api/transfers` 需要 `TransferFilter` → `logdb.TransferFilter` 的转换(现状 `transfers.go:48` 直接透传 `logdb.TransferCriteria`,B+ 后必须显式转)。

---

## 6. logdb 清零(B+)的 wire 判定

`api/dto` 里唯一的 `logdb` 引用是两个**请求** DTO 字段:

- `EventFilter.Order` / `TransferFilter.Order`:`logdb.Order` 是 `type Order string` + `"asc"`/`"desc"`(`logdb/types.go:45`)。dto 本地同构定义 → 序列化/反序列化字节相同。
- `TransferFilter.CriteriaSet`:`logdb.TransferCriteria` 三字段 `TxOrigin`/`Sender`/`Recipient`,均 `*thor.Address`,**无 json tag**(`logdb/types.go:90`)。dto 本地定义**加 lowerCamel tag**(见 §2 决策)。

handler 侧新增 dto→logdb 转换。`logdb.Range`/`logdb.EventFilter` 等只出现在 converter 返回值,不进 dto。

---

## 7. 阶段 4 去重与改名(附同构性证据)

**合并(已逐字段核对,JSON 输出相同)**:

| 保留 | 删除 | 证据 |
|---|---|---|
| `Event`(`common_types.go:18`) | `JSONEvent`(`blocks_types.go:59`) | `Address thor.Address json:"address"` / `Topics []thor.Bytes32 json:"topics"` / `Data string json:"data"` 三字段名、序、tag、类型全同 |
| `Transfer`(`common_types.go:25`) | `JSONTransfer`(`blocks_types.go:53`) | `Sender`/`Recipient thor.Address` / `Amount *math.HexOrDecimal256` 全同 |
| `Output`(`transactions_types.go:66`) | `JSONOutput`(`blocks_types.go:65`) | `ContractAddress *thor.Address` / `Events []*Event` / `Transfers []*Transfer` —— 元素类型即上两行的同构对,合并后 JSON 相同 |

**删除**:`JSONClause`(`blocks_types.go:47`)全仓零引用。注意它与 `Clause` **不同构**(`Value` 是值类型 `math.HexOrDecimal256`,`Clause` 是指针)—— 因此是删除,不是合并。

**改名**:5 个 `JSON*` 类型去前缀(§4)+ 3 个 `Build*` converter 改 `Convert*`(§5)。

**keyed 字面量**(位置化 → keyed,字段错位可编译期发现):
- `transactions_types.go:87` `ReceiptMeta{...}`
- `transactions_types.go:104` `&Output{...}`
- `blocks_types.go:169` `&Clause{...}`

---

## 8. golden 基线

**顺序关键**:基线必须在迁移前、于**顶层 `api` 包内**建立。

1. 阶段 0:新增 `api/wire_golden_test.go` —— 为每个响应 DTO 构造字段填满(含 `omitempty` 字段的有值/无值两态)的实例,`json.Marshal` 后与 `api/testdata/<type>.json` 对拍。首次运行生成 testdata 并提交。
2. 阶段 2 迁移时,测试文件与 testdata 一并移入 `api/dto`,**testdata 字节不改**。
3. 阶段 4 改名/合并后,testdata 仍**零 diff** —— 这是"改名不动 wire"的证明。

覆盖范围:所有出现在 REST 响应中的 DTO(§4 清单,除纯请求类型 `CallData`/`*Filter`/`*Option`;请求类型另加 unmarshal 用例,验证 `TransferCriteria` 加 tag 后 `TxOrigin` 与 `txOrigin` 两种输入都能解析)。

---

## 9. 分阶段执行

每阶段结束:`go build ./...`、`go test ./api/... ./thorclient/...`、golden 零 diff。

| 阶段 | 内容 | 破坏性 |
|---|---|---|
| 0 | golden 基线入库 | 无 |
| 1 | keyed 字面量(3 处)、删 `JSONClause`、`api/convert` 包 + `MapSlice` | 删 `JSONClause` 是 Go API 破坏(死代码) |
| 2 | 建 `api/dto`;全部 DTO 迁入;14 个 converter 迁到 §5 落点;顶层 `api` 全量 type alias 过渡(含 `const BlockRangeType = dto.BlockRangeType` 形式转发常量) | 无(alias 兜住) |
| 3 | `api/transactions.Transaction` 归位;B+ logdb 本地化 | `transactions.Transaction` 用 alias 兜住 |
| 4 | 去 `JSON` 前缀(5 个类型)、`Build*`→`Convert*`(3 个)、合并 3 对同构类型 | Go API 破坏 |
| 5 | 删除全部 alias;顶层 `api` 包消失;`thorclient` 19 个文件 216 处 `api.X`→`dto.X` | Go API 破坏(major) |

阶段 2/3 之后 `thorclient` 仍编译通过且**零改动**——`Transaction` 归位后在 `api/transactions` 留 `type Transaction = dto.Transaction`(该包已 import `api`,反向无环)。thorclient 的全部改写(19 个文件、216 处,含 4 个 `import api/transactions` 的文件)集中在阶段 5,纯机械替换。

---

## 10. 验收

- `go list -deps ./api/dto` 不含 `chain/state/muxdb/runtime/txpool/comm/bft/logdb`
- `go list -deps ./thorclient/...` 不含上述任一包
- golden testdata 零 diff
- `go test ./...` 全过
- `api/doc/thor.yaml` 无需修改(wire 未变);PR 描述标注 breaking change 与受影响的导出标识符清单

---

## 11. 风险

| | 风险 | 缓解 |
|---|---|---|
| R1 | wire 漂移 | golden 基线(§8),阶段 4 尤其依赖它 |
| R2 | Go API 破坏影响外部 SDK | 已接受;阶段 4/5 与阶段 0–3 分 commit,release note 列出全部消失的标识符 |
| R3 | import cycle | `api/dto` 禁止 import 任何 handler/server-only 包;`go list -deps` 断言 |
| R4 | `TransferCriteria` 加 tag 后 unmarshal 行为变化 | 显式单测覆盖 `TxOrigin`/`txOrigin` 两种输入 |
| R5 | 阶段 2 一次移动 60 个类型难 review | 按域分 commit,每个 commit 可编译可测 |

---

## 12. 不做什么

- 不改任何 REST 响应的 JSON 字段名/顺序/形状/`omitempty`/编码类型。
- 不在 domain 包(`block`/`tx`/`state`/`chain`/`logdb`/`runtime`)加任何 API/JSON 关注点。
- 不用反射/通用 marshaler 替代含业务计算的转换(合约地址推导、tx 类型分支、topics 过滤、reverted 跳过 outputs)。
- 不引入代码生成工具链。
- 不把 DTO 下沉到 handler 子包。
- 不做每域一个 types 子包。
- 本次不动 `api/jsonrpc`(独立课题,见 `docs/jsonrpc/jsonrpc-server-design.md`)。
