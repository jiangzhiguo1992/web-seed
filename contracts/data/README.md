# 数据契约（data）

> 本组回答两个问题：**数据库里有什么**（`tables`）、**业务怎么读写与解释它**（`entities` / `queries` / `enums`）。
> 本文件只写组级心智模型：目录职责、模块关系、放置判据与不可机验的组级判断。
> 字段形状、取值与可机验规则以 `contracts/_meta/` 的类型规范与 `make contract-check` 为准；实例清单以 [`contracts/index.yaml`](../index.yaml) 为准；操作步骤以 [`docs/playbooks/contract-change.md`](../../docs/playbooks/contract-change.md) 为准。

**标记约定**：`[评审]` 表示无机器校验、靠 review；可机验规则由 `_meta/data.*.yaml` 的 `rules` 与 `contract-check` 承担，本文件不复述。

---

## 1. 定位与边界

data 在单向链末端，没有下游：

```text
feature ──> capabilities ──> data
```

- 上游可以引用本层的三类事实：`data.queries`（service step）、`data.entities`（service `validations[]`、rules `constraint.entity`、lifecycle `subjects[]`）、`data.enums`（service / event / HTTP / 页面的类型引用）。
- 本层不负责：业务不变量（`features/rules`）、写编排与跨模块调用（`capabilities/services`）、部署与运行时（`platform`）。
- 表的写归属由 `owner_module` 决定；跨模块写必须走归属模块的 service，不直接写表（由 `cap.services` 的 `service-owner-write` 强制）。
- 目录内只有五类实例契约，外加一份类型词汇表 `types.yaml`。没有第六类实例；表达力不足时先扩展本层字段或规则，不新增平行概念。

---

## 2. 目录与模块

| 路径 | 回答的问题 | 职责与边界 |
|---|---|---|
| `tables/<table>.yaml` | 数据库里有什么 | 列、类型、可空、默认、CHECK、索引、继承；不是 DDL 文本，不写引擎 |
| `entities/<entity>.yaml` | 业务怎么读 | Go 读模型：字段语义、关系、跨字段校验；不是写模型 |
| `queries/<scope>.yaml` | 能怎么读写 | 入参、返回、目标表、索引/缓存/归档引用；不是 SQL |
| `enums/<module>.yaml` | 值是什么 | 跨端词汇：integer `code` 与名称对应；不是表约束 |
| `storage.yaml` | 物理侧事实 | 引擎归属、保留、缓存、归档；不是部署配置，也不是索引或视图 |
| `types.yaml` | 值类型叫什么 | 项目级值类型：type + 固定参数；不是表结构，也不是枚举 |

文件粒度：一表一文件、一实体一文件、一查询 scope 一文件、一模块一文件（枚举）。具体实例清单不在这里列举，以 [`contracts/index.yaml`](../index.yaml) 为准；新增/删除实例不需要改本文件。

文件 ID 由路径派生：`data.tables.<file>` / `data.entities.<file>` / `data.queries.<scope>` / `data.enums.<module>`；两个单例固定为 `data.storage` / `data.types`。`storage.yaml` 与 `types.yaml` 的文件名与 ID 不随实例增长；`enums/` 按 `owner_module` 一文件一契约，文件内可承载多个枚举项，枚举项 ID 全局唯一。

`tables/_base.yaml` 与 `entities/_base.yaml` 是两个固定基类单例，只声明基类列/字段和类型级 `do_not`；它们不是物理表/实体，具体文件通过 `table.extends` / `model.extends` 继承，基类列/字段不得重声明；唯一例外以 `contracts/_meta/data.entities.yaml` 为准。

---

## 3. 关系

上游只读本层，本层不反向引用上游：

```text
capabilities.services ──query─────────────> queries
capabilities.services ──validations──────> entities
features.rules / cap.lifecycles ──entity──> entities
capabilities / features ──type────────────> enums
```

数据层内部：

```text
queries ──tables / index_ref──> tables
queries ──returns──> entities
queries ──params/returns type──> enums
queries ──caches / archive──> storage
entities ──model.tables / source.column/join──> tables
entities ──type / source.join──> entities
entities ──type──> enums
tables ──extends / references / redundancy──> tables
storage ──table/cache_of/archive_of──> tables
tables / entities / queries ──type──> types
```

| 边 | 写在 | 字段 |
|---|---|---|
| service → query | capabilities | `steps[].query` |
| service → entity validation | capabilities | `steps[].validations[]` |
| rule / lifecycle → entity | features / capabilities | `entity`（constraint）/ `subjects[].entity` |
| capability / feature → enum | capabilities / features | 参数 / 返回 / schema 的类型；页面 `enum` |
| query → table | queries | `tables` / `index_ref` |
| query → entity | queries | `returns` |
| query → enum | queries | `params[].type` / `returns[].type` |
| query → storage | queries | `caches` / `archive` |
| entity → table | entities | `model.tables` / `source.column` / `source.join` |
| entity → entity | entities | 关系字段 `type` / `source.join` |
| entity → enum | entities | field `type` |
| table → table | tables | `table.extends` / `columns[].references` / `columns[].redundancy` |
| storage → table | storage | `table` / `cache_of` / `archive_of` |
| table / entity / query → type | data | `type` |

`indexes[].used_by` 与其余反向索引字段（`table.entities` / `table.references` / `table.referenced_by` / `table.queries` / `storage.tables[].description`（普通条目） / `model.includes` / `model.included_by` / `model.queries` / `queries[].used_by` / `enums[].used_by` / `constraints[].used_by` / `validation[].used_by` 等）由 `make gen-graph` 写回，禁止手改；它们只记直接关系，不展开传递闭包。完整清单见 [`contracts/_meta/README.md`](../_meta/README.md)。

---

## 4. 放置判据

| 要表达的事实 | 放在 | 判据 |
|---|---|---|
| 单行/单表、DDL 可表达 | `tables` 的 `constraints` / `indexes` | 数据库能拒绝 |
| 多列/多表的数据形状 | `entities` 的 `validation` | 需要联合判断，但仍属数据形状 |
| 谁在什么条件下能做什么 | `features/rules` | 业务行为，不是数据形状 |
| 一次读写的输入/输出/表/索引 | `queries` | 只写形状，不写 SQL |
| 引擎、保留、缓存、归档 | `storage.yaml` | 项目级物理事实 |
| 项目级值域词汇 | `types.yaml` / `enums/` | 值域或跨端词汇 |
| 实体状态随时间的迁移 | `capabilities/lifecycles` | 状态机属于能力层 |

判断顺序：先问“数据库能否拒绝”，再问“是否仍是数据形状”，最后才问“是否业务行为”。不要把 DDL 搬进 entity，也不要把业务规则塞进 table。

---

## 5. 设计取舍

| 选择 | 理由 | 代价 |
|---|---|---|
| 逻辑外键，不建数据库外键 | 引擎无关；迁移顺序自由 | 引用完整性靠应用/规则，漏写不会被数据库拒绝 |
| 表存 integer code，不存 enum | 改枚举不触发迁移 | entity 漏映射就是裸整数；旧 code 不得复用 |
| entity 是读模型 | 避免两份写模型漂移 | 写形状由 query + capability 约束 |
| query 只写形状，不写 SQL | SQL 不可机验，写进契约就是第二真相 | SQL 质量靠 `EXPLAIN` 与 review |
| storage 独立单例 | 换引擎只改一处 | 新增表要登记 `tables/` + `storage.tables.<engine>` 两处 |

---

## 6. 组级判断（不可机验）

以下判断无法由字段形状或 checker 完整表达，按 review 执行；可机验部分见 `_meta/data.*.yaml` 的 `rules`。

- 本层只描述持久化事实与读写形状；业务行为、写编排、部署运行时不在这里。
- 表是写模型的唯一来源；entity 只描述读出来的形状，不新增 `Create*` / `Update*` 写形状。
- 表不存枚举，只存 integer code；映射在 entity。
- 引擎与物理事实只在 `storage.yaml`；表契约保持引擎无关。
- 表达力不足时先扩展本层字段或规则，不新增平行契约类型。
- 模块名用单数（`account`）；集合响应字段与动作名按语义保留复数（`accounts` / `listAccounts`）。`[评审]`
- 时间列统一用 `_types.basics.timestamp.*`（具体 variant 按 `contracts/_meta/_types.yaml`；应用层只处理 UTC）；不使用 `datetime` 族。`[评审]`
- 实体 ID 与外键 ID 统一用 `_types.basics.id`；分页偏移、计数、状态码等非 ID 整数用 `_types.basics.integer.32`。`[评审]`

---

## 7. 阅读路径与闸门

- 定位实例：[`contracts/index.yaml`](../index.yaml)（只看摘要，不读正文）。
- 查字段与规则：`contracts/_meta/`（各 `data.*.yaml` 类型规范与 `_types.yaml`）；[`_meta/README.md`](../_meta/README.md) 是写契约的总入口。
- 走流程：[`docs/playbooks/contract-change.md`](../../docs/playbooks/contract-change.md)。
- 看实现与进度：[`docs/guides/architecture.md`](../../docs/guides/architecture.md)。

新建 `tables` / `entities` / `queries` / `enums` 实例用 `make contract-new type=<type> name=<name> ...`；单例文件按 `_meta` 规范维护，不要手抄实例文件当模板。

闸门：`make contract-check` 校验结构、引用与生成字段新鲜度；`make gen-graph` 写回反向索引；`make gen` 生成 DDL / 模型 / OpenAPI。各类型的 enforcement 以 [`contracts/index.yaml`](../index.yaml) 为准；本文件不维护闸门状态表。
