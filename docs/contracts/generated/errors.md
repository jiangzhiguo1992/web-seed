# 错误码

> 本文件由 `contracts/platform/errors/*.yaml` 生成，禁止手改。生成命令：`make gen-errors`。

**ID**: `platform.errors`

| code | biz_code | status | retryable | i18n | message |
|---|---|---|---|---|---|
| `COMMON_BAD_REQUEST` | 1001 | 400 | false | `errors.common.bad_request` | 请求参数不合法 |
| `COMMON_UNAUTHORIZED` | 1002 | 401 | false | `errors.common.unauthorized` | 未认证 |
| `COMMON_FORBIDDEN` | 1003 | 403 | false | `errors.common.forbidden` | 无权限 |
| `COMMON_NOT_FOUND` | 1004 | 404 | false | `errors.common.not_found` | 资源不存在 |
| `COMMON_CONFLICT` | 1005 | 409 | false | `errors.common.conflict` | 状态冲突 |
| `COMMON_RATE_LIMITED` | 1006 | 429 | true | `errors.common.rate_limited` | 请求过于频繁 |
| `COMMON_INTERNAL` | 1007 | 500 | true | `errors.common.internal` | 服务端错误 |
| `COMMON_AUDIT_FAILED` | 1008 | 500 | true | `errors.common.audit_failed` | 审计写入失败 |
| `COMMON_LOOP_EXHAUSTED` | 1009 | 503 | true | `errors.common.loop_exhausted` | 循环次数超过上限，请稍后重试 |
| `COMMON_UNAVAILABLE` | 1010 | 503 | true | `errors.common.unavailable` | 服务暂不可用 |
| `COMMON_PAYLOAD_TOO_LARGE` | 1011 | 413 | false | `errors.common.payload_too_large` | 请求体超过上限 |
| `COMMON_UNSUPPORTED_MEDIA_TYPE` | 1012 | 415 | false | `errors.common.unsupported_media_type` | 不支持的 Content-Type |
| `COMMON_GONE` | 1013 | 410 | false | `errors.common.gone` | 端点已移除 |
| `COMMON_METHOD_NOT_ALLOWED` | 1014 | 405 | false | `errors.common.method_not_allowed` | 方法不允许 |
| `ACCOUNT_NOT_FOUND` | 2001 | 404 | false | `errors.account.not_found` | 账号不存在 |
| `ACCOUNT_NUMBER_TAKEN` | 2002 | 409 | false | `errors.account.number_taken` | 账号编号已被使用 |
| `ACCOUNT_NUMBER_INVALID` | 2003 | 422 | false | `errors.account.number_invalid` | 账号编号格式不合法 |
| `ACCOUNT_NICKNAME_INVALID` | 2004 | 422 | false | `errors.account.nickname_invalid` | 昵称长度不合法 |
| `ACCOUNT_SUSPENDED` | 2005 | 403 | false | `errors.account.suspended` | 账号已被封禁 |
| `ACCOUNT_ILLEGAL_TRANSITION` | 2006 | 409 | false | `errors.account.illegal_transition` | 当前状态不允许该操作 |
| `ACCOUNT_ALREADY_DELETED` | 2007 | 409 | false | `errors.account.already_deleted` | 账号已删除 |
| `AUTH_INVALID_CREDENTIALS` | 3001 | 401 | false | `errors.auth.invalid_credentials` | 账号标识或凭据不正确 |
| `AUTH_LOCKED` | 3002 | 423 | true | `errors.auth.locked` | 认证已锁定，请稍后再试 |
| `AUTH_USERNAME_TAKEN` | 3003 | 409 | false | `errors.auth.username_taken` | 登录名已被使用 |
| `AUTH_PHONE_TAKEN` | 3004 | 409 | false | `errors.auth.phone_taken` | 手机号已被绑定 |
| `AUTH_EMAIL_TAKEN` | 3005 | 409 | false | `errors.auth.email_taken` | 邮箱已被绑定 |
| `AUTH_PHONE_INVALID` | 3006 | 422 | false | `errors.auth.phone_invalid` | 手机号格式不合法 |
| `AUTH_EMAIL_INVALID` | 3007 | 422 | false | `errors.auth.email_invalid` | 邮箱格式不合法 |
| `AUTH_WEAK_PASSWORD` | 3008 | 422 | false | `errors.auth.weak_password` | 密码强度不足 |
| `AUTH_VERIFICATION_INVALID` | 4001 | 422 | false | `errors.auth.verification_invalid` | 验证码不正确 |
| `AUTH_VERIFICATION_EXPIRED` | 4002 | 422 | false | `errors.auth.verification_expired` | 验证码已过期 |
| `AUTH_VERIFICATION_ATTEMPTS_EXCEEDED` | 4003 | 423 | true | `errors.auth.verification_attempts_exceeded` | 验证码尝试次数过多 |
| `AUTH_VERIFICATION_TARGET_INVALID` | 4004 | 422 | false | `errors.auth.verification_target_invalid` | 验证目标格式不合法 |
| `AUTH_TOKEN_INVALID` | 5001 | 401 | false | `errors.auth.token_invalid` | 令牌无效 |
| `AUTH_TOKEN_EXPIRED` | 5002 | 401 | true | `errors.auth.token_expired` | 令牌已过期 |
| `DICTIONARY_NOT_FOUND` | 7001 | 404 | false | `errors.dictionary.not_found` | 字典不存在 |
| `DICTIONARY_CODE_TAKEN` | 7002 | 409 | false | `errors.dictionary.code_taken` | 字典编码已被使用 |
| `DICTIONARY_INVALID` | 7003 | 422 | false | `errors.dictionary.invalid` | 字典字段不合法 |
| `DICTIONARY_PARENT_INVALID` | 7004 | 422 | false | `errors.dictionary.parent_invalid` | 父节点不存在或不可用 |
| `DICTIONARY_ILLEGAL_TRANSITION` | 7005 | 409 | false | `errors.dictionary.illegal_transition` | 当前状态不允许该操作 |
| `DICTIONARY_HAS_CHILDREN` | 7006 | 409 | false | `errors.dictionary.has_children` | 字典存在子节点，不能删除 |
| `ATTACHMENT_NOT_FOUND` | 8001 | 404 | false | `errors.attachment.not_found` | 附件不存在 |
| `ATTACHMENT_OBJECT_KEY_TAKEN` | 8002 | 409 | false | `errors.attachment.object_key_taken` | 对象 key 已被占用 |
| `ATTACHMENT_OWNER_INVALID` | 8003 | 422 | false | `errors.attachment.owner_invalid` | 附件所有者不存在或不可用 |
| `ATTACHMENT_INVALID` | 8004 | 422 | false | `errors.attachment.invalid` | 附件字段不合法 |
| `ATTACHMENT_ILLEGAL_TRANSITION` | 8005 | 409 | false | `errors.attachment.illegal_transition` | 当前状态不允许该操作 |

## 数据访问基线

以下错误由 repository / transaction 层统一映射，不写入每个 query 的 errors：

- `platform.errors.COMMON_INTERNAL`
- `platform.errors.COMMON_UNAVAILABLE`
- `platform.errors.COMMON_CONFLICT`
