---
name: contract-change
description: 修改契约（接口 / 表 / 枚举 / 状态机 / 页面 / 配置等）的完整流程
targets: [opencode, claude, cursor]
---

# 修改契约

> 适用：任何对 `contracts/` 的改动。
> 不适用：只改文档、只改实现（后者本身违规，见文末）。
>
> 状态：P2 生成器待落地；`make contract-check` 试点可用（data.tables / data.enums / used_by）。

---

## 0. 硬前提

1. **契约是唯一真相源**，实现是投影。改行为 = 改契约，不是改代码。
2. **一个 PR 内闭环**：契约 + 生成物 + 实现 + 测试。禁止"先合契约、实现下个 PR 补"。
3. **单次最多改 5 个契约文件**。超过说明任务粒度太大，先拆任务。
4. **写/改前读 `contracts/_meta/README.md` + `_meta/<type>.yaml`**，不要凭字段名猜语义。

---

## 1. 定位要改的契约

```bash
# 只看摘要，不读正文
grep -n "<关键词>" contracts/index.yaml
```

或在 `contracts/index.yaml` 中按 `summary` / `id` 检索。

**不要凭记忆写契约 ID 或字段名**——必须打开文件确认；字段语义见 `contracts/_meta/<type>.yaml`。

---

## 2. 判断变更类型

| 类型 | 例子 | 附加动作 |
|---|---|---|
| 新增 | 新端点、新表、新页面 | 新建契约文件 + 登记 `index.yaml` |
| 扩展 | 加可选字段、加枚举值 | 检查是否破坏兼容 |
| 修改语义 | 字段含义变、状态转换变 | **按破坏性变更处理**，走 3.5 演进规则 |
| 删除 | 删端点、删字段 | 先标 `deprecated`，窗口结束再删 |

四问必须回答：

1. 是否破坏兼容？
2. 是否需要新 API 版本（`/v2`）？
3. 是否需要数据迁移？
4. 影响面有哪些？

影响面（生成器落地后）：

```bash
make contract-impact id=<被修改的 ID>
```

---

## 3. 改契约

按组定位文件：

| 要改什么 | 改哪里 |
|---|---|
| 接口 | `contracts/capabilities/http/<module>.yaml` |
| 应用包含哪些接口 | `contracts/project.yaml` 的 `apps` 段 |
| 定时任务 / 消费者 | `contracts/capabilities/async/<job>.yaml` |
| 跨模块调用 | `contracts/capabilities/services/<module>.yaml` |
| 表结构 | `contracts/data/tables/<file>.yaml` |
| 查询 | `contracts/data/queries/<module>.yaml` |
| 枚举 | `contracts/data/enums.yaml` |
| 实体 / 状态机 / 规则 / 流程 | `contracts/business/**` |
| 路由 / 页面 / 用户旅程 | `contracts/features/**` |
| 错误码 / 权限 / 配置 / 环境 | `contracts/platform/**` |

改的时候：

- 引用写 **ID**，不写文件路径
- 引用只写在结构化字段（`ref` / `refs` / `capability` / `use` / `emits` / `x-*`）
- `features/**` **禁止**引用 `data.*`
- 未实现的能力用 `planned:<id>`，落地后替换
- 新契约文件必须同步 `contracts/index.yaml`

### 3.1 新建骨架

新增表 / 枚举时，先用模板，避免漏字段：

```bash
make contract-new type=data.tables name=note module=notes summary="备注表"
make contract-new type=data.tables name=account_balance module=accounts summary="账户余额" column_type=decimal
make contract-new type=data.enums  name=note_status
```

`column_type` 可选；指定后第一列使用该类型族，并自动带出该族的 required/default 扩展字段（例如 `decimal` 带 `precision` / `scale`）。

生成后替换所有 `<...>` 占位符。字段规范：`contracts/_meta/data.tables.yaml`。

### 3.2 校验

```bash
make contract-check
```

校验结构、ID、`extends`、基类列、约束/索引命名与字段、`do_not` 具体性、`owner_module` 写归属、`used_by` 三列表（`queries` 为查询项 ID）。不通过不要进入生成。

修改 `rules` / `schema_rules` 的 `implemented_by` 后，运行 `make gen-rule-ids` 更新 `rule_ids_gen.go`；`make contract-check` 会拒绝过期的生成文件。

---

## 4. 生成

```bash
make gen
```

按需单独执行：

```bash
make gen-openapi   # capabilities/http/* + apps/* → generated/openapi/<app>.yaml
make gen-db        # data/tables/* → schema.sql + migrations
make gen-graph     # refs → generated/{manifest.json,graph.json,lineage.md}
make gen-client    # 合并 spec → apps/web/packages/api-client/src/generated
```

> 生成物入库。**禁止手改** `contracts/generated/`、`apps/api/internal/generated/`、`apps/api/migrations/`。

---

## 5. 修编译错误（关键步骤）

生成后必然出现编译失败，这些失败就是"实现还没跟上契约"的清单：

| 报错 | 含义 | 正确做法 |
|---|---|---|
| `*Server` does not implement `StrictServerInterface` | 新端点没实现 | 在 `composition/<app>/handlers` 补方法 |
| 字段未定义 / 类型不匹配 | 结构体与契约不符 | 改实现，不改生成物 |
| TS 类型缺失 | 前端未接新接口 | 用 `apps/web/packages/api-client` 的生成类型 |
| sqlc 参数类型不符 | 查询契约改了 | 改 `data/queries`，不要手改 `.sql` |

**不要通过改生成物来消除报错。**

---

## 6. 跑闸门

```bash
make contract-check          # 结构 / ID / 约束 / 索引 / used_by
make contract-graph          # 重复 ID / 悬空 / 越层 / 循环 / 孤儿
make gen && git diff --exit-code   # 生成物是否同步
make test-api                # 路由集合比对 + 响应 schema 校验
make test-web                # client 覆盖断言 + 页面四态
make contract-compat         # oasdiff：破坏性变更检测
```

任一失败都不允许提交。

> P4 之前，`test-api` 只跑现有单测（路由比对与响应校验尚未落地）；P5 之前 `contract-compat` / `db-verify` 为占位。命令会明确提示未实现，不会静默通过。

---

## 7. 数据变更的额外步骤

改了 `data/tables/*` 或 `data/queries/*` 时：

```bash
make gen-db
make db-verify      # 空库 apply 全部迁移 → dump → 与契约目标态比对
```

- 迁移由生成器产出，禁止手写
- 生成器无法表达时（如 `CREATE INDEX CONCURRENTLY`），允许人工调整并标：
  ```sql
  -- manual-adjust: CONCURRENTLY 无法在事务内执行
  ```
- 禁止 `DROP COLUMN` / `RENAME COLUMN` / 原地改类型（见 `data/_policy.yaml`）

---

## 8. 提交

PR 必须包含：

- [ ] 契约改动
- [ ] 生成物（`make gen` 产出）
- [ ] 实现改动
- [ ] 测试改动
- [ ] 影响面报告（`make contract-impact` 输出，或手工列出）
- [ ] 若新增约束：说明本次沉淀了哪条（见 `contracts/README.md` 5.4）

PR 描述模板已包含上述清单。

---

## 9. 完成定义（DoD）

- [ ] `make gen` 幂等（连续两次无 diff）
- [ ] `make contract-check` 通过
- [ ] `make contract-graph` 通过
- [ ] 编译通过（Go + TS）
- [ ] 测试通过（含路由比对、响应 schema）
- [ ] 破坏性变更已升版本或走弃用流程
- [ ] 数据变更已通过 `db-verify`
- [ ] `index.yaml` 已同步（新增/删除契约文件时）
- [ ] 生成物未被手改

---

## 10. 常见错误

| 错误 | 后果 | 正确做法 |
|---|---|---|
| 先写代码后补契约 | 契约变成事后描述，必然漂移 | 先改契约 |
| 手改 `generated/` | 下次生成被覆盖，且 CI 失败 | 改契约后重新生成 |
| 手写 migrations | 与契约目标态不一致 | 改 `data/tables`，跑 `gen-db` |
| 在页面契约里写 `data.*` | 前端绕过 API 语义 | 引用 `capability` |
| 在注释里写契约 ID | 图校验无法检测，等于没写 | 写到 `ref` / `refs` 字段 |
| 一个 PR 只改契约 | CI 长期红，闸门失效 | 同一 PR 闭环 |
| 删除契约文件不同步 index | 图校验报孤儿/悬空 | 同步 `index.yaml` |
| 用 mock 验证契约一致性 | mock 永远符合契约，测不出漂移 | 真实 handler + `httptest` |
| 凭字段名猜语义（如 `extends`） | 写成 PG `INHERITS` / 外键 / 组合 | 读 `_meta/<type>.yaml` 的 `description` / `caution` |

---

## 11. 相关

- 契约规范：[`contracts/README.md`](../../contracts/README.md)
- 契约工具链：[`contracts/_meta/README.md`](../../contracts/_meta/README.md)
- 类型规范：[`contracts/_meta/`](../../contracts/_meta/)
- 一致性机制：[`docs/guides/architecture.md`](../../docs/guides/architecture.md) 附录 A
- 数据规范：[`contracts/data/README.md`](../../contracts/data/README.md)
- 迁移策略：[`contracts/data/_policy.yaml`](../../contracts/data/_policy.yaml)
