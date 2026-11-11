SHELL := /bin/bash
.DEFAULT_GOAL := help

APP_API := apps/api

.PHONY: help build build-api build-web test test-api test-web lint lint-api typecheck \
        gen dev api-dev web-dev compose-up compose-down doctor derive check-docs

help: ## 显示可用目标
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

build: build-api build-web ## 构建全部

build-api: ## 构建 Go API
	cd $(APP_API) && go build ./...

build-web: ## 构建前端（按拓扑：tokens/ui 先于 app）
	pnpm -r build

test: test-api ## 运行测试（P1 仅 Go）

test-api: ## Go 测试
	cd $(APP_API) && go test ./...

test-web: ## 前端测试（P1 未接入）
	@echo "web tests: 尚未配置（P7 接入 Vitest）"

lint: lint-api typecheck ## 静态检查

lint-api: ## Go vet + 格式检查
	cd $(APP_API) && go vet ./... && test -z "$$(gofmt -l .)"

typecheck: ## 前端类型检查
	pnpm -r --if-present typecheck

gen: ## 重新生成契约产物（P2 实现）
	@echo "gen: 尚未实现（P2）。计划：openapi / sqlc / error-codes / tokens / env"
	@exit 1

dev: ## 本地开发说明（P3 串接）
	@echo "分别运行: make compose-up && make api-dev && make web-dev"

api-dev: ## 启动 API
	cd $(APP_API) && go run ./cmd/userapi

web-dev: ## 启动前端
	pnpm --filter @webseed/user dev

compose-up: ## 启动本地依赖（PG）
	docker compose -f infra/compose.yaml --profile core up -d

compose-down: ## 停止本地依赖
	docker compose -f infra/compose.yaml --profile core down

doctor: ## 仓库健康检查
	./scripts/doctor.sh

derive: ## 从模板派生新项目（P7 实现）
	./scripts/derive.sh

check-docs: ## 检查文档关键路径
	./scripts/check-docs.sh
