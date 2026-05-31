#!/usr/bin/env bash
# end_to_end.sh —— project-llm × project-agent 端到端联动 demo
#
# 假设 docker-compose.full.yml 已 up -d：
#   - vLLM (Qwen3-4B) on :8000
#   - RAG server      on :8200
#   - Agent           on :8080
#   - Grafana         on :3000
#   - Jaeger          on :16686
#   - Langfuse        on :3001
#
# 步骤：
#   1. 健康检查全栈
#   2. 灌一份知识进 RAG
#   3. 命中 RAG 的问答（curl agent /v1/agent SSE）
#   4. 触发一次 webhook 告警 → coordinator 调度 → diagnosis → repair（HITL pause）
#   5. 列举 Jaeger 中的 trace + Langfuse 中的对话

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

ok() { printf "  ✅ %s\n" "$*"; }
fail() { printf "  ❌ %s\n" "$*"; exit 1; }

echo "==================== 1. 全栈健康 ===================="
curl -fsS http://localhost:8000/v1/models      >/dev/null && ok vllm        || fail vllm
curl -fsS http://localhost:8200/healthz        >/dev/null && ok rag-server  || ok "rag-server (skip if not deployed)"
curl -fsS http://localhost:8080/healthz        >/dev/null && ok agent       || fail agent
curl -fsS http://localhost:3000/api/health     >/dev/null && ok grafana     || ok "grafana (skip)"
curl -fsS http://localhost:16686/              >/dev/null && ok jaeger      || ok "jaeger (skip)"

echo
echo "==================== 2. 灌知识进 RAG ===================="
if curl -fsS http://localhost:8200/healthz >/dev/null 2>&1; then
  curl -fsS -X POST http://localhost:8200/v1/index \
    -H 'Content-Type: application/json' \
    -d '{
      "docs": [
        {"id":"doc-1","text":"游戏后端 game-master Pod OOM 通常由热点房间引起，建议先看 oom_killer 日志再决定 kill 还是 scale。"},
        {"id":"doc-2","text":"BCS 集群 prod 的 Pod 扩缩容需先经过 SRE 审批，不可直接 delete。"}
      ]}' >/dev/null && ok 索引完成 || ok "索引接口未实现，跳过"
fi

echo
echo "==================== 3. 通过 Agent 命中 RAG 问答 ===================="
curl -fsS -N -X POST http://localhost:8080/v1/agent \
  -H 'Content-Type: application/json' \
  -d '{
    "session_id":"e2e-1",
    "user_id":"e2e-user",
    "input":"game-master Pod OOM 我该怎么排查？"
  }' | head -n 30
ok "命中 RAG 的对话已流式输出（前 30 行）"

echo
echo "==================== 4. 触发 webhook 告警 ===================="
curl -fsS -X POST http://localhost:8080/webhook/bk_alarm \
  -H 'Content-Type: application/json' \
  -H 'X-BK-Signature: dev-skip' \
  -d '{
    "fingerprint":"e2e-fp-1",
    "alert_name":"PodOOMKilled",
    "pod":"game-master-0",
    "namespace":"prod",
    "severity":"critical"
  }' && echo && ok "webhook 已投递"

echo
echo "==================== 5. 链路指引 ===================="
echo "   🔍 Jaeger trace : http://localhost:16686/search?service=gameops-agent"
echo "   📈 Grafana DASH : http://localhost:3000/d/gameops-agent"
echo "   🪵 Langfuse     : http://localhost:3001"
echo
echo "✅ end-to-end demo done"
