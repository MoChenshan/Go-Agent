# ============================================================
# GameOps Agent — Makefile
# 统一封装本地开发 / 测试 / 构建 / 部署常用命令
# ============================================================

SHELL := /bin/bash
.DEFAULT_GOAL := help

VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT    ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
IMAGE     ?= gameops-agent
TAG       ?= $(VERSION)

GOFLAGS    := -trimpath
LDFLAGS    := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildTime=$(BUILD_TIME)
BUILD_TAGS ?= a2a agui iwiki redis

# 所有功能均已使用公开库实现，无需区分内网/外网构建。
# iwiki 和 redis 仍需对应的运行时环境（iWiki API 网络可达、Redis 实例），
# 但编译不再依赖任何内网包。

##@ Help

.PHONY: help
help: ## 列出所有可用目标
	@awk 'BEGIN {FS = ":.*##"; printf "Usage: make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)

##@ Develop

.PHONY: tidy
tidy: ## 整理依赖
	go mod tidy

.PHONY: fmt
fmt: ## 格式化代码
	gofmt -s -w .
	@command -v goimports >/dev/null && goimports -w . || true

.PHONY: lint
lint: ## 静态检查（需安装 golangci-lint v1.61+）
	golangci-lint run ./... --timeout=5m

.PHONY: vet
vet: ## go vet
	go vet ./...

##@ Test

.PHONY: test
test: ## 单元测试（不含 integration tag）
	go test -race -count=1 -coverprofile=coverage.out ./src/...

.PHONY: test-integration
test-integration: ## 集成测试
	go test -race -count=1 -tags=integration ./src/integration/...

.PHONY: test-chaos
test-chaos: ## 故障注入测试
	go test -race -count=1 -tags=chaos -run Chaos ./src/integration/...

.PHONY: bench
bench: ## 性能基准测试
	go test -bench=. -benchmem -run=^$$ ./src/audit/... ./src/async/...

.PHONY: coverage
coverage: test ## 生成 HTML 覆盖率报告
	go tool cover -html=coverage.out -o coverage.html
	@echo "✓ coverage.html generated"

##@ Build

.PHONY: build
build: ## 编译本地二进制（所有功能均使用公开库，无需内网访问）
	@echo "[build] 编译中（tags: $(BUILD_TAGS)）"
	CGO_ENABLED=0 go build $(GOFLAGS) -tags "$(BUILD_TAGS)" -ldflags "$(LDFLAGS)" -o bin/gameops-agent .

.PHONY: build-minimal
build-minimal: ## 编译最小模式（不带任何可选 tag）
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/gameops-agent-minimal .

.PHONY: docker
docker: ## 构建 Docker 镜像
	docker build \
	  --build-arg VERSION=$(VERSION) \
	  --build-arg COMMIT=$(COMMIT) \
	  --build-arg BUILD_TIME=$(BUILD_TIME) \
	  -t $(IMAGE):$(TAG) -t $(IMAGE):latest .

##@ Run

.PHONY: run
run: ## 本地运行（默认 :8080）
	go run . -debug

.PHONY: run-cli
run-cli: ## CLI 交互模式
	go run . -cli -debug

.PHONY: up
up: ## docker compose 启动全栈
	docker compose -f docker-compose.yml up -d --build

.PHONY: down
down: ## docker compose 停止
	docker compose -f docker-compose.yml down

.PHONY: logs
logs: ## 查看 agent 日志
	docker compose -f docker-compose.yml logs -f agent

##@ Smoke

.PHONY: smoke
smoke: ## 端到端冒烟测试（需要 up 已启动）
	@echo "→ /healthz"
	@curl -sf http://localhost:8080/healthz && echo " ✓"
	@echo "→ /v1/agent SSE"
	@curl -sN -X POST http://localhost:8080/v1/agent \
	  -H 'Content-Type: application/json' \
	  -d '{"user_id":"smoke","session_id":"s1","query":"hello"}' | head -n 5

##@ Eval

.PHONY: eval
eval: ## 运行 ADK 评测
	go run ./eval/cmd/evalrun -evalset eval/data/gameops-core/gameops-core.evalset.json

##@ Clean

.PHONY: clean
clean: ## 清理构建产物
	rm -rf bin/ coverage.out coverage.html
