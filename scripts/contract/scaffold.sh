#!/usr/bin/env bash
# 按 contracts/_meta 字段规范生成契约骨架。
#
# 用法（命名参数，不接受位置参数）：
#   type=data.entities name=note table=account summary="备注" ./scripts/contract/scaffold.sh
#   make contract-new type=data.entities name=note table=account summary="备注"
#
# type/name 必填；module 默认 name；summary 默认 name；table 默认 name。
# data.entities 的 module/column_type 忽略；data.queries 的 column_type 忽略。
#
# 字段树来自 _meta/<type>.yaml；本脚本只负责参数与输出路径。

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

usage() {
  echo "usage: type=<type> name=<name> [module=<module>] [summary=<text>] [column_type=<family>] [table=<stem>] $0" >&2
  echo "       make contract-new type=<type> name=<name> [module=...] [summary=...] [column_type=...] [table=...]" >&2
  echo "  type: data.tables | data.enums | data.entities | data.queries" >&2
  exit 2
}

if [[ $# -gt 0 ]]; then
  echo "positional arguments are not supported; use named parameters." >&2
  usage
fi

type="${type:-}"
name="${name:-}"
module="${module:-${name}}"
summary="${summary:-${name}}"
column_type="${column_type:-}"
table="${table:-${name}}"

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
    go run ./contract_new -root "$ROOT" -type data.entities -name "$name" -table "$table" -summary "$summary" -out auto
    ;;

  data.queries)
    go run ./contract_new -root "$ROOT" -type data.queries -name "$name" -module "$module" -table "$table" -summary "$summary" -out auto
    ;;

  *)
    usage
    ;;
esac
