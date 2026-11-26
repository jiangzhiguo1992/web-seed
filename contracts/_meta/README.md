# 契约字段规范（_meta）

> **字段命名铁律**：`type` 只承载数据类型（`_types.basics.*` / `_types.structures.*` / `data.*` / `context.Context` / `error` / `time.*`）；schema 结构节点用 `kind: _types.schemas.object|array|map` 判别，引用节点用 `type` 指向 entity/enum/本地 schema；`_meta` 的契约类型用 `contract`；`platform/http_conventions` 的 OpenAPI 原生类型用 `json_type`，由生成器还原为 `type`。任何非数据类型的值都不得占用 `type`。
> 本目录是**契约的单一源**：字段树、值域、类型级规则与校验入口。
> 不是业务契约：不登记 `contracts/index.yaml`，不进入 `generated/`。
> `contracts/generated/` 只放机器生成物；人读生成视图在 `docs/contracts/generated/`。

---

## 1. 目录

| 文件 | 职责 |
|---|---|
| `_types.yaml` | 统一类型注册表：`commons.basics_params`（所有族共享参数，后续可扩展 `structures_params` / `schemas_params`）、`basics`（标量族 + 参数）、`structures`（list/set/array/map/object）、`schemas`（object/array/map/scalar/ref 节点）；项目级自定义类型在 `contracts/data/types.yaml`，与 `basics` 共用 `_types.basics.*` 命名空间 |
| `project.yaml` | 项目元信息、应用拓扑与 derive 输入 |
| `data.tables.yaml` | 表、列、约束、索引 |
| `data.storage.yaml` | 存储拓扑单例：引擎、表级保留、缓存、归档 |
| `data.enums.yaml` | 跨前后端枚举集合 |
| `data.entities.yaml` | 读模型字段树与关系 |
| `data.queries.yaml` | 查询接口（入参/结果/表/索引/缓存/归档），不写 SQL |
| `cap.services.yaml` | 服务与 steps（数据流、分支、事务） |
| `cap.events.yaml` | 异步 topic（publish/subscribe）与 crons/dispatches |
| `cap.lifecycles.yaml` | 实体状态机 |
| `cap.protocols.http.yaml` | OpenAPI 3.1 HTTP 绑定 |
| `scenario.http.yaml` | HTTP 验收场景（请求/响应/持久数据差异） |
| `feature.pages.yaml` | 页面契约；data/actions.params 对齐 service 输入 |
| `feature.routes.yaml` | 路由与菜单；route/page/permission/layout 引用 |
| `feature.rules.yaml` | 可执行业务规则；所有规则都是 error，违反即阻止提交 |
| `platform.modules.yaml` | 模块注册表（owner_module 来源） |
| `platform.http_conventions.yaml` | HTTP 信封、分页、header、状态码与平台组件 |
| `platform.errors.yaml` | 错误码文件（全局约定 + 分域错误 + 基础设施基线） |
| `platform.security.permissions.yaml` | 权限点 |
| `platform.security.roles.yaml` | 角色与权限映射 |
| `platform.security.auth_policy.yaml` | JWT、token 表哈希、MFA 与密码策略 |
| `platform.security.access.yaml` | 入口访问策略；HTTP service 覆盖 |
| `platform.environments.yaml` | 环境清单、差异项与安全开关 |
| `platform.versioning.yaml` | API 版本、兼容检测与弃用头 |
| `platform.quality.yaml` | 测试门槛、必测项与 CI gates |
| `platform.analytics.events.yaml` | 埋点事件、属性与 HTTP/页面引用 |
| `platform.observability.log_fields.yaml` | 日志字段与 PII 规则 |
| `platform.observability.metrics.yaml` | 指标名、标签与导出 |
| `platform.architecture.go_arch.yaml` | Go 分层与模块依赖 |

`scripts/contract/scaffold.sh`（`make contract-new`）与 `scripts/contract/validate.sh`
（`make contract-check`）是薄适配器；`scripts/cmd/` 是独立 Go module，schema 解析与校验都在那里。

**没有 `schema/`、没有 `templates/`。** 字段规范只在 `_meta/<type>.yaml`；`contract-new` 从字段树渲染最小骨架，不维护第二份模板。

### 1.1 `_types.yaml` 与 `data/types.yaml`

值类型与结构是 tables / entities / queries / services / events 共用的词汇，分两类注册：

- `_meta/_types.yaml` 的 `basics` 定义标量族、参数与具体落型（`types.<variant>.{go,pg,ts}`）：位宽和时区形态只由 variant 表达（`integer.8/16/32/64`、`float.32/64`、`timestamp.time/long`、`datetime.timezone/time`），单变体族的 key 是族名本身（`string`/`boolean`/`decimal`/`json`/`bytes`/`any`/`bit`/`xml`），`geometry` 暂不实现；`pg` 是兼容集合（含 `json`/`jsonb`、`numeric`、`text`/`char`/`tsvector`/`tsquery`、`date`/`time`/`timetz`、`bit`/`varbit`、`xml`）；integer 的 `pg` 同时列基础类型与 `smallserial`/`serial`/`bigserial`，自增由 `auto_increment` 参数选择（仅表列），参数化由生成器追加（`min_len`/`max_len`/`pattern`/`precision`/...），`commons.basics_params` 定义所有 basics 族共享的参数（当前只有 `default`），`structures` 定义 `list`/`set`/`array`/`map`/`object` 五种容器（list/set/map 的集合大小参数是 `min_items`/`max_items`，array 用 `len`，map 另有 `key_patterns`；元素、map value 或 object field 的 `type` 可以是 `_types.basics.*` 或 `_types.structures.*`，递归包含类型族、自定义类型与容器），`schemas` 定义 `object`/`array`/`map`/`scalar`/`ref` 五种值域节点；这是元层，不随业务增长；
- `any` 没有 `types`：具体 apps 实现时再决定落型，契约不约束；实例仍写 `_types.basics.any`。
- `contracts/data/types.yaml` 的 `custom_types` 的 `type` 固定一个具体 basics 类型（如 `_types.basics.integer.64` / `_types.basics.string`）并附加固定参数，供表列与 schema 标量复用；这是 data 层词汇，随项目开发增长；
- **所有契约**（含 `_meta` 字段规范）的 `type` 必须写具体类型：多变体族写 `_types.basics.integer.32` / `_types.basics.float.64`，单变体族写 `_types.basics.string`，自定义类型写 `_types.basics.id`；裸写多变体族（`_types.basics.integer`）和给单变体族加冗余 variant（`_types.basics.string.string`）都报 `schema.type-concrete`。`Field.type` 统一写 `_types.basics.*` 或 `_types.structures.*`，泛型写 `_types.structures.list<_types.structures.object>` / `_types.structures.map<_types.basics.string>`，可嵌套到 `_types.structures.list<_types.structures.map<_types.structures.object>>`；`data.queries.params[].fields` 用字段树描述对象批量参数的元素结构（仅 `_types.structures.object` / `_types.structures.list<_types.structures.object>`，非空），不要用 `_types.basics.json` 代替；`list<map<object>>` 的 `item_fields` 描述 map value 的字段；`list<map>` 不合法，`map` 必须显式给出 value 类型；`map` 的 key 默认 `string`，可写 `_types.structures.map<_types.basics.integer.32, _types.structures.object>` 指定 `integer` key；`key_patterns` 是 string key 的正则列表，全部匹配；`_types.yaml` 自身也必须使用全限定名；
- `structures.list` / `set` / `array` 声明 `fields.item`，`map` 声明 `fields.key` / `value`（均为 `type: "*"`、`required: true`），生成器与实现层统一使用这些名字；checker 按 `Field.type` 递归校验，不再为每个容器组合维护独立分支；`key_patterns` 是 AND 语义，多个正则必须同时匹配，且只允许 string key；
- `list<object>` / `set<object>` / `array<object>` 必须声明 `item_fields`；空对象列表用 `list<any>`，不要用无字段的 `list<object>`；
- `columns[].type` 必须是 `_types.basics.<name>`（类型族或自定义类型）或 `data.tables.<t>.col.<c>`；表列不引用 `data.enums.*`；
- schema 的 `scalar.type` 必须是 `_types.basics.<name>`；entity 字段的 `type` 与表列共用 `_types.basics.<name>` 值类型，可带一层 `*` 表示指针（等价 `nullable`），不写 Go 类型名；Go/TS 具体类型由类型族的 lowering 规则决定；
- `normalize` 由 `gen_openapi` 输出为 `x-normalize`，runtime 在写入前执行 lower/upper；`min_len`/`max_len`/`pattern`/`min`/`max`/`default` 由 OpenAPI 输出。
- 新增族参数必须由生成器或检查器实际消费；消费点由 `TestTypeTableMatchesColumnType` / `TestTypeTableMatchesScalarMapping` 和生成器单测锁定，不再维护单独的消费方登记表。

### 1.2 `_types.yaml` 的 `schemas`

schema 节点只描述值域：`object`（`kind: _types.schemas.object` + `properties`）、`array`（`kind: _types.schemas.array` + `items`）、
`map`（`kind: _types.schemas.map` + `values`，键固定 string，另有 `min_items`/`max_items`/`key_patterns`/`validation`）、
`scalar`（`type` 指向 `_types.basics.*`）、
`ref`（`type` 指向 entity / enum / 全限定本地 schema）。

- `object.properties[]` 是槽位条目：`name` + `schema` + `required`/`nullable`/`read_only`/`write_only`/`default`/`description`；
- 三个正交轴：`required` 管键是否存在，`nullable` 管值是否可为 null，`read_only`/`write_only` 管方向；
- 槽位键出现在节点位置报 `schema.slot-position`；
- `_types.yaml` 用 `schemas.<kind>.keys` 描述自身；判别键与结构键必须 `required: true`；
- `cap.events` 的 `topics[].publish.payload` 投影为 `Event<File><Topic>` component；生成物只保留标准 schema，不携带事件扩展键。
- `cap.protocols.http` 使用完整 OpenAPI 3.1，但源契约的 operation 直接写类型 ID；平台级组件在 `platform/http_conventions.components`，HTTP 文件可声明私有组件并由 `gen_openapi` 合并。`request` 写 headers/path/query/body，headers 独立于 body；path/query/body/form 用 `to` 指向 service 参数，值域字段统一叫 `type`；`response` 是以三位状态码为键的 map，写 msg/headers/body/errors；operation 字段顺序固定为 id/name/summary/service/request/response/used_by；header/query/body/body.fields 的 `required` 省略为 true，可选项写 false；认证用 `Authorization` header 的 `auth: Bearer`，不再有 security/mapping 字段。
- `mount` 必须声明 `api_version` / `path` / `audience`：`api_version` 是 `v<数字>`，与 `mount.path`、`paths` key 一起由生成器派生 `servers: /<api_version>`；`paths` 必填且至少一个 operation。
- `cap.protocols.http` 的文件级 `mount` 承载 `path` 与 `audience`：`path` 是公共资源前缀（`/account`）；audience 段在 `paths`（`/admin`），`audience` 来自 `platform/http_conventions.audiences`；operation 的 `used_by` 可选：省略表示 API-only，存在时必须精确等于页面引用，错误按 platform.errors 的 status 分组、`COMMON_*` 不在 operation 重复；平台基线状态（含 body/deprecated）由 `error_responses` 注入。
- 槽位字段的值域可整体写 `types: [ _types.schemas.* ]`，即允许 `schemas` 下声明的全部节点种类；不要重复枚举节点种类。
- `map` 节点的键固定为 string（JSON/OpenAPI 对象键没有其他类型），因此没有 `key` 字段；需要 integer key 的 Go/SQL 容器用 `_types.structures.map<_types.basics.integer.32, T>`（`structures.map`），不要写进 schema 节点。
- `map` 与 `array` 对齐：`values` 是值域，`min_items` / `max_items` 约束键数，`key_patterns` 约束键名（全部匹配），`validation` 校验映射级规则且只能引用 `value`；生成到 OpenAPI 为 `additionalProperties` / `minProperties` / `maxProperties` / `propertyNames`。

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
| `make gen-graph` | 写回 `table.entities` / `table.queries` / `model.queries` / `query.used_by` / `service.used_by` / `constraints[].used_by` / `validation[].used_by` / `enums[].used_by` / 规则锚点 / 生命周期动作与 `states.values[].steps` / `topics[].publish.emitted_by` / `dispatches[].emitted_by` |
| `make gen-graph-check` | 只校验生成字段是否最新 |
| `make contract-check` | 结构 / ID / 约束 / 索引 / 引用 / owner / 数据流 / 类型级规则 / service.used_by 与 states.values[].steps 对账 |
| `make audit-types` | 审计全部契约 `type` 键：`_types.*` / 外部引用 / 显式例外，输出计数并以非零退出码报告裸类型 |
| `make check-index` | 校验 index 全类型登记及 renderer/targets ID 一致 |
| `make gen-model` | 生成 Go 模型（`apps/api/internal/generated/model`）与 TS 模型（`apps/web/packages/api-client/src/generated/model.ts`） |
| `make gen` | 生成 DDL / OpenAPI（HTTP + `cap.events` payload components）/ Go/TS 模型 / 错误文档 |
| `make gen-check` | 校验全部生成物最新 |
| `make gen-rule-ids` | 从 checker 的 `rulef("...")` 重新生成 `rule_ids_gen.go` |
| `make gen-rule-tests` | 生成 `docs/contracts/generated/rule-tests.md`（规则 → `implemented_by` → 测试直接引用数） |
| `make gen-scenarios` | 把 `contracts/scenarios/http/*.yaml` 投影为 `contracts/generated/scenarios/manifest.yaml` |
| `make gen-async` | 把 `contracts/capabilities/events/*.yaml` 投影为 `contracts/generated/async/manifest.yaml`（worker 重试/并发/调度策略） |
| `make test-rules` | 运行 `feature.rules` 的 policy 行为断言骨架（`go test -tags rules`） |

`contract-check` 已接入 `lefthook` pre-commit（`contracts/**/*.yaml`）与 CI；CI 额外运行 `make audit-types` 输出类型词汇报告。

类型词汇审计只豁免 `contracts/generated`（机器生成产物）；其他目录一律按 `type` 只承载数据类型校验，没有可配置例外。

---

## 4. 校验机制

- `_meta/<type>.yaml` 由 `KnownFields` 严格解析：拼错字段（如 `requred`）直接失败。
- `_meta` 的 `pattern` / `key_patterns` 必须能编译且整体锚定（`^...$`），在加载期直接失败，避免退化为子串匹配；实例 schema 的 `key_patterns` 同样要求锚定。
- 字段规范里的 `enum` / `const` / `default` / `examples` 值必须与字段类型兼容，并满足该字段的 `pattern` / `min_len` / `max_len` / `min` / `max`；`unique_key` 必须指向 string 字段；`unique` 只用于集合；`fields` 只用于 object。
- `default` 只用于可省略字段；`required: true` 的字段不得声明 `default`（加载期直接拒绝）。`contract-new` 的必填字段骨架值由渲染器提供，不写在模板的 `default` 里。
- 枚举兼容：`name` / `value` / `go_type` / `ts_type` / `code` 只能追加，不得删除或改名；`make contract-compat` 默认对比 `HEAD^1`，pre-commit 用 `make contract-compat-staged` 对比 index，CI 在完整历史 checkout 上执行。
- 实例结构由 `fields` / `item_fields` 的 `required` / `type` / `enum` / `pattern` / `unique` /
  `unique_key` / `min` / `max` / `min_items` / `min_len` / `max_len` / `len` / `const` / `types` / `key_patterns` 校验；
  `soft: true` 标记不做通用结构校验的 soft zone（领域规则仍可部分检查）。
- 字段顺序以模板为准（`checkFieldOrder`）；新增字段只改 `_meta` 模板。
- `contracts/**/*.yaml` 的文本字段必须双引号（`schema.quote`）；同一对象不得重复键。
- `contracts/` 下所有 YAML（含 `_meta` 字段规范）的 `type` 值必须是 `_types.basics.*` / `_types.structures.*` 或已声明外部引用（`data.enums.*` / `data.entities.*` / `cap.services.*.schemas.*` / `data.tables.*.col.*` / `context.Context` / `error` / `time.*`）；`_types.schemas.*` 只允许出现在字段规范的 `types` 节点种类列表里，不得作为 `type` 值。多变体 basics 族必须带具体 variant（缺 variant 报 `schema.type-concrete`）；单变体族必须裸写（带冗余 variant 也报 `schema.type-concrete`）；裸类型名报 `schema.type-vocabulary`，由 checker 扫描 `contracts/` 下全部 YAML；同一文件/路径已有更精确的领域错误时不重复报。`_meta` 顶层用 `contract`；`platform/http_conventions` 用 `json_type`，二者都不是 `type`，由专门规则校验。
- 类型级 `rules` 的每条必须有 `implemented_by`，包含自身 ID，且每个 ID 都是 checker 实际输出的
  `[rule-id]`；反向检查所有非 `schema.*` 输出都必须被某条规则声明。
- `rule_ids_gen.go` 由 `make gen-rule-ids` 从 `rulef("...")` 生成；独立二进制也带完整映射。
- `docs/contracts/generated/rule-tests.md` 由 `make gen-rule-tests` 生成，`refs` 为 0 表示该规则只有实现、没有测试直接断言；`make gen-check` 会阻止映射表过期，并按 `-max-zero` 阈值阻止新增未覆盖规则。
- `feature.rules` 的 policy 规则在 `scripts/cmd/internal/check/rules_policy_test.go`（`make test-rules`）自动登记，并校验 `implemented_by`/`given`/`when`/`then`；业务行为断言由实现层补充。
- `rules` 只允许出现在 `_meta`（`features/rules` 等领域规则集除外）；`do_not` 只允许出现在具体契约。
- `generated: true` 字段必须同时 `write: forbidden`；由 `make gen-graph` 写入，checker 对账。
  `generated` 字段的 `min_items` 与手写字段一样生效；只有 `contract-new` 的骨架在生成阶段放宽（`relaxGeneratedFields`），入库实例不再豁免。`stability` 只表示定型状态，不豁免任何校验。
  新增 `constraints[]` / `validation[]` 项时必须先写 `used_by: []`（`contract-new` 骨架已带）；`gen-graph` 只写值、不补字段，字段完全缺失会先报 `schema.required`。

---

## 5. 字段节点怎么读

| 键 | 用途 |
|---|---|
| `description` | 字段的准确含义；软区标记也写在这里 |
| `required` / `type` / `types` / `enum` / `pattern` / `key_patterns` / `const` / `unique` / `unique_key` / `min` / `max` / `min_items` / `max_items` / `min_len` / `max_len` / `len` | 结构约束；`types` 是 schema 节点种类列表（与 `type` 互斥），可整体写 `[ "*" ]`；`key_patterns` 约束 string map 键；`min_items`/`max_items` 约束 list/set/map 的元素数；`min_len`/`max_len` 只约束 string 长度；`len` 只约束 array |
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
| `draft` | 正在编写 | 默认状态；**不豁免任何校验，TODO/FIXME 同样报错** |
| `stable` | 评审通过、无 TODO | 替换所有 TODO/FIXME，`make contract-check` 通过 |
| `deprecated` | 已废弃 | 停止新增引用，准备移除 |

`stable` 的进入与退出只能由**用户手动修改**（`contract-new` 只产出 `draft`）；`contract-check` 无法识别修改者。
`stability` 只表达成熟度，不改变任何校验的报错机制。

---

## 6. 不要做的事

- 不要重建 `contracts/_meta/schema/` 或 `templates/`；不要在 `_meta` 之外手写第二份字段模板。
- 不要手写生成字段（`table.entities` / `table.references` / `table.referenced_by` / `table.queries` / `model.includes` / `model.included_by` / `model.queries` /
  `query.used_by` / `services.used_by` / `constraints[].used_by` / `validation[].used_by` / `enums[].used_by` / `feature.rules.implemented_by` / 生命周期锚点与 `states.values[].steps` / `topics[].publish.emitted_by` / `dispatches[].emitted_by`）。
- 不要只跑 `make gen` 而跳过 `make contract-check`。
- 不要把硬约束只写进 `rules` 而不写 `implemented_by`。
- 不要在 checker 里解析 `CHECK expr` 的 SQL 语义；只校验可判定的字面量。
- 不要在 `_meta` 下写 `do_not`；不要在具体契约写顶层 `rules`。
- 不要在 `contracts/_meta/` 里写业务实例；按类型一份规范。
