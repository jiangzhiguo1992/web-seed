#!/usr/bin/env bash
# 契约校验入口：rule_ids 与 used_by 新鲜度 + 全部契约结构与引用校验。
# 生成物（openapi/db/errors）由 make gen-check 单独校验。

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT/scripts/cmd"

go run ./gen_rule_ids -check
go run ./gen_graph -root "$ROOT" -check

# 直接 go run，避免在 SourceTree 等工具的临时目录里生成并执行未签名二进制（macOS 可能 SIGKILL）。
go run ./contract_check -root "$ROOT"
