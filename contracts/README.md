# 契约层（contracts）

> **本目录是本仓库的唯一真相源（Single Source of Truth）。**
> 所有实现——Go 代码、Vue 代码、SQL 迁移、配置、文档、测试——都是本目录的**投影**。
> 契约表达力不足时，**扩展契约**，而不是把真相下沉到实现。

---

## 0. 本文件的范围

本文件只写**契约的元规则**：怎么定义、怎么引用、怎么校验、怎么演进、什么不能做。
它不是教程，也不是操作手册。

| 内容 | 写在哪 | 理由 |
|---|---|---|
| 契约元规则、ID、分层、拆分、盲区 | **本文件** | 改契约的人与 AI 必读 |
| 具体任务的步骤与命令序列 | `docs/playbooks/contract-change.md` | 会随工具变化，本文件应稳定 |
| 为什么这样设计、否决了什么 | `docs/adr/` | 决策历史，不是规范 |
| AI 跨任务硬约束（禁止手改 gen、先改契约再实现） | 根 `AGENTS.md` | 需在每次会话生效 |
| 某条契约的字段与取值 | 各契约文件自身 | 就近，避免二次跳转 |
**判断新内容该不该写进本文件**：它是"所有契约都要遵守的规则"吗？是 → 写；只对某一类契约适用 → 写到该组 README 或文件头。

---

## 1. 核心原则

### 1.1 契约大于一切

| 推论 | 具体含义 |
|---|---|
| 实现由契约生成 | 类型、常量、路由、状态机、DDL 均从本目录产出 |
| 生成物不可手改 | `**/generated/` 与 `apps/api/migrations/` 是产物；手改会被 CI 拦截 |
| 需求变更先改契约 | 禁止"先写代码后补契约" |
| 契约不丢信息 | SQL 特性无法结构化时用 `ddl_ext` 逃生口，而非降级契约 |

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
| 接口形状不漂移 | 字段语义是否被正确理解 | `domain/glossary.md` + review |
| 状态转换穷举 | 业务规则是否被正确实现 | `domain/rules` 的可执行用例 |
| 表结构与迁移一致 | 事务边界与并发是否正确 | 集成测试 + race |
| 前后端类型一致 | 性能是否退化 | 基准测试 |
| 枚举、权限、错误码一致 | 契约本身是否写错 | review + 真实用例回归 |
| 引用关系不越层 | 时序与重试的真实行为 | `idempotency` 声明 + 测试 |

**结论**：契约决定"能编译什么"，闸门决定"能合入什么"，流程决定"AI 会先做什么"。三者缺一，一致性只是口号。

不要指望契约替代测试，也不要因为"契约没覆盖"就放弃契约化——先问能不能扩展契约表达力。

---

## 2. 目录结构与五组契约

```
contracts/
├── README.md                 # 本文件：规范正文
├── index.yaml                # 契约地图：所有条目的 ID / 路径 / 消费者 / 强度
├── project.yaml              # 项目元信息（derive 的唯一输入）
│
├── data/                     # ① 数据契约
│   ├── tables/<table>.yaml   #   目标态表结构（真相源；migrations 由此生成）
│   ├── enums.yaml            #   跨端枚举
│   ├── queries/<module>.yaml #   查询契约（SQL 是契约字段，.sql 是产物）
│   ├── seeds/<name>.yaml     #   系统必需数据（幂等）
│   └── migrations-policy.yaml#   迁移策略（禁止操作、零停机步骤）
│
├── capabilities/             # ② 能力契约
│   ├── http/<module>.yaml    #   OpenAPI，按业务模块拆
│   ├── async/<job>.yaml      #   定时任务 / 消息消费者 / webhook
│   ├── internal/<module>.yaml#   跨模块 service 接口
│   └── apps/<app>.yaml       #   应用组合清单：包含哪些能力
│
├── domain/                   # ③ 领域契约
│   ├── entities/<entity>.yaml#   实体、不变量、生命周期
│   ├── states/<machine>.yaml #   状态机
│   ├── rules/<scope>.yaml    #   业务规则（可执行断言）
│   ├── flows/<flow>.yaml     #   业务流程编排
│   └── glossary.md           #   术语表（review-only）
│
├── features/                 # ④ 功能契约（面向用户）
│   ├── routes.yaml           #   路由、菜单、布局
│   ├── pages/<page>.yaml     #   页面：数据源、四态、动作、字段
│   └── flows/<flow>.yaml     #   跨页用户旅程
│
├── platform/                 # ⑤ 平台契约（工程约束）
│   ├── environments.yaml     #   环境清单与差异
│   ├── versioning.yaml       #   版本与兼容策略
│   ├── errors.yaml           #   错误码、HTTP 映射、文案 key
│   ├── http-conventions.yaml #   信封、分页、header、状态码
│   ├── quality.yaml          #   测试策略与门槛
│   ├── config/env.schema.json
│   ├── architecture/{go-arch.yaml,web-deps.json}
│   ├── design/tokens.json
│   ├── observability/{log-fields,metrics}.yaml
│   ├── analytics/events.yaml
│   └── security/{permissions,roles,auth-policy}.yaml
│
└── generated/                # 生成物索引（禁止手改）
    ├── manifest.json         #   生成物清单 + 来源契约
    ├── graph.json            #   锚点图
    └── lineage.md            #   人读血缘
```

---

## 3. 锚点机制

### 3.1 ID 规范

```
<group>.<type>.<scope>.<name>[.<item>]
```

| 组 | 示例 |
|---|---|
| data | `data.table.notes`、`data.table.notes.col.title`、`data.enum.note_status`、`data.query.notes.list` |
| capabilities | `cap.http.notes.create`、`cap.async.notes.cleanup`、`cap.internal.notes.service` |
| domain | `domain.entity.note`、`domain.state.note`、`domain.rule.note.publish_requires_title`、`domain.flow.note.publish` |
| features | `feature.route.note_list`、`feature.page.note_list`、`feature.action.note_list.create` |
| platform | `platform.error.NOTE_NOT_FOUND`、`platform.permission.note_write`、`platform.role.editor`、`platform.event.auth_login_succeeded` |

规则：

- 只用小写、数字、下划线、点分隔
- **ID 一旦发布不可改名**；改名 = 新 ID + 旧 ID 标 `deprecated`
- 跨文件引用一律写 ID，**禁止写文件路径**

### 3.2 分层引用约束

```
features ──> capabilities ──> data
    │              │
    └──────────> domain ──> data
                   │
capabilities ──────┘

platform <── 所有层（只被引用，不引用业务）
```

| 允许 | 禁止 |
|---|---|
| `features → capabilities / domain / platform` | `features → data`（页面禁止直接引用表） |
| `capabilities → domain / data / platform` | `domain → capabilities / features` |
| `domain → data / platform` | `data → capabilities / domain` |
| `data → data / platform` | `platform → 业务组`（三个例外见下） |

**三个显式例外**（图校验白名单）：

| 例外 | 原因 |
|---|---|
| `platform.quality → features / domain` | 定义测试范围必须指向被测对象 |
| `platform.analytics → capabilities` | 埋点事件必须声明由哪个接口触发 |
| `platform.obs.log → platform.errors` | 日志脱敏需要错误码枚举（同组） |

**planned 引用**：未落地的能力可用 `planned:<id>` 前缀引用，图校验放行并在报告中单列。落地后必须替换为真实 ID，否则 CI 警告。

越层引用让 AI 跳过 API 直接操作表，是漂移的主要来源。**图校验会直接报错。**

### 3.3 图校验

```bash
make contract-graph     # 校验 + 生成 graph.json / lineage.md
make contract-impact id=data.table.notes.col.title
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
feature.page.note_list
  ├─ cap.http.notes.list ──> data.query.notes.list ──> data.table.notes
  └─ cap.http.notes.create ─> data.table.notes ──> domain.entity.note ─> domain.state.note
```

### 3.4 契约变更流程

改任何契约前必须回答四问：

1. 是否破坏兼容？是 → 升版本或走弃用流程
2. 是否需要新版本？URL major（`/v1` → `/v2`）
3. 是否需要数据迁移？先改 `data/tables`，再生成迁移
4. 影响面是什么？`make contract-impact id=<id>` 输出受影响的 API、页面、测试、生成物

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

`index.yaml` 中的 `path` **相对 `contracts/` 目录**，例如 `data/tables/notes.yaml`。
生成器与校验器统一按此规则拼接，避免仓库绝对路径进入契约。

### 4.1 文件头（所有契约文件必须有）

```yaml
# contract: <id>
# summary: <一句话，≤40 字>
# enforcement: generated | generated+tested | tested | review-only
# consumers: [api, web/admin, worker]
# refs: [<上游 ID>, ...]
# do_not:
#   - <AI 最容易犯的错>
```

### 4.2 拆分与行数

| 类型 | 拆分单位 | 上限 |
|---|---|---|
| `data/tables/*` | 一表一文件 | **不限**（表结构不拆分） |
| `capabilities/http/*` | 一模块一文件；>15 接口按子域再拆 | 200 |
| `capabilities/async/*` | 一任务/消费者一文件 | 120 |
| `capabilities/internal/*` | 一模块一文件 | 120 |
| `domain/entities/*` | 一实体一文件 | 120 |
| `domain/states/*` | 一状态机一文件 | 150 |
| `domain/rules/*` | 一组内聚规则（≤10 条） | 120 |
| `domain/flows/*` | 一流程一文件 | 120 |
| `features/pages/*` | 一页面一文件 | 150 |
| `features/flows/*` | 一旅程一文件 | 120 |
| `platform/**` | 一关注点一文件 | 150 |
| `index.yaml` | 单文件，只放摘要 | 400 |
| 各组 `README.md` | 组规范，可按主题合并 | 400 |

### 4.3 防过度拆分

拆完每个文件 < 20 行，或必须同时读 5 个以上文件才能理解一个概念 → 合并。

### 4.4 示例必填

每个契约至少提供：

- **1 个正例**：合法输入 + 期望输出
- **1 个反例**：最容易被 AI 写错的用法

没有示例的契约视为不完整——AI 会自行编造"合理但错误"的用法。
反例写在文件头 `do_not`，或字段级 `example_bad`。

### 4.5 人读视图自动生成

`docs/reference/generated/` 由契约生成，**禁止手写**：

| 文件 | 来源 |
|---|---|
| `errors.md` | `platform/errors.yaml` |
| `permissions.md` | `platform/security/permissions.yaml` |
| `data.md` | `data/tables/*.yaml` |
| `lineage.md` | `generated/graph.json` |

AI 读 YAML，人读 Markdown，单一源。手写视图必然与契约漂移。

---

## 5. AI 工作流

```
1. 读 docs/index.md                 获取地图
2. 读对应 playbook                  获取本任务必读契约 ID 列表
3. 读 index.yaml 相关条目           只看 summary，不读正文
4. 读 2–5 个契约文件                含 do_not / examples
5. 契约不足 → 先改契约，再实现       禁止"先写代码后补契约"
6. make gen && make verify          生成 + 锚点图 + 测试
7. PR 附 make contract-impact 报告
```

### 5.1 读取预算

单个任务最多读 5 个契约文件。超过说明任务粒度太大，应拆任务，而不是硬读。

### 5.2 生成器上限：6 个

| 生成器 | 输出 |
|---|---|
| openapi | Go server 类型 + TS client |
| db | DDL / migrations / sqlc 输入 |
| domain | 状态机、常量、断言、测试骨架 |
| features | 路由、页面骨架、类型 |
| platform | 配置校验、常量、CSS 变量 |
| graph | graph.json、lineage.md |

超过 6 个，维护成本会超过收益。

### 5.3 反例优先

AI 对负向约束的敏感度高于正向描述。

- 每个契约必须写 `do_not`
- **优先记录已经发生过的错误**，而不是想象出来的风险
- `index.yaml` 的 `common_mistakes` 是跨文件反例索引，AI 可先扫这一层

### 5.4 漂移回归

每次发现 AI 写错（绕过 API 直接查表、手写状态判断、改生成物、用未登记错误码），必须做且只做其中一件：

1. 在对应契约的 `do_not` 加一条
2. 或新增一条能失败的测试断言

并在 PR 描述里注明"本次沉淀了哪条约束"。

**契约要随事故变强**，而不是靠人一次写全。

---

## 6. 数据契约的特殊说明

**migrations 是生成物，不是真相源。**

```
data/tables/*.yaml ──> schema.sql（目标态）
                          ├─ diff 当前 DB ──> migrations/<ts>_<name>.sql
                          └─> sqlc 输入
```

| 规则 | 说明 |
|---|---|
| 禁止手写迁移 | 改 `tables/*.yaml` 后跑 `make gen-db` |
| 允许受控调整 | 仅限生成器无法表达的操作（如 `CONCURRENTLY`），必须标 `-- manual-adjust: <原因>` |
| CI 双向校验 | ① 空库 apply 全部迁移后 schema 必须等于契约目标态；② 契约变更必须有对应迁移 |
| 逃生口 | `ddl_ext` 承载原生 SQL 片段，必须标 `escape_hatch: true`；每表 ≤3 处，全局 ≤20 处 |

**诚实边界**：生产环境的在线 DDL（大表加索引、数据回填）无法全自动。契约约束**最终态**，迁移**过程**允许人工介入并在 `migrations-policy.yaml` 中声明。

---

## 7. 不设契约与按需扩展

| 对象 | 理由 |
|---|---|
| CI 流水线 | 本身就是 YAML，再抽象是双源 |
| Makefile / Dockerfile | 实现细节 |
| playbooks / ADR | 流程与决策记录 |
| 依赖版本 | lockfile 是唯一真相源；准入规则放 `docs/reference/` |
| 组件树、样式细节 | 实现 |
| 算法、并发实现 | 技术决策；影响行为的（乐观锁、事务语义）已进 tables / rules |
| 日志格式实现 | 字段名进契约，格式不进 |

按需扩展（有真实需求再建）：`capabilities/integrations.yaml`、`platform/compliance.yaml`、`platform/features.yaml`、`features/i18n.yaml`、`platform/storage.yaml`、`platform/notifications.yaml`。

---

## 8. 当前状态

P2 阶段：目录与最小示例（`notes`）已落盘，生成器待实现。

| 组 | 已建 | 待建 |
|---|---|---|
| data | tables/notes、enums、queries/notes、seeds/roles、migrations-policy | 其余业务表 |
| capabilities | http/{shared,notes}、async/notes_cleanup、internal/notes | 其余模块、auth |
| domain | entities/note、states/note、rules/note、flows/note_publish、events/note、glossary | 其余领域 |
| features | routes、pages/{note_list,note_detail,login,not_found}、flows/note_create | 其余页面 |
| platform | 15 个文件（环境/版本/错误/HTTP/质量/配置/架构/设计/观测/埋点/安全） | — |
| platform | environments、versioning、errors、http-conventions、quality、config、architecture、design、observability、analytics、security | — |
