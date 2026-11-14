# 契约字段规范（_meta）

> 本目录是**契约的单一源**：字段树、值域、字段含义、跨字段规则与校验入口。
> 它不是业务契约，不登记在 `contracts/index.yaml`，也不进入 `generated/`。
> `contracts/generated/` 只放**机器生成物**（如 `openapi/admin.yaml`）；人读视图在 `docs/reference/generated/`。

---

## 1. 目录

```
contracts/_meta/
├── README.md             # 本文件：AI 入口
├── _types.yaml           # 共享值类型：类型族与参数（不绑定存储引擎）
├── data.tables.yaml      # data.tables 的字段树 + 值域 + 说明
├── data.enums.yaml       # data.enums 的字段树 + 值域 + 说明
├── data.entities.yaml    # data.entities 的字段树 + 值域 + 说明
└── data.queries.yaml     # data.queries 的字段树 + 值域 + 说明

scripts/contract/
├── scaffold.sh           # make contract-new
└── validate.sh           # make contract-check

scripts/cmd/
├── go.mod                # 契约工具独立 Go module
├── internal/contractspec/ # schema 解析 + 骨架渲染
├── internal/check/       # 契约校验实现（tables / enums / entities / queries / refs）
├── internal/graph/       # used_by 入边计算与写回
├── contract_new/
├── contract_check/
└── gen_graph/
```

### 1.1 为什么有 `_types.yaml`

类型族是 `data.tables` / `data.entities` / `data.queries` 共用的值类型词汇（string/integer/timestamp/...）。把它挂在 `data.tables.yaml` 会让实体和查询间接依赖表规范，也无法独立演进。

- `_types.yaml` 只放 `column_types.families`：类型族键、`description`、`params`；
- `columns[].type` 的列引用规则写死在 checker：只允许 `data.tables.<t>.col.<c>`，不是类型族，也不是外键；
- checker 对 `_types.yaml` 使用 `KnownFields` 严格解析，缺少文件或 families 为空会直接失败。

**没有 `schema/`、没有 `templates/`。**
字段说明和值域在同一个文件、同一个字段节点里；`contract-new` 只生成最小骨架，字段规范以本目录为准，不维护第二份字段清单。

---

## 2. AI 写契约的顺序

```text
1. 读 contracts/index.yaml（定位要改的契约）
2. 读 contracts/_meta/<type>.yaml（字段含义、值域、生成行为、易错点）
3. 新建：make contract-new type=data.tables name=note module=notes
4. 改 YAML（只改契约，不写生成物）
5. make gen-graph（写回 table/entity used_by）
6. make contract-check
7. make gen
8. git diff --exit-code
```

**`make gen` 是“契约 → 产物”的投影，不是“生成契约”。**
新建契约用 `make contract-new`；`make gen` 必须在契约写完之后跑。

---

## 3. 命令

| 命令 | 作用 | 何时跑 |
|---|---|---|
| `make contract-new type=data.tables name=note module=notes summary="备注表"` | 按 `_meta/data.tables.yaml` 生成最小表骨架；`column_type=decimal` 可指定第一列类型并带出扩展字段 | 新建表前 |
| `make contract-new type=data.enums name=note_status` | 按 `_meta/data.enums.yaml` 打印枚举项片段 | 新增枚举时 |
| `make contract-new type=data.entities name=note table=note` | 按 `_meta/data.entities.yaml` 生成单表读模型骨架；owner_module 从目标表读取（目标表必须已存在） | 新建实体时 |
| `make contract-new type=data.queries name=note module=notes table=note` | 按 `_meta/data.queries.yaml` 生成 `SELECT EXISTS` 最小查询骨架（目标表必须已存在） | 新建查询集时 |
| `make gen-graph` | 按引用计算并写回 table/entity 的 `used_by` | 改完表/实体/查询后 |
| `make gen-graph-check` | 只校验 `used_by` 是否最新（CI） | CI |
| `make contract-check` | 结构 / ID / 约束 / 索引 / used_by / owner / entity 投影 / query 参数与返回 / persistence.queries | 每次改完契约 |
| `make gen` | 生成 DDL / OpenAPI / 类型 / graph | `contract-check` 通过后 |
| `make gen-openapi` / `gen-db` / `gen-graph` / `gen-client` | 单独投影 | 按需 |

`contract-check` 已接入 `lefthook` 的 pre-commit（`contracts/**/*.yaml`）。

`contract-new` 支持 `data.tables` / `data.enums` / `data.entities` / `data.queries`；类型族来自 `_meta/_types.yaml`。entity/query 骨架要求目标表契约已存在，entity 默认生成 lowerCamel 的 internal `model.name`，需要 public 时改成 PascalCase；query 默认生成 `returns: boolean` / `index_ref: [<表>.pk]` 的接口骨架，不伪造 SQL。`contract-new` 生成最小合法骨架：required 字段、必要的 params/index_ref，以及含 required/default 的类型族扩展字段；`storage.tables` 的 retention/partition、`columns.sensitivity` 等可选字段不强行展开，避免生成无意义默认值。状态列由 `_base` 继承，不在骨架中生成。生成后仍需跑 `contract-check` 完成硬约束校验。`data.enums` 片段用 `code: -1` 与 TODO 描述占位，`contract-check` 会拒绝，直到替换为真实值。

---

## 4. 当前试点范围

`contract-check` 当前覆盖：

- `_meta/<type>.yaml` 字段规范按 `KnownFields` 严格解析：未知字段（如 `requred`）直接失败，不再静默忽略
- `_meta/_types.yaml` 的 `column_types.families` 由 tables/entities/queries 共用；文件缺失、families 为空、族/参数定义非法都会直接失败（`schema.types`）
- 模板字段顺序是唯一顺序源：tables/entities/queries/enums 实例的顶层与嵌套对象（含 `fields` / `item_fields`）都按 `_meta/<type>.yaml` 的字段顺序校验；新增字段只改模板
- 没有 `_meta` 模板的契约（`data.storage`、capabilities/business/features/platform）暂不做字段顺序校验；需要覆盖时先补该类型的 `_meta` 模板
- `data.tables/*.yaml`
  - `targets.dir/glob` 决定扫描路径；`targets.id/naming` 由 checker 校验（ID 模板、snake_case）
  - `columns[].type` 只允许类型族键或 `data.tables.<t>.col.<c>`；表列不引用 `data.enums.*`
  - 必填字段、类型、枚举、`const`、`pattern`、`allowed`、`unique`
  - `id` 按 `targets.id` 模板生成；基类 `_base.yaml` 的路径/ID 写死在 checker，不参与模板
  - 物理表 `table.extends` 是父表列表：可指向 `data.tables.base` 或任意已存在的表；多父同名列只有来源相同（钻石继承）才允许；链不得成环且必须到达基类；不得重复任何继承列
  - 基类 `id` 的 `primary_key` 列不得可空；物理表不得声明 `primary_key`，主键统一由基类提供
  - `default` / `default_sql` 互斥；`default` 的 YAML 类型、长度、范围与列类型族兼容
  - 列 / 约束 / 索引重名；`do_not` 每条必须含本表具体标识
- `data.tables.base`（`contracts/data/tables/_base.yaml`，单例）
  - 路径/ID 由 checker 写死识别；YAML 无 `kind`/`targets` 特殊配置
  - 基类必须且只能是 5 列：`id` integer（bits: 64）主键、`status` integer 非空且非主键、三个时间列 timestamp（`created_at`/`updated_at` 非空、`deleted_at` 可空）
  - `do_not` 直接写在 `_base.yaml`；基类 `do_not` 允许类型级通用规则，物理表 `do_not` 必须含本表标识
  - 规则 `base-file`、`base-columns` 在 `_meta/data.tables.yaml` 的 `rules` 中声明
  - `constraints` 只允许 `check`，必须写 `expr`；唯一性统一由 `indexes[].unique` 表达
  - 禁止 `foreign_key`；跨表一致性由应用/业务规则保证
  - 唯一索引的 `live_only` 决定作用域：`true` 只约束未软删行（要求 nullable `deleted_at`），`false`/省略约束全表（含软删行）
  - `indexes` 的 `comment`、`columns` xor `expression`、命名前缀、列引用；`used_by` 至少一个查询项、查询引用当前表
  - `columns[].redundancy` 只写组 ID；`data/redundancy.yaml` 的 `groups` 定义 `source`/`copies`，两边双向一致，`type`/`nullable` 一致
  - `data/queries/*.yaml` 的 `index_ref`：指向 `data.tables.<t>.pk` 或 `indexes[].name`；显式索引必须在 `used_by` 中列出该 query
  - `data/storage.yaml` 的 `tables`：覆盖每张物理表且无孤儿；engine/size_class/retention/partition 的结构与取值；`data/redundancy.yaml` 的组 ID、source/copies 与列声明双向一致
  - `table.entities` 显式记录映射本表的实体，checker 校验；`used_by` 只记生成的 `queries`
  - `owner_module` 与 capabilities 写表归属一致（只有 `cap.services` / `cap.async` 可写）；跨模块读通过 `persistence.queries`，`read_only_tables` 已移除
  - `persistence.queries` 引用的查询项必须存在；`persistence.write_queries`（service）与 `writes`（async）必须已在 queries/uses 中登记，且每个写表在 `persistence.table/tables` 中、归属一致；`persistence.constraint_errors` 的键必须是所写表的约束或索引名
  - 索引名同一 schema 内必须全局唯一
  - contracts 下文本字段（`description` / `comment` / `summary` / `do_not` 等）必须使用双引号（`contract-check` 校验）
  - 所有契约的顶层 `fields` 必须竖排（block style），嵌套 `item_fields` 不限制（`schema.fields-style`）；`table.extends/entities`、`model.extends/tables/included_by` 必须竖排（`schema.list-style`）
  - 具体契约的 `do_not` 不得与组 README / `_base` 的指导重复（`schema.do-not-duplicate`）
  - `do_not` 只写脚本无法判定且非纯风格的业务/架构约束；字段形状、枚举、默认值、引用存在性等已由 `rules` / schema 强制，不重复
  - `rules` 只允许出现在 `_meta`；`business/rules/*`、`platform/architecture/go_arch.yaml`、`platform/observability/log_fields.yaml` 的 `rules` 是领域规则集正文，不属于类型规则清单。具体契约的 `do_not` 只写实例级；`_meta` 下不得出现 `do_not`（`schema.meta-placement`）
  - `rules` 的 `id`/`text` 非空、`id` 唯一；`implemented_by` 必须包含规则自身 ID，且每个值都指向 checker 实际输出的 `[rule-id]`（`schema.rules`）
  - checker 输出的非 `schema.*` 错误 ID 必须由某个类型的 `rules` 声明，反向防止“有实现没规则”
  - `rule_ids_gen.go` 由 `make gen-rule-ids` 从 `rulef("...")` 生成；`make contract-check` 校验它是否最新
  - `_meta/*.yaml` 以 `type` 作为类型 registry；checker/contract-new 按 type 查找，不再写死 meta 路径
  - 错误统一带 `[rule-id]` 前缀：类型规则用 `_meta/<type>.yaml` 的 `rules`，通用结构/格式用 `schema.*`（含 `schema.targets`）
- `data/enums.yaml`
  - 集合 ID；item 不写 id，item ID 由 `name` 派生为 `data.enums.<name>`；值唯一、`code` 唯一且为 int32（0..2147483647）
  - `name` / `go_type` / `ts_type` 在集合内唯一；类型名冲突会让 Go/TS 产物互相覆盖
  - `summary` 非空；`stability: stable` 不得含 TODO/FIXME
  - 任意契约（entity / capability / feature 等）的 `type: data.enums.*` 必须存在；表列仍禁止引用枚举
  - `used_by.entities/capabilities` 必须与 entity 字段和 capability 引用完全一致；不列物理表或 feature，空列表合法；两个列表逐行竖排
  - `do_not` 每条必须含集合 ID `data.enums`、枚举 name 或 go_type/ts_type；通用规则放组 README
- `data.entities.base`（`contracts/data/entities/_base.yaml`，单例）
  - 路径/ID 由 checker 写死识别；5 个字段固定为 `ID` / `Status` / `CreatedAt` / `UpdatedAt` / `DeletedAt`
  - `ID` / `Status` integer、`CreatedAt` / `UpdatedAt` timestamp、`DeletedAt` 可空 timestamp；类型从 `tables/_base.yaml` 继承，不在 `entities/_base.yaml` 重写；每个基类字段必须写 `source`/`validation`/`description`
  - `do_not` 直接写在 `_base.yaml`，承载 `data.entities` 类型级通用规则；具体实体 `do_not` 必须含本实体标识
- `data/entities/*.yaml`
  - 结构按 `_meta/data.entities.yaml`；`model.extends` 必须是 `[data.entities.base]`；具体实体不得重声明基类字段，`Status` 可用 `data.enums.*` 特化
  - `model.tables` 必填、非空、去重；`model.tables[0]` 是主表；每张物理表至少一个 entity 映射
  - `owner_module` 必填且必须等于主表 `model.tables[0]` 的 `owner_module`
  - 字段二选一：标量字段（`source`）或关系字段（`type` + `join_column_from` + `join_column_to` + `load`）；两套键不得混用
  - 标量字段必须显式 `description`/`source`/`validation`；存在 `source.column` 时必须属于本实体表，同一列不得被两个字段引用，多表合成每张表至少被一个字段引用
  - `source` 三选一：`column`（物理列）、`computed`（计算/聚合）、`soft`（外部传入或无法脚本化的来源说明）；`computed` 必须显式 `type`、指针类型且不得引用 `@param`；`soft` 必须显式 `type`
  - `json_name` 省略时取 snake_case(name)；标量字段的 `type`/`sensitivity` 可省略并按来源列派生（`soft` 除外）；关系字段的 `type` 是目标 entity；显式标量 `type` 的 `*` 必须与列 `nullable` 一致
  - `validation` 必填三选一：`column`（指向存在的列，且必须等于 `source.column`）、`pattern`（string 字段的合法正则）、`soft`（非空软表达式）；`soft` 来源不得用 `column`
  - 实体 `model.name` 是 Go 类型名：首字母大写 public，小写 internal；`model` 内字段顺序以 `_meta/data.entities.yaml` 的 `model.fields` 为准；不得重名
  - `ID` 是所有实体的唯一主键：来自主表 `id` 列、非指针、唯一主键
  - 字段 `name` 是 Go 字段名：首字母大写导出，小写不导出；`sensitivity: secret` 字段必须小写开头；`json_name` 省略时取 snake_case(name)，同实体唯一
  - 关系字段的 type entity / join_column_to 所属表 / join_column_from/to 类型一致性；基数由两端唯一性推断；eager 必须有索引且不得成环
  - 实体级 `validation[].name` 必须 snake_case 且实体内唯一，ID 派生为 `<entity-id>.<name>`；`expr` 非空、`on_violation` 必须存在
  - `model.included_by` 显式声明，checker 校验与关系字段入边一致；`used_by.queries` 与 returns 本实体的 query 完全一致，`used_by.capabilities` 与引用本实体的 capability 完全一致
- `data/queries/*.yaml`
  - 结构按 `_meta/data.queries.yaml`；item 不写 id，name=PascalCase 且全局唯一；项 ID 由 `<文件 id>.<snake_case(name)>` 派生
  - `tables` 是查询读写的表；每项 `{table, write}`，`write: true` 表示写该表；必须存在、不是 `data.tables.base`、不得重复；第一项为主表；可以多表；实例必须竖排
  - `offset` / `limit` 是保留分页参数名：出现时必须 type=integer、nullable:true、default 必填；`offset` 默认 0，`limit` 默认值按查询决定且必须为正数；list 返回必须同时声明两者
  - `params` 只声明接口入参；name 唯一、type 非空；未绑定 `column` 时 type 必须映射到类型族或 `data.enums.*`；`column` 绑定列后类型族必须与列一致（`data.enums.*` 按 integer 列）；`expr` 与 `column` 互斥；`default` 只在 `nullable: true` 时允许，checker 校验可判定的标量/数组外层，枚举值与数组元素交给生成器
  - `returns` 是 list<object>：每项用 `kind` 区分 `column` / `entity` / `table` / `scalar` / `affected_rows`；kind 专属字段必须出现，互斥字段不得出现
  - 同一 query 的多项 `list` 必须一致；`affected_rows` 必须是唯一返回项且不得为列表
  - `columns` 只用于 `table` / `entity`；实体显式 `columns` 必须覆盖全部非指针、非 `secret` 字段
  - 单表 query 至少一个 `index_ref`；显式索引必须与 `used_by` 双向一致
  - `sensitivity: secret` 列只有同模块可显式返回；`pii` 可返回
  - 写查询在 capabilities 的 `persistence.write_queries`（service）或 `writes`（async）中声明；查询必须有至少一个 `write: true` 的表；必须已在 queries/uses 中登记，checker 校验存在、每个写表归属
- 实体/查询/能力对表的引用存在
- `index.yaml` 的 path 存在、path 与 id 一致、id/path 不重复；entities/queries 文件必须登记

**尚未机器化**（写在 `_meta` 的 `description` / `rules`，靠 review）：

- 命名品味、业务语义、跨模块读归属是否最小化
- 其余契约类型规范（http/services/...）
- 按需生成的 graph 越层/循环/孤儿
- `generated/` diff

这些会在后续批次补；不要因为它们未机器化就跳过 `_meta` 的规则。

---

## 5. 字段节点怎么读

字段节点的硬/软分类、`required`、`default`、`stability` 等通用语义见本节；
类型族在 `_types.yaml`，`data.tables.yaml` 只保留表契约特有的字段规范。

| 键 | 用途 |
|---|---|
| `description` | 字段的准确含义 |
| `required` / `type` / `enum` / `allowed` / `pattern` / `const` / `unique` / `min` / `max` / `min_items` | 结构约束；`min_items` 只约束 list；`map<string>` 是通用 schema 类型（键 → 字符串映射） |
| `examples` | 列参数/骨架的示例值；实体字段不再支持 |
| `column_types` | `_types.yaml` 中的类型族策略：`families` 是类型族映射，每个族声明 `description` 与 `params`；参数可带 `required`、`min`、`max`、`enum`、`default`、`description`；`min`/`max` 的 `type` 使用所属族名，不使用抽象 number |
| `columns[].type` | 类型族键，或硬编码的 `data.tables.<t>.col.<c>`；不是类型族，也不是外键；表列不引用 `data.enums.*` |
| `targets` | `dir`/`glob` 扫描路径；`file` 是单文件契约的固定路径；`id` 是 ID 模板（`<file>` → 文件 stem；单例契约直接写固定 ID）；`naming` 是文件名规则（`snake_case`）；基类固定 `_base.yaml`，不在此声明 |
| `columns[].redundancy` | 冗余组 ID；组明细（source/copies）在 `data/redundancy.yaml` 的 `groups` |
| `indexes[].comment` | 索引必须写非空用途说明；生成 `COMMENT ON INDEX` |
| `indexes[].used_by` | 使用该索引的 query item ID 列表；至少一个，防空索引 |
| `queries[].index_ref` | query 使用的索引：`data.tables.<t>.pk` 或 `data.tables.<t>.<index-name>`；与 `indexes[].used_by` 双向一致 |
| `data/storage.yaml` 的 `tables` | 每张物理表的 engine（必填）/size_class/retention/partition；表契约不再承载这些物理事实 |
| `data/redundancy.yaml` 的 `groups` | 冗余组明细：`source` + `copies`；与表列上的 `redundancy` 双向一致 |
| `generated` / `write` | 生成物标记；`write: forbidden` 表示禁止手改 |
| `rules` | 本类型的专有规则；每条必须写 `implemented_by`（含规则自身 ID，指向 checker 实际输出的 `[rule-id]`）；通用结构规则由 checker 代码强制 |

**先读 `description` 和 `rules`，不要只按字段名猜。**

通用结构规则（`schema.id-from-file`、`schema.contract-id-unique`、`schema.unique-names`、`schema.duplicate-key`）由 checker 代码强制，不再维护独立的 YAML 清单。实例的 `enforcement` / `consumers` 以 `index.yaml` 为准。类型级 `do_not` 已移到组 README 或 `<type>/_base.yaml`，`_meta` 下禁止出现 `do_not`。

### 5.1 `stability` 流转

| 状态 | 含义 | 进入条件 |
|---|---|---|
| `draft` | 正在编写 | 默认状态；允许 TODO/FIXME |
| `stable` | 评审通过、无 TODO | 替换所有 TODO/FIXME，`make contract-check` 通过 |
| `deprecated` | 已废弃 | 停止新增引用，准备移除 |

状态可双向流转：`stable → draft` 表示重新修改；`deprecated → stable` 表示恢复。
但 `stable` 的进入与退出只能由**用户手动修改**；`contract-new` 等生成器只产出 `draft`，AI 不得自动把文件改为 `stable`，也不得擅自把 `stable` 降级。`contract-check` 只能校验状态自洽（`stable` 不得含 `TODO` / `FIXME`），不能识别修改者，最终由 PR 评审把关。

状态的**判断**仍由 PR 评审负责；`contract-check` 只负责当前状态自洽。

---

## 6. 不要做的事

- 不要重建 `contracts/_meta/schema/` 或 `contracts/_meta/templates/`。
- 不要在 `_meta` 之外再手写一份字段模板；需要样板时用 `make contract-new`。
- 不要把 `_meta` 或脚本放进 `contracts/generated/`。
- 不要手写 `used_by`；它由 `make gen-graph` 写入，`contract-check` 校验。
- 不要只跑 `make gen` 而跳过 `make contract-check`。
- 不要把硬约束只写进 `rules` 而不写 `implemented_by`；`contract-check` 会校验 `implemented_by` 必须包含规则自身 ID、每个 ID 都有实际输出，并反向检查所有非 `schema.*` 输出都已被规则声明；但不会判断检查逻辑本身是否正确（后者靠测试）。
- 不要在本文件之外再写一份通用字段语义；类型文件只写类型特有内容。
- 不要把 `column_types` 写回 `data.tables.yaml`；类型族只放在 `_types.yaml`。
- 不要在 checker 里解析 `CHECK expr` 的 SQL 语义；checker 只校验非空，语义属于生成器/数据库验证的软区。
- 不要在 `_meta` 下写 `do_not`；不要在具体契约写顶层 `rules`（`business/rules` 与 `platform` 的领域规则集除外）。`contract-check` 的 `schema.meta-placement` 会拦住。
- 不要在 `contracts/_meta/` 里写业务实例；它只描述类型。
- 不要为单个表/页面各写一份规范；按类型一份。
