# 文档地图

> AI 进入本仓库的第一站。按顺序读，不要跳读、不要随机翻目录。

---

## 1. 阅读顺序

1. 根 `AGENTS.md` —— 不可协商的硬规则
2. 本文件 —— 地图与任务路由
3. `docs/guides/architecture.md` —— 整体设计与实施进度
4. `contracts/index.yaml` —— **契约地图**：只看摘要，不读正文
5. 写/改契约时：`contracts/_meta/README.md` + `_meta/<type>.yaml` —— 字段语义、生成行为、易错点
6. 任务对应的 `docs/playbooks/<task>.md`
7. playbook 指定的 2–5 个契约文件
8. 需要决策背景时读 `docs/adr/`

**读取预算**：单个任务最多读 5 个契约文件。超过说明任务粒度过大，先拆任务。

---

## 2. 任务路由

| 要做的事 | 先读 playbook | 再读契约 |
|---|---|---|
| 改任何契约 | `playbooks/contract-change.md` | `_meta/README.md` → `_meta/<type>.yaml` → `index.yaml` → 目标文件 |
| 新增 / 修改 API | `contract-change` + `add-endpoint`（P7） | `capabilities/http/*.yaml`、`project.yaml`（apps） |
| 新增表 / 迁移 | `contract-change` + `add-table`（P7） | `_meta/data.tables.yaml`、`_meta/data.tables.base.yaml`、`data/tables/_base.yaml`、`data/tables/*.yaml`、`data/storage.yaml` |
| 改存储引擎 / 部署拓扑 | `contract-change` | `data/storage.yaml` |
| 新增页面 | `contract-change` + `add-feature`（P7） | `features/pages/*.yaml`、`features/routes.yaml` |
| 新增 API / Web 应用 | `contract-change` + `add-app`（P7） | `project.yaml`（apps 段） |
| 派生新项目 | `rename-project`（P7） | `project.yaml` |

playbook 未覆盖的任务：**先写 playbook，再动手**。这是硬规则。

---

## 3. 冲突优先级

```
局部 AGENTS.md > 根 AGENTS.md > playbooks > ADR > guides
```

低优先级与高优先级冲突时，以高优先级为准，并修正低优先级文档。

---

## 4. 目录

| 路径 | 内容 | 何时读 |
|---|---|---|
| `docs/guides/architecture.md` | 架构、生成链、实施进度、一致性机制 | 每次任务开始 |
| `contracts/index.yaml` | 契约地图（摘要级） | 定位契约时 |
| `contracts/_meta/README.md` | 契约字段规范：`data.tables` / `data.enums`、`contract-new` / `contract-check` | 写 / 改契约前 |
| `contracts/_meta/<type>.yaml` | 该类型每个字段的语义、生成行为、AI 易错点 | 写 / 改该类型契约前 |
| `contracts/README.md` | 契约元规则：ID、分层、拆分、演进 | 改契约前 |
| `contracts/data/README.md` | 数据契约与迁移规则 | 加表 / 写查询前 |
| `docs/playbooks/` | 步骤化操作手册 | 执行具体任务时 |
| `docs/adr/` | 已定决策与否决方案 | 想改架构前 |
| `docs/reference/` | 命名、术语、依赖清单 | 查约定时 |

---

## 5. 硬规则速查

- 契约是唯一真相源；`migrations` / `queries` / 类型 / 路由均从 `contracts/` 生成
- 改行为前先改契约，禁止"先写代码后补契约"
- 新建契约用 `make contract-new`，改完跑 `make contract-check`；`make gen` 只负责契约 → 产物
- 禁止手改 `generated/`、`apps/api/migrations/`
- 禁止在 `features/**` 里引用 `data.*`，页面必须走 `capability`
- 契约引用写 ID，且只写在结构化字段里（`ref` / `refs` / `capability` / `use` / `emits` / `x-*`）
- 新增依赖必须说明"标准库/现有依赖为何不够"
- 任务超出范围时先停下确认，不要顺手重构
