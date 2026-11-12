# web-seed 架构设计与实施方案

> **版本** v3.2 · **状态** P0/P1 完成；契约层已重构为五组；P2 进行中 · **更新** 2026-11-11
> **已定决策**：目录形态 `1A`（`apps/api` + `apps/web`）· UI `2A`（Tailwind + shadcn-vue）· 能力档 `3C`（全家桶，默认关闭）
>
> 本文是模板种子工程自身的架构设计与实施计划，不是派生项目的业务文档。
> 变更本文需同步更新第 0 节的进度与第 9 节的决策记录。

---

## 0. 实施进度

### 0.1 图例

| 标记 | 含义 |
|---|---|
| `[ ]` | 未开始 |
| `[~]` | 进行中 |
| `[x]` | 已完成 |
| `[!]` | 阻塞（原因写在任务后） |

### 0.2 阶段总览

| 阶段 | 目标 | 出口条件 | 状态 |
|---|---|---|---|
| P0 | 技术验证（Spike） | 5 项假设全部有实测结论 | `[x]` 见 0.4 |
| P1 | 仓库骨架 | `make help`/`build`/`lint`/`doctor` 全通过 | `[x]` |
| P2 | 契约生成链 | `make gen` 幂等、CI diff 通过 | `[~]` 契约层 + `_meta` 校验试点已落盘，生成器待实现 |
| P3 | 后端地基 | platform 六件套可用 | `[ ]` |
| P4 | 最小纵切 | accounts 全链路跑通（DB→API→页面） | `[ ]` |
| P5 | 一致性闸门 | 七道闸门全部生效 | `[~]` 闸门 1–3 原型已验证 |
| P6 | 可选模块 | auth/rbac/audit 可摘除地接入 | `[ ]` |
| P7 | 治理与文档 | derive/doctor/ADR/playbooks 齐备 | `[ ]` |

### 0.3 任务清单

#### P0 技术验证（落盘前必须完成）

- [x] **S1** `oapi-codegen` + chi：**可行**，采用 `strict-server` + `OapiRequestValidator`
- [x] **S2** `sqlc` 动态过滤/排序：**可用但有边界**，动态 ORDER BY 只能 `CASE WHEN` 枚举且失去索引
- [x] **S3** `go-arch-lint`：**可行**，需每个业务模块独立 component
- [x] **S4** pnpm workspace + Vite：**可行**，pnpm 12 需 `allowBuilds`
- [x] **S5** Tailwind v4 + tokens：**可行**，共享包必须显式 `@source`

### 0.4 P0 实测结论（2026-11-11）

环境：Go 1.27.0 / Node 26.7.0 / pnpm 12.18.0 / oapi-codegen v2.8.2 / chi v5.5.0 / kin-openapi v0.153.0

| 项 | 结论 | 关键证据与坑 |
|---|---|---|
| S1 | 可行 | `strict-server` 让响应类型由契约封闭；`NewStrictHandlerWithOptions` 可统一 problem+json 错误；**strict 只 decode 不校验**，必须额外挂 `OapiRequestValidatorWithOptions` |
| S2 | 可行有边界 | `sqlc.narg` 可用；`emit_pointers_for_null_types` 只对 text 生效，timestamptz/uuid 仍是 `pgtype.X` → repo 需适配层；动态 ORDER BY 只能用 `CASE WHEN` 枚举，且绕过索引 |
| S3 | 可行 | 必须**每个业务模块独立 component**，单一 `modules` 组件无法表达"模块间禁止"；`cmd` 需允许依赖 `platform`；违规报错带文件:行号 |
| S4 | 可行 | `workspace:*` + `vue-tsc` 跨包类型检查通过；**pnpm 12 配置键是 `allowBuilds`**（v11 起 `onlyBuiltDependencies` 已移除） |
| S5 | 可行有硬配置 | Tailwind v4 **不自动扫描 workspace 共享包**，每个 app 的 CSS 必须 `@source` 每个共享包；`@theme` 变量默认只输出被使用的；包名 `@import` 可解析 |

**闸门原型实测**

| 闸门 | 结果 | 细节 |
|---|---|---|
| 2 路由比对 | 通过 | `api.GetSwagger()` 直接拿内嵌 spec，无需读 YAML；`chi.Walk` 枚举；负向用例（加 `/v1/debug`）可见 |
| 3 响应校验 | 通过 | `additionalProperties: false` 下注入字段被拒；报错含 schema 与值 |
| 请求校验 | 通过 | `title:""` 被 400 拦截，错误可自定义为 problem+json |

**版本陷阱（必须锁定或写进 playbook）**

1. kin-openapi v0.153 移除 `T.FindRoute`，改用 `routers/legacy.NewRouter(doc)`
2. Vue 3.5 SFC 编译器无法解析 `defineProps<VariantProps<typeof button>>()`（cva 复杂类型），改用显式联合类型
3. pnpm 12 用 `allowBuilds: { esbuild: true }`
4. `strict-server` 与 `OapiRequestValidator` 必须同时启用，缺一不可

### 0.5 P1/P2 待验证假设

| # | 假设 | 风险 | 验证时机 |
|---|---|---|---|
| A1 | ~~oapi-codegen 跨文件 `$ref`~~ | 已规避：生成前先合并 `shared.yaml` + 模块文件为单 spec | 已验证策略 |
| A2 | 合并后的 spec 能被 oapi-codegen 与 openapi-typescript 同时消费 | 任一工具不兼容则需调整合并粒度 | P2 |
| A3 | Tailwind `@source` 在被 `@import` 的包内 CSS 中仍生效 | 不生效则退回 app 内写相对路径 | P1 前端骨架 |
| A4 | pnpm `-r build` 严格按拓扑先构建 tokens/ui | 否则 app 构建拿不到 theme.css | P1 |
| A5 | `golang:1.27-alpine` / `node:26-alpine` 镜像 tag 可用 | 镜像构建失败 | P3 真实 Docker |

> A3 / A4 已于 P1 实测通过：`apps/web/packages/ui-kit/styles.css` 的 `@source "./src"` 生效，`pnpm -r build` 先构建 tokens。

#### P1 仓库骨架

- [x] 根文件：`Makefile` `lefthook.yml` `.editorconfig` `.gitattributes` `.nvmrc` `pnpm-workspace.yaml` `package.json`
- [x] `docs/{index.md,adr/,playbooks/,guides/,reference/}`
- [x] `contracts/` 五组目录 + `index.yaml` + `project.yaml` + `README.md`
- [x] `apps/api` 骨架（`cmd/` `internal/{platform,modules,composition,generated}`、`composition/<app>/handlers/`、`migrations/` `queries/`）
- [x] `apps/web/admin` 骨架（`src/{app,pages,features,shared}`）
- [x] `apps/web/packages/{api-client,ui-kit,design-tokens,tsconfig,eslint-config}`
- [x] `compose.yaml`（profiles: core/data/mail/object/observability）
- [x] `scripts/{derive,doctor,check-docs,gen-env}.sh` 空壳 + `make help`
- [x] `Makefile` 预留 `gen` target（P2 实现），空跑时提示未配置

#### P2 契约生成链

- [ ] `capabilities/http/*.yaml` + `shared.yaml` → 合并为 `generated/openapi/admin.yaml`
- [ ] 合并 spec → Go server 类型（`oapi-codegen strict-server`）
- [ ] 合并 spec → TS 类型（`openapi-typescript` + `openapi-fetch`）
- [ ] `data/tables/*.yaml` → `schema.sql` + 增量 `migrations/*.sql`
- [x] 数据契约重构：`tables/_base` 基类、`data/storage.yaml`、account/token/auth_* 拆分（契约已落盘）
- [x] 契约元规范试点：`_meta/{data.tables,data.enums}` + `make contract-new` + `make contract-check`（结构 / ID / used_by）
- [ ] `data/queries/*.yaml` → `queries/*.sql` → sqlc 代码
- [ ] `data/enums.yaml` → Go 常量 + TS union + DB check
- [ ] `platform/errors.yaml` + `platform/security/permissions.yaml` → 两端常量
- [ ] `platform/config/env.json` → `.env.example`（`gen-env.sh`）
- [ ] `platform/design/tokens.json` → CSS variables（`@theme`）
- [ ] `business/**` → 状态机常量 + 断言 + 测试骨架
- [ ] `features/**` → 路由表 + 页面骨架 + 类型
- [ ] 全部 `refs` → `generated/{manifest.json,graph.json,lineage.md}`
- [ ] `make gen` 幂等：连续两次生成结果一致

#### P3 后端地基（`internal/platform`）

- [ ] `config`：env 加载 + fail-fast + 测试断言 key 集合与 schema 一致
- [ ] `httpserver`：中间件链（request-id / recover / logging / CORS / 安全头 / 超时 / 体积限制 / 请求校验）
- [ ] `httpserver`：RFC 9457 错误信封 + error-codes 映射
- [ ] `database`：pgx pool + `WithTx(ctx, fn)` + querier 注入
- [ ] `observability`：slog（字段契约）+ RED 指标 + OTel hook（默认 `exporter=none`）
- [ ] `security`：JWT 签发/校验（audience 区分）+ 密码哈希 + CORS 白名单
- [ ] 优雅关闭：信号 → 停收 → 等在途 → 关池
- [ ] `/healthz`（不查依赖）与 `/readyz`（查 DB）分离

#### P4 最小纵切（示例模块 accounts）

- [ ] 由 `data/tables/_base.yaml` + `account.yaml` 生成 schema 与迁移（含软删、乐观锁、部分唯一索引）
- [ ] 由 `data/queries/account.yaml` 生成 `.sql` 并跑 sqlc
- [ ] `internal/modules/accounts`（service + repo，不含 HTTP）
- [ ] `internal/composition/adminapi/handlers`（实现 StrictServerInterface）
- [ ] `routes_test.go`：路由集合 == 契约集合 + 响应 schema 校验
- [ ] 由 `features/**` 生成路由与页面骨架，补齐四态
- [ ] Playwright 跑通 `feature.flows.account_create`

#### P5 一致性闸门（详见附录 A）

- [x] 闸门 0：锚点图校验（重复 ID / 悬空 / 越层 / 循环）——原型脚本已跑通
- [x] 闸门 1：`make gen && git diff --exit-code`（生成确定性已在 S1 验证）
- [x] 闸门 2：路由集合 == 契约集合（含负向用例）
- [x] 闸门 3：响应 schema 校验（含注入字段负向用例）
- [ ] 闸门 4：`oasdiff` breaking change 检测
- [ ] 闸门 5：前端 client 方法覆盖断言
- [ ] 闸门 6：禁止手改 `generated/`（文件头 + lint）
- [ ] 闸门 7：禁止绕过 client 直连（`no-restricted-imports`）

#### P6 可选模块（默认非自动生成）

- [ ] `auth`：注册/登录/刷新/登出/会话吊销（audience = admin，未来按需扩展）
- [ ] `rbac`：角色/权限点 + `usePermission()`
- [ ] `audit`：who/what/when/before/after
- [ ] `user`：资料与偏好（与 auth 分离；模板默认不装，派生按需）
- [ ] `upload`：S3 接口抽象 + 预签名（默认不装）
- [ ] `notification`：站内信 + 邮件（默认不装）
- [ ] `webhook`：收/发/签名/重试（默认不装）

#### P7 治理与文档

- [ ] `scripts/doctor.sh`：生成物漂移、文档路径失效、孤儿模块、依赖漏洞
- [ ] `scripts/derive.sh`：重命名 + 清示例 + 重置 git + 生成 `docs/DERIVED.md`
- [ ] `scripts/check-docs.sh`：文档内路径/链接有效性
- [ ] CI：api / web / e2e / docs / self 五路并行，目标 < 5 分钟
- [ ] ADR 六篇（见第 9 节）
- [ ] playbooks：`contract-change` `add-app` `add-endpoint` `add-table` `add-feature` `rename-project`
- [ ] `.opencode/skills/`：`contract-change` `add-endpoint` `add-table` `add-app` `verify`（薄封装，见第 10 节）
- [ ] `AGENTS.md`（根 + `apps/api` + `apps/web`）

---

## 1. 定位与设计纲领

### 1.1 定位

通用脚手架 / 基础底座模板，**可复制派生**，用于快速新建 Web + API 项目。不是业务项目，是模板种子工程。

派生关系是 **copy-on-derive**：派生后独立演化，模板不承诺上游回灌，只提供 `CHANGELOG` 供人工拣选。

### 1.2 三条纲领

1. **可执行规约 > 散文文档**
   每条约束尽量落到机器可校验处：分层 → 架构 linter；API 形状 → 生成；表结构 → 契约生成 migrations；配置 → schema。

2. **可摘除设计（removable by construction）**
   可选能力在任何地方都不被引用，只通过一处显式注册表接入。删除 = 删目录 + 删一行注册。

3. **薄校验优先，生成器克制**
   能用测试/脚本比对的，不写代码生成器。生成器上限 **6 个**：openapi、db、business、features、platform、graph。超过则维护成本压过收益。

### 1.3 四防目标

| 目标 | 典型失效 | 机制 |
|---|---|---|
| 保真 | AI 凭记忆实现，与契约不符 | 类型由契约生成，不符合即编译失败 |
| 防漂移 | 文档/生成物/实现三者不一致 | 生成物 diff + 文档路径校验 + 路由比对 |
| 防幻想 | 编造字段/函数/已实现功能 | 禁止手改 `generated/`、playbook 强制查证现状、依赖准入 |
| 防扩散 | 顺手重构、越界改文件 | 范围条款 + 一任务一 playbook + PR 改动清单 |

**硬规则**：任何 AI 会话要做的任务，必须存在对应 playbook；没有就先写 playbook 再动手。

---

## 2. 契约体系

> 契约清单、粒度纪律、变更三问的唯一正文是 [`contracts/README.md`](../../contracts/README.md)。本节只保留设计意图与生成链。

### 2.1 为什么是一等公民

契约解决的不是"文档没人看"，而是**改动成本不对称**：改契约要生成、修编译、同步前端；改实现只要加几行。没有约束时，AI 永远选便宜的路。契约 + 生成 + 闸门的作用，是给"绕过契约"加税。

每类契约必须具备三件事：**有校验、有 playbook、有删除路径**，缺一即会腐烂。

### 2.2 五组结构

| 组 | 职责 | 真相源 |
|---|---|---|
| `data/` | 物理表（含 `_base` 基类）、逻辑实体、查询、存储与部署、迁移规则（不感知业务） | `data/tables/*.yaml`（migrations 由此生成）+ `data/storage.yaml` |
| `capabilities/` | 实现层：HTTP、异步、跨模块 service、业务流程编排 | `capabilities/**` |
| `business/` | 业务规格：状态机、规则、领域事件、术语（不含实体与实现） | `business/**` |
| `features/` | 功能定义：路由、页面、用户旅程 | `features/**` |
| `platform/` | 横切：环境、版本、错误、枚举、HTTP 约定、质量、配置、架构、设计、观测、埋点、安全 | `platform/**` |

`index.yaml` 是契约地图，`generated/` 是产物。详见 [`contracts/README.md`](../../contracts/README.md)。

### 2.3 生成链

```
data/tables/*.yaml
  ├─ schema.sql ──> sqlc ──> Go 查询代码
  └─ diff DB ─────> migrations/*.sql

capabilities/http/*.yaml + shared.yaml
  ├─ 合并 ─────────> generated/openapi/admin.yaml
  ├─ oapi-codegen ─> apps/api/internal/generated/openapi/
  └─ openapi-typescript ─> apps/web/packages/api-client/src/generated/

data/queries/*.yaml ──> queries/*.sql ──> sqlc

business/** ──> 状态机 / 常量 / 断言 / 测试骨架
features/** ──> 路由 / 页面骨架 / 类型
platform/** ──> 配置校验 / 常量 / CSS 变量

全部 refs ──> generated/graph.json + lineage.md
```

生成物入库，CI 用 `make gen && git diff --exit-code` 保证同步。

### 2.4 锚点机制

- ID 规范：`<group>.<type>.<scope>.<name>[.<item>]`
- 跨文件引用写 ID，不写路径
- **唯一顺序**：`feature → business → capabilities → data`（单向链，无反向）
- **锚点只在上游写**：`feature --uses--> business`，`business --implemented_by--> capabilities`，
  `capabilities --persists_via--> data`
- **capabilities 契约不引用 business**：返回 `data` 行；规则与事件由 business 侧锚点（`cap.flows` 例外：仅允许引用 `business.states/rule/event` 做编排）
- 图校验禁止 `cap.http` / `cap.services` / `cap.async` → `business`，仅放行 `cap.flows` → `business.{state,rule,event}`
- 影响面：`make contract-impact id=<id>`

**引用必须写在结构化字段里**（`ref` / `refs` / `capability` / `use` / `emits` / `page` / `route` / `x-*`）。
写在注释或 `description` 里的 ID 不参与校验——那不是引用，只是文字。
这条规则决定了图校验能否真正生效。

---

## 3. 目录结构

`[C]` core · `[O]` optional · `[E]` example

```
web-seed/
├── AGENTS.md  README.md  CONTRIBUTING.md  SECURITY.md  CHANGELOG.md
├── Makefile  lefthook.yml  .editorconfig  .gitattributes  .nvmrc
├── pnpm-workspace.yaml  package.json
├── .env.example                      # 生成物，禁止手改
│
├── contracts/                        [C] 五组契约（唯一真相源）
│   ├── index.yaml                    #   契约地图（AI 入口）
│   ├── project.yaml                  #   项目元信息（derive 输入）
│   ├── data/                         #   tables(_base) / entities / queries / storage / _policy
│   ├── capabilities/                 #   http / async / services / flows
│   ├── business/                       #   states / rules / events / glossary
│   ├── features/                     #   routes / pages / flows
│   ├── platform/                     #   环境/版本/错误/枚举/HTTP/质量/配置/架构/设计/观测/埋点/安全
│   └── generated/                    #   manifest / graph / lineage
├── docs/
│   ├── index.md                      # 唯一地图（AI 入口）
│   ├── adr/                          # 决策记录（含 Rejected alternatives）
│   ├── playbooks/                    # 步骤化手册
│   ├── guides/                       # 解释性文档（本文在此）
│   └── reference/                    # conventions / glossary / dependencies
│
├── apps/
│   ├── api/                          [C] 单 Go module，多 cmd，多应用装配
│   │   ├── AGENTS.md
│   │   ├── cmd/adminapi/main.go
│   │   ├── internal/
│   │   │   ├── platform/{config,httpserver,database,observability,security,storage}
│   │   │   ├── modules/{health,auth[O],rbac[O],audit[O],user[O],upload[O],notification[O],webhook[O],accounts[E]}
│   │   │   │   └── registry.go       # 唯一注册表
│   │   │   ├── composition/adminapi/  # app.go / routes.go / routes_test.go
│   │   │   └── generated/            # 生成物入库
│   │   ├── migrations/  queries/      # 生成物：来自 contracts/data
│   │   ├── sqlc.yaml  codegen/  Dockerfile
│   │   └── ...
│   └── web/
│       ├── admin/                    [C] 模板默认端
│       │   ├── AGENTS.md  vite.config.ts  Dockerfile
│       │   └── src/{app,pages,features,shared}
│       │   # user 端为派生时按需新增（非模板默认）
│       └── packages/{api-client,ui-kit,design-tokens,tsconfig,eslint-config}
│
├── compose.yaml                      [O] profiles: core/data/mail/object/observability
├── scripts/{derive,doctor,check-docs,gen-env}.sh
├── scripts/contract/{scaffold,validate}.sh
├── scripts/cmd/                    # 契约工具 Go module
├── .opencode/skills/                 # AI 行为入口（薄封装 playbook，见第 10 节）
└── .github/{workflows/{ci,template-selftest}.yml, PULL_REQUEST_TEMPLATE.md, ISSUE_TEMPLATE/}
```

---

## 4. 多应用架构

### 4.1 三层职责

```
cmd/<app>/main.go              只做启动：解析配置 → 构造 → 运行 → 关闭
internal/composition/<app>/           装配：选模块、挂路由、配中间件、聚合 handler
internal/composition/<app>/handlers/  HTTP 适配器：参数转换 → 调 service → 映射响应（禁止业务逻辑）
internal/modules/<biz>/        业务能力：service + repo，不依赖 HTTP 契约
internal/platform/             与业务无关的基础设施
```

### 4.2 硬边界（机器校验）

1. `cmd/*` 只能 import `internal/composition/*`，不许直接碰 `modules`
2. `modules/*` 之间禁止横向 import，跨模块走 service 接口或事件
3. `composition/*` 只允许"选择 + 组装 + 配置"，出现 `if/for` 业务分支即违规
4. `apps/web/*` 之间禁止互相 import，共享只能走 `apps/web/packages/*`
5. `modules/*` 禁止 import `internal/generated/http`；sqlc 产物在 `internal/generated/db`，仅 `modules/*/repo` 可 import

### 4.3 装配方式

- 模块构造全显式手写，禁止 `init()` 自注册
- 禁止 wire / fx（此规模属过度）
- `StrictServerInterface` 由 `composition/<app>/handlers` 聚合实现；模块只暴露 service
- 加一个 API 应用 = 契约 + `internal/composition/newapp/` + `cmd/newapp/`，业务模块零改动

### 4.4 每应用一组契约

业务契约按模块拆分，与 API 应用解耦；应用只声明"我包含哪些模块"：

```yaml
# contracts/project.yaml（apps 段）
apps:
  admin:
    kind: api
    includes: [cap.http.shared, cap.http.account]
    generated: { spec: generated/openapi/admin.yaml }
```

生成器按该清单合并出 `generated/openapi/admin.yaml`，供 oapi-codegen 与文档站使用。
新增应用 = 在 `project.yaml` 加一段 + `composition/<app>/` + `cmd/<app>/`，业务模块与模块契约零改动。

### 4.5 多应用待决问题

| 问题 | 影响 | 倾向 |
|---|---|---|
| 未来新增 user 端时登录态是否共享 | Cookie 域策略、CSRF 方案 | 默认不共享（不同域，各自登录） |
| 新增端是否与 admin 共库 | 迁移归属、只读账号 | 默认共库，模块自治表 |
| SPA 的 SEO 取舍 | 是否留 Nuxt 迁移路径 | 默认 SPA，ADR 记录代价 |

---

## 5. 后端设计

### 5.1 技术默认值

| 关注点 | 默认 | 备选 | 理由 |
|---|---|---|---|
| HTTP | `chi` + `oapi-codegen strict-server` | stdlib / gin | 响应类型由契约封闭；strict 只 decode，校验由 `OapiRequestValidator` 补齐 |
| 数据 | `pgx` + `sqlc` | GORM | 编译期类型安全，SQL 可见 |
| 迁移 | `goose` | atlas | 纯 SQL、可 embed、只前进 |
| 契约生成 | `oapi-codegen` | swaggo | design-first 防漂移 |
| 日志 | `slog` | zap | 标准库 |
| 追踪 | OTel SDK（`exporter=none`） | — | 能力预留，不默认外发 |

### 5.1.1 契约生成模式（已实测）

- `strict-server` 为默认：`StrictServerInterface` 返回 `XxxResponseObject`，响应形状由契约封闭，返回未声明类型会在运行时被拒
- 请求校验单独挂 `OapiRequestValidatorWithOptions`（strict 只 decode，不校验 `minLength`/`format`）
- 错误统一走 `StrictHTTPServerOptions` 的两个 handler，输出 RFC 9457
- 生成物入库；CI 用 `make gen && git diff --exit-code` 保证同步
- handler 落在 `composition/<app>/handlers/`，模块只提供 service——避免模块耦合 HTTP，也让多应用不同 spec 无需改模块

### 5.2 错误处理

- HTTP 层用 RFC 9457 `application/problem+json`：`{type,title,status,detail,instance,code,errors[]}`
- 业务错误码与 HTTP 状态解耦，映射表在 `platform/errors.yaml`
- 不返回的 error 必须记录（含栈 + 上下文）

### 5.3 事务边界

- `database.WithTx(ctx, fn)`，repo 从 ctx 取 querier
- 禁止把 `*pgx.Tx` 当参数层层传递
- 事务只在 service 层开启，handler 不开事务

### 5.4 数据规范（`contracts/data/README.md`）

- 表结构基类 `data/tables/_base.yaml`：`id / status / created_at / updated_at / deleted_at` 五列，所有物理表继承
- 主键 UUIDv7；时间列 `timestamp`，统一 UTC；`updated_at` 由应用写入
- 业务表可选 `version` 做乐观锁；不是基类字段
- 存储引擎、部署拓扑、连接池、锁在 `data/storage.yaml`，禁止逐表声明 engine
- 枚举用 `string + check`，不用 PG enum（迁移困难）
- 软删的唯一约束必须用部分索引：`WHERE deleted_at IS NULL`
- 索引命名 `idx_<table>_<cols>`；外键默认加，热点表可评估去掉
- 迁移只前进；零停机三步：加可空列 → 回填 → 加约束
- 软删默认过滤，必须在 repo 层统一，禁止每处手写
- 分区/表达式索引/`CONCURRENTLY` 用结构化字段；不设未结构化 DDL 字段

### 5.5 请求治理

| 项 | 约定 |
|---|---|
| 请求 ID | `X-Request-Id`，客户端可传，服务端校验否则生成，贯穿日志与响应 |
| 超时 | server read/write、handler context、外部调用三层各有默认值 |
| 体积 | 请求体上限可配，超限返回 413 |
| 幂等 | `Idempotency-Key` 中间件留口 |
| 健康 | `/healthz` 不查依赖；`/readyz` 查 DB |
| 关闭 | 信号 → 停收 → 等在途 → 关池 |
| 版本 | URL major `/v1`；弃用用 `Deprecation` / `Sunset` header |

---

## 6. 前端设计

### 6.1 分层（四层，不设 entities）

```
src/
├── app/        router(守卫/权限/懒加载) providers(query/错误边界/i18n) layouts styles
├── pages/      路由级视图
├── features/   用户动作，可独立删
└── shared/
    ├── ui/          primitives + 四态组件（loading/empty/error/forbidden）
    ├── api/         api-client 封装、拦截器、错误码→文案映射
    ├── lib/         datetime / money / id / storage / analytics
    ├── composables/ usePermission / useQueryState
    ├── config/      运行时配置（zod 校验）
    └── locales/     文案集中管理（预留 i18n）
```

### 6.2 约定

- 类型只能来自 `apps/web/packages/api-client`，禁止手写 API 类型
- 页面只能通过 `capability` 引用接口，禁止直接写 URL，更禁止引用 `data.*`
- 禁止绕过 client 直连 `fetch`
- 时间统一 UTC 传输、本地展示；金额整数分 + 币种
- 四态（loading/empty/error/forbidden）使用共享组件，禁止各页自造
- 路由与页面骨架由 `features/**` 契约生成，禁止手写路由表
- 运行时配置由 `public/config.js` 注入，前端启动时 zod 校验 → 同一镜像多环境
- 主题走 CSS 变量，暗色模式为 token 的第二组值
- **Tailwind v4 不自动扫描 workspace 共享包**：由 `apps/web/packages/ui-kit` 自备 `styles.css`（内含 `@source "./src"`），app 只 `@import "@webseed/ui-kit/styles.css"`，避免路径耦合；`@theme` 变量默认只输出被使用的

### 6.3 埋点

| 层 | 载体 | 默认 |
|---|---|---|
| 前端行为 | `platform/analytics/events.yaml` → TS 类型；`track()` provider 可插拔 | noop |
| 后端指标 | `platform/observability/metrics.yaml` | 注册不导出 |
| 审计 | `audit` 模块 | 开启 |

规则：事件名 `<domain>.<past_tense>`；属性 snake_case；`pii: true` 字段生成时剔除；未登记事件编译失败。

---

## 7. 工程治理

### 7.1 工具链

| 项 | 选择 | 说明 |
|---|---|---|
| 任务入口 | `Makefile` | 唯一入口，`make help` 自文档 |
| 钩子 | `lefthook` | 单二进制，跨语言 |
| Go lint | `golangci-lint` | |
| 前端 lint | ESLint + Prettier | 共享 `apps/web/packages/eslint-config` |
| 架构校验 | `go-arch-lint` / `dependency-cruiser` | |
| 契约兼容 | `oasdiff` | |
| 测试 | testify / Vitest / Playwright | |
| 前端包管理 | pnpm 12 workspace | `allowBuilds` 显式放行 esbuild 等构建脚本 |

### 7.2 CI

五路并行，目标 < 5 分钟：

```
contract: gen-diff → contract-graph → oasdiff
api:      lint → arch-lint → test → build → schema-verify
web:      lint → typecheck → test → build → client-coverage
e2e:      compose up → playwright
docs:     check-docs
self:     doctor（模板自身健康）
```

### 7.3 模板治理

- `doctor.sh`：生成物漂移、文档路径失效、孤儿模块、依赖漏洞
- `derive.sh`：交互式重命名、清示例、重置 git、写入 `docs/DERIVED.md`
- 模板自带 `template-selftest.yml`，防止模板本身腐烂

### 7.4 版本锁定

`go.mod` 的 go 指令 + `package.json` 的 `packageManager` + `.nvmrc`，零新工具。

---

## 8. 明确不做

多租户 · Outbox · SSR/Nuxt · 微服务 · K8s manifests · CDN · 备份 · 告警平台 · 完整 i18n · 低代码 · turbo/nx · GraphQL · gRPC · MySQL · MongoDB · 分库分表 · 多主 · 跨地域。

**"完整"的定义**：不是功能全量，而是所有横切面都有明确归属——做 / 不做 / 留口，没有"没人管"的地带。

---

## 9. 决策记录（ADR 索引，待补）

| 编号 | 决策 |
|---|---|
| 0001 | 采用 ADR 记录架构决策（含 Rejected alternatives） |
| 0002 | **契约层为唯一真相源**：migrations / queries / 类型 / 路由均由契约生成 |
| 0003 | 五组契约模型：`data / capabilities / business / features / platform` |
| 0004 | 锚点机制：全局 ID + 结构化引用 + 分层约束 + 图校验 |
| 0005 | 模块化单体 + 多应用装配层 + 每应用契约组合 |
| 0006 | copy-on-derive，不承诺上游同步 |
| 0007 | 薄校验优先，生成器上限 6 个 |
| 0008 | SPA 而非 SSR（记录 SEO 代价） |
| 0009 | 模板默认 admin 端；user 端派生时按需新增 |

---

## 10. Skills 与 AI 能力沉淀

### 10.1 定位：skills 不保证一致性

一致性由编译、测试、CI 闸门保证；skills 只能降低 AI 走错路的概率。
不要写"提醒 AI 改契约"的 skill 当作机制——提醒不可靠，闸门才可靠。

### 10.2 跨工具原则：正文工具无关，适配层可生成

团队里 agent 不统一（OpenCode / Claude Code / Cursor / Codex / Gemini），各工具的 skill 路径不同。
解决办法不是选一个工具，而是分层：

| 层 | 位置 | 工具无关 | 作用 |
|---|---|---|---|
| 硬规则 | 根 `AGENTS.md` | 是（事实标准） | 不可协商的约束 |
| 流程正文 | `docs/playbooks/*.md` | 是 | 步骤、验证、完成定义 |
| 适配层 | `.opencode/skills/`、`.claude/skills/`、`.cursor/rules/` | 否 | 每个 10–20 行的薄入口 |
| 生成器 | `scripts/sync-agents.sh` | 是 | 从 playbook frontmatter 生成适配层（P7 实现） |

判断标准：**`AGENTS.md` 是底线，skills 是增强。** 即使成员不用任何 skill，靠 AGENTS.md + playbooks 也能完整工作。

### 10.3 单一源格式

playbook 顶部带 frontmatter：

```yaml
---
name: add-endpoint
description: 新增 API 端点的完整流程
targets: [opencode, claude, cursor]
---
```

`make agents` 生成各工具入口，内容只有四段：触发条件 → 必读 playbook 路径 → 必跑命令 → 完成定义。

生成物示例：

```markdown
<!-- .opencode/skills/add-endpoint/SKILL.md（生成物，禁止手改） -->
---
name: add-endpoint
description: 新增 API 端点的完整流程
---
1. 读 docs/playbooks/add-endpoint.md
2. 按其中步骤执行
3. 跑 make verify
```

### 10.4 三条纪律

1. **不用 symlink**：Windows checkout 需要开发者模式，跨团队必踩坑；用生成 + CI diff
2. **CI 校验生成物无 diff**：与契约生成同一套思路
3. **不承诺全工具支持**：只维护团队实际在用的 2–3 个，其余靠 `AGENTS.md` 兜底

### 10.5 建议的 skill 清单

| skill | 触发场景 | 随派生保留 |
|---|---|---|
| `contract-change` | 改 OpenAPI / 错误码 / 权限点 | 是 |
| `add-endpoint` | 新增 API 端点 | 是 |
| `add-table` | 新增表 / 迁移 | 是 |
| `add-app` | 新增 api / web 应用 | 是 |
| `verify` | 提交前跑全套闸门 | 是 |
| `template-derive` | 派生新项目 | 否，派生即卸 |
| `template-doctor` | 模板自身健康检查 | 否，派生即卸 |

派生脚本删除 `template-*`，其余保留。

### 10.6 反模式

- 用 skill 复制一份契约内容 → 必然漂移
- 用 skill 代替 CI 检查
- **把正文写在 `.opencode/` 里** → 换工具即失效
- 为每个 CRUD 端点写一个 skill → 爆炸

## 附录 A：契约与实现的一致性机制

> 目标：解决两类漂移——①契约改了实现没改；②实现改了契约没改。

### A.1 核心原则：单向生成 + 双向校验

**不要双向生成。** 两份真相源必然漂移。正确结构是：

```
契约 ──生成──> 类型/桩/客户端        （契约 → 实现，编译期约束）
实现 ──探测──> 与契约比对，不符即失败 （实现 → 契约，测试期发现）
```

生成解决"实现不知道契约变了"，探测解决"契约不知道实现变了"。

### A.2 八道闸门

| # | 闸门 | 检测的漂移 | 时机 | 成本 | 强度 |
|---|---|---|---|---|---|
| 0 | **锚点图校验**（`make contract-graph`） | 悬空引用、越层引用、重复 ID、孤儿契约 | CI | 低 | 100% |
| 1 | `make gen && git diff --exit-code` | 契约改了，生成物没更新 | CI | 极低 | 100% |
| 2 | **路由集合 == 契约集合**（Go 测试枚举 chi 路由树，与 OpenAPI `paths` 求对称差） | 新增/删除端点两边不同步 | 单测 | 低 | 100% |
| 3 | **响应 schema 校验**（`openapi3filter` 挂测试中间件，实际响应必须过契约） | 多返回字段、类型变、必填缺失 | 单测 | 中 | 高 |
| 4 | `oasdiff` 对比基线分支 | 破坏性变更未走版本/弃用流程 | CI | 极低 | 100% |
| 5 | **前端 client 覆盖断言**（契约 operation ↔ client 方法一一对应） | 前端漏接新端点 | 单测 | 低 | 100% |
| 6 | 禁止手改 `generated/`（文件头 + lint + 重新生成 diff） | AI 直接改生成物而非改契约 | CI | 低 | 100% |
| 7 | 禁止绕过 client 直连（`no-restricted-imports` 禁 `fetch`/`axios`） | 前端自造请求，逃逸类型检查 | 编译 | 低 | 100% |

> `make contract-check`（结构 / ID / `used_by` / `index` 路径）是闸门 0 的前置试点，已接入 pre-commit 与 CI；完整图校验仍以闸门 0 `make contract-graph` 为准。

**闸门 2 是性价比最高的一道**：一个测试函数即可覆盖"新增端点没写契约"和"契约加了端点没实现"两个方向。实现要点：

```
遍历 OpenAPI paths → {(method, path)}
遍历 chi 路由树 → {(method, path)}
对称差非空 → t.Fatal
```

**实测实现要点（已验证）**

- `api.GetSwagger()` 返回内嵌 spec，闸门直接用它，不读 YAML
- OpenAPI 侧用 `swagger.Paths.Map()` 取 `method + path`；实现侧用 `chi.Walk`
- 响应校验用 `routers/legacy.NewRouter(swagger).FindRoute(req)` + `openapi3filter.ValidateResponse`
- 负向用例必须进 CI：故意加未登记路由、故意返回注入字段，断言闸门变红
- kin-openapi v0.153 已移除 `T.FindRoute`，不要照抄旧示例

**闸门 3 依赖契约纪律**：响应 schema 必须 `additionalProperties: false`，否则多字段检测不到。

**不要用契约生成的 mock 做一致性测试**：mock 永远符合契约，测试永远通过，真实 handler 可能早已违背。
闸门 3 必须挂**真实 handler** + `httptest`；MSW mock 只用于开发期解耦，e2e 必须打真实后端。

### A.3 检测矩阵

| 漂移形态 | 谁会失败 |
|---|---|
| 契约加字段，实现没加 | 闸门 1（生成物）、编译（Go 结构体缺字段仅在使用时）、闸门 3 |
| 契约加端点，实现没加 | 闸门 1、**编译**（`ServerInterface` 未实现）、闸门 2 |
| 实现加端点，契约没加 | 闸门 2 |
| 实现多返回字段 | 闸门 3（需 `additionalProperties:false`） |
| 实现读契约外 query 参数 | 请求校验中间件 + arch-lint（禁止直接读 `r.URL.Query()`） |
| 前端用未定义字段 | **TS 编译失败**（类型来自契约） |
| 前端自己 fetch | 闸门 7 |
| 契约破坏兼容 | 闸门 4 |
| 改了 `generated/` 手写补丁 | 闸门 6 + 闸门 1 |
| 契约引用悬空或越层 | 闸门 0（图校验） |

### A.4 非 API 契约的一致性

| 契约 | 机制 |
|---|---|
| env schema | Go/TS 各写一个测试：代码注册的 key 集合 == schema key 集合 |
| 错误码 | 生成常量 + lint 禁止裸字符串错误码 |
| 权限点 | 生成常量 + lint 禁止裸字符串 |
| design tokens | 生成 CSS 变量 + stylelint 禁止裸色值 |
| 埋点事件 | 生成 TS 类型，未登记事件编译失败 |
| 页面契约 | 数据源必须指向 capability；四态缺一即校验失败 |
| 架构分层 | linter 本身 |
| 数据库 | 迁移由 `data/tables` 生成；空库重放后 dump 必须等于契约目标态 |
| 锚点关系 | 图校验：ID 唯一、引用可解析、分层合法、无孤儿 |

### A.5 流程约束（针对 AI）

技术闸门之外，必须有行为约束，写进根 `AGENTS.md`：

1. **改行为前先改契约**，禁止"先写代码后补契约"
2. **禁止手改 `generated/` 与 `migrations/`**，只允许 `make gen` 重建
3. **禁止在 handler 里直接读 `r.URL.Query()` / `r.Header.Get()`**，只能用生成的参数结构体
4. **禁止在页面契约里引用 `data.*`**，必须走 capability
5. **禁止凭记忆写契约 ID 或字段**，必须读 `index.yaml` 与目标文件
6. **新增依赖必须说明**"标准库/现有依赖为何不够"，记入 `docs/reference/dependencies.md`
7. **任何契约改动必须走 `contract-change` playbook**：改契约 → gen → 修编译 → 图校验 → 影响面报告
8. **PR 必须列出改动文件清单**，超出任务范围需说明

### A.6 落地优先级

先做三件，覆盖约 80% 漂移：

1. `make gen` + CI diff（1 小时）
2. 锚点图校验（半天）
3. 路由集合比对（半天）
4. 响应 schema 校验（半天）

再加：`oasdiff`（1 小时）、前端覆盖断言（2 小时）、两道 lint（各 1 小时）。

### A.7 已知盲区（诚实边界）

自动闸门无法覆盖：

- **语义漂移**：字段类型没变但含义变了（如 `status` 从"订单状态"变成"支付状态"）
- **契约本身的错误**：契约写错了，生成和校验都会一致地错
- **时序与并发**：契约不描述事务边界、竞态
- **性能退化**：响应变慢但形状不变
- **业务逻辑错误**：符合契约但算错了

这些只能靠单测、集成测试、review 和 ADR 约束。**不要指望闸门替代测试。**

### A.8 一句话总结

> 契约决定"能编译什么"，闸门决定"能合入什么"，流程决定"AI 会先做什么"。
> 三者缺一，一致性就只是口号。
