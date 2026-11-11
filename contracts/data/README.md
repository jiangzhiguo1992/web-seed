# 数据契约（data）

> **`data/tables/*.yaml` 是表结构的唯一真相源。**
> `apps/api/migrations/*.sql` 与 `apps/api/internal/generated/db/schema.sql` 都是生成物。
> 禁止手写迁移；改表先改契约，再 `make gen-db`。
> 字段语义与生成行为见 `contracts/_meta/data.tables.yaml` / `data.enums.yaml`；新建表用 `make contract-new`，改完跑 `make contract-check`。

---

## 1. 文件职责

| 文件 | 内容 | 产物 |
|---|---|---|
| `tables/_base.yaml` | 表结构基类：`id/status/created_at/updated_at/deleted_at` | 无（不生成 DDL、不建实体、不落存储） |
| `tables/<file>.yaml` | 物理表：字段、类型、约束、索引、分区、保留期 | `schema.sql`、增量 `migrations/*.sql`、sqlc 输入 |
| `entities/<entity>.yaml` | 逻辑数据实体：身份、字段、关系、复杂校验 | Go/TS 类型 |
| `enums.yaml` | 跨前后端枚举集合 | Go 常量、TS union、DB CHECK |
| `queries/<module>.yaml` | 查询契约：id、参数、返回、SQL、语义 | `queries/*.sql`、sqlc 代码 |
| `storage.yaml` | 项目级存储引擎、部署拓扑、连接与锁 | 配置校验、运维约束 |
| `_policy.yaml` | **组级配置**（非实例契约）：命名、继承、禁止操作、校验命令 | 约束生成器 |

### 1.1 基类不是物理表

`tables/_base.yaml` 只收敛 5 个字段：

- `id`：UUIDv7，应用生成，主键
- `status`：业务状态；物理表必须覆盖为具体枚举
- `created_at`：数据库默认 `now()`
- `updated_at`：应用写入
- `deleted_at`：软删时间；为空表示未删除

物理表必须声明 `extends: [data.tables.base]`，不得重复声明这 5 列；只允许覆盖 `type/default/default_sql/comment/max_len`。

### 1.2 表与实体是两层

| 概念 | 视角 | 位置 | 内容 |
|---|---|---|---|
| 表（table） | 持久化 | `data/tables/<file>.yaml` | 字段、类型、可空、默认值、CHECK/FK/UNIQUE、索引、分区、`used_by` |
| 实体（entity） | 业务 | `data/entities/<entity>.yaml` | 身份、字段、关系、`validation`（数据库表达不了的复杂校验） |

两者不是 1:1，所以不合并：

- **一张表不一定是一个实体**：`tokens`、`audit_logs`、关联表只有结构，没有业务不变量
- **一个实体可能跨多表**：账号与 `auth_*` / `tokens` 是聚合关系
- **约束层次不同**：`CHECK (char_length(number) > 0)` 是 DDL；「已软删的账号不可修改」是业务不变量

关联方式是 `ref`：实体通过 `table: data.tables.account` 指回表，字段通过 `column:` 指回列（同名可省略）。

### 1.3 约束与校验的分界

| 类型 | 写在哪 | 例子 |
|---|---|---|
| 数据库能表达的约束 | `tables/*.yaml` 的 `constraints` | `CHECK`、`FOREIGN KEY`、`UNIQUE`、枚举 `CHECK` |
| 跨字段/跨表/业务不变量 | `entities/*.yaml` 的 `validation` | 邮箱拆分一致性、令牌过期顺序 |
| 业务行为规则 | `business/rules/*.yaml` | 编号唯一、仅本人可改 |

**不要把 DDL 搬到 entities，也不要把业务规则塞进 tables。**

---

## 2. 生成链

```
data/tables/_base.yaml + tables/*.yaml ──> schema.sql（目标态）
                                             ├─ diff 当前 DB ──> migrations/<ts>_<name>.sql
                                             └─> sqlc 输入 ──> Go 查询代码

data/enums.yaml ──────> Go 常量 + TS union + CHECK 约束
data/queries/*.yaml ──> queries/*.sql ──> sqlc 代码
data/storage.yaml ────> 配置校验 + 运维约束
```

**初始数据**：仅当表确实需要静态种子时，在 `tables/<file>.yaml` 写 `initial_rows`；当前示例没有种子数据。RBAC 的 seed 由可选 `rbac` 模块负责，不在平台契约里承诺。

---

## 3. 硬规则

1. **一表一文件**，不按字段拆分；基类列不重复声明。
2. **每个可寻址项必须有稳定 ID**：表 `data.tables.<file>`、列 `data.tables.<file>.col.<col>`、约束 `data.tables.<file>.constraint.<constraint.name>`、查询 `data.queries.<module>.<query_id>`、枚举 `data.enums.<name>`。查询/枚举必须显式写 `id`；列/约束用 `name` 派生，不得改名。契约文件名用**单数**（`tables/account.yaml` → `data.tables.account`）；物理表名默认等于文件段，复数表用 `table.name` 覆盖（`account.yaml` 里 `table.name: accounts`）。
3. **字段类型用契约内逻辑类型**：`string`、`boolean`、`integer`、`float`、`decimal`、`datetime`、`timestamp`、`json`、`bytes`、`data.enums.<name>`、`data.tables.<file>.col.<col>`。
4. **类型族扩展字段写在列上**：`string`/`bytes` 用 `min_len`/`max_len`/`fixed`，`integer` 用 `bits`/`unsigned`，`float` 用 `bits`，`decimal` 用 `precision`/`scale`，`datetime` 用 `mode`/`timezone`，`timestamp` 用 `timezone`/`precision`，`json` 用 `binary`。字段定义与 `required` 在 `contracts/_meta/data.tables.yaml` 的 `column_types.value_type_families`。
5. **时间统一 `timestamp`**，应用层只用 UTC。
6. **软删表必须有 `deleted_at`**，唯一约束必须带 `where: "deleted_at IS NULL"`。
7. **枚举用 `string + check`**，不用 PostgreSQL enum；枚举值只能追加。
8. **索引必须写 `reason`**；表达式索引用 `expression`，大表用 `concurrently: auto`。
9. **主键默认由应用层生成 UUIDv7**（`generated_by: application`），数据库不设 default。
10. **`owner_module` 是写归属**：只有该模块的 `cap.services` / `cap.async` 可以写表，其他模块只能经 service 读。
11. **禁止未结构化 DDL 字段**：分区、表达式索引、CONCURRENTLY 必须用结构化字段表达（`table.partition`、`indexes[].expression`、`indexes[].concurrently`），不要塞原生 SQL。
12. **`used_by` 是生成物**：由 `make gen-graph` 写入，禁止手改；结构固定为 `entities` / `queries` / `capabilities` 三个列表，只记**直接入边**。完整影响面（页面/端点/流程）用 `make contract-impact id=<id>` 或 `generated/lineage.md` 查询。

---

## 3.1 迁移禁止操作

以下操作生成器直接报错，不会产出迁移（机器可读部分见 `_policy.yaml`）：

| 禁止 | 替代做法 |
|---|---|
| `DROP COLUMN` | 先标 deprecated，停止写入一个版本后再删 |
| `RENAME COLUMN` | 加新列 → 双写 → 回填 → 切读 → 删旧列 |
| 给已有数据的表加 `NOT NULL` 且无默认值 | 加可空 → 回填 → 加约束 |
| 原地改列类型 | 新列 + 回填 + 切换 |
| `size_class=large` 的表非 `CONCURRENTLY` 建索引 | `CREATE INDEX CONCURRENTLY`（`transaction: false`） |

零停机三步：加可空列 → 分批回填 → 加约束（`NOT VALID` 后 `VALIDATE`）。

### 3.2 结构化 DDL（替代未结构化字段）

契约不提供 `ddl_ext` 这类逃生口；SQL 特性用结构化字段表达：

```yaml
table:
  partition: { strategy: range, columns: [created_at], interval: "1 month" }

indexes:
  - name: idx_accounts_email_lower
    expression: "lower(email)"
    method: btree
    concurrently: auto        # auto 由 size_class 决定
    reason: 邮箱不区分大小写查找
```

- `table.partition`：`strategy` 为 `range|list|hash`；`columns` 必须是表列 ID 的末段
- `indexes[].expression`：表达式索引；必须写 `reason`
- `indexes[].method`：`btree|hash|gin|gist|brin`，默认 `btree`
- `indexes[].concurrently`：`auto|true|false`；`size_class=large` 时默认 `auto → true`
- 生成器无法表达的新特性：先扩展本契约的字段，不要手写迁移

生成器无法表达时允许人工调整，但必须标：

```sql
-- manual-adjust: CONCURRENTLY 无法在事务内执行
```

---

## 4. CI 校验

| 检查 | 手段 |
|---|---|
| 迁移与契约一致 | 空库 apply 全部迁移 → dump → 与 `schema.sql` 逐字段比对 |
| 契约改了但没生成迁移 | `make gen-db && git diff --exit-code` |
| 查询未登记 | 每个 `.sql` 必须能追溯到 `data/queries/*.yaml` 条目 |
| 枚举两端一致 | Go 常量集合 == TS union 集合 |
| 索引无理由 | `reason` 缺失即警告 |
| `used_by` 漂移 | `make gen-graph && git diff --exit-code` |
| 敏感字段外泄 | `never_expose` 列不得出现在普通 OpenAPI/TS/事件/普通 entity；认证 query 与 `x-auth-response` 是唯一例外 |

---

## 5. 命名

- 契约文件：单数 snake_case（`tables/account.yaml` → `data.tables.account`）
- 物理表名：默认等于契约文件段；复数表用 `table.name` 覆盖（`accounts`、`tokens`）
- 索引：`idx_<physical_table>_<cols>`；唯一：`uq_<physical_table>_<cols>`
- 外键：`fk_<physical_table>_<ref>`；检查：`ck_<physical_table>_<rule>`
- 查询：`data.queries.<module>.<snake_case>`

---

## 6. 存储与部署

`data/storage.yaml` 是项目级单例：

- 引擎、版本、扩展、连接池、迁移工具
- 部署拓扑（single-primary / 副本数 / 读写分离）
- 定时任务运行方式与分布式锁
- `not_supported` 列表（mysql / mongodb / sharding / multi-primary / cross-region / outbox）

表级只声明会影响 DDL/查询的物理事实：`size_class`、`retention`、`partition`。
**禁止在 `tables/*.yaml` 里逐表声明 engine。**

### 6.1 凭据与令牌

- `auth_pwd.password_hash` 是 argon2id 主路径；`password_md5` 只做遗留兼容。登录成功后应升级为 `password_hash` 并清空 MD5。
- `token` 表只存 `access_token_hash` / `refresh_token_hash`，不存原文；原文只在 `/auth/login`、`/auth/refresh` 的响应中返回一次。
- 哈希算法与 TTL 由 `platform/security/auth_policy.yaml` 定义，data 层不复制。

---

## 7. 诚实边界

生产在线 DDL（大表加索引需 `CONCURRENTLY`、数据回填分步）无法全自动。
契约约束**最终态**，迁移**过程**允许人工介入，机器规则见 `_policy.yaml`。

P2 生成器尚未实现：当前的 `generated+tested` 是目标值，不代表已有闸门；生成器落地后以 `make gen` / `make gen-db` / `make gen-graph` 为准。
