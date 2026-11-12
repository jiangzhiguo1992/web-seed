# 数据契约（data）

> **`data/tables/*.yaml` 是表结构的唯一真相源。**
> `apps/api/migrations/*.sql` 与 `apps/api/internal/generated/db/schema.sql` 都是生成物。
> 禁止手写迁移；改表先改契约，再 `make gen-db`。
> 字段语义与生成行为见 `contracts/_meta/data.tables.yaml` / `data.enums.yaml`；新建表用 `make contract-new`，改完跑 `make contract-check`。

---

## 1. 文件职责

| 文件 | 内容 | 产物 |
|---|---|---|
| `tables/_base.yaml` | 表结构基类：5 列 + `do_not`（单例，字段树与规则复用 `data.tables`） | 无（不生成 DDL、不建实体、不落存储） |
| `tables/<file>.yaml` | 物理表：字段、类型、约束、索引、分区、保留期 | `schema.sql`、增量 `migrations/*.sql`、sqlc 输入 |
| `entities/<entity>.yaml` | 读模型：身份、字段、关系、复杂校验 | Go/TS 类型 |
| `enums.yaml` | 跨前后端枚举集合 | Go 常量、TS union、entity 映射 |
| `queries/<module>.yaml` | 查询契约：id、参数、返回、SQL、语义 | `queries/*.sql`、sqlc 代码 |
| `storage.yaml` | 项目级存储引擎、部署拓扑、连接与锁 | 配置校验、运维约束 |

### 1.1 基类不是物理表

`tables/_base.yaml` 只收敛 5 个字段：

- `id`：UUIDv7，应用生成，主键
- `status`：业务状态整数码；由基类统一提供，枚举映射在 entity
- `created_at`：数据库默认 `now()`
- `updated_at`：应用写入
- `deleted_at`：软删时间；为空表示未删除

基类的 `do_not` 直接写在 `_base.yaml`；列字段树与规则沿用 `data.tables` 的 meta。物理表必须声明 `extends`（可多父，链最终到达 `data.tables.base`）；不得重复声明任何继承列；枚举映射只在 entity。

### 1.2 表与实体是两层

| 概念 | 视角 | 位置 | 内容 |
|---|---|---|---|
| 表（table） | 持久化 | `data/tables/<file>.yaml` | 字段、类型、可空、默认值、CHECK/UNIQUE/EXCLUDE、索引、分区、`used_by`、`redundancy` |
| 实体（entity） | 业务 | `data/entities/<entity>.yaml` | 身份、字段、关系、`validation`（数据库表达不了的复杂校验） |

实体是**读模型**，不是表的第二份写模型：

- **每张物理表至少被一个 entity 映射**；否则数据没有业务出口。基类不是物理表，不参与映射。
- **一个实体可以跨多表**：合成只读，JOIN 条件写在 query，entity 只声明字段来源。
- **约束层次不同**：`CHECK (char_length(number) > 0)` 是 DDL；「已软删的账号不可修改」是业务不变量。

关联方式是 `table` / `tables` + `column`：单表实体可省略 `column`（按字段名匹配列），多表合成必须写 `column: data.tables.<t>.col.<c>`。实体字段不重复物理长度参数，也不允许聚合/计算字段；统计结果放 query 的 `output_fields`。

### 1.3 约束与校验的分界

| 类型 | 写在哪 | 例子 |
|---|---|---|
| 数据库能表达的约束 | `tables/*.yaml` 的 `constraints` | `CHECK`、`UNIQUE`、`EXCLUDE`；表不使用 enum 约束；软删后可复用的唯一性用 `indexes[].unique` + `where` |
| 跨字段/跨表/业务不变量 | `entities/*.yaml` 的 `validation` | 邮箱拆分一致性、令牌过期顺序 |
| 业务行为规则 | `business/rules/*.yaml` | 编号唯一、仅本人可改 |

**不要把 DDL 搬到 entities，也不要把业务规则塞进 tables。**

### 1.4 索引必须有查询，冗余列必须互相指向

**索引不是“将来可能有用”的占位符。** `indexes[].used_by` 至少列一个查询项 ID，`contract-check` 校验：

- 每个索引都有 `used_by`；
- 每个 ID 都是已存在的 `data.queries.*` 项；
- 该查询的 `table` / `tables` 包含当前表。

因此 `uq_*` 也要写 `used_by`（插入/唯一性校验也是使用方）；没有查询的索引直接删除，不要写一个假 ID。

**冗余字段是显式设计，不是随手复制。** 需要冗余时写：

```yaml
redundancy: { group: account_own_type, role: source }
# 副本表：
redundancy: { group: account_own_type, role: copy, ref: data.tables.account.col.own_type }
```

约束：

- 同一 `group` 只能有一个 `source`，且至少有一个 `copy`；
- `copy` 必须写 `ref` 指向同组 `source`；
- `copy` 与 `source` 的 `type` / `nullable` 必须一致；
- `source` 不写 `ref`。

`group` 是双方共享的稳定 ID，等价于“互相指向”；权威数据只保留在 `source`，避免双向引用产生第二个事实源。

### 1.5 查询声明索引列：`index_columns`

`data/queries/*.yaml` 的每个 query 必须写 `index_columns`，声明 SQL 过滤/连接/排序需要的列；每个内层列表是一个**最左前缀要求**：

```yaml
- id: data.queries.account.list_accounts
  table: data.tables.account
  index_columns:
    - [created_at, id]
    - [own_type]
```

校验规则：

- 每个内层列表必须按顺序匹配某条索引的最左前缀，或匹配主键（主键是隐式索引）；
- 匹配的显式索引必须在 `used_by` 中列出该 query；
- 反之，索引 `used_by` 中的 query 必须有一个内层列表能匹配该索引的最左前缀；
- `index_columns` 的列必须存在于当前表，且必须出现在 SQL 中；
- 单表查询必须写 `index_columns`；多表/JOIN 查询不得写，也不得出现在 `indexes[].used_by` 中。

所以 `list_accounts` 可以同时声明游标索引 `[created_at, id]` 和归属过滤索引 `[own_type]`；索引列更多时，query 只写自己需要的前缀，剩余列由索引覆盖。多表查询的索引性能由 `EXPLAIN` 与评审负责，契约层不假装能表达。

---

## 2. 生成链

```
data/tables/_base.yaml + tables/*.yaml ──> schema.sql（目标态）
                                             ├─ diff 当前 DB ──> migrations/<ts>_<name>.sql
                                             └─> sqlc 输入 ──> Go 查询代码

data/enums.yaml ──────> Go 常量 + TS union + entity 整数映射
data/queries/*.yaml ──> queries/*.sql ──> sqlc 代码
data/storage.yaml ────> 配置校验 + 运维约束
```

**初始数据**：当前表契约不提供 `initial_rows`；需要静态种子时，先扩展 `contracts/_meta/data.tables.yaml` 与生成器，不要在表文件里写未定义字段。RBAC 的 seed 由可选 `rbac` 模块负责，不在平台契约里承诺。

---

## 3. 硬规则

1. **一表一文件**，不按字段拆分；基类列不重复声明。
2. **每个可寻址项必须有稳定 ID**：表 `data.tables.<file>`、列 `data.tables.<file>.col.<col>`、约束 `data.tables.<file>.constraint.<constraint.name>`、查询 `data.queries.<module>.<query_id>`、枚举 `data.enums.<name>`。查询/枚举必须显式写 `id`；列/约束用 `name` 派生，不得改名。契约文件名决定 ID；物理表名默认等于文件段，需要不同名时用 `table.name` 覆盖（如 `account.yaml` 的 `table.name: accounts`）。
3. **字段类型用契约内逻辑类型**：`string`、`boolean`、`integer`、`float`、`decimal`、`datetime`、`timestamp`、`json`、`bytes`、`data.tables.<file>.col.<col>`。表列不引用 `data.enums.*`；entity 字段才用枚举。
4. **类型族扩展字段写在列上**：`string` 用 `min_len`/`max_len`/`fixed`/`pattern`/`normalize`，`bytes` 用 `min_len`/`max_len`/`fixed`，`integer` 用 `bits`/`unsigned`/`min`/`max`，`float` 用 `bits`/`min`/`max`，`decimal` 用 `precision`/`scale`/`min`/`max`，`datetime` 用 `mode`/`timezone`，`timestamp` 用 `timezone`/`fractional_seconds`，`json` 用 `binary`。字段定义与 `required` 在 `contracts/_meta/data.tables.yaml` 的 `column_types.families`。
5. **时间统一 `timestamp`**，应用层只用 UTC。
6. **软删表必须有 `deleted_at`**。全局唯一（含软删行）用 `constraints.unique`；软删后可复用用唯一索引 + `where: "deleted_at IS NULL"`。
7. **表不使用枚举**：`status` 等状态列用 `integer`，`data/enums.yaml` 的 `values[].code` 是数据库整数码，entity 负责 code ↔ 枚举映射；枚举值可改名或删除，code 在当前枚举内唯一。
8. **索引必须写 `reason` 与 `used_by`**；`used_by` 至少一个查询项 ID，且查询引用当前表；表达式索引用 `expression`，大表用 `concurrently: auto`。没有查询的索引直接删除。
9. **主键默认由应用层生成 UUIDv7**（`generated_by: application`），数据库不设 default。
10. **`owner_module` 是写归属**：只有该模块的 `cap.services` / `cap.async` 可以写表；跨模块读通过 `persistence.queries`。
11. **禁止未结构化 DDL 字段**：分区、表达式索引、CONCURRENTLY 必须用结构化字段表达（`table.partition`、`indexes[].expression`、`indexes[].concurrently`），不要塞原生 SQL。
12. **表级 `used_by` 是生成物**：由 `make gen-graph` 写入，禁止手改；结构固定为 `entities` / `queries` 两个列表，只记**直接入边**。`queries` 记录查询项 ID（`data.queries.<module>.<query>`），不是查询文件 ID。注意 `indexes[].used_by` 是手写设计声明，不是生成物；完整影响面（页面/端点/流程）用 `make contract-impact id=<id>` 或 `generated/lineage.md` 查询。
13. **禁止数据库外键**：不使用 `foreign_key`；跨表一致性由应用服务与 `business.rules` 保证。
14. **冗余字段必须声明**：`columns[].redundancy` 的 `source`/`copy` 同组，`copy` 必须写 `ref` 指向同组 `source`，`type`/`nullable` 一致；同组只能有一个 `source`。
15. **单表 query 必须声明 `index_columns`**：每个内层列表按顺序匹配表索引最左前缀或主键；匹配的索引必须把 query 写进 `used_by`，反向也校验。多表 query 不写 `index_columns`，也不进索引 `used_by`。
16. **entity 字段必须能投影回列**：单表实体无 `column` 时按字段名匹配；多表合成必须写 `column`，且每张表至少被一个字段引用。字段的 `required` 必须等价于来源列非空，`pii`/`never_expose` 不得放松。
17. **query 返回实体时必须完整**：`returns: data.entities.*` 的 `return_columns`（省略表示全字段）必须覆盖 required 非敏感字段；`never_expose` 字段只有同模块 query 可以返回。返回实体的 query 会写入 `entity.used_by.queries`。
18. **entity.used_by 是生成物**：`queries` 记录 `returns` 本实体的 query 项 ID，`entities` 记录通过 relations 指向本实体的 entity ID；禁止手改，`contract-check` 对账。

---

## 3.1 迁移禁止操作

以下操作生成器直接报错，不会产出迁移（机器可读部分见 `_types.yaml` 的 `types.data.tables.migration`）：

迁移文件名：`<utc_timestamp>_<snake_case_name>.sql`。

| 禁止 | 替代做法 |
|---|---|
| `DROP COLUMN` | 先标 deprecated，停止写入一个版本后再删 |
| `RENAME COLUMN` | 加新列 → 双写 → 回填 → 切读 → 删旧列 |
| 给已有数据的表加 `NOT NULL` 且无默认值 | 加可空 → 回填 → 加约束 |
| 原地改列类型 | 新列 + 回填 + 切换 |
| `size_class=large` 的表非 `CONCURRENTLY` 建索引 | `CREATE INDEX CONCURRENTLY`（`transaction: false`） |

零停机三步：加可空列 → 分批回填 → 加约束（`NOT VALID` 后 `VALIDATE`）。

软删状态由 entity 映射；如保留 DB CHECK，用整数码（如 account 的 `deleted = 3`），不要写枚举名。

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

`manual_adjust` 与 `verification` 的机器可读定义在 `_types.yaml` 的 `types.data.tables.migration`。

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

- 契约文件：snake_case（`tables/account.yaml` → `data.tables.account`）
- 物理表名：默认等于契约文件段；需要不同名时用 `table.name` 覆盖（`accounts`、`tokens`）
- 索引：`idx_<physical_table>_<cols>`；唯一：`uq_<physical_table>_<cols>`
- 唯一约束：`uq_<physical_table>_<cols>`；检查：`ck_<physical_table>_<rule>`；排除：`ex_<physical_table>_<rule>`
- 查询：`data.queries.<module>.<snake_case>`

---

## 6. 存储与部署

`data/storage.yaml` 是项目级单例：

- `supported_engines` / `default_engine`（postgres / mysql / mongodb）；`default_engine` 是当前项目选择，不是唯一允许引擎
- 部署拓扑（single-primary / 副本数 / 读写分离）
- 定时任务运行方式与分布式锁
- `not_supported` 列表（sharding / multi-primary / cross-region / outbox）

表级只声明会影响 DDL/查询的物理事实：`size_class`、`retention`、`partition`。
**禁止在 `tables/*.yaml` 里逐表声明 engine。**

### 6.1 凭据与令牌

- `auth_pwd.password_hash` 是 argon2id 唯一路径；不保留 MD5 遗留字段。
- `token` 表只存 `access_token_hash` / `refresh_token_hash`，不存原文；原文只在 `/auth/login`、`/auth/refresh` 的响应中返回一次。
- 哈希算法与 TTL 由 `platform/security/auth_policy.yaml` 定义，data 层不复制。

---

## 7. 诚实边界

生产在线 DDL（大表加索引需 `CONCURRENTLY`、数据回填分步）无法全自动。
契约约束**最终态**，迁移**过程**允许人工介入，机器规则见 `_types.yaml` 的 `types.data.tables.migration`。

P2 生成器尚未实现：当前的 `generated+tested` 是目标值，不代表已有闸门；生成器落地后以 `make gen` / `make gen-db` / `make gen-graph` 为准。
