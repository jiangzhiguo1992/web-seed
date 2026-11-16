# 错误码

> 本文件由 `contracts/platform/errors.yaml` 生成，禁止手改。生成命令：`make gen-errors`。

**ID**: `platform.errors`

| code | status | retryable | i18n | message |
|---|---|---|---|---|
| `COMMON_BAD_REQUEST` | 400 | false | `errors.common.bad_request` | 请求参数不合法 |
| `COMMON_UNAUTHORIZED` | 401 | false | `errors.common.unauthorized` | 未认证 |
| `COMMON_FORBIDDEN` | 403 | false | `errors.common.forbidden` | 无权限 |
| `COMMON_NOT_FOUND` | 404 | false | `errors.common.not_found` | 资源不存在 |
| `COMMON_CONFLICT` | 409 | false | `errors.common.conflict` | 状态冲突 |
| `COMMON_IDEMPOTENCY_CONFLICT` | 409 | false | `errors.common.idempotency_conflict` | 幂等键冲突 |
| `COMMON_RATE_LIMITED` | 429 | true | `errors.common.rate_limited` | 请求过于频繁 |
| `COMMON_INTERNAL` | 500 | true | `errors.common.internal` | 服务端错误 |
| `COMMON_AUDIT_FAILED` | 500 | true | `errors.common.audit_failed` | 审计写入失败 |
| `COMMON_LOOP_EXHAUSTED` | 503 | true | `errors.common.loop_exhausted` | 循环次数超过上限，请稍后重试 |
| `COMMON_UNAVAILABLE` | 503 | true | `errors.common.unavailable` | 服务暂不可用 |
| `ACCOUNT_NOT_FOUND` | 404 | false | `errors.account.not_found` | 账号不存在 |
| `ACCOUNT_NUMBER_TAKEN` | 409 | false | `errors.account.number_taken` | 账号编号已被使用 |
| `ACCOUNT_NUMBER_INVALID` | 422 | false | `errors.account.number_invalid` | 账号编号格式不合法 |
| `ACCOUNT_NICKNAME_INVALID` | 422 | false | `errors.account.nickname_invalid` | 昵称长度不合法 |
| `ACCOUNT_OWN_INVALID` | 422 | false | `errors.account.own_invalid` | 账号归属不合法 |
| `ACCOUNT_SUSPENDED` | 403 | false | `errors.account.suspended` | 账号已被封禁 |
| `ACCOUNT_ILLEGAL_TRANSITION` | 409 | false | `errors.account.illegal_transition` | 当前状态不允许该操作 |
| `ACCOUNT_ALREADY_DELETED` | 409 | false | `errors.account.already_deleted` | 账号已删除 |
| `AUTH_INVALID_CREDENTIALS` | 401 | false | `errors.auth.invalid_credentials` | 账号标识或凭据不正确 |
| `AUTH_LOCKED` | 423 | true | `errors.auth.locked` | 认证已锁定，请稍后再试 |
| `AUTH_USERNAME_TAKEN` | 409 | false | `errors.auth.username_taken` | 登录名已被使用 |
| `AUTH_PHONE_TAKEN` | 409 | false | `errors.auth.phone_taken` | 手机号已被绑定 |
| `AUTH_EMAIL_TAKEN` | 409 | false | `errors.auth.email_taken` | 邮箱已被绑定 |
| `AUTH_PHONE_INVALID` | 422 | false | `errors.auth.phone_invalid` | 手机号格式不合法 |
| `AUTH_EMAIL_INVALID` | 422 | false | `errors.auth.email_invalid` | 邮箱格式不合法 |
| `AUTH_VERIFICATION_INVALID` | 422 | false | `errors.auth.verification_invalid` | 验证码不正确 |
| `AUTH_VERIFICATION_EXPIRED` | 422 | false | `errors.auth.verification_expired` | 验证码已过期 |
| `AUTH_VERIFICATION_ATTEMPTS_EXCEEDED` | 423 | true | `errors.auth.verification_attempts_exceeded` | 验证码尝试次数过多 |
| `AUTH_VERIFICATION_TARGET_INVALID` | 422 | false | `errors.auth.verification_target_invalid` | 验证目标格式不合法 |
| `AUTH_WEAK_PASSWORD` | 422 | false | `errors.auth.weak_password` | 密码强度不足 |
| `AUTH_TOKEN_INVALID` | 401 | false | `errors.auth.token_invalid` | 令牌无效 |
| `AUTH_TOKEN_EXPIRED` | 401 | true | `errors.auth.token_expired` | 令牌已过期 |
