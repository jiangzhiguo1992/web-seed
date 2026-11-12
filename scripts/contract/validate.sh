#!/usr/bin/env bash
# 契约校验入口。当前覆盖 data.tables / data.enums / data.entities / data.queries。
# 后续扩展：其余契约类型、graph.json、generated diff。

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT/scripts/cmd"

go run ./gen_rule_ids -check

tmp="$(mktemp "${TMPDIR:-/tmp}/contractcheck.XXXXXX")"
trap 'rm -f "$tmp"' EXIT
go build -o "$tmp" ./contract_check
"$tmp" -root "$ROOT"
