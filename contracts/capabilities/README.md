# 能力契约（capabilities）

> 能力层分三类：**services**（服务）、**events**（领域事件）、**protocols**（传输协议）。
> 业务规则与状态机属于功能规格，放在 `features/{rules,states}`。
> 依赖方向：`protocols → services → data.queries`；`services → feature.rules / feature.states / events`；`features.pages → services`。

---

## 1. 三类职责

| 目录 | 回答的问题 | 真相源内容 | 产物 |
|---|---|---|---|
| `services/<module>.yaml` | 模块能提供哪些服务、每个服务如何实现 | interface、services[]、steps、errors、事务、持久化归属 | Go interface、usecase 骨架、写归属校验 |
| `events/<subject>.yaml` | 跨模块异步消息长什么样 | name、version、payload、emitted_by | 事件常量、payload 类型 |
| `protocols/<protocol>/<module>.yaml` | 服务如何通过某种协议暴露 | path、method、status、header、schema 引用 | server 类型、client、合并 spec |

一句话区分：

- **service = 一个可调用单元**（同步/异步/内部），有明确的 params/returns/errors 和 steps。
- **step = 服务实现路径上的一步**，五类：query / service / logic / event / transition。
- **rule / state 在 features**，被 service 的 `kind: logic` / `kind: transition` step 引用。
- **event = 跨模块消息契约**，被 service 的 `kind: event` step 发射，被异步 service 的 `trigger.event` 消费。
- **protocol = service 到传输协议的绑定**，只写协议差异。

---

## 2. 依赖方向

```
protocols ──x-service──> services ──steps──> data.queries
     │                     │
     │                     ├──kind=service──> services（跨模块）
     │                     ├──rules─────────> features.rules
     │                     ├──transition────> features.states
     │                     └──event─────────> events
     └──不引用 queries      └──kind=query────> 本模块 data.queries
```

| 边 | 写在哪 | 字段 |
|---|---|---|
| protocol → service | `protocols/<protocol>/*.yaml` | `x-service` |
| service → query | `services/*.yaml` 的 steps | `kind: query` + `query` |
| service → service | `services/*.yaml` 的 steps | `kind: service` + `service` |
| service → rule | `services/*.yaml` 的 steps | `kind: logic` + `rules: [feature.rules.*]` |
| service → state | `services/*.yaml` 的 steps | `kind: transition` + `transition: feature.states.*` |
| service → event | `services/*.yaml` 的 steps | `kind: event` + `event` |
| feature → service | `features/**` | `capability: cap.services.*` |
| feature → rule/state | `features/{pages,rules,states}/**` | `feature.rules.*` / `feature.states.*` |

**禁止**：

- protocol 直接引用 `data.queries.*` 或 `feature.rules.*`（对外入口只能是 service）。
- `kind: query` 引用其他模块的 query（跨模块必须走 `kind: service`）。
- flow 嵌套 flow；service 调用图不得成环。
- service 声明 `business.*`（business 层已移除）。

---

## 3. services

`services/<module>.yaml` 一个文件对应一个 Go interface。`services[]` 的每一项是一个可调用单元。

文件级必须声明：

- `interface.name` / `interface.package`（`internal/modules/<module>/api`）
- `persistence.tables` / `persistence.constraint_errors`（本文件可写的表与约束错误映射）
- `types`（本地对象 schema；可跨文件引用）

service 项必须声明：

- `name` / `summary` / `visibility`（`public` 跨模块可见，`internal` 仅本模块可见）
- `params` / `returns` / `errors`
- `transaction`（`required` / `read_only` / `none`）
- `steps`（至少一步）

可选：`permissions` / `audit` / `rate_limit` / `idempotency`（入口生成器消费）；`trigger` / `retry` / `concurrency`（异步入口）；`max_iterations`（后向 goto）。

### 3.1 steps

| kind | 必填 | 说明 |
|---|---|---|
| `query` | `query` + `params` | 调用本模块 `data.queries.*` |
| `service` | `service` + `params` | 调用其他（或本模块）service；跨模块目标必须 `public` |
| `logic` | `output` + `errors`；可选 `queries` / `rules` | 手写实现；`queries` 用于 query 反向索引，`rules` 是业务依据 |
| `event` | `event` + `payload` | 发射 `cap.events.*`，生成器在事务提交后执行 |
| `transition` | `transition` | 执行 `feature.states.*` 转换；可选 `queries` / `output` |

通用字段：`id` / `kind` / `summary` / `depends_on` / `when` / `on_error` / `errors`。

- `when` 是受限布尔表达式（`true/false`、`!`、`&&`、`||`、括号、`input.*` / `step.<id>.*` 与字面量比较）；静态 false 的步骤不可达。
- `on_error` 是 `{error, action, target}` 列表；`action: abort | continue | goto`；存在后向/自环 goto 时 `max_iterations` 必填。
- `service.errors` 必须覆盖所有可达步骤的未处理错误与入口策略错误（权限→`COMMON_FORBIDDEN`、限流→`COMMON_RATE_LIMITED`、审计→`COMMON_AUDIT_FAILED`、幂等→`COMMON_IDEMPOTENCY_CONFLICT`、循环耗尽→`COMMON_LOOP_EXHAUSTED`）。

### 3.2 事务与写归属

- `transaction` 是 service 级事务边界；生成器在 usecase 方法体外层包裹 `WithTx`。
- `kind: query` 的写表必须属于文件 `owner_module`；跨模块写必须走 `kind: service`。
- `persistence.tables` 必须覆盖所有 query 写表；`constraint_errors` 的键必须是所写表的约束或索引名。
- `query.used_by` 由 `make gen-graph` 写回，只允许 `cap.services.*`，且不得为空。

---

## 4. 业务规格在 features

`rules` / `states` 属于功能规格，放在 `contracts/features/{rules,states}/`：

- service 的 `kind: logic` step 用 `rules: [feature.rules.*]` 引用业务规则；
- service 的 `kind: transition` step 用 `transition: feature.states.*` 引用状态转换；
- `feature.rules` / `feature.states` 的 `implemented_by` 反向指向 `cap.services.*`。

capabilities 不再有 `rules/` / `states/` 目录。

## 5. events

`events/<subject>.yaml` 按业务主体组织（不按生产者模块）：

- 每个事件有 `id` / `name` / `version` / `payload` / `emitted_by` / `consumers`；
- `payload` 是 object 且 `additionalProperties: false`；
- `emitted_by` 指向 `cap.services.*`；`consumers` 是消费模块；
- 事件发射是 service 的 `kind: event` step，不是 service 项。

## 6. protocols

`protocols/<protocol>/<module>.yaml` 只写协议差异：

- path / method / operationId / `x-contract-id` / `x-service`
- status code / content-type / header
- request/response 的 schema `$ref`
- `components.schemas` 通过 `x-service-type` / `x-enum` / `x-entity` 指向唯一来源

**不要写**：权限、审计、限流、幂等、事务——这些在 service。

HTTP 约束：

- 响应 schema 必须 `additionalProperties: false`。
- 错误响应统一引用 `protocols/http/shared.yaml` 的 `Problem`。
- 默认错误响应由 `default_error_responses` 注入，不要逐接口重复声明。
- 503 必须通过 shared 的 `ServiceUnavailable` 返回 `Retry-After`。
- 每个 operation 必须写 `x-contract-id` 与 `x-service`；`x-service` 只能指向 `public` 且无 `trigger` 的同步 service。

---

## 7. 目录与 ID

```
contracts/capabilities/
├── README.md
├── services/<module>.yaml
├── events/<subject>.yaml
└── protocols/
    ├── http/
    │   ├── shared.yaml
    │   └── <module>.yaml
    ├── grpc/          # 未来
    └── graphql/       # 未来
```

| 目录 | ID | 示例 |
|---|---|---|
| `services/` | `cap.services.<file>` | `cap.services.account` |
| `events/` | `cap.events.<file>` | `cap.events.account` |
| `protocols/http/` | `cap.http.<file>` | `cap.http.account` |

方法/步骤项 ID：

- service 项：`cap.services.<file>.<snake_case(name)>`
- rule 项：显式 `id`
- state 转换：`feature.states.<file>.<snake_case(transition)>`
- event 项：显式 `id`
- step：步骤内 `id`，不是全局契约 ID

---

## 8. 当前落地状态

- `services/*.yaml` 已合并原 methods + flows；每个 service 项都有 steps。
- `features/rules/*` / `features/states/*` 承载业务规则与状态机；`capabilities/events/*` 承载领域事件。
- `protocols/http/*.yaml` 已全部使用 `x-service` 指向同步 service。
- `data.queries.*.used_by` 由 `make gen-graph` 写回，只允许 `cap.services.*`，不得为空。
- 术语表在 `docs/reference/glossary.md`（review-only，不占契约组）。
