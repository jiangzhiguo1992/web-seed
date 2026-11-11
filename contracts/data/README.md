# 数据契约（data）

> **`data/tables/*.yaml` 是表结构的唯一真相源。**
> `apps/api/migrations/*.sql` 与 `apps/api/internal/generated/db/schema.sql` 都是生成物。
> 禁止手写迁移；改表先改契约，再 `make gen-db`。

---

## 1. 文件职责

| 文件 | 内容 | 产物 |
|---|---|---|
| `tables/<table>.yaml` | 物理表：字段、类型、约束、索引、注释 | `schema.sql`、增量 `migrations/*.sql`、sqlc 输入 |
| `entities/<entity>.yaml` | 逻辑数据实体：身份、字段语义、关系 | Go/TS 类型 |
| `enums.yaml` | 跨前后端枚举 | Go 常量、TS union、DB check 约束 |
| `queries/<module>.yaml` | 查询契约：名称、参数、返回、SQL、语义 | `queries/*.sql`、sqlc 代码 |
| `_policy.yaml` | **组级配置**（非实例契约）：禁止操作、人工调整边界、校验命令 | 约束生成器 |

## 2. 生成链

```
data/tables/*.yaml ──> schema.sql（目标态）
                          ├─ diff 当前 DB ──> migrations/<ts>_<name>.sql
                          └─> sqlc 输入 ──> Go 查询代码

data/enums.yaml ──────> Go 常量 + TS union + CHECK 约束
data/queries/*.yaml ──> queries/*.sql ──> sqlc 代码
```

**初始数据**不进独立文件：作为 `tables/<table>.yaml` 的 `initial_rows` 字段，
由 `platform/security/{permissions,roles}.yaml` 派生的落库任务执行。

## 2.1 表与实体是两层

| 概念 | 视角 | 位置 | 内容 |
|---|---|---|---|
| 表（table） | 持久化 | `data/tables/<table>.yaml` | 字段、类型、可空、默认值、CHECK/FK/UNIQUE、索引、`ddl_ext` |
| 实体（entity） | 业务 | `business/entities/<entity>.yaml` | 身份、字段到列的映射、业务不变量、生命周期、派生属性、关系 |

两者不是 1:1，所以不合并：

- **一张表不一定是一个实体**：`user_sessions`、`audit_logs`、关联表只有结构，没有业务不变量
- **一个实体可能跨多表**：拆分存储、扩展表、多态关联
- **约束层次不同**：`CHECK (char_length(title) > 0)` 是 DDL；「已软删的账号不可修改」是业务不变量，数据库表达不了

关联方式是 `ref`：实体字段通过 `column: data.table.accounts.col.title` 指回表列。
反向不引用（`data` 不知道 `business` 的存在），保证 `data → data/platform` 的分层不被打破。

## 3. 硬规则

1. **一个表一个文件**，不按字段拆分（表结构必须整体可读）。不变量、生命周期、派生字段也写在本文件，不另建实体文件。
2. **字段类型用契约内类型**：`text`、`uuid`、`timestamptz`、`integer`、`boolean`、`jsonb`、`data.enum.<name>`、`data.table.<t>.col.<c>`。
3. **时间统一 `timestamptz`**，应用层只用 UTC。
4. **软删表必须有 `deleted_at`**，唯一约束必须带 `where: "deleted_at IS NULL"`。
5. **枚举用 `text + check`**，不用 PostgreSQL enum（加值要 DDL 锁表）。
6. **索引必须写 `reason`**，否则视为无理由索引，CI 警告。
7. **`ddl_ext` 是逃生口**：仅用于契约无法结构化的 DDL（分区、表达式索引），必须标 `escape_hatch: true`；每表 ≤3 处，全局 ≤20 处。
8. **主键默认由应用层生成 UUIDv7**（`generated_by: application`），数据库不设 default；若使用扩展则显式写 `default`。

## 3.1 迁移禁止操作

以下操作生成器直接报错，不会产出迁移（机器可读部分见 `_policy.yaml`）：

| 禁止 | 替代做法 |
|---|---|
| `DROP COLUMN` | 先标 deprecated，停止写入一个版本后再删 |
| `RENAME COLUMN` | 加新列 → 双写 → 回填 → 切读 → 删旧列 |
| 给已有数据的表加 `NOT NULL` 且无默认值 | 加可空 → 回填 → 加约束 |
| 原地改列类型 | 新列 + 回填 + 切换 |
| 大表非 `CONCURRENTLY` 建索引 | `CREATE INDEX CONCURRENTLY`（需 `transaction: false`） |

零停机三步：加可空列 → 分批回填 → 加约束（`NOT VALID` 后 `VALIDATE`）。

生成器无法表达时允许人工调整，但必须标：

```sql
-- manual-adjust: CONCURRENTLY 无法在事务内执行
```

## 4. CI 校验

| 检查 | 手段 |
|---|---|
| 迁移与契约一致 | 空库 apply 全部迁移 → dump → 与 `schema.sql` 逐字段比对 |
| 契约改了但没生成迁移 | `make gen-db && git diff --exit-code` |
| 查询未登记 | 每个 `.sql` 必须能追溯到 `data/queries/*.yaml` 条目 |
| 枚举两端一致 | Go 常量集合 == TS union 集合 |
| 索引无理由 | `reason` 缺失即警告 |

## 5. 命名

- 表：小写复数，下划线（`accounts`、`user_sessions`）
- 索引：`idx_<table>_<cols>`；唯一：`uq_<table>_<cols>`
- 外键：`fk_<table>_<ref>`；检查：`ck_<table>_<rule>`

## 6. 诚实边界

生产在线 DDL（大表加索引需 `CONCURRENTLY`、数据回填分步）无法全自动。
契约约束**最终态**，迁移**过程**允许人工介入，机器规则见 `_policy.yaml`。
