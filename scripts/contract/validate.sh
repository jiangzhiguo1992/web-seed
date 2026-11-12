#!/usr/bin/env bash
# 契约校验入口。当前试点覆盖 data.tables / data.enums。
# 后续扩展：全类型 schema、graph.json、generated diff。

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT/scripts/cmd"

tmp="$(mktemp "${TMPDIR:-/tmp}/contractcheck.XXXXXX")"
trap 'rm -f "$tmp"' EXIT
go build -o "$tmp" ./contract_check
"$tmp" -root "$ROOT"
