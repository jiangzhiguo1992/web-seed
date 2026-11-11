# web-seed

通用 Web + API 脚手架 / 基础底座模板。**可复制派生**，用于快速新建项目；它不是业务项目，是模板种子工程。

后端 Go · 前端 Vue 3 · 单仓库多应用 · 契约优先。

> 当前进度：**P0 技术验证、P1 仓库骨架、契约层五组重构已完成**；P2 契约工具链（`_meta` + `contract-check`）试点完成，生成器进行中。
> 完整设计与实施清单见 [`docs/guides/architecture.md`](docs/guides/architecture.md)，AI 入口见 [`docs/index.md`](docs/index.md)。

---

## 1. 设计原则

1. **契约是一等公民**
   API、数据库、配置、错误码、权限点、设计令牌都有唯一真相源，代码从契约生成；实现必须通过探测证明自己符合契约。

2. **可执行规约 > 散文文档**
   分层规则交给架构 linter，API 形状交给 OpenAPI，表结构交给契约生成 migrations。文档只解释"为什么"。

3. **可摘除设计**
   可选模块在任何地方都不被引用，只通过一处显式注册表接入。删除 = 删目录 + 删一行注册。

4. **copy-on-derive**
   派生后项目独立演化，模板不承诺上游同步，只提供 CHANGELOG 供人工拣选。

### 四防机制

| 目标 | 机制 |
|---|---|
| 保真 | 类型由契约生成，不符合即编译失败 |
| 防漂移 | 生成物 diff + 路由集合比对 + 响应 schema 校验 |
| 防幻想 | 禁止手改 `generated/`、playbook 强制查证、依赖准入 |
| 防扩散 | 范围条款 + 一任务一 playbook + PR 改动清单 |

---

## 2. 目录总览

`[C]` 已落地 · `[P]` 规划中 · `[O]` 可选（派生时可删）

```
web-seed/
├── AGENTS.md                    [C] AI 宪法：优先级链、硬约束
├── README.md                    [C] 本文件
├── Makefile                     [C] 唯一任务入口，make help 自文档
├── package.json                 [C] 根脚本与 devDeps
├── pnpm-workspace.yaml          [C] 前端多应用 workspace
├── lefthook.yml                 [C] 提交/推送钩子
├── .editorconfig .gitattributes .nvmrc .dockerignore .gitignore
│
├── contracts/                   [C] 契约层：唯一真相源
│   ├── README.md                #   契约规范：原则、ID、分层、拆分、工作流
│   ├── index.yaml               #   契约地图（AI 唯一入口，只放摘要）
│   ├── _meta/                   #   契约工具链：schema / templates / contract-check
│   ├── project.yaml             #   项目元信息（derive 唯一输入）
│   ├── data/                    #   ① 数据：tables（真相源）/ entities / queries / _policy
│   ├── capabilities/            #   ② 实现层：http / async / services / flows
│   ├── business/                  #   ③ 业务规格：states / rules / events / glossary
│   ├── features/                #   ④ 功能：routes / pages / flows
│   ├── platform/                #   ⑤ 横切：环境/版本/错误/枚举/HTTP/质量/配置/架构/设计/观测/埋点/安全
│   └── generated/               #   产物索引：manifest / graph / lineage
│
├── docs/                        [C] 文档层（面向 AI + 人）
│   ├── index.md                 #   唯一地图，AI 先读
│   ├── guides/architecture.md   #   架构设计与实施进度
│   ├── adr/                     [P] 决策记录（含否决方案）
│   ├── playbooks/               [P] 步骤化操作手册
│   └── reference/               [P] 命名、术语、依赖清单
│
├── apps/
│   ├── api/                     [C] Go 后端（单 module，多 cmd，多应用装配）
│   │   ├── cmd/adminapi/         #   管理端 API 入口
│   │   ├── internal/
│   │   │   ├── platform/        #   基础设施：config、httpserver（P3 扩 database/observability/security）
│   │   │   ├── modules/         [P] 业务模块：service + repo，不依赖 HTTP
│   │   │   ├── composition/adminapi/ #   应用装配 + handlers/[P] 契约适配器
│   │   │   └── generated/       [P] 生成物（openapi / db / business / features / 常量）
│   │   ├── migrations/          [P]
│   │   ├── queries/             [P]
│   │   └── go.mod  Dockerfile
│   │
│   └── web/
│       └── admin/               [C] 管理端前端（模板默认端）
│           ├── src/app/         [P] router / providers / layouts
│           ├── src/pages/       [P] 路由级视图
│           ├── src/features/    [P] 用户动作
│           ├── src/shared/      [P] ui / api / lib
│           └── vite.config.ts  tsconfig.json  Dockerfile
│
├── packages/                    [C] 前端共享包
│   ├── api-client/              [C] 契约生成的客户端（P2 填充）
│   ├── ui-kit/                  [C] primitives（Button、cn、styles）
│   ├── design-tokens/           [C] tokens.json → theme.css 生成器与产物
│   ├── tsconfig/                [C] 共享 TS 配置
│   └── eslint-config/           [P] ESLint flat config（P7 接入）
│
├── infra/compose.yaml           [C] 本地依赖（PG，profiles: core）
├── scripts/                     [C] derive / doctor / check-docs / gen-env
├── .opencode/skills/            [P] AI 适配层（由 playbook 生成，P7）
└── .github/                     [C] CI（contract/api/web/e2e/docs/self）+ PR 模板
```

### 各层职责

| 层 | 职责 | 禁止 |
|---|---|---|
| `contracts/` | 唯一真相源 | 写实现细节、散文 |
| `docs/` | 解释为什么、怎么做 | 与契约冲突 |
| `apps/api/cmd` | 启动与关闭 | 业务逻辑 |
| `apps/api/internal/composition` | 选模块、挂路由、聚合 handler | 业务分支、直接访问 DB |
| `apps/api/internal/modules` | 业务能力 | 依赖 HTTP 契约、跨模块横向 import |
| `apps/api/internal/platform` | 基础设施 | 依赖业务 |
| `apps/web/*/src/app` | 应用装配 | 业务规则 |
| `packages/ui-kit` | 无业务的基础组件 | 业务组件、请求 |

---

## 3. 技术栈

| 层 | 选型 | 状态 |
|---|---|---|
| 后端语言 | Go 1.27 | 已用 |
| HTTP | chi + net/http | 已用 |
| 契约生成 | 6 个生成器：openapi / db / business / features / platform / graph | P2 |
| 接口生成 | oapi-codegen（`strict-server`）+ openapi-typescript | P2 |
| 数据访问 | pgx + sqlc | P2 |
| 迁移 | goose | P2 |
| 日志 | slog（JSON） | 已用 |
| 前端 | Vue 3.5 + Vite 7 + TypeScript 5.9 | 已用 |
| 样式 | Tailwind v4（CSS-first） | 已用 |
| 组件 | shadcn-vue（primitives 落在 `packages/ui-kit`） | P3 |
| 状态与数据 | Pinia + TanStack Query | P3 |
| 包管理 | pnpm 12 workspace | 已用 |
| 测试 | Go testing / Vitest / Playwright | P4/P7 |

---

## 4. 快速开始

前置：Go 1.27+、Node 26+、pnpm 12+、Docker（仅本地依赖需要）。

```bash
pnpm install          # 安装前端 workspace 依赖
make build            # 构建 Go + 前端（先构建 tokens，再构建 app）
make lint             # go vet + gofmt + vue-tsc
make doctor           # 仓库健康检查
```

启动：

```bash
make compose-up       # 启动 PG（可选）
make api-dev          # API: http://localhost:8080
make web-dev          # Web: http://localhost:5173
```

验证 API：

```bash
curl -i http://localhost:8080/healthz   # 200，不查依赖
curl -i http://localhost:8080/readyz    # 200，P3 起查 DB
```

构建产物：

```bash
make build-api        # apps/api 编译
make build-web        # apps/web/admin/dist
```

---

## 5. 常用命令

| 命令 | 说明 |
|---|---|
| `make help` | 列出全部目标 |
| `make build` | 构建 Go + 前端 |
| `make lint` | 静态检查 |
| `make test` | 运行测试（P1 仅 Go） |
| `make gen` | 重新生成全部契约产物（P2 实现，当前会失败） |
| `make gen-openapi` / `gen-db` / `gen-graph` / `gen-client` | 分项生成（P2） |
| `make contract-new type=data.tables name=note` | 从 `_meta/templates` 生成契约骨架 |
| `make contract-check` | 校验契约结构、ID、`used_by`（data.tables / data.enums 试点） |
| `make contract-graph` | 锚点图校验（P2） |
| `make contract-impact id=<id>` | 影响面分析（P2） |
| `make contract-compat` | 破坏性变更检测（P5） |
| `make db-verify` | 迁移与契约目标态比对（P5） |
| `make doctor` | 编译、格式、文档、生成物健康检查 |
| `make check-docs` | 校验文档关键路径与相对链接 |
| `make compose-up` / `down` | 本地 PG |
| `make derive` | 派生新项目（P7 实现，当前会失败） |

---

## 6. 契约工作流

目标流程（P2 起生效）：

```
1. 读 contracts/index.yaml 定位相关契约（只看摘要）
2. 写/改前读 contracts/_meta/README.md + _meta/schema/<type>.yaml
3. 新建契约：make contract-new ...（生成骨架，替换占位符）
4. 改契约（2-5 个文件）
5. make contract-check  # 结构 / ID / used_by
6. make gen             # 生成类型 / DDL / 常量 / 图
7. 修复编译错误（类型由契约生成）
8. 跑闸门：锚点图 / 路由比对 / 响应校验 / oasdiff
9. 同一个 PR 内提交：契约 + 生成物 + 实现 + 测试
```

硬规则：

- 契约是唯一真相源，**migrations / queries 由 `data/` 生成**，禁止手写
- 禁止先写实现后补契约
- 禁止手改 `generated/`
- 禁止绕过 `packages/api-client` 直连 `fetch`
- 禁止在 `features` 契约里直接引用 `data.*`（页面必须走 capability）
- CI 必须 `make gen && git diff --exit-code` + `make contract-check` + `make contract-graph`

当前已可用的闸门见 `docs/guides/architecture.md` 附录 A。

---

## 7. 派生新项目

`make derive` 尚未实现（P7）。当前手工步骤：

1. 复制仓库，删除 `.git` 后重新 `git init`
2. 修改 `contracts/project.yaml`（项目名 / module / scope / 端口）——**唯一输入**
3. 手工同步替换（`derive.sh` 落地前）：
   - Go module：`github.com/yourorg/web-seed/apps/api` → 你的 module
   - 前端 scope：`@webseed/*` → 你的 scope
4. 删除示例契约与实现：`data/tables/{account,token,auth_pwd,auth_phone,auth_email}.yaml`、`data/entities/{account,token,auth_pwd,auth_phone,auth_email}.yaml`、`data/queries/{account,auth}.yaml`、`capabilities/http/{account,auth}.yaml`、`capabilities/services/{account,auth}.yaml`、`capabilities/flows/account_activation.yaml`、`capabilities/async/account_cleanup.yaml`、`business/**/account*.yaml`、`features/{pages,flows}/account_*.yaml` 及对应代码
5. 删除 `.opencode/opencode.json`（含本地 MCP 配置）
6. 创建 `.env.local`，参考 `.env.example`（P2 生成）

---

## 8. 文档地图

| 文档 | 何时读 |
|---|---|
| [`docs/index.md`](docs/index.md) | AI 进入仓库的第一站 |
| [`docs/guides/architecture.md`](docs/guides/architecture.md) | 架构、契约体系、实施进度 |
| [`contracts/README.md`](contracts/README.md) | 改契约前 |
| [`contracts/data/README.md`](contracts/data/README.md) | 加表 / 写查询前 |
| [`docs/playbooks/contract-change.md`](docs/playbooks/contract-change.md) | 改任何契约前（**已就绪**） |
| `docs/playbooks/` 其余 | 新增端点 / 表 / 应用 / 页面（P7） |
| `docs/adr/` | 想改架构前（P7） |

文档与契约的分工：

| 维度 | `contracts/` | `docs/` |
|---|---|---|
| 内容 | schema、配置、规则常量 | why、流程、决策、术语 |
| 读者 | 工具链 + 人 | 人 + AI |
| 是否真相源 | 是 | 否，只引用 |

文档冲突时的优先级：

```
局部 AGENTS.md > 根 AGENTS.md > playbooks > ADR > guides
```

---

## 9. 明确不做

多租户 · Outbox · SSR/Nuxt · 微服务 · K8s manifests · CDN · 备份 · 告警平台 · 完整 i18n · 低代码 · GraphQL · gRPC · monorepo 编排工具（turbo/nx）。

---

## 10. 许可证

见 [`LICENSE`](LICENSE)。
