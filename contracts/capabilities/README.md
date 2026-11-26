# 能力契约（capabilities）

> 本组回答四件事：模块能提供哪些**可调用单元**（services）、实体状态如何随时间变化（lifecycles）、
> 哪些**异步事实与任务**存在（events）、这些能力如何通过协议暴露（protocols）。
> 业务规则在 `features/flows`，持久化形状在 `data`，入口策略在 `platform/security/access.yaml`。

**本文件只写**组级心智模型、放错层的判据、以及无法机器校验的组级约束。

字段形状归 `contracts/_meta/cap.*.yaml`，全局元规则归 `contracts/README.md`，操作步骤归 `docs/playbooks/contract-change.md`，实例清单与 enforcement 归 `contracts/index.yaml`，进度归 `docs/guides/architecture.md` §0。

**标记约定**：`[强制: <rule-id>]` 表示由 `contract-check` 或生成器强制；`[评审]` 表示无机器校验、靠 review。

**准入测试**：新增一个实例文件时，这段内容需要改吗？会变（文件清单、实例状态、某类型的字段）→ 不写进本文件；只有新增契约类型或调整分层才变 → 属于本文件。

---

## 1. 定位与边界

业务契约的依赖只有一条单向链（`platform` 是横切约束，只被引用，不反向依赖）：

```
features ──> capabilities ──> data
```

- **对上游**：`features` 通过 service ID 使用能力（`capability`），通过 `available_when` 引用状态机动作；页面不描述实现。
- **对下游**：service 的 `kind: query` step 落到 `data.queries`；写表由 query 反推，service 不单独声明。
- **不属于本组**：业务规则 → `features/flows`；表 / 索引 / 查询形状 → `data`；入口权限 / 审计 / 限流 → `platform/security/access.yaml`；HTTP 信封 / 状态码 / header → `platform/http_conventions.yaml`。

---

## 2. 目录与单元

| 目录 | 回答的问题 | 实例粒度 | 不是 |
|---|---|---|---|
| `services/<module>.yaml` | 本模块能提供哪些可调用单元、如何编排 | 一文件 = 一个 Go interface（`<Owner><File>Service`，owner 与 file 同名时折叠） | 不是 controller，也不是 endpoint 清单 |
| `lifecycles/<dimension>.yaml` | 一个状态机如何创建、流转、销毁 | 一文件 = 一个状态机，可覆盖多个 `{entity, field}` | 不是 UI 状态 |
| `events/<module>.yaml` | 本模块有哪些异步事实、周期任务与内部命令 | 一文件 = 一个模块的异步面 | 不是同步 service |
| `protocols/<protocol>/<module>.yaml` | 同一能力如何通过某种协议暴露 | 一文件 = 一个模块的一种协议 | 不是业务契约 |

文件级 ID：`cap.services.<file>` / `cap.lifecycles.<file>` / `cap.events.<file>` / `cap.protocols.http.<file>`。
项 ID：service 与异步任务按 `name` 派生，lifecycle 动作按 `create` / `destroy` / `transitions.<name>` 派生，topic 显式写 `id`。

**实例清单以 `contracts/index.yaml` 为准**：本文件不列举实例文件，也不因新增 service / 状态机而修改。

---

## 3. 关系

### 3.1 组内方向

```
features.pages ──capability──> services
features.pages ──available_when──> lifecycles
protocols ──service──> services
services ──kind: query──> data.queries
services ──kind: service──> services（跨模块必须 public）
services ──kind: logic|when──> features.rules
services ──lifecycles / state_gate──> lifecycles
services ──kind: events──> events（topic / dispatch）
lifecycles ──implemented_by（生成）──> services
events ──service──> services（subscribe / cron / dispatch）
```

| 边 | 写在哪 | 字段 |
|---|---|---|
| protocol → service | `protocols/**` 的 operation | `service` |
| service → query | `services/**` 的 step | `kind: query` + `query` |
| service → service | `services/**` 的 step | `kind: service` + `service` |
| service → rule | `services/**` 的 step | `kind: logic/when` + `rules[].rule` |
| service → lifecycle | `services/**` 的 step | `lifecycles[]`（任意 kind）+ `state_gate[]` |
| service → event | `services/**` 的 step | `kind: events` + `events[].ref` |
| lifecycle → service | `lifecycles/**` 的 create/destroy/transitions | `implemented_by`（生成） |
| 异步任务 → service | `events/**` 的 subscribe/crons/dispatches | `service` |
| 异步任务 → topic | `events/**` 的 topic 嵌套 | `subscribe` 所在 topic |
| feature → service | `features/**` | `capability` |
| feature → lifecycle | `features/**` 的页面动作 | `available_when.transition(s)` |

### 3.2 禁止边

- protocol 直接引用 `data.queries` / `feature.flows`：对外入口只能是 service。
- `kind: query` 引用其他模块的 query；跨模块必须走 `kind: service`。`[评审]`
- service 调用图成环。`[强制: service-cycle]`
- 异步 handler 再被 HTTP 绑定。`[强制: async-service]`
- lifecycle 动作锚不到 service step，或 step 不回引该动作。`[强制: lifecycle-anchors]`
- 在 capabilities 声明业务规则或持久化形状；两层已分别归 `features/flows` 与 `data`。

### 3.3 跨组引用

| 能力层需要 | 来自 | 绑定方式 |
|---|---|---|
| 入口权限 / 审计 / 限流 | `platform/security/access.yaml` | `bindings[].target: cap.services.*`；service 不重复声明 |
| HTTP 信封、状态码、平台 header 与组件 | `platform/http_conventions.yaml` | 生成器注入；protocol 文件不写私有约定 |
| 错误码 | `platform/errors/*.yaml` | service step `errors`；protocol 只投影，不维护文案 |
| 实体字段校验 | `data/entities/*.yaml` | step `validations[]`；`used_by` 由 `gen-graph` 反推 |
| 应用包含哪些 HTTP 文件 | `contracts/project.yaml` 的 `apps` | `includes: [cap.protocols.http.*]` |
| 验收场景 | `contracts/features/scenarios/http/*.yaml` | 按 HTTP 文件分组；绑定 features flow/pages |

---

## 4. 类型级判据

### 4.1 services：先决定「要不要成为一步」

- 一个 service 只有一个实现入口，控制流由 `steps` + `goto` 显式表达；没有隐式顺序。`[强制: service-goto]`
- step 五类怎么选：

| 想做的事 | `kind` | 边界 |
|---|---|---|
| 读写数据 | `query` | 不要在任何 logic 里内联查询 |
| 复用另一个可调用单元 | `service` | 不要内联调用；跨模块目标必须 `public` |
| 判断、计算、组装 | `logic` | 不直接调 query/service，也不发事件 |
| 发事件 / 投递命令 | `events` | 不夹带其他副作用；`output` 必须为空 |
| 条件分支 | `when` | 不用 logic 模拟跳转 |

- 数据链是硬约束：`input` 只能引用 `params.*` 或前序 `step.*`；显式 `output` 必须被后续消费；分支汇聚要求前序生产者在所有路径上都已执行，或构成完备互斥切面。`[强制: service-input-format]` `[强制: service-input-order]` `[强制: service-chain]` `[强制: service-merge]`
- 事务与写归属：`read_only` 不得直接或间接写表；`kind: query` 只能写本文件 `owner_module` 的表。`[强制: service-transaction]` `[强制: service-owner-write]`
- 能力必须有消费者：`service.used_by` 不得为空。`[强制: service-used-by]`

### 4.2 lifecycles：一个状态机，不是一组零散状态

- 状态机属于能力层（`cap.lifecycles`），业务规则属于 `features/flows`；features 下不再建状态机。
- 一文件 = 一个状态机；`subjects[]` 可列出共享同一状态机的多个 `{entity, field}`，必须同枚举、同 `owner_module`；同一 `(entity, field)` 只能被一份契约声明。`[强制: lifecycle-subject]` `[强制: lifecycle-dimension]`
- 所有状态必须从 `initial` 可达；`final` 必填（没有终态写 `[]`）；`destroy` 可从任意非终态销毁。`[强制: lifecycle-structure]` `[强制: lifecycle-exhaustive]`
- 幂等转换用 `from` 自环表达，不单独维护幂等列表。
- 跨维度前置写 `requires`（状态集合 + 解释规则 + `on_violation`），列表内 AND，requires 图不得有环；实现该动作的 service 必须自行校验。`[强制: lifecycle-requires]`
- 声明了动作就必须有实现：`create/destroy/transitions[].implemented_by` 不得为空，由 `step.lifecycles` 反推写入。`[强制: lifecycle-anchors]`
- 状态可用性由 `state_gate[]` 声明：省略表示全部状态允许；`states.values[].steps` 由它反推，禁止手改。`[强制: service-state-gate]` `[强制: lifecycle-anchors]`

### 4.3 events：三种触发源，不要混用

| 触发源 | 语义 | 消费者 | 选择判据 |
|---|---|---|---|
| `topics` | 已发生的事实 | `subscribe[]`（可无，可多个） | 有多订阅者，或消费者需要独立演进 |
| `dispatches` | 内部命令 | 恰好一个同步 service | 需要延迟 / 重试 / 削峰，且只有一个消费者 |
| `crons` | 周期任务 | 一个同步 service | 无外部触发，按时间运行 |

- 发布由 service step 的 `kind: events` 声明；`publish.emitted_by` / `dispatch.emitted_by` 由 `gen-graph` 反推，至少一个。`[强制: event-structure]`
- `dispatch.payload` 必须与消费者 service 的非 ctx 参数同名、required/nullable/类型一致，不多不少。`[强制: event-structure]`
- 异步 handler 必须与 events 文件同模块，且不得再被 HTTP 绑定。`[强制: async-service]`
- 本层是项目自持的异步机制，不直接对接 protocols。

### 4.4 protocols：只写协议差异

- 每个 operation 必须绑定 `service`，且目标只能是 `public`、未被异步任务引用的同步 service。`[强制: http-operation]` `[强制: service-visibility]`
- 文件级 `mount` 承载 `api_version` / 公共资源前缀 / audience；版本只由 `mount.api_version` 声明，`paths` 不写版本前缀。`[强制: http-mount]` `[强制: http-version]`
- 只写协议差异：请求 / 响应形状与 header；权限、审计、限流、事务不在这里；信封、平台 header、基线状态由 `platform/http_conventions.yaml` 定义、生成器注入。
- 错误只写 `platform.errors.*` 引用，按状态分组并由 checker 与 service 错误闭包对账。`[强制: http-errors]`
- 文件必须被 `project.yaml` 某个 api app 的 `includes` 收录，否则不生成 OpenAPI。`[强制: http-app-included]`

---

## 5. 相关

- 字段语义与规则：`contracts/_meta/cap.*.yaml`；字段规范与命令入口：`contracts/_meta/README.md`；全局元规则与流程：`contracts/README.md`、`docs/playbooks/contract-change.md`
- 相邻组：`contracts/data/README.md`、`contracts/features/README.md`、`contracts/platform/README.md`
- 实例与 enforcement：`contracts/index.yaml`；术语：`docs/reference/glossary.md`
