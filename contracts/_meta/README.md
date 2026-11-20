# 契约字段规范（_meta）

> 本目录是**契约的单一源**：字段树、值域、类型级规则与校验入口。
> 不是业务契约：不登记 `contracts/index.yaml`，不进入 `generated/`。
> `contracts/generated/` 只放机器生成物；人读生成视图在 `docs/contracts/generated/`。

---

## 1. 目录

| 文件 | 职责 |
|---|---|
| `_types.yaml` | 统一类型注册表：`basic_types`（标量族 + 参数）、`structures`（list/set/array/map/object）、`schemas`（object/array/scalar/ref 节点）、`param_targets`；项目级自定义类型在 `contracts/data/types.yaml` |
| `data.tables.yaml` | 表、列、约束、索引 |
| `data.storage.yaml` | 存储拓扑单例：引擎、表级保留、缓存、归档 |
| `data.enums.yaml` | 跨前后端枚举集合 |
| `data.entities.yaml` | 读模型字段树与关系 |
| `data.queries.yaml` | 查询接口（入参/结果/表/索引/缓存/归档），不写 SQL |
| `cap.services.yaml` | 服务与 steps（数据流、分支、事务） |
| `cap.events.yaml` | 异步 topic 与 job |
| `cap.lifecycles.yaml` | 实体状态机 |
| `cap.protocols.http.yaml` | OpenAPI 3.1 HTTP 绑定 |
| `feature.pages.yaml` | 页面契约 |
| `feature.rules.yaml` | 可执行业务规则；所有规则都是 error，违反即阻止提交 |
| `platform.modules.yaml` | 模块注册表（owner_module 来源） |
| `platform.security.permissions.yaml` | 权限点 |
| `platform.security.roles.yaml` | 角色与权限映射 |
| `platform.security.auth_policy.yaml` | 认证、会话、令牌与密码策略 |

`scripts/contract/scaffold.sh`（`make contract-new`）与 `scripts/contract/validate.sh`
（`make contract-check`）是薄适配器；`scripts/cmd/` 是独立 Go module，schema 解析与校验都在那里。

**没有 `schema/`、没有 `templates/`。** 字段规范只在 `_meta/<type>.yaml`；`contract-new` 从字段树渲染最小骨架，不维护第二份模板。

### 1.1 `_types.yaml` 与 `data/types.yaml`

值类型与结构是 tables / entities / queries / services / events 共用的词汇，分两类注册：

- `_meta/_types.yaml` 的 `basic_types` 定义标量族与参数（`min_len`/`max_len`/`pattern`/`bits`/`precision`/...），`structures` 定义 `list`/`set`/`array`/`map`/`object` 五种容器（各自带 `min_len`/`max_len`/`len`/`key_patterns`/`additional_properties` 参数；元素、map value 或 object field 的 `type` 可以是 `_types` 下任意已定义类型，递归包含 `basic_types`/`custom_types`/`structures`），`schemas` 定义 `object`/`array`/`scalar`/`ref` 四种值域节点；这是元层，不随业务增长；
- `contracts/data/types.yaml` 的 `custom_types` 固定某个族的参数，供表列与 schema 标量复用；这是 data 层词汇，随项目开发增长；
- `Field.type` 统一写 `_types.<name>`，泛型写 `_types.list<_types.object>` / `_types.map<_types.string>`，可嵌套到 `_types.list<_types.map<_types.object>>`；`list<map<object>>` 的 `item_fields` 描述 map value 的字段；`list<map>` 不合法，`map` 必须显式给出 value 类型；`map` 的 key 默认 `string`，可写 `_types.map<_types.integer, _types.object>` 指定 `integer` key；`key_patterns` 是 string key 的正则列表，全部匹配；`_types.yaml` 自身是 bootstrap，允许裸名；
- `structures.list` / `set` / `array` 声明 `fields.item`，`map` 声明 `fields.key` / `value`（均为 `type: "*"`、`required: true`），生成器与实现层统一使用这些名字；checker 按 `Field.type` 递归校验，不再为每个容器组合维护独立分支；`key_patterns` 是 AND 语义，多个正则必须同时匹配，且只允许 string key；
- `list<object>` / `set<object>` / `array<object>` 必须声明 `item_fields`；空对象列表用 `list<any>`，不要用无字段的 `list<object>`；
- `columns[].type` 必须是 `_types.<name>`（类型族或自定义类型）或 `data.tables.<t>.col.<c>`；表列不引用 `data.enums.*`；
- schema 的 `scalar.type` 必须是 `_types.<name>`；entity 字段的 `type` 是 Go 类型，不加前缀；
- 新增族参数必须在 `_types.yaml` 的 `param_targets` 登记消费方（openapi/storage/runtime）；键必须是已声明参数，checker 会拒绝无消费方的参数。

### 1.2 `_types.yaml` 的 `schemas`

schema 节点只描述值域：`object`（`type: object` + `properties`）、`array`（`type: array` + `items`）、
`scalar`（`type` 指向 `_types` 的族或自定义类型）、`ref`（entity / enum / 全限定本地 schema）。

- `object.properties[]` 是槽位条目：`name` + `schema` + `required`/`nullable`/`read_only`/`write_only`/`default`/`description`；
- 三个正交轴：`required` 管键是否存在，`nullable` 管值是否可为 null，`read_only`/`write_only` 管方向；
- 槽位键出现在节点位置报 `schema.slot-position`；
- `_types.yaml` 用 `schemas.<kind>.keys` 描述自身；判别键与结构键必须 `required: true`；
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
| `make contract-new type=<type> name=<name> [module=...] [table=...] [service=...]` | 按 `_meta/<type>.yaml` 渲染最小骨架；`contract-new` 不支持的类型需手写 |
| `make gen-graph` | 写回 `table.queries` / `model.queries` / `query.used_by` / 规则与生命周期锚点 |
| `make gen-graph-check` | 只校验生成字段是否最新 |
| `make contract-check` | 结构 / ID / 约束 / 索引 / 引用 / owner / 数据流 / 类型级规则 |
| `make check-index` | 校验 index 全类型登记及 renderer/targets ID 一致 |
| `make gen` | 生成 DDL / OpenAPI / 错误文档 |
| `make gen-check` | 校验全部生成物最新 |
| `make gen-rule-ids` | 从 checker 的 `rulef("...")` 重新生成 `rule_ids_gen.go` |
| `make gen-rule-tests` | 生成 `docs/contracts/generated/rule-tests.md`（规则 → `implemented_by` → 测试直接引用数） |
| `make test-rules` | 运行 `feature.rules` 的 policy 行为断言骨架（`go test -tags rules`） |

`contract-check` 已接入 `lefthook` pre-commit（`contracts/**/*.yaml`）与 CI。

---

## 4. 校验机制

- `_meta/<type>.yaml` 由 `KnownFields` 严格解析：拼错字段（如 `requred`）直接失败。
- 实例结构由 `fields` / `item_fields` 的 `required` / `type` / `enum` / `allowed` / `pattern` / `unique` /
  `unique_key` / `min` / `max` / `min_items` / `min_len` / `max_len` / `len` / `const` / `types` / `key_patterns` 校验；
  `soft: true` 标记不做通用结构校验的 soft zone（领域规则仍可部分检查）。
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
| `required` / `type` / `types` / `enum` / `allowed` / `pattern` / `key_patterns` / `const` / `unique` / `unique_key` / `min` / `max` / `min_items` / `min_len` / `max_len` / `len` | 结构约束；`types` 是 schema 节点种类列表（与 `type` 互斥）；`key_patterns` 约束 string map 键；`min_items` 约束 list/set/array；`min_len`/`max_len` 约束 string/list/set/map；`len` 只约束 array |
| `soft` | 无通用结构校验的 soft zone（领域规则仍可部分检查；只允许 `any`/`string`） |
| `examples` | 骨架示例值；实体字段不支持 |
| `fields` / `item_fields` | 嵌套对象 / 对象列表的字段树 |
| `targets` | `dir`/`glob` 扫描路径；`id` 是 ID 模板（`<file>` → 文件 stem）；单文件契约用固定 ID；`naming` 是文件名规则；基类固定 `_base.yaml`，不在此声明 |
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
