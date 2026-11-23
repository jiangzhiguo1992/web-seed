# ADR 0001：HTTP 契约的边界与验收模型

- 状态：已采纳（mount、错误模型、audience、consumer、多 app 已实现；验收场景类型/checker/生成清单已实现，runner 待实现）
- 日期：2026-11-23
- 关联：`contracts/capabilities/protocols/http/*.yaml`、`contracts/platform/http_conventions.yaml`、`contracts/_meta/cap.protocols.http.yaml`、`contracts/_meta/scenario.http.yaml`

## 背景

对 HTTP 契约提出六类问题：文件级 base_path 与版本、默认错误响应的归属、调用端（audience）、`used_by` 与页面串联、按状态聚合错误码、以及可百分百验收的测试数据。本文记录最终取舍、已落地范围与未实现边界。

## 决策 1：`mount` 承载 base path 与 audience，版本仍在 app

文件级公共载体定名为 `mount`：

```yaml
mount:
  path: /account/admin
  audience: [admin]
paths:
  /: ...
  /{id}: ...
```

- `mount.path` 是资源前缀，`paths` 的 key 是相对路径；生成 OpenAPI 时拼接为完整 path。
- URL 层级强制“资源在前、端在后”：`/account/admin` 合法，`/admin/account` 由 `http-mount` 拒绝。当前账号管理端因此挂在 `/account/admin`。
- `mount.audience` 是文件级默认调用端；operation 可覆盖。值必须在 `platform/http_conventions.audiences` 中。
- **版本仍只在 `project.yaml` 的 `app.api_version`**。`mount.path` 不写 `/v1`；生成器输出 `servers: [{url: /v1}]`。版本是 app 级事实，同一文件可被 v1/v2 app 复用；写进文件会迫使同一 app 的所有 include 再做版本一致性校验。
- `shared.yaml` 没有 `paths`，因此没有 `mount`。

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
- operation 的非 2xx response 只写业务 codes：
  ```yaml
  409:
    codes: [platform.errors.ACCOUNT_NUMBER_TAKEN]
  ```
- `COMMON_*` 不在 operation 重复；基线状态由生成器注入。
- 业务错误可以补在基线状态上（如受保护端点的 403 补 `ACCOUNT_SUSPENDED`），但不得写空的错误 response。
- 生成 OpenAPI 时，业务响应附带 `x-error-codes: [{code, biz_code}]`，客户端/AI 可直接看到错误清单。
- 429/503 的 shared response 必须声明 `Retry-After`；423/429/503 的 shared response 已覆盖。
- 410/413/415 已补齐 `platform.errors`（`COMMON_GONE`/`COMMON_PAYLOAD_TOO_LARGE`/`COMMON_UNSUPPORTED_MEDIA_TYPE`）与 `shared.responses`。

结果：`listAccounts` 不再声称 404/409/422；`login` 公开、无 401/403 基线，但保留自身 401/403/423 业务码。

## 决策 3：audience 是生成过滤器，不是标签

- `platform/http_conventions.audiences: [admin, user]` 是注册表。
- HTTP 文件在 `mount.audience` 写默认端，operation 可覆盖。
- `project.yaml` 的 api app 必须声明 `audience`，且必须在其 include 的每个 HTTP 文件中至少命中一个 operation。
- `gen_openapi` 按 `app.audience` 过滤 operation；不匹配的端点不进入该 app 的 spec。

本次新增三个 api app：`admin`（v1，管理端）、`admin_v2`（v2，复用同一组文件）、`user`（v1，只 include `auth`），验证了版本复用与多端 include。当前 `auth` 的 `mount.audience: [admin, user]`；账号管理只属于 `admin`。

## 决策 4：`used_by` 非空且精确等于页面引用

不再使用 `unbound_operations` 豁免清单。每个 operation 的 `used_by` 必须非空，且精确等于引用其 service 的 `feature.pages.*` 集合（页面 `data.*.capability` 与 `actions.*.capability` 反查）。缺页面就补页面：

- `auth.refresh` / `logout` / `me` → 新增 `feature.pages.session`（数据 `token.verify`，动作 `refresh`/`logout`）。
- `auth.verification_send` / `verify` → 补到 `feature.pages.login` 的 `send_code`/`verify_code` 动作与验证码字段。
- `auth.bind_password` / `bind_phone` / `bind_email` → 新增 `feature.pages.account_security`（数据 `account.get`，动作三个 bind）。
- `account.get` 同时被 `account_detail` 与 `account_security` 引用。

反向闭合：页面引用的每个 service 必须至少被一个 HTTP operation 暴露。当前 16 个 operation 全部有页面消费者，`contract-check` 通过。

已知建模限制：同一 service 被多个 HTTP operation 绑定时，页面只能反查到 service，`used_by` 是两个 operation 的并集。当前没有这种绑定；若将来出现，要么拆 service，要么在页面 action 上增加显式 operation 字段。

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
      headers: { Idempotency-Key: "idem-0001" }
      body: { number: "A-001", nickname: "Alice", user_id: null }
    then:
      response:
        status: 201
        body:
          code: 0
          message: "ok"
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
- **基线/业务码合并**：同一状态既可能是平台基线（403）又有业务码（`ACCOUNT_SUSPENDED`），合并规则是“状态由基线注入，codes 由 operation 补充”；checker 要求每个 service 错误落到基线或非 2xx response。
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
