# 术语表

> enforcement: review-only
> 唯一命名来源。代码、契约、UI 文案、日志字段必须使用本表术语。
> 禁止同义词漂移（例如同时出现 `account` / `user` / `member`）。

| 术语 | 英文 / 标识符 | 定义 | 禁止使用 |
|---|---|---|---|
| 账号 | account | 可登录主体，含邮箱、密码哈希、状态 | user、member（`user` 仅保留给未来 C 端） |
| 待激活 | pending | 账号初始状态，不能登录 | inactive、new |
| 正常 | active | 可登录的状态 | enabled、live |
| 已封禁 | suspended | 违规被封禁，可解封 | banned、disabled、locked（locked 是临时锁定） |
| 锁定 | locked | 连续登录失败的临时状态，到期自动解除 | suspended |
| 软删 | soft delete | 标记 `deleted_at`，数据保留 | 硬删、物理删 |
| 密码哈希 | password_hash | argon2id 结果，永不外泄 | password、pwd、secret |
| 版本号 | version | 乐观锁计数，每次写入 +1 | revision、rev、etag |
| 幂等键 | idempotency key | 客户端生成的请求唯一标识 | request key、dedup key |
| 权限点 | permission | 形如 `account_read` 的最小授权单元 | scope、role（role 是权限集合） |
| 角色 | role | 权限点的集合 | group、team |
| 能力 | capability | 可调用单元（HTTP / 异步 / service / flow） | API、endpoint（仅指 HTTP 时可用） |
| 功能 | feature | 用户可见的功能单元 | module、function |
| 业务规格 | business | 实体、状态机、规则、事件；不含实现 | logic、core |
| 血缘 | lineage | 契约之间的引用链路 | dependency graph |

## 命名约定

- 代码标识符：`snake_case`（YAML 字段、DB 列） / `PascalCase`（Go 类型） / `camelCase`（TS 变量）
- 契约 ID：`<group>.<type>.<scope>.<name>[.<item>]`
- 事件名：`<scope>.<past_tense>`（`account.activated`）
- 错误码：`<SCOPE>_<REASON>`（`ACCOUNT_NOT_FOUND`）
- 权限点：`<resource>_<action>`（`account_read`）
