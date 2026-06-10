# 安全策略

## 报送漏洞

如果你发现了本项目的安全问题，请**不要直接提 Issue**，按以下流程报送：

1. 发送邮件到：`security@example.com`（请替换为真实联系邮箱）
2. 邮件标题：`[SECURITY] gameops-agent: <一句话描述>`
3. 邮件正文请包含：
   - 漏洞类型（SSRF / Prompt Injection / 鉴权绕过 / RCE / ...）
   - 复现步骤（最小化）
   - 影响范围（哪些版本、哪些场景）
   - 建议修复（可选）

我们承诺：
- **48 小时**内确认收到
- **5 个工作日**内给出初步评估
- **30 天**内修复（高危优先）

## 安全设计原则

本项目内置以下安全机制（实现位于 `src/plugin/`、`src/audit/`）：

| 风险 | 控制措施 | 代码位置 |
|---|---|---|
| Prompt 注入 | InputGuard 关键词/规则匹配，可热加载 | `src/plugin/input_guard.go` |
| 越权工具调用 | 工具白名单 + 集群级 RBAC | `mcp_servers.yaml` + `src/plugin/safety_guard.go` |
| 高危操作误执行 | HITL（Human-in-the-Loop）强制审批 | `src/agents/repair_agent/` |
| 敏感数据泄露 | OutputGuard PII 正则 + OTel 属性脱敏 | `src/plugin/output_guard.go` + `deploy/otel/collector.yaml` |
| 审计抵赖 | HMAC 链式审计（前哈希链 + 远端 sink） | `src/audit/hmac.go` + `src/audit/remote_sink.go` |
| 重放攻击 | Webhook 签名校验 + 幂等键 | `src/services/webhook/webhook.go` + `src/idempotency/` |
| SSRF | 工具层 URL 白名单 + 内网段拒绝 | `src/infrastructure/*/client.go` |

## 已知限制

- **不运行用户提交的任意代码**：`skills/*/scripts/` 都是只读脚本，不接受用户上传
- **不直接执行 SQL/Shell**：所有外部交互通过受控的 MCP/HTTP API 客户端
- **HITL 不可绕过**：高危工具（pod_restart / scale / configmap_update / secret_update / network_update）写死了 `requires_approval = true`

## 加固清单（生产部署必看）

- [ ] 替换 `AUDIT_HMAC_KEY` 为 KMS 托管的 32 字节随机密钥（不可使用 docker-compose 默认值）
- [ ] 启用 mTLS 调用 BCS / DevOps API
- [ ] OTel Collector 启用 attribute redaction（已默认开启 `gen_ai.prompt` / `gen_ai.completion` 哈希）
- [ ] Webhook 端点配置 IP 白名单（K8s NetworkPolicy / 网关）
- [ ] Redis / Postgres 启用密码 + TLS
- [ ] 定期轮转 LLM API key（建议 ≤90 天）
