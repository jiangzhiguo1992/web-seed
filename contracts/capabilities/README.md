# 能力契约（capabilities）

> 能力层分四类： **services**（服务）、 **async**（异步任务）、 **events**（领域事件）、 **protocols**（传输协议）。
> 业务规则与状态机属于功能规格，放在 `features/{rules,states}`。
> 依赖方向：`protocols → services → data.queries`；`services → feature.rules / feature.states / events`；
> `async → services / events`；`features.pages → services`。

---

## 1. 四类职责

| 目录                                 | 回答的问题                         | 真相源内容                                           | 产物                                   |
|--------------------------------------|------------------------------------|------------------------------------------------------|----------------------------------------|
| `services/<module>.yaml`             | 模块能提供哪些可调用单元、如何实现 | services[]、steps、input/output 数据链、事务、写归属 | Go interface、usecase 骨架、写归属校验 |
| `async/<module>.yaml`                | 哪些任务异步执行、如何触发与重试   | jobs[]、trigger、retry、concurrency、handler service | worker 注册表、调度配置                |
| `events/<subject>.yaml`              | 跨模块异步消息长什么样             | name、version、payload、emitted_by、consumers        | 事件常量、payload 类型                 |
| `protocols/<protocol>/<module>.yaml` | 服务如何通过某种协议暴露           | path、method、status、header、schema 引用            | server 类型、client、合并 spec         |

一句话区分：

- **service = 一个可调用单元**，有明确的 params/returns（末位显式 `err`）和 steps；同步调用，不声明 trigger。
- **async = 一个异步任务**，引用一个同步 service 作为 handler，声明 trigger/retry/concurrency。
- **step = 服务实现路径上的一步**，四类：query / service / logic / transition。
- **rule / state 在 features**，被 service 的 `kind: logic` / `kind: transition` step 引用。
- **event = 跨模块消息契约**，由 service step 的 `events` 发射，被 async 的 `trigger.event` 消费。
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
     ├──不引用 queries      └──kind=query────> 本模块 data.queries

async ──service──> services
  └──trigger.event──> events
```

| 边                   | 写在哪                             | 字段                                   |
|----------------------|------------------------------------|----------------------------------------|
| protocol → service   | `protocols/<protocol>/*.yaml`      | `x-service`                            |
| service → query      | `services/*.yaml` 的 steps         | `kind: query` + `query`                |
| service → service    | `services/*.yaml` 的 steps         | `kind: service` + `service`            |
| service → rule       | `services/*.yaml` 的 steps         | `kind: logic/transition` + `rules`     |
| service → state      | `services/*.yaml` 的 steps         | `kind: transition` + `transitions`     |
| service → event      | `services/*.yaml` 的 steps         | `kind: logic/transition` + `events`    |
| async → service      | `async/*.yaml` 的 jobs             | `service`                              |
| async → event        | `async/*.yaml` 的 jobs             | `trigger.event`                        |
| feature → service    | `features/**`                      | `capability: cap.services.*`           |
| feature → rule/state | `features/{pages,rules,states}/**` | `feature.rules.*` / `feature.states.*` |

**禁止**：

- protocol 直接引用 `data.queries.*` 或 `feature.rules.*`（对外入口只能是 service）。
- `kind: query` 引用其他模块的 query（跨模块必须走 `kind: service`）。
- service 调用图成环。
- async 引用被 protocol 绑定的 service。
- service 或 async 声明 `business.*`（business 层已移除）。

---

## 3. services

`services/<module>.yaml` 一个文件对应一个 Go interface，接口名由 `owner_module + file` 派生：`<Owner><File>Service`，owner
与 file 相同时折叠为 `<Owner>Service`（account/account → AccountService，account/auth → AccountAuthService）。`services[]`
的每一项是一个可调用单元。

文件级字段：

- `schemas`（可选；本地 schema，值是值域节点，`object.properties[]` 的条目按 `_meta/_schemas.yaml` 的 `property.keys`
  校验，标量约束引用 `_types`，可跨文件引用）

入口权限、审计与限流在 `contracts/platform/security/access.yaml` 绑定，不在 service 项声明。query 的 `errors` 在
`data/queries/*.yaml` 的查询项上声明；它只列 query 自身的领域错误，输入校验、权限与 DB 引擎错误不在 query 内枚举，
`errors: []` 表示没有领域错误。

service 项必须声明：

- `name` / `summary` / `visibility`（`public` 跨模块可见，`internal` 仅本模块可见）
- `params` / `returns`；最后一项必须是 `{name: err, type: error}`，不能省略；无业务返回值也必须写 `err`
- `transaction`（`required` / `read_only` / `none`）
- `steps`（至少一步）

### 3.1 steps

| kind         | 必填                                                                    | 说明                                                                                                                                                              |
|--------------|-------------------------------------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `query`      | `queries`                                                               | 调用本模块 `data.queries.*`；`queries[].input` 的 key 必须是查询参数名，必填参数必须全部出现，不写 `ctx`（生成器注入）；`step.errors` 缺省取所有 query 错误的并集 |
| `service`    | `services`                                                              | 调用其他（或本模块）service；`services[].input[].name` 必须是目标参数名，必填参数必须全部出现；跨模块目标必须 `public`                                            |
| `logic`      | `output` + `errors`；可选 `input` / `queries` / `rules` / `events`      | 手写实现；`queries` 是 `{query, input}` 对象列表；`input` 声明本步读取的入参与用途                                                                                |
| `transition` | `transitions`；可选 `input` / `output` / `queries` / `rules` / `events` | 执行 `feature.states.*` 转换                                                                                                                                      |

通用字段：`id` / `kind` / `summary` / `depends_on` / `errors` / `input`。

- `input` 是 `{name, value, description}` 列表；`value` 只能是 `params.<path>`、`step.<id>.<path>` 或 `expr:` 表达式；
  `description` 说明这项输入的业务含义，分支条件也写在这里。
- `depends_on` 是可选顺序提示；`input` / `queries[].input` 引用 `step.<id>` 只要求该步骤在前序，不要求写入 `depends_on`。
- `queries` 是 `{query, input}` 列表；`kind: query` 也使用它，一步可以声明多个 query，由实现决定实际执行哪个。
- `services` 是 `{service, input}` 列表，`input` 是 `{name, value, description}` 列表；一步可以调用多个 service。
- 入口策略错误（权限→`COMMON_FORBIDDEN`、限流→`COMMON_RATE_LIMITED`、审计→`COMMON_AUDIT_FAILED`）只由 `access.yaml`
  拥有，service 与 step 不重复声明。
- `step.errors` 只列该步骤实际返回的领域错误：`kind: query` 缺省取所有 queries 的并集；`kind: service` 必须是目标 service
  错误并集的子集；`logic/transition` 必须覆盖其 `queries` 与 `rules.on_violation` 的 `errors`。
- 数据链是硬约束：`input` 的值只能是 `params.<path>`、`step.<id>.<path>` 或显式 `expr:`；`logic/transition` 的每个显式
  `output` 必须被后续 `input`/`queries[].input` 引用，或与 `returns` 业务字段同名；`returns` 的每个业务字段必须有同名、类型一致且
  `nullable` 一致的产出（`*T` 视为可空；`err` 不参与）；service 的每个非 `ctx` 参数必须被至少一个 `input` 引用。
- `expr:` 是显式软区：表示该值由本步内部计算，checker 只校验非空，不解析来源，也不把它计入 `input`/`step` 的消费关系；能用
  `input.` / `step.` 表达的来源不得用 `expr:` 绕过。
- 分支、循环与错误流转由实现决定；契约不声明 `when` / `on_error` / `max_iterations`。

### 3.2 事务与写归属

- `transaction` 是 service 级事务边界；生成器在 usecase 方法体外层包裹 `WithTx`。`read_only` 不得调用写表或返回
  `affected_rows` 的 query；写操作必须声明 `required` 或 `none`。
- `kind: query` 的写表必须属于文件 `owner_module`；跨模块写必须走 `kind: service`。
- 写表由 query 的 `tables[].write` 反向推导，不再单独声明；`queries[].query` 的 `errors` 必须被 `step.errors` 覆盖。
- `query.used_by` 由 `make gen-graph` 写回，只允许 `cap.services.*`，且不得为空。

---

## 4. 业务规格在 features

`rules` / `states` 属于功能规格，放在 `contracts/features/{rules,states}/`：

- service 的 `kind: logic` step 用 `rules: [feature.rules.*]` 引用业务规则；
- service 的 `kind: transition` step 用 `transitions: [feature.states.*]` 引用状态转换；
- `feature.rules` / `feature.states` 的 `implemented_by` 反向指向 `cap.services.*`。

capabilities 不再有 `rules/` / `states/` 目录。

---

## 5. async

`async/<module>.yaml` 按归属模块组织。一个 job 只做调度，业务步骤属于被引用的 service。

- 文件级：`id` / `summary` / `owner_module` / `schema_version` / `stability` / `do_not` / `jobs`
- job 项：`id` / `name` / `summary` / `service` / `trigger` / `retry` / `concurrency`
- `service` 必须与 async 文件同 `owner_module`；被 async 引用的 service 不得再被 `cap.protocols.http` 绑定。
- `trigger.type`：`cron`（必须 `schedule`）/ `event`（必须存在的 `cap.events.*`）/ `webhook`（不得写 `schedule`/`event`）。
- `retry`：`max_attempts` / `backoff`（`fixed` / `exponential`）/ `dead_letter`。
- `concurrency`：`key` / `ttl`。

---

## 6. events

`events/<subject>.yaml` 按业务主体组织（不按生产者模块）：

- 每个事件有 `id` / `name` / `version` / `payload` / `emitted_by` / `consumers`；
- `payload` 是 schema 值域节点：`type: object` + `properties`（property 条目列表，条目携带 `required`/`nullable`/方向注解）、
  `type: array` + `items`、标量 `type` 或 `ref`（entity/enum/本地 schema）；
- `emitted_by` 指向发射事件的服务项，`consumers` 由 async job 的 `trigger.event` 生成，二者都由 `make gen-graph` 维护；
- 事件发射由 service step 的 `events` 声明，不是 service 项。

---

## 7. protocols

`protocols/<protocol>/<module>.yaml` 只写协议差异：

- path / method / operationId / `x-contract-id` / `x-service`
- status code / content-type / header
- request/response 的 schema `$ref`；request 用 `x-service-type`、response 用 `x-service-return` 指向 service 的
  params/returns
- `components.schemas` 通过 `x-service-type` / `x-enum` / `x-entity` 指向唯一来源

**不要写**：权限、审计、限流（在 `platform/security/access.yaml`）、事务（在 service 项）。

HTTP 约束：

- 响应 schema 由 `cap.services.schemas` 生成，object 默认封闭。
- 错误响应统一引用 `protocols/http/shared.yaml` 的 `Problem`。
- 默认错误响应由 `default_error_responses` 注入，不要逐接口重复声明；operation 需要更细粒度时用 `x-default-errors` 覆盖。
- 503 必须通过 shared 的 `ServiceUnavailable` 返回 `Retry-After`。
- 每个 operation 必须写 `x-contract-id` 与 `x-service`；`x-service` 只能指向 `public` 且无 async job 的同步 service。

---

## 8. 目录与 ID

```
contracts/capabilities/
├── README.md
├── services/<module>.yaml
├── async/<module>.yaml
├── events/<subject>.yaml
└── protocols/
    ├── http/
    │   ├── shared.yaml
    │   └── <module>.yaml
    ├── grpc/          # 未来
    └── graphql/       # 未来
```

| 目录              | ID                          | 示例                         |
|-------------------|-----------------------------|------------------------------|
| `services/`       | `cap.services.<file>`       | `cap.services.account`       |
| `async/`          | `cap.async.<file>`          | `cap.async.token`            |
| `events/`         | `cap.events.<file>`         | `cap.events.account`         |
| `protocols/http/` | `cap.protocols.http.<file>` | `cap.protocols.http.account` |

方法/步骤项 ID：

- service 项：`cap.services.<file>.<snake_case(name)>`
- async job：`cap.async.<file>.<snake_case(name)>`
- rule 项：显式 `id`
- state 转换：`feature.states.<file>.<snake_case(transition)>`
- event 项：显式 `id`
- step：步骤内 `id`，不是全局契约 ID

---

## 9. 当前落地状态

- `services/*.yaml` 已合并原 methods + flows；每个 service 项都有 steps。
- `async/*.yaml` 承载异步调度；`services` 不再声明 `trigger/retry/concurrency`。
- `features/rules/*` / `features/states/*` 承载业务规则与状态机；`capabilities/events/*` 承载领域事件。
- `protocols/http/*.yaml` 已全部使用 `x-service` 指向同步 service，response 用 `x-service-return` 绑定 returns。
- `data.queries.*.used_by` 由 `make gen-graph` 写回，只允许 `cap.services.*`，不得为空。
- 术语表在 `docs/reference/glossary.md`（review-only，不占契约组）。
