# GameOps Agent — 端到端 LLM 剧本（E2E Playbook）

> **目标**：用一条真实 LLM 对话把 `Coordinator → Diagnosis → Repair → HITL → Audit` 整条链路
> 完整跑一遍，作为：
> - **面试演示脚本**（可复用截图/录屏）
> - **QA 回归用例**（凭据到位后按此检查）
> - **生产部署前的 smoke test**

---

## 1. 前置准备

### 1.1 环境变量最小集
```powershell
# 必需：LLM 模型
$env:OPENAI_API_KEY  = "<your-key>"
$env:OPENAI_BASE_URL = "http://hunyuanapi.woa.com/openapi/v1"  # 或 DeepSeek/其他

# 可选：打开审计日志落盘
$env:AUDIT_SINK = "both"
$env:AUDIT_FILE = "./audit.log"

# 所有 BK/BCS/工蜂/蓝盾/TAPD 凭据留空 → 自动走 Mock 模式
# （剧本无需真实凭据即可跑通）
```

### 1.2 自检
```powershell
go run ./src/cmd/preflight
```
预期输出（关键字段）：
```
✅  LLM Model        REAL       [base=http://hunyuanapi.woa.com/openapi/v1]
✅  审计日志          REAL       [sink=both]
🟡  蓝鲸监控          MOCK       [缺: BK_APP_CODE,BK_APP_SECRET]
🟡  BCS 容器          MOCK       [缺: BCS_TOKEN]
🟡  工蜂 Git          MOCK       [缺: GONGFENG_TOKEN]
🟡  蓝盾 CI/CD        MOCK       [缺: DEVOPS_TOKEN]
🟡  TAPD              MOCK       [缺: TAPD_USER,TAPD_TOKEN]
🟡  iWiki             MOCK       [缺: IWIKI_PAAS_ID,IWIKI_TOKEN]
```

### 1.3 启动服务
```powershell
# 方式 A：HTTP 模式（SSE 推送，给前端/Postman）
go run . -mode http -addr :8080

# 方式 B：CLI 模式（交互式，适合 demo）
go run . -mode cli
```

---

## 2. 剧本：OOM 故障诊断与回滚（★ 推荐演示这条）

### 2.1 对话脚本（用户 ↔ Agent）

#### 第 1 轮：用户报警
> **用户**：`letsgo-gamesvr 生产环境 CPU 告警，pod 反复 OOMKilled，帮我排查一下`

**预期 Agent 行为**：
1. `Coordinator` 决定转交给 `DiagnosisAgent`
2. `DiagnosisAgent` 调用 `bk_alarm_query` 获取告警上下文（走 Mock）
3. 调用 `bk_metrics_query` 拉最近 15 分钟 CPU / Memory
4. 调用 `bcs_resource_query` 查询 Pod 状态和 Events
5. 产出**诊断报告**，点明"内存持续打满、触发 OOMKill，最近一次发布 `game-core-1.2.3` 引入嫌疑"

**关键 SSE 事件**：
```
event: agent_transfer   → diagnosis_agent
event: tool_call        → bk_alarm_query
event: tool_call        → bk_metrics_query
event: tool_call        → bcs_resource_query
event: delta            → (诊断结论文本)
```

#### 第 2 轮：用户确认升级到修复
> **用户**：`可以回滚到上个版本吗？回滚 game-core 到 revision 4`

**预期 Agent 行为**：
1. `Coordinator` 把控制权交给 `RepairAgent`
2. `RepairAgent` 先调用 `bcs_helm_manage action=history` 确认历史版本
3. 然后调用 `bcs_helm_manage action=rollback revision=4`（**未带 confirmed**）
4. 工具返回 `awaiting_confirmation`，LLM 原样展示 `human_prompt`

**关键 SSE 事件**：
```
event: agent_transfer          → repair_agent
event: tool_call               → bcs_helm_manage (history)
event: tool_call               → bcs_helm_manage (rollback, confirmed=false)
event: confirmation_required   → {action: "bcs.helm.rollback", severity: "high", ...}
```

预期用户看到的确认面板：
```
⚠ 即将执行写操作：bcs.helm.rollback（严重级别：high）

• 作用对象：BCS-K8S-00001 / letsgo / game-core
• 副作用：release "game-core" 将回滚到 revision=4，滚动重启对应 Pod
• 影响范围：命名空间 letsgo 下所有关联 Deployment/StatefulSet 实例
• 回滚预案：若回滚后仍异常，可再次调用本工具指向更早的 revision
• 关键参数：cluster_id=BCS-K8S-00001, namespace=letsgo,
           release_name=game-core, revision=4

请回复『确认』以继续；或提供不同参数重新发起。
```

#### 第 3 轮：用户明确确认
> **用户**：`确认`

**预期 Agent 行为**：
1. `RepairAgent` 带 `confirmed=true` 重新调用 `bcs_helm_manage action=rollback revision=4`
2. 工具执行（Mock 下返回 `ROLLED_BACK (mock)`）
3. `audit.Emit` 写一条审计日志
4. LLM 总结处理结果，建议观察 3~5 分钟监控

**关键 SSE 事件**：
```
event: tool_call    → bcs_helm_manage (rollback, confirmed=true)
event: delta        → "已成功回滚 game-core 到 revision=4，建议观察..."
event: final
```

**预期审计日志**（`./audit.log`）：
```json
{"ts":"2026-04-20T17:05:42+08:00","user":"unknown","agent":"repair_agent","action":"bcs.helm.rollback","severity":"high","target":"BCS-K8S-00001 / letsgo / game-core","params":{"cluster_id":"BCS-K8S-00001","namespace":"letsgo","release_name":"game-core","revision":4},"result":"success","mock":true}
```

---

## 3. 剧本：代码缺陷修复（MR 流）

### 3.1 对话脚本
#### 第 1 轮：
> **用户**：`刚才的 OOM 根因是 goroutine 泄漏，请在 video/game-core 仓库开一个 MR 修复`

**预期 Agent 行为**：
1. `RepairAgent` 调用 `gongfeng_mr_create`（未带 confirmed）
2. 返回 `awaiting_confirmation` 展示 Plan

#### 第 2 轮：
> **用户**：`确认`

**预期 Agent 行为**：
1. 带 `confirmed=true` 重调，Mock 返回 MR IID + web_url
2. 审计日志落盘 `gongfeng.mr.create`

---

## 4. 剧本：关联 TAPD 缺陷单

### 4.1 对话脚本
> **用户**：`顺便把这个问题登记到 TAPD 吧`

**预期 Agent 行为**：
1. `RepairAgent` 调用 `tapd_bug_create`（低危软写，仍走 HITL）
2. 用户确认后登记并返回缺陷 ID

---

## 5. 验收 Checklist

在每次跑完剧本后，按此 checklist 核验：

| # | 验收项 | 预期 | 验证方法 |
|---|--------|------|----------|
| 1 | Coordinator 正确 Transfer | 3 次（diagnosis/repair/repair） | SSE `agent_transfer` 事件数 |
| 2 | 诊断工具未触发 HITL | 0 次 `confirmation_required` 发生在 Diagnosis 阶段 | SSE 事件流 |
| 3 | 写操作必触发 HITL | 每次写前必有 `awaiting_confirmation` | 检查工具 args 是否 `confirmed=false` |
| 4 | 确认后仅调用一次 API | Mock 日志 或 真实 API trace 仅一次 | Mock 模式下 `[Mock]` 仅打印一次/action |
| 5 | 审计日志完整 | 每个 `confirmed=true` 动作一条 JSON | `wc -l audit.log` ≥ 3 |
| 6 | 没有 HITL 死循环 | Coordinator 不会在 transfer 后再转回自己 | 观察 `agent_transfer` 方向 |
| 7 | 敏感参数脱敏 | 审计日志 `params` 中不含 token/secret | `grep -i "token\|secret" audit.log` 为空 |

---

## 6. 故障排查

### 6.1 LLM 未触发工具调用
- 检查 `OPENAI_API_KEY` 是否正确
- 检查 base_url 可达性：`curl $OPENAI_BASE_URL/v1/models`
- 查看 `filter.go` 打印的 request/response 日志

### 6.2 Coordinator 不 Transfer
- 检查 `src/agents/coordinator/system_prompt.md` 路由规则是否清晰
- 尝试在用户消息里显式加"请让诊断 agent 处理"

### 6.3 HITL 被绕过
- `echo %HITL_DISABLE%`，若为 1/true → 关掉（仅允许测试用）
- 代码层：搜 `hitl.Require` 看当前工具是否调用

### 6.4 审计日志没落盘
- `echo %AUDIT_DISABLE%`，若为 1/true → 关掉
- 检查 `AUDIT_SINK`（默认 stdout）、`AUDIT_FILE`
- 写入权限：`ls -l audit.log`

---

## 7. 真实凭据下的扩展剧本（生产验收）

当拿到真实凭据后，按此路径切换并跑一遍：

1. `BK_APP_CODE` / `BK_APP_SECRET` → 实际拉 letsgo-* 服务的 CPU/Mem 指标
2. `BCS_TOKEN` → 实际读取 pods / events
3. `GONGFENG_TOKEN` → 真实创建 MR
4. `GONGFENG_ALLOW_AUTO_MERGE=1` + 用户明确确认 → 真实合并（⚠ 生产环境首次演练不开）
5. `DEVOPS_TOKEN` + `DEVOPS_ALLOW_AUTO_OPS=1` → 真实重跑流水线
6. `TAPD_USER` / `TAPD_TOKEN` → 真实登记缺陷单
7. `IWIKI_PAAS_ID` / `IWIKI_TOKEN` → KnowledgeAgent 访问云端 iWiki

预期跑完一轮：
- `preflight -strict` 返回 0
- 审计日志全部 `"mock": false`
- 工蜂 / TAPD / 蓝盾界面可见真实单据

---

## 8. 面试演示建议

⏱ 3 分钟演示时间轴：

| 时间 | 内容 |
|------|------|
| 0:00 - 0:20 | 启动 `preflight` 自检，解释 "LLM REAL + 其他 MOCK" 是设计选择（让演示可重复）|
| 0:20 - 1:00 | 第 1 轮：OOM 排障；强调"Coordinator 自动路由、诊断工具按需并发调用" |
| 1:00 - 1:45 | 第 2 轮：回滚 Helm；强调 "HITL 两段式是强制的，不是可选" |
| 1:45 - 2:15 | 第 3 轮：用户确认；展示审计日志 jsonl |
| 2:15 - 2:45 | 切到 MR 剧本；强调"合并 MR 默认额外闸门 `GONGFENG_ALLOW_AUTO_MERGE`" |
| 2:45 - 3:00 | 收尾："从感知到修复的完整闭环 + 可审计 + 可 RBAC，符合 SRE 变更流程" |

---

**维护说明**：本文档作为剧本真相源，任何 Agent 行为调整（system_prompt / 工具字段 / HITL 级别）都应同步更新预期输出。
