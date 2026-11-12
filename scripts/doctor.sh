#!/usr/bin/env bash
# 仓库健康检查：编译、静态检查、格式、文档、生成物。
set -euo pipefail
cd "$(dirname "$0")/.."

echo "== go build =="
(cd apps/api && go build ./...)

echo "== go vet =="
(cd apps/api && go vet ./...)

echo "== gofmt =="
unformatted=$(cd apps/api && gofmt -l .)
if [[ -n "$unformatted" ]]; then
  echo "gofmt required:"
  echo "$unformatted"
  exit 1
fi

echo "== docs =="
./scripts/check-docs.sh

echo "== contract tools =="
(cd scripts/cmd && go build ./... && go vet ./...)
unformatted=$(cd scripts/cmd && gofmt -l .)
if [[ -n "$unformatted" ]]; then
  echo "contract tools gofmt required:"
  echo "$unformatted"
  exit 1
fi

echo "== contracts =="
./scripts/contract/validate.sh

echo "== design tokens =="
if [[ ! -f apps/web/packages/design-tokens/theme.css ]]; then
  echo "theme.css missing; run: pnpm --filter @webseed/design-tokens build"
  exit 1
fi

echo "doctor: ok"
