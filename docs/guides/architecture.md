# web-seed 架构设计与实施方案

> **版本** v3 · **状态** 草案（待 Spike 验证） · **更新** 2026-11-11
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
| P0 | 技术验证（Spike） | 5 项假设全部有实测结论 | `[ ]` |
| P1 | 仓库骨架 | `make help` 可用、目录齐备 | `[ ]` |
| P2 | 契约生成链 | `make gen` 幂等、CI diff 通过 | `[ ]` |
| P3 | 后端地基 | platform 六件套可用 | `[ ]` |
| P4 | 最小纵切 | notes 全链路跑通（DB→API→页面） | `[ ]` |
| P5 | 一致性闸门 | 七道闸门全部生效 | `[ ]` |
| P6 | 可选模块 | auth/rbac/audit 可摘除地接入 | `[ ]` |
| P7 | 治理与文档 | derive/doctor/ADR/playbooks 齐备 | `[ ]` |

### 0.3 任务清单

#### P0 技术验证（落盘前必须完成）

- [ ] **S1** `oapi-codegen` + chi + 自定义中间件：验证统一错误拦截、`ServerInterface` 与装配层结合是否干净
- [ ] **S2** `sqlc` 动态过滤/排序的真实边界：确定"必须手写 pgx"的判据
- [ ] **S3** `go-arch-lint` 表达力：能否约束 `cmd → apps → modules → platform` 且禁止 modules 横向依赖
- [ ] **S4** pnpm workspace + Vite 多应用：HMR、类型解析、共享包引用
- [ ] **S5** Tailwind v4（CSS-first `@theme`）+ shadcn-vue + design tokens 三者融合，确认只有一套 CSS 变量

#### P1 仓库骨架

- [ ] 根文件：`Makefile` `lefthook.yml` `.editorconfig` `.gitattributes` `.nvmrc` `pnpm-workspace.yaml` `package.json`
- [ ] `docs/{index.md,adr/,playbooks/,guides/,reference/}`
- [ ] `contracts/` 九类目录 + `README.md`（变更三问）
- [ ] `apps/api` 骨架（`cmd/` `internal/{platform,modules,apps,gen}` `migrations/` `queries/`）
- [ ] `apps/web/user` 骨架（`src/{app,pages,features,shared}`）
- [ ] `packages/{api-client,ui,design-tokens,tsconfig,eslint-config}`
- [ ] `infra/compose.yaml`（profiles: core/data/mail/object/observability）
- [ ] `scripts/{derive,doctor,check-docs,gen-env}.sh` 空壳 + `make help`

#### P2 契约生成链

- [ ] `contracts/openapi/user.yaml` → Go server 类型
- [ ] 同上 → `packages/api-client` TS 类型（`openapi-typescript` + `openapi-fetch`）
- [ ] `contracts/config/env.schema.json` → `.env.example`（`gen-env.sh`）
- [ ] `contracts/domain/error-codes.yaml` → Go 常量 + TS union
- [ ] `contracts/domain/enums.yaml` → 同上（只收跨端枚举）
- [ ] `contracts/design/tokens.json` → CSS variables（`@theme`）
- [ ] `contracts/security/permissions.yaml` → Go/TS 常量
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

#### P4 最小纵切（示例模块 notes）

- [ ] 迁移：`notes` 表（含审计列、软删、乐观锁、部分唯一索引示例）
- [ ] `queries/notes.sql` + sqlc 生成
- [ ] `internal/modules/notes`（handler/service/repo 三段）
- [ ] `contracts/openapi/user.yaml` 补 notes 端点
- [ ] `internal/apps/userapi` 装配 + `routes_test.go`
- [ ] `apps/web/user`：列表/详情/创建三态页面
- [ ] Playwright 跑通一条关键路径

#### P5 一致性闸门（详见附录 A）

- [ ] 闸门 1：`make gen && git diff --exit-code`
- [ ] 闸门 2：路由集合 == 契约集合（Go 测试）
- [ ] 闸门 3：响应 schema 校验（`openapi3filter`）
- [ ] 闸门 4：`oasdiff` breaking change 检测
- [ ] 闸门 5：前端 client 方法覆盖断言
- [ ] 闸门 6：禁止手改 `gen/`（文件头 + lint）
- [ ] 闸门 7：禁止绕过 client 直连（`no-restricted-imports`）

#### P6 可选模块（默认非自动生成）

- [ ] `auth`：注册/登录/刷新/登出/会话吊销（audience = user | admin）
- [ ] `rbac`：角色/权限点 + `usePermission()`
- [ ] `audit`：who/what/when/before/after
- [ ] `user`：资料与偏好（与 auth 分离）
- [ ] `upload`：S3 接口抽象 + 预签名（默认不装）
- [ ] `notification`：站内信 + 邮件（默认不装）
- [ ] `webhook`：收/发/签名/重试（默认不装）

#### P7 治理与文档

- [ ] `scripts/doctor.sh`：生成物漂移、文档路径失效、孤儿模块、依赖漏洞
- [ ] `scripts/derive.sh`：重命名 + 清示例 + 重置 git + 生成 `docs/DERIVED.md`
- [ ] `scripts/check-docs.sh`：文档内路径/链接有效性
- [ ] CI：api / web / e2e / docs / self 五路并行，目标 < 5 分钟
- [ ] ADR 六篇（见第 9 节）
- [ ] playbooks：`add-app` `add-endpoint` `add-table` `add-feature` `rename-project`
- [ ] `AGENTS.md`（根 + `apps/api` + `apps/web`）

---

## 1. 定位与设计纲领

### 1.1 定位

通用脚手架 / 基础底座模板，**可复制派生**，用于快速新建 Web + API 项目。不是业务项目，是模板种子工程。

派生关系是 **copy-on-derive**：派生后独立演化，模板不承诺上游回灌，只提供 `CHANGELOG` 供人工拣选。

### 1.2 三条纲领

1. **可执行规约 > 散文文档**
   每条约束尽量落到机器可校验处：分层 → 架构 linter；API 形状 → 生成；表结构 → migrations；配置 → schema。

2. **可摘除设计（removable by construction）**
   可选能力在任何地方都不被引用，只通过一处显式注册表接入。删除 = 删目录 + 删一行注册。

3. **薄校验优先于重生成**
   能用测试/脚本比对的，不写代码生成器。只有四类保留生成：OpenAPI 类型、sqlc、错误码/权限常量、CSS 变量。

### 1.3 四防目标

| 目标 | 典型失效 | 机制 |
|---|---|---|
| 保真 | AI 凭记忆实现，与契约不符 | 类型由契约生成，不符合即编译失败 |
| 防漂移 | 文档/生成物/实现三者不一致 | 生成物 diff + 文档路径校验 + 路由比对 |
| 防幻想 | 编造字段/函数/已实现功能 | 禁止手改 `gen/`、playbook 强制 `grep` 现状、依赖准入 |
| 防扩散 | 顺手重构、越界改文件 | 范围条款 + 一任务一 playbook + PR 改动清单 |

**硬规则**：任何 AI 会话要做的任务，必须存在对应 playbook；没有就先写 playbook 再动手。

---

## 2. 契约体系（九类）

契约是唯一真相源。每类必须具备：**有校验、有 playbook、有删除路径**，三者缺一即会腐烂。

| # | 类型 | 真相源 | 生成/校验 | 防的是 |
|---|---|---|---|---|
| 1 | 接口 | `contracts/openapi/{user,admin}.yaml` + `shared/` | Go server 桩 + TS client | 前后端各说各话 |
| 2 | 数据 | `migrations/` + `queries/` | sqlc；迁移可重放 | 编字段、编表 |
| 3 | 配置 | `contracts/config/env.schema.json` | 生成 `.env.example`；两端测试断言 key 一致 | 少配变量到生产才炸 |
| 4 | 领域 | `contracts/domain/{error-codes,enums}.yaml` | Go 常量 + TS union + 文档 | 错误码/状态值漂移 |
| 5 | 设计 | `contracts/design/tokens.json` | CSS variables → Tailwind `@theme` | user/admin 两套色值 |
| 6 | 架构 | `contracts/architecture/{go-arch.yaml,web-deps.json}` | go-arch-lint / dependency-cruiser | 跨层调用 |
| 7 | HTTP | `contracts/http/envelope.yaml` | 错误信封/分页/headers/状态映射 | 响应形状不一 |
| 8 | 观测 | `contracts/observability/{log-fields,metrics}.yaml` | 字段/指标常量 + 脱敏规则 | 多应用日志无法聚合 |
| 9 | 埋点 | `contracts/analytics/events.yaml` | TS 事件类型 + PII 剔除 | 事件名漂移、PII 外泄 |

附加两份：

- `contracts/security/permissions.yaml`：权限点 → Go/TS 常量 + `usePermission()`。权限是最典型的跨端契约，不定则三处各写一套字符串。
- `contracts/events/`：仅有 README，定义未来异步事件的形状，不实现。

### 2.1 契约的粒度纪律

- OpenAPI 只描述**外部**形状，内部 DTO 不进
- 禁止 `oneOf` 与深层 `allOf`；spec 保持平铺
- 响应 schema 默认 `additionalProperties: false`（否则"多返回字段"无法被检测）
- 错误码总量 ≤ 50，分层：`COMMON_*` / `<DOMAIN>_*`
- 只收**跨前后端**的枚举，模块内部枚举不进契约

### 2.2 变更流程（契约三问）

改任何契约前必须回答：

1. 是否破坏兼容？（是 → 升版本或走弃用流程）
2. 是否需要新版本？（URL major：`/v1`）
3. 是否需要数据迁移？（是 → 先写迁移，再改契约）

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
├── contracts/                        [C] 九类契约
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
│   │   ├── cmd/{userapi,adminapi[O]}/main.go
│   │   ├── internal/
│   │   │   ├── platform/{config,httpserver,database,observability,security,storage}
│   │   │   ├── modules/{health,auth[O],rbac[O],audit[O],user[O],upload[O],notification[O],webhook[O],notes[E]}
│   │   │   │   └── registry.go       # 唯一注册表
│   │   │   ├── apps/{userapi,adminapi[O]}/  # app.go / routes.go / routes_test.go
│   │   │   └── gen/                  # 生成物入库
│   │   ├── migrations/  queries/  seeds/  sqlc.yaml  codegen/  Dockerfile
│   │   └── ...
│   └── web/
│       ├── user/                     [C]
│       │   ├── AGENTS.md  vite.config.ts  Dockerfile
│       │   └── src/{app,pages,features,shared}
│       └── admin/                    [O] 同构
│
├── packages/{api-client,ui,design-tokens,tsconfig,eslint-config}
├── infra/compose.yaml                [O] profiles: core/data/mail/object/observability
├── scripts/{derive,doctor,check-docs,gen-env}.sh
└── .github/{workflows/{ci,template-selftest}.yml, PULL_REQUEST_TEMPLATE.md, ISSUE_TEMPLATE/}
```

---

## 4. 多应用架构

### 4.1 三层职责

```
cmd/<app>/main.go          只做启动：解析配置 → 构造 → 运行 → 关闭
internal/apps/<app>/       装配：选模块、挂路由、配中间件、定鉴权策略
internal/modules/<biz>/    业务能力，应用无关，自包含 handler+service+repo
internal/platform/         与业务无关的基础设施
```

### 4.2 硬边界（机器校验）

1. `cmd/*` 只能 import `internal/apps/*`，不许直接碰 `modules`
2. `modules/*` 之间禁止横向 import，跨模块走 service 接口或事件
3. `apps/*` 只允许"选择 + 组装 + 配置"，出现 `if/for` 业务分支即违规
4. `apps/web/*` 之间禁止互相 import，共享只能走 `packages/*`

### 4.3 装配方式

- 模块构造全显式手写，禁止 `init()` 自注册
- 禁止 wire / fx（此规模属过度）
- 加一个 API 应用 = 契约 + `internal/apps/newapp/` + `cmd/newapp/`，业务模块零改动

### 4.4 多应用待决问题

| 问题 | 影响 | 倾向 |
|---|---|---|
| user/admin 登录态是否共享 | Cookie 域策略、CSRF 方案 | 默认不共享（不同域，各自登录） |
| admin 是否与 user 共库 | 迁移归属、只读账号 | 默认共库，模块自治表 |
| SPA 的 SEO 取舍 | 是否留 Nuxt 迁移路径 | 默认 SPA，ADR 记录代价 |

---

## 5. 后端设计

### 5.1 技术默认值

| 关注点 | 默认 | 备选 | 理由 |
|---|---|---|---|
| HTTP | `chi` | stdlib / gin | 零反射、贴近标准库，可退化 |
| 数据 | `pgx` + `sqlc` | GORM | 编译期类型安全，SQL 可见 |
| 迁移 | `goose` | atlas | 纯 SQL、可 embed、只前进 |
| 契约生成 | `oapi-codegen` | swaggo | design-first 防漂移 |
| 日志 | `slog` | zap | 标准库 |
| 追踪 | OTel SDK（`exporter=none`） | — | 能力预留，不默认外发 |

### 5.2 错误处理

- HTTP 层用 RFC 9457 `application/problem+json`：`{type,title,status,detail,instance,code,errors[]}`
- 业务错误码与 HTTP 状态解耦，映射表在 `error-codes.yaml`
- 不返回的 error 必须记录（含栈 + 上下文）

### 5.3 事务边界

- `database.WithTx(ctx, fn)`，repo 从 ctx 取 querier
- 禁止把 `*pgx.Tx` 当参数层层传递
- 事务只在 service 层开启，handler 不开事务

### 5.4 数据规范（`contracts/db/conventions.md`）

- 主键 UUIDv7；时间列 `timestamptz`，统一 UTC
- 业务表标准列：`created_at / updated_at / created_by / deleted_at / version`
- 枚举用 `text + check`，不用 PG enum（迁移困难）
- 软删的唯一约束必须用部分索引：`WHERE deleted_at IS NULL`
- 索引命名 `idx_<table>_<cols>`；外键默认加，热点表可评估去掉
- 迁移只前进；零停机三步：加可空列 → 回填 → 加约束
- 软删默认过滤，必须在 repo 层统一，禁止每处手写

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
    ├── ui/          primitives + 三态组件（loading/empty/error）
    ├── api/         api-client 封装、拦截器、错误码→文案映射
    ├── lib/         datetime / money / id / storage / analytics
    ├── composables/ usePermission / useQueryState
    ├── config/      运行时配置（zod 校验）
    └── locales/     文案集中管理（预留 i18n）
```

### 6.2 约定

- 类型只能来自 `packages/api-client`，禁止手写 API 类型
- 禁止绕过 client 直连 `fetch`
- 时间统一 UTC 传输、本地展示；金额整数分 + 币种
- 三态（loading/empty/error）使用共享组件，禁止各页自造
- 运行时配置由 `public/config.js` 注入，前端启动时 zod 校验 → 同一镜像多环境
- 主题走 CSS 变量，暗色模式为 token 的第二组值

### 6.3 埋点

| 层 | 载体 | 默认 |
|---|---|---|
| 前端行为 | `analytics/events.yaml` → TS 类型；`track()` provider 可插拔 | noop |
| 后端指标 | `observability/metrics.yaml` | 注册不导出 |
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
| 前端 lint | ESLint + Prettier | 共享 `packages/eslint-config` |
| 架构校验 | `go-arch-lint` / `dependency-cruiser` | |
| 契约兼容 | `oasdiff` | |
| 测试 | testify / Vitest / Playwright | |

### 7.2 CI

五路并行，目标 < 5 分钟：

```
api:    lint → arch-lint → test → build → gen-diff
web:    lint → typecheck → test → build → gen-diff
e2e:    compose up → playwright
docs:   check-docs
self:   doctor（模板自身健康）
```

### 7.3 模板治理

- `doctor.sh`：生成物漂移、文档路径失效、孤儿模块、依赖漏洞
- `derive.sh`：交互式重命名、清示例、重置 git、写入 `docs/DERIVED.md`
- 模板自带 `template-selftest.yml`，防止模板本身腐烂

### 7.4 版本锁定

`go.mod` 的 go 指令 + `package.json` 的 `packageManager` + `.nvmrc`，零新工具。

---

## 8. 明确不做

多租户 · Outbox · SSR/Nuxt · 微服务 · K8s manifests · CDN · 备份 · 告警平台 · 完整 i18n · 低代码 · turbo/nx · GraphQL · gRPC。

**"完整"的定义**：不是功能全量，而是所有横切面都有明确归属——做 / 不做 / 留口，没有"没人管"的地带。

---

## 9. 决策记录（ADR 索引，待补）

| 编号 | 决策 |
|---|---|
| 0001 | 采用 ADR 记录架构决策 |
| 0002 | OpenAPI 作为接口唯一真相源（design-first） |
| 0003 | 模块化单体 + 多应用装配层 |
| 0004 | copy-on-derive，不承诺上游同步 |
| 0005 | 薄校验优先于重生成 |
| 0006 | SPA 而非 SSR（记录 SEO 代价） |

---

## 附录 A：契约与实现的一致性机制

> 目标：解决两类漂移——①契约改了实现没改；②实现改了契约没改。

### A.1 核心原则：单向生成 + 双向校验

**不要双向生成。** 两份真相源必然漂移。正确结构是：

```
契约 ──生成──> 类型/桩/客户端        （契约 → 实现，编译期约束）
实现 ──探测──> 与契约比对，不符即失败 （实现 → 契约，测试期发现）
```

生成解决"实现不知道契约变了"，探测解决"契约不知道实现变了"。

### A.2 七道闸门

| # | 闸门 | 检测的漂移 | 时机 | 成本 | 强度 |
|---|---|---|---|---|---|
| 1 | `make gen && git diff --exit-code` | 契约改了，生成物没更新 | CI | 极低 | 100% |
| 2 | **路由集合 == 契约集合**（Go 测试枚举 chi 路由树，与 OpenAPI `paths` 求对称差） | 新增/删除端点两边不同步 | 单测 | 低 | 100% |
| 3 | **响应 schema 校验**（`openapi3filter` 挂测试中间件，实际响应必须过契约） | 多返回字段、类型变、必填缺失 | 单测 | 中 | 高 |
| 4 | `oasdiff` 对比基线分支 | 破坏性变更未走版本/弃用流程 | CI | 极低 | 100% |
| 5 | **前端 client 覆盖断言**（契约 operation ↔ client 方法一一对应） | 前端漏接新端点 | 单测 | 低 | 100% |
| 6 | 禁止手改 `gen/`（文件头 + lint + 重新生成 diff） | AI 直接改生成物而非改契约 | CI | 低 | 100% |
| 7 | 禁止绕过 client 直连（`no-restricted-imports` 禁 `fetch`/`axios`） | 前端自造请求，逃逸类型检查 | 编译 | 低 | 100% |

**闸门 2 是性价比最高的一道**：一个测试函数即可覆盖"新增端点没写契约"和"契约加了端点没实现"两个方向。实现要点：

```
遍历 OpenAPI paths → {(method, path)}
遍历 chi.RouteTree（或注册时自收集）→ {(method, path)}
对称差非空 → t.Fatal
```

**闸门 3 依赖契约纪律**：响应 schema 必须 `additionalProperties: false`，否则多字段检测不到。

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
| 改了 `gen/` 手写补丁 | 闸门 6 + 闸门 1 |

### A.4 非 API 契约的一致性

| 契约 | 机制 |
|---|---|
| env schema | Go/TS 各写一个测试：代码注册的 key 集合 == schema key 集合 |
| 错误码 | 生成常量 + lint 禁止裸字符串错误码 |
| 权限点 | 生成常量 + lint 禁止裸字符串 |
| design tokens | 生成 CSS 变量 + stylelint 禁止裸色值 |
| 埋点事件 | 生成 TS 类型，未登记事件编译失败 |
| 架构分层 | linter 本身 |
| 数据库 | 迁移可重放 + sqlc 与 schema 同源 |

### A.5 流程约束（针对 AI）

技术闸门之外，必须有行为约束，写进根 `AGENTS.md`：

1. **改 API 行为前先改契约**，禁止"先写代码后补契约"
2. **禁止手改 `gen/`**，只允许 `make gen` 重建
3. **禁止在 handler 里直接读 `r.URL.Query()` / `r.Header.Get()`**，只能用生成的参数结构体
4. **新增依赖必须说明**"标准库/现有依赖为何不够"，记入 `docs/reference/dependencies.md`
5. **任何契约改动必须走 `add-endpoint` playbook**，含"改契约 → gen → 修编译 → 跑闸门"四步
6. **PR 必须列出改动文件清单**，超出任务范围需说明

### A.6 落地优先级

先做三件，覆盖约 80% 漂移：

1. `make gen` + CI diff（1 小时）
2. 路由集合比对（半天）
3. 响应 schema 校验（半天）

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
