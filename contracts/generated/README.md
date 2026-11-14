# 机器生成物（generated）

> 本目录只放**机器生成物**，禁止手改；人读视图在 `docs/reference/generated/`。
> 契约规范、模板、脚本都在 `contracts/_meta/` 与 `scripts/`，不在本目录。

---

## 1. 内容

| 文件 | 来源 | 用途 |
|---|---|---|
| `openapi/admin.yaml` | `capabilities/http/*.yaml` + `shared.yaml` 合并 | oapi-codegen / Swagger / `gen-client` 的输入 |

`db` 产物（`schema.sql`、`migrations/*.sql`）由 `gen-db` 输出到 `apps/api/`，不在本目录。

## 2. 生成命令

```bash
make gen-openapi   # 合并 spec
```

`make gen` 是聚合入口，当前尚未实现；`make gen-graph` 只写回 table/entity 的 `used_by.queries`，不产出本目录文件。

## 3. 不再入库的产物

| 产物 | 去向 | 原因 |
|---|---|---|
| `graph.json` | 按需生成（临时目录/CI） | 只有影响面分析需要时生成，避免与契约引用重复 |
| `lineage.md` | `docs/reference/generated/lineage.md` | 人读文档统一进 `docs/` |
| `manifest.json` | 删除设计 | `contracts/index.yaml` + `project.yaml` 已覆盖，无消费者 |
| `inbound.yaml` | 删除设计 | `used_by.queries` / `table.entities` / `model.included_by` 已是反向索引 |

## 4. 校验

| 检查 | 失败 |
|---|---|
| 生成物与契约不一致 | CI `git diff --exit-code` 失败 |
| 手改生成物 | 重新生成 diff 失败 |
| OpenAPI 合并漂移 | `make gen-openapi && git diff --exit-code` 失败（待实现） |
