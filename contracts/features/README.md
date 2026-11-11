# 功能契约（features）

> 功能 = 用户可见的功能单元。描述"页面上必须有什么、能做什么、在什么状态下显示什么"，
> **不描述组件树与样式**。

---

## 1. 文件职责

| 文件 | 内容 | 产物 |
|---|---|---|
| `routes.yaml` | 路由、菜单、布局、页面级权限 | Vue Router 配置、菜单数据 |
| `pages/<page>.yaml` | 数据源、四态、动作、字段约束 | 页面骨架、query 类型、表单校验、测试骨架 |
| `flows/<flow>.yaml` | 跨页用户旅程 | 集成测试骨架 |

## 2. 引用方向

```
features → capabilities（必须通过 operationId / capability ID）
features → business（状态、规则、术语）
features → platform（权限点、错误码、埋点）
features ✗ data（禁止直接引用表或字段）
```

**禁止引用 `data.*` 是硬规则**：页面只能通过 API 拿数据；直接在页面契约里写表名，等于允许前端绕过 API 语义。

## 3. 页面契约必须声明的四态

| 状态 | 必填条件 |
|---|---|
| `loading` | 首次加载或刷新中 |
| `empty` | 请求成功但无数据 |
| `error` | 请求失败，展示 `Problem.detail` |
| `forbidden` | 无权限时的表现 |

缺任一状态即校验失败——这是前端最容易漏、也最影响体验的部分。

## 4. 与 `capabilities/flows` 的区别

| | `capabilities/flows/*` | `features/flows/*` |
|---|---|---|
| 视角 | 系统 / 业务 | 用户 / 界面 |
| 步骤 | 能力调用、状态转换、事件 | 页面跳转、表单提交、提示 |
| 消费者 | api、worker | web |

## 5. 不做的事

- 不定义组件层级与复用关系 → 属于实现
- 不定义样式、间距、颜色 → 属于 `platform/design/tokens.json` 与实现
- 不定义接口字段类型 → 由 `capabilities/http` 生成，页面契约只引用
