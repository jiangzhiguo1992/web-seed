# 能力契约（capabilities）

> 能力层分三层：**methods**（原子方法）、**flows**（编排）、**protocols**（传输协议）。
> 依赖方向单向：`protocols → flows → methods → data`。capabilities 不引用 business。

---

## 1. 三层职责

| 目录 | 回答的问题 | 真相源内容 | 产物 |
|---|---|---|---|
| `methods/<module>.yaml` | 模块能提供哪些**原子方法** | interface、方法签名、错误、事务、持久化归属 | Go interface、mock、写归属校验 |
| `flows/<module>.yaml` | 一个模块的**可独立触发用例集**如何编排 | flows 列表：steps、权限、审计、事务、幂等、重试 | 用例骨架、集成测试、影响面 |
| `protocols/<protocol>/<module>.yaml` | 能力如何通过某种**协议**暴露 | path、method、status、header、schema 引用 | server 类型、client、合并 spec |

一句话区分：

- **method = 一个方法一件事**，跨模块可见，不直接对进程外暴露。
- **flow = 最小可独立触发的编排单元**，可以只有一步；同步或异步由 `trigger` 决定。
- **protocol = flow 到传输协议的绑定**，只写协议差异。

---

## 2. 依赖方向

```
protocols ──x-flow──> flows ──use──> methods ──queries──> data.queries
     │                   │              └──persistence──> data.tables
     └──不引用 methods    └──不引用 data.queries
```

| 边 | 写在哪 | 字段 |
|---|---|---|
| protocol → flow | `protocols/<protocol>/*.yaml` | `x-flow` |
| flow → method | `flows/*.yaml` 的 steps | `use: cap.methods.*` |
| method → query | `methods/*.yaml` 的 `interface.methods[].queries` | `data.queries.*` |
| method → table | `methods/*.yaml` | `persistence.table/tables` + `constraint_errors` |

**禁止**：

- protocol 直接引用 `cap.methods.*`（对外入口只能是 flow）。
- flow 直接引用 `data.queries.*`（query 只能由 method 调用；flow 只能编排 method 或内部逻辑）。
- flow 嵌套另一个 flow（编排只到 method 层）。
- capabilities 任何位置出现 `business.*`。

---

## 3. methods

`methods/<module>.yaml` 登记模块对**其他模块**暴露的原子方法。模块内部私有方法不进契约——Go 编译器已经能约束。

必须声明：

- `interface.name` / `interface.package` / `interface.methods`
- 每个方法的 `params` / `returns` / `errors` / `transaction` / `queries`
- `persistence`（表归属、约束错误映射）

类型级负向约束：

- 不要在 interface 里暴露 `*pgx.Tx` 或 HTTP 类型。
- 不要返回表行 `data.tables.*.row`；返回 `data.entities.*` 或本地 `types.*`。
- 只登记跨模块调用的方法；模块内 helper 不要进契约。
- 写表必须属于 `module`；每个方法用 `queries` 列出它调用的 query 项，query 的 `used_by` 由 `make gen-graph` 反向生成。
- 跨模块写不允许由一个 query 完成：拆成多个单表 query，由 flow 编排多个 method；flow 的 `transaction` 覆盖所有 steps。
- 不要声明权限/审计/限流/幂等——这些属于 flow。
- `used_by.flows` 由 `make gen-graph` 写回；不要手改。
- `transaction` 只写 `none` 或 `read_only`；写事务由 flow 管理。
- `types` 可以引用其他 methods 文件的类型（如 `AuthSession` 引用 `TokenPair`）；生成器按文件依赖解析。
- 不要引用 `business.*`。

---

## 4. flows

`flows/<module>.yaml` 是编排层，一个文件承载一个模块的多个 flow item。同步和异步共用同一套 `steps` 结构，通过 `trigger` 区分。

### 4.1 同步 flow

不写 `trigger`，由 `protocols` 的 `x-flow` 引用。

必填：`module` / `steps` / `permissions` / `transaction` / `errors`。

### 4.2 异步 flow

写 `trigger`：

```yaml
trigger:
  type: cron | event | webhook
  schedule: "0 4 * * *"       # type=cron
  event: business.events.*    # type=event（唯一允许引用 business 的位置）
```

必填：`module` / `trigger` / `input` / `output` / `steps` / `idempotency` / `retry` / `concurrency`。

### 4.3 共同约束

- `steps[].use` 只允许：`cap.methods.*`、`platform.security.permissions.*`；不写 `use` 表示 flow 内部逻辑。
- flow 不得直接引用 `data.queries.*`；query 只能由 method 调用。
- `steps[].on_error` 必须是 `use` 目标声明的 `errors` 子集。
- `compensation` 必须结构化：`action: rollback | compensate | dead_letter`。
- `postconditions` 必须结构化：`{ field, op, value }`，不要写散文。
- 一个 flow 只有一步也可以；判断标准是“是否需要独立的触发入口、权限边界和事务边界”，不是步骤数。
- 异步 flow 不允许被 `protocols` 引用；同步 flow 必须至少被一个 protocol 引用，否则是孤儿。

---

## 5. protocols

`protocols/<protocol>/<module>.yaml` 只写协议差异：

- path / method / operationId
- status code / content-type / header
- request/response 的 schema `$ref`
- `x-flow: cap.flows.*`

**不要写**：权限、审计、限流、幂等、事务——这些在 flow。

当前只启用 `http`（OpenAPI 3.1 片段）。`grpc` / `graphql` 未来按需新增，新增时只加目录与对应生成器，不改能力层。

HTTP 约束：

- 响应 schema 必须 `additionalProperties: false`。
- 错误响应统一引用 `protocols/http/shared.yaml` 的 `Problem`。
- 默认错误响应由 `default_error_responses` 注入，不要逐接口重复声明。
- 每个 operation 必须写 `x-contract-id` 与 `x-flow`。
- 不要手写与 method 重复的 schema；request/response 从 flow 的 `input/output` 引用。

---

## 6. capabilities 不依赖 business

capabilities 契约里**不出现任何 `business.*`**。方向只有一个：

```
business ──implemented_by / emitted_by──> capabilities
```

| 不要写 | 改写为 |
|---|---|
| `x-preconditions: business.rules.*` | 由 `business/rules` 的 `implemented_by` 声明 |
| `emits: business.events.*` | 由 `business/events` 的 `emitted_by` 声明 |
| `use: business.states.account.transition` | 状态转换封装在 `cap.methods.*` 内；flow 只调用 method |

唯一例外：异步 flow 的 `trigger.event` 引用 `business.events.*`，因为事件就是它的输入。这只是“引用输入类型”，不是“声明实现关系”。

---

## 7. 目录

```
contracts/capabilities/
├── README.md
├── methods/<module>.yaml
├── flows/<module>.yaml
└── protocols/
    ├── http/
    │   ├── shared.yaml
    │   └── <module>.yaml
    ├── grpc/          # 未来
    └── graphql/       # 未来
```

ID 派生：

| 目录 | ID | 示例 |
|---|---|---|
| `methods/` | `cap.methods.<file>` | `cap.methods.account` |
| `flows/` | `cap.flows.<file>.<name>` | `cap.flows.account.delete` |
| `protocols/http/` | `cap.http.<file>` | `cap.http.account` |
| `protocols/grpc/` | `cap.grpc.<file>` | `cap.grpc.account` |
| `protocols/graphql/` | `cap.graphql.<file>` | `cap.graphql.account` |

方法/步骤项 ID 由文件 ID + 显式 `name` 派生：

- method 项：`cap.methods.<file>.<snake_case(method)>`
- flow 步骤：步骤内 `id`，不是全局契约 ID。

---

## 8. 当前落地状态

- `protocols/http/*.yaml` 已全部使用 `x-flow` 指向同步 flow；`x-service` 已移除。
- `flows/<module>.yaml` 的每个 flow item 用 `implementation: direct`（直通一个 method）或 `implementation: flow`（显式 steps）。
- `methods/*.yaml` 的写方法 `transaction` 已改为 `none`；事务边界在对应的 flow 上声明。
- 原 `cap.flows.account_activation` 已删除：它是单模块内部编排，且引用了 `business.*`。
