# 生成物索引（generated）

> 本目录全部是**产物**，禁止手改。删除后可 `make gen` 重建。
> 契约工具链（schema / templates / scaffold / check）在 `contracts/_meta/`，**不在本目录**。

---

## 1. 内容

| 文件 | 来源 | 用途 |
|---|---|---|
| `manifest.json` | 所有契约的 `generates` 字段汇总 | 生成物 → 来源契约映射 |
| `graph.json` | 所有契约的 `refs` 字段 | 锚点图，供 AI 与影响面分析查询 |
| `inbound.yaml` | `graph.json` 反向索引 | 每张表/契约被谁引用；写入 `tables/*.yaml` 的 `used_by.{entities,queries}`（`queries` 为查询项 ID） |
| `lineage.md` | `graph.json` 渲染 | 人读血缘报告 |
| `openapi/admin.yaml` | `capabilities/http/*.yaml` + `shared.yaml` 合并 | 完整 OpenAPI spec，供 oapi-codegen / Swagger |

## 2. 生成命令

```bash
make gen          # 全部
make gen-graph    # 仅锚点图与血缘
make gen-openapi  # 仅合并 spec
```

## 3. 校验

| 检查 | 失败 |
|---|---|
| 生成物与契约不一致 | CI `git diff --exit-code` 失败 |
| 手改生成物 | 文件头校验 + 重新生成 diff |
| 图存在悬空/越层/循环 | `make contract-graph` 失败 |
| `used_by` 与实际入边不一致 | `make gen-graph && git diff --exit-code` 失败（比较 `entities` / `queries` 两个列表） |

## 4. 为什么入库

派生项目即使未装生成器也能编译；同时让契约变更在 PR 中可见（生成物 diff 即契约影响面）。
