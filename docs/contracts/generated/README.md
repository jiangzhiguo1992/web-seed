# 契约生成的人读视图

> 本目录的**视图文件**由 `contracts/` 生成，禁止手写；本 README 是说明，不受此限。
> `errors.md` 已由 `make gen-errors` 生成；其余视图仍为 P2。手写视图必然与契约漂移，见 `contracts/README.md` 4.6。

| 文件 | 来源 | 状态 |
|---|---|---|
| `errors.md` | `contracts/platform/errors.yaml` | ✅ `make gen-errors` |
| `permissions.md` | `contracts/platform/security/permissions.yaml` | ⬜ P2 |
| `data.md` | `contracts/data/tables/*.yaml` | ⬜ P2 |
| `lineage.md` | `make gen-graph`（按需生成） | ⬜ P2 |

`errors.md` 的漂移由 `make gen-errors-check` 拦截；其余生成器落地后同样接入 CI diff。
