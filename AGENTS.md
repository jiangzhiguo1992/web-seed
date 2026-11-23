# AGENTS.md

## Agent 交互协议
- 覆盖全局AGENTS.md里的**Git commit**准则：未经允许，git不能擅自commit和push。不要更改代码的暂存状态(用户自己改的)，且新的修改不叠加到暂存里。

## 项目概述
> 参考文档 [`README.md`](README.md)

## 契约工具链（强制）

- 新建/修改 `contracts/` 前，先读 `contracts/_meta/README.md` 与对应 `_meta/<type>.yaml`；不要凭字段名猜语义。
- 新建契约用 `make contract-new`；改完跑 `make contract-check`；`make gen` 只负责契约 → 产物。
- `contracts/generated/` 只放产物；字段规范在 `contracts/_meta/`，脚本在 `scripts/contract/`，Go 工具在 `scripts/cmd/`。
