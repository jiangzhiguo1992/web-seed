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
features.pages ──capability──> capabilities（只能引用 service / HTTP operation / lifecycle）
features.rules ──subject──> data.entities（业务主体）
features.rules ──on_violation──> platform.errors
services ──kind=logic/transition──> features.rules / cap.lifecycles
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
- `available_when` 只允许结构化引用：`{ transition: cap.lifecycles.* }` / `{ transitions: [...] }` / `{ state_not: [...] }`；禁止字符串表达式

**feature.rules**

- `constraint` 必须有 `entity` / `field` / `op` / `value`，且 `entity` 必须存在、`field` 必须是该实体的非关系字段或基类列；`invariant` 必须有 `assert`；`policy` 必须有 `implemented_by`
- `implemented_by` 只能指向 `cap.services.*`
- `on_violation` 必须是存在的 `platform.errors.*`
- 不要写散文式规则；不能结构化的用 `policy` + `given/when/then`

**生命周期**

- `cap.lifecycles.*` 承载 create/destroy/transitions；不要在 features 下再建生命周期。
- 一个契约只描述一个状态维度：`subjects[]` 写 `{entity, field}`；同一实体的多个状态字段拆成多份契约。
- 必须穷举：每个非 final 状态有出边，final 无出边；每条边锚到具体 `cap.services.<file>.<service>.<step>`。
- `guard` 只能是 `true` 或 `{ rule: feature.rules.* }`；跨维度前提写 `requires: [{lifecycle, state}]`；事件由 service step 的 `events` 声明，lifecycle 不写 `side_effects`。

## 3. 页面契约必须声明的 `view_states` 四态

| 状态 | 必填条件 |
|---|---|
| `loading` | 首次加载或刷新中 |
| `empty` | 请求成功但无数据 |
| `error` | 请求失败，展示 `Problem.detail` |
| `forbidden` | 无权限时的表现 |

缺任一状态即校验失败——这是前端最容易漏、也最影响体验的部分。`view_states` 与 `cap.lifecycles` 是不同概念：前者是 UI 加载态，后者是业务生命周期。

## 4. 不做的事

- 不定义组件层级与复用关系 → 属于实现
- 不定义样式、间距、颜色 → 属于 `platform/design/tokens.json` 与实现
- 不定义接口字段类型 → 由 `capabilities/http` 生成，页面契约只引用
- 不写跨页用户旅程（`flows/` 已移除）；旅程由页面动作和测试覆盖
