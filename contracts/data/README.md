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
- 目录内只有五类契约。没有第六类；表达力不足时先扩展本层字段，不要新增平行概念。

---

## 2. 五类契约

| 契约 | 回答的问题 | 唯一职责 | 不是 |
|---|---|---|---|
| `tables/<file>.yaml` | 数据库里有什么 | 列、类型、可空、默认、CHECK、索引、继承 | 不是 DDL 文本容器 |
| `entities/<entity>.yaml` | 业务怎么读它 | Go 读模型：字段语义、关系、跨字段校验 | 不是写模型 |
| `queries/<scope>.yaml` | 能怎么读写 | 入参、返回、目标表、索引引用 | 不是 SQL |
| `enums.yaml` | 值是什么 | 跨端词汇：integer `code` ↔ 名称 | 不是表约束 |
| `storage.yaml` | 物理侧事实 | 引擎归属、`size_class`、保留、分区 | 不是部署配置，也不是索引或视图 |

`enums.yaml` / `storage.yaml` 是**固定文件**：文件名不随实例增长。`storage` 只有一个契约 ID；`enums.yaml` 承载多个 `data.enums.<name>` 项。`tables/` / `entities/` / `queries/` 按实例一文件一契约，实例清单以 `contracts/index.yaml` 为准，本文件不列举。

### 2.1 关系

```
queries    ──(tables / index_ref)──>  tables
queries    ──(returns)─────────────>  entities
entities   ──(source.column)───────>  tables
entities   ──(type)────────────────>  enums
storage    ──(tables / partition)──>  tables
```

五条边里三条直接落到 `tables`；`entities` 是唯一中转层——`queries` 经由它返回业务数据，`enums` 则被它引用为字段类型。**`tables` 是物理结构的汇聚点。** 跨表冗余列关系直接写在 `tables` 的 `columns[].redundancy`，不引入新契约文件。

### 2.2 两个基类不是实例

`tables/_base.yaml` 与 `entities/_base.yaml` 是两个固定单例，ID 由 checker 写死识别（`[强制: base-file]`、`[强制: entity-base-file]`）：

- `data.tables.base`：5 列（`id` / `status` / `created_at` / `updated_at` / `deleted_at`）+ 类型级 `do_not`。**不是物理表**：不生成 DDL、不建实体、不落存储。
- `data.entities.base`：5 个基类字段的实体声明；类型从 `tables/_base.yaml` 继承，不重写。

物理表通过 `table.extends` 继承；实体通过 `model.extends: [data.entities.base]` 继承。基类列与字段不得重声明（`[强制: no-base-columns]`、`[强制: entity-no-base-fields]`）。

---

## 3. 为什么这样分层

每一条都是取舍，不是风格。

### 3.1 表不使用外键

**选择**：跨表一致性由应用服务与 `feature.rules` 保证，表 `constraints` 只写单行可表达的 CHECK；契约里没有 `foreign_key`。
**理由**：外键把部署顺序、分库分表、软删语义绑死在数据库上；契约层要允许引擎替换（`storage.yaml` 的 `supported_engines`）。
**代价**：一致性校验必须显式写，漏写不会报错。跨表不变量的落点是 `feature.rules`，不是注释。

### 3.2 表不使用枚举，只存 integer 码

**选择**：`status` 等状态列是 `integer`；`data.enums.<name>.values[].code` 是数据库里的整数码，映射在 entity（`[强制: column-type]`）。
**理由**：枚举值会改名、会增加；DDL 只认整数，改枚举不触发迁移。
**代价**：entity 漏映射就是裸整数；新增枚举必须先定 `code`，且旧 `code` 不得复用（历史行仍引用）。

### 3.3 entity 是读模型，不是第二份写模型

**选择**：写模型只有 `tables` 一份；entity 只描述"读出来长什么样"。
**理由**：两份写模型必然漂移。entity 承载数据库表达不了的东西：字段语义、关系、跨字段校验（`validation`）、枚举映射。
**代价**：写路径的形状由 `queries` + `capabilities.persistence` 约束。不要给 entity 加 `CreateXxx` / `UpdateXxx` 这类写形状。

### 3.4 query 不写 SQL

**选择**：只声明 `tables` / `params` / `returns` / `index_ref`。
**理由**：SQL 语义 checker 无法验证；写进契约就是第二份真相，必然与 `.sql` 实现漂移。
**代价**：SQL 质量靠 `EXPLAIN` 与评审。契约只保证"这个查询被设计过、用的索引被登记过"。

### 3.5 storage 独立成单例

**选择**：引擎归属、`size_class`、`retention`、`partition` 不写在单表里。
**理由**：这些是项目级物理事实，不是表的业务结构。放在单表里会让"换引擎"变成改 N 个文件。
**代价**：新增表要登记两处（`tables/` + `storage.tables.<engine>`），漏登记被 `[强制: storage-registry]` 拦住。存储类特性用结构化字段表达（`partition` / `indexes[].expression`），不设 `ddl_ext` 逃生口。

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

```yaml
# tables/account.yaml 与 tables/token.yaml 的 own_type 列写同一份列表
columns:
  - name: own_type
    redundancy:
      - column: data.tables.account.col.own_type
        source: true
      - column: data.tables.token.col.own_type
        source: false
```

### 4.3 哪些是手写，哪些是生成的

反向映射与生成物是两类东西，混淆是最高频的错误：

| 字段 | 谁写 | 含义 |
|---|---|---|
| `table.entities` | 手写 | 映射本表的 entity 集合（`[强制: table-entities]`） |
| `model.included_by` | 手写 | 通过关系字段指向本实体的 entity（`[强制: entity-included-by]`） |
| `indexes[].used_by` | 手写 | 使用该索引的 query（设计声明） |
| `table.queries` | `make gen-graph` | 使用本表的 query（`[强制: table-queries-generated]`，禁止手改） |
| `model.queries` | `make gen-graph` | 返回本实体的 query（`[强制: entity-used-by]`，禁止手改） |
| `queries[].used_by` | `make gen-graph` | 调用本 query 的 method（`[强制: query-used-by]`，禁止手改） |

这六个字段都只记**直接关系**，不展开传递闭包；完整影响面用 `make contract-impact id=<id>` 查询（P2 未实现）。

### 4.4 保留策略是选择，不是枚举

`retention.<table>` 不是从若干策略里四选一：`pending` 用于未完成记录，`ttl` 用于过期凭证，`soft_delete` / `hard_delete` 只在确实存在对应清理策略时才写；主数据与认证绑定默认不写。**这是选择判据，`[评审]`。** 值域与约束由 `[强制: storage-retention]` 校验（含 `cache_ttl` 只能用于 `caches` 中的表且不得超过 `ttl`）。

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
    live_only: true
    comment: "账号编号在未删除账号内唯一"
    used_by:
      - data.queries.account.get_account_by_number
```

`live_only: true` 只约束未软删行——软删后同值可以重建；`false` / 省略则约束全表（含软删行）。`on_violation` 由 `make gen-db` 汇总到 `apps/api/internal/generated/constraint_errors_gen.go` 的 `ConstraintErrors`，供错误翻译使用。

### 5.2 查询：参数 + 返回 + 索引引用

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
    - data.tables.account.idx_account_created_at_id
```

返回值用 `kind` 区分：`scalar` / `affected_rows` / `entity` / `columns`；没有 `column` / `table`。`entity` 只写 entity、不能写 columns；`columns` 只在没有对应 entity 时使用，必须写列引用；`secret` 只有同模块可显式列出，跨模块禁止。

### 5.3 实体：列来源 + 枚举映射 + 关系

```yaml
- name: Status
  type: data.enums.account_status
  description: "账号状态"
  source: { column: data.tables.account.col.status }

- name: Tokens
  type: data.entities.token
  description: "账号的设备令牌"
  source:
    join:
      from: data.tables.account.col.id
      to: data.tables.token.col.account_id
      lazy: true
```

标量字段与关系字段互斥：标量写 `source.column/computed/soft`，关系写 `type` + `source.join.from/to/lazy`，两套键不得混用（`[强制: entity-field-variant]`）。实体不重复物理长度参数；统计口径不同时用 query 的 `returns`，不要把查询专属聚合塞进实体。

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
| 在表里声明引擎 | 引擎归属只在 `storage.tables.<engine>` | 登记到 `storage.yaml` |
| 在表里写 `initial_rows` / seed | 字段不存在，生成器不识别 | 先扩展 `_meta/data.tables.yaml` 与生成器 |
| 枚举值复用旧 `code` | 历史行仍引用旧码 | 新增 `code` |

---

## 7. 当前闸门状态

`make contract-check` 覆盖 data 层全部结构与引用校验（`data.tables` / `data.enums` / `data.entities` / `data.queries` / `storage`），并内嵌 `used_by` 新鲜度与 `rule-id` 同步检查。

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
