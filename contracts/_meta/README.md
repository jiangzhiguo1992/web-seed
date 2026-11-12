# 契约字段规范（_meta）

> 本目录是**契约的单一源**：字段树、值域、字段含义、跨字段规则与校验入口。
> 它不是业务契约，不登记在 `contracts/index.yaml`，也不进入 `generated/`。
> `contracts/generated/` 仍然只放**产物**：manifest / graph / inbound / openapi / db。

---

## 1. 目录

```
contracts/_meta/
├── README.md             # 本文件：AI 入口
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

### 1.1 为什么没有 `_types.yaml`

类型级信息只有两类：

- **可执行规则**：`schema.*` 由 checker 代码直接强制，不写 YAML 清单；
- **字段规范**：每类一份 `_meta/<type>.yaml`，包含字段树、值域、类型规则和 `targets`；

不再维护“元数据的元数据”第三份文件；实例的 `enforcement` / `consumers` 以 `index.yaml` 为准。

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

`contract-new` 支持 `data.tables` / `data.enums` / `data.entities` / `data.queries`；entity/query 骨架要求目标表契约已存在，entity 默认生成 lowerCamel 的 internal `model.name`，需要 public 时改成 PascalCase；query 默认生成只依赖主键的 `SELECT EXISTS`，不会伪造业务字段。`contract-new` 只生成 `required` 字段和“含 required/default 的类型族扩展字段”；`table.retention`、`table.partition`、`columns.pii` 等可选字段不强行展开，避免生成无意义默认值。`status` 固定 `bits: 32`，不自动写 `default`（新表的状态整数码尚未确定）。生成后仍需跑 `contract-check` 完成硬约束校验。`data.enums` 片段用 `code: -1` 与 TODO 描述占位，`contract-check` 会拒绝，直到替换为真实值。

---

## 4. 当前试点范围

`contract-check` 当前覆盖：

- `_meta/<type>.yaml` 字段规范按 `KnownFields` 严格解析：未知字段（如 `requred`）直接失败，不再静默忽略
- `data.tables/*.yaml`
  - `targets.dir/glob` 决定扫描路径；`targets.id/naming` 由 checker 校验（ID 模板、snake_case）
  - `column_refs.prefixes` 必须以 `.` 结尾；`require_col` 必须是 `prefixes` 的子集
  - 必填字段、类型、枚举、`const`、`pattern`、`allowed`、`unique`
  - `id` 按 `targets.id` 模板生成；基类 `_base.yaml` 的路径/ID 写死在 checker，不参与模板
  - 物理表 `extends` 是父表列表：可指向 `data.tables.base` 或任意已存在的表；多父同名列只有来源相同（钻石继承）才允许；链不得成环且必须到达基类；不得重复任何继承列
  - `primary_key` 列不得可空
  - `default` / `default_sql` 互斥；`default` 的 YAML 类型、长度、范围与列类型族兼容
  - 列 / 约束 / 索引重名；`do_not` 每条必须含本表具体标识
- `data.tables.base`（`contracts/data/tables/_base.yaml`，单例）
  - 路径/ID 由 checker 写死识别；YAML 无 `kind`/`targets` 特殊配置
  - 基类必须且只能是 5 列：`id` string 主键、`status` integer 非空且非主键、三个时间列 timestamp（`created_at`/`updated_at` 非空、`deleted_at` 可空）
  - `do_not` 直接写在 `_base.yaml`；基类 `do_not` 允许类型级通用规则，物理表 `do_not` 必须含本表标识
  - 规则 `base-file`、`base-columns` 在 `_meta/data.tables.yaml` 的 `rules` 中声明
  - `constraints` 的 `type`、按类型允许的字段、必填子字段与列引用；表不使用 enum 约束
  - 禁止 `foreign_key`；跨表一致性由应用/业务规则保证
  - `unique` 约束与唯一索引的软删语义不同：约束含软删行，唯一索引按 `where` 过滤
  - `indexes` 的 `reason`、`columns` xor `expression`、`order` 长度/方向、唯一索引软删过滤、命名前缀、列引用；`used_by` 至少一个查询项、查询引用当前表且 SQL 提及索引列；large 表不得显式禁用 `concurrently`
  - `columns.redundancy`：同组唯一 `source`、至少一个 `copy`；`copy.ref` 指向同组 `source`，`type`/`nullable` 一致
  - `data/queries/*.yaml` 的 `index_columns`：每个内层列表按顺序匹配表索引最左前缀或主键；匹配的显式索引必须在 `used_by` 中列出该 query
  - `table.partition.columns` 存在；range 必须有 `interval`，其他策略不得有
  - `table.retention` 的数值 >= 1，`unit` 只能是 minutes/hours/days
  - `used_by` 只记 `entities` 与 `queries`，与实际入边一致
  - `owner_module` 与 capabilities 写表归属一致；跨模块读通过 `persistence.queries`，`read_only_tables` 已移除
  - `persistence.queries` 引用的查询项必须存在；`persistence.constraint_errors` 的键必须是所写表的约束或索引名
  - 索引名与 `unique`/`exclude` 约束名共用 PG 索引命名空间，必须全局唯一
  - contracts 下文本字段（`description` / `comment` / `summary` / `do_not` 等）必须使用双引号（`contract-check` 校验）
  - 所有契约的顶层 `fields` 必须竖排（block style），嵌套 `item_fields` 不限制（`schema.fields-style`）
  - 具体契约的 `do_not` 不得与组 README / `_base` 的指导重复（`schema.do-not-duplicate`）
  - `rules` 只允许出现在 `_meta`；`business/rules/*`、`platform/architecture/go_arch.yaml`、`platform/observability/log_fields.yaml` 的 `rules` 是领域规则集正文，不属于类型规则清单。具体契约的 `do_not` 只写实例级；`_meta` 下不得出现 `do_not`（`schema.meta-placement`）
  - `rules` 的 `id`/`text` 非空、`id` 唯一；`implemented_by` 必须包含规则自身 ID，且每个值都指向 checker 实际输出的 `[rule-id]`（`schema.rules`）
  - checker 输出的非 `schema.*` 错误 ID 必须由某个类型的 `rules` 声明，反向防止“有实现没规则”
  - `rule_ids_gen.go` 由 `make gen-rule-ids` 从 `rulef("...")` 生成；`make contract-check` 校验它是否最新
  - `_meta/*.yaml` 以 `type` 作为类型 registry；checker/contract-new 按 type 查找，不再写死 meta 路径
  - 错误统一带 `[rule-id]` 前缀：类型规则用 `_meta/<type>.yaml` 的 `rules`，通用结构/格式用 `schema.*`（含 `schema.targets`）
- `data/enums.yaml`
  - 集合 ID、item ID = `data.enums.<name>`、值唯一、`code` 唯一且为 int32（0..2147483647）
  - `name` / `go_type` / `ts_type` 在集合内唯一；类型名冲突会让 Go/TS 产物互相覆盖
  - `summary` 非空；`stability: stable` 不得含 TODO/FIXME
  - 任意契约（entity / capability / feature 等）的 `type: data.enums.*` 必须存在；表列仍禁止引用枚举
  - `used_by.entities/tables` 必须与 entity 字段引用完全一致；只列 data.entities/data.tables，capability/feature 引用不写入 used_by，空列表合法
- `data.entities.base`（`contracts/data/entities/_base.yaml`，单例）
  - 路径/ID 由 checker 写死识别；5 个字段固定为 `ID` / `Status` / `CreatedAt` / `UpdatedAt` / `DeletedAt`
  - `ID` string 非空、`Status` integer 非空、时间列 timestamp（`CreatedAt`/`UpdatedAt` 非空、`DeletedAt` 可空）；`ID` 与三个时间列 `mutable:false`
  - `do_not` 直接写在 `_base.yaml`，承载 `data.entities` 类型级通用规则；具体实体 `do_not` 必须含本实体标识
- `data/entities/*.yaml`
  - 结构按 `_meta/data.entities.yaml`；`model.extends` 必须是 `[data.entities.base]`；具体实体不得重声明基类字段，`Status` 可用 `data.enums.*` 特化
  - `model.tables` 必填、非空、去重；`model.tables[0]` 是主表；每张物理表至少一个 entity 映射
  - `owner_module` 必填且必须等于主表 `model.tables[0]` 的 `owner_module`
  - 字段必须投影回列：单表可省略 `column`，多表必须写；同一列不得被两个字段引用
  - `required` 必须等价于来源列非空；`pii`/`never_expose` 不得放松；显式类型复用 `data.tables` 类型族，或 `data.enums.*`
  - 实体 `model.name` 是 Go 类型名：首字母大写 public，小写 internal；`model` 顺序为 name/extends/tables；不得重名
  - `ID` 是所有实体的唯一主键：来自主表 `id` 列，required、mutable:false、唯一主键
  - 字段 `name` 是 Go 字段名：首字母大写导出，小写不导出；`never_expose` 字段必须小写开头
  - 默认列名是 `snake_case(name)`，可用 `column` 覆盖；`json_name` 必填且同实体唯一
  - 字段可声明生成语义：`examples`、`description`；`examples` 元素类型必须与字段类型一致
  - `relations` 的 target/via/方向/类型一致性；eager 必须有索引且不得成环
  - `validation` 的 id 前缀、expr 非空、`on_violation` 必须存在
  - `used_by.queries` 与 returns 本实体的 query 完全一致；`used_by.entities` 与 relations 入边一致
- `data/queries/*.yaml`
  - 结构按 `_meta/data.queries.yaml`；query id 前缀、name=PascalCase、name 全局唯一
  - 不再手写 `table`/`tables`：表从 `sql.from` / `sql.into` / `sql.joins[].table` 推导；SQL 必须声明 from/into，表必须存在
  - params 与 SQL 双向一致；`nullable` 与 `sqlc.narg` 严格对应
  - `returns` DSL：表行 / 实体 / 标量 / object；`[]` 与 kind 一致；object 必须有 output_fields
  - `return_columns` 必须是物理列；实体返回必须覆盖 required 非敏感字段
  - 单表 query 必须有 `index_columns` 且列出现在 SQL；多表 query 不得写，也不得进索引 `used_by`
  - 写查询的 module 必须等于所写表 `owner_module`；`never_expose` 列只有同模块可返回
  - 禁止 `SELECT *`；`one` 非标量返回必须命中唯一索引或 `sql.limit: "1"`
- 实体/查询/能力对表的引用存在
- 查询 `return_columns` 必须是物理列（实体返回也按列名）
- `index.yaml` 的 path 存在、path 与 id 一致、id/path 不重复；entities/queries 文件必须登记

**尚未机器化**（写在 `_meta` 的 `description` / `caution` / `rules`，靠 review）：

- 命名品味、业务语义、跨模块读归属是否最小化
- 其余契约类型规范（http/services/...）
- `graph.json` 越层/循环/孤儿
- `generated/` diff

这些会在后续批次补；不要因为它们未机器化就跳过 `_meta` 的规则。

---

## 5. 字段节点怎么读

字段节点的硬/软分类、`required`、`default`、`stability` 等通用语义见本节；
`data.tables.yaml` 只保留 data.tables 特有的 `column_types` / `column_refs`。

| 键 | 用途 |
|---|---|
| `description` | 字段的准确含义 |
| `generates` | 生成器会产出什么 / 怎么消费 |
| `caution` | AI 最容易写错的用法 |
| `required` / `type` / `enum` / `allowed` / `pattern` / `const` / `unique` / `min` / `max` / `min_items` | 结构约束；`min_items` 只约束 list，`list<list<string>>` 用于 `index_columns` |
| `examples` | 示例值；`contract-new` 可用于生成 |
| `column_types` | 类型族策略：`families` 是类型族映射，每个族声明 `description` 与 `params`；参数可带 `required`、`min`、`max`、`enum`、`default`、`description`；`min`/`max` 的 `type` 使用所属族名，不使用抽象 number |
| `column_refs` | `prefixes` 是 `columns[].type` 允许的引用命名空间；`require_col` 中的前缀必须带 `.col.`；不是类型族，也不是外键；表列不引用 `data.enums.*` |
| `targets` | `dir`/`glob` 扫描路径；`file` 是单文件契约的固定路径；`id` 是 ID 模板（`<file>` → 文件 stem；单例契约直接写固定 ID）；`naming` 是文件名规则（`snake_case`）；基类固定 `_base.yaml`，不在此声明 |
| `columns[].redundancy` | 冗余字段声明：`group`/`role`（source/copy）/`ref`；copy 指向同组 source |
| `indexes[].used_by` | 使用该索引的 query item ID 列表；至少一个，防空索引 |
| `generated` / `write` | 生成物标记；`write: forbidden` 表示禁止手改 |
| `rules` | 本类型的专有规则；每条必须写 `implemented_by`（含规则自身 ID，指向 checker 实际输出的 `[rule-id]`）；通用结构规则由 checker 代码强制 |

**先读 `description` 和 `caution`，不要只按字段名猜。**

通用结构规则（`schema.id-from-file`、`schema.contract-id-unique`、`schema.unique-names`）由 checker 代码强制，不再维护独立的 YAML 清单。实例的 `enforcement` / `consumers` 以 `index.yaml` 为准。类型级 `do_not` 已移到组 README 或 `<type>/_base.yaml`，`_meta` 下禁止出现 `do_not`。

### 5.1 `stability` 流转

| 状态 | 含义 | 进入条件 |
|---|---|---|
| `draft` | 正在编写 | 默认状态；允许 TODO/FIXME |
| `stable` | 评审通过、无 TODO | 替换所有 TODO/FIXME，`make contract-check` 通过 |
| `deprecated` | 已废弃 | 停止新增引用，准备移除 |

状态可双向流转：`stable → draft` 表示重新修改；`deprecated → stable` 表示恢复。
`contract-check` 只校验 `stable` 文件不得含 `TODO` / `FIXME`，不阻止状态回退。

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
- 不要在 `_meta` 下写 `do_not`；不要在具体契约写顶层 `rules`（`business/rules` 与 `platform` 的领域规则集除外）。`contract-check` 的 `schema.meta-placement` 会拦住。
- 不要在 `contracts/_meta/` 里写业务实例；它只描述类型。
- 不要为单个表/页面各写一份规范；按类型一份。
