#!/usr/bin/env bash
# 契约兼容性：enum append-only（枚举名/value/go_type/ts_type/code；默认 HEAD^1）；配置 CONTRACT_BASELINE_REF 时对比生成 OpenAPI；存在 v2 时检查 v2 不破坏 v1。
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
staged="${CONTRACT_COMPAT_STAGED:-0}"
if [[ "$staged" == "1" || "$staged" == "true" ]]; then
  # pre-commit：HEAD 是当前提交，待提交内容在 index；不能读工作区，否则未暂存改动会污染结果。
  if git rev-parse --verify -q HEAD >/dev/null 2>&1; then
    echo "== enum append-only (staged) vs HEAD =="
    (cd scripts/cmd && go run ./contract_compat -root ../.. -ref HEAD -staged) || status=1
  else
    echo "== enum append-only skipped (no HEAD) =="
  fi
  exit "$status"
fi

# 枚举 append-only 默认对比 HEAD^1：普通提交是父提交；PR merge commit 的第一父提交是 base；多父提交取第一父。
# 非 merge 的 CI（直接 checkout PR head）没有 HEAD^2，优先用 GITHUB_BASE_REF 的远端分支做基线。
# 没有 HEAD^1（初始提交或 depth=1 浅克隆）时明确跳过；CI 使用 fetch-depth: 0。
enum_ref="${CONTRACT_BASELINE_REF:-}"
if [[ -z "$enum_ref" ]]; then
  if git rev-parse --verify -q HEAD^2 >/dev/null 2>&1; then
    # merge commit：第一父是 base。
    enum_ref=HEAD^1
  elif [[ "${GITHUB_EVENT_NAME:-}" == pull_request* && -n "${GITHUB_BASE_REF:-}" ]]; then
    # 非 merge 的 CI（直接 checkout PR head）：必须用远端 base；拿不到就不退回 HEAD^1。
    if git rev-parse --verify -q "origin/${GITHUB_BASE_REF}" >/dev/null 2>&1; then
      enum_ref="origin/${GITHUB_BASE_REF}"
    fi
  elif git rev-parse --verify -q HEAD^1 >/dev/null 2>&1; then
    enum_ref=HEAD^1
  fi
fi
if [[ -n "$enum_ref" ]]; then
  echo "== enum append-only vs ${enum_ref} =="
  (cd scripts/cmd && go run ./contract_compat -root ../.. -ref "$enum_ref") || status=1
elif [[ "${GITHUB_EVENT_NAME:-}" == pull_request* && -n "${GITHUB_BASE_REF:-}" ]]; then
  # PR head 直接 checkout 且远端 base 不可用：不能退回 HEAD^1（只覆盖最后一个提交）。
  echo "== enum append-only: no PR base; set CONTRACT_BASELINE_REF ==" >&2
  status=1
else
  echo "== enum append-only skipped (no HEAD^1; shallow checkout?) =="
fi
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
