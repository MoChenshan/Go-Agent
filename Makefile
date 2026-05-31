# Go-Agent —— 顶层 Makefile
#
# 一站式管理 project-llm + project-agent 两侧的常用流程。

.PHONY: help all demo demo-agent demo-llm \
        build-llm build-agent \
        up down logs status \
        smoke-llm smoke-agent smoke \
        test-llm test-agent test \
        lint clean

help:
	@echo "Go-Agent —— monorepo make targets"
	@echo ""
	@echo "构建 / 准备："
	@echo "  make build-agent       构建 project-agent 二进制 (go build)"
	@echo "  make build-llm         构建 project-llm 推理镜像 (docker build)"
	@echo ""
	@echo "运行（全栈）："
	@echo "  make up                docker compose -f docker-compose.full.yml up -d"
	@echo "  make down              docker compose down"
	@echo "  make logs              tail 全栈日志"
	@echo "  make status            ps + 端口列表"
	@echo ""
	@echo "演示（最小依赖）："
	@echo "  make demo              拉起两侧 demo（不需要凭据）"
	@echo "  make demo-agent        只跑 agent 侧 demo（pkg/resilience 真实链路）"
	@echo "  make demo-llm          只跑 llm 侧 demo notebook"
	@echo ""
	@echo "测试 / 冒烟："
	@echo "  make test              project-agent 全套 go test + project-llm pytest"
	@echo "  make smoke             启动 + 一条 webhook 调用 + 健康检查"

all: build-agent build-llm

# -------------------- 构建 --------------------
build-agent:
	cd project-agent && go build -o bin/agent .
	cd project-agent && go build -o bin/demo ./src/cmd/demo

build-llm:
	cd project-llm && docker build -f Dockerfile.infer -t go-agent/llm-infer:dev .

# -------------------- 全栈 --------------------
up:
	docker compose -f docker-compose.full.yml up -d
	@echo ""
	@echo "🌐 服务已就绪："
	@echo "   Agent      : http://localhost:8080"
	@echo "   Demo Agent : http://localhost:8090"
	@echo "   vLLM       : http://localhost:8000/v1"
	@echo "   RAG Server : http://localhost:8200"
	@echo "   Grafana    : http://localhost:3000"
	@echo "   Jaeger     : http://localhost:16686"
	@echo "   Langfuse   : http://localhost:3001"

down:
	docker compose -f docker-compose.full.yml down -v

logs:
	docker compose -f docker-compose.full.yml logs -f --tail=100

status:
	docker compose -f docker-compose.full.yml ps

# -------------------- 演示 --------------------
demo: demo-agent

demo-agent:
	cd project-agent && go run ./src/cmd/demo

demo-llm:
	cd project-llm && jupyter nbconvert --to notebook --execute demo/demo_notebook.ipynb \
		--output demo_executed.ipynb --ExecutePreprocessor.timeout=300

# -------------------- 端到端 demo（无 GPU）--------------------
smoke-agent:
	@bash scripts/smoke_agent.sh

smoke-llm:
	@bash scripts/smoke_llm.sh

smoke: smoke-agent smoke-llm

# -------------------- 测试 --------------------
test-agent:
	cd project-agent && go test ./... -count=1 -timeout=180s

test-llm:
	cd project-llm && pytest -q

test: test-agent test-llm

lint:
	cd project-agent && golangci-lint run --timeout=5m
	cd project-llm   && ruff check .

clean:
	rm -rf project-agent/bin
	rm -rf project-llm/output project-llm/.venv
