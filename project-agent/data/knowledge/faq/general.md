
# FAQ：GameOps Agent 常见问题

## Q1：告警面板上看到问题，怎么给 GameOps Agent？

直接把告警消息复制粘贴进对话框，或者发 Trace ID / 告警 ID。Agent 会自动判断这是诊断请求并调用 DiagnosisAgent 处理。

## Q2：Agent 会不会自己做 rollback 把业务搞挂？

不会。所有写操作（Helm rollback / uninstall、工蜂 MR merge、蓝盾流水线 rerun）都受 HITL 约束：
- 工具层内置 `confirmed` 参数
- 未经人工『确认』时，工具只会返回预览信息，**不下发真实请求**
- 只有收到用户明确回复『确认』后，Agent 才以 `confirmed=true` 二次调用

## Q3：Agent 支持哪些数据源？

**监控类**：蓝鲸监控指标、日志、告警、事件、调用链、CMDB 元数据
**容器类**：BCS 项目 / 集群 / K8s 资源 / Helm release
**代码类**：工蜂 Git（计划 D8+）
**单据类**：TAPD（计划 D8+）
**流水线类**：蓝盾 CI/CD（计划 D9+）
**知识类**：本地 Markdown、iWiki（计划 D12+）

## Q4：Agent 找不到我想要的信息怎么办？

- 检查 `bk_biz_id` / `cluster_id` 是否正确
- 检查凭据：`BK_APP_CODE/BK_APP_SECRET/BCS_TOKEN` 是否配置
- 检查时间范围是否合理（> 30 天的日志可能已归档）
- 日志/Trace 量大时 Agent 会用多次细粒度查询聚合

## Q5：如何让 Agent 记住某些业务知识？

把 Markdown 文档放到 `data/knowledge/` 下对应类别目录：
- `runbook/`      — 操作手册
- `architecture/` — 架构文档
- `faq/`          — 常见问答
- `incident/`     — 故障复盘

重启服务后 KnowledgeAgent 会自动加载并向量化。
