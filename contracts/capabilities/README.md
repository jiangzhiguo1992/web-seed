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
| `lifecycles/<subject>.yaml`          | 实体如何创建、流转与销毁           | subjects[]、initial/final、create/destroy、transitions | 生命周期常量、转换表、非法动作测试     |
| `events/<module>.yaml`               | 哪些异步主题与任务、如何触发与重试 | topics[]（payload/发布/订阅）、jobs[]（trigger/retry） | 事件常量、payload 类型、worker 注册表  |
| `protocols/<protocol>/<module>.yaml` | 服务如何通过某种协议暴露           | path、method、status、header、schema 引用            | server 类型、client、合并 spec         |

一句话区分：

- **service = 一个可调用单元**，有明确的 params/returns（末位显式 `err`）和 steps；同步调用，不声明 trigger。
- **lifecycle = 一个实体的创建、状态转换与销毁**，每个动作（create/destroy/transition）锚到具体 service step。
- **event = 异步事实**，由 service step 的 `events` 发布到 topic，由 job 的 `trigger.topic` 订阅。
- **job = 异步任务**，引用一个同步 service 作为 handler，声明 `trigger`（cron/topic）/retry/concurrency。
- **step = 服务实现路径上的一步**，六类：query / service / logic / transition / tx_begin / tx_commit；其中 `transition` 执行生命周期动作，`tx_*` 是事务边界，二者不是一回事。
- **rule 在 features**，被 service 的 `kind: logic` / `kind: transition` step 引用；**lifecycle 在 capabilities**。
- **protocol = service 到传输协议的绑定**，只写协议差异。

---

## 2. 依赖方向

```
protocols ──x-service──> services ──steps──> data.queries
     │                     │
     │                     ├──kind=service──> services（跨模块）
     │                     ├──rules─────────> features.rules
     │                     ├──lifecycle────> lifecycles
     │                     └──event─────────> events
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
| service → rule       | `services/*.yaml` 的 steps         | `kind: logic/transition` + `rules`     |
| service → lifecycle  | `services/*.yaml` 的 steps         | `lifecycle`（create/destroy/transition） |
| lifecycle → service  | `lifecycles/*.yaml` 的 actions     | `step` / `implemented_by`（生成）      |
| service → event      | `services/*.yaml` 的 steps         | `kind: logic/transition` + `events`    |
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

- `schemas`（可选；本地 schema，值是值域节点，`object.properties[]` 的条目按 `_meta/_schemas.yaml` 的 `property.keys`
  校验，标量约束引用 `_types`，可跨文件引用）

入口权限、审计与限流在 `contracts/platform/security/access.yaml` 绑定，不在 service 项声明。query 的 `errors` 在
`data/queries/*.yaml` 的查询项上声明；它只列 query 自身的领域错误，输入校验、权限与 DB 引擎错误不在 query 内枚举，
`errors: []` 表示没有领域错误。

service 项必须声明：

- `name` / `summary` / `visibility`（`public` 跨模块可见，`internal` 仅本模块可见）
- `params` / `returns`；最后一项必须是 `{name: err, type: error}`，不能省略；无业务返回值也必须写 `err`；`params[].default` 只在
  `nullable: true` 时允许，调用方省略时取该值
- `transaction`（`required` / `read_only` / `none`）
- `steps`（至少一步）

### 3.1 steps

| kind         | 必填                                                                    | 说明                                                                                                                                                              |
|--------------|-------------------------------------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `query`      | `queries`                                                               | 调用本模块 `data.queries.*`；`queries[].input` 的 key 必须是查询参数名，必填参数必须全部出现，不写 `ctx`（生成器注入）；`step.errors` 缺省取所有 query 错误的并集 |
| `service`    | `services`                                                              | 调用其他（或本模块）service；`services[].input[].name` 必须是目标参数名，必填参数必须全部出现；跨模块目标必须 `public`                                            |
| `logic`      | `output` + `errors`；可选 `input` / `queries` / `rules` / `events`      | 手写实现；`queries` 是 `{query, input}` 对象列表；`input` 声明本步读取的入参与用途                                                                                |
| `transition` | `lifecycle`（只能引用 destroy/transition）；可选 `input` / `output` / `queries` / `rules` / `events` | 执行生命周期动作；create 只能由 `logic`/`query` step 实现                                                                                                          |
| `tx_begin`   | —                                                                       | 事务开始；只允许 `id` / `kind` / `summary`，`transaction≠none` 时必须是首步                                                                                       |
| `tx_commit`  | —                                                                       | 事务提交；只允许 `id` / `kind` / `summary`，`transaction≠none` 时必须是末步，关闭不得遗漏                                                                          |

字段按 kind 取交集：`id` / `kind` / `summary` 恒有；`lifecycle` 是公共反向引用；`input` 仅 `logic` / `transition`；`output` 仅 `query` / `logic` /
`transition`；`errors` 仅 `query` / `service` / `logic` / `transition`；`depends_on` 除 tx 步骤外可选（按模板顺序书写）。

- `input` 是 `{name, value, description}` 列表；`value` 只能是 `params.<path>` 或 `step.<id>.<path>`，禁止内联表达式；
  `description` 说明这项输入的业务含义，分支条件也写在这里。
- 计算值必须先落成前序 `kind: logic` step 的 `output`，再以 `step.<id>.<path>` 引用；`expr:` 软区已移除。
- `depends_on` 是可选顺序提示；`input` / `queries[].input` 引用 `step.<id>` 只要求该步骤在前序，不要求写入 `depends_on`。
- `queries` 是 `{query, input}` 列表；`kind: query` 也使用它，一步可以声明多个 query，由实现决定实际执行哪个。
- `services` 是 `{service, input}` 列表，`input` 是 `{name, value, description}` 列表；一步可以调用多个 service。
- 入口策略错误（权限→`COMMON_FORBIDDEN`、限流→`COMMON_RATE_LIMITED`、审计→`COMMON_AUDIT_FAILED`）只由 `access.yaml`
  拥有，service 与 step 不重复声明。
- `step.errors` 只列该步骤实际返回的领域错误：`kind: query` 缺省取所有 queries 的并集；`kind: service` 必须是目标 service
  错误并集的子集；`logic/transition` 必须覆盖其 `queries` 与 `rules.on_violation` 的 `errors`。
- 数据链是硬约束：`input` 的值只能是 `params.<path>` 或 `step.<id>.<path>`，且值的静态类型必须与目标参数类型兼容
  （指针/`List<>` 结构一致，类型族归一后比较，枚举按 integer）；`logic/transition` 的每个显式
  `output` 必须被后续 `input`/`queries[].input` 引用，或与 `returns` 业务字段同名；`returns` 的每个业务字段必须有同名、类型一致且
  `nullable` 一致的产出（`*T` 视为可空；`err` 不参与）；service 的每个非 `ctx` 参数必须被至少一个 `input` 引用。
- `params[].type` 只允许基础类型、`data.enums.*`、`data.entities.*`、本地 schema 或 `_types` 类型族；指针与 `List<>` 各最多一层。
- 分支、循环与错误流转由实现决定；契约不声明 `when` / `on_error` / `max_iterations`。

### 3.2 事务与写归属

- `transaction` 是 service 级事务边界，并且必须用显式步骤声明：`required` / `read_only` 时首步是 `tx_begin`、末步是
  `tx_commit`，各且只有一个；`none` 不得出现两者。生成器据此包裹方法体，提交步骤不得遗漏。
- `transaction≠none` 的 service 不得调用 `transaction=required` 的 service（避免嵌套事务）；跨事务调用目标必须是
  `none`（或由调用方持有事务时的显式约定）。
- `read_only` 不得直接调用写表或返回 `affected_rows` 的 query，也不得经 `kind: service` 间接调用会写表的 service
  （写能力沿调用图传递）；写操作必须声明 `required` 或 `none`。
- `kind: query` 的写表必须属于文件 `owner_module`；跨模块写必须走 `kind: service`。
- 写表由 query 的 `tables[].write` 反向推导，不再单独声明；`queries[].query` 的 `errors` 必须被 `step.errors` 覆盖。
- `query.used_by` 由 `make gen-graph` 写回，只允许 `cap.services.*`，且不得为空。

---

## 4. lifecycles 与 features 规则

`rules` 仍属于功能规格，放在 `contracts/features/rules/`；`lifecycles` 属于能力层，放在
`contracts/capabilities/lifecycles/`：

- service 的 `kind: logic` step 用 `rules: [feature.rules.*]` 引用业务规则；
- 任意 step 用 `lifecycle: [cap.lifecycles.*.*]` 回引生命周期动作（create/destroy/transition）；
- `cap.lifecycles` 的 `create.step` / `destroy.step` / `transitions[].implemented_by` 由 `make gen-graph` 写回；
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
- request/response 的 schema `$ref`；request 用 `x-service-type`、response 用 `x-service-return` 指向 service 的
  params/returns
- `components.schemas` 通过 `x-service-type` / `x-enum` / `x-entity` 指向唯一来源

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
