---
name: add-feature
description: 新增或修改用户功能（路由 / 页面 / 跨页流程 / 内嵌规则 / 验收场景）的完整流程
targets: [opencode, claude, cursor]
---

# 新增或修改用户功能

> 适用：`contracts/features/**` 的页面、流程、规则与验收场景。
> 不适用：只改实现、只改平台约定（分别见 `contract-change.md` 与对应组 README）。

## 0. 硬前提

1. **先读** `contracts/features/README.md`、`_meta/feature.pages.yaml`、`_meta/feature.flows.yaml`、`_meta/scenario.http.yaml`、`contracts/features/routes.yaml`。
2. **一个功能一个 flow**：`features/flows/<feature>.yaml` 同时承载步骤与规则；规则不再单独建 `features/rules/`。
3. **一次最多改 5 个契约文件**；超过先拆功能。
4. **一个 PR 闭环**：契约 + 生成物 + 实现 + 测试；禁止先合契约后补实现。

## 1. 定位功能

```bash
grep -n "<关键词>" contracts/features/flows/*.yaml contracts/features/pages/*.yaml contracts/features/routes.yaml
```

先回答：

- 入口路由是哪个？`entry: feature.routes.<name>`
- 涉及哪些页面？每个页面的动作名是什么？
- 每一步依赖哪些规则？规则属于哪个 flow？
- 哪些场景覆盖这些页面与规则？

## 2. 改契约

| 要改什么 | 改哪里 |
|---|---|
| 入口/菜单/布局 | `features/routes.yaml` |
| 页面数据源/四态/动作/字段 | `features/pages/<page>.yaml` |
| 跨页顺序/步骤/规则 | `features/flows/<feature>.yaml` |
| 验收场景 | `features/scenarios/http/<file>.yaml` |

### 2.1 flow 结构

```yaml
id: feature.flows.<feature>
summary: "..."
owner_module: <module>
schema_version: 1
stability: draft
do_not: ["..."]
scope: ui                 # 或 api；省略时按 steps 推断
entry: feature.routes.<route>
steps:
  - id: <step>
    page: feature.pages.<page>
    action: <page action>
    rules:
      - feature.flows.<feature>.<rule>
    next: <step|done>
    on_success: feature.routes.<route>   # 可选
rules:
  - id: feature.flows.<feature>.<rule>
    kind: constraint|invariant|policy
    verified_by: [ scenario.http.<file>.<name> ]   # policy/invariant 必填；或测试文件
    decision:                                      # policy 必填
      - when: "条件表达式"
        effect: deny|allow|call|set|normalize|record|create|revoke|forbid
        error: platform.errors.<CODE>              # effect=deny
        target: cap.services.<svc>.<method>        # effect=call；其他 effect 为逻辑字段/表
        value: ...                                 # effect=set/forbid
    ...
```

- `scope: ui` 必须 `entry` + 非空 `steps`；`scope: api` 必须 `steps: []` 且无 `entry`。
- 每个 ui step 的 `rules` 非空；每条规则必须被至少一个 step 引用。
- `cap.services` 仍在 step 的 `rules` 里引用 `feature.flows.<feature>.<rule>`；`implemented_by` 由 `make gen-graph` 写回。
- `policy` / `invariant` 必须有 `verified_by`：指向一个 `scenario.http.*`（且该场景 `rules` 包含此规则）或一个 `scripts/cmd/internal/check/*_test.go`（`policy` 必须作为 `policyExpectations` 的键，`invariant` 必须作为 `invariantAssertions` 的键；其他 checker 测试可用 `mustContainRule` / `[rule-id]`）。
- `policy.decision` 每项是 `when`（rulesdsl）+ `effect`；`deny` 必须有 `error`，`call` 的 `target` 必须是存在的 `cap.services.*`，`set`/`normalize`/`record`/`create`/`revoke`/`forbid` 必须有 `target`。policy 不再接受 `given/when/then`。
- `invariant.assert` 必须是 `rulesdsl` 可解析表达式；`make contract-check` 会解析，`make test-rules` 会用真值表执行。

### 2.2 page 结构

- `data[].capability` 指向 `cap.services.*`；禁止 `data.tables` / `data.queries` / `data.storage`。
- `fields[].name` / `filters[].name` 必须来自 data/actions 的返回字段或 `from` 取值。
- `actions.available_when.rules` 引用 flow 规则；该规则必须绑定到本页该动作的 step。
- `actions.on_error` 引用 `platform.errors.*`；`invalidates` 只引用本页 `data` key。
- `success_ui` / `pending` 使用枚举；`from` 只允许 `query` / `route.params` / `form` / `row` / `device` / `user` / `page` / `auth` 前缀。

### 2.3 scenario 结构

```yaml
scenarios:
  - name: <name>
    summary: "..."
    flow: feature.flows.<feature>
    pages: [ feature.pages.<page> ]
    rules: [ feature.flows.<feature>.<rule> ]   # 可选
    operation: cap.protocols.http.<...>
    ...
```

- `flow` 必须存在；`pages` 必须是该 flow 的步骤页面且是 operation 的 `used_by`。
- `given.db` / `then.db` 的表 ID 必须来自 `data.tables.*`；`then.db.unchanged: strict`。

## 3. 校验

```bash
make contract-check
make gen-graph
make contract-check
```

`contract-check` 会校验：flow 结构、step 页面/动作/规则、规则实现锚点、页面字段来源、动作错误/刷新、场景 flow/pages/rules 绑定、features 数据边界。

## 4. 生成

```bash
make gen
make gen-rule-tests
make gen-check
```

新增/删除规则后运行 `make gen-rule-ids`。

## 5. 实现

- 页面/路由仍由 `features/**` 描述，但前端代码生成属 P2；当前只校验契约，不生成 `apps/` 代码。
- 页面只能通过生成的 API client 调用 `capability`；禁止直接写 URL 或引用 `data.*`。
- 错误码、字段、枚举、权限必须来自契约；不要自造字符串。
- 四态与动作 pending/error/invalidates 按 `features/pages` 实现。

## 6. 完成定义（DoD）

- [ ] `make contract-check` 通过
- [ ] `make gen` 幂等，`make gen-check` 通过
- [ ] `make test-tools`、`make test-rules` 通过
- [ ] `make test-web` 暂不适用（前端代码生成属 P2）
- [ ] 每个 ui flow 的每条规则都被 step 引用
- [ ] 每个新增页面至少有一个 scenario 绑定 flow/pages
- [ ] `contracts/index.yaml` 已同步新增/删除文件
- [ ] 没有在 `features/pages` / `features/flows` 引用 `data.tables` / `data.queries` / `data.storage`
