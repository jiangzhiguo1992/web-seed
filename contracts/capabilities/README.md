# 能力契约（capabilities）

> 能力 = 系统对外或对内的可调用单元。三类触发方式，分别建目录。

---

## 1. 三类能力

| 目录 | 触发方式 | 真相源格式 | 产物 |
|---|---|---|---|
| `http/<module>.yaml` | HTTP 请求 | OpenAPI 3.1 片段 | Go server 类型、TS client、组合后的完整 spec |
| `async/<job>.yaml` | 定时 / 消息 / webhook | 自定义 YAML | job 注册、消费者骨架、重试与死信策略 |
| `services/<module>.yaml` | 进程内跨模块调用 | 自定义 YAML | Go interface、mock、实现断言 |

模块内部私有函数**不进契约**——Go 编译器已能完全约束。

## 1.1 不引用 business

capabilities 是 business 的实现，**契约里不出现 `business.*`**：

- 返回类型用 `data.entities.*`，不用 `data.tables.*.row`
- 事件由 `business/events` 的 `emitted_by` 声明
- 规则由 `business/rules` 的 `implemented_by` 声明

实现代码按 business 契约编写，但契约层不反向引用——两处都写必然漂移。

## 2. HTTP：拆分与组合

```
contracts/capabilities/http/
├── shared.yaml      # 共享组件：Problem、分页、排序、header 参数
├── account.yaml     # 一个业务模块一个文件
└── auth.yaml        # 同上

生成时合并 ──> contracts/generated/openapi/admin.yaml（完整 spec）
           ──> apps/api/internal/generated/openapi/（Go 类型）
           ──> apps/web/packages/api-client/src/generated/（TS 类型）
```

**为什么先拆再合并**：AI 改 account 只读 `account.yaml`（≤200 行），不需要读整个 spec。组合文件是产物，禁止手改。

### 2.1 OpenAPI 扩展字段（必须使用）

| 扩展 | 含义 | 缺省后果 |
|---|---|---|
| `x-idempotent` | 是否幂等，配合 `Idempotency-Key` | 重试可能重复写入 |
| `x-timeout-ms` | 服务端处理超时 | 使用全局默认 |
| `x-rate-limit` | 限流策略 | 无限流 |
| `x-preconditions` | 前置条件，引用 `platform.security.permissions.*` | 条件散落在代码里 |
| `x-side-effects` | 副作用（发事件、写审计） | AI 不知道会触发什么 |
| `x-audit` | 是否写审计日志 | 漏审计 |

### 2.2 schema 纪律

- 响应 schema 必须 `additionalProperties: false`
- 每个 schema 至少一个 `example`
- 禁止 `oneOf` 与深层 `allOf`；保持平铺
- 错误响应统一引用 `shared.yaml` 的 `Problem`

## 3. async：必须声明的五件事

```yaml
trigger:    { type: cron | event | webhook, ... }
input:      { ... }        # 入参 schema
output:     { ... }        # 产出或副作用
idempotency: { key: ..., window: ... }
retry:      { max_attempts, backoff, dead_letter }
```

缺任一项即校验失败——这是定时任务和消费者最容易漂移的地方。

## 4. services：跨模块 service

只登记**跨模块调用**的方法。声明方法名、入参、返回、错误、事务语义，生成 Go interface 与 mock。
模块内部方法不进契约。

`apps/api/internal/modules/*` 之间禁止直接 import 彼此的 repo，只能依赖对方契约生成的 interface。
