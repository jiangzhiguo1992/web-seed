# ADR 0001：HTTP 契约的边界与验收模型

- 状态：已采纳（mount、错误模型、audience、consumer、多 app、模板封闭化、错误闭包、覆盖率报告已实现；acceptance runner 待实现）
- 修订：文末「2026-11-23 决策修订」「2026-11-23 JWT 认证决策」「2026-11-25 `_types.basics.id` 决策」分别取代本文关于版本归属、`shared.yaml`、`_types.basics.any`、`request.idempotency`、旧认证方案以及 `_types.basics.positive_integer` 的结论。
- 日期：2026-11-23
- 关联：`contracts/capabilities/protocols/http/*.yaml`、`contracts/platform/http_conventions.yaml`、`contracts/_meta/cap.protocols.http.yaml`、`contracts/_meta/scenario.http.yaml`

## 背景

对 HTTP 契约提出六类问题：文件级 base_path 与版本、默认错误响应的归属、调用端（audience）、`used_by` 与页面串联、按状态聚合错误码、以及可百分百验收的测试数据。本文记录最终取舍、已落地范围与未实现边界。

## 决策 1：`mount` 承载 base path 与 audience，版本仍在 app

文件级公共载体定名为 `mount`：

```yaml
mount:
  path: /account
  audience: [admin]
paths:
  /admin: ...
  /admin/{id}: ...
```

- `mount.path` 是公共资源前缀，`paths` 的 key 是相对路径；生成 OpenAPI 时拼接为完整 path。
- audience 段属于 `paths`，不属于 `mount`：`mount: /account` + `paths: /admin` 生成 `/account/admin`；`mount.path` 出现 audience 段会被 `http-mount` 拒绝。
- `mount.audience` 是文件级默认调用端；operation 可覆盖。值必须在 `platform/http_conventions.audiences` 中。
- **版本仍只在 `project.yaml` 的 `app.api_version`**。`mount.path` 不写 `/v1`；生成器输出 `servers: [{url: /v1}]`。版本是 app 级事实，同一文件可被 v1/v2 app 复用；写进文件会迫使同一 app 的所有 include 再做版本一致性校验。
- `shared.yaml` 没有 `paths`，因此没有 `mount`；它只提供 `shared` 组件。
- 模板不再有 `components` 源字段：`shared` 是唯一源，生成时规范化为 OpenAPI `components`。

另外修掉了真正的版本泄漏：`auth_policy.yaml`、`shared.yaml`、`http_conventions.yaml` 里硬编码的 `/v1/auth` 改为 `<app.api_version>/auth`，`_meta/platform.security.auth_policy.yaml` 的 path pattern 允许占位符。

## 决策 2：错误响应按 HTTP 状态分组，平台只保留基线

原设计每个文件重复一份 `default_error_responses`，且把 404 注入“列出账号”这类不可能 404 的 operation。新模型：

- 平台基线在 `platform/http_conventions.yaml`：
  ```yaml
  error_responses:
    always: [400, 429, 500, 503]
    secured: [401, 403]      # operation 声明非空 security 时
    body: [413, 415]         # operation 有 requestBody 时
    deprecated: [410]        # 文件 stability: deprecated 时
  ```
- operation 的非 2xx response 只写业务 errors：
  ```yaml
  409:
    errors: [platform.errors.ACCOUNT_NUMBER_TAKEN]
  ```
- `COMMON_*` 不在 operation 重复；基线状态由生成器注入。
- 业务错误可以补在基线状态上（如受保护端点的 403 补 `ACCOUNT_SUSPENDED`），但不得写空的错误 response。
- 生成 OpenAPI 时，业务响应附带 `x-error-codes: [{code, biz_code, msg}]`，客户端/AI 可直接看到错误清单。
- 429/503 的 shared response 必须声明 `Retry-After`；423/429/503 的 shared response 已覆盖。
- 410/413/415 已补齐 `platform.errors`（`COMMON_GONE`/`COMMON_PAYLOAD_TOO_LARGE`/`COMMON_UNSUPPORTED_MEDIA_TYPE`）与 `shared.responses`。

结果：`listAccounts` 不再声称 404/409/422；`login` 公开、无 401/403 基线，但保留自身 401/403/423 业务码。

## 决策 2.1：错误闭包由脚本递归计算

`service` 不再声明 `errors`；每个 service 的完整错误集由脚本 `computeServiceErrorClosure` 递归计算：

| step kind | 取哪些字段 |
|---|---|
| `query` | `query.errors` + `step.errors` |
| `service` | 未声明 `step.errors`：目标 service 的闭包；声明：只取声明集合（必须是目标子集） |
| `logic` / `when` | `step.errors` |
| `events` / 其他 step | `step.errors` |
| 异步事件 | 不贡献同步错误 |

递归有环保护；`http-errors` 用闭包校验每个 operation 的 `errors`，`COMMON_*` 不进入业务 codes。闭包实现与单元测试在 `scripts/cmd/internal/check/capabilities.go` / `capabilities_rules_test.go`，不依赖 AI 手工枚举。

## 决策 3：audience 是生成过滤器，不是标签

- `platform/http_conventions.audiences: [admin, user]` 是注册表。
- HTTP 文件在 `mount.audience` 写默认端，operation 可覆盖。
- `project.yaml` 的 api app 必须声明 `audience`，且必须在其 include 的每个 HTTP 文件中至少命中一个 operation。
- `gen_openapi` 按 `app.audience` 过滤 operation；不匹配的端点不进入该 app 的 spec。

本次新增三个 api app：`admin`（v1，管理端）、`admin_v2`（v2，复用同一组文件）、`user`（v1，只 include `auth`），验证了版本复用与多端 include。当前 `auth` 的 `mount.audience: [admin, user]`；账号管理只属于 `admin`。

## 决策 4：`used_by` 与页面引用闭合（2026-11-25 调整：允许 API-only）

不再使用 `unbound_operations` 豁免清单。`used_by` 是可选的：省略表示 API-only；一旦存在，必须精确等于引用其 service 的 `feature.pages.*` 集合（页面 `data.*.capability` 与 `actions.*.capability` 反查）。保留页面时仍按原规则闭合：

- `auth.refresh` / `me` / `logout` → 新增 `feature.pages.current_account`（数据 `token.verify`，动作 `refresh` / `logout`）。
- `auth.verification_send` / `verify` → 补到 `feature.pages.login` 的 `send_code`/`verify_code` 动作与验证码字段。
- `auth.bind_password` / `bind_phone` / `bind_email` → 新增 `feature.pages.account_security`（数据 `account.get`，动作三个 bind）。
- `account.get` 同时被 `account_detail` 与 `account_security` 引用。

反向闭合：页面引用的每个 service 必须至少被一个 HTTP operation 暴露；API-only operation 不受页面反查约束。

已知建模限制：同一 service 被多个 HTTP operation 绑定时，页面只能反查到 service，`used_by` 是两个 operation 的并集。当前没有这种绑定；`http-consumer` 现在直接拒绝同一 service 的多 operation 绑定，要求拆 service；若将来必须共享，再在页面 action 上增加显式 operation 字段。

## 决策 5：验收场景源在 contracts，清单在 contracts/generated

- 源场景：`contracts/scenarios/http/<file>.yaml`（新类型 `scenario.http`）。
- 生成清单：`contracts/generated/scenarios/manifest.yaml`（`make gen-scenarios`），包含每个场景解析后的 `method/path/service/有效状态码/codes`，供实现侧 runner 读取。
- 实现侧 runner 尚未实现：当前 `apps/api` 只有装配骨架，没有 handler/DB，无法在契约层伪造端到端执行。runner 落地时必须读生成清单，不得读源 YAML。
- 不选 `scripts/`（工具不是真相）与 `apps/api/testdata`（实现拥有期望，测不出契约漂移）。

场景形状（`scenario.http`）：

```yaml
id: scenario.http.account
scenarios:
  - name: create_success
    operation: cap.protocols.http.account.create
    actor: { anonymous: false, permissions: [platform.security.permissions.account_write] }
    given:
      clock: "2026-01-01T00:00:00Z"
      id_seq: [1001]
      db: { data.tables.account: [] }
    when:
      body: { number: "A-001", nickname: "Alice", user_id: null }
    then:
      response:
        status: 201
        body:
          code: 0
          msg: "ok"
          data: { id: 1001, status: pending, number: "A-001", nickname: "Alice", user_id: null, created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z", deleted_at: null }
      db:
        added:
          data.tables.account:
            - { id: 1001, status: 0, number: "A-001", nickname: "Alice", user_id: null, created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z", deleted_at: null }
        changed: {}
        removed: {}
        unchanged: strict
```

checker 已实现：operation/status/role/permission/event 引用、表 ID 属于 service 链、`given.db`/`then.db` 覆盖链上表、`changed` 行必须含 `key/set`、行只使用表列、`unchanged: strict`。尚未实现：全 operation 覆盖率、每个错误码至少一个场景、分支覆盖、真实 PG 全库 diff runner。当前只落了 `account` 的 3 个场景作为样例；其余 operation 的场景按同一格式补齐。

**百分百验收的边界**：确定性请求 → 响应 → 持久数据可以做到 100%；并发竞态、外部依赖真实行为、限流时间窗不能靠单份场景覆盖，必须显式标注。

## 边界分析（选项 1）

- **多 app 同文件**：`admin` / `admin_v2` 复用同一 HTTP 文件，版本由 app 派生；生成器对每个 app 深拷贝文档，避免生成过程中的 mutation 串味。
- **op 级 audience 冲突**：文件级 `mount.audience` 是默认值，op 覆盖只能收窄/扩展该文件的有效端；`http-audience` 校验每个 app 的 audience 在其 include 文件中至少命中一次。若一个文件同时服务多个端且路径完全不同，应拆文件，而不是在同一 mount 下堆 op 级 audience。
- **基线/业务码合并**：同一状态既可能是平台基线（403）又有业务码（`ACCOUNT_SUSPENDED`），合并规则是“状态由基线注入，errors 由 operation 补充”；checker 要求每个 service 错误落到基线或非 2xx response。
- **used_by 歧义**：见决策 4 的建模限制。

## 被否决的替代方案

- **版本写进 HTTP 文件**：与 app 级版本重复，无法表达同一文件多版本复用。
- **`/admin/account` 顺序**：audience 在前会把部署/权限层级写进资源路径；统一资源在前。
- **`unbound_operations` 豁免清单**：会掩盖页面缺口；已改为补页面并强制非空。
- **测试数据放 `apps/api/testdata`**：实现拥有期望，契约漂移无法被发现。
- **在 `apps` 下生成场景**：污染源码；生成清单放 `contracts/generated/scenarios/`。

## 后续

- 实现 `apps/api` 的 handler/DB 后，落地读 `contracts/generated/scenarios/manifest.yaml` 的 runner：真实 PG、独立 schema、整库 before/after 快照 diff、注入 clock/ID。
- 为其余 13 个 operation 补场景与错误码场景；checker 增加全 operation/错误码/分支覆盖率。
- 评估 502/504：属于网关层，当前不进入 per-op 基线；413/415 已由 `body` 基线覆盖，410 由 `deprecated` 基线覆盖。

## 设计复盘：为什么会出现 `paths: any`

根因不是“模板写少了”，而是契约建模方向错了：早期把 `cap.protocols.http` 当成“OpenAPI + 几个 x-* 注解”，于是 `paths` 直接透传 OpenAPI。透传的代价是：

1. **结构不可判定**：脚本只能看到 `map[string]any`，无法知道 `paths.<p>.<method>` 下应该有哪些字段，只能靠 `http-shape` 事后遍历并报“未知字段”。AI 看到 `any` 也会认为“这里可以自由发挥”，于是漂移是设计的结果，不是执行失误。
2. **语义藏在 x-***：`name/id/service/used_by` 被塞进 `x-contract-id`/`x-service`，normalizer 再拆出来。源文件看起来像 OpenAPI，实际语义靠隐式约定；AI 读源文件时看不到硬约束。
3. **`used_by` 的形状由字段属性约束**：可选；存在时由 `min_items: 1` 与 `pattern` 保证非空且只指向页面，精确集合由 `http-consumer` 校验。
4. **`components` 是迁移逃生口**：它让源文件可以直接写 OpenAPI components，和 `shared` 形成两个源；normalizer 还要处理“二者不能共存”。有逃生口，AI 就会走。

这次改为**封闭结构**：

- `paths` 是 `map<path, object>`，对象只允许 `get/post/put/patch/delete/head/options`；每个 method 复用同一个 operation 节点（YAML anchor 只是写法，结构是固定的）。
- operation 节点固定为 `id/name/service/summary/used_by/security/request/response`；`request` 固定为 `path/query/body`，参数用 `to` 指向 service 参数；`response` 的 key 固定为三位状态码。
- `used_by` 可选；存在时由 `min_items: 1` 保证非空，`http-consumer` 只保留“精确集合”这一条脚本规则。
- 删除 `components` 源字段；`shared` 是唯一源，normalizer 拒绝任何源文件里的 `components`。生成的 OpenAPI `components` 是产物，不是契约。
- `make contract-new` 的 `RenderHTTP` 同步改为新形状；否则脚手架会继续生成旧结构，这是“AI 漂移”的另一个入口。

仍然保留的 `_types.basics.any` 只有两处，且都是**多态叶子**：

- `request.body.type` / `response.body.type`：可以是类型 ID 字符串，也可以是匿名 object；`http-service-type` / `http-service-return` 负责语义。
- `shared.*`：OpenAPI 组件片段；`http-components` / `http-shared` 负责逐项校验。

这两处不是“懒得定义”，而是 YAML 类型系统没有 union；如果强行拆成两个字段，会让 AI 在两种写法之间选择，反而增加漂移。边界写在这里，而不是藏在模板里。

## 硬约束矩阵

| 约束 | 载体 | 脚本 |
|---|---|---|
| 文件 ID / 文件名 | `_meta` targets + pattern | `http-file` |
| 文件级 prefix / audience | `mount` 字段 | `http-mount` / `http-audience` |
| 版本只在 app | `project.yaml` + `http-version` | `http-version` |
| operation 结构 | `paths` 固定 method + operation 字段 | `validateDoc` / `http-shape` |
| `used_by` 非空 | 字段 `required` + `min_items` | 结构校验 |
| `used_by` 精确集合 | 页面引用反查（省略表示 API-only） | `http-consumer` |
| 错误码聚合 | service 错误闭包 | `service-errors` + `http-errors` |
| 入参/出参映射 | 参数级 `to` + 类型 ID | `http-service-params` / `http-service-type` / `http-service-return` |
| 基线错误 | `platform/http_conventions.error_responses` | `http-default-errors` |
| 共享组件 | `shared` | `http-components` / `http-shared` |

## 脚本不做什么

- 不解析事件订阅者来推导同步错误：事件是异步的，不贡献 HTTP 同步错误；`kind: events` 的 `step.errors` 仍然显式声明。
- 不解析实体校验/数据库约束来推导错误：这些必须由 query/service 的 `step.errors` 显式声明；否则脚本只能猜测，违反“确定性”。
- 不检查 `$ref` 类型内部的字段级 nullable/optional：那是被引用类型自己的契约；HTTP 层只校验“类型 ID 一致”。
- 不把参数级 `to` 写入 OpenAPI：它是契约校验用的绑定，不是运行时协议。
- `COMMON_*` 只能落在平台基线状态（400/401/403/429/500/503，以及按条件注入的 413/415/410）；404/409 等非基线状态必须使用领域错误（如 `ACCOUNT_NOT_FOUND`），否则 operation 没有合法位置声明该错误。
- 不在没有实现 runner 时伪造场景执行：场景覆盖率目前是 `gen_scenarios -strict` 的硬命令，未补齐会失败；默认 `make gen-check` 只校验清单最新，不掩盖缺口。

## 当前覆盖率现状

`contracts/generated/scenarios/manifest.yaml` 的 `coverage` 段由 `make gen-scenarios` 生成，列出每个 operation 的 `scenarios`、`success_statuses`、`missing_statuses`、每个错误码的 `covered`。当前只有 account 的 3 个场景，`go run ./gen_scenarios -strict` 会列出 13 个 operation 无场景，以及 create 等已覆盖 operation 的错误码缺口；这是真实信号，不是脚本失败。补齐场景的前提是先有 acceptance runner（真实 PG、整库 diff），否则只会得到“结构合法但语义空洞”的假覆盖。

## 2026-11-23 硬化增量

- `platform/http_conventions.yaml` 的基线解析收敛到 `internal/spec.LoadHTTPPolicy` 一处；checker、`gen_openapi`、`gen_scenarios` 共用，避免三份 `always/secured/body/deprecated` 漂移。
- 生成的 OpenAPI 基线响应附带 `x-error-codes`（如 `COMMON_UNAUTHORIZED`），spec 自描述完整错误面；`gen_openapi` 在写出前用 `kin-openapi` 按 OpenAPI 3.1 做硬校验。
- 同一 service 被多个 operation 绑定会由 `http-consumer` 直接拒绝；当前模型下页面只能反查到 service，多绑定会造成 `used_by` 并集歧义，要求拆 service，真需要共享时再引入 operation 级消费者。

## 2026-11-23 契约层硬化（Batch 1–4）

- `feature.routes` 新增 `_meta` 与 `route-structure` / `route-redirect` 校验；route id/path/name 唯一，page/permission/layout/redirect/not_found 引用闭合。
- `feature.pages.actions` 新增 `params`；`page-params` 校验 data/actions.params 的 key 必须来自 service 参数或对象字段，并校验类型族；`page-action-target` 校验 `on_success` 只允许 `navigate`/`close_and_redirect` 到已声明 route。
- access 新增 `_meta/platform.security.access.yaml` 与 `access-coverage`：每个被 HTTP 暴露的 service 必须有 permissions/audit/rate_limit binding；补齐 token.refresh、token.verify、auth.bind_* 五个绑定。
- `feature.routes`、`platform.environments`、`platform.security.access` 纳入 `checkPlatformContracts`；roles 增加继承环/深度/未知权限/默认角色校验。
- `platform.http_conventions.status_codes` 由 `http-status` 脚本化；`*Page` schema 由 `service-page-schema` 强制 items/offset/limit/has_more。
- 密码强度规则 `feature.rules.auth.password_strength` 接入 bind_password create/update step，`AUTH_WEAK_PASSWORD` 进入 HTTP 422。
- `index.yaml` 的 enforcement 与 README 真相表对齐：无 checker/generator 的 platform 契约降为 `review-only`，不再宣称 tested/generated。

### 本轮边界

- `page-params` 校验“页面声明的参数”是否属于 service 参数/对象字段及类型族；不强制 service 的每个必填参数都出现在页面 params，因为 JWT/path/route 上下文由 HTTP 层提供。它防的是拼写/类型漂移，不是完整性。
- `page-action-target` 只允许 `navigate` / `close_and_redirect` 到 `feature.routes.*`；`close()` 等未来函数需要先扩语法，不能在页面里自由写。
- `access-coverage` 只覆盖被 HTTP 暴露的 service；内部 service、异步 job 不走该规则。
- `role-inheritance` 校验环/深度/未知权限；`role-permissions` 只允许 admin 使用 `*`，不展开继承后的有效权限集。
- `feature.routes.app` 暂未与 `project.yaml` 的 web app 对齐；当前 `web/admin` 是 consumer 标签语义，需单独定名。

### platform checker 边界

- `quality-refs` 只校验 `required_scenarios` 存在、make 目标存在、`must_test.id` 唯一；不判断 gate 是否真的 blocking，也不执行命令。
- `versioning-refs` 只校验 `api.current`、`detection.command` 与弃用头；不校验 `accepted_breaking_changes` 的语义。
- `analytics-refs` 校验事件名/HTTP ref/page/forbidden，并校验 `common_properties` 的 forbidden/命名/重复、事件内 property 重名、`trigger: mount` 必须有 page、`trigger: api_success` 必须有 ref；`analytics-types` 校验 property.type 是标量 basic type、`data.types` 自定义类型或 `data.enums` 枚举名，拒绝 `_types.basics.any` / `_types.basics.json`；不校验 property 的值域约束是否与业务字段一致。值域映射刻意不建模：property 是事件载荷，不保证等于表列/实体字段；直接按名字映射会产生 `account_id(string)` vs 表列 `integer` 这类假阳性；能确定枚举来源的属性（如 `page.account_list_viewed.filter_status`）已改为 `data.enums.account_status`，其余保持事件载荷类型。
- `analytics-pii` 校验 forbidden 名单 snake_case、同一 property 在 common/event 中的 type/pii 一致、pii:true 不得出现在 forbidden，并要求 `pii: true` 的属性被 `log_fields.never_log` 覆盖（`pii: false` 不得命中 never_log）；不把 property 自动映射到表列 sensitivity，表列覆盖由 `log-field-refs` 单独校验。`session_id`/`account_id` 当前按非 PII 关联 ID 处理，合规要求变化时需显式改 `pii: true`。
- `log-field-refs` 校验字段名与 `required_for` 枚举、`never_log` 条目格式/重复，并反查 `data/tables/*.yaml` 中 `sensitivity: pii|secret` 的列是否被 `never_log` 覆盖；不校验日志调用点是否真的使用这些字段。`never_log` 的 `*_body` 曾把 analytics 的 `has_body` 误判为正文，已将属性改名为 `has_payload`；同类布尔字段（如未来的 `has_token`）会命中 `*_token`，命名时必须避开敏感后缀。`TestNeverLogPatterns` 固定了当前 glob 语义：`*_token` 命中 `access_token` 但不命中 `token_type`，`*_email` 命中 `contact_email` 但不命中 `email_verified`。
- `metric-shape` 只校验指标名唯一、kind、forbidden labels；不强制 counter/histogram 的命名后缀。
- `architecture-refs` 校验层/模块引用；不生成或执行 go-arch-lint / dependency-cruiser。
- `auth-policy-jwt` 只校验 JWT 字段（format/issuer/claims/audience/pii/revocation）与策略一致；不执行签名或验签。
- `auth-policy-password` 只校验页面 `password.minLength`；不校验服务端哈希算法或复杂度规则。
- `config.env` 用 JSON Schema 编译，并校验 `required` 含 APP_ENV/HTTP_ADDR、`additionalProperties: false`、APP_ENV/LOG_LEVEL 枚举与默认值、secret 字段为 string；不校验运行时真实值。
- `web_deps` 校验必填字段与类型、`id` 固定值、`stability`/`enforcement` 枚举、层名唯一、`rules.from` 唯一、`mayDependOn`/`forbid` 不重叠、`generates` 非空，并拒绝未声明的 JSON 字段；不执行 dependency-cruiser，不校验 `from.path`/`to.path` 正则。

## 2026-11-23 决策修订

后续实现推翻并取代了本文多项旧结论：

1. **版本归属**：`mount.api_version`（必填，`v<数字>`）成为唯一版本源；生成器按 app 内 include 文件的版本派生 `servers: /<api_version>`，`mount.path` 与 `paths` 不写版本。`project.yaml` 的 `app.api_version` 已删除；同一 app 混版本会被 checker/generator 拒绝。在没有 v2 mount 的情况下，`admin_v2` app 与生成物一并移除，待 v2 契约重做后再加回。
2. **共享组件**：删除 `protocols/http/shared.yaml`，HTTP 文件也不再保留 `shared`。平台级 securityScheme / schema / parameter / header / response 全部在 `platform/http_conventions.yaml` 的 `components`，由 `gen_openapi` 注入每个 api app；HTTP 文件只引用组件名，不再定义文件私有组件。
3. **`_types.basics.any` 退场**：`cap.protocols.http` 不再有 `_types.basics.any`。`request` / `response` 都有独立 `headers`；path/query 参数是带 `name` / `type` / `description` / `to` 的对象列表；body 用 `kind` 区分 json/form/multipart/text/binary；body 类型必须是具名类型 ID，匿名 object 请求体被 `http-service-type` 拒绝。
4. **客户端幂等键删除**：幂等键 header、OpenAPI components 与场景字段全部删除；幂等由 API 内部按业务语义决定，不由客户端传入的 header 控制。
5. **平台链路头删除**：客户端链路头不再写入 HTTP 契约、不接收客户端值、不回传响应；服务端在收到请求时生成内部 request id，注入日志与下游调用。HTTP 契约的 `request.headers` / `response.headers` 保留为独立字段，`Authorization` 通过 header 的 `auth: Bearer` 表达。
6. **安全凭据注入**：`request.headers` 中的 `Authorization` 使用 `auth: Bearer`；认证中间件校验 JWT，并把 `account_id` / `access_token` 注入同名 service 参数；HTTP 参数不得映射到这两个参数，避免“token 校验了但业务信任 body 里的账号 ID”。
7. **契约优先 HTTP 模型**：`request.headers` / `response.headers` 独立于 body；`Content-Type`、`Accept`、`Authorization` 等业务/表示层头在 headers 中声明；`Connection`、`Content-Length`、`Expect`、`Transfer-Encoding` 等传输层头不写进契约；`body.kind` 支持 json/form/multipart/text/binary；response 支持按状态声明 headers 与 body。生成器再把这些映射为 OpenAPI security/content/headers，契约阶段不以 OpenAPI 的字段形状为约束。
8. **账号 ID 类型**：删除 `_types.basics.account_id`，HTTP path 与认证注入的 `account_id` 改用 `_types.basics.positive_integer`（`min: 1`）；普通 service/query 的账号 ID 仍是 `_types.basics.integer.32`。
9. **JWT 认证**：见文末「2026-11-23 JWT 认证决策」；access/refresh 都是 JWT，通过响应体下发。

同时参数级 `to` 的值统一写 `params.<service 参数路径>`（如 `params.input`、`params.status`），让 HTTP 绑定与 service 内部的 `params.*` 引用使用同一套语法。

验证：`make contract-check`、`make gen`、`make gen-rule-ids`、`make test-tools` 通过；OpenAPI `servers` 带 `/<mount.api_version>`，平台组件出现在每个 spec 的 `components`。


## 2026-11-23 JWT 认证决策

认证机制从旧方案改为 JWT，并保留你设计的 token 表作为 JWT 哈希与吊销/轮换记录。以下结论取代本文件所有旧认证方案相关内容：

1. **令牌形态**：access 与 refresh 都是 JWT；`platform.security.auth_policy.token.access/refresh.format` 固定 `jwt`，`issuer` 必须声明，`claims_required` 固定包含 `iss/sub/aud/exp/iat/jti`，`pii_in_token` 固定 false。token 表只存 JWT 哈希与吊销/轮换元数据，不存原文。
2. **传输方式**：登录与刷新只通过响应体返回 JWT；客户端用 `Authorization: Bearer` 携带 access JWT。删除旧认证方案及相关认证与平台组件。
3. **token 表保留**：`data.tables.token`、`data.entities.token`、`data.queries.token`、`cap.lifecycles.token`、`cap.events.token` 全部保留；JWT 哈希用于吊销、refresh rotation/reuse detection 与审计。
4. **端点变化**：`POST /auth/login` 返回 `AuthResult`（含必填 `refresh`）；`POST /auth/refresh` 是公开 operation，请求体 `RefreshInput`，响应 `RefreshResult`；`POST /auth/logout` 保留，请求体 `RefreshInput`，吊销 refresh JWT 记录；access JWT 通过 token 表 revoked_at 立即失效。
5. **失效边界**：`token.revocation.strategy` 固定 `server`。`token.verify` 验签后读取 token 表校验 revoked_at/expiry，因此登出、refresh 轮换、账号删除、改密/换绑手机或邮箱都会吊销 token 表记录，access JWT 即时失效。
6. **账号删除**：`account.delete` 软删认证绑定、作废验证体并调用 `token.revoke_by_account` 吊销 token 表记录；access JWT 随之立即失效。

## 2026-11-24 字段命名、required、msg 与错误字段

1. **值域字段统一叫 `type`**：HTTP 源契约的 header / path / query / body / form field 原先用 `schema` 表示类型 ID，现全部改名为 `type`；OpenAPI 生成物里的 `schema` 键是生成结果，不变。`normalizeHTTPSchema` 只负责把 `type` 值投影为 OpenAPI schema。
2. **`required` 省略为 true**：header / query / body / body.fields 的 `required` 可省略，省略时值为 true；可选项显式写 `required: false`。`request.headers` 与 `response.headers` 列表本身都必填；非 2xx/204 的 `response.headers` 写空列表 `[]`。
3. **request body 块状书写**：`request.body` 按模板顺序写 `kind` / `type` / `to`，`required` 省略为 true，不写成单行 flow mapping；response body 同样块状。格式不改变校验语义。
4. **form 不独立于 body**：`form` / `multipart` 是 `body.kind` 的取值，与 `json` 互斥；`fields` 是表单字段列表，不新增独立的 `form` 顶层字段。
5. **response 保持状态码 map**：键是三位 HTTP 状态码，值写 `msg` / `headers` / `body` / `errors`；`msg` 对应统一信封的 `msg`，所有状态码必填，非 2xx 也必填。多个状态码共享同一响应形状时重复声明；当前没有该需求，不引入 list 或 response alias。
6. **错误字段保持 `errors: list<string>`**：`errors` 是“该状态允许出现的错误 ID 集合”，不是响应体；统一信封由 `platform/http_conventions.components.schemas.ApiErrorResponse` 定义一次。源契约只引用 `platform.errors.*`，生成 OpenAPI 时投影为 `x-error-codes: [{code, biz_code, msg}]`，`msg` 仍由 `platform.errors` 单一源维护。把 `errors` 改成 `list<object>` 会复制 code/msg 并与 `platform.errors` 产生双源漂移，因此不采用。
7. **operation 字段顺序**：固定为 `id` / `name` / `summary` / `service` / `request` / `response` / `used_by`；`used_by` 放最后，避免在 operation 主体中间打断请求/响应阅读。
8. **认证方案名统一为 `Bearer`**：源契约 `auth: Bearer`，平台 `components.securitySchemes.Bearer` 同名；生成 OpenAPI 的 `security` 直接使用该名字，不再维护 `BearerAuth` 的额外映射。

## 2026-11-25 `_types.basics.id` 决策

第 8 条的 `_types.basics.positive_integer` 已删除，账号 ID 不再区分 HTTP 与 service/query。统一规则：

1. **单一 ID 类型**：实体 ID 与外键 ID 全部用 `_types.basics.id`（对应 `_types.basics.integer.64`，0 可作为无值哨兵）；分页、计数、状态码等非 ID 整数继续用 `_types.basics.integer.32`。
2. **全链路覆盖**：表列、entity 字段（由来源列推断）、query/service/event 的 ID 参数和返回、HTTP path ID 参数、`contract-new` 骨架全部使用 `_types.basics.id`。
3. **硬校验**：`reference-target` 比较 integer 列的类型 variant 位宽；`query-param-column` 比较参数与列的位宽；`service-input-type` 比较赋值两端的 integer 位宽；`http-service-params` 比较 path/query/form 参数与 service 声明的 integer 位宽。32 位类型不能绑定或赋给 64 位 ID。
4. **HTTP 取舍**：HTTP path ID 使用 `_types.basics.id` 后不再生成 `minimum: 1`；`0` 视为无效 ID，由查询返回未找到，而不是由 OpenAPI 层拒绝。
5. **不引入 `id_positive`**：HTTP path 与 service/query 共用 `_types.basics.id`，`0` 只作为 service 内部“无值”哨兵（如 auth 的 `binding_id`）。若未来需要路径层拒绝，必须单独设计请求约束类型，不得把哨兵语义塞回领域 ID。当前决定接受 OpenAPI 缺少 `minimum`，以保持单一 ID 类型。
