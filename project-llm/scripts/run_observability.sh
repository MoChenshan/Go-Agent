#!/usr/bin/env bash
# =============================================================================
# run_observability.sh —— 一键启动 Langfuse + Prometheus + Grafana
# =============================================================================
set -uo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

echo "=========================== [1/3] 启动观测栈 ==========================="
docker compose -f observability/docker-compose.obs.yaml up -d

echo ""
echo "=========================== [2/3] 等待服务就绪 ==========================="
for i in 1 2 3 4 5 6; do
    if curl -sf http://localhost:9090/-/ready >/dev/null 2>&1 \
       && curl -sf http://localhost:3001/api/health >/dev/null 2>&1; then
        echo "✅ 就绪"
        break
    fi
    echo "  waiting... ($i/6)"; sleep 5
done

echo ""
echo "=========================== [3/3] 访问入口 ==========================="
cat <<'EOF'
  🧠 Langfuse    → http://localhost:3000   (首次注册即 admin)
  📊 Grafana     → http://localhost:3001   (admin / admin)
  📈 Prometheus  → http://localhost:9090

  第一次使用 Langfuse：
    1. 打开 http://localhost:3000 注册账号（自托管无邀请码）
    2. Settings → API Keys 创建 Public/Secret Key
    3. export LANGFUSE_PUBLIC_KEY=pk-lf-...
       export LANGFUSE_SECRET_KEY=sk-lf-...
    4. 重启 rag_serve，trace 就会自动上报
EOF
