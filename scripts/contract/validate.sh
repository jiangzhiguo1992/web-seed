#!/usr/bin/env bash
# 契约校验入口：rule_ids 与 used_by 新鲜度、data.* 结构/引用、storage/redundancy、capabilities 写归属。
# 后续扩展：其余契约类型、graph.json 越层/循环、generated diff。

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT/scripts/cmd"

go run ./gen_rule_ids -check
go run ./gen_graph -root "$ROOT" -check

tmp="$(mktemp "${TMPDIR:-/tmp}/contractcheck.XXXXXX")"
trap 'rm -f "$tmp"' EXIT
go build -o "$tmp" ./contract_check
"$tmp" -root "$ROOT"
