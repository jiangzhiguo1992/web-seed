# 数据契约（data）

> **`data/tables/*.yaml` 是表结构的唯一真相源。**
> `apps/api/migrations/*.sql` 与 `apps/api/internal/generated/db/schema.sql` 都是生成物。
> 禁止手写迁移；改表先改契约，再 `make gen-db`。
> 字段语义与生成行为见 `contracts/_meta/_types.yaml` / `data.tables.yaml` / `data.enums.yaml`；新建表用 `make contract-new`，改完跑 `make contract-check`。

---

## 1. 文件职责

| 文件 | 内容 | 产物 |
|---|---|---|
| `tables/_base.yaml` | 表结构基类：5 列 + `do_not`（单例，字段树与规则复用 `data.tables`） | 无（不生成 DDL、不建实体、不落存储） |
| `tables/<file>.yaml` | 物理表：字段、类型、约束、索引、继承 | `schema.sql`、增量 `migrations/*.sql`、sqlc 输入 |
| `entities/<entity>.yaml` | 读模型：Go 类型 `name`、表映射、语义字段（来源/关系/枚举/computed）、复杂校验 | Go/TS 类型 |
| `enums.yaml` | 跨前后端枚举集合 | Go 常量、TS union、entity 映射 |
| `queries/<module>.yaml` | 查询契约：入参、结果、目标表、索引 | 查询接口、sqlc 代码 |
| `storage.yaml` | 项目级存储引擎、部署拓扑、连接与锁；每张表的 engine/size_class/retention/partition；跨表冗余组 | 配置校验、运维约束 |

### 1.1 基类不是物理表

`tables/_base.yaml` 只收敛 5 个字段：

- `id`：integer（bits: 64），主键；具体生成方式由实现决定
- `status`：业务状态整数码；由基类统一提供，枚举映射在 entity
- `created_at`：数据库默认 `now()`
- `updated_at`：应用写入
- `deleted_at`：软删时间；为空表示未删除

基类的 `do_not` 直接写在 `_base.yaml`；列字段树与规则沿用 `data.tables` 的 meta。物理表必须在 `table.extends` 声明父表（可多父，链最终到达 `data.tables.base`）；不得重复声明任何继承列；枚举映射只在 entity。

### 1.2 表与实体是两层

| 概念 | 视角 | 位置 | 内容 |
|---|---|---|---|
| 表（table） | 持久化 | `data/tables/<file>.yaml` | 字段、类型、可空、默认值、CHECK、索引、`used_by`、冗余组 ID |
| 实体（entity） | 业务 | `data/entities/<entity>.yaml` | 继承 `_base`、业务字段/关系字段、`validation`（数据库表达不了的复杂校验） |

实体是**读模型**，不是表的第二份写模型：

- **每张物理表至少被一个 entity 映射**；否则数据没有业务出口。基类不是物理表，不参与映射。
- **一个实体可以跨多表**：合成只读，JOIN 条件写在 query，entity 用 `source.column` 声明字段来源。
- **约束层次不同**：`CHECK (char_length(number) > 0)` 是 DDL；「已软删的账号不可修改」是业务不变量。

5 个通用字段（`ID` / `Status` / `CreatedAt` / `UpdatedAt` / `DeletedAt`）在 `entities/_base.yaml`；类型从 `tables/_base.yaml` 继承，不在 `entities/_base.yaml` 重写。具体实体在 `model.extends` 写 `[data.entities.base]`，只写业务字段。基类字段不得重声明；`Status` 允许用 `data.enums.*` 特化类型，且必须是 `fields` 第一项。

实体字段顺序以 `contracts/_meta/data.entities.yaml` 的 `fields` 为准；`model` 内顺序以 `model.fields` 为准。`owner_module` 必须等于主表的 `owner_module`。

实体和字段名都是 Go 标识符：实体 `model.name` 首字母大写表示 public、小写表示 internal；字段 `name` 同理，首字母大写导出、小写不导出。`sensitivity: secret` 字段必须小写开头。`json_name` 省略时取 `snake_case(name)`；只有自定义命名才需要写。字段不再有 `visibility` / `read_only` / `write_only` / `format` / `deprecated`。类型不另起一套：显式 `type` 复用 `_meta/_types.yaml` 的 `column_types.families`，或写 `data.enums.*`。

`fields` 同时承载两种字段，二者互斥：

- **标量字段**：`description` + `source` + `validation` 必填。`source` 三选一：`column: data.tables.<t>.col.<c>`（数据库列）、`computed: "<表达式>"`（计算/聚合，必须使用指针类型 `*T`，且不得引用 `@param`）、`soft: "<说明>"`（外部传入或无法脚本化的来源）。`type` 前缀 `*` 表示指针，必须与来源列 `nullable` 完全一致；枚举字段只能映射 integer 列。`validation` 三选一：`column`（复用列规则，必须等于 `source.column`）、`pattern`（字符串硬正则）、`soft`（脚本无法验证的软表达式）；`soft` 来源不得用 `column`。
- **关系字段**：`type`（目标 entity）+ `join_column_from` + `join_column_to` + `load`（eager/lazy，默认 lazy）必填；基数由两端列唯一性推断；不得写 `source`/`validation`/`sensitivity`。`load: lazy` 表示默认不关联查询。

`model.tables[0]` 是主表，`ID` 必须来自主表 `id` 列。实体字段不重复物理长度参数；统计口径不同时用 query 的 `returns`，不要把查询专属聚合塞进实体。

### 1.3 约束与校验的分界

| 类型 | 写在哪 | 例子 |
|---|---|---|
| 数据库能表达的约束 | `tables/*.yaml` 的 `constraints` / `indexes` | `CHECK`、唯一索引；唯一性统一用 `indexes[].unique`；表不使用 enum 约束 |
| 跨字段/跨表/业务不变量 | `entities/*.yaml` 的 `validation` | 邮箱拆分一致性、令牌过期顺序 |
| 业务行为规则 | `business/rules/*.yaml` | 编号唯一、仅本人可改 |

**不要把 DDL 搬到 entities，也不要把业务规则塞进 tables。**

### 1.4 索引必须有查询，冗余组必须在 storage 登记

**索引不是“将来可能有用”的占位符。** `indexes[].used_by` 至少列一个查询项 ID，`contract-check` 校验：

- 每个索引都有 `used_by`；
- 每个 ID 都是已存在的 `data.queries.*` 项；
- 该查询的 `tables` 包含当前表。

因此 `uq_*` 也要写 `used_by`（插入/唯一性校验也是使用方）；没有查询的索引直接删除，不要写一个假 ID。

**冗余是显式设计，不是随手复制。** 表列只写组 ID，组明细在 `data/storage.yaml`：

```yaml
# data/tables/account.yaml
columns:
  - name: own_type
    redundancy: account_own_type

# data/storage.yaml
redundancy_groups:
  account_own_type:
    source: data.tables.account.col.own_type
    copies:
      - data.tables.token.col.own_type
```

约束：

- 每个组只能有一个 `source`，且至少有一个 `copy`；
- 组内所有列必须写同一个 `redundancy` 组 ID，且组必须列出该列（双向一致）；
- `copy` 与 `source` 的 `type` / `nullable` 必须一致；
- 组 ID 是双方共享的稳定 ID；权威数据只保留在 `source`。

### 1.5 查询声明索引引用：`index_ref`

`data/queries/*.yaml` 的每个 query 用 `index_ref` 指向表契约里已有的索引，而不是重新声明列前缀：

```yaml
- name: ListAccounts
  index_ref:
    - data.tables.account.idx_account_created_at_id
    - data.tables.account.idx_account_own_type_own_id
```

- `data.tables.<t>.pk` 表示主键；`data.tables.<t>.<index-name>` 指向 `indexes[].name`；
- 显式索引必须在 `indexes[].used_by` 中列出该 query，反向也校验；
- 单表查询至少一个 `index_ref`；多表查询可省略，也可引用任一查询表上的索引；
- 索引是否真正命中仍由 `EXPLAIN` 与评审负责，契约层只保证索引被设计和使用方登记。

### 1.6 查询是接口契约

查询只声明**入参、结果、目标表与索引引用**；SQL 如何执行不在契约里，由实现负责；读写归属在 capabilities 层。

```yaml
- name: ListAccounts
  tables:
    - data.tables.account
  params:
    - { name: offset, type: integer, nullable: true, default: 0 }
    - { name: limit, type: integer, nullable: true, default: 20 }
    - { name: status, type: integer, column: data.tables.account.col.status, nullable: true }
    - { name: has_nickname, type: boolean, expr: "nickname IS NOT NULL" }
  returns:
    - { name: accounts, kind: entity, entity: data.entities.account, list: true }
  index_ref:
    - data.tables.account.idx_account_created_at_id
    - data.tables.account.idx_account_own_type_own_id
```

- `tables`：查询读写的表，第一个是主表；可以多表；实例必须竖排；
- `params.offset` / `params.limit`：list 返回必须声明的分页参数；`offset` 默认 0，`limit` 默认值按查询决定且必须为正数；两者 type=integer、nullable:true；
- `params`：接口入参；数组统一用 `List<T>`；`column` 可把参数绑定到物理列，绑定后类型族必须与列一致；`expr` 用于不绑定单列的表达式，二者互斥；`default` 只在 `nullable: true` 时允许；
- `returns`：list<object>；每项用 `kind` 区分 `column` / `entity` / `table` / `scalar` / `affected_rows`；
- `returns[].columns`：表行/实体的结果投影，省略表示默认投影；`secret` 列只有同模块可显式列出，`pii` 列可返回；
- `returns[].list`：同一 query 的多项必须一致；`affected_rows` 不得为列表且必须是唯一返回项；
- `index_ref`：查询依赖的索引；单表查询至少一个；显式索引与 `indexes[].used_by` 双向校验；是否真正命中由 `EXPLAIN` 验证。

**不写** `statement` / `where` / `order_by` / `values` / `joins` / `semantics`。这些是 SQL 实现细节，写进契约只会制造第二份真相，且 checker 无法真正验证 SQL 语义；`params.expr` 只允许作为参数表达式存在，不承载整段 SQL。

---

## 2. 生成链

```
data/tables/_base.yaml + tables/*.yaml ──> schema.sql（目标态）
                                             ├─ diff 当前 DB ──> migrations/<ts>_<name>.sql
                                             └─> sqlc 输入 ──> Go 查询代码

data/enums.yaml ──────> Go 常量 + TS union + entity 整数映射
data/queries/*.yaml ──> queries/*.sql ──> sqlc 代码
data/{tables,entities,queries} ──> make gen-graph ──> used_by 反向索引
data/storage.yaml ────> 配置校验 + 运维约束
```

**初始数据**：当前表契约不提供 `initial_rows`；需要静态种子时，先扩展 `contracts/_meta/data.tables.yaml` 与生成器，不要在表文件里写未定义字段。RBAC 的 seed 由可选 `rbac` 模块负责，不在平台契约里承诺。

---

## 3. 硬规则

1. **一表一文件**，不按字段拆分；基类列不重复声明。
2. **每个可寻址项必须有稳定 ID**：表 `data.tables.<file>`、列 `data.tables.<file>.col.<col>`、约束 `data.tables.<file>.constraint.<constraint.name>`、查询 `data.queries.<module>.<snake_case(name)>`、枚举 `data.enums.<name>`。查询项和枚举项不写 `id`（由显式 `name` 派生，`name` 是稳定键，改名即新 ID）；列/约束用 `name` 派生，不得改名。契约文件名决定 ID；物理表名默认等于文件段；本仓库统一使用单数，不再通过 `table.name` 覆盖为复数。
3. **字段类型用契约内逻辑类型**：`string`、`boolean`、`integer`、`float`、`decimal`、`datetime`、`timestamp`、`json`、`bytes`、`data.tables.<file>.col.<col>`。表列不引用 `data.enums.*`；entity 字段才用枚举。
4. **类型族扩展字段写在列上**：`string` 用 `min_len`/`max_len`/`fixed`/`pattern`/`normalize`，`bytes` 用 `min_len`/`max_len`/`fixed`，`integer` 用 `bits`/`unsigned`/`min`/`max`，`float` 用 `bits`/`min`/`max`，`decimal` 用 `precision`/`scale`/`min`/`max`，`datetime` 用 `mode`/`timezone`，`timestamp` 用 `timezone`/`fractional_seconds`，`json` 用 `binary`。字段定义与 `required` 在 `contracts/_meta/_types.yaml` 的 `column_types.families`。
5. **时间统一 `timestamp`**，应用层只用 UTC。
6. **软删表必须有 `deleted_at`**。唯一性统一用唯一索引：`live_only: true` 只约束未软删行（软删后可复用），`false`/省略约束全表（含软删行）。
7. **表不使用枚举**：`status` 等状态列用 `integer`，`data/enums.yaml` 的 `values[].code` 是数据库整数码，entity 负责 code ↔ 枚举映射；枚举值可改名或删除，code 在当前枚举内唯一。
8. **索引必须写 `comment` 与 `used_by`**；`used_by` 至少一个查询项 ID，且查询引用当前表；表达式索引用 `expression`。没有查询的索引直接删除。
9. **主键类型是 integer（bits: 64）**，具体生成方式由实现决定；`created_at` 由数据库 `now()`，`updated_at` / `deleted_at` 由应用写入。
10. **`owner_module` 是写归属**：只有该模块的 `cap.services` / `cap.async` 可以写表；写查询用 `persistence.write_queries` / `writes` 显式登记，checker 校验单表与归属；跨模块读通过 `persistence.queries`。
11. **禁止未结构化 DDL 字段**：分区写在 `data/storage.yaml` 的 `tables.<id>.partition`，表达式索引用 `indexes[].expression`；大表索引的并发策略由 `storage.deployment.migrations.concurrent_index` 决定，不要塞原生 SQL。
12. **反向映射分两类**：`table.entities` 与 `model.included_by` 是**显式声明**，checker 双向校验；entity 的 `used_by.queries` / `used_by.capabilities` 是**生成物**，由 `make gen-graph` 按 query/capability 引用计算并写回，禁止手改。四类都只记**直接入边**。`used_by.queries` 记录查询项 ID（`data.queries.<module>.<snake_case(name)>`），不是查询文件 ID。注意 `indexes[].used_by` 是手写设计声明；完整影响面（页面/端点/流程）用 `make contract-impact id=<id>` 或 `generated/lineage.md` 查询。
13. **禁止数据库外键**：不使用 `foreign_key`；跨表一致性由应用服务与 `business.rules` 保证。
14. **冗余组必须登记**：`columns[].redundancy` 只写组 ID；`data/storage.yaml` 的 `redundancy_groups` 定义 `source`/`copies`，两边双向一致，`type`/`nullable` 一致；每组只能有一个 `source`。
15. **单表 query 必须声明 `index_ref`**：指向 `data.tables.<t>.pk` 或 `indexes[].name`；显式索引必须把 query 写进 `used_by`，反向也校验。多表 query 可省略，也可引用任一查询表的索引。
16. **entity 字段必须显式**：标量字段必写 `description`/`source`/`validation`；`source` 三选一（`column`/`computed`/`soft`），`source.column` 必须属于本实体表，同一列不得被两个字段引用，多表合成每张表至少被一个字段引用。`json_name` 省略时取 `snake_case(name)`；`type`/`sensitivity` 可省略并按来源列派生（`soft` 除外）；显式 `type` 的 `*` 必须与列 `nullable` 一致；`computed` 必须显式 `type`；`sensitivity` 不得放松。表↔实体的多对多映射由 `model.tables` + 每个字段的 `source.column` 明确；反向映射写在 `table.entities`，checker 校验双向一致。
17. **query 返回实体时必须完整**：`returns[].kind: entity` 的 `columns` 省略表示默认投影（全部非 secret 列）；显式时必须覆盖非指针、非 secret 列字段；`sensitivity: secret` 字段默认不进入投影，只有同模块 query 通过 `columns` 显式列出才可返回。返回实体的 query 会写入 `entity.used_by.queries`。
18. **反向映射必须对账**：`table.entities` 记录映射本表的 entity；`model.included_by` 记录通过关系字段指向本实体的 entity；`used_by.queries` 记录返回本实体的 query；`used_by.capabilities` 记录引用本实体的 capability。前两者显式声明，后两者由 `make gen-graph` 写回；`contract-check` 对四者逐一校验，悬空或缺失都报错。

---

## 3.1 迁移禁止操作

以下操作生成器直接报错，不会产出迁移：

迁移文件名：`<utc_timestamp>_<snake_case_name>.sql`。

| 禁止 | 替代做法 |
|---|---|
| `DROP COLUMN` | 先标 deprecated，停止写入一个版本后再删 |
| `RENAME COLUMN` | 加新列 → 双写 → 回填 → 切读 → 删旧列 |
| 给已有数据的表加 `NOT NULL` 且无默认值 | 加可空 → 回填 → 加约束 |
| 原地改列类型 | 新列 + 回填 + 切换 |
| `storage.tables.size_class=large` 的表非 `CONCURRENTLY` 建索引 | `CREATE INDEX CONCURRENTLY`（`transaction: false`） |

零停机三步：加可空列 → 分批回填 → 加约束（`NOT VALID` 后 `VALIDATE`）。

软删状态由 entity 映射；如保留 DB CHECK，用整数码（如 account 的 `deleted = 3`），不要写枚举名。

### 3.2 结构化 DDL（替代未结构化字段）

契约不提供 `ddl_ext` 这类逃生口；SQL 特性用结构化字段表达：

```yaml
# data/storage.yaml
tables:
  data.tables.account:
    partition: { strategy: range, columns: [created_at], interval: "1 month" }

# data/tables/account.yaml
indexes:
  - name: idx_account_email_lower
    expression: "lower(email)"
    comment: "邮箱不区分大小写查找"
```

- `storage.tables.<id>.partition`：`strategy` 为 `range|list|hash`；`columns` 必须是表列 ID 的末段
- `indexes[].expression`：表达式索引；必须写非空 `comment`
- `indexes[].comment`：生成 `COMMENT ON INDEX`；与 `columns` / `expression` 一起构成索引定义
- 生成器无法表达的新特性：先扩展本契约的字段，不要手写迁移

生成器无法表达时允许人工调整，但必须标：

```sql
-- manual-adjust: CONCURRENTLY 无法在事务内执行
```

人工调整统一使用 `-- manual-adjust:` 前缀；验证命令在 CI/Makefile 中执行。

### 3.3 类型级负向约束

以下约束适用于该类型的所有契约；具体实例只写差异，不要再复制。`data.entities` 的通用约束见 `entities/_base.yaml` 的 `do_not`。

**data.tables**

- 不要手写 migrations，改表契约后运行 `make gen-db`
- `CHECK expr` 只校验非空；SQL 语义由生成器/数据库验证，checker 不解析 SQL
- 物理表必须在 `table.extends` 声明一个或多个父表（最终到达 `data.tables.base`）；不得重复声明任何继承列
- 不要绕过软删过滤（`deleted_at IS NULL`）
- 唯一性统一用 `indexes[].unique`：`live_only: true` 只约束未软删行，`false`/省略约束全表（含软删行）
- 禁止数据库外键；跨表一致性由应用/业务规则保证
- 表列不引用 `data.enums.*`；状态整数码用 `integer`，由 entity 映射为枚举
- 不要用未结构化字段塞原生 DDL；分区用 `storage.tables.<id>.partition`，表达式索引用 `indexes[].expression`
- `table.entities` / `model.included_by` 显式声明；entity 的 `used_by.queries` / `used_by.capabilities` 是生成物，禁止手改
- 只有 `owner_module` 相同的 `cap.services` / `cap.async` 可以写表；跨模块读通过 `persistence.queries`
- `sensitivity: secret` 列禁止进入普通 OpenAPI/TS/事件/普通 entity；`pii` 列可脱敏返回；认证 query 与 `x-auth-response` 是唯一例外

**data.queries**

- 每个 query 不写 item id；用显式 `name`（PascalCase），项 ID 派生为 `data.queries.<file>.<snake_case(name)>`
- 每个 query 的表从 `tables` 推导，供图校验推导血缘
- 返回实体用 `returns[].kind: entity`；统计结果用 `kind: scalar` / `kind: column`
- 写查询在 capabilities 的 `persistence.write_queries` / `cap.async.writes` 中声明；必须已在 `persistence.queries` / `uses` 中登记，目标单表且归属与 capability 模块一致
- 不要在 repo 层手写 SQL
- 投影由 `returns[].columns` 声明；含 `secret` 列的表必须显式裁剪返回列
- nullable 参数在 SQL 中使用 `sqlc.narg('name')`

**data.enums**

- 枚举值可改名或删除；`code` 在当前枚举内唯一
- 删除枚举值后不要复用其 `code`；历史数据仍引用旧 code
- 每个枚举必须写 `used_by`，只列 `data.entities` / capability ID；不列物理表或 feature；列表逐行竖排
- 每个枚举项只写 `name`，项 ID 派生为 `data.enums.<name>`；不得再写 `id`
- 不要收模块内部枚举

**data.storage**：见 `storage.yaml` 的 `do_not`（单例契约，类型级约束就近写在文件内）。

---

## 4. CI 校验

| 检查 | 手段 |
|---|---|
| 迁移与契约一致 | 空库 apply 全部迁移 → dump → 与 `schema.sql` 逐字段比对 |
| 契约改了但没生成迁移 | `make gen-db && git diff --exit-code` |
| 查询未登记 | 每个 `.sql` 必须能追溯到 `data/queries/*.yaml` 条目 |
| 枚举两端一致 | Go 常量集合 == TS union 集合 |
| 索引无说明 | `comment` 缺失即报错 |
| `used_by` 漂移 | `make gen-graph-check` |
| 敏感字段外泄 | `sensitivity: secret` 列不得出现在普通 OpenAPI/TS/事件/普通 entity；认证 query 与 `x-auth-response` 是唯一例外 |

---

## 5. 命名

- 契约文件：snake_case（`tables/account.yaml` → `data.tables.account`）
- 物理表名：默认等于契约文件段；本仓库统一使用单数，不再通过 `table.name` 覆盖为复数
- 索引：`idx_<physical_table>_<cols>`；唯一索引：`uq_<physical_table>_<cols>`
- 检查约束：`ck_<physical_table>_<rule>`
- 查询：`data.queries.<module>.<snake_case>`
- 模块名：单数（`account`）；集合响应字段与动作名按语义保留复数（`accounts`、`listAccounts`）

---

## 6. 存储与部署

`data/storage.yaml` 是项目级单例：

- `supported_engines` / `default_engine`（postgres / mysql / mongodb）；`default_engine` 是当前项目选择，不是唯一允许引擎
- 部署拓扑（single-primary / 副本数 / 读写分离）
- 定时任务运行方式与分布式锁
- `not_supported` 列表（sharding / multi-primary / cross-region / outbox）

表级物理事实（`engine`、`size_class`、`retention`、`partition`）统一写在 `tables` 下；跨表冗余组写在 `redundancy_groups` 下；表契约不再承载。
**禁止在 `tables/*.yaml` 里逐表声明 engine。**

### 6.1 凭据与令牌

- `auth_pwd.password_hash` 是 argon2id 唯一路径；不保留 MD5 遗留字段。
- `token` 表只存 `access_token_hash` / `refresh_token_hash`，不存原文；原文只在 `/auth/login`、`/auth/refresh` 的响应中返回一次。
- 哈希算法与 TTL 由 `platform/security/auth_policy.yaml` 定义，data 层不复制。

---

## 7. 诚实边界

生产在线 DDL（大表加索引需 `CONCURRENTLY`、数据回填分步）无法全自动。
契约约束**最终态**，迁移**过程**允许人工介入，规则见本节。

P2 生成器尚未实现：当前的 `generated+tested` 是目标值，不代表已有闸门；生成器落地后以 `make gen` / `make gen-db` / `make gen-graph` 为准。
