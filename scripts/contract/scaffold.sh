#!/usr/bin/env bash
# 从 _meta/templates 生成契约骨架。
#
# 用法：
#   ./scripts/contract/scaffold.sh data.tables <name> [module] [summary]
#   ./scripts/contract/scaffold.sh data.enums  <name>
#
# 只生成骨架，不写业务语义；生成后必须替换 <...> 占位符并跑 make contract-check。

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

escape_sed() { printf '%s' "$1" | sed -e 's/[&|]/\\&/g'; }
pascal() {
  printf '%s' "$1" | awk -F_ '{ for (i=1; i<=NF; i++) $i = toupper(substr($i,1,1)) substr($i,2) } 1' OFS=''
}

case "$type" in
  data.tables)
    target="$ROOT/contracts/data/tables/$name.yaml"
    [[ -e "$target" ]] && { echo "already exists: ${target#$ROOT/}" >&2; exit 1; }
    enum_name="${name}_status"
    sed \
      -e "s|<file>|$(escape_sed "$name")|g" \
      -e "s|<module>|$(escape_sed "$module")|g" \
      -e "s|<summary>|$(escape_sed "$summary")|g" \
      -e "s|<enum_name>|$(escape_sed "$enum_name")|g" \
      -e "s|<initial_value>|active|g" \
      -e "s|<physical_table>|$(escape_sed "$name")|g" \
      -e "s|<column>|name|g" \
      "$ROOT/contracts/_meta/templates/data.tables.yaml" > "$target"
    echo "created ${target#$ROOT/}"
    echo "next: replace <...>; register in contracts/index.yaml; add data.enums.${enum_name} if missing; make contract-check; make gen"
    ;;

  data.enums)
    pc="$(pascal "$name")"
    sed \
      -e "s|<name>|$(escape_sed "$name")|g" \
      -e "s|<description>|$(escape_sed "$name")|g" \
      -e "s|<value>|$(escape_sed "$name")|g" \
      -e "s|<PascalCase>|$(escape_sed "$pc")|g" \
      "$ROOT/contracts/_meta/templates/data.enums.item.yaml"
    ;;

  *)
    usage
    ;;
esac
