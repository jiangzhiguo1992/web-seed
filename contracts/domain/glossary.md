# 术语表

> enforcement: review-only
> 唯一命名来源。代码、契约、UI 文案、日志字段必须使用本表术语。
> 禁止同义词漂移（例如同时出现 `note` / `memo` / `article`）。

| 术语 | 英文 / 标识符 | 定义 | 禁止使用 |
|---|---|---|---|
| 笔记 | note | 用户创建的文本条目，含标题与正文 | memo、article、post |
| 草稿 | draft | 笔记初始状态，仅作者可见 | unpublished、new |
| 已发布 | active | 笔记对外可见的状态 | published、live |
| 归档 | archived | 用户主动隐藏但可恢复 | hidden、disabled |
| 软删 | soft delete | 标记 `deleted_at`，数据保留 | 硬删、物理删 |
| 版本号 | version | 乐观锁计数，每次写入 +1 | revision、rev、etag |
| 幂等键 | idempotency key | 客户端生成的请求唯一标识 | request key、dedup key |
| 权限点 | permission | 形如 `note:read` 的最小授权单元 | scope、role（role 是权限集合） |
| 角色 | role | 权限点的集合 | group、team |
| 能力 | capability | 可调用单元（HTTP / 异步 / 内部 service） | API、endpoint（仅指 HTTP 时可用） |
| 功能 | feature | 用户可见的功能单元 | module、function |
| 领域 | domain | 业务规则与实体 | business、logic |
| 血缘 | lineage | 契约之间的引用链路 | dependency graph |

## 命名约定

- 代码标识符：`snake_case`（YAML 字段、DB 列） / `PascalCase`（Go 类型） / `camelCase`（TS 变量）
- 契约 ID：`<group>.<type>.<scope>.<name>[.<item>]`
- 事件名：`<domain>.<past_tense>`（`note.published`）
- 错误码：`<DOMAIN>_<REASON>`（`NOTE_NOT_FOUND`）
- 权限点：`<resource>_<action>`（`note_read`）
