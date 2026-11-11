# 业务规格（business）
#
# > business 描述**业务上必须成立的事实**：状态、规则、领域事件、术语。
# > 实体在 data 层（data/entities），business 只引用 data.entities.* 作为业务主体。
# > 它不描述实现，也不引用 data.tables/query/storage。实现由 capabilities 承担，通过 `implemented_by` 锚点声明。

---

## 1. 文件职责

| 文件 | 内容 | 产物 |
|---|---|---|
| `states/<machine>.yaml` | 状态、转换、守卫、副作用 | 状态常量、转换表、非法转换测试 |
| `rules/<scope>.yaml` | 业务规则：given/when/then、错误码、实现锚点 | 规则函数骨架、表驱动测试 |
| `events/<scope>.yaml` | 领域事件：payload、版本 | 事件常量、payload 类型 |
| `glossary.md` | 术语表 | 无（review-only） |

## 2. 硬规则

1. **实体在 data 层**（`data/entities/`），本层只写状态机、规则、事件、派生。
2. **状态机必须穷举**：每个状态的出边全部列出，未列出即为非法。
3. **规则必须可执行**：每条规则至少一组 `given/when/then`，并声明 `implemented_by`。
4. **business 只允许引用 `data.entities.*` 作为业务主体**；禁止引用 `data.tables` / `data.queries` / `data.storage` 等实现细节；其他跨层引用只走 `platform`（枚举、错误码）。
5. **锚点单向**：`business → capabilities` 仅用于 `implemented_by`，语义是"这条规则由谁实现"。
6. **术语表是唯一命名来源**：代码、契约、UI 文案必须与 `glossary.md` 一致。

## 3. 引用方向

```
business → platform            （枚举、错误码）
business → data.entities         （业务主体；不引用表/查询/存储）
business → capabilities        （implemented_by / emitted_by：这条规则由谁实现）
```

capabilities 契约**不反向引用 business**（`cap.flows` 例外：只允许引用 `business.states/rules/events` 做编排，不复制规则逻辑）。
实现代码按本层契约编写，但契约层不出现 `business.*`。

## 4. 与 capabilities/flows 的区别

| | `capabilities/flows/*` | `features/flows/*` |
|---|---|---|
| 关注点 | 业务编排（系统视角） | 用户旅程（界面视角） |
| 例子 | 发布账号 → 写库 → 发事件 | 填写表单 → 提交 → 跳转详情 |
| 消费者 | api / worker | web |

流程编排依赖具体能力，属于实现层，因此不在 business。
