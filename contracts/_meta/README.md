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
| `make contract-check` | 结构 / ID / 基类 / 约束 / 索引 / used_by | 每次改完契约 |
| `make gen` | 生成 DDL / OpenAPI / 类型 / graph | `contract-check` 通过后 |
| `make gen-openapi` / `gen-db` / `gen-graph` / `gen-client` | 单独投影 | 按需 |

`contract-check` 已接入 `lefthook` 的 pre-commit（`contracts/**/*.yaml`）。

`contract-new` 只生成 `required` 字段和“含 required/default 的类型族扩展字段”；`table.retention`、`table.partition`、`columns.pii` 等可选字段不强行展开，避免生成无意义默认值。`status` 不自动写 `default`（新表的枚举值尚未确定，写入 `active` 会制造隐式假设）。生成后仍需跑 `contract-check` 完成硬约束校验。

---

## 4. 当前试点范围

`contract-check` 当前覆盖：

- `data.tables/*.yaml`
  - 必填字段、类型、枚举、`const`、`pattern`、`allowed`、`unique`
  - `id` 与文件名一致；`_base.yaml` 特例；`kind: base` 只能出现在 `_base.yaml`
  - 物理表 `extends: [data.tables.base]`；不得重复基类列；`status` 必须重声明为 `data.enums.*`
  - `primary_key` 列不得可空
  - `default` / `default_sql` 互斥；`default` 的 YAML 类型、长度、范围与列类型族兼容
  - 列 / 约束 / 索引重名；`do_not` 每条必须含本表具体标识
  - `constraints` 的 `type`、按类型允许的字段、必填子字段与列引用；enum 约束与列类型一致
  - 外键 `columns` 与 `references` 数量一致，目标表/列存在
  - `indexes` 的 `reason`、`columns` xor `expression`、`order` 长度/方向、唯一索引软删过滤、命名前缀、列引用；large 表不得显式禁用 `concurrently`
  - `table.partition.columns` 存在；range 必须有 `interval`，其他策略不得有
  - `table.retention` 的值 >= 1
  - `used_by` 三列表与实际入边一致；`queries` 记录查询项 ID
  - `owner_module` 与 capabilities 写表归属一致；写表不得同时列在 `read_only_tables`
  - `data.enums.*` 引用存在；枚举列 `default` 必须是已有枚举值
- `data/enums.yaml`
  - 集合 ID、item ID = `data.enums.<name>`、值唯一、`db_type: text`
- 实体/查询/能力对表的引用存在
- 查询 `return_columns` 必须是表列
- `index.yaml` 的 path 存在、path 与 id 一致、id/path 不重复

**尚未机器化**（写在 `_meta` 的 `description` / `caution` / `rules`，靠 review）：

- 命名品味、业务语义、跨模块读归属是否最小化
- 全类型规范（entities/queries/http/services/...）
- `graph.json` 越层/循环/孤儿
- `generated/` diff

这些会在后续批次补；不要因为它们未机器化就跳过 `_meta` 的规则。

---

## 5. 字段节点怎么读

每个字段固定包含：

| 键 | 用途 |
|---|---|
| `description` | 字段的准确含义 |
| `generates` | 生成器会产出什么 / 怎么消费 |
| `caution` | AI 最容易写错的用法 |
| `required` / `type` / `enum` / `allowed` / `pattern` / `const` / `unique` / `min` / `max` | 结构约束 |
| `default` | required 字段上只影响 `contract-new` 骨架；optional 字段上表示省略时的语义默认值；`contract-check` 不补值 |
| `examples` | 示例值；`contract-new` 可用于生成 |
| `column_types` | 类型族策略：`families` 是类型族映射，每个族声明 `description` 与 `params`；每个参数可带 `required`、`min`、`max`、`enum`、`default`；`min`/`max` 的 `type` 使用所属族名（integer / float / decimal），不使用抽象 number；不是数据库物理类型 |
| `column_refs` | `prefixes` 是 `columns[].type` 允许的引用命名空间；不是类型族，也不是外键；与 `column_types` 分开 |
| `generated` / `write` | 生成物标记；`write: forbidden` 表示禁止手改 |
| `rules` | 跨字段规则的声明式清单；声称硬约束的规则必须在 `scripts/cmd/internal/check` 有对应实现，否则只是文档 |

**先读 `description` 和 `caution`，不要只按字段名猜。**

### 5.1 `stability` 流转

| 状态 | 进入条件 | 退出条件 |
|---|---|---|
| `draft` | 默认；正在编写 | 替换所有 `TODO`/`FIXME`，`make contract-check` 通过后改 `stable` |
| `stable` | 评审通过、无 TODO | 改 `deprecated`，停止新增引用，准备移除 |
| `deprecated` | 已废弃；不再新增引用 | 所有入边迁完并删除契约文件 |

状态只允许向前流转：

```text
draft → stable
draft → deprecated
stable → deprecated
```

`deprecated` 不能回到 `draft` 或 `stable`；`stable` 不能回退到 `draft`。

`contract-check` 能校验：

- `stable` 文件不得含 `TODO` / `FIXME`；
- 用 `git show HEAD:<file>` 对比上一版，禁止非法回退。

状态的**判断**仍由 PR 评审负责；`contract-check` 只负责当前状态自洽和方向不回退。

---

## 6. 不要做的事

- 不要重建 `contracts/_meta/schema/` 或 `contracts/_meta/templates/`。
- 不要在 `_meta` 之外再手写一份字段模板；需要样板时用 `make contract-new`。
- 不要把 `_meta` 或脚本放进 `contracts/generated/`。
- 不要手写 `used_by`；它由 `make gen-graph` 写入，`contract-check` 校验。
- 不要只跑 `make gen` 而跳过 `make contract-check`。
- 不要把硬约束只写进 `rules` 而不在 `contract-check` 实现；`rules` 是清单，不是执行器。
- 不要在 `contracts/_meta/` 里写业务实例；它只描述类型。
- 不要为单个表/页面各写一份规范；按类型一份。
