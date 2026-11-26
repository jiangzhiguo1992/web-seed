# 功能契约（features）

> 功能 = 用户可见的功能单元 + 其业务规格。描述"页面上必须有什么、能做什么、在什么状态下显示什么"，
> 以及"业务上必须成立哪些规则"；生命周期属于 capabilities，见 `capabilities/README.md`。**不描述组件树与样式**。

---

## 1. 文件职责

| 文件 | 内容 | 产物 |
|---|---|---|
| `routes.yaml` | 路由、菜单、布局、页面级权限 | Vue Router 配置、菜单数据 |
| `pages/<page>.yaml` | 数据源、四态、动作、字段约束 | 页面骨架、query 类型、表单校验、测试骨架 |
| `rules/<subject>.yaml` | 业务规则：constraint / invariant / policy | 校验函数、断言、测试用例 |

## 2. 引用方向

```
features.pages ──capability──> services（service ID）
features.pages ──available_when──> cap.lifecycles（create / destroy / transitions.<name>）
features.rules ──subject──> data.entities（业务主体）
services ──kind=logic|when──> features.rules
services ──lifecycles──> cap.lifecycles
features ✗ data.tables / data.queries（禁止直接引用表或查询）
```

**方向说明**：

- `features.pages → capabilities` 是"功能依赖接口"；
- `services → features.rules / cap.lifecycles` 是"实现依赖业务规格"；
- 两者方向不同，不构成循环；但 `features.pages` 不得反向引用 `services` 的实现细节（只能引用 service / operation ID）。

**禁止引用 `data.*` 是硬规则**：页面只能通过 API 拿数据；直接在页面契约里写表名，等于允许前端绕过 API 语义。

### 2.1 类型级负向约束

**feature.routes**

- 不要手写路由表
- 不要在路由 meta 硬编码权限字符串

**feature.pages**

- 不要手写 URL，必须引用 capability
- `view_states` 四态（loading/empty/error/forbidden）缺一不可；禁止再用 `states` 命名 UI 状态
- 禁止引用 `data.*`
- 页面不得展示 secret 字段（密码、手机号、邮箱、令牌原文）
- `available_when` 只允许结构化引用：`{ transition: cap.lifecycles.<lifecycle>.{create|destroy|transitions.<name>} }` / `{ transitions: [...] }` / `{ state_not: [...] }`；禁止字符串表达式
- `feature.pages` 的结构由 `_meta/feature.pages.yaml` 校验；`enum` / `transition(s)` / `state_not` / `transition_ui.source` / `capability` / `route` 由 `pages.go` 做跨契约引用校验

**feature.rules**

- `constraint` 必须有 `entity` / `field` / `op` / `value`，且 `entity` 必须存在、`field` 必须是该实体的非关系字段或基类列；`op`/`value` 必须可判定：pattern 为非空合法正则且字段为 string，in/not_in 为非空标量列表，min_len/max_len 为非负整数且字段为 string，required/not_null 为布尔，eq/ne 为标量，gt/ge/lt/le 为数字且字段为数值或时间；`invariant` 必须有 `assert`；`policy` 必须有 `implemented_by`
- `implemented_by` 只能指向 `cap.services.*`
- 规则用 `errors` 声明违反时可能返回的错误码；实现该规则的 service 必须在 `step.errors` 中覆盖这些错误码
- 不要写散文式规则；不能结构化的用 `policy` + `given/when/then`

**生命周期**

- `cap.lifecycles.*` 承载 create/destroy/transitions；不要在 features 下再建生命周期。
- 一个契约 = 一个状态机：`subjects[]` 列出共享该状态机的 `{entity, field}`；同一 `(entity, field)` 只能被一份契约声明。
- `states` 写 `values` / `initial` / `final`；`final` 是终态列表，每个 state 必须从 initial 可达，每条边锚到具体 `cap.services.<file>.<service>.<step>`。
- `destroy` 可销毁任意非 final 状态；`final` 最多一个时 destroy 进入该终态，`final` 为空时只软删实体；幂等转换用 `from` 自环表达。
- 跨维度前提写 `requires: [{lifecycle, states: [...]}]`（create/destroy/transitions 均可）；事件由 service step 的 `events` 声明。

## 3. 页面契约必须声明的 `view_states` 四态

| 状态 | 必填条件 |
|---|---|
| `loading` | 首次加载或刷新中 |
| `empty` / `idle` | 请求成功但无数据；表单页用 `idle` 表示初始可编辑态 |
| `error` | 请求失败，展示错误信封的 `message` |
| `forbidden` | 无权限时的表现 |

缺任一状态即校验失败——这是前端最容易漏、也最影响体验的部分。`view_states` 与 `cap.lifecycles` 是不同概念：前者是 UI 加载态，后者是业务生命周期。

## 4. 不做的事

- 不定义组件层级与复用关系 → 属于实现
- 不定义样式、间距、颜色 → 属于 `platform/design/tokens.json` 与实现
- 不定义接口字段类型 → 由 `capabilities/http` 生成，页面契约只引用
- 不写跨页用户旅程（`flows/` 已移除）；旅程由页面动作和测试覆盖
