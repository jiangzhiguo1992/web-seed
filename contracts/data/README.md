# 数据契约（data）

> 数据层只回答两件事：**数据库里有什么**（`tables`）、**业务怎么读写与解释它**（`entities` / `queries` / `enums`）。
> 字段语义、值域与生成行为以 `contracts/_meta/data.*.yaml` 为准；校验入口 `make contract-check`。
> 全局契约规则（ID、依赖链、变更流程）与迁移政策在 `contracts/README.md`，本文件不复述。

**标记约定**：`[强制: <rule-id>]` 表示该约束由 `contract-check` 强制；`[评审]` 表示无机器校验、靠 review。

---

## 1. 边界

data 在单向链末端，没有下游：

```
feature ──> capabilities ──> data
```

- 被 `capabilities` 通过 `method.queries` 读取、通过写查询修改；被 `features.rules/states` 通过 `data.entities` 引用为业务主体。
- **不负责**：业务不变量（`features/rules`）、写归属编排（`capabilities/services`）、部署与运行时（`platform`）。
- 目录内只有五类**实例**契约，外加一份类型词汇表 `types.yaml`。没有第六类实例；表达力不足时先扩展本层字段，不要新增平行概念。

---

## 2. 五类实例契约与类型词汇

| 契约 | 回答的问题 | 唯一职责 | 不是 |
|---|---|---|---|
| `tables/<file>.yaml` | 数据库里有什么 | 列、类型、可空、默认、CHECK、索引、继承 | 不是 DDL 文本容器 |
| `entities/<entity>.yaml` | 业务怎么读它 | Go 读模型：字段语义、关系、跨字段校验 | 不是写模型 |
| `queries/<scope>.yaml` | 能怎么读写 | 入参、返回、目标表、索引/缓存/归档引用 | 不是 SQL |
| `enums/<module>.yaml` | 值是什么 | 跨端词汇：按 owner_module 拆分，integer `code` ↔ 名称 | 不是表约束 |
| `storage.yaml` | 物理侧事实 | 引擎归属、扁平保留、缓存、归档（含能否回迁） | 不是部署配置，也不是索引或视图 |
| `types.yaml` | 值类型叫什么 | 项目级标量约束：family + 固定参数 | 不是表结构，也不是枚举 |

`storage.yaml` 与 `types.yaml` 是**固定文件**：文件名不随实例增长，只有一个契约 ID。`enums/` 按 `owner_module` 一文件一契约（文件 ID `data.enums.<module>`），文件内承载多个 `data.enums.<name>` 项；枚举项 ID 全局唯一。`tables/` / `entities/` / `queries/` / `enums/` 的实例清单以 `contracts/index.yaml` 为准，本文件不列举。

### 2.1 关系

```
queries    ──(tables / index_ref)──>  tables
queries    ──(caches / archive)────>  storage
queries    ──(returns)─────────────>  entities
entities   ──(source.column)───────>  tables
entities   ──(type)────────────────>  enums
storage    ──(table / cache_of / archive_of 条目)──>  tables
tables / entities / queries ──(type)──>  types
```

查询的 `tables` / `index_ref` 落到 `tables`，`caches` / `archive` 落到 `storage`（再由 `cache_of` / `archive_of` 落到 `tables`）；`entities` 是唯一中转层——`queries` 经由它返回业务数据，`enums` 则被它引用为字段类型。**`tables` 是物理结构的汇聚点。** 跨表冗余列关系直接写在 `tables` 的 `columns[].redundancy`，不引入新契约文件。

### 2.2 两个基类不是实例

`tables/_base.yaml` 与 `entities/_base.yaml` 是两个固定单例，ID 由 checker 写死识别（`[强制: base-file]`、`[强制: entity-base-file]`）：

- `data.tables.base`：5 列（`id` / `status` / `created_at` / `updated_at` / `deleted_at`）+ 类型级 `do_not`。**不是物理表**：不生成 DDL、不建实体、不落存储。
- `data.entities.base`：5 个基类字段的实体声明；类型从 `tables/_base.yaml` 继承，不重写。

物理表通过 `table.extends` 继承；实体通过 `model.extends: [data.entities.base]` 继承。基类列与字段不得重声明（`[强制: no-base-columns]`、`[强制: entity-no-base-fields]`）。

---

## 3. 为什么这样分层

每一条都是取舍，不是风格。

### 3.1 表不建数据库外键，但声明逻辑外键

**选择**：DDL 不生成 `FOREIGN KEY`；跨表引用用 `columns[].references` 表达（目标唯一列 + `on_delete`），一致性校验仍由应用服务与 `feature.rules` 保证。
**理由**：数据库外键把部署顺序、分库分表、软删语义绑死在引擎上；契约层要允许引擎替换（`storage.yaml` 的 `supported_engines`）。逻辑外键只描述业务层的硬删级联与关系语义，不产生 DDL。
**代价**：引用完整性不由数据库拒绝，漏写 `references` 不会报错；但一旦写了，`[强制: reference-target]` / `[强制: reference-join]` 会校验目标唯一性与实体关系一致性。跨表不变量的落点是 `feature.rules`，不是注释。

### 3.2 表不使用枚举，只存 integer 码

**选择**：`status` 等状态列是 `integer`；`data.enums.<name>.values[].code` 是数据库里的整数码，映射在 entity（`[强制: column-type]`）。
**理由**：枚举值会改名、会增加；DDL 只认整数，改枚举不触发迁移。
**代价**：entity 漏映射就是裸整数；新增枚举必须先定 `code`，且旧 `code` 不得复用（历史行仍引用）。

### 3.3 entity 是读模型，不是第二份写模型

**选择**：写模型只有 `tables` 一份；entity 只描述"读出来长什么样"。
**理由**：两份写模型必然漂移。entity 承载数据库表达不了的东西：字段语义、关系、跨字段校验（`validation`）、枚举映射。
**代价**：写路径的形状由 `queries` + `capabilities.persistence` 约束。不要给 entity 加 `CreateXxx` / `UpdateXxx` 这类写形状。

### 3.4 query 不写 SQL

**选择**：只声明 `tables` / `params` / `returns` / `index_ref` / `caches` / `archive`。`params` 第一项必须是 `ctx context.Context`（生成器注入，SQL 参数从第二项开始）；`returns` 最后一项必须是 `{name: err, kind: error}`，不能省略。
**理由**：SQL 语义 checker 无法验证；写进契约就是第二份真相，必然与 `.sql` 实现漂移。
**代价**：SQL 质量靠 `EXPLAIN` 与评审。契约只保证"这个查询被设计过、用到的索引列前缀与登记列一致、缓存/归档引用与 storage 双向对齐"。

### 3.5 storage 独立成单例

**选择**：引擎归属、`retention` 不写在单表里，而是内聚在 `storage.tables.<engine>` 的普通条目中；每条条目必须写 `description`：普通条目等于表契约的 `summary`，归档条目说明归档用途；归档用同位置的 `archive_of` 条目表达，`promotable` 标明该归档能否回迁热表。
**理由**：这些是项目级物理事实，不是表的业务结构。放在单表里会让"换引擎"变成改 N 个文件；条目化让同一张表的存储事实只有一个落点，也让不同引擎的字段差异可被脚本强制。
**代价**：新增表要登记两处（`tables/` + `storage.tables.<engine>`），漏登记被 `[强制: storage-registry]` 拦住。存储类特性用结构化字段表达（`archive_of`/`promotable`/`on_conflict` 归档条目、`columns` + `indexes[].expression` 混合表达式索引），不设 `ddl_ext` 逃生口。

`retention` / `caches` 是物理事实，不是 baseline DDL 的生成输入：`gen_db` 只消费 `tables.<engine>` 的权威表列表，生成列/约束/索引；清理与缓存配置由部署层实现。

---

## 4. 边界判据

### 4.1 约束写在哪一层

| 约束类型 | 写在哪 | 判据 |
|---|---|---|
| 数据库能表达的 | `tables` 的 `constraints` / `indexes` | 单行、单表、DDL 可表达（`CHECK`、唯一索引） |
| 跨字段 / 跨表不变量 | `entities` 的 `validation` | 需要多列或多表联合判断，但仍是数据形状 |
| 业务行为规则 | `features/rules` | 涉及"谁可以做""什么时候可以做" |

**不要把 DDL 搬进 entity，也不要把业务规则塞进 table。** 判据：这条约束失效时，该由数据库拒绝，还是由业务拒绝？

模块名用单数（`account`）；集合响应字段与动作名按语义保留复数（`accounts` / `listAccounts`）。**（`[评审]`）**

时间列统一用 `timestamp`（应用层只处理 UTC），`datetime` 族不使用。**（`[评审]`）**

### 4.2 索引与冗余

- **索引必须有查询**：`indexes[].used_by` 至少一个 `data.queries.*` 项，且该查询的 `tables` 包含本表（`[强制: index-used-by]`、`[强制: index-used-by-ref]`）。唯一索引也要写——插入与唯一性校验同样是使用方。没有查询的索引直接删除，不要写假 ID。
- **冗余必须成组声明**：`columns[].redundancy` 直接列出同组全部列引用（`[强制: redundant-group]`、`[强制: redundant-ref]`）。`source: true` 标记权威列，每组恰好一个；组内每列声明完全相同的列表，`copy` 的 `type` / `nullable` 必须与 source 一致。成员只接受物理表自行声明的列，继承列与基类列不参与。

- **引用必须声明为逻辑外键**：`columns[].references` 只指向目标表主键或全局唯一（`live_only: false`）单列索引列，两端类型族一致（`[强制: reference-target]`）；`on_delete` 表达**硬删**动作（`cascade` / `restrict` / `set_null` / `no_action`），软删不写在这里（`[强制: reference-on-delete]`）。每条 references 必须与 entity 关系字段的 `source.join` 列对双向一致（`[强制: reference-join]`）：实体声明了关系，表侧就必须给出删除策略；表侧写了逻辑外键，实体就必须有对应关系。

```yaml
# 子表列：account 被硬删时级联删除本行
columns:
  - name: account_id
    type: _types.integer
    nullable: false
    references: { column: data.tables.account.col.id, on_delete: cascade }
```

`redundancy` 与 `references` 不互斥：反规范化外键可以同时是"同值副本"和"删除级联"。当前契约没有该场景；新增时不要为了二选一拆成两列。

```yaml
# 示例（假设存在 tenant 表）：tenant_id 是 tenant.id 的副本，同时承担删除级联
columns:
  - name: tenant_id
    redundancy:
      - { column: data.tables.tenant.col.id, source: true }
      - { column: data.tables.order.col.tenant_id, source: false }
    references: { column: data.tables.tenant.col.id, on_delete: cascade }
```

### 4.3 哪些是手写，哪些是生成的

反向映射与生成物是两类东西，混淆是最高频的错误：

| 字段 | 谁写 | 含义 |
|---|---|---|
| `table.entities` | 手写 | 映射本表的 entity 集合（`[强制: table-entities]`） |
| `model.includes` | `make gen-graph` | 本实体关系字段指向的 entity 及基数 `{entity, cardinality}`（`[强制: entity-included-by]`，禁止手改） |
| `model.included_by` | `make gen-graph` | 通过关系字段指向本实体的 entity 及反向基数 `{entity, cardinality}`（`[强制: entity-included-by]`，禁止手改） |
| `table.references` | `make gen-graph` | 本表 `columns[].references` 的目标列（`[强制: table-referenced-by]`，禁止手改） |
| `table.referenced_by` | `make gen-graph` | 指向本表列的 `references` 源列（`[强制: table-referenced-by]`，禁止手改） |
| `indexes[].used_by` | 手写 | 使用该索引的 query（设计声明） |
| `table.queries` | `make gen-graph` | 使用本表的 query（`[强制: table-queries-generated]`，禁止手改） |
| `model.queries` | `make gen-graph` | 返回本实体的 query（`[强制: entity-used-by]`，禁止手改） |
| `queries[].used_by` | `make gen-graph` | 调用本 query 的 method（`[强制: query-used-by]`，禁止手改） |

这七个字段都只记**直接关系**，不展开传递闭包；完整影响面用 `make contract-impact id=<id>` 查询（P2 未实现）。

### 4.4 保留策略是选择，不是枚举

`storage.tables.<engine>[].retention` 是布尔开关，开启后 `unit` 与 `ttl` 是同级字段；`ttl` 是过期时长，只用于确实会过期清理的数据（如验证体），主数据与认证绑定默认不写。**这是选择判据，`[评审]`。** 值域与约束由 `[强制: storage-retention]` 校验；缓存副本必须写 `prefix`（Redis key 前缀，同一引擎内唯一）、`cache_of`（源表）与 `key`（列名列表，可写 `account_id` / `col.account_id` / 完整列引用；同一表可按不同 key 建多条缓存，`key` 顺序参与 `(cache_of,key)` 唯一性）以及 `caches.<engine>[].retention: true`，只允许 `unit` / `cache_ttl`，且不得超过权威表条目的 `ttl`。每个缓存必须被至少一个查询的 `caches` 有效引用，未引用的缓存必须删除；读查询的每个缓存键列必须出现在 `params[].column` 或同表 `index_ref[].columns` 中。归档用独立条目表达：每条 storage 条目都必须写 `description`（普通条目等于表 `summary`，归档条目说明归档用途）；`archive_of` 指向已登记的权威表，`promotable` 必填并决定能否回迁热表；查询用 `archive.promotable` 显式声明是否回迁，二者必须一致；`promotable:true` 的归档必须被至少一个 `archive.promotable:true` 的查询引用，查询必须写与 storage 一致的 `age_by`；`promotable:false` 的归档可供只读查询使用，查询不得写 `age_by`。能回迁时必须写 `on_conflict: skip|overwrite|error` 定义热表已有同唯一键行时的策略（skip 返回热表行并保留归档行，overwrite 用归档行覆盖热表行，error 返回冲突错误；不能回迁时禁止），`table` 是归档物理表名，`age_by` 是 `created_at` / `updated_at`，`after` 是按该列计算的数据年龄阈值。归档表只封存源表的原始行，不建 `data.tables` 契约、实体或查询；同一张表只能有一条归档，由 `[强制: storage-archive-source]` / `[强制: storage-archive-ref]` 校验。

---

## 5. 最小正例

### 5.1 表：约束 + 唯一索引 + 使用方

```yaml
constraints:
  - name: ck_account_deleted_consistent
    expr: "(status = 3) = (deleted_at IS NOT NULL)"
    on_violation: platform.errors.ACCOUNT_ILLEGAL_TRANSITION
    used_by:
      - data.queries.account.set_account_status
      - data.queries.account.soft_delete_account

indexes:
  - name: uq_account_number
    columns: [ number ]
    unique: true
    comment: "账号编号全局唯一；软删后不释放"
    used_by:
      - data.queries.account.exists_account_number
```

`live_only: true` 只用于唯一索引，表示唯一性只约束未软删行——软删后同值可以重建；`false` / 省略则约束全表（含软删行）。当前生成器只产出 MySQL DDL：用 `CASE WHEN deleted_at IS NULL THEN 1 ELSE NULL END` 表达式键排除软删行；PostgreSQL 对应 `WHERE deleted_at IS NULL` partial unique index（DDL 生成尚未接入）。非唯一索引不使用该字段，需要 live 行前导时把 `deleted_at` 显式写进 `indexes[].columns`，并在 `index_ref` 中把它作为最左列声明。`on_violation` 由 `make gen-db` 汇总到 `apps/api/internal/generated/constraint_errors_gen.go` 的 `ConstraintErrors`，供错误翻译使用。

### 5.2 查询：参数 + 返回 + 索引/缓存/归档引用

```yaml
- name: ListAccounts
  description: "按状态与归属筛选账号列表"
  tables:
    - table: data.tables.account
  params:
    - { name: offset, type: integer, nullable: true, default: 0 }
    - { name: limit, type: integer, nullable: true, default: 20 }
  returns:
    - { name: accounts, list: true, kind: entity, entity: data.entities.account }
  index_ref:
    - { index: data.tables.account.idx_account_created_at_id, columns: [ created_at, id ] }
```

返回值用 `kind` 区分：`scalar` / `affected_rows` / `entity` / `columns`；没有 `column` / `table`。`entity` 只写 entity、不能写 columns；`columns` 只在没有对应 entity 时使用，必须写列引用；`secret` 只有同模块可显式列出，跨模块禁止。`caches` / `archive` 引用 storage；`archive` 每项写 `{archive, promotable, age_by}`，`promotable` 必须与 storage 归档条目一致；`promotable:true` 时 `age_by` 必填且与 storage 一致，回迁时更新该列，避免再次被判定为冷数据；`promotable:false` 是只读穿透，禁止写 `age_by`。每个 `queries/<scope>.yaml` 只承载本 scope 的查询；其他组的普通业务查询不要写进来或引用本文件。

### 5.3 实体：列来源 + 枚举映射 + 关系

```yaml
- name: Status
  type: data.enums.account_status
  description: "账号状态"
  source: { column: data.tables.account.col.status }

- name: AuthPwd
  type: data.entities.auth_pwd
  description: "账号的密码认证记录"
  source:
    join:
      from: data.tables.account.col.id
      to: data.tables.auth_pwd.col.account_id
```

标量字段与关系字段互斥：标量写 `source.column/computed/soft`，关系写 `type` + `source.join.from/to`，两套键不得混用（`[强制: entity-field-variant]`）。多对多关系在 `source.join.through` 写中间表 `{table, from, to}`，中间表两列必须分别列级 `references` 到关系两端；关系基数由两端唯一索引与 `through` 推导（`[强制: entity-relation-cardinality]`），`model.includes` / `model.included_by` 由 `make gen-graph` 写入 `{entity, cardinality}`。实体不重复物理长度参数；统计口径不同时用 query 的 `returns`，不要把查询专属聚合塞进实体。

---

## 6. 常见错误

只列两类：复发过的错误、已被拒绝的越界写法。通用规则已由 checker 强制，不在此复述。

| 错误 | 为什么错 | 正确做法 |
|---|---|---|
| 手写 `migrations/*.sql` | 生成物，下次 `make gen-db` 被覆盖且 CI diff 失败 | 改 `tables/*.yaml`（`contracts/README.md` §6） |
| 唯一索引不写 `used_by` | `[强制: index-used-by]` 直接拒绝 | 把插入 / 唯一性查询列进去 |
| 软删表用全表唯一索引 | 删除后无法重建同值 | `live_only: true` |
| 给 entity 加写形状 | 第二份写模型必然漂移 | 用 query + capability 表达 |
| query 写 `statement` / `where` | 第二份真相，checker 无法验证 | 只写形状 |
| 在表里声明引擎 | 引擎归属只在 `storage.tables.<engine>` | 登记到 `storage.yaml` 的引擎条目 |
| 在表里写 `initial_rows` / seed | 字段不存在，生成器不识别 | 先扩展 `_meta/data.tables.yaml` 与生成器 |
| 枚举值复用旧 `code` | 历史行仍引用旧码 | 新增 `code` |
| `summary` / 列 `comment` 超长 | MySQL 会静默截断 COMMENT | `summary` <= 2048、列 `comment` <= 1024（`[强制: table-summary-length]` / `[强制: column-comment-length]`） |

---

## 7. 当前闸门状态

`make contract-check` 覆盖 data 层全部结构与引用校验（`data.tables` / `data.enums` / `data.entities` / `data.queries` / `storage` / `types`），并内嵌 `used_by` 新鲜度与 `rule-id` 同步检查。

| 闸门 | 机制 | 状态 |
|---|---|---|
| 契约结构与引用 | `make contract-check`（lefthook pre-commit + CI） | ✅ 已启用 |
| 反向索引漂移 | `gen_graph -check`（内嵌于 contract-check） | ✅ 已启用 |
| 规则 ID 同步 | `gen_rule_ids -check`（内嵌于 contract-check） | ✅ 已启用 |
| 契约 → DDL / migrations / sqlc | `make gen-db` | ✅ schema.sql + 0001_baseline.sql；sqlc ⬜ P2 |
| 迁移重放与目标态比对 | `make db-verify` | ⬜ P5 未实现 |
| 枚举两端一致（Go / TS） | `make gen` | ⬜ P2 未实现 |
| 影响面与血缘 | `make contract-impact` / `contract-graph` | ⬜ P2 未实现 |

**未实现的闸门不产生保护。** 在它们落地前，迁移正确性靠 review 与 `EXPLAIN`，不靠本文件。迁移政策与 `-- manual-adjust` 约定见 `contracts/README.md` §6；零停机操作步骤见 `docs/guides/architecture.md` §5.4。
