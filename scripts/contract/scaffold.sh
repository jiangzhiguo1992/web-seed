#!/usr/bin/env bash
# 按 contracts/_meta 字段规范生成契约骨架。
#
# 用法：
#   ./scripts/contract/scaffold.sh data.tables <name> [module] [summary]
#   ./scripts/contract/scaffold.sh data.enums  <name>
#
# 字段树来自 _meta/<type>.yaml；本脚本只负责参数与输出路径。

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
type="${1:-}"
name="${2:-}"
module="${3:-${name}}"
summary="${4:-${name}}"

usage() {
  echo "usage: $0 data.tables <name> [module] [summary]" >&2
  echo "       $0 data.enums  <name>" >&2
  exit 2
}

[[ -n "$type" && -n "$name" ]] || usage
[[ "$name" =~ ^[a-z][a-z0-9_]*$ ]] || { echo "invalid name: $name (snake_case required)" >&2; exit 2; }

cd "$ROOT/apps/api"

case "$type" in
  data.tables)
    target="$ROOT/contracts/data/tables/$name.yaml"
    [[ -e "$target" ]] && { echo "already exists: ${target#$ROOT/}" >&2; exit 1; }
    go run ./cmd/contractnew -root "$ROOT" -type data.tables -name "$name" -module "$module" -summary "$summary" -out "$target"
    ;;

  data.enums)
    go run ./cmd/contractnew -root "$ROOT" -type data.enums -name "$name"
    ;;

  *)
    usage
    ;;
esac
