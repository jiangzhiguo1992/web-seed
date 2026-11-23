# AGENTS.md

## 项目概述
> 参考文档 [`README.md`](README.md)

## 契约工具链（强制）

- 新建/修改 `contracts/` 前，先读 `contracts/_meta/README.md` 与对应 `_meta/<type>.yaml`；不要凭字段名猜语义。
- 新建契约用 `make contract-new`；改完跑 `make contract-check`；`make gen` 只负责契约 → 产物。
- `contracts/generated/` 只放产物；字段规范在 `contracts/_meta/`，脚本在 `scripts/contract/`，Go 工具在 `scripts/cmd/`。
