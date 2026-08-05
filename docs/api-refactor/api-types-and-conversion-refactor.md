# thor REST API server 重构设计:DTO 组织与 JSON 转换

> 范围:纯内部重构,**不改变任何 REST 响应的字节级 JSON 输出**。回答两个问题:①响应类型都堆在顶层 `api` 包是否可以更合理地组织;②手写 `Convert*` 转换是否有更优雅的方式。
>
> 证据来自当前 `json-rpc` 分支源码与 `go list -deps`。

---

## 1. 背景与目标

thor REST API 分两层:handler 在子包(`api/accounts`、`api/blocks`、`api/transactions`、`api/events`、`api/node`、`api/debug`、`api/fees`、`api/subscriptions`…),而所有请求/响应 DTO 集中在**顶层 `api` 包**的 11 个 `api/*_types.go`(约 60 个类型、~1168 行)。这套 DTO 同时是 server 与 `thorclient` 共享的 wire 契约(thorclient 20 个文件、139 处引用 `api.Xxx`)。

目标:在**不改变对外 JSON 契约**的前提下,让类型组织更清晰、转换代码更少样板。

---

## 2. 现状与问题(带证据)

### 2.1 类型"堆在顶端"——且这不只是难看

顶层 `api` 包把 DTO 和 converter 混在同一批 `*_types.go` 里。因为 converter 的**签名引用了 server-only 类型**,而 DTO 的使用者(thorclient)会把整包的 import 一并吃进去:

- `blocks_types.go:102` `BuildJSONBlockSummary(summary *chain.BlockSummary, …)` → 拉入 `chain`
- `account_types.go:53` `ConvertCallResultWithInputGas(vo *runtime.Output, …)` → 拉入 `runtime`
- `node_types.go:31` `ConvertPeersStats([]*comm.PeerStats)` + `:13` `Network` 接口 → 拉入 `comm`
- `events_types.go:97/167` `ConvertEventFilter/ConvertRange(chain *chain.Chain, …)` → 拉入 `chain`

**后果(关键):`go list -deps ./thorclient/httpclient` 目前传递依赖 `muxdb, chain, state, runtime, txpool, comm, logdb, bft` 这些 server-only 包。** 一个纯 HTTP 客户端不该链接进节点存储/共识/执行层。根因两条:

1. converter 与 DTO 同包 —— client 只用 DTO struct(`api.Convert*` 在 thorclient 中出现 **0 次**,已核实),却被迫吃下 converter 的重依赖;
2. thorclient 还**直接 import 了 handler 子包** `api/transactions`(`thorclient.go:41`、`httpclient/client.go:23`),而该包依赖 `chain + txpool`。

即"类型在顶端"表面是审美问题,底下是一个**已经发生的分层违规**。

### 2.2 命名/放置/写法不一致

- **JSON 前缀不统一**:`JSONBlockSummary`/`JSONCollapsedBlock`/`JSONEvent`… vs 无前缀的 `Receipt`/`Output`/`Event`/`Clause`/`FilteredEvent`…
- **converter 命名不统一**:`Convert*`(11 个) vs `BuildJSON*`(`BuildJSONBlockSummary`/`BuildJSONEmbeddedTxs`/`buildJSONOutput`)。
- **放置不统一**:`ConvertTransaction` 及其返回类型 `Transaction` 孤立在子包 `api/transactions/transactions_converter.go:38`,还反向 import 顶层 `api` 的 `Clause`/`ConvertClause`/`TxMeta` —— 与"DTO 在顶端"的规则自相矛盾。
- **位置化 struct 字面量**(字段错位不报错,脆弱):
  - `transactions_types.go:87` `ReceiptMeta{header.ID(), header.Number(), header.Timestamp(), tx.ID(), origin}`
  - `transactions_types.go:104` `&Output{contractAddr, make(...), make(...)}`
  - `blocks_types.go:169` `&Clause{c.To(), (*math.HexOrDecimal256)(c.Value()), hexutil.Encode(c.Data())}`
- **死代码**:`JSONClause`(`blocks_types.go:47`)全仓无使用点。
- **结构重复**:`common_types.go` 的 `Event`/`Transfer`/`Output` 与 `blocks_types.go` 的 `JSONEvent`/`JSONTransfer`/`JSONOutput` 字段完全同构。

---

## 3. 硬约束

1. **wire 字节冻结**:REST 响应的 JSON 字段名/顺序/`omitempty`/编码类型不得改变(公共 API)。
2. **domain 纯净**:`block`/`tx`/`state`/`chain`/`logdb`/`runtime` 不得引入任何 API/JSON 关注点。
3. **无 import cycle**;且不得让 thorclient 拽进 server-only 依赖(现状已违反,重构应顺带修复)。
4. 风格从简,贴合 thor 现有约定。

---

## 4. Q1 类型组织 —— 推荐 B:抽专用 DTO 子包

去掉 converter 后,DTO 本身只需要 `thor`/`tx`/`block`/`math`/`hexutil`,外加**很轻**的 `logdb`(仅两个请求 DTO 字段用到:`transfers_types.go:23` `TransferFilter.CriteriaSet []*logdb.TransferCriteria`、`events_types.go:94` `EventFilter.Order logdb.Order`;`logdb` 传递依赖不含 chain/state/muxdb)。

| 选项 | DTO 位置 | thorclient 拽入的 server-only 依赖 | 环 | 评价 |
|---|---|---|---|---|
| A 现状扁平 | 顶层 api,DTO+converter 同包 | muxdb,chain,state,runtime,txpool,comm,logdb,bft(**已违反约束3**) | 无 | 丑 + 漏重依赖给 client |
| **B 专用 DTO 子包**(如 `api/restmodel`) | 纯 DTO 移入子包;converter 留各 server 包 | 仅 `logdb`(可选再消除);其余全部脱钩 | 无 | **推荐** |
| C 每域一个 types 子包 | `api/blocks/blocktypes`… | 同 B | 有风险 | 跨域共享类型(`common` 的 Event/Transfer/Clause)会逼出再拆一个 common 包 = "B + 碎片化",不推荐 |
| D DTO 下沉 handler 子包 | `api/blocks` 自持 | **最差**:client import handler 即吃全套 server 依赖(现有 `api/transactions.Transaction` 就是此反模式) | 高 | handler 间易成环,拒绝 |

**依赖方向(推荐 B,单向无环)**:

```
thor / tx / block / logdb
        ▲
   api/restmodel  (纯 DTO)
        ▲
   ┌────┴─────────────────────────┐
api/blocks…(handler + converter)   thorclient/*
```

server-only 包(chain/state/runtime/txpool/comm/muxdb/bft)只被 handler 引用,永不进入 DTO 子包 → 永不进入 thorclient。

**环判定**:B 不成环(DTO 子包不 import 任何 handler/thorclient/server-only 包);D 的环风险来自 handler 互相 import(如 `api/node` 已调用 `transactions.ConvertTransaction`,DTO 下沉后 blocks↔transactions↔node 易成环)。

### 4.1 8 个 server-only 包逐个怎么切断(证据 `go list`)

thorclient 只 import 两个 server 侧包,构成两条入口边:
- **边①** `thorclient → api`(顶层 DTO+converter 包):`api` 直接 import `chain`/`comm`/`logdb`/`runtime`。
- **边②** `thorclient → api/transactions`(handler 包):直接 import `chain`/`txpool`,并经 `api/transactions → api/restutil → bft` 带进 `bft`。

真正的"直接边"只有 5 个包;`muxdb`/`state` 是它们的传递依赖,不是独立问题。

| 包 | 入口 | 现证据 | B 如何切断 |
|---|---|---|---|
| `chain` | 边① converter + 边② handler | `blocks_types.go:102`、`events_types.go:97/167`;`api/transactions` 直接 import | converter 移入 handler 包;thorclient 改依赖 restmodel、不再 import `api/transactions` |
| `runtime` | 边① converter | `account_types.go:53` `ConvertCallResultWithInputGas(*runtime.Output)` | converter 移入 handler(accounts) |
| `comm` | 边① converter + 接口 | `node_types.go:31` `ConvertPeersStats`、`:13` `Network` | 一并移入 handler(node) |
| `txpool` | 边② handler | `api/transactions` 直接 import | thorclient 只需 `Transaction` DTO→移入 restmodel,断开 handler 边 |
| `bft` | 边② `api/transactions→api/restutil` | `api/transactions` 传递依赖含 bft(`api` 本身不含) | 同上,断开 handler 边即断 |
| `muxdb` | 传递(chain/runtime/comm/txpool) | — | 上面切完**自动消失** |
| `state` | 传递(runtime/comm/txpool) | — | **自动消失** |
| `logdb` | **DTO 字段**(唯一非 converter 来源) | `events_types.go:94` `EventFilter.Order`、`transfers_types.go:23` `TransferFilter.CriteriaSet` | B 后仍在(轻,不带 chain/state/muxdb);B+ 用 restmodel 本地 `Order`/`TransferCriteria` 清零 |

**机制两刀**:(1) 所有 converter 从 DTO 包挪进各 handler 包 → 切断边①的 chain/runtime/comm;(2) thorclient 依赖纯 DTO 子包 restmodel 而非 handler 包 `api/transactions` → 切断边②的 chain/txpool/bft。muxdb/state 随之脱落,仅剩 logdb。

**顺带归位**:把孤立的 `api/transactions.Transaction` + `ConvertTransaction` 归位(`Transaction` 进 DTO 子包,`ConvertTransaction` 进 `api/transactions` handler 包),消除"一个 DTO 在子包"的例外。

**可选加强(B+)**:若想把 `logdb` 也从 thorclient 摘掉,在 DTO 子包定义 API 本地的 `Order`(string)与 `TransferCriteria`(保持 json tag/字段顺序不变),handler 侧再转 `logdb` 类型。收益有限(logdb 很轻),列为可选。

---

## 5. Q2 JSON 转换 —— 推荐 A(手写标准化)+ E(泛型 mapSlice)

当前正确输出来自「struct 上的 json tag + `hexutil`/`math.HexOrDecimal256` 类型」。**任何不改 struct 定义/字段顺序/tag/omitempty 的改写都是 wire-safe。**

| 选项 | wire 影响 | 优雅度 | 复杂度 | 结论 |
|---|---|---|---|---|
| **A 手写标准化**(keyed 字面量 + 统一命名/放置) | 零 | 中高 | 低 | **推荐(核心)** |
| **E 泛型 `mapSlice[A,B]` 削 slice 样板** | 零 | 中 | 低 | **推荐(辅助)** |
| B DTO 上实现 `MarshalJSON` | 需人工与 tag 保持一致,易漂移 | 低 | 中 | 不推荐(tag 已够用,无收益) |
| C domain 类型上 `MarshalJSON` | — | — | **违反约束2** | 拒绝 |
| D struct tag + 反射/通用 marshaler | — | — | 转换含业务计算,反射表达不了 | 拒绝 |
| F 代码生成 | 零 | 中 | 高(工具链/心智);~12 个函数不值 | 拒绝 |

**D/F 拒绝的实质**:转换不是纯字段搬运,含业务计算 —— 合约地址推导(`blocks_types.go:135`/`transactions_types.go:101` `thor.CreateContractAddress`,仅 `clause.To()==nil` 时)、tx 类型分支(legacy 用 `GasPriceCoef`,动态费用用 `MaxFeePerGas`)、topics 过滤(`events_types.go:48` 只收非 nil topic)、reverted 跳过 outputs(`blocks_types.go:174`)。反射/朴素 codegen 覆盖不了。

**E 的落点**:最大样板是 Events/Transfers/Outputs/Clauses 的 `make + for-append`,用一个与 wire 无关的泛型 helper 削掉:

```go
func mapSlice[A, B any](in []A, f func(A) B) []B { … }
```

### 代表性改造(`ConvertReceipt`;位置化 → keyed;输出逐字节不变)

改造前(`transactions_types.go:87`,位置化,字段错位不报错):
```go
Meta: ReceiptMeta{header.ID(), header.Number(), header.Timestamp(), tx.ID(), origin},
...
otp := &Output{contractAddr, make([]*Event, len(output.Events)), make([]*Transfer, len(output.Transfers))}
```

改造后(keyed + mapSlice,安全;struct/tag/字段顺序未动 → JSON 字节相同):
```go
Meta: ReceiptMeta{
    BlockID:        header.ID(),
    BlockNumber:    header.Number(),
    BlockTimestamp: header.Timestamp(),
    TxID:           tx.ID(),
    TxOrigin:       origin,
},
...
otp := &Output{ContractAddress: contractAddr}
otp.Events = mapSlice(output.Events, convertEvent)
otp.Transfers = mapSlice(output.Transfers, convertTransfer)
```

wire 安全论证:JSON key 顺序由 struct 定义顺序决定,未改 struct;keyed 字面量与泛型 helper 都不触碰 tag/字段/类型 → `encoding/json` 输出字节一致。

---

## 6. 综合建议与分阶段迁移

**推荐组合:Q1 = B(专用 DTO 子包) + Q2 = A(手写标准化) + E(泛型 mapSlice)。**

每阶段结束跑全量 API 测试 + JSON golden 快照对拍,确保字节不变:

- **阶段 0 基线**:固化现有 REST 响应 JSON 快照(golden files),作为字节级回归基准。
- **阶段 1(低风险,无移动)**:位置化字面量 → keyed;删死代码 `JSONClause`;引入 `mapSlice`。纯包内改写。
- **阶段 2(结构)**:新建 DTO 子包(如 `api/restmodel`),纯 DTO struct 移入;converter 留原 handler 包并改引 DTO 子包;顶层 `api` 保留 type alias 过渡(`type Receipt = restmodel.Receipt`),避免一次性改动 20 个 thorclient 文件与外部 SDK。
- **阶段 3(归位)**:`api/transactions.Transaction`/`ConvertTransaction` 归位。
- **阶段 4(命名,可选/谨慎)**:统一 `BuildJSON*` → `Convert*`;消除 `Event`/`JSONEvent` 等重复类型。**注意:重命名/删除导出标识符是 Go API 破坏性变更**(非 wire 破坏),波及 thorclient 及外部 Go SDK,需评估价值,或用 alias 缓冲。
- **阶段 5**:移除阶段 2 的过渡 alias(若决定彻底切换)。

**验收**:迁移后 `go list -deps ./thorclient/...` 断言不含 `chain/state/muxdb/runtime/txpool/comm/bft`;golden 快照零 diff;全量 API 测试通过。

---

## 7. 风险

| | 风险 | 缓解 |
|---|---|---|
| R1 | wire 漂移(唯一真实风险) | golden JSON 快照 + 现有 `*_types_test.go` 兜底 |
| R2 | Go API 破坏(类型迁移/改名影响外部 import) | type alias 过渡;破坏性动作单独 PR |
| R3 | import cycle 回归 | 迁移后 `go list -deps ./thorclient/...` 断言 |
| R4 | logdb 残留(B 后 thorclient 仍含 logdb,轻) | 需清零走 B+,收益小 |

---

## 8. 明确"不做什么"

- 不改任何 REST 响应的 JSON 字段名/顺序/形状/omitempty/编码类型(字节级冻结)。
- 不在 domain 包加 `MarshalJSON` 或任何 API/JSON 关注点(排除 Q2-C)。
- 不用反射/通用 marshaler 替代含计算的转换逻辑(排除 Q2-D)。
- 不引入代码生成工具链(规模不够,违背从简风格)(排除 Q2-F)。
- 不把 DTO 下沉 handler 子包(泄漏 server 依赖 + 成环)(排除 Q1-D)。
- 不做每域一个 types 子包(共享类型逼出 common 包,更碎)(排除 Q1-C)。
- 命名统一/去重是可选项,非必须;若做则单独 PR + alias 缓冲,不与结构迁移混在一起放大回归面。
