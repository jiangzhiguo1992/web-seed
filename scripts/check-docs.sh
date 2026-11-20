#!/usr/bin/env bash
# 检查文档与仓库关键路径是否一致。
# 目前做最小实现：关键文件存在性 + markdown 内相对链接存在性。
set -euo pipefail
cd "$(dirname "$0")/.."

missing=0

required=(
  "AGENTS.md"
  "README.md"
  "docs/index.md"
  "docs/guides/architecture.md"
  "contracts/README.md"
  "contracts/index.yaml"
  "contracts/data/README.md"
  "apps/api/go.mod"
  "apps/web/package.json"
  "apps/web/pnpm-workspace.yaml"
  "apps/web/pnpm-lock.yaml"
  "apps/web/admin/package.json"
)

for p in "${required[@]}"; do
  if [[ ! -e "$p" ]]; then
    echo "check-docs: missing required path: $p"
    missing=1
  fi
done

# 契约 schema 节点示例必须写 _types.object/_types.array/_types.map。
if ! command -v grep >/dev/null 2>&1; then
  echo "check-docs: grep required"
  exit 1
fi
if hits=$(grep -rnE '(^|[[:space:]])type: (object|array|map)([[:space:]]|$)' docs/playbooks 2>/dev/null || true); [[ -n "$hits" ]]; then
  echo "check-docs: playbook uses bare schema kind (use _types.object/_types.array/_types.map):"
  echo "$hits"
  missing=1
fi

# 校验 markdown 中的相对链接（忽略 http/https/mailto/锚点）
while IFS= read -r md; do
  dir=$(dirname "$md")
  while IFS= read -r link; do
    case "$link" in
      http*|mailto:*|\#*|"") continue ;;
    esac
    target="${link%%#*}"
    [[ -z "$target" ]] && continue
    if [[ ! -e "$dir/$target" ]]; then
      echo "check-docs: broken link in $md -> $link"
      missing=1
    fi
  done < <(grep -oE '\]\([^)]+\)' "$md" | sed -E 's/^\]\(//; s/\)$//' || true)
done < <(find docs contracts -name '*.md' -type f)

if [[ $missing -ne 0 ]]; then
  exit 1
fi

echo "check-docs: ok"
