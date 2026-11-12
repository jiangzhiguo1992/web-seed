SHELL := /bin/bash
.DEFAULT_GOAL := help

APP_API := apps/api

.PHONY: help build build-api build-web test test-api test-tools test-web test-e2e lint lint-api lint-tools typecheck \
        gen gen-openapi gen-db gen-graph gen-client contract-new contract-check contract-graph contract-impact contract-compat db-verify \
        dev api-dev web-dev compose-up compose-down doctor derive check-docs

help: ## 显示可用目标
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

build: build-api build-web ## 构建全部

build-api: ## 构建 Go API
	cd $(APP_API) && go build ./...

build-web: ## 构建前端（按拓扑：tokens/ui 先于 app）
	pnpm -r build

test: test-api test-tools ## 运行测试（API + 契约工具）

test-api: ## Go 测试
	cd $(APP_API) && go test ./...

test-tools: ## 契约工具测试
	cd scripts/cmd && go test ./...

test-race: ## Go 竞态测试
	cd $(APP_API) && go test -race ./...

test-web: ## 前端测试（P1 未接入）
	@echo "web tests: 尚未配置（P7 接入 Vitest）"

lint: lint-api lint-tools typecheck ## 静态检查

lint-api: ## Go vet + 格式检查
	cd $(APP_API) && go vet ./... && test -z "$$(gofmt -l .)"

lint-tools: ## 契约工具 vet + 格式检查
	cd scripts/cmd && go vet ./... && test -z "$$(gofmt -l .)"

typecheck: ## 前端类型检查
	pnpm -r --if-present typecheck

gen: ## 重新生成全部契约产物（P2 实现）
	@echo "gen: 尚未实现（P2）。子命令：gen-openapi / gen-db / gen-graph / gen-client / gen-env"
	@exit 1

gen-openapi: ## 合并 capabilities/http/* + apps/* → generated/openapi（P2）
	@echo "gen-openapi: 尚未实现（P2）"
	@exit 1

gen-db: ## data/tables/* → schema.sql + migrations（P2）
	@echo "gen-db: 尚未实现（P2）"
	@exit 1

gen-graph: ## refs → generated/{manifest.json,graph.json,lineage.md}（P2）
	@echo "gen-graph: 尚未实现（P2）"
	@exit 1

gen-client: ## 合并 spec → apps/web/packages/api-client（P2）
	@echo "gen-client: 尚未实现（P2）"
	@exit 1

contract-new: ## 按 _meta 字段规范生成契约骨架（type=data.tables name=note module=notes [column_type=decimal]）
	@./scripts/contract/scaffold.sh "$(type)" "$(name)" "$(module)" "$(summary)" "$(column_type)"

contract-check: ## 校验契约结构与 used_by（试点：data.tables/data.enums）
	@./scripts/contract/validate.sh

contract-lint: ## 契约 lint：do_not 重复、缺字段、YAML 引号（P2）
	@echo "contract-lint: 尚未实现（P2）。检查项：do_not 与 _meta/_types.yaml 重复、缺具体标识、未引号的冒号值"
	@exit 1

contract-graph: ## 锚点图校验：重复/悬空/越层/循环/孤儿（P2）
	@echo "contract-graph: 尚未实现（P2）"
	@exit 1

contract-impact: ## 影响面分析：make contract-impact id=<id>（P2）
	@echo "contract-impact: 尚未实现（P2）"
	@exit 1

contract-compat: ## oasdiff 破坏性变更检测（P5）
	@echo "contract-compat: 尚未实现（P5）"
	@exit 1

db-verify: ## 空库重放迁移并与契约目标态比对（P5）
	@echo "db-verify: 尚未实现（P5）"
	@exit 1

test-e2e: ## Playwright 端到端（P4）
	@echo "test-e2e: 尚未实现（P4）"
	@exit 1

dev: ## 本地开发说明（P3 串接）
	@echo "分别运行: make compose-up && make api-dev && make web-dev"

api-dev: ## 启动 API
	cd $(APP_API) && go run ./cmd/adminapi

web-dev: ## 启动前端
	pnpm --filter @webseed/admin dev

compose-up: ## 启动本地依赖（PG）
	docker compose -f compose.yaml --profile core up -d

compose-down: ## 停止本地依赖
	docker compose -f compose.yaml --profile core down

doctor: ## 仓库健康检查
	./scripts/doctor.sh

derive: ## 从模板派生新项目（P7 实现）
	./scripts/derive.sh

check-docs: ## 检查文档关键路径
	./scripts/check-docs.sh
