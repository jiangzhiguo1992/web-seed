# 契约（一等公民）

契约是唯一真相源。代码从契约生成，实现必须通过探测证明自己符合契约。

## 什么算契约

只有同时满足以下两条才是契约：

1. 跨端或跨层被消费（前后端、应用与模块、代码与运维）
2. 可以被机器校验或生成

散文式的"约定"不算契约，放 `docs/reference/`。

## 目录纪律

`contracts/` 下只允许两类文件：

1. 机器可读的契约文件（`.yaml` / `.json`）
2. 该契约的 `README.md`（变更流程、约束说明）

不允许放独立的散文文档；解释性内容放 `docs/`。`contracts/` 是真相源，`docs/` 只引用、不复制。

## 九类契约

| 类型 | 真相源 | 生成/校验 |
|---|---|---|
| 接口 | `openapi/admin.yaml` + `shared/` | Go server 桩 + TS client |
| 数据 | `apps/api/migrations/` + `queries/` | sqlc |
| 配置 | `config/env.schema.json` | `.env.example` + 两端 key 断言 |
| 领域 | `domain/{error-codes,enums}.yaml` | Go 常量 + TS union |
| 设计 | `design/tokens.json` | CSS variables |
| 架构 | `architecture/{go-arch.yaml,web-deps.json}` | go-arch-lint / dependency-cruiser |
| HTTP | `http/envelope.yaml` | 状态码映射、header 名、分页形状（只写规则，不写 schema） |
| 观测 | `observability/{log-fields,metrics}.yaml` | 字段/指标常量 |
| 埋点 | `analytics/events.yaml` | TS 事件类型 + PII 剔除 |

附加：`security/permissions.yaml`（权限点 → Go/TS 常量）。

## 变更三问

改任何契约前必须回答：

1. 是否破坏兼容？是 → 升版本或走弃用流程（`Deprecation` / `Sunset` header）
2. 是否需要新版本？URL major：`/v1`
3. 是否需要数据迁移？是 → 先写迁移，再改契约

## 生成物规则

- 生成物入库，保证派生项目无需工具链也能编译
- 文件头标注 `Code generated ... DO NOT EDIT.`
- CI 必须跑 `make gen && git diff --exit-code`
- 一个 PR 内契约、生成物、实现、测试必须同时更新

## 粒度纪律

- OpenAPI 只描述外部形状，内部 DTO 不进
- 禁止 `oneOf` 与深层 `allOf`；spec 保持平铺
- 响应 schema 默认 `additionalProperties: false`
- 错误码 ≤ 50，分层 `COMMON_*` / `<DOMAIN>_*`
- 只收跨前后端的枚举
