# 契约层（contracts）

> **本目录是本仓库的唯一真相源（Single Source of Truth）。**
> 所有实现——Go 代码、Vue 代码、SQL 迁移、配置、文档、测试——都是本目录的**投影**。
> 契约表达力不足时，**扩展契约**，而不是把真相下沉到实现。

---

## 0. 本文件的范围

本文件只写**契约的元规则**：怎么定义、怎么引用、怎么校验、怎么演进、什么不能做。
它不是教程，也不是操作手册。

| 内容 | 写在哪 |
|---|---|
| 契约元规则、ID、分层、拆分、盲区 | **本文件** |
| 步骤与命令序列 | `docs/playbooks/contract-change.md` |
| 为什么这样设计、否决了什么 | `docs/adr/` |
| AI 跨任务硬约束 | 根 `AGENTS.md` |
| 某条契约的字段与取值 | 各契约文件自身 |
**判断新内容该不该写进本文件**：它是"所有契约都要遵守的规则"吗？是 → 写；只对某一类契约适用 → 写到该组 README 或文件头。

---

## 1. 核心原则

### 1.1 契约大于一切

| 推论 | 具体含义 |
|---|---|
| 实现由契约生成 | 类型、常量、路由、生命周期、DDL 均从本目录产出 |
| 生成物不可手改 | `**/generated/` 与 `apps/api/migrations/` 是产物；手改会被 CI 拦截 |
| 需求变更先改契约 | 禁止"先写代码后补契约" |
| 契约不丢信息 | SQL 特性用结构化字段表达（`storage.tables.<engine>[].partition`、归档条目、`indexes[].expression`），禁止塞回原生 DDL |

### 1.2 契约的判定标准

只有同时满足三条才是契约：

1. 跨实现被消费（前后端、应用与模块、代码与运维）
2. 可被机器校验或生成
3. 描述"必须成立的事实"，不描述"如何实现"

不满足的放 `docs/`，不要放本目录。

### 1.3 enforcement 三档

| 档位 | 含义 | 缺失后果 |
|---|---|---|
| `generated` | 从契约生成代码，手改即漂移 | CI diff 失败 |
| `tested` | 有测试断言契约 | 测试失败 |
| `review-only` | 只能靠 review，必须显式标注 | 风险可见，不静默 |

**没有 `enforcement` 声明的文件不算契约。**

### 1.4 契约能保证什么、不能保证什么

| 契约 + 闸门能保证 | 契约无法保证 | 靠什么补 |
|---|---|---|
| 接口形状不漂移 | 字段语义是否被正确理解 | `docs/reference/glossary.md` + review |
| 状态转换穷举 | 业务规则是否被正确实现 | `features/rules` 的可执行用例 |
| 表结构与迁移一致 | 事务边界与并发是否正确 | 集成测试 + race |
| 前后端类型一致 | 性能是否退化 | 基准测试 |
| 枚举、权限、错误码一致 | 契约本身是否写错 | review + 真实用例回归 |
| 引用关系不越层 | 时序与重试的真实行为 | 并发与重试测试 |

**结论**：契约决定"能编译什么"，闸门决定"能合入什么"，流程决定"AI 会先做什么"。三者缺一，一致性只是口号。不要指望契约替代测试；契约没覆盖时，先问能否扩展表达力。

---

## 2. 目录结构与五组契约

```
contracts/
├── README.md                 # 本文件：规范正文
├── index.yaml                # 契约地图：所有条目的 ID / 路径 / 消费者 / 强度
├── _meta/                    # 契约元规范（非业务契约，不登记 index）
│   ├── README.md             #   AI 入口：写前读什么、写后跑什么
│   ├── _types.yaml           #   共享值类型：类型族 + 自定义类型
│   ├── data.tables.yaml      #   物理表字段树 + 值域 + 规则（单一源）
│   ├── data.entities.yaml    #   实体字段树 + 值域 + 规则
│   ├── data.queries.yaml     #   查询字段树 + 值域 + 规则
│   └── data.enums.yaml       #   枚举字段树 + 值域 + 规则
├── project.yaml              # 项目元信息 + 应用组合（derive 的唯一输入）
│
├── data/                     # ① 数据契约（模型、边界与取舍见 data/README.md）
│   ├── tables/<file>.yaml    #   物理表（_base.yaml 为基类，不是物理表）
│   ├── entities/<file>.yaml  #   业务读模型（_base.yaml 为基类）
│   ├── queries/<scope>.yaml  #   读写形状：入参/返回/索引引用，不含 SQL
│   ├── enums/<module>.yaml   #   跨端枚举：按 owner_module 拆分，integer code ↔ 名称
│   └── storage.yaml          #   单例：引擎归属 / 条目化的保留 / 分区 / 归档 / 多 key 缓存
│
├── capabilities/             # ② 能力层：services + lifecycles + events（topics/jobs）+ protocols
│   ├── services/<module>.yaml #   服务：services + schemas（interface 名由 owner_module + file 派生）
│   ├── lifecycles/<dimension>.yaml # 状态机生命周期：subjects(entity+field)、states、create/destroy/transitions，锚到 service step
│   ├── events/<module>.yaml   #   异步主题与任务：topics + jobs
│   └── protocols/
│       └── http/<module>.yaml #   HTTP 绑定：只写协议差异，x-service 指回 service
│
├── features/                 # ③ 功能契约（面向用户）
│   ├── routes.yaml           #   路由、菜单、布局
│   ├── pages/<page>.yaml     #   页面：数据源、四态、动作、字段
│   └── rules/<subject>.yaml  #   业务规则（constraint / invariant / policy）
│
├── platform/                 # ④ 平台契约（工程约束）
│   ├── environments.yaml     #   环境清单与差异
│   ├── versioning.yaml       #   版本与兼容策略
│   ├── errors/               #   错误码按域拆分（_base / common / account / auth ...）
│   ├── http_conventions.yaml #   信封、分页、header、状态码
│   ├── quality.yaml          #   测试策略与门槛
│   ├── config/env.json  architecture/{go_arch.yaml,web_deps.json}
│   ├── design/tokens.json      observability/{log_fields,metrics}.yaml
│   └── analytics/events.yaml   security/{permissions,roles,auth_policy}.yaml
│
└── generated/                # 机器产物（见 4.7；禁止手改）
    └── openapi/admin.yaml    #   合并 OpenAPI spec（代码生成输入）
```

人读视图（`lineage.md` / `data.md` / `errors.md` / `permissions.md`）在 `docs/contracts/generated/`。

### 2.1 组 README 索引

契约元规则在本文件；各组的类型规范在各组 README，按任务取用：

| README | 何时读 |
|---|---|
| `_meta/README.md` | 写 / 改任何契约字段前 |
| `data/README.md` | 加表 / 写查询前 |
| `capabilities/README.md` | 加 API / 生命周期 / 异步任务 / 跨模块 service 前 |
| `features/README.md` | 改规则前 |
| `features/README.md` | 加页面 / 用户旅程前 |
| `platform/README.md` | 改错误码 / 权限 / 配置 / 观测约定前 |

---

## 3. 锚点机制

### 3.1 ID 规范

```
<group>.<type>.<scope>.<name>[.<item>]
```

| 组 | 示例 |
|---|---|
| data | `data.tables.account`、`data.tables.account.col.number`、`data.tables.account.constraint.ck_account_deleted_consistent`、`data.enums.account_status`、`data.queries.account.get_account`、`data.storage`、`data.tables.base` |
| capabilities | `cap.services.account.get`、`cap.lifecycles.account`、`cap.events.token.purge_expired`、`feature.rules.account.number_format`、`cap.events.account.created`、`cap.protocols.http.account.create` |
| features | `feature.routes.account_list`、`feature.pages.account_list` |
| platform | `platform.errors.ACCOUNT_NOT_FOUND`、`platform.security.permissions.account_read`、`platform.security.roles.admin`、`platform.analytics.events.page.account_list_viewed` |

规则：

- 只用小写、数字、下划线、点分隔
- **ID 一旦发布不可改名**；改名 = 新 ID + 旧 ID 标 `deprecated`
- 跨文件引用一律写 ID，**禁止写文件路径**

**集合契约 vs 项契约**：一个文件可以承载一组同类契约。每个可寻址项必须有显式 `id`，或由文件 ID + 显式 `name` 确定性派生；禁止从文件内容猜名称。`data.queries` 的项采用后者：`name` 显式，项 ID = `<文件 id>.<snake_case(name)>`。

| 集合文件 | 文件 ID | 项 ID |
|---|---|---|
| `data/tables/<file>.yaml` | `data.tables.<file>` | `data.tables.<file>.col.<column>` / `data.tables.<file>.constraint.<name>` |
| `data/queries/<scope>.yaml` | `data.queries.<scope>` | `data.queries.<scope>.<snake_case(name)>`（name 显式） |
| `data/enums/<module>.yaml` | `data.enums.<module>` | `data.enums.<name>`（name 派生，不写 id；跨文件全局唯一） |
| `platform/security/permissions.yaml` | `platform.security.permissions` | `platform.security.permissions.<code>` |
| `platform/security/roles.yaml` | `platform.security.roles` | `platform.security.roles.<code>` |

`data.storage` 是单例契约，没有项 ID；`tables/_base.yaml` 的 ID 固定为 `data.tables.base`，由 checker 写死识别。

### 3.2 唯一顺序：单向链

```
feature ──> capabilities ──> data
              ├── services ──> data.queries
              ├── services ──> lifecycles / rules / events
              ├── lifecycles ──> services（step 锚点）
              └── events ──> services / topics
```

契约依赖只有这一条链，**没有反向**。capabilities 内部允许 `services → lifecycles/rules/events`（业务依据）；`lifecycles/rules` 允许引用 `data.entities.*` 作为业务主体，但禁止引用 `data.tables` / `data.queries` / `data.storage`。

| 环节 | 职责 | 锚点写在哪 |
|---|---|---|
| feature | 定义用户功能 | 锚 `capabilities`（service / lifecycle / rule / event） |
| capabilities | 实现 + 业务规格 | service 锚 `data`（steps → queries；写表由 query 反向推导）；service 锚 `lifecycles/rules/events` |
| data | 持久化 | 无下游 |

**所有跨层关系只在上游写一处**：

| 边 | 写在 | 字段 |
|---|---|---|
| feature → capabilities | 页面契约 | `capability:` / `feature.rules.*` / `cap.lifecycles.*` |
| capabilities/protocols → capabilities/services | protocol operation | `x-service` |
| capabilities/services → capabilities/services | service steps | `kind: service` + `service` |
| capabilities/services → features/rules | service steps | `kind: logic/transition` + `rules` |
| capabilities/services → capabilities/lifecycles | service steps | `lifecycle`（create/destroy/transition） |
| capabilities/lifecycles → capabilities/services | lifecycle action | `step` / `implemented_by`（由 step.lifecycle 生成） |
| capabilities/services → capabilities/events | service steps | `kind: logic/transition` + `events` |
| capabilities/events → capabilities/services | job | `service` |
| capabilities/events → capabilities/events | job | `trigger.topic` |
| capabilities/services → data | service 契约 | `steps[].queries[].query` / query `errors` |
| data.queries → data.tables | 查询项 | `tables` / `index_ref` / `caches` / `archive` |
| data.entities → data.tables | 实体 | `model.tables` / `source.column` / `type` + `source.join.from/to` |
| data.tables → 上游（反向） | 表文件 | `table.entities`（显式，checker 校验） / `table.queries`（生成物） |
| data.entities → 上游（反向） | 实体文件 | `model.included_by`（显式，checker 校验） / `model.queries`（生成物） |
| data.queries → 上游（反向） | 查询项 | `used_by`（生成物，指向 `cap.services.*`，不得为空） |

### 3.2.1 capabilities 的业务规格引用

capabilities 契约内部按业务主体引用 `rules` / `states` / `events`，不再有 `business.*`：

| 不要写 | 改写为 |
|---|---|
| `returns: data.tables.account.row` | `returns: data.entities.account` |
| `emits: cap.events.*` | 由 `cap.events.*.emitted_by` 声明；service 的 `kind: logic/transition` step 用 `events` 引用 |
| `x-preconditions: feature.rules.*` | 由 `feature.rules.*.implemented_by` 声明；service 的 `kind: logic/transition` step 引用规则 |

理由：service 是规则的实现，**实现不反向声明"我实现了谁"**——那由 `implemented_by` / `emitted_by` 锚点。两处都写必然漂移。

capabilities 内部允许 `services → lifecycles/rules/events`：`kind: logic/transition` 的 `rules`、step 的 `lifecycle`、`kind: logic/transition` 的 `events`。这是同组内的业务依据，不是跨层引用。

`lifecycles` / `rules` 允许引用 `data.entities.*` 作为业务主体（生命周期的 `subjects[].entity`、规则的实体），但禁止引用 `data.tables` / `data.queries` / `data.storage`。生命周期必须同时写 `subjects[].field`：一个契约描述一个状态机，`subjects[]` 可列出共享该状态机的多个实体状态字段（同枚举、同 `owner_module`）；同一 `(entity, field)` 只能被一份契约声明。`business.*` 已全部移除。

### 3.3 图校验（⬜ P2）

以下为 P2 目标；`make contract-graph` / `make contract-impact` 当前是占位目标（`exit 1`），尚未产生保护。

```bash
make contract-graph     # 校验 + 按需生成 graph / lineage
make contract-impact id=data.tables.account.col.number
```

| 检查 | 失败 |
|---|---|
| ID 重复 | 报错 |
| 悬空引用（dangling ref） | 报错 |
| 越层引用 | 报错 |
| 循环依赖（未标 `bidirectional: true`） | 报错 |
| 孤儿契约（无入边，白名单除外） | 警告 |
| 生成物与契约不一致 | CI 失败 |

血缘**自动推导，不手写**：

```
feature.pages.account_list
  ├─ cap.protocols.http.account.list ─x-service→ cap.services.account.list
  │    └─ kind=logic → data.queries.account.list_accounts
  └─ cap.protocols.http.account.create ─x-service→ cap.services.account.create
       ├─ kind=logic → data.queries.account.create_account
       └─ rules → feature.rules.account.*
            └─ implemented_by → cap.services.account.create

cap.lifecycles.account ─subjects(status)→ data.entities.account ─table→ data.tables.account
```

### 3.4 契约变更流程

改任何契约前必须回答四问：

1. 是否破坏兼容？是 → 升版本或走弃用流程
2. 是否需要新版本？URL major（`/v1` → `/v2`）
3. 是否需要数据迁移？先改 `data/tables`，再生成迁移
4. 影响面是什么？`make contract-impact id=<id>` 输出受影响的 API、页面、测试、生成物（⬜ P2 未实现，当前靠人工检索）

**变更必须原子**：一个 PR 内同时包含 契约 + 生成物 + 实现 + 测试。
禁止"先合契约、实现下个 PR 补"——那会让 CI 长期红，闸门失效。

评审分级：

| 变更类型 | 要求 |
|---|---|
| 改 ID、删字段、改枚举值、改错误码含义 | 额外 review + 影响面报告 |
| 新增可选字段 / 新端点 / 新契约文件 | 常规 review；新文件必须同步 `index.yaml` |
| 修改 `do_not`、`summary`、注释 | 无需额外 review |

### 3.5 演进规则

| 规则 | 说明 |
|---|---|
| ID 不可改名 | 改名 = 新 ID + 旧 ID 标 `deprecated` |
| `schema_version` | 每个契约文件带版本；格式升级写批量迁移脚本，禁止新旧格式长期共存 |
| 废弃窗口 | API 字段 ≥90 天；契约条目 ≥1 个发布版本 |
| 契约总量 | 业务契约 ≤60 个文件；超出说明拆太细或范围失控 |
| 只增不改 | `additive_only`：加字段 / 加端点合法，改语义不合法 |

---

## 4. 单文件规范

### 4.0 index.yaml 的 path 约定

`index.yaml` 中的 `path` **相对 `contracts/` 目录**，例如 `data/tables/account.yaml`。
生成器与校验器统一按此规则拼接，避免仓库绝对路径进入契约。

`_meta/` 是契约字段规范与脚本入口，不登记 `index.yaml`；`generated/` 只放产物，也不登记为业务契约。

### 4.1 文件结构（按职责分层）

| 层 | 位置 | 内容 |
|---|---|---|
| 类型规范 | `_meta/<type>.yaml` | 字段树 + 值域 + 可机器校验的类型级 `rules` |
| 类型指导 | `<group>/README.md` 或 `<type>/_base.yaml` 的 `do_not` | 不可机器化的通用反例 |
| 实例元数据 | 文件正文 | `id` / `summary` / `stability` / 实例级 `do_not` |
| 人类注释 | 文件第一行 | 一句 `# <用途>`，不承载机器信息 |

```yaml
# 账号主表。表结构真相源，migrations 由本文件生成。

id: data.tables.account
summary: 账号主表
stability: stable
do_not:                        # 只写实例级；通用约束见组 README 或 _base
  - number 唯一索引必须 live_only: true

table:
  name: account                  # 物理表名；必须显式声明，本仓库保持单数
  ...
```

- 契约文件名与物理表名解耦：文件名决定契约 ID；物理表名必须显式声明；本仓库统一单数
- `stability` 流转见 `contracts/_meta/README.md` §5.1：`stable` 只能用户手动设置/降级，生成器只产出 `draft`；`stable` 前必须替换 TODO 并跑 `make contract-check`
- 类型由文件级 ID 推导（`data.tables.account` → `data.tables`；`data.enums` / `data.storage` 按完整 ID 匹配 `_meta/<type>.yaml`；`data.tables.base` / `data.entities.base` 由 checker 固定识别），文件无需声明 `type`
- 生效元数据 = `_meta/<type>.yaml` + 继承的 `_base` 字段 + 文件字段覆盖
- **通用约束不要复制到每个文件**：复制即漂移
- **不写 `refs`**：依赖关系由图校验器从 `ref` / `capability` / `use` / `emits` / `column` / `target` 等结构化字段自动推导

### 4.2 拆分与行数

| 类型 | 拆分单位 | 上限 |
|---|---|---|
| `data/tables/*` | 一物理表一文件 | **不限** |
| `data/entities/*` | 一逻辑实体一文件 | 150 |
| `capabilities/services/*` | 一模块一文件（services + schemas；interface 名由 owner_module + file 派生） | 250 |
| `capabilities/events/*` | 一模块一文件 | 120 |
| `capabilities/events/*` | 一业务主体一文件 | 120 |
| `capabilities/protocols/http/*` | 一模块一文件；>15 接口按子域再拆 | 200 |
| `capabilities/lifecycles/*` | 一个状态机一文件 | 150 |
| `features/rules/*` | 一规则集一文件 | 120 |
| `features/pages/*` | 一页面一文件 | 150 |
| `platform/**` | 一关注点一文件 | 150 |
| `index.yaml` | 只放摘要 | 400 |
| 各组 `README.md` | 规范正文（含图示与对照表） | 450 |

### 4.3 防过度拆分

拆完每个文件 < 20 行，或必须同时读 5 个以上文件才能理解一个概念 → 合并。

### 4.4 示例必填

每个契约至少提供：

- **1 个正例**：合法输入 + 期望输出
- **1 个反例**：最容易被 AI 写错的用法

没有示例的契约视为不完整——AI 会自行编造"合理但错误"的用法。反例写 `do_not`（实例级）；通用反例写组 README 或 `_base`。

### 4.5 约束写在哪里

四层，按"适用范围从大到小"放：

| 层 | 位置 | 例子 | 维护 |
|---|---|---|---|
| **类型级（可执行）** | `_meta/<type>.yaml` 的 `rules` | "所有表必须写 table.queries" | 机器校验 |
| **类型级（指导）** | `<group>/README.md` 或 `<type>/_base.yaml` 的 `do_not` | "所有表不要手写 migrations" | 一处定义，全类继承 |
| **组级** | `<group>/README.md` | "features 不引用 data"、"实体在 data 层" | 手写，稳定 |
| **实例级** | 契约正文 `do_not` | "account.status 使用 integer；枚举只在 entity"、"游标分页要有 tiebreaker" | 就近，每个文件 1–3 条 |
| **全局** | `index.yaml` 的 `global_notes` | "错误响应由生成器注入" | 跨文件坑，极少 |

例外：`features/rules/*`、`platform/architecture/go_arch.yaml`、`platform/observability/log_fields.yaml` 的 `rules` 是领域规则集正文，不是类型规则清单；除此以外具体契约禁止顶层 `rules`；嵌套的数据属性可以叫 `rules`，不视为契约规则。

**不建目录级 index 汇总约束**，原因：

1. 与 `index.yaml` 功能重叠——后者已是全局索引
2. 手写必漂移，生成则多一层间接
3. 违反就近原则：约束离定义越远，越容易在改动时被遗忘
4. 组级规范已有 `README.md` 承载

判断标准：**改这个契约时会看到它的约束吗？** 会 → 写在该文件；不会 → 上移一层。

### 4.5.1 `do_not` 的去重规则

每条 `do_not` 必须包含**该契约的具体标识**（表名、字段名、枚举 name 或 go_type/ts_type、方法名、规则名、页面名）。

| 写法 | 判定 |
|---|---|
| `account.status 使用 integer；枚举只在 entity` | ✅ 含字段名，实例级 |
| `游标分页必须有唯一 tiebreaker（created_at, number）` | ✅ 含字段名，实例级 |
| `不要手写 migrations` | ❌ 所有表都适用 → 上移组 README 或 `_base` |
| `不要绕过软删过滤` | ❌ 所有软删表都适用 → 上移 |

**同一句话只能出现一次**：在组 README / `_base` 或某个契约文件里，不能两处都有。
`make contract-check` 的 `schema.do-not-duplicate` 会检查重复并报告。

### 4.5.2 `fields` 的排版

所有契约的顶层 `fields` 一律竖排（每个键一行）；嵌套 `item_fields` 不限制。`contract-check` 的 `schema.fields-style` 会拦住括号写法，避免 AI 在两种风格间来回切换。

### 4.6 人读视图自动生成

`docs/contracts/generated/` 下的视图由契约生成，**禁止手写**（`errors.md` ✅；其余 ⬜ P2）：

| 文件 | 来源 | 状态 |
|---|---|---|
| `errors.md` | `platform/errors/*.yaml` | ✅ `make gen-errors` |
| `permissions.md` | `platform/security/permissions.yaml` | ⬜ P2 |
| `data.md` | `data/tables/*.yaml` | ⬜ P2 |
| `lineage.md` | 按需生成的 graph | ⬜ P2 |

AI 读 YAML，人读 Markdown，单一源。手写视图必然与契约漂移。

### 4.7 机器产物（`contracts/generated/`）

`contracts/generated/` 只放机器生成物，禁止手改；人读视图在 `docs/contracts/generated/`。

| 产物 | 来源 | 用途 |
|---|---|---|
| `openapi/admin.yaml` | `capabilities/protocols/http/*.yaml` + `shared.yaml` 合并 | oapi-codegen / Swagger / `gen-client` 的输入 |

`db` 产物（`schema.sql`、`migrations/*.sql`）输出到 `apps/api/`，不在本目录。

不再入库的产物：

| 产物 | 去向 | 原因 |
|---|---|---|
| `graph.json` | 按需生成（临时目录 / CI） | 只有影响面分析需要时生成 |
| `lineage.md` | `docs/contracts/generated/lineage.md` | 人读文档统一进 `docs/` |
| `manifest.json` | 删除设计 | `index.yaml` + `project.yaml` 已覆盖，无消费者 |
| `inbound.yaml` | 删除设计 | `used_by.*` / `table.entities` / `model.included_by` 已是反向索引 |

`make gen` 是聚合入口；`make gen-graph` 只写回 `table.queries` / `model.queries` / `query.used_by` 与规则、生命周期、事件锚点，不产出本目录文件。

---

## 5. 契约层的 AI 规则

阅读顺序与读取预算见 `docs/index.md` §1；本节只写契约层特有的内容。

契约不足 → 先改契约，再实现；禁止"先写代码后补契约"。
`make gen` 是契约 → 产物，不是生成契约；新建契约用 `make contract-new`。

### 5.1 生成器上限：4 个（已实现）

| 生成器 | 输出 | 状态 |
|---|---|---|
| graph | `used_by` 写回；rules/states/events 锚点 | ✅ 写回 / ⬜ lineage |
| errors | `platform/errors/*.yaml` → 错误码文档 | ✅ |
| db | DDL / migrations / sqlc 输入 | ✅ `schema.sql` + `0001_baseline.sql`；增量 diff ⬜ P5 |
| openapi | 合并 spec 并展开 service schemas | ✅ |
| features | 路由、页面骨架、类型 | ⬜ P2 |
| platform | 配置校验、常量、CSS 变量 | ⬜ P2 |
| rules/lifecycles | `feature.rules` / `cap.lifecycles` → 校验函数 / 生命周期 | ⬜ P2 |
| service / usecase / handler / worker | apps 代码生成 | ⬜ P2 |

已实现 4 个契约生成器；apps 代码生成属于实现层，不在契约阶段生成。

### 5.2 反例优先

AI 对负向约束的敏感度高于正向描述。

- 每个契约必须有 `do_not`；没有实例级反例时写空列表
- **优先记录已经发生过的错误**，而不是想象出来的风险
- `index.yaml` 的 `common_mistakes` 是跨文件反例索引，AI 可先扫这一层

### 5.3 漂移回归

每次发现 AI 写错（绕过 API 直接查表、手写状态判断、改生成物、用未登记错误码），必须做且只做其中一件：

1. 在对应契约的 `do_not` 加一条
2. 或新增一条能失败的测试断言 / `contract-check` 规则

并在 PR 描述里注明"本次沉淀了哪条约束"。

**契约要随事故变强**，而不是靠人一次写全。

---

## 6. 数据契约的特殊说明

**migrations 是生成物，不是真相源。**

```
data/tables/_base.yaml + tables/*.yaml ──> schema.sql（目标态）
                                             ├─ diff 当前 DB ──> migrations/<ts>_<name>.sql
                                             └─> sqlc 输入
data/storage.yaml ──> 按引擎分组的表条目 + 表级物理事实
```

| 规则 | 说明 |
|---|---|
| 禁止手写迁移 | 改 `tables/*.yaml` 后跑 `make gen-db` |
| 目标引擎 | MySQL 8.0.16+；`live_only` 索引用 functional unique index 表达（`deleted_at IS NULL` 表达式键），不是 PostgreSQL partial index |
| 允许受控调整 | 仅限生成器无法表达的操作（如 `CONCURRENTLY`），必须标 `-- manual-adjust: <原因>` |
| CI 双向校验（⬜ P5） | ① 空库 apply 全部迁移后 schema 必须等于契约目标态；② 契约变更必须有对应迁移 |
| 结构化 DDL | 分区用 `storage.tables.<engine>[].partition`，归档用 `archive_of` 条目（`promotable` 标明能否回迁），表达式索引用 `indexes[].expression`；大表索引的并发策略由运行时迁移配置决定；**不设未结构化 DDL 字段** |

### 6.1 迁移禁止操作

生成器落地后（P2），以下操作直接报错、不产出迁移；在此之前靠 review 拦截：

| 禁止 | 替代做法 |
|---|---|
| `DROP COLUMN` | 先标 deprecated，停止写入一个版本后再删 |
| `RENAME COLUMN` | 加新列 → 双写 → 回填 → 切读 → 删旧列 |
| 给已有数据的表加 `NOT NULL` 且无默认值 | 加可空 → 回填 → 加约束 |
| 原地改列类型 | 新列 + 回填 + 切换 |

迁移文件名：`<utc_timestamp>_<snake_case_name>.sql`。

**诚实边界**：生产环境的在线 DDL（大表加索引、数据回填）无法全自动。契约约束**最终态**，迁移**过程**允许人工介入，并用 `-- manual-adjust: <原因>` 显式声明。

---

## 7. 不设契约与按需扩展

| 对象 | 理由 |
|---|---|
| CI 流水线 | 本身就是 YAML，再抽象是双源 |
| Makefile / Dockerfile / 组件树 / 样式 | 实现细节 |
| playbooks / ADR | 流程与决策记录 |
| 依赖版本 | lockfile 是唯一真相源 |
| 算法、并发实现 | 技术决策；影响行为的已进 tables / rules |
| 日志格式 | 字段名进契约，格式不进 |

按需扩展（有真实需求再建）：`capabilities/integrations.yaml`、`platform/compliance.yaml`、`platform/features.yaml`、`features/i18n.yaml`、`platform/notifications.yaml`。

---

## 8. 当前阶段

P2：契约与 `contract-check` 已可用，生成器待实现（逐项状态见 §5.1）。

`generated` / `tested` 是目标值，不代表已有闸门；每条契约的实际 enforcement 与 stability 以 `index.yaml` 为准。data 层已启用的闸门见 `data/README.md` §7。

`_meta/<type>.yaml` 是字段规范，不是实例契约，不登记 `index.yaml`，也不进入 `generated/`。
