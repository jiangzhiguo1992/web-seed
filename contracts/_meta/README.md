# 契约字段规范（_meta）

> 本目录是**契约的单一源**：字段树、值域、类型级规则与校验入口。
> 不是业务契约：不登记 `contracts/index.yaml`，不进入 `generated/`。
> `contracts/generated/` 只放机器生成物；人读生成视图在 `docs/contracts/generated/`。

---

## 1. 目录

| 文件 | 职责 |
|---|---|
| `_types.yaml` | 共享值类型：`type_families`（类型族 + 参数）与 `custom_types`（family + 固定参数） |
| `_schemas.yaml` | `cap.services.schemas` 与 `cap.events.payload` 共用的受限 schema DSL |
| `data.tables.yaml` | 表、列、约束、索引；`storage.yaml` 的引擎级事实也由本文件的 `storage-*` 规则校验 |
| `data.enums.yaml` | 跨前后端枚举集合 |
| `data.entities.yaml` | 读模型字段树与关系 |
| `data.queries.yaml` | 查询接口（入参/结果/表/索引/缓存/归档），不写 SQL |
| `cap.services.yaml` | 服务与 steps（数据流、分支、事务） |
| `cap.events.yaml` | 异步 topic 与 job |
| `cap.lifecycles.yaml` | 实体状态机 |
| `cap.protocols.http.yaml` | OpenAPI 3.1 HTTP 绑定 |
| `feature.pages.yaml` | 页面契约 |
| `feature.rules.yaml` | 可执行业务规则；所有规则都是 error，违反即阻止提交 |

`scripts/contract/scaffold.sh`（`make contract-new`）与 `scripts/contract/validate.sh`
（`make contract-check`）是薄适配器；`scripts/cmd/` 是独立 Go module，schema 解析与校验都在那里。

**没有 `schema/`、没有 `templates/`。** 字段规范只在 `_meta/<type>.yaml`；`contract-new` 从字段树渲染最小骨架，不维护第二份模板。

### 1.1 `_types.yaml`

值类型是 tables / entities / queries / services / events 共用的词汇：

- `type_families` 定义族与参数（`min_len`/`max_len`/`pattern`/`bits`/`precision`/...）；
- `custom_types` 固定某个族的参数，供表列与 schema 标量复用；
- `columns[].type` 允许类型族、自定义类型或 `data.tables.<t>.col.<c>`；表列不引用 `data.enums.*`；
- 新增族参数必须登记 `SchemaParamTargets`（消费方：openapi/storage/runtime），checker 会拒绝无消费方的参数。

### 1.2 `_schemas.yaml`

schema 节点只描述值域：`object`（`type: object` + `properties`）、`array`（`type: array` + `items`）、
`scalar`（`type` 指向 `_types` 的族或自定义类型）、`ref`（entity / enum / 全限定本地 schema）。

- `object.properties[]` 是槽位条目：`name` + `schema` + `required`/`nullable`/`read_only`/`write_only`/`default`/`description`；
- 三个正交轴：`required` 管键是否存在，`nullable` 管值是否可为 null，`read_only`/`write_only` 管方向；
- 槽位键出现在节点位置报 `schema.slot-position`；
- `_schemas.yaml` 用 `kinds.<kind>.keys` 描述自身；判别键与结构键必须 `required: true`；
- `cap.protocols.http` 使用完整 OpenAPI 3.1，不复用这份 DSL。

---

## 2. AI 写契约的顺序

```text
1. 读 contracts/index.yaml（定位要改的契约）
2. 读 contracts/_meta/<type>.yaml（字段含义、值域、规则）
3. 新建：make contract-new type=<type> name=<name> ...
4. 改 YAML（只改契约，不写生成物）
5. make gen-graph（写回生成字段）
6. make contract-check
7. make gen
8. git diff --exit-code
```

`make gen` 是“契约 → 产物”的投影，不是“生成契约”。新建契约用 `make contract-new`。

---

## 3. 命令

| 命令 | 作用 |
|---|---|
| `make contract-new type=<type> name=<name> [module=...] [table=...] [service=...]` | 按 `_meta/<type>.yaml` 渲染最小骨架；`feature.pages` 无骨架，需手写 |
| `make gen-graph` | 写回 `table.queries` / `model.queries` / `query.used_by` / 规则与生命周期锚点 |
| `make gen-graph-check` | 只校验生成字段是否最新 |
| `make contract-check` | 结构 / ID / 约束 / 索引 / 引用 / owner / 数据流 / 类型级规则 |
| `make gen` | 生成 DDL / OpenAPI / 错误文档 |
| `make gen-check` | 校验全部生成物最新 |
| `make gen-rule-ids` | 从 checker 的 `rulef("...")` 重新生成 `rule_ids_gen.go` |
| `make gen-rule-tests` | 生成 `docs/contracts/generated/rule-tests.md`（规则 → `implemented_by` → 测试直接引用数） |
| `make test-rules` | 运行 `feature.rules` 的 policy 行为断言骨架（`go test -tags rules`） |

`contract-check` 已接入 `lefthook` pre-commit（`contracts/**/*.yaml`）与 CI。

---

## 4. 校验机制

- `_meta/<type>.yaml` 由 `KnownFields` 严格解析：拼错字段（如 `requred`）直接失败。
- 实例结构由 `fields` / `item_fields` 的 `required` / `type` / `enum` / `pattern` / `unique` /
  `unique_key` / `min` / `max` / `min_items` / `const` / `types` / `key_pattern` 校验；`soft` 标记不解析的 soft zone。
- 字段顺序以模板为准（`checkFieldOrder`）；新增字段只改 `_meta` 模板。
- `contracts/**/*.yaml` 的文本字段必须双引号（`schema.quote`）；同一 mapping 不得重复键。
- 类型级 `rules` 的每条必须有 `implemented_by`，包含自身 ID，且每个 ID 都是 checker 实际输出的
  `[rule-id]`；反向检查所有非 `schema.*` 输出都必须被某条规则声明。
- `rule_ids_gen.go` 由 `make gen-rule-ids` 从 `rulef("...")` 生成；独立二进制也带完整映射。
- `docs/contracts/generated/rule-tests.md` 由 `make gen-rule-tests` 生成，`refs` 为 0 表示该规则只有实现、没有测试直接断言；`make gen-check` 会阻止映射表过期，并按 `-max-zero` 阈值阻止新增未覆盖规则。
- `feature.rules` 的 policy 规则在 `scripts/cmd/internal/check/rules_policy_test.go`（`make test-rules`）自动登记，并校验 `implemented_by`/`on_violation`/`given`/`when`/`then`；业务行为断言由实现层补充。
- `rules` 只允许出现在 `_meta`（`features/rules` 等领域规则集除外）；`do_not` 只允许出现在具体契约。
- `generated: true` 字段必须同时 `write: forbidden`；由 `make gen-graph` 写入，checker 对账。

---

## 5. 字段节点怎么读

| 键 | 用途 |
|---|---|
| `description` | 字段的准确含义；软区标记也写在这里 |
| `required` / `type` / `types` / `enum` / `allowed` / `pattern` / `key_pattern` / `const` / `unique` / `unique_key` / `min` / `max` / `min_items` | 结构约束；`types` 是 schema 节点种类列表（与 `type` 互斥）；`key_pattern` 约束 map 键；`min_items` 只约束 list |
| `soft` | 无结构/语义校验的 soft zone（只允许 `any`/`string`） |
| `examples` | 骨架示例值；实体字段不支持 |
| `fields` / `item_fields` | 嵌套对象 / 对象列表的字段树 |
| `targets` | `dir`/`glob` 扫描路径；`id` 是 ID 模板（`<file>` → 文件 stem）；`naming` 是文件名规则；基类固定 `_base.yaml`，不在此声明 |
| `generated` / `write` | 生成物标记；`write: forbidden` 表示禁止手改 |
| `rules` | 本类型的专有规则；每条必须有 `implemented_by` |

**先读 `description` 和 `rules`，不要只按字段名猜。**

`enforcement` / `consumers` 以 `contracts/index.yaml` 为准；类型级 `do_not` 放在组 README 或 `<type>/_base.yaml`。

### 5.1 `stability` 流转

| 状态 | 含义 | 进入条件 |
|---|---|---|
| `draft` | 正在编写 | 默认状态；允许 TODO/FIXME |
| `stable` | 评审通过、无 TODO | 替换所有 TODO/FIXME，`make contract-check` 通过 |
| `deprecated` | 已废弃 | 停止新增引用，准备移除 |

`stable` 的进入与退出只能由**用户手动修改**（`contract-new` 只产出 `draft`）；`contract-check` 只能校验
`stable` 不含 TODO/FIXME，不能识别修改者。

---

## 6. 不要做的事

- 不要重建 `contracts/_meta/schema/` 或 `templates/`；不要在 `_meta` 之外手写第二份字段模板。
- 不要手写生成字段（`table.referenced_by` / `table.queries` / `model.included_by` / `model.queries` /
  `query.used_by` / `feature.rules.implemented_by` / 生命周期锚点 / `emitted_by` / `consumers`）。
- 不要只跑 `make gen` 而跳过 `make contract-check`。
- 不要把硬约束只写进 `rules` 而不写 `implemented_by`。
- 不要在 checker 里解析 `CHECK expr` 的 SQL 语义；只校验可判定的字面量。
- 不要在 `_meta` 下写 `do_not`；不要在具体契约写顶层 `rules`。
- 不要在 `contracts/_meta/` 里写业务实例；按类型一份规范。
