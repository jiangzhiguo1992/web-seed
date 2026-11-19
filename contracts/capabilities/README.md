# 能力契约（capabilities）

> 能力层分四类： **services**（服务）、 **lifecycles**（生命周期）、 **events**（异步事件与任务）、 **protocols**（传输协议）。
> 业务规则仍属于功能规格，放在 `features/rules`；生命周期是能力层契约。
> 依赖方向：`protocols → services → data.queries`；`services → feature.rules / cap.lifecycles / events`；
> `lifecycles → services`；`events → services`；`features.pages → services / lifecycles`。

---

## 1. 四类职责

| 目录                                 | 回答的问题                         | 真相源内容                                           | 产物                                   |
|--------------------------------------|------------------------------------|------------------------------------------------------|----------------------------------------|
| `services/<module>.yaml`             | 模块能提供哪些可调用单元、如何实现 | services[]、steps、input/output 数据链、事务、写归属 | Go interface、usecase 骨架、写归属校验 |
| `lifecycles/<dimension>.yaml`        | 实体的某个状态机如何创建、流转与销毁 | subjects[{entity,field}]、states(values/initial/final)、create/destroy、transitions | 生命周期常量、转换表、非法动作测试     |
| `events/<module>.yaml`               | 哪些异步主题与任务、如何触发与重试 | topics[]（payload/发布/订阅）、jobs[]（trigger/retry） | 事件常量、payload 类型、worker 注册表  |
| `protocols/<protocol>/<module>.yaml` | 服务如何通过某种协议暴露           | path、method、status、header、schema 引用            | server 类型、client、合并 spec         |

一句话区分：

- **service = 一个可调用单元**，有明确的 params/returns（末位显式 `err`）和 steps；同步调用，不声明 trigger。
- **lifecycle = 一个实体的创建、状态转换与销毁**，每个动作（create/destroy/transition）锚到具体 service step。
- **event = 异步事实**，由 service step 的 `events` 发布到 topic，由 job 的 `trigger.topic` 订阅。
- **job = 异步任务**，引用一个同步 service 作为 handler，声明 `trigger`（cron/topic）/retry/concurrency。
- **step = 服务实现路径上的一步**，五类：query / service / logic / events / when；`events` 是纯事件发射步骤，`when` 是条件分支步骤。
- **rule 在 features**，被 service 的 `kind: logic` step 引用；**lifecycle 在 capabilities**。
- **protocol = service 到传输协议的绑定**，只写协议差异。

---

## 2. 依赖方向

```
protocols ──x-service──> services ──steps──> data.queries
     │                     │
     │                     ├──kind=service──> services（跨模块）
     │                     ├──rules─────────> features.rules
     │                     ├──lifecycles───> lifecycles
     │                     └──events─────────> events
     ├──不引用 queries      └──kind=query────> 本模块 data.queries

lifecycles ──step──> services
events/jobs ──service──> services
events/jobs ──trigger.topic──> events/topics
```

| 边                   | 写在哪                             | 字段                                   |
|----------------------|------------------------------------|----------------------------------------|
| protocol → service   | `protocols/<protocol>/*.yaml`      | `x-service`                            |
| service → query      | `services/*.yaml` 的 steps         | `kind: query` + `query`                |
| service → service    | `services/*.yaml` 的 steps         | `kind: service` + `service`            |
| service → rule       | `services/*.yaml` 的 steps         | `kind: logic/when` + `rules[{rule}]`   |
| service → lifecycle  | `services/*.yaml` 的 steps         | `lifecycles`（任何 kind 均可带）        |
| lifecycle → service  | `lifecycles/*.yaml` 的 actions     | `step` / `implemented_by`（生成）      |
| service → event      | `services/*.yaml` 的 steps         | `kind: events` + `events`              |
| event/job → service  | `events/*.yaml` 的 jobs            | `service`                              |
| event/job → topic    | `events/*.yaml` 的 jobs            | `trigger.topic`                        |
| feature → service    | `features/**`                      | `capability: cap.services.*`           |
| feature → rule/lifecycle | `features/**`                    | `feature.rules.*` / `cap.lifecycles.*` |

**禁止**：

- protocol 直接引用 `data.queries.*` 或 `feature.rules.*`（对外入口只能是 service）。
- `kind: query` 引用其他模块的 query（跨模块必须走 `kind: service`）。
- service 调用图成环。
- job 引用被 protocol 绑定的 service（异步 handler 不对外暴露）。
- service 或 events 声明 `business.*`（business 层已移除）。
- lifecycle 引用不存在的 service step，或 step 不回引 lifecycle 动作。

---

## 3. services

`services/<module>.yaml` 一个文件对应一个 Go interface，接口名由 `owner_module + file` 派生：`<Owner><File>Service`，owner
与 file 相同时折叠为 `<Owner>Service`（account/account → AccountService，account/auth → AccountAuthService）。`services[]`
的每一项是一个可调用单元。

文件级字段：

- `schemas`（可选；本地 schema，键是 Go 类型名，引用时必须写 `cap.services.<file>.schemas.<Name>`；值是值域节点，`object.properties[]` 的条目按 `_meta/_schemas.yaml` 的 `property.keys`
  校验，标量约束引用 `_types`，可跨文件引用）

入口权限、审计与限流在 `contracts/platform/security/access.yaml` 绑定，不在 service 项声明。query 的 `errors` 在
`data/queries/*.yaml` 的查询项上声明；它只列 query 自身的领域错误，输入校验、权限与 DB 引擎错误不在 query 内枚举，
`errors: []` 表示没有领域错误。

service 项必须声明：

- `name` / `summary` / `visibility`（`public` 跨模块可见，`internal` 仅本模块可见）
- `params` / `returns`；最后一项必须是 `{name: err, type: error}`，不能省略；无业务返回值也必须写 `err`；每个业务返回项必须写 `value` 指向来源（`params.<path>` 或前序 `step.<id>.<path>`），`err` 不写 `value`；`params[].default` 只在
  `nullable: true` 时允许，调用方省略时取该值
- `tx_mode`（`required` / `read_only` / `none`）
- `steps`（至少一步）

### 3.1 steps

| kind         | 必填                                                                    | 说明                                                                                                                                                              |
|--------------|-------------------------------------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `query`      | `query`（字符串 ID）                                                     | 调用本模块 `data.queries.*`；`step.input` 的 name 必须是查询参数名，必填参数必须全部出现，不写 `ctx`（生成器注入）；`step.output` 必须是查询 returns 的子集，name/type/nullable 严格一致；`step.errors` 缺省取该 query 错误的并集 |
| `service`    | `service`（字符串 ID）                                                   | 调用其他（或本模块）service；`step.input` 的 name 必须是目标参数名，必填参数必须全部出现；跨模块目标必须 `public`；`step.output` 必须是目标 returns 的子集，name/type/nullable 严格一致 |
| `logic`      | `input` + `output` + `errors` + `rules`（均可为空列表） | 手写判断与数据变换；`rules` 是业务规则依据，必须声明字段；不直接调 query、service 或发事件 |
| `events`     | `events`（至少一项 `{ref}`，可多个）                                     | 纯事件发射；可发多个事件，`step.input` 是这些事件 payload 字段的并集，name 必须属于至少一个事件的 payload；每个事件的必填字段必须出现；`output` 必须为空列表；logic 不直接发事件 |
| `when`       | `input` + `output: [ ]` + `when`；可选 `rules`                          | 条件分支；每项是 `{input, expr, goto}`：`input` 必须是本步 input 中声明的名字，`expr` 只能引用本步 `input.<name>[.<field>]` 且必须引用该主体；按列表顺序匹配，第一个为真的分支生效 |

字段按 kind 取交集：`id` / `kind` / `summary` / `input` / `output` / `errors` / `lifecycles` / `goto` 恒有；`input` / `output` 所有 kind 都必须展示（无内容写空列表），`events` / `when` 的 `output` 必须为空；`query` 仅 `query`；`service` 仅 `service`；`events` 仅 `events`；`rules` 仅 `logic` / `when`；`when` 仅 `when`。

一一对应：`kind: query` → `query`、`kind: service` → `service`、`kind: events` → `events`、`kind: logic` → `rules`、`kind: when` → `when`。出现哪个业务字段，kind 就必须是对应的那一种；logic 不能携带 `query` / `service` / `events`，query / service / events 也不能携带 `rules`。`input` / `output` / `errors` / `goto` 是数据流与公共字段，不参与一一对应。

- `goto` 必填：它是本步结束后的唯一跳转目标，必须指向本 service 的后续 step，或 `$end` 表示直接结束 service；`kind: when` 的 `goto` 是所有分支都不匹配时的默认跳转。控制流没有隐式顺序，前向限制保证无环；首步必须能经 goto/when 分支到达所有 step。
- `input` 是 `{name, value, description}` 列表，所有 step 必填（无输入写 `[ ]`）；`value` 只能是 `params.<path>` 或 `step.<id>.<path>`，禁止内联表达式。query/service 时 `name` 是目标参数名；events 时是至少一个事件 payload 的字段名；logic/when 时是本步读取名。
- `output` 是 `{name, type, description, nullable}` 列表，所有 step 必填（无产出写 `[ ]`）；`events` / `when` 只能为空。query/service 时 `name` 必须是目标 returns 字段，`type/nullable` 必须与目标严格一致；logic 时是自身产出。消费者只能引用已声明的 output，不能直接穿透到目标 returns。
- `lifecycles` 是状态流转标记，可列多个 lifecycle 动作，任何 kind 都可带；它只表示本步包含这些状态流转，不限制本步只做状态流转；位置固定在 `summary` 之后。
- `when` 只用于 `kind: when`：每项 `{input, expr, goto}`；`input` 必须是本步 input 中声明的名字，`expr` 是受限布尔表达式，只能引用本步 `input.<name>[.<field>]`、字面量与 `== != > >= < <= && || !` 和括号，且必须引用 `input` 指定的主体；字面量类型必须与字段静态类型兼容（boolean 用 `true`/`false`，integer 用整数，string 用字符串，`data.enums.*` 用枚举名）。按列表顺序匹配，第一个为真的分支跳转，都不满足时走本步 `goto`。
- 计算值必须先落成前序 `kind: logic` 的 `output`，再以 `step.<id>.<path>` 引用；`expr:` 软区已移除。
- `query` 是 `kind: query` 的单值字符串 ID，实例写作 `query: data.queries.<file>.<name>`；logic 不直接调 query，查询一律独立成 step。
- `service` 是 `kind: service` 的单值字符串 ID，实例写作 `service: cap.services.<file>.<name>`；logic 不直接调 service，service 调用一律独立成 step。
- `events` 是 `kind: events` 的 `[{ref}]` 列表，可发多个事件；payload 由本步 input 按字段名映射，input 是所有事件 payload 字段的并集；logic 不直接发事件。
- 入口策略错误（权限→`COMMON_FORBIDDEN`、限流→`COMMON_RATE_LIMITED`、审计→`COMMON_AUDIT_FAILED`）只由 `access.yaml`
  拥有，service 与 step 不重复声明。
- `step.errors` 只列该步骤实际返回的领域错误：`kind: query` 缺省取该 query 的错误集，声明时必须覆盖；`kind: service` 取目标 service
  的错误集；`logic` 的 `rules[].rule` 对应规则的 `on_violation` 错误必须出现在 service 错误集。
- `rules` 是对象列表：每项 `{ rule: feature.rules.* }`，rule 值是结构 ID，不加引号；按引用检查存在性，不允许字符串简写。
- `lifecycles` 只是标记：它表示本步包含这些状态流转，不限制本步的 kind，也不要求本步自己写状态列；目标状态以 lifecycle 为唯一事实源。
- 数据链是硬约束：`input` 的值只能是 `params.<path>` 或 `step.<id>.<path>`，且值的静态类型必须与目标参数类型兼容
  （指针/`List<>` 结构一致，类型族归一后比较，枚举按 integer）；可空的源不得赋给非 null 目标（query/service/events input 的源若 nullable，目标参数/payload 字段必须 nullable）；`step.<id>` 还必须通过 goto/when 边从被引用步骤可达，
  不能引用不可达分支；query/service 的 output 必须与目标 returns 对齐，每个显式 `output`（`events` / `when` 为空）必须被后续 `input` 引用或被 `returns.value` 引用；`returns` 的每个业务字段必须写 `value`（`params.<path>` 或 `step.<id>.<path>`），类型一致且 nullable 严格一致
  （`*T` 与嵌套字段的 nullable 都参与；`err` 不写 `value`）；service 的每个非 `ctx` 参数必须被至少一个 `input` 引用。
- 分支汇聚是硬约束：step input 引用的前序 step 必须在所有到达该 step 的路径上都已执行；若无法保证，同一 step 引用的未保证生产者必须两两互斥且构成完备切面（每条路径恰好经过一个）；`returns.value` 的来源必须能到达 service 终点。
- `params[].type` 只允许基础类型、`data.enums.*`、`data.entities.*`、全限定本地 schema（`cap.services.<file>.schemas.<Type>`）或 `_types` 类型族；指针与 `List<>` 各最多一层。
- 控制流由 `when` / `goto` 显式声明；`goto` 只允许前向，循环仍不支持；错误流转由实现决定；契约不声明 `on_error` / `max_iterations`。

### 3.2 事务与写归属

- `tx_mode` 是 service 级事务模式：`required` 写事务、`read_only` 只读事务、`none` 无事务；生成器据此包裹方法体。
- `tx_mode≠none` 的 service 不得调用 `tx_mode=required` 的 service（避免嵌套事务）；跨事务调用目标必须是
  `none`（或由调用方持有事务时的显式约定）。
- `read_only` 不得直接调用写表或返回 `affected_rows` 的 query，也不得经 `kind: service` 间接调用会写表的 service
  （写能力沿调用图传递）；写操作必须声明 `required` 或 `none`。
- `kind: query` 的写表必须属于文件 `owner_module`；跨模块写必须走 `kind: service`。
- 写表由 query 的 `tables[].write` 反向推导，不再单独声明；`query` 的 `errors` 必须被 `step.errors` 覆盖。
- `query.used_by` 由 `make gen-graph` 写回，只允许 `cap.services.*`，且不得为空。

---

## 4. lifecycles 与 features 规则

`rules` 仍属于功能规格，放在 `contracts/features/rules/`；`lifecycles` 属于能力层，放在
`contracts/capabilities/lifecycles/`：

- 一个 lifecycle = 一个状态机：`subjects` 列出共享该状态机的全部 `{entity, field}`，必须同枚举、同 `owner_module`；同一 `(entity, field)` 只能被一份契约声明；
- `states` 是对象：`values` 是状态集合，`initial` 是初始状态，`final` 是终态集合；每个 state 必须从 initial 可达；
- `final` 是列表：单终态（如 account 的 `[deleted]`）与多终态（如 token 的 `[revoked, expired]`）用同一形状表达；`destroy` 存在时 `final` 最多一个；
- `destroy` 可从任意非 final 状态销毁，不写 `from` / `to`；`final` 恰好一个时 destroy 进入该终态，`final` 为空时只软删实体，多终态 lifecycle 不使用 destroy；
- 幂等转换：把当前状态也写进 `from`（自环），不要单独维护幂等列表；
- `create` / `destroy` 写 `services`（由 `make gen-graph` 从 step.lifecycles 反推）与 `events`（必须由对应 service 的 step 发出）；`transitions[].implemented_by` 仍由 gen-graph 写回；
- `transitions[].requires` 声明跨维度前置状态：`{lifecycle, states: [...]}`，处于任一状态即可，列表内 AND；requires 图不得有环；实现该转换的 service 必须自行校验；
- service 的 `kind: logic` / `kind: when` step 用 `rules: [{ rule: feature.rules.* }]` 引用业务规则；
- 任意 step 用 `lifecycles: [cap.lifecycles.*.*]` 回引生命周期动作（create/destroy/transition）；`lifecycles` 不限制 step 的 kind，一个 step 可列多个动作；
- 每个生命周期动作的 step 不再声明 `state.write`；`lifecycles` 只是标记，不限制 step kind，也不要求本步写状态列；
- 事件只由 service step 的 `events` 声明（`kind: events`）；lifecycle 的 `create.events` / `destroy.events` 是对应动作的事件投影，必须能回指到 service step；
- `feature.rules` 的 `implemented_by` 仍指向 service 项。

capabilities 不承载 `rules/` 目录；features 不承载 `states/` 目录。

---

## 5. events（异步层）

`events/<module>.yaml` 同时承载两类异步定义，二者共享 `cap.events` ID 空间：

- `topics`：发布/订阅主题（事件），由 service step 的 `events` 发布，由 job 的 `trigger.topic` 订阅；
- `jobs`：异步任务，引用一个同步 service 作为 handler，支持 `cron` / `topic` 触发。

文件级：`id` / `summary` / `owner_module` / `schema_version` / `stability` / `do_not` / `delivery`（仅 topics 非空时必填）/
`topics` / `jobs`；`topics` 与 `jobs` 至少一个非空。

topic 项：

- `id` / `name` / `version` / `payload` / `summary` / `emitted_by` / `consumers`；
- `payload` 是受限 schema 值域节点（object 的 properties 用 property 条目列表）；
- `emitted_by` 指向发射它的 service 项，`consumers` 指向订阅它的 job 项，二者都由 `make gen-graph` 维护；
- 发布由 service step 的 `events` 声明，不是 service 项。

job 项：

- `id` / `name` / `summary` / `service` / `trigger` / `retry` / `concurrency`；
- `service` 必须与文件 `owner_module` 同模块；被 job 引用的 service 不得再被 `cap.protocols.http` 绑定；
- `trigger.type`：`cron`（必须 `schedule`）/ `topic`（必须存在的 `cap.events.*` 主题 ID）；
- `retry`：`max_attempts` / `backoff`（`fixed` / `exponential`）/ `dead_letter`；
- `concurrency`：`key` / `ttl`。

> 本层是项目自持的异步机制，不直接对接 protocols；触发只有 `cron` / `topic` 两种。

---

## 6. protocols

`protocols/<protocol>/<module>.yaml` 只写协议差异：

- path / method / operationId / `x-contract-id` / `x-service`
- status code / content-type / header
- request/response 的 schema `$ref`；request 用 `x-service-type`、response 用 `x-service-return` 以全限定 ID 指向
  service 的 params/returns
- `components.schemas` 通过 `x-service-type` / `x-enum` / `x-entity` 以全限定 ID 指向唯一来源

**不要写**：权限、审计、限流（在 `platform/security/access.yaml`）、事务（在 service 项）。

HTTP 约束：

- 响应 schema 由 `cap.services.schemas` 生成，object 默认封闭。
- 错误响应统一引用 `protocols/http/shared.yaml` 的 `Problem`。
- 默认错误响应由 `default_error_responses` 注入，不要逐接口重复声明；operation 需要更细粒度时用 `x-default-errors` 覆盖。
- 503 必须通过 shared 的 `ServiceUnavailable` 返回 `Retry-After`。
- 每个 operation 必须写 `x-contract-id` 与 `x-service`；`x-service` 只能指向 `public` 且未被 job 引用的同步 service。

---

## 7. 目录与 ID

```
contracts/capabilities/
├── README.md
├── services/<module>.yaml
├── events/<module>.yaml
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
| `lifecycles/`     | `cap.lifecycles.<file>`     | `cap.lifecycles.account`      |
| `events/`         | `cap.events.<file>`         | `cap.events.account`         |
| `protocols/http/` | `cap.protocols.http.<file>` | `cap.protocols.http.account` |

方法/步骤项 ID：

- service 项：`cap.services.<file>.<snake_case(name)>`
- job 项：`cap.events.<file>.<snake_case(name)>`
- rule 项：显式 `id`
- lifecycle 动作：`cap.lifecycles.<file>.<create|destroy|name>`
- topic 项：显式 `id`（`cap.events.<topic name>`）
- step：步骤内 `id`，不是全局契约 ID

---

## 8. 当前落地状态

- `services/*.yaml` 已合并原 methods + flows；每个 service 项都有 steps。
- `events/*.yaml` 同时承载 topics 与 jobs；`services` 不再声明 `trigger/retry/concurrency`。
- `capabilities/lifecycles/*` 承载生命周期；`features/rules/*` 承载业务规则；`capabilities/events/*` 承载异步主题与任务。
- `protocols/http/*.yaml` 已全部使用 `x-service` 指向同步 service，response 用 `x-service-return` 绑定 returns。
- `data.queries.*.used_by` 由 `make gen-graph` 写回，只允许 `cap.services.*`，不得为空。
- 术语表在 `docs/reference/glossary.md`（review-only，不占契约组）。
