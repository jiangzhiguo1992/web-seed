# 契约字段规范（_meta）

> 本目录是**契约的单一源**：字段树、值域、字段含义、跨字段规则与校验入口。
> 它不是业务契约，不登记在 `contracts/index.yaml`，也不进入 `generated/`。
> `contracts/generated/` 仍然只放**产物**：manifest / graph / inbound / openapi / db。

---

## 1. 目录

```
contracts/_meta/
├── README.md             # 本文件：AI 入口
├── _types.yaml            # 每类契约的默认元数据（enforcement / consumers / 通用 do_not）
├── data.tables.yaml      # data.tables 的字段树 + 值域 + 说明
└── data.enums.yaml       # data.enums 的字段树 + 值域 + 说明

scripts/contract/
├── scaffold.sh           # make contract-new
└── validate.sh           # make contract-check

scripts/cmd/
├── go.mod                # 契约工具独立 Go module
├── internal/contractspec/ # schema 解析 + 骨架渲染
├── internal/check/       # 契约校验实现（tables / enums / refs）
├── contract_new/
└── contract_check/
```

**没有 `schema/`、没有 `templates/`。**
字段说明和值域在同一个文件、同一个字段节点里；`contract-new` 只生成最小骨架，字段规范以本目录为准，不维护第二份字段清单。

---

## 2. AI 写契约的顺序

```text
1. 读 contracts/index.yaml（定位要改的契约）
2. 读 contracts/_meta/<type>.yaml（字段含义、值域、生成行为、易错点）
3. 新建：make contract-new type=data.tables name=note module=notes
4. 改 YAML（只改契约，不写生成物）
5. make contract-check
6. make gen
7. git diff --exit-code
```

**`make gen` 是“契约 → 产物”的投影，不是“生成契约”。**
新建契约用 `make contract-new`；`make gen` 必须在契约写完之后跑。

---

## 3. 命令

| 命令 | 作用 | 何时跑 |
|---|---|---|
| `make contract-new type=data.tables name=note module=notes summary="备注表"` | 按 `_meta/data.tables.yaml` 生成最小表骨架；`column_type=decimal` 可指定第一列类型并带出扩展字段 | 新建表前 |
| `make contract-new type=data.enums name=note_status` | 按 `_meta/data.enums.yaml` 打印枚举项片段 | 新增枚举时 |
| `make contract-check` | 结构 / ID / 约束 / 索引 / used_by / owner / persistence.queries | 每次改完契约 |
| `make gen` | 生成 DDL / OpenAPI / 类型 / graph | `contract-check` 通过后 |
| `make gen-openapi` / `gen-db` / `gen-graph` / `gen-client` | 单独投影 | 按需 |

`contract-check` 已接入 `lefthook` 的 pre-commit（`contracts/**/*.yaml`）。

`contract-new` 只生成 `required` 字段和“含 required/default 的类型族扩展字段”；`table.retention`、`table.partition`、`columns.pii` 等可选字段不强行展开，避免生成无意义默认值。`status` 固定 `bits: 32`，不自动写 `default`（新表的状态整数码尚未确定）。生成后仍需跑 `contract-check` 完成硬约束校验。`data.enums` 片段用 `code: -1` 与 TODO 描述占位，`contract-check` 会拒绝，直到替换为真实值。

---

## 4. 当前试点范围

`contract-check` 当前覆盖：

- `_meta/<type>.yaml` 字段规范按 `KnownFields` 严格解析：未知字段（如 `requred`）直接失败，不再静默忽略
- `data.tables/*.yaml`
  - `targets.dir/glob` 决定扫描路径；`targets.id/naming/exception` 由 checker 校验（ID 模板、snake_case、基类例外）
  - `column_refs.prefixes` 必须以 `.` 结尾；`require_col` 必须是 `prefixes` 的子集
  - 必填字段、类型、枚举、`const`、`pattern`、`allowed`、`unique`
  - `id` 按 `targets.id` 模板生成；例外按 `targets.exception`；`kind: base` 只能出现在 base 例外文件
  - 物理表 `extends` 必须等于 `targets.exception` 的 base ID；不得重复基类列；`status` 必须重声明为 `integer`
  - 基类必须且只能是 5 列：`id` string 主键、`status` integer 非空、三个时间列 timestamp（`created_at`/`updated_at` 非空、`deleted_at` 可空）
  - `primary_key` 列不得可空
  - `default` / `default_sql` 互斥；`default` 的 YAML 类型、长度、范围与列类型族兼容
  - 列 / 约束 / 索引重名；`do_not` 每条必须含本表具体标识
  - `constraints` 的 `type`、按类型允许的字段、必填子字段与列引用；表不使用 enum 约束
  - 禁止 `foreign_key`；跨表一致性由应用/业务规则保证
  - `unique` 约束与唯一索引的软删语义不同：约束含软删行，唯一索引按 `where` 过滤
  - `indexes` 的 `reason`、`columns` xor `expression`、`order` 长度/方向、唯一索引软删过滤、命名前缀、列引用；`used_by` 至少一个查询项、查询引用当前表且 SQL 提及索引列；large 表不得显式禁用 `concurrently`
  - `columns.redundancy`：同组唯一 `source`、至少一个 `copy`；`copy.ref` 指向同组 `source`，`type`/`nullable` 一致
  - `data/queries/*.yaml` 的 `index_columns`：每个内层列表按顺序匹配表索引最左前缀或主键；匹配的显式索引必须在 `used_by` 中列出该 query
  - `table.partition.columns` 存在；range 必须有 `interval`，其他策略不得有
  - `table.retention` 的数值 >= 1，`unit` 只能是 minutes/hours/days
  - `used_by` 三列表与实际入边一致；`queries` 记录查询项 ID
  - `owner_module` 与 capabilities 写表归属一致；跨模块读通过 `persistence.queries`，`read_only_tables` 已移除
  - `persistence.queries` 引用的查询项必须存在；`persistence.constraint_errors` 的键必须是所写表的约束或索引名
  - 索引名与 `unique`/`exclude` 约束名共用 PG 索引命名空间，必须全局唯一
  - contracts 下文本字段（`description` / `comment` / `summary` / `do_not` 等）必须使用双引号（`contract-check` 校验）
  - `rules` 的 `id`/`text` 非空、`id` 唯一；`implemented_by` 必须包含规则自身 ID，且每个值都指向 checker 实际输出的 `[rule-id]`（`schema.rules`）
  - checker 输出的非 `schema.*` 错误 ID 必须由某个 `rules`/`schema_rules` 声明，反向防止“有实现没规则”
  - `rule_ids_gen.go` 由 `make gen-rule-ids` 从 `rulef("...")` 生成；`make contract-check` 校验它是否最新
  - `_meta/*.yaml` 以 `type` 作为类型 registry；checker/contract-new 按 type 查找，不再写死 meta 路径
  - 错误统一带 `[rule-id]` 前缀：表专有规则用 `data.tables.yaml` 的 `rules`，跨类型规则用 `_types.yaml` 的 `schema_rules` / `schema_semantics`，结构/格式用 `schema.*`（含 `schema.targets`）
- `data/enums.yaml`
  - 集合 ID、item ID = `data.enums.<name>`、值唯一、`code` 唯一且 >= 0
  - `name` / `go_type` / `ts_type` 在集合内唯一；类型名冲突会让 Go/TS 产物互相覆盖
  - `summary` 非空；`stability: stable` 不得含 TODO/FIXME
  - 任意契约（entity / capability / feature 等）的 `type: data.enums.*` 必须存在；表列仍禁止引用枚举
  - `enum-append-only` 对比 HEAD；PR merge ref 自动使用 HEAD^1（CI checkout 需完整历史）
- 实体/查询/能力对表的引用存在
- 查询 `return_columns` 必须是表列
- `index.yaml` 的 path 存在、path 与 id 一致、id/path 不重复

**尚未机器化**（写在 `_meta` 的 `description` / `caution` / `rules` / `schema_rules`，靠 review）：

- 命名品味、业务语义、跨模块读归属是否最小化
- 全类型规范（entities/queries/http/services/...）
- `graph.json` 越层/循环/孤儿
- `generated/` diff

这些会在后续批次补；不要因为它们未机器化就跳过 `_meta` 的规则。

---

## 5. 字段节点怎么读

字段节点的硬/软分类、`required`、`default`、`stability` 等通用语义见
`_types.yaml` 的 `schema_semantics`。本文件只保留 data.tables 特有的 `column_types` / `column_refs`。

| 键 | 用途 |
|---|---|
| `description` | 字段的准确含义 |
| `generates` | 生成器会产出什么 / 怎么消费 |
| `caution` | AI 最容易写错的用法 |
| `required` / `type` / `enum` / `allowed` / `pattern` / `const` / `unique` / `min` / `max` | 结构约束 |
| `examples` | 示例值；`contract-new` 可用于生成 |
| `column_types` | 类型族策略：`families` 是类型族映射，每个族声明 `description` 与 `params`；参数可带 `required`、`min`、`max`、`enum`、`default`、`description`；`min`/`max` 的 `type` 使用所属族名，不使用抽象 number |
| `column_refs` | `prefixes` 是 `columns[].type` 允许的引用命名空间；`require_col` 中的前缀必须带 `.col.`；不是类型族，也不是外键；表列不引用 `data.enums.*` |
| `targets` | `dir`/`glob` 扫描路径；`id` 是 ID 模板（`<file>` → 文件 stem；单例契约直接写固定 ID）；`naming` 是文件名规则（`snake_case`）；`exception` 是 `{file,id,kind}` 例外，`kind: base` 标记基类 |
| `columns[].redundancy` | 冗余字段声明：`group`/`role`（source/copy）/`ref`；copy 指向同组 source |
| `indexes[].used_by` | 使用该索引的 query item ID 列表；至少一个，防空索引 |
| `generated` / `write` | 生成物标记；`write: forbidden` 表示禁止手改 |
| `rules` | 本类型的专有规则；每条必须写 `implemented_by`（含规则自身 ID，指向 checker 实际输出的 `[rule-id]`）；跨类型规则放 `_types.yaml` 的 `schema_rules` |

**先读 `description` 和 `caution`，不要只按字段名猜。**

`_types.yaml` 的 `schema_rules` 存放跨类型规则（`id-from-file`、`contract-id-unique`、`unique-names`、`default-vs-default-sql`、`default-type`、`primary-key-nullable`、`do-not-specific`）；`data.tables.yaml` 的 `rules` 只保留表专有规则，避免同一规则在多个类型文件中重复。

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
- 不要把硬约束只写进 `rules` / `schema_rules` 而不写 `implemented_by`；`contract-check` 会校验 `implemented_by` 必须包含规则自身 ID、每个 ID 都有实际输出，并反向检查所有非 `schema.*` 输出都已被规则声明；但不会判断检查逻辑本身是否正确（后者靠测试）。
- 不要在 `data.tables.yaml` 重复 `_types.yaml` 的通用字段语义；类型文件只写类型特有内容。
- 不要在 `contracts/_meta/` 里写业务实例；它只描述类型。
- 不要为单个表/页面各写一份规范；按类型一份。
