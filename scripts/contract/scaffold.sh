#!/usr/bin/env bash
# 按 contracts/_meta 字段规范生成契约骨架。
#
# 用法：
#   ./scripts/contract/scaffold.sh data.tables   <name> [module] [summary] [column-type]
#   ./scripts/contract/scaffold.sh data.enums    <name>
#   ./scripts/contract/scaffold.sh data.entities <name> [table] [summary]
#   ./scripts/contract/scaffold.sh data.queries  <name> [module] [summary] [table]
#
# make 层可用 table=<stem> 覆盖目标表（第 6 个参数）。
#
# 字段树来自 _meta/<type>.yaml；本脚本只负责参数与输出路径。

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
type="${1:-}"
name="${2:-}"
module="${3:-${name}}"
summary="${4:-${name}}"
column_type="${5:-}"
table_override="${6:-}"

usage() {
  echo "usage: $0 data.tables   <name> [module] [summary] [column-type]" >&2
  echo "       $0 data.enums    <name>" >&2
  echo "       $0 data.entities <name> [table] [summary]" >&2
  echo "       $0 data.queries  <name> [module] [summary] [table]" >&2
  exit 2
}

[[ -n "$type" && -n "$name" ]] || usage
[[ "$name" =~ ^[a-z][a-z0-9_]*$ ]] || { echo "invalid name: $name (snake_case required)" >&2; exit 2; }

cd "$ROOT/scripts/cmd"

case "$type" in
  data.tables)
    if [[ -n "$column_type" ]]; then
      go run ./contract_new -root "$ROOT" -type data.tables -name "$name" -module "$module" -summary "$summary" -column-type "$column_type" -out auto
    else
      go run ./contract_new -root "$ROOT" -type data.tables -name "$name" -module "$module" -summary "$summary" -out auto
    fi
    ;;

  data.enums)
    go run ./contract_new -root "$ROOT" -type data.enums -name "$name"
    ;;

  data.entities)
    table="${table_override:-${3:-$name}}"
    summary="${4:-$name}"
    go run ./contract_new -root "$ROOT" -type data.entities -name "$name" -table "$table" -summary "$summary" -out auto
    ;;

  data.queries)
    query_module="${3:-$name}"
    summary="${4:-$name}"
    table="${table_override:-${5:-$name}}"
    go run ./contract_new -root "$ROOT" -type data.queries -name "$name" -module "$query_module" -table "$table" -summary "$summary" -out auto
    ;;

  *)
    usage
    ;;
esac
