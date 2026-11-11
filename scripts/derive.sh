#!/usr/bin/env bash
# P7 实现：从模板派生新项目。
set -euo pipefail
cd "$(dirname "$0")/.."

cat <<'EOF'
derive: 尚未实现（P7）。

计划步骤：
  1. 询问项目名、Go module、包 scope、端口、是否生成 admin
  2. 替换 github.com/yourorg/web-seed、@webseed/*
  3. 删除示例模块与示例页面
  4. 重置 git 历史并生成首个 commit
  5. 写入 docs/DERIVED.md（记录来源模板版本）
EOF
exit 1
