# 平台契约（platform）

> 平台层是工程约束：环境、版本、错误、HTTP 约定、质量门槛、配置、架构分层、设计令牌、观测、埋点、安全。
> 这一层只被业务层引用，不反向引用 `data` / `capabilities` / `features`。

---

## 1. 文件职责

| 文件 | 内容 | enforcement |
|---|---|---|
| `environments.yaml` | 环境清单、域名、差异项、密钥来源 | tested |
| `versioning.yaml` | API 版本、契约版本、弃用窗口 | tested（`make contract-compat`） |
| `errors/` | 错误码按域拆分；`_base.yaml` 全局约定，`data_access.yaml` 基础设施基线 | generated+tested |
| `http_conventions.yaml` | 信封、分页、header、状态码映射与平台 HTTP 组件 | generated |
| `quality.yaml` | 覆盖率门槛、必测路径、禁止项 | tested（quality-refs） |
| `config/env.json` | 环境变量 schema | tested（JSON Schema + 环境枚举） |
| `architecture/go_arch.yaml` | Go 分层与模块边界 | tested（architecture-refs） |
| `architecture/web_deps.json` | 前端依赖方向 | tested（层引用校验） |
| `design/tokens.json` | 设计令牌 | generated |
| `observability/log_fields.yaml` | 日志字段、PII 规则 | tested（log-field-refs） |
| `observability/metrics.yaml` | 指标名与标签 | tested（metric-shape） |
| `analytics/events.yaml` | 埋点事件、属性、PII | tested（analytics-refs） |
| `security/permissions.yaml` | 权限点 | generated+tested |
| `security/roles.yaml` | 角色 → 权限映射 | generated+tested |
| `security/auth_policy.yaml` | 会话、token、MFA 策略 | tested |

字段规范在 `contracts/_meta/platform.*.yaml`：`platform.modules`、`platform.security.permissions`、`platform.security.roles`、`platform.security.auth_policy` 的结构与字段顺序由 `contract-check` 校验；`security/access.yaml` 与 `errors/*.yaml` 继续由 `access.go` / `errors.go` 的专门检查校验。

## 2. 硬规则

1. **错误码按域集中定义**，代码里禁止裸字符串；文案 key 由错误码派生。
2. **日志字段名必须登记**，未登记字段 CI 警告；标 `pii: true` 的字段禁止进入日志（生成时剔除）。
3. **权限点只增不改**；删除权限点必须走弃用流程。
4. **架构规则是契约**，`go-arch-lint` / `dependency-cruiser` 违反即 CI 失败。
5. **设计令牌是唯一色值来源**，组件内禁止裸色值（stylelint 校验）。
6. **环境差异必须显式声明**，禁止代码里写 `if env == "prod"`。

### 2.1 类型级负向约束

以下约束适用于该类型的所有契约；具体实例只写差异，不要再复制。

**platform.\***

- 不要绕过本契约直接写常量
- 新增字段必须登记，否则 CI 警告

## 3. 与 docs 的边界

| platform 契约 | docs |
|---|---|
| 机器可校验的配置与常量 | 解释、背景、决策 |
| 改了会影响构建或运行时 | 改了只影响阅读 |

例如：`quality.yaml` 是门槛（改了就影响 CI），`docs/guides/testing.md` 是解释怎么测。

## 4. enforcement 真相

`index.yaml` 的 `enforcement` 必须与实际校验一致。当前：

- `tested`：`platform.environments`（schema + environment-shape）、`platform.versioning`（schema + versioning-refs）、`platform.quality`（schema + quality-refs）、`platform.analytics.events`（schema + analytics-refs）、`platform.observability.log_fields`（schema + log-field-refs）、`platform.observability.metrics`（schema + metric-shape）、`platform.architecture.go_arch`（schema + architecture-refs）、`platform.security.access`（schema + access-coverage）、`platform.security.roles`（schema + role-inheritance/role-permissions）、`platform.security.auth_policy`（schema）、`platform.security.permissions`（schema + refs）、`feature.routes`（schema + route-structure/route-redirect）。
- `generated`：`platform.http_conventions`（gen_openapi）、`platform.design.tokens`（web design-tokens 构建读取）。
- `review-only`：无。JSON 契约已接结构校验；`gen-env.sh` 的生成能力仍未实现，但 schema 本身已测试。
