#!/usr/bin/env bash
# smoke_agent.sh —— project-agent 最小 smoke 验证
#
# 1. 用 cmd/demo 拉起零依赖服务
# 2. 调一次 /demo/alarm
# 3. 拉一次审计
# 4. 退出

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

cd "$ROOT/project-agent"
echo "[smoke] building demo binary ..."
go build -o /tmp/gago-demo ./src/cmd/demo

echo "[smoke] starting demo on :8090 ..."
/tmp/gago-demo -addr ":8090" >/tmp/gago-demo.log 2>&1 &
PID=$!
trap "kill $PID 2>/dev/null || true" EXIT

# 等待启动
for i in {1..20}; do
  if curl -fsS http://localhost:8090/healthz >/dev/null 2>&1; then
    break
  fi
  sleep 0.3
done

echo "[smoke] /healthz:"
curl -fsS http://localhost:8090/healthz | jq . || curl -fsS http://localhost:8090/healthz

echo
echo "[smoke] POST /demo/alarm x5 ..."
for i in 1 2 3 4 5; do
  curl -fsS -X POST http://localhost:8090/demo/alarm \
    -H 'Content-Type: application/json' \
    -d "{\"alert\":\"oom-$i\",\"pod\":\"game-$i\"}" | jq -c .
done

echo
echo "[smoke] GET /demo/audit/last:"
curl -fsS http://localhost:8090/demo/audit/last | jq .

echo
echo "✅ agent smoke passed"
