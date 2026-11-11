# 数据契约（data）

> **`data/tables/*.yaml` 是表结构的唯一真相源。**
> `apps/api/migrations/*.sql` 与 `apps/api/internal/generated/db/schema.sql` 都是生成物。
> 禁止手写迁移；改表先改契约，再 `make gen-db`。

---

## 1. 文件职责

| 文件 | 内容 | 产物 |
|---|---|---|
| `tables/<table>.yaml` | 目标态：字段、类型、约束、索引、注释、生命周期 | `schema.sql`、增量 `migrations/*.sql`、sqlc 输入、Go 类型 |
| `enums.yaml` | 跨前后端枚举 | Go 常量、TS union、DB check 约束 |
| `queries/<module>.yaml` | 查询契约：名称、参数、返回、SQL、语义 | `queries/*.sql`、sqlc 代码 |
| `seeds/<name>.yaml` | 系统必需数据（角色、权限等），必须幂等 | seed SQL / 启动任务 |
| `migrations-policy.yaml` | 迁移策略：禁止操作、零停机步骤、允许的人工调整 | 约束生成器 |

## 2. 生成链

```
data/tables/*.yaml ──> schema.sql（目标态）
                          ├─ diff 当前 DB ──> migrations/<ts>_<name>.sql
                          └─> sqlc 输入 ──> Go 查询代码

data/enums.yaml ──────> Go 常量 + TS union + CHECK 约束
data/queries/*.yaml ──> queries/*.sql ──> sqlc 代码
data/seeds/*.yaml ────> seed SQL（幂等 UPSERT）
```

## 3. 硬规则

1. **一个表一个文件**，不按字段拆分（表结构必须整体可读）。
2. **字段类型用契约内类型**：`text`、`uuid`、`timestamptz`、`integer`、`boolean`、`jsonb`、`data.enum.<name>`、`data.table.<t>.col.<c>`。
3. **时间统一 `timestamptz`**，应用层只用 UTC。
4. **软删表必须有 `deleted_at`**，唯一约束必须带 `where: "deleted_at IS NULL"`。
5. **枚举用 `text + check`**，不用 PostgreSQL enum（加值要 DDL 锁表）。
6. **索引必须写 `reason`**，否则视为无理由索引，CI 警告。
7. **`ddl_ext` 是逃生口**：仅用于契约无法结构化的 DDL（分区、表达式索引），必须标 `escape_hatch: true`；每表 ≤3 处，全局 ≤20 处。
8. **主键默认由应用层生成 UUIDv7**（`generated_by: application`），数据库不设 default；若使用扩展则显式写 `default`。

## 4. CI 校验

| 检查 | 手段 |
|---|---|
| 迁移与契约一致 | 空库 apply 全部迁移 → dump → 与 `schema.sql` 逐字段比对 |
| 契约改了但没生成迁移 | `make gen-db && git diff --exit-code` |
| 查询未登记 | 每个 `.sql` 必须能追溯到 `data/queries/*.yaml` 条目 |
| 枚举两端一致 | Go 常量集合 == TS union 集合 |
| 索引无理由 | `reason` 缺失即警告 |

## 5. 命名

- 表：小写复数，下划线（`notes`、`user_sessions`）
- 索引：`idx_<table>_<cols>`；唯一：`uq_<table>_<cols>`
- 外键：`fk_<table>_<ref>`；检查：`ck_<table>_<rule>`

## 6. 诚实边界

生产在线 DDL（大表加索引需 `CONCURRENTLY`、数据回填分步）无法全自动。
契约约束**最终态**，迁移**过程**允许人工介入，规则见 `migrations-policy.yaml`。
