# 平台契约（platform）

> 平台层是工程约束：环境、版本、错误、HTTP 约定、质量门槛、配置、架构分层、设计令牌、观测、埋点、安全。
> 这一层只被业务层引用，不反向引用 `data` / `capabilities` / `business` / `features`。

---

## 1. 文件职责

| 文件 | 内容 | enforcement |
|---|---|---|
| `environments.yaml` | 环境清单、域名、差异项、密钥来源 | tested |
| `versioning.yaml` | API 版本、契约版本、弃用窗口 | tested |
| `errors.yaml` | 错误码、HTTP 映射、文案 key、可重试性 | generated+tested |
| `http_conventions.yaml` | 信封、分页、header、状态码映射 | generated |
| `quality.yaml` | 覆盖率门槛、必测路径、禁止项 | tested |
| `config/env.json` | 环境变量 schema | generated+tested |
| `architecture/go_arch.yaml` | Go 分层与模块边界 | tested（linter） |
| `architecture/web_deps.json` | 前端依赖方向 | tested（linter） |
| `design/tokens.json` | 设计令牌 | generated |
| `observability/log_fields.yaml` | 日志字段、PII 规则 | generated+tested |
| `observability/metrics.yaml` | 指标名与标签 | generated |
| `analytics/events.yaml` | 埋点事件、属性、PII | generated |
| `security/permissions.yaml` | 权限点 | generated+tested |
| `security/roles.yaml` | 角色 → 权限映射 | generated+tested |
| `security/auth_policy.yaml` | 会话、token、MFA 策略 | tested |

## 2. 硬规则

1. **错误码集中定义**，代码里禁止裸字符串；文案 key 由错误码派生。
2. **日志字段名必须登记**，未登记字段 CI 警告；标 `pii: true` 的字段禁止进入日志（生成时剔除）。
3. **权限点只增不改**；删除权限点必须走弃用流程。
4. **架构规则是契约**，`go-arch-lint` / `dependency-cruiser` 违反即 CI 失败。
5. **设计令牌是唯一色值来源**，组件内禁止裸色值（stylelint 校验）。
6. **环境差异必须显式声明**，禁止代码里写 `if env == "prod"`。

## 3. 与 docs 的边界

| platform 契约 | docs |
|---|---|
| 机器可校验的配置与常量 | 解释、背景、决策 |
| 改了会影响构建或运行时 | 改了只影响阅读 |

例如：`quality.yaml` 是门槛（改了就影响 CI），`docs/guides/testing.md` 是解释怎么测。
