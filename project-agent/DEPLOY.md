# GameOps Agent — 部署运行手册（DEPLOY.md）

> 本文档把「司内 / 司外、Linux / Windows、开发 / 生产」四个维度的部署流程一次讲清。
> 配套清单：[TODO.md](../TODO.md)、入口：[main.go](main.go)、镜像：[Dockerfile](Dockerfile)、本地全栈：[docker-compose.yml](docker-compose.yml)、K8s：[deploy/helm](deploy/helm)。

---

## 1. 总览

| 场景 | OS 选型 | 推荐方式 | 入口 |
|---|---|---|---|
| 开发联调 | Win / Linux 都行 | `make run` 或 `go run . -cli` | [main.go](main.go) |
| 本地全栈演示 | Linux 优先；Win 需 Docker Desktop / WSL2 | `make up`（docker compose） | [docker-compose.yml](docker-compose.yml) |
| 司内生产 | **Linux K8s** | Helm | [deploy/helm](deploy/helm) |
| 司外演示 | Linux + Docker Compose | LLM 接 DeepSeek/OpenAI，三方平台全 Mock | [.env.example](.env.example) |

---

## 2. 前置依赖

| 依赖 | 版本 | 备注 |
|---|---|---|
| Go | ≥ 1.21（生产构建用 1.24） | Dockerfile 用 `golang:1.24-alpine` |
| Docker | ≥ 20.10（含 BuildKit） | Linux 推荐，Win 用 Docker Desktop |
| Docker Compose | v2 | `docker compose ...`（无 dash） |
| Make | GNU Make | Win：`choco install make` 或走 WSL2 |
| K8s | ≥ 1.24 | 生产部署，配 Helm v3 |
| `golangci-lint` | ≥ v1.61（可选） | `make lint` 用 |

---

## 3. 内 / 外网差异速查

| 项 | 司内 | 司外 |
|---|---|---|
| Go 模块代理 | `GOPROXY=https://goproxy.woa.com,direct`<br>`GOPRIVATE=git.woa.com,trpc.group` | `GOPROXY=https://goproxy.cn,direct`；私有依赖用 `make build-stub` 或 vendor |
| LLM 后端 | 混元 `http://hunyuanapi.woa.com/openapi/v1` | DeepSeek `https://api.deepseek.com/v1` 或 OpenAI |
| 蓝鲸 / BCS / 工蜂 / 蓝盾 / TAPD / iWiki | 配真实 token | **必须**全开 `*_API_MOCK=1` |
| Docker 基础镜像 | `mirrors.tencent.com/library/...` | docker.io 直连或自建镜像 |
| 可观测性后端 | 内部 OTel Collector / Langfuse | 自部署 Langfuse 或公有云 |

**Mock 模式说明**：未配置凭据 / 显式 `*_API_MOCK=1` 时，对应平台工具会返回预置样例数据，Agent 业务流程仍可完整跑通，是司外演示和 CI 的标准玩法。

---

## 4. 开发联调（Win / Linux 通用）

### 4.1 Linux / macOS（bash）

```bash
# 1) Go 代理
go env -w GOPROXY=https://goproxy.woa.com,direct          # 司内
go env -w GOPRIVATE=git.woa.com,trpc.group
# 司外：go env -w GOPROXY=https://goproxy.cn,direct

# 2) 模型 API
export OPENAI_API_KEY=<your-key>
export OPENAI_BASE_URL=http://hunyuanapi.woa.com/openapi/v1   # 司内
# 司外：export OPENAI_BASE_URL=https://api.deepseek.com/v1

# 3) 司外必开三方 Mock
export BK_API_MOCK=1 BCS_API_MOCK=1 \
       GONGFENG_API_MOCK=1 DEVOPS_API_MOCK=1 TAPD_API_MOCK=1

# 4) 拉依赖 + 跑
cd project-agent
go mod tidy
make run            # 等价 go run . -debug，HTTP :8080
# 或 CLI：
make run-cli
```

### 4.2 Windows（PowerShell）

```powershell
go env -w GOPROXY=https://goproxy.woa.com,direct
go env -w GOPRIVATE=git.woa.com,trpc.group

$env:OPENAI_API_KEY  = "<your-key>"
$env:OPENAI_BASE_URL = "http://hunyuanapi.woa.com/openapi/v1"
$env:BK_API_MOCK="1"; $env:BCS_API_MOCK="1"
$env:GONGFENG_API_MOCK="1"; $env:DEVOPS_API_MOCK="1"; $env:TAPD_API_MOCK="1"

cd project-agent
go mod tidy
go run . -debug
```

### 4.3 验证

```bash
curl http://localhost:8080/healthz
# ok

curl -N -X POST http://localhost:8080/v1/agent \
  -H 'Content-Type: application/json' \
  -d '{"user_id":"dev","session_id":"s1","query":"你好"}'
# 收到 SSE 流式事件
```

也可直接 `make run-cli` 在终端里和 Agent 对话。

---

## 5. 本地全栈（docker compose）

### 5.1 准备 .env

```bash
cd project-agent
cp .env.example .env
# Windows: copy .env.example .env
```

按 [.env.example](.env.example) 中的注释填入实际值。**强烈建议**：

```bash
# 用随机串覆盖审计 HMAC key（默认值带有 dev-only 字样）
sed -i "s/^AUDIT_HMAC_KEY=.*/AUDIT_HMAC_KEY=$(openssl rand -hex 32)/" .env
```

### 5.2 起 / 停

```bash
make up        # docker compose up -d --build
make logs      # 跟随 agent 日志
make smoke     # 端到端冒烟测试（healthz + SSE）
make down      # 停
```

### 5.3 访问入口

| 地址 | 用途 |
|---|---|
| http://localhost:8080/healthz | 健康检查 |
| http://localhost:8080/v1/agent | Agent SSE |
| http://localhost:8080/agui | Web 前端（仅 `-tags agui`） |
| http://localhost:16686 | Jaeger UI |
| http://localhost:3001 | Langfuse |
| http://localhost:9090 | Prometheus |
| http://localhost:3000 | Grafana（admin/admin） |

### 5.4 Windows Docker Desktop 注意事项

- Settings → Resources → File Sharing 把仓库目录加白，否则 `./deploy/...` 卷挂载会失败
- 至少分配 8 GB 内存给 Docker（agent + redis + pg + jaeger + langfuse + prom + grafana 一起跑）
- 如需用本机 Ollama / vLLM，`OPENAI_BASE_URL=http://host.docker.internal:8000/v1`

---

## 6. K8s 生产部署（Helm）

### 6.1 构建并推送镜像

```bash
cd project-agent
make docker IMAGE=mirrors.tencent.com/gameops/agent TAG=v1.0.0
docker push mirrors.tencent.com/gameops/agent:v1.0.0
```

### 6.2 安装

```bash
helm upgrade --install gameops-agent ./deploy/helm \
  --namespace gameops --create-namespace \
  --set image.repository=mirrors.tencent.com/gameops/agent \
  --set image.tag=v1.0.0 \
  --set secrets.openaiApiKey=<...> \
  --set secrets.auditHmacKey=<...> \
  --set config.session.redisAddr=gameops-redis-master.gameops-deps:6379 \
  --set config.otel.endpoint=http://otel-collector.gameops-deps:4318
```

### 6.3 默认开启的能力（见 [values.yaml](deploy/helm/values.yaml)）

- 副本：3，HPA 3~20，CPU 70% / Memory 80%
- PDB：minAvailable=2（保证 HITL 不中断）
- 反亲和：避免单节点全挂
- NetworkPolicy：仅放行 ingress-nginx + monitoring + 内网 + LLM 域名
- ServiceMonitor：Prometheus Operator 自动采集 `/metrics`
- terminationGracePeriodSeconds=60：给 graceful shutdown 留够时间

### 6.4 推荐用 ExternalSecrets / Vault 注入凭据

```yaml
# values.yaml override
secrets:
  openaiApiKey: ""        # 留空，由 ExternalSecrets 注入到同名 Secret
  auditHmacKey: ""
```

---

## 7. 部署前自检（preflight）

```bash
go run ./src/cmd/preflight
```

输出每个平台的状态：

```
[bk-monitor]   REAL    BK_APP_CODE=xxx, BK_APP_SECRET=*** (8 chars)
[bcs]          MOCK    BCS_TOKEN unset
[gongfeng]     REAL    GONGFENG_TOKEN=*** (40 chars)
[devops]       DISABLED DEVOPS_ALLOW_AUTO_OPS=0
[tapd]         REAL    TAPD_USER=xxx
[iwiki]        STUB    IWIKI_PAAS_ID unset
[knowledge]    REAL    OPENAI_API_KEY=*** (51 chars)
[hitl]         ENABLED
[audit]        sink=file path=/app/audit.log hmac=on
```

---

## 8. 联动 project-llm（可选）

如果要把 [project-llm](../project-llm) 训练出的模型作为 LLM 后端：

```bash
# Linux GPU 机上
cd ../project-llm
make up                                   # 起 vLLM :8000

# 同机 / 另一台
cd ../project-agent
export OPENAI_BASE_URL=http://<vllm-host>:8000/v1
export OPENAI_API_KEY=any                 # vLLM 不校验
export MODEL_NAME=knowledge-expert        # 与 vllm --served-model-name 对齐
make up
```

---

## 9. 故障排查

| 现象 | 可能原因 | 处理 |
|---|---|---|
| `go mod download` 卡住 | GOPROXY 未配 / 私有依赖拉不到 | 司内配 `goproxy.woa.com`；司外用 `make build-stub` |
| `make up` 后 agent 反复重启 | `OPENAI_API_KEY` 没填 / Base URL 不可达 | 看 `make logs`，先用 stub key 排除 |
| HITL 卡住 | 前端没把 `confirmation_required` 渲染成确认按钮 | 见 [README.md](README.md) §SSE 协议 |
| 写工具不真正下发 | `GONGFENG_ALLOW_AUTO_MERGE` / `DEVOPS_ALLOW_AUTO_OPS` 默认关 | 按治理流程显式开 |
| Prometheus 抓不到 `/metrics` | `OTEL_ENABLED=true` 必开；K8s 检查 ServiceMonitor 标签 | `helm get values` 核对 |

---

## 10. 安全红线（生产必读）

- ❌ **永远**不要设 `HITL_DISABLE=1`
- ❌ `AUDIT_HMAC_KEY` **不得**用 dev 默认值
- ❌ 凭据**不得**进 git；走 K8s Secret / Vault / ExternalSecrets
- ✅ `GONGFENG_ALLOW_AUTO_MERGE` / `DEVOPS_ALLOW_AUTO_OPS` 默认 `0`，按治理流程开
- ✅ NetworkPolicy 默认开启，Egress 收敛到必需域名
