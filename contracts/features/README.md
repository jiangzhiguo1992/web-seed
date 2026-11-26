# 功能契约（features）

> 功能 = 用户可感知的行为单元 + 其业务规格。描述"从哪个入口进入、页面上有什么、按什么顺序操作、每一步必须成立什么规则、如何验收"。
> 生命周期属于 capabilities，见 `capabilities/README.md`；**不描述组件树与样式**。

---

## 1. 文件职责

| 文件 | 内容 | 产物 |
|---|---|---|
| `routes.yaml` | 路由、菜单、布局、页面级权限 | Vue Router 配置、菜单数据 |
| `pages/<page>.yaml` | 数据源、四态、动作、字段、筛选、分页、文案 | 页面骨架、query 类型、表单校验、测试骨架 |
| `flows/<flow>.yaml` | 跨页步骤、步骤绑定的规则、流程内嵌规则 | 页面动作编排、规则校验、E2E 骨架 |
| `scenarios/http/<file>.yaml` | 绑定 flow/pages/rules 的 HTTP 验收场景 | 验收清单、实现侧 runner 输入 |

**规则不再单独成目录**：规则内嵌在 `flows/*.yaml` 的 `rules:`，由 `steps[].rules` 引用。`cap.services` 仍然通过 `step.rules` 引用 `feature.flows.<flow>.<rule>`；`implemented_by` 由 `gen-graph` 反向写回。

## 2. 引用方向

```
features.scenarios ──flow/pages/rules──> features.flows / features.pages
features.scenarios ──operation──> cap.protocols.http
features.scenarios ──data.*（仅本目录允许）──> data.tables / data.queries
features.flows ──entry/step.page──> feature.routes / feature.pages
features.flows ──step.rules/rules──> 本 flow 的 rules
features.pages ──capability──> cap.services
features.pages ──available_when──> cap.lifecycles / feature.flows.rules
features.flows.rules ──entity──> data.entities
cap.services ──kind=logic|when──> feature.flows.rules
cap.services ──lifecycles──> cap.lifecycles
features.routes / features.pages / features.flows ✗ data.tables / data.queries / data.storage
```

- `features.scenarios` 是唯一允许引用 `data.tables.*` / `data.queries.*` 的 features 子目录：验收需要精确断言持久化结果。
- `features.flows` / `features.pages` 不得引用持久化对象；页面只能通过 `cap.services` 取数据。
- `features.flows.rules.constraint.entity` 可以引用 `data.entities.*`（业务主体），但不得引用表/查询/存储。

### 2.1 类型级负向约束

**feature.routes**

- 不要手写路由表
- 不要在路由 meta 硬编码权限字符串

**feature.pages**

- 不要手写 URL，必须引用 capability
- `view_states` 四态（loading/empty/error/forbidden）缺一不可；禁止再用 `states` 命名 UI 状态
- 禁止引用 `data.tables` / `data.queries` / `data.storage`
- 页面不得展示 secret 字段；只有输入字段可以声明 `secret: true`
- `fields[].name` / `filters[].name` 必须来自本页 data/actions 的 service 返回字段或 `from` 取值；不能凭空声明
- `actions.available_when` 只允许结构化引用：`transition` / `transitions` / `state_not` / `rules`；禁止字符串表达式
- `actions.on_error.error` 必须是已存在的错误码；`actions.invalidates` 只能引用本页 `data` key
- `actions.success_ui` / `pending` 使用枚举；不要自造 UI 变体
- `from` 只允许 `query` / `route.params` / `form` / `row` / `device` / `user` / `page` / `auth` 前缀

**feature.flows**

- 一个 flow = 一个用户功能流程；`scope: ui` 必须有 `entry` 和非空 `steps`；`scope: api` 必须 `steps: []` 且不得声明 `entry`
- 每个 ui step 必须有 `page` / `action` / `rules`；`action` 必须存在于该页面；`rules` 必须属于本 flow
- ui flow 的每条规则必须被至少一个 step 引用；不能写"只声明不挂步骤"的规则
- `constraint` 必须有 `entity` / `field` / `op` / `value`；`invariant` 必须有可执行的 `assert`；`policy` 必须有非空 `decision`（`rulesdsl` 条件 + 结构化 effect：deny/allow/call/set/normalize/record/create/revoke/forbid）
- `implemented_by` 只能指向 `cap.services.*`；规则用 `errors` 声明错误码，实现 service 必须覆盖
- `policy` / `invariant` 必须有 `verified_by`：指向一个 `scenario.http.*`（该场景 `rules` 包含此规则）或 `scripts/cmd/internal/check/*_test.go`（`policy` 必须作为 `policyExpectations` 的键，`invariant` 必须作为 `invariantAssertions` 的键；其他 checker 测试可用 `mustContainRule` / `[rule-id]`）；否则 `contract-check` 报 `flow-rule-verified`
- 不要写散文式规则；policy 不再接受 `given/when/then`，必须写成 `decision` 决策表

**scenario.http**

- 每个场景必须有 `flow` / `pages` / `operation`
- `flow` 必须存在；`pages` 必须是该 flow 的步骤页面，且是 operation 的 `used_by`
- `rules` 必须属于该 flow；用于声明场景显式覆盖的业务规则
- `then.db.unchanged` 必须是 `strict`；`given.db` / `then.db` 的表 ID 必须来自 `data.tables.*`

**生命周期**

- `cap.lifecycles.*` 承载 create/destroy/transitions；不要在 features 下再建生命周期。
- 一个契约 = 一个状态机：`subjects[]` 列出共享该状态机的 `{entity, field}`；同一 `(entity, field)` 只能被一份契约声明。
- `states` 写 `values` / `initial` / `final`；每个 state 必须从 initial 可达，每条边锚到具体 `cap.services.<file>.<service>.<step>`。
- `destroy` 可销毁任意非 final 状态；`final` 最多一个时 destroy 进入该终态，`final` 为空时只软删实体；幂等转换用 `from` 自环表达。
- 跨维度前提写 `requires: [{lifecycle, states: [...]}]`；事件由 service step 的 `events` 声明。

## 3. 页面契约必须声明的 `view_states` 四态

| 状态 | 必填条件 |
|---|---|
| `loading` | 首次加载或刷新中 |
| `empty` / `idle` | 请求成功但无数据；表单页用 `idle` 表示初始可编辑态；不适用时写 `empty: { when: "false", note: "..." }` |
| `error` | 请求失败，展示错误信封的 `message` |
| `forbidden` | 无权限时的表现 |

缺任一状态即校验失败。`view_states` 与 `cap.lifecycles` 是不同概念：前者是 UI 加载态，后者是业务生命周期。

## 4. 验收绑定

`features/scenarios/http/*.yaml` 是 features 的下游验收，不是第二份页面/流程定义：

```
scenario.http.<file>.<name>
  ├─ flow: feature.flows.<flow>          # 被测流程
  ├─ pages: [feature.pages.<page>]       # 流程步骤页面，且是 operation.used_by
  ├─ rules: [feature.flows.<flow>.<rule>] # 场景显式覆盖的规则
  └─ operation: cap.protocols.http.*     # 被测接口
```

`make gen-scenarios` 把场景投影为 `contracts/generated/scenarios/manifest.yaml`；`make contract-check` 校验 flow/page/rule/operation 绑定。

前端路由与页面骨架仍属 P2；当前契约侧只负责 `features/routes.yaml` 与 `pages/*.yaml` 的校验，不生成 `apps/` 代码。

## 5. 不做的事

- 不定义组件层级与复用关系 → 属于实现
- 不定义样式、间距、颜色 → 属于 `platform/design/tokens.json` 与实现
- 不定义接口字段类型 → 由 `capabilities/http` 生成，页面契约只引用
- 不在页面/流程里引用 `data.tables` / `data.queries` / `data.storage`；只有 `features/scenarios` 例外
