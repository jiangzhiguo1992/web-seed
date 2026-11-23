#!/usr/bin/env bash
# 契约兼容性：对比 origin/main 的生成 OpenAPI 与当前生成物，并检查 v2 不破坏 v1。
set -euo pipefail
cd "$(dirname "$0")/../.."

ref="${CONTRACT_BASELINE_REF:-}"
version="${OASDIFF_VERSION:-v1.39.0}"
run() {
  if command -v oasdiff >/dev/null 2>&1; then
    oasdiff "$@"
  else
    go run "github.com/oasdiff/oasdiff@${version}" "$@"
  fi
}

status=0
if [[ -n "$ref" ]]; then
  for spec in contracts/generated/openapi/*.yaml; do
    rel="${spec#./}"
    if git cat-file -e "${ref}:${rel}" 2>/dev/null; then
      echo "== ${rel} vs ${ref} =="
      run breaking "${ref}:${rel}" "${rel}" --fail-on ERR || status=1
    else
      echo "== ${rel}: new spec vs ${ref} =="
    fi
  done
else
  echo "== baseline skipped (set CONTRACT_BASELINE_REF=origin/main to compare) =="
fi

v1="contracts/generated/openapi/admin.yaml"
v2="contracts/generated/openapi/admin_v2.yaml"
if [[ -f "$v1" && -f "$v2" ]]; then
  echo "== v2 compatibility: ${v2} vs ${v1} =="
  run breaking "$v1" "$v2" --fail-on ERR || status=1
fi
exit "$status"
