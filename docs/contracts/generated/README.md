# 契约生成的人读视图

> 本目录的**视图文件**由 `contracts/` 生成，禁止手写；本 README 是说明，不受此限。
> 生成器未落地（P2），当前只有本说明；手写视图必然与契约漂移，见 `contracts/README.md` 4.6。

| 文件 | 来源 | 状态 |
|---|---|---|
| `errors.md` | `contracts/platform/errors.yaml` | ⬜ P2 |
| `permissions.md` | `contracts/platform/security/permissions.yaml` | ⬜ P2 |
| `data.md` | `contracts/data/tables/*.yaml` | ⬜ P2 |
| `lineage.md` | `make gen-graph`（按需生成） | ⬜ P2 |

生成器落地后，手改由 CI 的 `make gen && git diff --exit-code` 拦截。
