# gameops-agent Helm Chart

## 安装

```bash
# 1. 创建 namespace
kubectl create namespace gameops

# 2. 准备 secrets（推荐用 ExternalSecrets / Vault，不要明文）
helm install gameops-agent ./deploy/helm \
  -n gameops \
  --set image.tag=1.8.1 \
  --set secrets.openaiApiKey="$OPENAI_API_KEY" \
  --set secrets.auditHmacKey="$AUDIT_HMAC_KEY"

# 3. 验证
kubectl -n gameops rollout status deploy/gameops-agent
kubectl -n gameops port-forward svc/gameops-agent 8080:8080
curl http://localhost:8080/healthz
```

## 关键配置

| 字段 | 默认 | 含义 |
|---|---|---|
| `replicaCount` | 3 | 副本数（HPA 启用时仅作为初值） |
| `autoscaling.minReplicas` | 3 | HPA 最小副本 |
| `autoscaling.maxReplicas` | 20 | HPA 最大副本 |
| `podDisruptionBudget.minAvailable` | 2 | 主动驱逐时保留副本数 |
| `terminationGracePeriodSeconds` | 60 | 优雅关闭窗口 |
| `networkPolicy.enabled` | true | 启用 NetworkPolicy 限制流量 |
| `serviceMonitor.enabled` | true | 启用 prometheus-operator ServiceMonitor |

## 升级 / 回滚

```bash
# 升级
helm upgrade gameops-agent ./deploy/helm -n gameops --set image.tag=1.9.0

# 回滚到上一版本
helm rollback gameops-agent 0 -n gameops

# 历史
helm history gameops-agent -n gameops
```

## 故障预案

1. **滚动升级期间 HITL 中断风险**：
   - `terminationGracePeriodSeconds: 60` + `preStop sleep 5`
   - 长 HITL 由 Redis 持久化，新副本接管时可继续审批
2. **整集群被 LLM 限速**：
   - 启动 `autoscaling.customMetrics`，按 `gameops_session_inflight` 缩放
3. **Redis 故障**：
   - Session/Idempotency 自动降级为内存（生产建议 Redis Sentinel/Cluster）
