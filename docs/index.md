# 文档地图

> AI 进入本仓库的第一站。按顺序读，不要跳读、不要随机翻目录。

## 阅读顺序

1. 根 `AGENTS.md` —— 不可协商的硬规则
2. 本文件 —— 地图
3. `docs/guides/architecture.md` —— 整体设计与实施进度
4. 任务对应的 `docs/playbooks/<task>.md`
5. 相关 `docs/adr/`

## 冲突优先级

```
局部 AGENTS.md > 根 AGENTS.md > playbooks > ADR > guides
```

低优先级内容与高优先级冲突时，一律以高优先级为准，并提 issue 修正低优先级文档。

## 目录

| 路径 | 内容 | 何时读 |
|---|---|---|
| `docs/guides/architecture.md` | 架构、契约体系、实施进度 | 每次任务开始 |
| `docs/playbooks/` | 步骤化操作手册 | 执行具体任务时 |
| `docs/adr/` | 已定决策与否决方案 | 想改架构前 |
| `docs/reference/` | 命名、术语、依赖清单 | 查约定时 |
| `contracts/` | 机器可读的真相源 | 改 API/表/配置前 |

## 硬规则速查

- 改 API 行为前先改 `contracts/`，禁止先写代码后补契约
- 禁止手改 `gen/`，只允许 `make gen`
- 新增依赖必须说明"标准库/现有依赖为何不够"
- 任务超出范围时先停下确认，不要顺手重构
