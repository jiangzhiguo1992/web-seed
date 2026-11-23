# ADR 0001：HTTP 契约的边界与验收模型

- 状态：已采纳（错误模型、audience、consumer 已实现；验收场景为设计，尚未实现）
- 日期：2026-11-23
- 关联：`contracts/capabilities/protocols/http/*.yaml`、`contracts/platform/http_conventions.yaml`、`contracts/_meta/cap.protocols.http.yaml`

## 背景

对 HTTP 契约提出六类问题：文件级公共字段（URL 前缀、版本）、默认错误响应的归属、调用端（audience）、`used_by` 与页面串联、按状态聚合错误码、以及可百分百验收的测试数据。本文给出取舍与落地边界；实现只做已确认的硬约束，未确认的留在设计。

## 决策 1：版本与 URL 前缀留在 `project.yaml`

**版本不放进 HTTP 文件。** 当前 `http-version` 规则要求 `api_version` 只出现在 `project.yaml` 的 api app 上，生成器输出 `servers: [{url: /v1}]`。这是正确的单源：

- 版本是 app 级事实，不是文件级事实；同一文件可被 v1/v2 app 复用。
- 如果文件各自写版本，同一 app include 两个不同版本的文件时，必须再加“所有 include 版本一致”的校验，属于用规则补救设计错误。
- OpenAPI 的 `servers` 本来就是文档级，不是 path item 级。

**文件级 `base_path` 不采纳。** URL 前缀是挂载点，属于 `project.yaml` 的 include，不属于契约文件：

- 同一份 HTTP 契约可能被不同 app 挂到不同前缀（例如管理端 `/admin/account`、用户端 `/account`）；写在文件里会锁死挂载。
- 当前只有一个 api app，收益只是少写几次 `/account`；引入“相对 path + 前缀拼接”的新语义，normalizer、checker、冲突检测都要多一条路径，属于过度设计。
- 若将来确实需要多前缀，正确位置是 include 条目：`{contract: cap.protocols.http.account, prefix: /admin}`，而不是文件字段。

**文件级公共载体只保留 `audience`。** 其余候选均否决：`security` 默认值会弱化“每个 operation 显式声明”的安全审查；`tags`/`servers` 是 OpenAPI 文档级、应由 app 生成；`default_errors` 由平台基线取代（见决策 2）；`shared` 已由 `shared.yaml` 承载。

## 决策 2：错误响应按 HTTP 状态分组，平台只保留基线

原设计有两个问题：

1. 每个文件重复一份 `default_error_responses`，account/auth 只差 404/423，且生成器把 404 注入到“列出账号”这种不可能 404 的 operation。
2. operation 只有扁平的 `errors` 列表，AI 与客户端无法直接看出“哪个状态码会返回哪些业务码”。

新模型：

- `platform/http_conventions.yaml` 声明**平台基线状态**：
  - `error_responses.always: [400, 429, 500, 503]`
  - `error_responses.secured: [401, 403]`（operation 声明了非空 `security` 时生效）
- operation 的非 2xx response 只写业务 codes：
  ```yaml
  409:
    codes:
      - platform.errors.ACCOUNT_NUMBER_TAKEN
  ```
- `COMMON_*` 不在 operation 重复；平台基线状态由中间件/框架产生，归 platform 所有。
- 业务错误可以补在基线状态上（例如受保护端点的 403 补 `ACCOUNT_SUSPENDED`），但不得写空的错误 response。
- 生成 OpenAPI 时，基线状态从 `shared.responses` 注入；业务状态输出现有的 `$ref`，并附带 `x-error-codes: [{code, biz_code}]`，客户端可直接生成错误处理。
- 429/503 的 shared response 必须声明 `Retry-After`（429 原先遗漏，本次补上）。

结果：`listAccounts` 不再声称 404/409/422；`login` 公开、无 401/403 基线，但保留自身 401/403/423 业务码；`/auth/me` 的 401 同时列出 `AUTH_TOKEN_EXPIRED` 与 `AUTH_TOKEN_INVALID`。

## 决策 3：`audience` 是生成过滤器，不是标签

- `platform/http_conventions.yaml` 维护 `audiences: [admin]` 注册表。
- HTTP 文件可写文件级默认 `audience`，operation 可覆盖；每个 operation 必须解析出非空 audience。
- `project.yaml` 的 api app 必须声明 `audience`，且必须在其 include 的每个 HTTP 文件中至少命中一个 operation。
- `gen_openapi` 按 `app.audience` 过滤 operation：不匹配的端点不进入该 app 的 spec。

这样 audience 有确定结果（spec 内容不同），不是给 AI 看的死字段。当前只有 `admin` 一个端，收益是机制先就位；新增用户端时只需加注册表值、app 与 operation 的 audience，不需要改生成器。

## 决策 4：`used_by` 用“页面并集 + 显式缺口”替代 `min_times: 1`

先看事实：16 个 HTTP operation 中有 8 个没有任何页面引用：

```
auth.bind_email / bind_password / bind_phone
auth.logout / refresh / me
auth.verification_send / verification_verify
```

如果直接要求 `used_by` 至少 1 项，这 8 个端点只能靠伪造页面或伪造引用过关，反而污染契约。因此采用：

- 每个 operation 的 `used_by` 必须**精确等于**引用其 service 的 `feature.pages.*` 集合（页面 `data.*.capability` 与 `actions.*.capability` 反查）。多写、少写、写错都会报 `http-consumer`。
- `used_by` 为空的 operation 必须在 `platform/http_conventions.yaml` 的 `unbound_operations` 登记 `operation + reason`；登记项必须对应真实且仍无页面消费者的 operation，否则报“stale”。
- 反向闭合：页面引用的每个 service 必须至少被一个 HTTP operation 暴露。

这等价于“每个端点必须有消费者”（页面或显式登记的 app shell/未建模流程），但不会强迫编造页面。当前 8 个缺口已显式登记，后续补页面时 checker 会要求同步删除登记。

已知建模限制：同一 service 被多个 HTTP operation 绑定时，页面只能反查到 service，无法区分具体 operation；此时 `used_by` 是两个 operation 的并集。当前没有这种绑定。若将来出现，要么拆 service，要么在页面 action 上增加显式 operation 字段，不能靠 `used_by` 猜。

结构性串联结论：`contract-check` 已通过，没有悬空引用；未暴露的 12 个 service 全部是 `visibility: internal` 或由其他 service/事件编排，属于预期；唯一的断点是上述 8 个页面缺口，已显式化。

## 决策 5：验收场景放 `contracts/scenarios/`，生成物放 `apps/api/internal/acceptance/generated/`

**位置选择：**

| 候选 | 结论 | 理由 |
|---|---|---|
| `contracts/scenarios/http/*.yaml` | 采用 | 场景是“必须成立的事实”，可机器校验；与 cap 层形状契约分离，但仍在唯一真相源内 |
| `scripts/` | 否决 | scripts 是工具，不是业务真相；测试数据放这里会变成第二套规格 |
| `apps/api/testdata/` | 否决 | 实现拥有验收期望会导致“实现改测试跟着改”，无法发现漂移 |
| 单独顶层 `scenarios/` | 否决 | 破坏 `contracts/` 单源；index 与组规则还要再分裂一套 |

生成物（SQL fixture、JSON 期望、Go 表驱动用例）放实现侧 `apps/api/internal/acceptance/generated/`，禁止手改；源场景只在 contracts。

**场景形状（草案，新类型 `scenario.http`）：**

```yaml
id: scenario.http.account.create.success
summary: "创建账号成功"
operation: cap.protocols.http.account.create
actor:
  role: admin                       # 取自 platform.security.roles
  permissions: [platform.security.permissions.account_write]
given:
  clock: "2026-01-01T00:00:00Z"     # 注入时钟，禁止墙钟
  id_seq: [1001]                    # 注入 ID 序列
  db:
    data.tables.account: []         # 按表 ID 给完整行
when:
  headers: { Idempotency-Key: "idem-0001" }
  body: { number: "A-001", nickname: "Alice", user_id: null }
then:
  response:
    status: 201
    headers: { X-Request-Id: "<echo>" }
    body:
      code: 0
      message: "ok"
      data:
        id: 1001
        status: pending
        number: "A-001"
        nickname: "Alice"
        user_id: null
        created_at: "2026-01-01T00:00:00Z"
        updated_at: "2026-01-01T00:00:00Z"
        deleted_at: null
  db:
    added:
      data.tables.account:
        - id: 1001
          status: 0
          number: "A-001"
          nickname: "Alice"
          user_id: null
          created_at: "2026-01-01T00:00:00Z"
          updated_at: "2026-01-01T00:00:00Z"
          deleted_at: null
    changed: {}
    removed: {}
    unchanged: strict               # 全库快照 diff，任何未声明变化都失败
```

**从 service 链自动推导，而不是人工猜：**

1. `operation.service` → service 项 → 每个 step 的 `query` / `service` / `lifecycles` / `events`。
2. query 的 `tables[].write` 区分读表与写表；递归展开 `kind: service` 的子服务。
3. checker 要求：
   - `given.db` 覆盖链上所有读表（允许空数组，但必须显式出现）；
   - `then.db.added/changed/removed` 覆盖链上所有写表；
   - 出现链外表的任何变化 → 失败；
   - 每个非 `COMMON_*` 错误码至少一个场景断言；
   - 每个 2xx 状态至少一个成功场景；
   - service 的每个分支（`when` / `goto` 分支）至少被一个场景覆盖；
   - 每个 query 至少被一个场景执行（读或写）。
4. 运行器在真实 PG 上为每个场景建独立 schema/database，应用 migrations，按场景 seed；执行 `httptest` 请求；对**整库**做 before/after 快照 diff，逐行精确比较。
5. 鉴权矩阵由场景显式覆盖：`actor.anonymous`（401）、缺权限（403）、正确权限（成功）；`idempotency: optional/required` 的 operation 必须有一个重复请求场景，断言“同一响应 + 一次持久化”。

**“百分百验收”的诚实边界：** 该模型能百分百验收“确定性请求 → 响应 → 持久数据”的数据流，包括错误码、状态码、header、DB 行的增加/修改/删除。它不能覆盖并发竞态、外部依赖真实行为、限流时间窗、以及未声明的业务语义；这些需要并发测试、契约测试或独立规则，不能靠同一份 scenario 冒充。要满足“无需额外测试数据”的前提是：时钟、ID、随机数、外部副作用全部可注入；否则必须在 `then` 里声明忽略项，而忽略项本身就是验收缺口，必须在 ADR/审查中显式批准。

## 被否决的替代方案

- **每文件 `version: v1`**：与 app 级版本重复，且无法表达同一文件多版本复用。
- **每文件 `base_path`**：把部署挂载点写死进契约；多端挂载会冲突。
- **platform 统一 `default_error_responses` 列表**：仍是“每个端点都声称可能返回 404/409”，且无法表达业务码；改为基线 + 业务 codes。
- **`used_by` 强制 `min_items: 1`**：16 个端点中 8 个没有页面消费者，会逼出假页面；改为页面并集 + 显式缺口登记。
- **测试数据放 `apps/api/testdata`**：实现拥有期望，契约漂移无法被发现。

## 后续

- 实现 `scenario.http` 类型、checker 与 Go 运行器（独立 PR；契约 + 生成物 + 实现 + 测试同一 PR）。
- 为首批 operation 补场景：`account.create`、`account.list`、`auth.login`、`auth.refresh`、`verification.verify`。
- 补时钟/ID 注入设施（若实现层尚不可注入），否则无法做到全库精确 diff。
- 评估 413/415/410：`status_codes` 已列出但 `shared.responses` 与 `platform.errors` 未定义，属于下一轮契约缺口，不在本次扩展。
