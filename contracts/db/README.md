# 数据契约约定

> 本文是数据库契约的**说明文档**，不是契约本身。表结构以 `apps/api/migrations/` 为唯一真相源。

适用于 `apps/api/migrations/` 与 `apps/api/queries/`。

## 命名

- 表：小写复数，下划线分隔（`notes`、`user_sessions`）
- 索引：`idx_<table>_<cols>`
- 外键：`fk_<table>_<ref>`
- 唯一约束：`uq_<table>_<cols>`

## 标准列

所有业务表必须包含：

```sql
id          uuid PRIMARY KEY,
created_at  timestamptz NOT NULL DEFAULT now(),
updated_at  timestamptz NOT NULL DEFAULT now(),
created_by  uuid,
deleted_at  timestamptz,
version     integer NOT NULL DEFAULT 1
```

- 主键 UUIDv7（有序、索引友好）
- 时间列统一 `timestamptz`，应用层只用 UTC
- `version` 用于乐观锁：`WHERE id = $1 AND version = $2`

## 软删

- 查询默认过滤 `deleted_at IS NULL`，在 repo 层统一，禁止每处手写
- 软删的唯一约束必须用部分索引：

```sql
CREATE UNIQUE INDEX uq_notes_slug ON notes (slug) WHERE deleted_at IS NULL;
```

## 枚举

用 `text + CHECK`，不用 PostgreSQL enum（加值需要 DDL 锁表）：

```sql
status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived'))
```

## 迁移

- 只前进，生产不自动 down；出错用补偿迁移
- 零停机三步：加可空列 → 回填 → 加约束
- 迁移文件不可修改，已合并的迁移视为不可变历史
- 迁移执行时机：独立 job（不在应用启动时自动跑）

## 查询

- 静态查询走 sqlc
- 动态过滤可用 `sqlc.narg`
- 动态排序只能用 `CASE WHEN` 枚举，且仅用于小表；大表手写 pgx 并做白名单拼接
- 复合游标：`(created_at, id) < ($1, $2)`，配套 `(created_at DESC, id DESC)` 索引
