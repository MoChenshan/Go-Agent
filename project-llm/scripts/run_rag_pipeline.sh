#!/usr/bin/env bash
# =============================================================================
# run_rag_pipeline.sh —— Agentic RAG 一键启动（本地开发）
#
# 串联：Qdrant → 构建索引 → 启动 rag_serve → 启动 mcp_expert → 自测
#
# 使用：
#   bash scripts/run_rag_pipeline.sh
#   SMOKE=1 bash scripts/run_rag_pipeline.sh   # 使用 mock KB 做 smoke
# =============================================================================
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

KB_DIR="${KB_DIR:-data/raw/kb}"
SMOKE="${SMOKE:-0}"

# ---- Step 0：准备 KB（smoke 模式自动构造示例） ----
if [[ "$SMOKE" == "1" ]] && [[ ! -d "$KB_DIR" ]]; then
    mkdir -p "$KB_DIR"
    cat > "$KB_DIR/cpu_alarm_sop.md" <<'EOF'
# CPU 告警排查 SOP

## 告警阈值
- warning: CPU > 70% 持续 5 分钟
- critical: CPU > 90% 持续 3 分钟

## 排查步骤
1. 登录监控面板，查看具体 Pod 负载
2. 执行 `kubectl top pod -n gameops` 确认 Top-N
3. 若 JVM 应用，先看 GC 日志；若原生服务，用 perf 采样 5s
4. 确认是否近期发版引起，可对比 release 时间轴

## 常见根因
- 流量突增（优先扩容）
- 死循环（回滚代码）
- GC 风暴（调整堆大小）
EOF
    cat > "$KB_DIR/bcs_scale.md" <<'EOF'
# BCS 扩容指南

扩容命令：kubectl scale deploy gameops-api -n gameops --replicas=N
副本建议：根据 QPS × 单实例容量 × 1.5 冗余
EOF
    echo "[smoke] 已生成示例 KB → $KB_DIR"
fi

# ---- Step 1：启动 Qdrant + vLLM + RAG 服务（docker） ----
echo ""
echo "=========================== [1/4] docker compose up ==========================="
if command -v docker >/dev/null 2>&1; then
    docker compose -f deploy/rag_docker-compose.yaml up -d qdrant || true
else
    echo "[warn] 未安装 docker，请手动启动 Qdrant (http://localhost:6333)"
fi

# ---- Step 2：构建索引 ----
echo ""
echo "=========================== [2/4] 构建索引 ==========================="
python scripts/build_index.py \
    --config configs/knowledge_rag.yaml \
    --source_dir "$KB_DIR" \
    --recreate || {
    echo "[warn] 索引构建失败，检查依赖 (FlagEmbedding / qdrant-client)"
}

# ---- Step 3：启动 rag_serve + mcp_expert（本地跑） ----
echo ""
echo "=========================== [3/4] 启动 RAG + MCP ==========================="
echo "[info] rag_serve  → http://localhost:8100"
echo "[info] mcp_expert → http://localhost:8200/mcp"
echo ""
echo "在两个终端分别运行："
echo "  uvicorn deploy.rag_serve:app --host 0.0.0.0 --port 8100"
echo "  python deploy/mcp_expert_server.py --host 0.0.0.0 --port 8200"
echo ""

# ---- Step 4：自测 ----
echo "=========================== [4/4] 自测 ==========================="
cat <<'SELF'
# 健康检查
curl http://localhost:8100/healthz

# RAG 直查
curl -X POST http://localhost:8100/rag/query \
  -H "Content-Type: application/json" \
  -d '{"query":"CPU 告警怎么排查？","top_k":5}'

# MCP 调用
curl -X POST http://localhost:8200/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"knowledge_expert_query","arguments":{"question":"CPU 告警怎么排查？"}}}'
SELF

echo ""
echo "========== ✅ 阶段 F 流水线就绪 =========="
echo "下一步：在 project-agent/conf/mcp_servers.yaml 注册 llm_knowledge_expert"
echo "详细：见 docs/agent_integration.md"
