# Changelog

本项目遵循 [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) 规范，
版本号遵循 [Semantic Versioning](https://semver.org/lang/zh-CN/)。

## [Unreleased]

### Added
- 顶层 `Dockerfile` + `Makefile` + `docker-compose.yml`，支持一键起栈
- `pkg/resilience/`：通用 retry / breaker / bulkhead / ratelimit 公共包
- `src/idempotency/`：基于 Redis 的幂等键，防 Webhook 重复触发
- `src/session/redis_session.go`：会话 Redis 持久化（生产场景必需）
- `main.go` 增加 graceful shutdown：信号 → http.Shutdown → Runner.Stop → audit/otel flush
- `src/integration/chaos_test.go`：MCP 超时 / 熔断 / 限流故障注入测试
- `src/audit/hmac_bench_test.go`：审计链 HMAC 性能基准
- A2A 多 Agent 端到端测试：Coordinator + 2 sub-agent 协作
- `deploy/helm/`：完整 K8s Helm Chart（Deployment + HPA + PDB + NetworkPolicy + ServiceMonitor）
- `api/openapi.yaml`：对外 contract 标准化
- `ARCHITECTURE.md`：架构图与决策日志
- `SECURITY.md`：漏洞报送与加固清单
- `.golangci.yml` + GitHub Actions CI

### Changed
- 启动横幅展示版本号 + commit + build time
- OTel Collector 默认开启 PII 属性脱敏

## [1.8.1] - 2026-04-30

### Added
- D16 阶段：完整 OTel GenAI Semantic Convention v1.30 接入
- LLM-as-Judge 评测体系（`eval/judge*.go`）

## [1.7.0] - 2026-04-15

### Added
- D15：Webhook 入口 + 报告自动生成（`/webhook/bk_alarm` + `/webhook/tapd`）

## [1.5.0] - 2026-03-30

### Added
- D11：AG-UI Web 前端，AGUI 与 SSE 共享 session
- D8：A2A v0.2 协议，build tag 支持 stub/real

## [1.0.0] - 2026-02-01

### Added
- 初始版本：Coordinator + Diagnosis + Repair + Knowledge + FileAnalyst 5 Agent 框架
