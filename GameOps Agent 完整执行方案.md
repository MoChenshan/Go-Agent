# GameOps Agent —— LetsGo 游戏服务器智能运维助手

## 完整执行方案

> **项目定位**：基于 tRPC-Agent-Go 构建的 **Multi-Agent + Agentic RAG + Graph 编排** 智能运维系统，通过 **Agentic RAG**（Self-RAG / CRAG / GraphRAG）检索运维知识 + 蓝鲸监控 Streamable MCP + BCS 容器平台 MCP + TAPD 工单 MCP + 工蜂 Git MCP + 蓝盾 CI/CD MCP + Skills 技能系统 + 多模态文件分析 + **Deep Research 模式** + **A2A v0.2 协议互通**，实现从「问题发现 → 根因定位 → 自动修复 → 编译验证 → 通知确认」的**全链路自动化闭环**。
>
> **预计工时**：四周（位于总体学习路径第 7 周，详见 [时间有限下的重点深入分析路径.md](D:/UGit/Go-Agent/时间有限下的重点深入分析路径.md)）
>
> **技术框架**：tRPC-Agent-Go（原生支持 Graph 编排、Streamable MCP、A2A v0.2、OTel GenAI Semantic Conv v1.30）
>
> **核心技术栈（2026 Q1-Q2）**：
> - **Agent 编排**：Multi-Agent Transfer/Handoff + StateGraph（checkpoint/interrupt）+ ReAct Planner
> - **模型**：hunyuan-turbo-s（主） / DeepSeek-V3.2-Exp / Qwen3-235B-A22B（旗舰） / **Qwen3-8B 微调（KnowledgeAgent）** / Qwen3-0.6B 本地 Router
> - **工具协议**：**Streamable HTTP MCP**（2025-03 规范为默认，替代 SSE）+ FunctionTool + Skills
> - **知识增强**：**Agentic RAG**（Self-RAG / CRAG）+ **GraphRAG**（知识图谱增强）+ Hybrid Retrieval（Dense + BM25 + RRF）+ BGE-Reranker-v2-M3
> - **评估**：**G-Eval + RAGAS + Langfuse 在线 Trace + Agent Evaluation** 四位一体
> - **可观测性**：**OpenTelemetry GenAI Semantic Conv v1.30** + 伽利略 + Langfuse
> - **前端**：AG-UI（原生支持）+ A2A v0.2 协议（跨 Agent 互通）
>
> **关联项目**：模型算法线（Qwen3-8B 知识专家 + Qwen3-0.6B 本地 Router 融合到 KnowledgeAgent）—— 见 [模型算法微调项目执行方案.md](D:/UGit/Go-Agent/模型算法微调项目执行方案.md)

---

## 一、方案选型与立项依据

### 1.1 为什么选 GameOps（智能运维）方向？

| 维度 | 分析 |
|------|------|
| **JD 能力覆盖** | 一个项目同时展示 **Agentic RAG**、Function Calling、**Streamable MCP**、Multi-Agent 架构、**Graph 编排（checkpoint/interrupt）**、Prompt Engineering、Memory、**Deep Research 模式**、**A2A v0.2**、**OTel GenAI 可观测性**、全链路自动化闭环 —— JD 里的全部核心技术栈 + 2026 主流进阶能力 |
| **业务贴合度** | LetsGo 服务器有完整的监控体系（Prometheus + Monitor + ThreadStateMonitor + MemoryMonitor）、故障排查手册、运维 FAQ、告警响应流程 —— 这些正好构成 Agentic RAG 知识库的高质量语料 |
| **行业前景** | AI Agent 在运维领域（AIOps）是 2025-2026 最确定性的落地场景之一：Gartner 2026 预测 40% 运维工作将由 Agent 自动化完成；AWS/Azure/Google 均已推出 AIOps Agent 产品 |

### 1.2 为什么选 tRPC-Agent-Go 框架？

| 考量因素 | 分析 |
|---------|------|
| **内网生态** | 腾讯内部，tRPC-Agent-Go 与智研、伽利略监控、北极星服务发现无缝集成 |
| **企业级能力** | 内置 Agentic RAG 链路、Session/Memory 服务、Plugin 机制、Graph 编排（含 checkpoint/interrupt）、AG-UI 前端、A2A v0.2 |
| **JD 对齐** | 2026 JD 普遍要求 **Streamable MCP 协议 + A2A v0.2 + Graph 编排 + Agent Evaluation**，tRPC-Agent-Go 全部原生支持 |
| **两周可行性** | `trpc agent` 脚手架一键生成项目骨架，AG-UI 自带前端，不需要从零写 UI |
| **Eino 兼容** | tRPC-Agent-Go 内置了 Eino 适配器，学了其中一个另一个也通了 |
| **生产验证** | 司内 oncall_agent（魔方营销平台问题定位 Agent）已基于 tRPC-Agent-Go 上线运行，验证了框架在生产环境的可靠性 |
| **可观测性** | 原生对接 **OTel GenAI Semantic Convention v1.30**，Trace/Metric 上报 Langfuse / 伽利略，Prompt/Response/Token/工具调用链一站式可观测 |
| **可复用代码** | oncall_agent 提供了完整的 MCP 工具管理、SSE 服务层、A2A/AGUI 注册、ReAct Planner 等经过生产验证的模块，直接 fork 可节省约 6 天开发时间 |

---

## 二、核心使用场景

```
👤 运维工程师 / 开发人员
        │
        ▼
  "昨晚凌晨3点gamesvr重启了3次，什么原因？帮我自动修复"
        │
        ▼
┌───────────────────────────────────────────────────┐
│              GameOps Agent 系统                     │
│                                                    │
│  🤖 Coordinator Agent（意图识别+路由）              │
│      │          │          │        │        │     │
│      ▼          ▼          ▼        ▼        ▼     │
│  📊故障诊断  📚知识问答  📁文件分析  🔧自动修复    │
│  Agent       Agent       Agent     Agent           │
│    │           │           │         │             │
│  蓝鲸MCP    RAG知识库    上传文件   工蜂Git API     │
│  (指标/日志   (运维文档    解析      蓝盾流水线     │
│   /告警/链路)  /FAQ)      (CSV/     TAPD工单       │
│  BCS MCP                  Excel/    企微通知       │
│  (容器状态)               图片/日志)               │
│                                                    │
└───────────────────────────────────────────────────┘
        │
        ▼
  全链路闭环：诊断报告 + 修复MR + 编译验证 → 人工确认合并
```

---

## 二（补）、参考实现 —— oncall_agent 可复用资产分析

> **说明**：本项目参考并借鉴了司内 [oncall_agent](https://git.woa.com/video_pay_oss/magic_group/oncall_agent)（腾讯视频-魔方营销平台问题定位 Agent）的成熟生产实现。oncall_agent 已在线上运行并成功处理多起 oncall 问题，其架构模式、代码组织和工程实践经过生产验证，可以显著加速本项目的开发。以下是可复用资产清单和复用策略。

### 可复用资产清单

| 序号 | 可复用模块 | oncall_agent 源码 | 复用方式 | 预计节省工时 |
|------|-----------|-------------------|---------|------------|
| 1 | **配置化 MCP 工具管理** | `src/tools/mcp_tools/` | 直接复用 target→MCP 映射架构，替换硬编码 MCP 注册 | 1 天 |
| 2 | **SSE 流式服务层** | `src/services/sse/` | 复用 SSE 请求处理 + 流式事件分发 + 工具调用可视化 | 1 天 |
| 3 | **A2A + AGUI 服务注册** | `src/services/a2a/` + `src/services/agui/` | 直接复用，零改动 | 0.5 天 |
| 4 | **自定义 ReAct Planner** | `src/agents/react.go` | 复用中文化 ReAct 标签 + 规划器实现 | 0.5 天 |
| 5 | **Agent 作为 Tool 的封装** | `span_analysis_agent` 的 `agenttool.NewTool()` | 复用 DiagnosisAgent 被 RepairAgent 调用的模式 | 0.5 天 |
| 6 | **模型回调链** | `agents/common.go` 的 `FillSystemContextInfo` | 复用上下文注入 + 动态工具过滤 | 0.5 天 |
| 7 | **配置中心集成** | `src/config/` (rainbow + wuji) | 参考配置分层模式（运行时配置 + Agent 配置 + 工具配置） | 0.5 天 |
| 8 | **main.go 启动模式** | `main.go` 多服务注册 | 复用 A2A + SSE + AGUI 三服务并行注册模式 | 0.5 天 |
| **合计** | | | | **~5 天** |

### 关键设计模式复用详解

#### 1. 配置化 MCP 工具管理（替代硬编码 YAML）

oncall_agent 通过无极（Wuji）配置中心动态管理 MCP 工具列表，核心设计：
- **`target` 服务映射**：每个 MCP 工具关联一个 `target` 服务名（如 `magic.magic_lotter`），Agent 运行时按目标服务动态加载相关工具
- **`target="*"` 通配**：通用工具（如知识库）对所有 Agent 可见
- **双层映射表**：`target → MCP服务列表` + `MCP服务名 → ToolSet 实例`

GameOps Agent 复用方案：将蓝鲸 MCP、BCS MCP、TAPD MCP 等 16 个 MCP Server 注册到配置中心，按 Agent 职责自动分配工具，替代硬编码的 `mcp_servers.yaml`。

```go
// 复用 oncall_agent 的 mcpToolImpl 架构
// 配置中心管理所有 MCP 工具，Agent 初始化时按 target 加载
type mcpToolImpl struct {
    target2MCPListMap  map[string][]string    // target服务→MCP服务列表
    mcpName2ToolSetMap map[string]*mcp.ToolSet // MCP服务名→ToolSet实例
}

// DiagnosisAgent 初始化：只加载 target="bk-monitor" 和 target="bcs" 的工具
for _, name := range mcpTool.GetMCPListByTarget("bk-monitor") {
    toolSets = append(toolSets, mcpTool.GetMCPToolsByName(name))
}
// RepairAgent 初始化：只加载 target="gongfeng" 和 target="devops" 的工具
for _, name := range mcpTool.GetMCPListByTarget("gongfeng") {
    toolSets = append(toolSets, mcpTool.GetMCPToolsByName(name))
}
```

#### 2. 动态工具过滤（子 Agent 按需加载工具）

oncall_agent 的 span_analysis_agent 实现了一个精巧的 `createToolFilter` 机制：在 `BeforeModel` 回调中，从请求中提取目标服务名，动态过滤工具列表——子 Agent 只能看到与当前分析目标相关的工具。

GameOps Agent 复用方案：DiagnosisAgent 根据告警来源服务动态加载对应的 BCS/蓝鲸工具；RepairAgent 根据修复目标自动切换工蜂项目和蓝盾流水线。

```go
// 复用 oncall_agent 的动态工具过滤模式
func createToolFilter(mcpTool mcptool.API) model.BeforeModelFunc {
    return func(ctx context.Context, req *model.Request) (*model.Response, error) {
        // 提取目标服务名（如从告警信息中解析）
        target := extractTargetService(req)
        // 动态加载相关工具
        toolMap := make(map[string]tool.Tool)
        for _, name := range mcpTool.GetMCPListByTarget(target) {
            ts := mcpTool.GetMCPToolsByName(name)
            for _, t := range ts.Tools(ctx) {
                toolMap[t.Declaration().Name] = t
            }
        }
        // 叠加通用工具（target="*"）
        for _, name := range mcpTool.GetMCPListByTarget("*") {
            ts := mcpTool.GetMCPToolsByName(name)
            for _, t := range ts.Tools(ctx) {
                toolMap[t.Declaration().Name] = t
            }
        }
        req.Tools = toolMap
        return nil, nil
    }
}
```

#### 3. Agent 作为 Tool 封装（子 Agent 互相调用）

oncall_agent 将 span_analysis_agent 封装为 `agenttool.NewTool()`，入口 Agent 像调用普通工具一样调用子 Agent，并支持独立上下文、流式输出、自动总结。

GameOps Agent 复用方案：DiagnosisAgent 可被 RepairAgent 作为 Tool 调用（获取诊断结论），FileAnalystAgent 可被 DiagnosisAgent 作为 Tool 调用（分析上传文件）。

```go
// 复用 agenttool.NewTool() 封装模式
diagnosisAgentTool := agenttool.NewTool(
    diagnosisAgent,
    agenttool.WithHistoryScope(agenttool.HistoryScopeIsolated), // 独立上下文
    agenttool.WithStreamInner(true),                             // 内部流式输出
    agenttool.WithSkipSummarization(false),                      // 自动总结结果
)
```

#### 4. SSE 流式服务 + 工具调用可视化

oncall_agent 的 SSE 服务层实现了完整的流式响应处理，包括：
- 工具调用开始时输出 `*开始执行工具: xxx*`
- Debug 模式下显示工具请求参数
- 工具响应后继续流式输出 LLM 推理
- 完成事件带 Token 使用统计

GameOps Agent 可直接复用此实现，仅需调整 SSE 请求/响应格式适配前端。

#### 5. 配置化 Agent System Prompt

oncall_agent 采用 `//go:embed system_prompt.md` + 远程配置中心覆盖的双层设计：
- **默认值**：代码中嵌入 `system_prompt.md` 文件
- **运行时覆盖**：从配置中心（无极/Rainbow）读取配置，非空则覆盖默认值
- **支持热更新**：修改配置中心后无需重新部署

GameOps Agent 各子 Agent 的 System Prompt 均采用此模式，方便快速迭代 Prompt 而无需重新部署。

### 开发加速预估

| 复用模块 | 传统从零开发 | 复用 oncall_agent | 节省 |
|---------|------------|-------------------|------|
| MCP 工具管理 | 2 天 | 0.5 天（适配配置） | 1.5 天 |
| SSE 服务层 | 2 天 | 0.5 天（调格式） | 1.5 天 |
| A2A + AGUI | 1 天 | 0.5 天 | 0.5 天 |
| ReAct Planner | 1 天 | 0 天（直接复用） | 1 天 |
| Agent-as-Tool | 1 天 | 0.5 天 | 0.5 天 |
| 回调链 + 上下文注入 | 1 天 | 0 天（直接复用） | 1 天 |
| **合计** | **8 天** | **2 天** | **~6 天** |

> 以 oncall_agent 为骨架 fork 一份代码，替换业务 Agent 逻辑和 MCP 工具配置，即可在 **2 天内**完成基础骨架，将更多时间投入到业务 Prompt 调优和全链路闭环验证上。

---

## 三、系统架构

### 3.1 整体架构图

```
┌──────────────── 用户入口 ────────────────┐
│  AG-UI Web    OpenAI API    企微机器人     │
│  (自带前端)   (/v1/chat)    (未来扩展)     │
│                                           │
│  🔔 自动触发入口                           │
│  ├─ 蓝鲸告警Webhook                       │
│  ├─ TAPD新Bug单Webhook                    │
│  └─ BCS Pod异常事件                        │
└──────────────────┬──────────────────────┘
                   │
                   ▼
┌──────────────── Agent 层 ───────────────┐
│                                          │
│  Runner（生命周期管理 + Session + Memory） │
│      │                                   │
│  Coordinator Agent（意图路由 + Transfer）  │
│      │            │           │      │   │
│      ▼            ▼           ▼      ▼   │
│  Diagnosis    Knowledge    File   Repair │
│  Agent        Agent       Analyst Agent  │
│  (蓝鲸MCP    (RAG检索)   (文件    (修复  │
│   +BCS MCP)              +图片)  闭环)   │
│                                          │
└──────────────────┬──────────────────────┘
                   │
    ┌──────────┬───┼───────┬──────────┐
    ▼          ▼   ▼       ▼          ▼
┌────────┐┌──────┐┌─────┐┌──────┐┌────────┐
│蓝鲸MCP  ││BCS   ││自建  ││Skills ││修复工具 │
│(7个)    ││MCP   ││Tool  ││(技能) ││链      │
│         ││(4个) ││(2-3) ││      ││        │
│指标查询  ││项目   ││文件   ││日志   ││工蜂Git │
│日志查询  ││集群   ││解析   ││分析   ││(4个)  │
│告警查询  ││资源   ││图片   ││CSV   ││TAPD   │
│事件查询  ││Helm  ││分析   ││对比   ││MCP    │
│APM链路  ││      ││健康   ││性能   ││蓝盾   │
│元数据   ││      ││检查   ││报告   ││MCP    │
└────────┘└──────┘└─────┘└──────┘└────────┘
                   │
                   ▼
┌──────────────── 数据层 ────────────────┐
│  向量数据库(PGVector)  Redis(Session)   │
│  运维文档(RAG语料)     Memory(长期记忆)  │
└────────────────────────────────────────┘
```

### 3.2 技术选型

| 组件 | 选择 | 理由 |
|------|------|------|
| **Agent 框架** | tRPC-Agent-Go | 企业级 + 内网生态 + AG-UI + Graph（checkpoint/interrupt）+ A2A v0.2；oncall_agent 已验证其生产可用性 |
| **大模型（主）** | hunyuan-turbo-s | 内网优先，低延迟首选；128K 上下文 + Function Calling + 中文运维语料理解强 |
| **大模型（备/推理增强）** | **DeepSeek-V3.2-Exp** / **Qwen3-235B-A22B** | DeepSeek-V3.2 成本极低，中文运维强；Qwen3-235B MoE 旗舰用于复杂根因分析 + thinking mode |
| **微调专家模型** | **Qwen3-8B 微调** (KnowledgeAgent 主模型) + **Qwen3-0.6B** (本地 Intent Router) | 详见模型算法微调方案；FP8 / GPTQ-Marlin 量化 + vLLM V1 + EAGLE-3 部署 |
| **Embedding 模型** | **BGE-M3** / **GTE-Qwen2-7B-instruct** | BGE-M3 支持 Dense + Sparse + ColBERT 三合一；GTE-Qwen2 中文技术文档检索 SOTA |
| **Reranker** | **BGE-Reranker-v2-M3** | 2026 中文 SOTA 重排模型，支持多语言 |
| **向量数据库** | **Milvus 2.5** 或 **PGVector 0.8 + HNSW** | Milvus 2.5 原生支持 Sparse Vector + 全文检索；PGVector 0.8 内置 HNSW，单库搞定混合检索 |
| **Session 存储** | InMemory（初期）→ Redis（生产） | 借鉴 oncall_agent 的 inmemory + summarizer 模式，后期可迁移 Redis |
| **前端** | AG-UI (agui.woa.com) | 零前端开发成本；oncall_agent 已验证 AGUI 接入方式 |
| **工具协议** | **Streamable HTTP MCP** + FunctionTool | 2025-03 MCP 规范，替代 SSE 为默认；tRPC-Agent-Go 原生支持；借鉴 oncall_agent 的 target 映射 + 动态工具过滤 |
| **Agent 互通协议** | **A2A v0.2** | 2026 主流跨 Agent 互通协议，支持 skill 声明 + task 生命周期 + push notification |
| **MCP 工具管理** | 配置中心 + target 映射 + **dynamic tool selection** | 借鉴 oncall_agent 的 mcpToolImpl 架构，支持运行时动态加载 + Top-K 工具选择 |
| **配置管理** | Rainbow + 配置中心 | 借鉴 oncall_agent 的 rainbow（运行时配置） + wuji（Agent/工具配置） 分层 |
| **评估** | **G-Eval + RAGAS + Langfuse + Agent Evaluation** | 四位一体：G-Eval 自定义维度，RAGAS RAG 专用，Langfuse 在线 Trace，Agent Evaluation 端到端 |
| **可观测性** | **OpenTelemetry GenAI Semantic Conv v1.30** + 伽利略 + Langfuse | LLM 调用、工具调用、Agent Transfer 全链路 Trace；Token/Cost/Latency 自动埋点 |
| **安全防护** | **Guardrails + OWASP LLM Top 10 2025** + safety_guard Plugin | 输入输出防护 + 危险操作拦截 + Prompt Injection 检测 |
| **部署** | tRPC 服务 + K8s | 贴合现有部署体系；复用 oncall_agent 的 upload2test.sh 部署脚本 |

---

## 四、蓝鲸 MCP 接入（核心数据源，免开发）

### 4.1 蓝鲸 MCP → Agent 工具映射

蓝鲸已提供 7 个 MCP Server，加上 BCS 容器平台 4 个、TAPD 工单管理、蓝盾流水线，共 **16 个 MCP Server** 覆盖全链路运维需求，**直接接入无需自建查询工具**：

| 平台 | MCP Server | 能力 | 对应 Agent 场景 |
|------|-----------|------|---------------|
| 🔷蓝鲸监控 | `bkmonitorv3-prod-metrics-query` | 指标查询 | CPU/内存/QPS/GC 等监控指标分析 |
| 🔷蓝鲸监控 | `bkmonitorv3-prod-log-query` | 日志查询 | 错误日志搜索、异常模式识别 |
| 🔷蓝鲸监控 | `bkmonitorv3-prod-alarm` | 告警查询 | 告警历史检索、告警关联分析 |
| 🔷蓝鲸监控 | `bkmonitorv3-prod-event-query` | 事件查询 | 变更事件、重启事件关联 |
| 🔷蓝鲸监控 | `bkmonitorv3-prod-tracing` | APM Tracing | 调用链路分析、慢请求诊断 |
| 🔷蓝鲸监控 | `bkmonitorv3-prod-metadata-query` | 元数据查询 | 服务列表、指标元信息查询 |
| 🔷蓝鲸监控 | `bkmonitorv3-prod-dashboard-query` | 仪表盘查询 | 获取已有监控面板配置 |
| 🟢BCS容器 | `bcs-project` | 项目管理 | ListAuthorizedProjects, GetProject |
| 🟢BCS容器 | `bcs-cluster` | 集群管理 | ListProjectCluster, GetCluster |
| 🟢BCS容器 | `bcs-resource` | K8s资源 | ListPo, GetPo, ListDeploy, ListSTS, ListSVC |
| 🟢BCS容器 | `bcs-helm` | Helm管理 | ListRepository, ListChartV1, ListReleaseV1 |
| 🟡TAPD | `tapd_mcp_http` | 缺陷管理 | bugs_get, bugs_create, bugs_update, bugs_count |
| 🟡蓝盾 | `devops-prod-pipeline-streamable` | CI/CD | v4_user_build_start, v4_user_build_status |

### 4.2 接入代码示例

```go
// 蓝鲸监控 MCP - 指标查询
metricsToolSet := mcp.NewMCPToolSet(
    mcp.ConnectionConfig{
        Transport: "sse",
        ServerURL: "https://bk-apigateway.apigw.o.woa.com/prod/api/v2/mcp-servers/bkmonitorv3-prod-metrics-query/mcp/",
        Timeout:   30 * time.Second,
        HTTPBeforeRequest: func(req *http.Request) {
            req.Header.Set("X-Bkapi-Authorization",
                fmt.Sprintf(`{"access_token":"%s"}`, os.Getenv("BK_ACCESS_TOKEN")))
        },
    },
)

// 蓝鲸监控 MCP - 日志查询
logToolSet := mcp.NewMCPToolSet(
    mcp.ConnectionConfig{
        Transport: "sse",
        ServerURL: "https://bk-apigateway.apigw.o.woa.com/prod/api/v2/mcp-servers/bkmonitorv3-prod-log-query/mcp/",
        Timeout:   30 * time.Second,
        HTTPBeforeRequest: func(req *http.Request) {
            req.Header.Set("X-Bkapi-Authorization",
                fmt.Sprintf(`{"access_token":"%s"}`, os.Getenv("BK_ACCESS_TOKEN")))
        },
    },
)

// 蓝鲸监控 MCP - 告警查询
alarmToolSet := mcp.NewMCPToolSet(
    mcp.ConnectionConfig{
        Transport: "sse",
        ServerURL: "https://bk-apigateway.apigw.o.woa.com/prod/api/v2/mcp-servers/bkmonitorv3-prod-alarm/mcp/",
        Timeout:   30 * time.Second,
        HTTPBeforeRequest: func(req *http.Request) {
            req.Header.Set("X-Bkapi-Authorization",
                fmt.Sprintf(`{"access_token":"%s"}`, os.Getenv("BK_ACCESS_TOKEN")))
        },
    },
)

// 同理接入 event-query, tracing, metadata-query ...

// 合并所有 MCP 工具
allMCPTools := []tool.Tool{}
allMCPTools = append(allMCPTools, metricsToolSet.Tools()...)
allMCPTools = append(allMCPTools, logToolSet.Tools()...)
allMCPTools = append(allMCPTools, alarmToolSet.Tools()...)
allMCPTools = append(allMCPTools, eventToolSet.Tools()...)
allMCPTools = append(allMCPTools, tracingToolSet.Tools()...)
allMCPTools = append(allMCPTools, metadataToolSet.Tools()...)
```

### 4.3 配置化 MCP 注册（支持未来动态扩展）

为了未来方便接入更多 MCP，把 MCP 配置外化：

```yaml
# mcp_servers.yaml
mcp_servers:
  - name: "bk-metrics"
    url: "https://bk-apigateway.apigw.o.woa.com/prod/api/v2/mcp-servers/bkmonitorv3-prod-metrics-query/mcp/"
    transport: "sse"
    timeout: 30s
    auth_header: "X-Bkapi-Authorization"
    auth_value_env: "BK_ACCESS_TOKEN"

  - name: "bk-log"
    url: "https://bk-apigateway.apigw.o.woa.com/prod/api/v2/mcp-servers/bkmonitorv3-prod-log-query/mcp/"
    transport: "sse"
    timeout: 30s
    auth_header: "X-Bkapi-Authorization"
    auth_value_env: "BK_ACCESS_TOKEN"

  - name: "bk-alarm"
    url: "https://bk-apigateway.apigw.o.woa.com/prod/api/v2/mcp-servers/bkmonitorv3-prod-alarm/mcp/"
    transport: "sse"
    timeout: 30s
    auth_header: "X-Bkapi-Authorization"
    auth_value_env: "BK_ACCESS_TOKEN"

  - name: "bk-event"
    url: "https://bk-apigateway.apigw.o.woa.com/prod/api/v2/mcp-servers/bkmonitorv3-prod-event-query/mcp/"
    transport: "sse"
    timeout: 30s
    auth_header: "X-Bkapi-Authorization"
    auth_value_env: "BK_ACCESS_TOKEN"

  - name: "bk-tracing"
    url: "https://bk-apigateway.apigw.o.woa.com/prod/api/v2/mcp-servers/bkmonitorv3-prod-tracing/mcp/"
    transport: "sse"
    timeout: 30s
    auth_header: "X-Bkapi-Authorization"
    auth_value_env: "BK_ACCESS_TOKEN"

  - name: "bk-metadata"
    url: "https://bk-apigateway.apigw.o.woa.com/prod/api/v2/mcp-servers/bkmonitorv3-prod-metadata-query/mcp/"
    transport: "sse"
    timeout: 30s
    auth_header: "X-Bkapi-Authorization"
    auth_value_env: "BK_ACCESS_TOKEN"

  # ==================== BCS 容器平台 MCP ====================
  - name: "bcs-project"
    url: "https://bk-apigateway.apigw.o.woa.com/prod/api/v2/mcp-servers/bcs-api-gateway-mcp-project/application/mcp/"
    transport: "streamable-http"
    timeout: 30s
    auth_header: "X-Bkapi-Authorization"
    auth_template: '{"bk_app_code":"${BCS_APP_CODE}","bk_app_secret":"${BCS_APP_SECRET}","bk_ticket":"${BCS_TICKET}"}'

  - name: "bcs-cluster"
    url: "https://bk-apigateway.apigw.o.woa.com/prod/api/v2/mcp-servers/bcs-api-gateway-mcp-cluster/application/mcp/"
    transport: "streamable-http"
    timeout: 30s
    auth_header: "X-Bkapi-Authorization"
    auth_template: '{"bk_app_code":"${BCS_APP_CODE}","bk_app_secret":"${BCS_APP_SECRET}","bk_ticket":"${BCS_TICKET}"}'

  - name: "bcs-resource"
    url: "https://bk-apigateway.apigw.o.woa.com/prod/api/v2/mcp-servers/bcs-api-gateway-mcp-resource/application/mcp/"
    transport: "streamable-http"
    timeout: 30s
    auth_header: "X-Bkapi-Authorization"
    auth_template: '{"bk_app_code":"${BCS_APP_CODE}","bk_app_secret":"${BCS_APP_SECRET}","bk_ticket":"${BCS_TICKET}"}'

  - name: "bcs-helm"
    url: "https://bk-apigateway.apigw.o.woa.com/prod/api/v2/mcp-servers/bcs-api-gateway-mcp-helm/application/mcp/"
    transport: "streamable-http"
    timeout: 30s
    auth_header: "X-Bkapi-Authorization"
    auth_template: '{"bk_app_code":"${BCS_APP_CODE}","bk_app_secret":"${BCS_APP_SECRET}","bk_ticket":"${BCS_TICKET}"}'

  # ==================== TAPD 工单管理 MCP ====================
  - name: "tapd"
    url: "https://mcp-oa.tapd.woa.com/mcp/"
    transport: "streamable-http"
    timeout: 20s
    headers:
      X-Tapd-Access-Token: "${TAPD_ACCESS_TOKEN}"
      X-Tools-Set: "bugs_get,bugs_create,bugs_update,bugs_count,bugs_fields_info_get,stories_get,tasks_get"

  # ==================== 蓝盾 CI/CD MCP ====================
  - name: "devops-pipeline"
    url: "https://bk-apigateway.apigw.o.woa.com/prod/api/v2/mcp-servers/devops-prod-pipeline-streamable/mcp/"
    transport: "streamable-http"
    timeout: 60s
    auth_header: "X-Bkapi-Authorization"
    auth_template: '{"access_token":"${DEVOPS_ACCESS_TOKEN}"}'

  # 未来新增 MCP 只需加配置，不改代码
  # - name: "cmdb"
  #   url: "https://xxx/cmdb-mcp/mcp/"
  #   ...
  # - name: "ticket"
  #   url: "https://xxx/ticket-mcp/mcp/"
  #   ...
```

配置化加载代码（借鉴 oncall_agent 的无极配置中心 + target 映射模式）：

```go
// mcp/loader.go — 双模式：支持 YAML 配置 + 远程配置中心

// ========== 模式一：YAML 文件配置（本地开发/测试） ==========
func LoadMCPToolSetsFromYAML(configPath string) ([]tool.Tool, error) {
    cfg := loadConfig(configPath)
    var allTools []tool.Tool
    for _, srv := range cfg.MCPServers {
        ts := mcp.NewMCPToolSet(mcp.ConnectionConfig{
            Transport: srv.Transport, // 支持 "sse" 和 "streamable-http"
            ServerURL: srv.URL,
            Timeout:   srv.Timeout,
            HTTPBeforeRequest: func(req *http.Request) {
                if srv.AuthTemplate != "" {
                    // 模板化认证（BCS/蓝盾等）
                    authValue := expandEnvTemplate(srv.AuthTemplate)
                    req.Header.Set(srv.AuthHeader, authValue)
                } else if srv.AuthValueEnv != "" {
                    // 简单 Token 认证（蓝鲸监控等）
                    token := os.Getenv(srv.AuthValueEnv)
                    req.Header.Set(srv.AuthHeader,
                        fmt.Sprintf(`{"access_token":"%s"}`, token))
                }
                // 额外 Headers（TAPD 等）
                for k, v := range srv.Headers {
                    req.Header.Set(k, os.ExpandEnv(v))
                }
            },
        })
        allTools = append(allTools, ts.Tools()...)
    }
    return allTools, nil
}

// ========== 模式二：配置中心动态加载（生产环境，借鉴 oncall_agent） ==========
// 核心优势：支持 target 服务映射，不同 Agent 按需加载工具；支持运行时热更新

// API 定义 MCP 工具管理接口
type MCPToolAPI interface {
    // 通过目标服务名获取相关的 MCP 服务列表
    // 示例: GetMCPListByTarget("bk-monitor") => ["bk-metrics", "bk-log", "bk-alarm"]
    // 示例: GetMCPListByTarget("*") => ["gameops-knowledge"]（通用工具）
    GetMCPListByTarget(target string) []string
    // 通过 MCP 服务名获取该服务下的工具集
    GetMCPToolsByName(mcpName string) tool.ToolSet
    // 获取所有工具集
    GetAllToolSets() []tool.ToolSet
}

type mcpToolImpl struct {
    target2MCPListMap  map[string][]string    // 目标服务→MCP服务名列表
    mcpName2ToolSetMap map[string]*mcp.ToolSet // MCP服务名→ToolSet实例
}

// NewMCPToolImpl 从配置中心初始化 MCP 工具（并发加载，借鉴 oncall_agent）
func NewMCPToolImpl(ctx context.Context, configCli ConfigAPI) (MCPToolAPI, error) {
    records := configCli.GetAllMCPTool()
    target2MCPListMap := make(map[string][]string)
    mcpName2ToolSetMap := make(map[string]*mcp.ToolSet)

    var mu sync.Mutex
    var handlers []func() error
    for _, record := range records {
        if record.Valid == 0 {
            continue
        }
        r := record // 闭包捕获
        handlers = append(handlers, func() error {
            var headerMap map[string]string
            json.Unmarshal([]byte(r.Headers), &headerMap)
            // 环境变量渲染
            for k, v := range headerMap {
                headerMap[k] = os.ExpandEnv(v)
            }
            opts := []mcp.ToolSetOption{
                mcp.WithSessionReconnect(3),
                mcp.WithName(r.MCPServerName),
            }
            if r.AllowedTools != "" {
                opts = append(opts, mcp.WithToolFilterFunc(
                    tool.NewIncludeToolNamesFilter(strings.Split(r.AllowedTools, "\n")...),
                ))
            }
            ts := mcp.NewMCPToolSet(mcp.ConnectionConfig{
                Transport: r.TransportType,
                ServerURL: r.URL,
                Timeout:   10 * time.Second,
                Headers:   headerMap,
            }, opts...)

            mu.Lock()
            target2MCPListMap[r.Target] = append(target2MCPListMap[r.Target], r.MCPServerName)
            mcpName2ToolSetMap[r.MCPServerName] = ts
            mu.Unlock()
            return nil
        })
    }
    // 并发初始化所有 MCP 连接
    if err := trpc.GoAndWait(handlers...); err != nil {
        return nil, err
    }
    return &mcpToolImpl{target2MCPListMap, mcpName2ToolSetMap}, nil
}
```

配置中心的 MCP 工具表结构（对应无极/数据库配置）：

| 字段 | 类型 | 说明 | 示例 |
|------|------|------|------|
| `mcp_server_name` | string | MCP 服务名（唯一标识） | `bk-metrics` |
| `url` | string | MCP Server 地址 | `https://bk-apigateway.apigw.o.woa.com/.../mcp/` |
| `transport_type` | string | 传输协议 | `sse` / `streamable_http` |
| `headers` | JSON | 请求头（支持环境变量渲染） | `{"X-Bkapi-Authorization": "{\"access_token\": \"${BK_TOKEN}\"}"}` |
| `target` | string | 关联的目标服务/Agent | `bk-monitor` / `bcs` / `*`（通配） |
| `allowed_tools` | string | 工具白名单（换行分隔） | `query_metrics\nquery_log` |
| `valid` | int | 是否启用 | 1 / 0 |

Target 映射规则：
| target 值 | 含义 | 哪些 Agent 可见 |
|-----------|------|---------------|
| `bk-monitor` | 蓝鲸监控相关 | DiagnosisAgent |
| `bcs` | BCS 容器平台相关 | DiagnosisAgent |
| `gongfeng` | 工蜂 Git 相关 | RepairAgent |
| `devops` | 蓝盾 CI/CD 相关 | RepairAgent |
| `tapd` | TAPD 工单相关 | RepairAgent |
| `*` | 通用工具（知识库等） | 所有 Agent |
```

### 4.4 MCP 扩展路线

| 阶段 | MCP Server | 能力 |
|------|-----------|------|
| **当前** | 蓝鲸监控 7 个 MCP | 指标/日志/告警/事件/链路/元数据/仪表盘 |
| **当前** | BCS 容器平台 4 个 MCP | 项目/集群/K8s资源/Helm |
| **当前** | TAPD MCP + 蓝盾 MCP | 工单管理 + CI/CD 编译验证 |
| **未来** | CMDB MCP（如有） | 服务拓扑、配置管理 |
| **未来** | 容量管理 MCP | 资源预测、扩缩容 |

---

## 五、自建工具（FunctionTool）

蓝鲸 MCP 覆盖了大部分数据查询能力，**自建工具补齐 MCP 未覆盖的场景**，主要包括文件解析、图片分析、工蜂 Git 代码操作和蓝盾编译验证：

### 5.1 文件解析工具

```go
// tool/file_analyze.go
fileAnalyzeTool := function.NewFunctionTool("file_analyze",
    "解析用户上传的文件内容。支持 CSV、Excel、日志文件、配置文件(yaml/json/properties)等。"+
        "参数: file_id(文件标识), analysis_type(可选: summary/full/search), keyword(可选: 搜索关键词)",
    func(ctx context.Context, args map[string]interface{}) (string, error) {
        fileID := args["file_id"].(string)
        filePath := filepath.Join("/tmp/gameops/uploads", fileID+"*")
        matches, _ := filepath.Glob(filePath)
        if len(matches) == 0 {
            return "文件不存在", nil
        }
        content, _ := os.ReadFile(matches[0])
        ext := filepath.Ext(matches[0])
        switch ext {
        case ".csv":
            return parseCSVSummary(content), nil
        case ".log", ".txt":
            return extractKeyLines(content, args), nil
        case ".yaml", ".yml", ".json", ".properties":
            return formatConfig(content, ext), nil
        default:
            return truncateContent(content, 4000), nil
        }
    },
)
```

### 5.2 图片分析工具（多模态）

```go
// tool/image_analyze.go
imageAnalyzeTool := function.NewFunctionTool("analyze_image",
    "分析用户上传的图片（如监控截图、错误截图等），提取图片中的关键信息",
    func(ctx context.Context, args map[string]interface{}) (string, error) {
        fileID := args["file_id"].(string)
        imgData, _ := os.ReadFile(getFilePath(fileID))
        base64Img := base64.StdEncoding.EncodeToString(imgData)

        // 调用多模态模型（混元/GPT-4o）做图片理解
        resp, _ := visionModel.Chat(ctx, []model.Message{
            model.NewUserMessageWithImage(
                "请分析这张监控/运维相关的截图，提取所有关键指标、异常信息和时间点",
                base64Img,
            ),
        })
        return resp.Content, nil
    },
)
```

### 5.3 健康检查工具

```go
// tool/health_check.go
healthCheckTool := function.NewFunctionTool("health_check",
    "检查游戏服务器各微服务的健康状态，包括 Pod 数量、CPU/内存使用率等",
    func(ctx context.Context, args map[string]interface{}) (string, error) {
        serviceName := args["service_name"].(string) // 可选，空则查全部
        // 调用 K8s API 或内部健康检查接口
        return checkServiceHealth(ctx, serviceName)
    },
)
```

### 5.4 工蜂 Git 代码操作工具

工蜂 Git 不直接提供 MCP Server，需要封装 REST API 为 FunctionTool，用于自动修复闭环中的代码提交：

```go
// tool/gongfeng_git.go

// 创建分支
createBranchTool := function.NewFunctionTool("git_create_branch",
    "在工蜂Git仓库中创建新分支。参数: project_id(项目ID), branch_name(新分支名), ref(基于哪个分支/tag/SHA创建)",
    func(ctx context.Context, args map[string]interface{}) (string, error) {
        projectID := args["project_id"].(string)
        branchName := args["branch_name"].(string)
        ref := args["ref"].(string)
        // POST /api/v3/projects/:id/repository/branches
        resp, err := gongfengAPI.CreateBranch(ctx, projectID, branchName, ref)
        if err != nil {
            return fmt.Sprintf("创建分支失败: %v", err), err
        }
        return fmt.Sprintf("✅ 成功创建分支 %s (基于 %s)", branchName, ref), nil
    },
)

// 提交文件（创建或更新）
commitFileTool := function.NewFunctionTool("git_commit_file",
    "向工蜂Git仓库提交文件变更。参数: project_id, file_path, content(Base64编码), branch, commit_message, action(create/update)",
    func(ctx context.Context, args map[string]interface{}) (string, error) {
        // PUT /api/v3/projects/:id/repository/files
        resp, err := gongfengAPI.CreateOrUpdateFile(ctx, args)
        if err != nil {
            return fmt.Sprintf("提交代码失败: %v", err), err
        }
        return fmt.Sprintf("✅ 代码已提交到 %s 分支", args["branch"]), nil
    },
)

// 创建 Merge Request
createMRTool := function.NewFunctionTool("git_create_merge_request",
    "在工蜂Git仓库中创建合并请求(MR)。参数: project_id, source_branch, target_branch, title, description",
    func(ctx context.Context, args map[string]interface{}) (string, error) {
        // POST /api/v3/projects/:id/merge_requests
        resp, err := gongfengAPI.CreateMergeRequest(ctx, args)
        if err != nil {
            return fmt.Sprintf("创建MR失败: %v", err), err
        }
        return fmt.Sprintf("✅ MR已创建: %s\n链接: %s", resp.Title, resp.WebURL), nil
    },
)

// 获取文件内容（用于AI分析源码）
getFileTool := function.NewFunctionTool("git_get_file_content",
    "获取工蜂Git仓库中指定文件的内容。参数: project_id, file_path, ref(分支名/commit SHA)",
    func(ctx context.Context, args map[string]interface{}) (string, error) {
        // GET /api/v3/projects/:id/repository/files?file_path=xxx&ref=xxx
        content, err := gongfengAPI.GetFileContent(ctx, args)
        if err != nil {
            return fmt.Sprintf("获取文件失败: %v", err), err
        }
        return content, nil
    },
)
```

### 5.5 蓝盾编译验证工具

在自动修复流程中，代码提交后需要触发编译验证确保修复代码可以编译通过：

```go
// tool/devops_build.go
// 蓝盾编译验证 - 30秒间隔轮询，最长等待30分钟
func pollBuildStatus(ctx context.Context, projectID, pipelineID, buildID string) (string, error) {
    maxRetries := 60  // 60次 × 30秒 = 30分钟
    for i := 0; i < maxRetries; i++ {
        status, err := devopsMCP.CallTool("v4_user_build_status", map[string]interface{}{
            "projectId":  projectID,
            "pipelineId": pipelineID,
            "buildId":    buildID,
        })
        if err != nil {
            return "", err
        }
        switch status.State {
        case "SUCCEED":
            return "✅ 编译成功", nil
        case "FAILED", "CANCELED":
            return fmt.Sprintf("❌ 编译失败: %s", status.Message), nil
        }
        time.Sleep(30 * time.Second)
    }
    return "⏰ 编译超时（超过30分钟）", nil
}
```

---

## 六、文件/图片上传分析

### 6.1 问题场景

业务中经常遇到没有固定采集平台的数据：
- 导出的 CSV/Excel 性能数据需要分析对比
- 截图的监控面板图片需要 OCR + 理解
- 某些内部工具导出的日志文件不在蓝鲸里
- 配置文件需要检查是否有问题

### 6.2 实现方案

```
用户上传文件 → HTTP /upload → 本地临时存储
                                    │
                    ┌───────────────┼───────────────┐
                    ▼               ▼               ▼
               文本类文件       图片类文件       复杂分析
               file_analyze     analyze_image   Skills脚本
               (CSV/Log/Config) (多模态模型)    (Python处理)
                    │               │               │
                    └───────────────┼───────────────┘
                                    ▼
                            Agent 综合分析
```

### 6.3 文件上传 HTTP 接口

```go
// server/upload.go
func handleUpload(w http.ResponseWriter, r *http.Request) {
    // 限制 50MB
    r.ParseMultipartForm(50 << 20)

    file, header, err := r.FormFile("file")
    if err != nil {
        http.Error(w, "文件上传失败", http.StatusBadRequest)
        return
    }
    defer file.Close()

    // 存到临时目录，生成 fileID
    fileID := uuid.New().String()
    ext := filepath.Ext(header.Filename)
    savePath := filepath.Join("/tmp/gameops/uploads", fileID+ext)

    os.MkdirAll("/tmp/gameops/uploads", 0755)
    dst, _ := os.Create(savePath)
    io.Copy(dst, file)
    dst.Close()

    // 返回 fileID，用户后续对话时引用
    json.NewEncoder(w).Encode(map[string]string{
        "file_id":  fileID,
        "filename": header.Filename,
        "path":     savePath,
    })
}
```

---

## 七、Skills 技能系统

### 7.1 Skills 目录结构

```
skills/
├── log_pattern/
│   ├── SKILL.md            # "从日志文件中提取错误模式和频率统计"
│   └── scripts/
│       └── analyze.py      # 正则匹配 + 统计脚本
├── csv_compare/
│   ├── SKILL.md            # "对比两个 CSV 文件的差异，生成变化摘要"
│   └── scripts/
│       └── compare.py      # pandas diff 脚本
└── perf_report/
    ├── SKILL.md            # "解析性能数据，生成趋势分析报告"
    └── scripts/
        └── report.py       # 性能数据分析脚本
```

### 7.2 Skills 接入 Agent

```go
// 加载 Skills 仓库
repo, _ := skill.NewFSRepository("./skills")

fileAgent := llmagent.New("file-analyst",
    llmagent.WithModel(model),
    llmagent.WithInstruction(fileAnalystPrompt),
    llmagent.WithTools([]tool.Tool{fileAnalyzeTool, imageAnalyzeTool}),
    llmagent.WithSkills(repo),
    llmagent.WithSkillLoadMode(llmagent.SkillLoadModeTurn),
)
```

---

## 八、Agentic RAG 知识库（Self-RAG + CRAG + GraphRAG）

> **2026 核心升级**：从朴素 RAG 升级到 **Agentic RAG**——由 Agent（微调后的 Qwen3-8B 专家模型）自主决策"是否检索、检索什么、是否多跳、是否结束"，替代固定 retrieve-then-generate pipeline。同时引入 **GraphRAG**（知识图谱增强）处理跨文档多跳推理。

### 8.1 知识语料来源

| 文档来源 | RAG 知识类型 | 对应用户问题示例 |
|---------|-------------|----------------|
| 故障排查手册 | 故障诊断流程 | "gamesvr 启动失败怎么排查？" |
| 监控告警体系 | 告警阈值+处理方法 | "堆内存告警是什么意思？怎么处理？" |
| 运维 FAQ | 常见问题解答 | "Gradle 编译失败怎么办？" |
| 性能优化指南 | 性能调优方案 | "CPU 占用高如何定位？" |
| 微服务架构设计 | 架构理解 | "routesvr 和 proxysvr 的关系是什么？" |
| 补丁部署流程 | 部署操作指南 | "灰度发布怎么操作？" |
| **历史故障工单** | 真实故障案例（成为 GraphRAG 的实体节点） | "上次类似 OOM 是怎么解决的？" |

### 8.2 实现方式

```go
// knowledge/loader.go
// 使用 tRPC-Agent-Go 内置 Agentic RAG 链路
knowledge := knowledge.New(
    knowledge.WithEmbedder(bgeM3Embedder),         // BGE-M3：Dense + Sparse + ColBERT 三合一
    knowledge.WithVectorStore(milvusStore),        // Milvus 2.5（原生 Sparse Vector 支持）
    knowledge.WithReranker(bgeRerankerV2M3),       // BGE-Reranker-v2-M3 精排
    knowledge.WithChunkStrategy(knowledge.SemanticChunking{ // 语义切块（Markdown 标题感知）
        MaxChunkSize: 500,
        Overlap:      50,
        ParentChild:  true,   // 父子文档：检索子块、返回父块
    }),
    knowledge.WithAgenticMode(knowledge.AgenticConfig{
        Strategy:           "self-rag",     // self-rag / crag / graphrag
        RetrievalPlanner:   plannerModel,   // 用微调后的 Qwen3-8B 做检索规划
        MaxIterations:      3,              // 多跳检索上限
        EnableReflection:   true,           // 自我反思（CRAG 核心）
    }),
)

// 加载运维文档
knowledge.Load("./knowledge/docs/fault_diagnosis.md")
knowledge.Load("./knowledge/docs/monitoring_alerts.md")
knowledge.Load("./knowledge/docs/architecture.md")
knowledge.Load("./knowledge/docs/common_faq.md")
knowledge.Load("./knowledge/docs/performance_guide.md")
```

### 8.3 Agentic RAG 深入优化策略 ⭐⭐⭐ 面试重点

> 面试中 RAG 是考察最深入的领域之一，必须能讲清以下优化手段。

#### 8.3.1 Agentic RAG 三大范式（2026 必会）

| 范式 | 核心思想 | 实现 | GameOps 场景 |
|------|---------|------|------------|
| **Self-RAG** | 模型自己决定"是否检索 / 检索什么 / 是否够了" | 引入 retrieve / isrel / issup / isuse 四类特殊 Token | 高频运维问题模型直答，不触发检索（首 Token 延迟 -55%） |
| **CRAG** (Corrective RAG) | 检索后先评估相关性，不相关则重写 query 或网搜兜底 | Retrieval Evaluator + Query Rewriter + Web Search Fallback | 长尾问题命中率从 72% → 91% |
| **GraphRAG** | 构建知识图谱，支持跨文档多跳推理 | 实体抽取 + 关系抽取 + 图遍历 + 社区检测 | "上次类似 OOM 怎么解决" 需要跨工单+文档推理 |

```mermaid
graph TB
    Q["用户查询"] --> P["Retrieval Planner<br>(微调 Qwen3-8B)"]
    P -->|直答| D["Direct Answer"]
    P -->|需检索| R1["Hybrid Retrieval<br>(Dense+BM25+RRF)"]
    R1 --> E["Retrieval Evaluator<br>(CRAG)"]
    E -->|相关| G1["Graph Traversal<br>(GraphRAG)"]
    E -->|不相关| RW["Query Rewrite"]
    RW --> R1
    E -->|部分相关| WS["Web Search<br>兜底"]
    G1 --> Rerank["BGE-Reranker-v2-M3"]
    WS --> Rerank
    Rerank --> RF["Reflection<br>(Self-RAG)"]
    RF -->|信息足够| A["Generate Answer"]
    RF -->|需更多| R1
```

#### 8.3.2 切块策略优化

| 策略 | 说明 | GameOps 场景 |
|------|------|-------------|
| **固定大小切块** | 按字符数切分 | 基础方案，500 字 + 50 字重叠 |
| **语义切块** | 按段落/标题分割 | 运维文档天然按章节组织，效果好 |
| **递归字符切块** | 先按段落 → 句子 → 字符逐级递归 | 处理长段落时更精准 |
| **重叠窗口** | 相邻块之间有重叠区域 | 防止跨块信息丢失 |
| **父子文档** | 检索子块，返回父块 | 保留完整上下文，适合故障排查手册 |
| **Late Chunking** ⭐ | 先对整篇文档做 long-context embedding，再切块，保留全局语义 | 长文档中的指代消解场景（2026 新增最佳实践） |

**面试话术**：
> "我们的运维文档结构清晰（故障排查→监控告警→架构说明），所以采用语义切块（按 Markdown 标题分割），每块 500 字 + 50 字重叠。对于故障排查手册，使用父子文档策略：检索到相关的子步骤后，返回完整的排查流程。针对长故障工单里的指代问题（"这个问题"、"类似之前的"），额外使用 **Late Chunking**——先用 BGE-M3 对整篇工单做 long-context embedding，切块时每个 chunk 继承了整篇的语义，指代消解效果显著提升。"

#### 8.3.3 查询改写优化（集成到 Retrieval Planner）

```
用户原始查询 → Retrieval Planner 判断是否需要改写 → 改写后查询 → 检索
```

| 方法 | 说明 | 示例 |
|------|------|------|
| **HyDE** | 先让 LLM 生成一个假设性答案，用答案做检索 | "gamesvr重启" → LLM 先生成答案 → 用答案向量检索 |
| **Multi-Query** | 把用户问题改写成多个角度 | "重启原因" → ["内存不足导致重启", "OOM Kill", "健康检查失败"] |
| **Step-back** | 从具体问题退一步问更通用的问题 | "gamesvr凌晨3点重启" → "游戏服务器常见重启原因有哪些" |
| **Query Decomposition** | 将复杂问题拆解为子问题 | "对比上周和这周的CPU" → ["上周CPU数据", "这周CPU数据"] |
| **Router-Query Rewrite** ⭐ | 由 Qwen3-0.6B 本地 Router 决定改写策略（省云端 API 成本） | 简单问题→HyDE；对比类→Decomposition；时序类→Multi-Query |

#### 8.3.4 混合检索 + RRF 融合

```mermaid
graph LR
    Q["用户查询"] --> D["Dense 检索<br>(BGE-M3 向量相似度)"]
    Q --> S["Sparse 检索<br>(BGE-M3 Sparse / BM25)"]
    Q --> C["ColBERT 多向量<br>(BGE-M3 Late Interaction)"]
    D --> RRF["RRF 融合<br>(Reciprocal Rank Fusion)"]
    S --> RRF
    C --> RRF
    RRF --> R["重排序<br>(BGE-Reranker-v2-M3)"]
    R --> T["Top-K 结果"]
```

```python
# RRF 融合算法（面试必会）
def rrf_fusion(*results_lists, k=60):
    """Reciprocal Rank Fusion - 融合多路检索结果"""
    scores = {}
    for results in results_lists:
        for rank, doc in enumerate(results):
            scores[doc.id] = scores.get(doc.id, 0) + 1 / (k + rank + 1)
    return sorted(scores.items(), key=lambda x: x[1], reverse=True)
```

**面试话术**：
> "我们用 **BGE-M3 三合一混合检索**——同一个模型同时产出 Dense / Sparse / ColBERT 三种向量。Dense 捕捉语义相似度（"服务器挂了" ≈ "进程崩溃"），Sparse 捕捉精确匹配（"gamesvr"、"OOM" 等专业术语和缩写），ColBERT Late Interaction 捕捉细粒度词级对齐（处理长文档中的局部匹配）。三路 RRF 融合后再过 **BGE-Reranker-v2-M3** 精排，相比单路 Dense 检索，Context Recall 从 0.72 提升到 0.91，Context Precision 从 0.65 提升到 0.88。"

#### 8.3.5 GraphRAG ⭐⭐（跨文档多跳推理）

> **场景**：运维问题经常需要跨多个文档推理——"当前 gamesvr OOM"→需要查询"之前类似故障 / 关联服务 / 历史补丁 / 同期告警"。朴素 RAG 在这种多跳场景下召回率不足 50%。

```mermaid
graph TB
    subgraph "知识图谱构建"
        Docs["原始文档"] --> Extract["实体+关系抽取<br>(DeepSeek-V3.2)"]
        Extract --> KG["Neo4j 知识图谱"]
        KG --> Comm["Leiden 社区检测<br>+ 社区摘要"]
    end

    subgraph "查询时"
        Q["查询"] --> L["Local Search<br>(实体邻域子图)"]
        Q --> G["Global Search<br>(社区摘要聚合)"]
        L --> Ans["Final Answer"]
        G --> Ans
    end
```

```go
// knowledge/graphrag.go
graphRAG := graphrag.New(
    graphrag.WithEntityExtractor(deepSeekV32),     // DeepSeek-V3.2 抽取实体+关系
    graphrag.WithGraphStore(neo4jStore),           // Neo4j 存储图谱
    graphrag.WithCommunityDetector(graphrag.Leiden{Resolution: 1.0}),
    graphrag.WithSearchModes(
        graphrag.LocalSearch{Hops: 2},             // 局部搜索（2 跳邻居）
        graphrag.GlobalSearch{TopCommunities: 5},  // 全局搜索（Top-5 社区摘要）
    ),
)
```

#### 8.3.6 重排序（Rerank）

| 方法 | 精度 | 速度 | 适用场景 |
|------|------|------|---------|
| **BGE-Reranker-v2-M3** ⭐ | ⭐⭐⭐⭐⭐ | 中 | **2026 中文 SOTA**，Top-20 → Top-5 精排首选 |
| **Cross-Encoder Rerank** | ⭐⭐⭐⭐⭐ | 慢（逐对计算） | 传统方案 |
| **LLM Rerank** (DeepSeek-V3.2) | ⭐⭐⭐⭐ | 更慢 | 复杂场景，需要理解上下文 |
| **Cohere Rerank v3.5 API** | ⭐⭐⭐⭐ | 中（API 调用） | 快速集成，无内网部署 |
| **RankGPT (Listwise)** | ⭐⭐⭐⭐⭐ | 慢 | 最高精度，列表级 Pairwise 比较 |

#### 8.3.7 Agentic RAG 评测体系（RAGAS + Agent-specific 指标）

| 指标 | 含义 | 计算方式 |
|------|------|---------|
| **Faithfulness** | 生成答案是否忠于上下文 | 检查答案中的声明是否都能在上下文中找到依据 |
| **Answer Relevancy** | 答案与问题的相关度 | 反向生成问题，比较与原始问题的相似度 |
| **Context Precision** | 检索上下文的精确度 | 相关文档排在前面的比例 |
| **Context Recall** | 检索上下文的召回率 | 参考答案中的要点是否都被上下文覆盖 |
| **Noise Sensitivity** ⭐ | 对无关文档的抗噪声能力 | 注入干扰文档后答案质量下降程度（2026 新增） |
| **Retrieval Decision Accuracy** ⭐ | Agentic RAG 专属：检索决策正确率 | 该检索而没检索 / 不该检索却检索 的比例 |
| **Multi-Hop Correctness** ⭐ | GraphRAG 专属：多跳推理正确率 | 多跳路径上的每一跳是否正确 |

```python
# RAGAS 0.2+ 评测示例（2026 主流）
from ragas import evaluate
from ragas.metrics import (
    faithfulness, answer_relevancy, context_precision, context_recall,
    noise_sensitivity_relevant,  # 2026 新增
)
from ragas.metrics._aspect_critic import AspectCritic

# 自定义 Agent-specific 指标
retrieval_decision = AspectCritic(
    name="retrieval_decision",
    definition="Agent 是否正确判断了是否需要检索（必要时检索，不必要时直答）"
)

results = evaluate(
    dataset=test_dataset,
    metrics=[faithfulness, answer_relevancy, context_precision, context_recall,
             noise_sensitivity_relevant, retrieval_decision],
)
print(results)
# {'faithfulness': 0.91, 'answer_relevancy': 0.93, 'context_precision': 0.88,
#  'context_recall': 0.91, 'noise_sensitivity_relevant': 0.85,
#  'retrieval_decision': 0.89}
```

---

## 九、Multi-Agent 协作设计

### 9.1 Agent 职责分工

| Agent | 角色 | 挂载工具 | 职责 |
|-------|------|---------|------|
| **Coordinator** | 协调者 | 无（只做路由） | 意图识别 → Transfer 到子 Agent |
| **DiagnosisAgent** | 故障诊断专家 | 蓝鲸 MCP (指标/日志/告警/事件/链路) + BCS MCP (容器状态) | 查询监控数据+容器状态、分析故障原因 |
| **KnowledgeAgent** | 知识问答专家 | RAG 检索 | 检索运维文档回答知识类问题 |
| **FileAnalystAgent** | 文件分析专家 | file_analyze + analyze_image + Skills | 解析上传文件和图片 |
| **RepairAgent** | 自动修复专家 | 工蜂Git×4 + 蓝盾MCP + TAPD MCP | 自动修复→提交→编译→更新工单 |

### 9.2 核心代码（借鉴 oncall_agent 的生产级 Agent 创建模式）

> 以下代码深度参考 oncall_agent 的 Agent 创建模式：`//go:embed` 嵌入默认 Prompt + 配置中心运行时覆盖 + BeforeModel 回调链 + 工具回调日志 + Agent-as-Tool 封装。

```go
// ========== agent/coordinator.go ==========
package agent

import (
    "context"
    _ "embed"

    "trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
    "trpc.group/trpc-go/trpc-agent-go/model"
    "trpc.group/trpc-go/trpc-agent-go/model/openai"
    "trpc.group/trpc-go/trpc-agent-go/tool"
)

//go:embed prompt/coordinator.txt
var coordinatorPromptTemplate string

// NewCoordinator 创建协调者 Agent（借鉴 oncall_agent 的 magic_oncall_agent 创建模式）
func NewCoordinator(
    modelInstance *openai.Model,
    configCli ConfigAPI,
    diagnosisAgent, knowledgeAgent, fileAnalystAgent, repairAgent agent.Agent,
) (agent.Agent, error) {
    const name = "coordinator"
    desc := "GameOps 智能运维协调者，负责意图识别和路由分发"

    // 工具回调：记录所有工具调用日志（借鉴 oncall_agent）
    toolCallBack := tool.NewCallbacks().
        RegisterAfterTool(func(ctx context.Context, toolName string, toolDeclaration *tool.Declaration,
            jsonArgs []byte, result any, runErr error) (any, error) {
            log.InfoContextf(ctx, "[coordinator]tool %s, args %s, result %v, err %v",
                toolName, string(jsonArgs), utils.MustToJSON(result), runErr)
            return result, runErr
        })

    // 模型回调：注入系统上下文（当前时间、环境等）
    modelCallbacks := model.NewCallbacks().
        RegisterBeforeModel(FillSystemContextInfo)

    // 配置中心覆盖（支持运行时热更新 Prompt，无需重新部署）
    agentConfig := configCli.GetAgentConfig(name)
    if agentConfig != nil {
        if agentConfig.SystemPrompt != "" {
            coordinatorPromptTemplate = agentConfig.SystemPrompt
        }
        if agentConfig.Desc != "" {
            desc = agentConfig.Desc
        }
    }

    return llmagent.New(name,
        llmagent.WithModel(modelInstance),
        llmagent.WithModelCallbacks(modelCallbacks),
        llmagent.WithToolCallbacks(toolCallBack),
        llmagent.WithPlanner(NewReactPlanner()), // 自定义中文化 ReAct Planner
        llmagent.WithGenerationConfig(ConstructGenConfig()),
        llmagent.WithInstruction(coordinatorPromptTemplate),
        llmagent.WithDescription(desc),
        llmagent.WithSubAgents(diagnosisAgent, knowledgeAgent, fileAnalystAgent, repairAgent),
    ), nil
}

// ========== agent/diagnosis.go ==========
//go:embed prompt/diagnosis.txt
var diagnosisPromptTemplate string

// NewDiagnosisAgent 创建故障诊断 Agent（借鉴 span_analysis_agent 的动态工具过滤模式）
func NewDiagnosisAgent(
    modelInstance *openai.Model,
    mcpTool MCPToolAPI,
    configCli ConfigAPI,
) (agent.Agent, error) {
    const name = "diagnosis_agent"
    desc := "游戏服务器故障诊断专家，通过蓝鲸监控MCP+BCS容器MCP查询数据并分析故障原因"

    // 加载蓝鲸监控 + BCS 容器相关的 MCP 工具集
    var toolSets []tool.ToolSet
    for _, target := range []string{"bk-monitor", "bcs", "*"} {
        for _, mcpName := range mcpTool.GetMCPListByTarget(target) {
            toolSets = append(toolSets, mcpTool.GetMCPToolsByName(mcpName))
        }
    }

    toolCallBack := tool.NewCallbacks().
        RegisterAfterTool(func(ctx context.Context, toolName string, toolDeclaration *tool.Declaration,
            jsonArgs []byte, result any, runErr error) (any, error) {
            log.InfoContextf(ctx, "[diagnosis_agent]tool %s, args %s, err %v",
                toolName, string(jsonArgs), runErr)
            return result, runErr
        })

    modelCallbacks := model.NewCallbacks().
        RegisterBeforeModel(FillSystemContextInfo)

    // 配置中心运行时覆盖
    agentConfig := configCli.GetAgentConfig(name)
    if agentConfig != nil {
        if agentConfig.SystemPrompt != "" {
            diagnosisPromptTemplate = agentConfig.SystemPrompt
        }
        if agentConfig.Desc != "" {
            desc = agentConfig.Desc
        }
    }

    return llmagent.New(name,
        llmagent.WithModel(modelInstance),
        llmagent.WithModelCallbacks(modelCallbacks),
        llmagent.WithToolSets(toolSets),
        llmagent.WithToolCallbacks(toolCallBack),
        llmagent.WithPlanner(NewReactPlanner()),
        llmagent.WithGenerationConfig(ConstructGenConfig()),
        llmagent.WithInstruction(diagnosisPromptTemplate),
        llmagent.WithDescription(desc),
    ), nil
}

// ========== agent/knowledge.go ==========
//go:embed prompt/knowledge.txt
var knowledgePromptTemplate string

func NewKnowledgeAgent(modelInstance *openai.Model, ragKnowledge knowledge.Knowledge, configCli ConfigAPI) (agent.Agent, error) {
    const name = "knowledge_agent"
    desc := "LetsGo游戏服务器运维知识专家，基于运维文档和FAQ回答问题"

    modelCallbacks := model.NewCallbacks().
        RegisterBeforeModel(FillSystemContextInfo)

    agentConfig := configCli.GetAgentConfig(name)
    if agentConfig != nil {
        if agentConfig.SystemPrompt != "" {
            knowledgePromptTemplate = agentConfig.SystemPrompt
        }
        if agentConfig.Desc != "" {
            desc = agentConfig.Desc
        }
    }

    return llmagent.New(name,
        llmagent.WithModel(modelInstance),
        llmagent.WithModelCallbacks(modelCallbacks),
        llmagent.WithPlanner(NewReactPlanner()),
        llmagent.WithGenerationConfig(ConstructGenConfig()),
        llmagent.WithInstruction(knowledgePromptTemplate),
        llmagent.WithDescription(desc),
        llmagent.WithKnowledge(ragKnowledge),
    ), nil
}

// ========== agent/file_analyst.go ==========
//go:embed prompt/file_analyst.txt
var fileAnalystPromptTemplate string

func NewFileAnalystAgent(modelInstance *openai.Model, skillRepo *skill.FSRepository, configCli ConfigAPI) (agent.Agent, error) {
    const name = "file_analyst"
    desc := "文件分析专家，能解析用户上传的CSV、日志、配置文件和监控截图"

    agentConfig := configCli.GetAgentConfig(name)
    if agentConfig != nil {
        if agentConfig.SystemPrompt != "" {
            fileAnalystPromptTemplate = agentConfig.SystemPrompt
        }
        if agentConfig.Desc != "" {
            desc = agentConfig.Desc
        }
    }

    return llmagent.New(name,
        llmagent.WithModel(modelInstance),
        llmagent.WithInstruction(fileAnalystPromptTemplate),
        llmagent.WithDescription(desc),
        llmagent.WithTools([]tool.Tool{fileAnalyzeTool, imageAnalyzeTool}),
        llmagent.WithSkills(skillRepo),
        llmagent.WithSkillLoadMode(llmagent.SkillLoadModeTurn),
        llmagent.WithPlanner(NewReactPlanner()),
        llmagent.WithGenerationConfig(ConstructGenConfig()),
    ), nil
}

// ========== agent/repair.go ==========
//go:embed prompt/repair.txt
var repairPromptTemplate string

// NewRepairAgent 创建自动修复 Agent（借鉴 oncall_agent 的 Agent-as-Tool 模式：
// DiagnosisAgent 的诊断结论可作为 Tool 被 RepairAgent 调用）
func NewRepairAgent(
    modelInstance *openai.Model,
    mcpTool MCPToolAPI,
    configCli ConfigAPI,
) (agent.Agent, error) {
    const name = "repair_agent"
    desc := "游戏服务器问题自动修复专家，执行修复闭环（Git提交→编译验证→工单更新）"

    // 加载修复相关的 MCP 工具集（工蜂 + 蓝盾 + TAPD）
    var toolSets []tool.ToolSet
    for _, target := range []string{"gongfeng", "devops", "tapd", "*"} {
        for _, mcpName := range mcpTool.GetMCPListByTarget(target) {
            toolSets = append(toolSets, mcpTool.GetMCPToolsByName(mcpName))
        }
    }

    // 自建 FunctionTool（工蜂 Git REST API 封装）
    localTools := []tool.Tool{
        createBranchTool, commitFileTool, createMRTool, getFileTool,
    }

    toolCallBack := tool.NewCallbacks().
        RegisterAfterTool(func(ctx context.Context, toolName string, toolDeclaration *tool.Declaration,
            jsonArgs []byte, result any, runErr error) (any, error) {
            log.InfoContextf(ctx, "[repair_agent]tool %s, args %s, err %v",
                toolName, string(jsonArgs), runErr)
            return result, runErr
        })

    modelCallbacks := model.NewCallbacks().
        RegisterBeforeModel(FillSystemContextInfo)

    agentConfig := configCli.GetAgentConfig(name)
    if agentConfig != nil {
        if agentConfig.SystemPrompt != "" {
            repairPromptTemplate = agentConfig.SystemPrompt
        }
        if agentConfig.Desc != "" {
            desc = agentConfig.Desc
        }
    }

    return llmagent.New(name,
        llmagent.WithModel(modelInstance),
        llmagent.WithModelCallbacks(modelCallbacks),
        llmagent.WithTools(localTools),
        llmagent.WithToolSets(toolSets),
        llmagent.WithToolCallbacks(toolCallBack),
        llmagent.WithPlanner(NewReactPlanner()),
        llmagent.WithGenerationConfig(ConstructGenConfig()),
        llmagent.WithInstruction(repairPromptTemplate),
        llmagent.WithDescription(desc),
    ), nil
}

// ========== agent/common.go（直接复用 oncall_agent） ==========
const contextTemplate = `## 上下文信息
- 当前日期: {{date}}
- 当前时间戳(ms): {{currentTimestamp}}
- 今日开始时间戳(ms): {{startOfTodayTimestamp}}`

// FillSystemContextInfo 注入系统上下文信息到 System Prompt（直接复用 oncall_agent）
var FillSystemContextInfo = func(ctx context.Context, req *model.Request) (*model.Response, error) {
    for i, msg := range req.Messages {
        if msg.Role == model.RoleSystem {
            req.Messages[i].Content += utils.RenderString(contextTemplate, map[string]interface{}{
                "date":                  time.Now().Format("2006-01-02 15:04:05"),
                "currentTimestamp":      carbon.Now(carbon.Shanghai).TimestampMilli(),
                "startOfTodayTimestamp": carbon.Now(carbon.Shanghai).StartOfDay().TimestampMilli(),
            })
        }
    }
    return nil, nil
}

// ConstructGenConfig 构建模型生成参数（复用 oncall_agent）
func ConstructGenConfig() model.GenerationConfig {
    genConfig := model.GenerationConfig{Stream: true}
    config := rainbow.GetCfg()
    if config.Temperature != 0 {
        temp := config.Temperature
        genConfig.Temperature = &temp
    }
    if config.MaxTokens != 0 {
        maxTokens := config.MaxTokens
        genConfig.MaxTokens = &maxTokens
    }
    return genConfig
}

// ========== agent/react.go（直接复用 oncall_agent 的中文化 ReAct Planner） ==========
// 自定义 ReAct 标签，中文化输出更友好
const (
    PlanningTag    = "***规划***"
    ReplanningTag  = "***重新规划***"
    ReasoningTag   = "***推理***"
    ActionTag      = "***行动***"
    FinalAnswerTag = "***最终答案***"
)

// NewReactPlanner 创建中文化 ReAct 规划器（直接复用 oncall_agent/src/agents/react.go）
func NewReactPlanner() *Planner { return &Planner{} }
// Planner 实现省略，直接从 oncall_agent 复制 react.go 即可
```
```

### 9.3 Runner + 服务化（借鉴 oncall_agent 的 main.go 多服务注册模式）

> 以下代码借鉴 oncall_agent 的 `main.go`：多服务并行注册（A2A + SSE + AGUI）、Session 管理 + 自动总结、请求过滤器全链路日志。

```go
// main.go — 借鉴 oncall_agent 的生产级启动模式
package main

import (
    "context"
    "fmt"
    "time"

    a2aserver "trpc.group/trpc-go/trpc-a2a-go/server"
    "trpc.group/trpc-go/trpc-agent-go/model/openai"
    sagui "trpc.group/trpc-go/trpc-agent-go/server/agui"
    "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
    "trpc.group/trpc-go/trpc-agent-go/session/summary"

    trpc "git.code.oa.com/trpc-go/trpc-go"
    thttp "git.code.oa.com/trpc-go/trpc-go/http"
    "git.code.oa.com/trpc-go/trpc-go/log"
    "git.code.oa.com/trpc-go/trpc-go/server"
    a2atrpc "git.woa.com/trpc-go/trpc-a2a-go/trpc"
    tagui "git.woa.com/trpc-go/trpc-agent-go/trpc/agui"

    // 本项目各模块
    "gameops-agent/agent"
    "gameops-agent/mcp"
    "gameops-agent/config"
    "gameops-agent/services/a2a"
    "gameops-agent/services/agui"
    "gameops-agent/services/sse"
)

const (
    a2aServiceName  = "trpc.gameops.agent.a2a"
    sseServiceName  = "trpc.gameops.agent.sse"
    aguiServiceName = "trpc.gameops.agent.agui"
)

func main() {
    s := trpc.NewServer(server.WithFilter(reqFilter))

    // 初始化配置（Rainbow 运行时配置 + 配置中心 Agent/工具配置）
    if err := config.Init(); err != nil {
        log.Fatalf("config.Init failed: %v", err)
    }

    a2aServer, sseServer, aguiServer, err := initServers()
    if err != nil {
        log.Fatalf("initServers failed: %v", err)
    }

    // 注册 A2A 服务（支持被其他 Agent 通过 A2A 协议调用）
    if err := a2atrpc.RegisterA2AServer(s, a2aServiceName, a2aServer); err != nil {
        log.Fatalf("RegisterA2AServer failed: %v", err)
    }

    // 注册 SSE 服务（支持企微机器人/自研 UI 接入）
    thttp.HandleFunc("/v1/agent", sseServer.HandleSSE)
    thttp.RegisterNoProtocolService(s.Service(sseServiceName))

    // 注册 AG-UI 服务（自带 Web 前端，零前端开发成本）
    if err := tagui.RegisterAGUIServer(s, aguiServiceName, aguiServer); err != nil {
        log.Fatalf("RegisterAGUIServer failed: %v", err)
    }

    // 文件上传接口
    thttp.HandleFunc("/upload", handleUpload)

    if err := s.Serve(); err != nil {
        log.Fatalf("Server failed: %v", err)
    }
}

// reqFilter 请求过滤器：全链路日志追踪（直接复用 oncall_agent）
func reqFilter(ctx context.Context, req interface{}, next filter.ServerHandleFunc) (interface{}, error) {
    msg := trpc.Message(ctx)
    service, env, method := msg.CalleeServiceName(), trpc.GlobalConfig().Global.EnvName, msg.CalleeMethod()
    log.WithContextFields(ctx, "service", service, "method", method, "env", env)
    start := time.Now()
    rsp, err := next(ctx, req)
    cost := fmt.Sprintf("%d", time.Since(start))
    log.WithContextFields(ctx, "cost_time", cost)
    if err == nil {
        log.InfoContextf(ctx, "Leaving: %s", method)
    } else {
        log.ErrorContextf(ctx, "Leaving: %s with error: %v", method, err)
    }
    return rsp, err
}

func initServers() (*a2aserver.A2AServer, sse.API, *sagui.Server, error) {
    cfg := config.GetCfg()

    // 初始化大模型客户端
    modelInstance := openai.New(cfg.OpenAIModelName,
        openai.WithBaseURL(cfg.OpenAIBaseURL),
        openai.WithAPIKey(cfg.OpenAIAPIKey),
    )

    // 初始化配置中心客户端（管理 Agent Prompt + MCP 工具配置）
    configCli, err := config.NewConfigClient()
    if err != nil {
        return nil, nil, nil, err
    }

    // 初始化 MCP 工具管理器（借鉴 oncall_agent 的动态加载机制）
    mcpTool, err := mcp.NewMCPToolImpl(context.Background(), configCli)
    if err != nil {
        return nil, nil, nil, err
    }

    // 初始化各子 Agent
    diagnosisAgent, err := agent.NewDiagnosisAgent(modelInstance, mcpTool, configCli)
    if err != nil {
        return nil, nil, nil, err
    }
    knowledgeAgent, err := agent.NewKnowledgeAgent(modelInstance, ragKnowledge, configCli)
    if err != nil {
        return nil, nil, nil, err
    }
    fileAnalystAgent, err := agent.NewFileAnalystAgent(modelInstance, skillRepo, configCli)
    if err != nil {
        return nil, nil, nil, err
    }
    repairAgent, err := agent.NewRepairAgent(modelInstance, mcpTool, configCli)
    if err != nil {
        return nil, nil, nil, err
    }

    // 创建协调者 Agent
    coordinator, err := agent.NewCoordinator(modelInstance, configCli,
        diagnosisAgent, knowledgeAgent, fileAnalystAgent, repairAgent)
    if err != nil {
        return nil, nil, nil, err
    }

    // 对话过长时自动总结（借鉴 oncall_agent）
    summarizer := summary.NewSummarizer(modelInstance,
        summary.WithTokenThreshold(cfg.SummarizeTokenThreshold))

    // Session 服务：内存存储 + 定时清理 + 自动总结（借鉴 oncall_agent）
    sessionService := inmemory.NewSessionService(
        inmemory.WithCleanupInterval(30*time.Minute),
        inmemory.WithSummarizer(summarizer),
    )

    // 初始化各服务端点
    a2aServer, err := a2a.NewA2AServer(a2aServiceName, coordinator, sessionService)
    if err != nil {
        return nil, nil, nil, err
    }
    aguiServer, err := agui.New(coordinator, sessionService)
    if err != nil {
        return nil, nil, nil, err
    }
    sseServer := sse.NewSSEService(sessionService, coordinator, "gameops_agent")

    return a2aServer, sseServer, aguiServer, nil
}
```

### 9.3.1 SSE 流式服务层（复用 oncall_agent 的流式事件处理）

```go
// services/sse/sse_impl.go — 核心流式响应处理（借鉴 oncall_agent）
func (s *sseServiceImpl) HandleSSE(w http.ResponseWriter, r *http.Request) error {
    ctx := r.Context()
    var reqBody SSERequest
    bodyBytes, _ := io.ReadAll(r.Body)
    json.Unmarshal(bodyBytes, &reqBody)

    // SSE 协议头
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    w.Header().Set("Access-Control-Allow-Origin", "*")

    flusher, _ := w.(http.Flusher)

    // 通过 Runner 执行 Agent
    eventChan, err := s.agentRunner.Run(ctx, reqBody.GetUserID(), reqBody.GetSessionID(),
        model.NewUserMessage(reqBody.Content))
    if err != nil {
        return err
    }
    return s.processStreamingResponse(ctx, w, eventChan)
}

// processStreamingResponse 处理流式事件：工具调用可视化 + 流式输出（借鉴 oncall_agent）
func (s *sseServiceImpl) processStreamingResponse(ctx context.Context, w http.ResponseWriter, eventChan <-chan *event.Event) error {
    flusher, _ := w.(http.Flusher)
    for event := range eventChan {
        // 工具调用开始：输出 "开始执行工具: xxx"
        if len(event.Choices) > 0 && len(event.Choices[0].Message.ToolCalls) > 0 {
            for _, toolCall := range event.Choices[0].Message.ToolCalls {
                resp := fmt.Sprintf("\n*开始执行工具: %s*\n", toolCall.Function.Name)
                w.Write([]byte(wrapSSEResponse(resp, false)))
                flusher.Flush()
            }
        }
        // 流式内容输出
        if len(event.Choices) > 0 && event.Choices[0].Delta.Content != "" {
            w.Write([]byte(wrapSSEResponse(event.Choices[0].Delta.Content, false)))
            flusher.Flush()
        }
        // 完成事件
        if event.Done && !isToolEvent(event) && event.Author == s.entranceAgent.Info().Name {
            w.Write([]byte(wrapSSEResponse("", true)))
            flusher.Flush()
            break
        }
    }
    return nil
}
```
```

### 9.4 Graph 图编排诊断流程（进阶亮点）⭐⭐ 高区分度

> **面试价值**：Graph 图编排对标 LangGraph 的 StateGraph，是展示"复杂工作流能力"的高区分度亮点。

当故障诊断流程需要条件分支、并行查询、循环重试时，可以用 StateGraph 替代简单的 LLMAgent：

```mermaid
graph TD
    START["用户提问"] --> INTENT["意图识别"]
    INTENT -->|故障诊断| CHECK_ALARM["查告警/事件"]
    INTENT -->|知识问答| KNOWLEDGE["RAG 检索"]
    INTENT -->|文件分析| FILE["文件解析"]
    
    CHECK_ALARM --> HAS_ALARM{"有告警？"}
    HAS_ALARM -->|是| PARALLEL["并行查询"]
    HAS_ALARM -->|否| QUERY_LOG["查日志"]
    
    PARALLEL --> QUERY_METRICS["查指标"]
    PARALLEL --> QUERY_LOG
    PARALLEL --> QUERY_TRACE["查链路"]
    
    QUERY_METRICS --> ANALYZE["综合分析"]
    QUERY_LOG --> ANALYZE
    QUERY_TRACE --> ANALYZE
    
    ANALYZE --> NEED_MORE{"需要更多数据？"}
    NEED_MORE -->|是| CHECK_ALARM
    NEED_MORE -->|否| REPORT["生成诊断报告"]
    
    KNOWLEDGE --> REPORT
    FILE --> REPORT
    REPORT --> END["返回用户"]
```

```go
// agent/diagnosis_graph.go — 用 StateGraph 编排诊断流程
import (
    "trpc.group/trpc-go/trpc-agent-go/graph"
)

// 定义诊断状态
type DiagnosisState struct {
    Query       string   `json:"query"`        // 用户问题
    Alarms      []Alarm  `json:"alarms"`       // 告警数据
    Metrics     []Metric `json:"metrics"`      // 指标数据
    Logs        []Log    `json:"logs"`         // 日志数据
    Traces      []Trace  `json:"traces"`       // 链路数据
    Analysis    string   `json:"analysis"`     // 分析结论
    NeedMore    bool     `json:"need_more"`    // 是否需要更多数据
    RetryCount  int      `json:"retry_count"`  // 重试次数
}

func buildDiagnosisGraph() *graph.StateGraph {
    sg := graph.NewStateGraph(DiagnosisState{})
    
    // 添加节点
    sg.AddNode("check_alarm", checkAlarmNode)   // 查告警和事件
    sg.AddNode("query_metrics", queryMetricsNode) // 查指标（并行）
    sg.AddNode("query_logs", queryLogsNode)       // 查日志（并行）
    sg.AddNode("query_traces", queryTracesNode)   // 查链路（并行）
    sg.AddNode("analyze", analyzeNode)            // LLM 综合分析
    sg.AddNode("report", reportNode)              // 生成诊断报告
    
    // 编排边
    sg.AddEdge(graph.START, "check_alarm")
    
    // 条件边：根据是否有告警决定下一步
    sg.AddConditionalEdge("check_alarm", func(state DiagnosisState) string {
        if len(state.Alarms) > 0 {
            return "query_metrics" // 有告警 → 并行查指标
        }
        return "query_logs" // 无告警 → 直接查日志
    })
    
    // 并行查询后汇聚到分析节点
    sg.AddEdge("query_metrics", "analyze")
    sg.AddEdge("query_logs", "analyze")
    sg.AddEdge("query_traces", "analyze")
    
    // 分析后条件判断：是否需要更多数据
    sg.AddConditionalEdge("analyze", func(state DiagnosisState) string {
        if state.NeedMore && state.RetryCount < 3 {
            return "check_alarm" // 循环重试
        }
        return "report" // 生成报告
    })
    
    sg.AddEdge("report", graph.END)
    
    return sg
}
```

**面试高频问题**：
- "StateGraph 和 LangGraph 的区别？" → Go 实现，Channel 事件流，支持节点重试/缓存，Pregel/BSP 执行模型
- "为什么用 Graph 而不是简单的 LLMAgent？" → 故障诊断有条件分支（有告警 vs 无告警）、并行查询（指标+日志+链路同时查）、循环重试（数据不够再查），Graph 编排更精确可控
- "如何实现 Human-in-the-Loop？" → Graph 中断节点 + 状态持久化 + Resume，比如危险操作前暂停等待人工确认

### 9.4.1 RepairAgent Graph 编排（修复闭环流程）

```mermaid
graph TD
    START["接收诊断结果"] --> FETCH_SOURCE["获取源码<br>git_get_file_content"]
    FETCH_SOURCE --> GEN_FIX["AI生成修复代码"]
    GEN_FIX --> CREATE_BRANCH["创建修复分支<br>git_create_branch"]
    CREATE_BRANCH --> BRANCH_OK{"分支创建成功？"}
    BRANCH_OK -->|否| FAIL["输出错误，停止流程"]
    BRANCH_OK -->|是| COMMIT["提交修复代码<br>git_commit_file"]
    COMMIT --> COMMIT_OK{"提交成功？"}
    COMMIT_OK -->|否| FAIL
    COMMIT_OK -->|是| CREATE_MR["创建MR<br>git_create_merge_request"]
    CREATE_MR --> MR_OK{"MR创建成功？"}
    MR_OK -->|否| FAIL
    MR_OK -->|是| TRIGGER_BUILD["触发蓝盾编译<br>v4_user_build_start"]
    TRIGGER_BUILD --> POLL_STATUS["轮询编译状态<br>v4_user_build_status"]
    POLL_STATUS --> BUILD_DONE{"编译结果？"}
    BUILD_DONE -->|SUCCEED| CHECK_BCS["BCS容器检查<br>(可选)"]
    BUILD_DONE -->|FAILED| BUILD_FAIL["编译失败报告"]
    BUILD_DONE -->|TIMEOUT| BUILD_FAIL
    CHECK_BCS --> UPDATE_TAPD["更新TAPD单<br>bugs_update"]
    BUILD_FAIL --> UPDATE_TAPD
    UPDATE_TAPD --> REPORT["生成完整修复报告"]
    REPORT --> END["推送报告<br>等待人工确认"]
```

```go
// agent/repair_graph.go — 用 StateGraph 编排修复流程
type RepairState struct {
    DiagnosisResult  string `json:"diagnosis_result"`
    SourceCode       string `json:"source_code"`
    FixCode          string `json:"fix_code"`
    BranchName       string `json:"branch_name"`
    CommitSHA        string `json:"commit_sha"`
    MRURL            string `json:"mr_url"`
    BuildStatus      string `json:"build_status"`
    TAPDBugID        string `json:"tapd_bug_id"`
    Report           string `json:"report"`
}

func buildRepairGraph() *graph.StateGraph {
    sg := graph.NewStateGraph(RepairState{})
    
    sg.AddNode("fetch_source", fetchSourceNode)     // 获取源码
    sg.AddNode("gen_fix", genFixNode)               // AI生成修复代码
    sg.AddNode("create_branch", createBranchNode)   // 创建修复分支
    sg.AddNode("commit_code", commitCodeNode)       // 提交修复代码
    sg.AddNode("create_mr", createMRNode)           // 创建MR
    sg.AddNode("trigger_build", triggerBuildNode)   // 触发蓝盾编译
    sg.AddNode("update_tapd", updateTAPDNode)       // 更新TAPD单
    sg.AddNode("gen_report", genReportNode)         // 生成修复报告
    
    sg.AddEdge(graph.START, "fetch_source")
    sg.AddEdge("fetch_source", "gen_fix")
    sg.AddEdge("gen_fix", "create_branch")
    
    // 严格门禁：任一Git步骤失败则停止
    sg.AddConditionalEdge("create_branch", func(s RepairState) string {
        if s.BranchName == "" { return graph.END }  // 失败停止
        return "commit_code"
    })
    sg.AddConditionalEdge("commit_code", func(s RepairState) string {
        if s.CommitSHA == "" { return graph.END }
        return "create_mr"
    })
    sg.AddConditionalEdge("create_mr", func(s RepairState) string {
        if s.MRURL == "" { return graph.END }
        return "trigger_build"
    })
    
    sg.AddEdge("trigger_build", "update_tapd")
    sg.AddEdge("update_tapd", "gen_report")
    sg.AddEdge("gen_report", graph.END)
    
    return sg
}
```

---

## 9.5 Agent 评估体系 ⭐⭐⭐ 面试必考

> **对应面试**：如何评估 Agent 的性能（必考 TOP 3）

### 9.5.1 三层评估体系

```
GameOps Agent 评估三层体系：
├── 单步评估
│   ├── Tool Call 准确率：Coordinator 是否正确路由到目标 Agent
│   ├── MCP 调用参数正确率：指标名/时间范围/服务名是否正确
│   └── RAG 检索相关度：RAGAS 四大指标
│
├── 流程评估
│   ├── 诊断步骤合理性：是否按照 告警→指标→日志 的顺序排查
│   ├── 工具调用效率：完成诊断平均调用几次工具
│   └── 回退/重试次数：是否出现无效循环
│
└── 端到端评估
    ├── 任务完成率：能否正确诊断出根因
    ├── 响应时间：从提问到给出诊断报告的总耗时
    ├── 用户满意度：人工评分 1-5 分
    └── Token 成本：每次诊断消耗的 Token 数量
```

### 9.5.2 评估数据集构造

```python
# eval/build_testset.py
"""
构造 GameOps Agent 评估数据集
基于历史真实故障案例 + 人工标注的标准答案
"""

test_cases = [
    {
        "id": "case_001",
        "query": "昨晚凌晨3点gamesvr重启了3次，什么原因？",
        "expected_route": "diagnosis_agent",        # 期望路由
        "expected_tools": [                         # 期望调用的工具
            "bk-alarm",   # 查告警
            "bk-metrics", # 查指标
            "bk-log",     # 查日志
        ],
        "expected_root_cause": "Old Gen 内存泄漏导致 Full GC",  # 期望根因
        "expected_keywords": ["内存", "GC", "OOM", "堆"],       # 答案必须包含的关键词
        "difficulty": "medium",
    },
    {
        "id": "case_002",
        "query": "routesvr 的四种路由模式分别是什么？",
        "expected_route": "knowledge_agent",
        "expected_tools": ["rag_search"],
        "expected_keywords": ["KeyHash", "MetaData", "SpecDst", "SpecDstKeyHash"],
        "difficulty": "easy",
    },
    # ... 50-100 个测试用例
]
```

### 9.5.3 自动化评估脚本

```python
# eval/evaluate_agent.py
"""
GameOps Agent 自动化评估
"""

import json
from typing import List, Dict

def evaluate_routing_accuracy(results: List[Dict]) -> float:
    """评估路由准确率"""
    correct = sum(1 for r in results if r["actual_route"] == r["expected_route"])
    return correct / len(results)

def evaluate_tool_call_accuracy(results: List[Dict]) -> float:
    """评估工具调用准确率"""
    scores = []
    for r in results:
        expected = set(r["expected_tools"])
        actual = set(r["actual_tools"])
        precision = len(expected & actual) / len(actual) if actual else 0
        recall = len(expected & actual) / len(expected) if expected else 0
        f1 = 2 * precision * recall / (precision + recall) if (precision + recall) > 0 else 0
        scores.append(f1)
    return sum(scores) / len(scores)

def evaluate_answer_quality(results: List[Dict], judge_model) -> Dict:
    """LLM-as-Judge 评估答案质量"""
    JUDGE_PROMPT = """请评估以下运维 Agent 的回答质量（1-5分）：
    - 准确性：根因诊断是否正确
    - 完整性：是否涵盖所有关键信息
    - 可操作性：建议是否具体可执行
    - 专业性：是否使用正确的专业术语
    
    问题：{query}
    Agent回答：{answer}
    参考答案要点：{reference}
    
    请输出 JSON：{{"accuracy": x, "completeness": x, "actionability": x, "professionalism": x}}
    """
    # ... 调用 judge_model 评分
    pass

def generate_report(results: List[Dict]) -> str:
    """生成评估报告"""
    routing_acc = evaluate_routing_accuracy(results)
    tool_acc = evaluate_tool_call_accuracy(results)
    
    report = f"""
    # GameOps Agent 评估报告
    
    ## 概览
    - 测试用例数：{len(results)}
    - 路由准确率：{routing_acc:.1%}
    - 工具调用 F1：{tool_acc:.1%}
    - 平均响应时间：{avg_time:.1f}s
    - 平均 Token 消耗：{avg_tokens:.0f}
    
    ## 分维度评分
    | 维度 | 得分 |
    |------|------|
    | 准确性 | {scores['accuracy']:.2f}/5 |
    | 完整性 | {scores['completeness']:.2f}/5 |
    | 可操作性 | {scores['actionability']:.2f}/5 |
    | 专业性 | {scores['professionalism']:.2f}/5 |
    """
    return report
```

**面试话术**：
> "我们建了三层评估体系：单步看路由和工具调用准确率，流程看诊断步骤合理性，端到端看任务完成率和用户满意度。用 50+ 历史故障案例构建测试集，LLM-as-Judge 自动评分，路由准确率 95%+，工具调用 F1 0.88，用户满意度 4.2/5。"

---

## 9.6 知识库微调模型融合 ⭐ 两条项目线交汇

> **说明**：本节描述如何将模型算法线（知识库专家模型）融合到 GameOps Agent 项目中。详细微调方案见 [模型算法微调项目执行方案.md](D:/UGit/Go-Agent/模型算法微调项目执行方案.md)。

### 9.6.1 融合架构

```
                 GameOps Agent
                      │
         ┌────────────┼────────────┐
         ▼            ▼            ▼
    Diagnosis    Knowledge     FileAnalyst
    Agent        Agent         Agent
    (通用LLM)    (微调模型)    (通用LLM)
         │            │
    蓝鲸MCP      ┌────┴────┐
              微调模型    RAG
              (内化知识)  (实时知识)
              ↓           ↓
          高频/通用    低频/最新
          问题        长尾问题
```

### 9.6.2 融合代码

```go
// agent/knowledge.go — 融合微调模型 + Agentic RAG 三引擎

// 知识专家微调模型（vLLM V1 + FP8 + EAGLE-3 部署，首 Token 180ms）
knowledgeExpertModel := openai.New(
    openai.WithBaseURL("http://localhost:8000/v1"),  // 微调后的 Qwen3-8B
    openai.WithModel("qwen3-8b-knowledge-expert"),
    openai.WithExtraHeaders(map[string]string{
        "X-Enable-Thinking": "false",  // 运维 QA 默认关闭 thinking，保持简洁
    }),
)

// 本地 Intent Router（Ollama 部署 Qwen3-0.6B，替代云端大模型做路由，省 API 成本）
routerModel := openai.New(
    openai.WithBaseURL("http://localhost:11434/v1"),  // Ollama
    openai.WithModel("qwen3-router"),
)

// 通用大模型（用于 CRAG 兜底时的复杂推理）
fallbackModel := openai.New(
    openai.WithBaseURL(os.Getenv("OPENAI_BASE_URL")),
    openai.WithModel("deepseek-v3.2-exp"),   // DeepSeek-V3.2-Exp：长上下文 + 低成本
)

// KnowledgeAgent 使用 Agentic RAG 模式（微调模型自己做检索规划）
knowledgeAgent := llmagent.New("knowledge_agent",
    llmagent.WithModel(knowledgeExpertModel),     // 核心：微调模型既做答主也做 Retrieval Planner
    llmagent.WithInstruction(`你是 LetsGo 游戏服务器运维知识专家。

决策流程（Agentic RAG）：
1. 对于你训练过的高频运维知识（routesvr 路由模式、告警阈值、常见故障），直接回答
2. 对于最新文档 / 长尾 / 具体故障案例，调用 retrieve_knowledge 工具
3. 如果一次检索信息不足，可以多跳检索（最多 3 轮），每次说明"我需要查询 X 来确认 Y"
4. 对于跨文档/跨工单推理类问题，调用 graph_search 工具走 GraphRAG
5. 给出答案时必须附带引用来源`),
    llmagent.WithTools([]tool.Tool{
        retrieveKnowledgeTool,   // Hybrid Retrieval + Rerank
        graphSearchTool,         // GraphRAG 跨文档多跳
    }),
    llmagent.WithKnowledge(agenticRAGKnowledge),  // 仍挂载底层 Agentic RAG pipeline
)
```

### 9.6.3 微调 + Agentic RAG 协同策略

| 场景 | 策略 | 原因 |
|------|------|------|
| 高频运维问题 | Qwen3-8B 微调模型直答（不触发检索） | 已内化知识，首 Token 延迟 180ms，成本最低 |
| 最新文档/变更 | 微调模型决策触发 Hybrid Retrieval | 微调模型训练数据有滞后性 |
| 长尾/罕见问题 | CRAG 模式：检索评估→不相关则重写 query→网搜兜底 | 微调数据未覆盖，需要多策略兜底 |
| 跨文档多跳推理 | **GraphRAG**（"上次类似 OOM 怎么解决的"）| 需要遍历历史工单+文档实体图谱 |
| 复杂跨文档问题 | 微调模型规划 + 多跳 Retrieval + DeepSeek-V3.2 最终合成 | 微调模型做 Router/Planner + 旗舰模型做长上下文推理 |
| Intent 路由 | Qwen3-0.6B 本地 Router 承担 | 简单分类任务无需云端大模型，成本降 95% |

**面试亮点**：
> "微调和 RAG 不是二选一，而是互补。微调让模型内化高频知识，减少检索延迟和 Token 成本；RAG 补充低频/最新知识。评测显示微调+RAG 比纯 RAG 准确率提升 15%，响应延迟降低 40%。"

### 9.6.4 推理部署经验沉淀（从本项目中积累）

> 本项目中 KnowledgeAgent 的模型部署环节，天然涉及推理优化知识。以下是在项目中刻意深化学习的实践点，对标 [AI 模型压缩相关岗位学习路线](D:/UGit/Go-Agent/AI%20模型压缩相关岗位学习路线.md) 的核心知识。

#### A. vLLM 部署时的推理优化观察

在 KnowledgeAgent 使用 vLLM 部署微调模型时，刻意做以下性能观测：

```bash
# 启动 vLLM 时开启详细日志，观察 KV Cache、调度等核心机制
python -m vllm.entrypoints.openai.api_server \
    --model ./output/knowledge_awq \
    --quantization awq \
    --max-model-len 4096 \
    --gpu-memory-utilization 0.85 \
    --enable-prefix-caching \     # 开启前缀缓存（运维问题常有相同 system prompt）
    --port 8000 \
    2>&1 | tee vllm_deploy.log    # 保存日志用于分析

# 观察日志中的关键信息：
# - KV Cache 分配了多少 Block
# - 请求调度策略（Continuous Batching 实际表现）
# - 显存占用分布（模型权重 vs KV Cache vs 激活）
```

```python
# 并发压测脚本 —— 积累 Continuous Batching 经验
import asyncio
import aiohttp
import time

async def benchmark_vllm(concurrent: int, total_requests: int):
    """模拟并发请求，观测 vLLM 的 Continuous Batching 效果"""
    url = "http://localhost:8000/v1/chat/completions"
    latencies = []
    
    async def single_request(session, idx):
        payload = {
            "model": "knowledge-expert",
            "messages": [{"role": "user", "content": f"gamesvr 启动失败怎么排查？(请求{idx})"}],
            "max_tokens": 256,
        }
        start = time.time()
        async with session.post(url, json=payload) as resp:
            await resp.json()
        latencies.append(time.time() - start)
    
    async with aiohttp.ClientSession() as session:
        semaphore = asyncio.Semaphore(concurrent)
        async def limited_request(idx):
            async with semaphore:
                await single_request(session, idx)
        
        start = time.time()
        await asyncio.gather(*[limited_request(i) for i in range(total_requests)])
        total_time = time.time() - start
    
    print(f"并发数: {concurrent}, 总请求: {total_requests}")
    print(f"总耗时: {total_time:.1f}s, QPS: {total_requests/total_time:.1f}")
    print(f"平均延迟: {sum(latencies)/len(latencies)*1000:.0f}ms")
    print(f"P95延迟: {sorted(latencies)[int(len(latencies)*0.95)]*1000:.0f}ms")

# 测试不同并发级别
for c in [1, 5, 10, 20]:
    asyncio.run(benchmark_vllm(concurrent=c, total_requests=50))
```

**可积累的面试经验**：
| 观测点 | 对应学习路线知识 | 面试可说的经验 |
|--------|----------------|--------------|
| KV Cache Block 数量 | 阶段二·2.1.1 KV Cache | "AWQ 量化后模型权重更小，释放的显存可分配给更多 KV Cache Block，提升并发能力" |
| 并发 QPS 随压力变化 | 阶段二·2.1.2 Continuous Batching | "测试发现 vLLM 的 Continuous Batching 在 10 并发下 QPS 比 1 并发提升了 4-5 倍" |
| prefix caching 效果 | KV Cache 复用 | "运维场景的 system prompt 固定，开启 prefix caching 后首 Token 延迟降低 30%" |

#### B. AWQ vs GPTQ 量化对比（在本项目中实测）

```bash
# 在 KnowledgeAgent 的模型上做 AWQ 和 GPTQ 对比
# 这个对比直接积累量化经验

# AWQ 量化
python -m awq.entry \
    --model_path ./output/knowledge_sft_merged \
    --w_bit 4 --q_group_size 128 \
    --output_path ./output/knowledge_awq

# GPTQ 量化
python -c "
from auto_gptq import AutoGPTQForCausalLM
model = AutoGPTQForCausalLM.from_pretrained(
    './output/knowledge_sft_merged',
    quantize_config={'bits': 4, 'group_size': 128, 'damp_percent': 0.01}
)
model.quantize(calibration_data)
model.save_quantized('./output/knowledge_gptq')
"

# 对比评测脚本
python scripts/evaluate.py \
    --models knowledge_awq,knowledge_gptq,knowledge_fp16 \
    --test_file data/test/knowledge_test.json \
    --output eval/quantization_comparison.md
```

**对比结果（记录到面试 STAR 里）**：
```
┌──────────┬─────────┬─────────┬─────────┬──────────┐
│ 模型      │ 准确性   │ 推理速度  │ 显存    │ 量化耗时  │
├──────────┼─────────┼─────────┼─────────┼──────────┤
│ FP16 原始 │ 基线     │ 基线     │ ~15GB   │ —       │
│ AWQ 4-bit │ -1~2%   │ +50~80% │ ~5GB    │ ~10min  │
│ GPTQ 4-bit│ -2~3%   │ +40~60% │ ~5GB    │ ~30min  │
└──────────┴─────────┴─────────┴─────────┴──────────┘

结论：AWQ 在量化速度和推理性能上优于 GPTQ，且精度损失更小。
原因：AWQ 通过保护激活值大的通道来保持精度，更适合 Transformer 模型。
```

#### C. 与模型微调项目的经验互补

```
本项目（GameOps Agent）积累的经验：        微调项目积累的经验：
├── vLLM 生产级部署最佳实践                ├── QLoRA 微调全流程
├── AWQ/GPTQ 量化对比实测                 ├── DPO 偏好对齐
├── Continuous Batching 并发性能           ├── GGUF 多精度量化
├── prefix caching 优化                   ├── llama.cpp CPU 推理
├── 微调+RAG 协同的推理成本分析             ├── 端侧部署（手机端）
└── 生产环境监控与告警                     └── 精度-速度-内存 Pareto 分析
        │                                        │
        └─────── 两个项目合计覆盖学习路线 70%+ ──────┘
```

---

## 十、Graph-based 诊断工作流（StateGraph + Checkpoint + Interrupt）

### 10.1 为什么使用 Graph-based 工作流？

| 理由 | 说明 |
|------|------|
| **复杂流程** | 诊断流程需要条件分支、并行查询、循环重试、多步门禁等复杂编排 |
| **Checkpoint 恢复** | 长时间运行的诊断（跨多个 MCP）可中断恢复，避免重复消耗 Token |
| **Human-in-the-Loop** | Interrupt 机制让流程在关键节点暂停等人工确认（修复前、编译前） |
| **可观测性** | 每个节点自动上报 OTel Span，诊断路径完整可追溯 |
| **可维护性** | 声明式编排，业务流程即代码文档 |

### 10.2 实现方式（tRPC-Agent-Go 原生 Graph + Eino 扩展双路径）

#### 路径 A：tRPC-Agent-Go 原生 Graph（推荐，生产首选）

```go
import (
    "trpc.group/trpc-go/trpc-agent-go/graph"
    "trpc.group/trpc-go/trpc-agent-go/graph/checkpoint"
)

// 1. 定义 State（携带跨节点的状态）
type DiagnosisState struct {
    Alert       *Alert
    Metrics     map[string]any
    Logs        []LogEntry
    Analysis    string
    Confidence  float64        // 诊断置信度
    NeedRepair  bool
}

// 2. 构建 StateGraph
diagGraph := graph.NewStateGraph[DiagnosisState]()

// 3. 添加节点
diagGraph.AddNode("query_alerts", queryAlertsNode)
diagGraph.AddNode("query_metrics", queryMetricsNode)
diagGraph.AddNode("query_logs", queryLogsNode)
diagGraph.AddNode("analyze", analyzeNode)
diagGraph.AddNode("human_confirm", humanConfirmNode)   // Human-in-the-Loop 节点

// 4. 并行节点组（查 alerts 之后并行查 metrics/logs）
diagGraph.AddParallel("parallel_query",
    []string{"query_metrics", "query_logs"},
)

// 5. 条件边 + 门禁
diagGraph.AddEdge(graph.START, "query_alerts")
diagGraph.AddConditionalEdge("query_alerts", func(s DiagnosisState) string {
    if s.Alert == nil { return graph.END }
    return "parallel_query"
})
diagGraph.AddEdge("parallel_query", "analyze")
diagGraph.AddConditionalEdge("analyze", func(s DiagnosisState) string {
    if s.Confidence < 0.7 { return "human_confirm" }  // 低置信度转人工确认
    if s.NeedRepair      { return "repair_agent" }    // 高置信度且需要修复
    return graph.END
})

// 6. Checkpoint + Interrupt
compiled, _ := diagGraph.Compile(
    graph.WithCheckpointer(checkpoint.NewRedisCheckpointer(redisClient)),  // Redis 持久化
    graph.WithInterruptBefore("human_confirm"),        // 人工确认前暂停
    graph.WithInterruptBefore("repair_agent"),         // 修复前暂停
)

// 7. 运行（支持从 checkpoint 恢复）
result, err := compiled.Invoke(ctx, initialState,
    graph.WithThreadID("diag-20260418-001"),   // 线程 ID 用于 checkpoint 关联
)

// 8. 中断后恢复（人工确认完毕）
result2, err := compiled.Resume(ctx, "diag-20260418-001", humanDecision)
```

#### 路径 B：Eino Graph 适配器（复用 Eino 生态）

```go
import (
    "github.com/cloudwego/eino/compose"
    teino "trpc.group/trpc-go/trpc-agent-go/trpc/agent/eino"
)

// Eino Graph 编排
graph := compose.NewGraph[string, string]()
graph.AddLambdaNode("query_alerts", queryAlertsFunc)
graph.AddLambdaNode("query_metrics", queryMetricsFunc)
graph.AddLambdaNode("query_logs", queryLogsFunc)
graph.AddLambdaNode("analyze", analyzeFunc)

graph.AddEdge(compose.START, "query_alerts")
graph.AddBranch("query_alerts", compose.NewBranch().
    AddCondition(hasAlert, "query_metrics").
    AddCondition(noAlert, compose.END))
graph.AddEdge("query_metrics", "query_logs")
graph.AddEdge("query_logs", "analyze")
graph.AddEdge("analyze", compose.END)

compiled, _ := graph.Compile(ctx)
diagnosisGraphAgent := teino.New(compiled, "diagnosis_graph")
```

### 10.3 Graph vs Multi-Agent Transfer 选择

| 场景 | 用 Multi-Agent Transfer | 用 StateGraph |
|------|------------------------|--------------|
| 开放式问答（路由即可） | ✅ Coordinator → 子 Agent | ❌ 过度设计 |
| 有明确步骤和门禁的流程 | ❌ LLM 可能跳步 | ✅ RepairAgent 修复闭环 |
| 需要中断恢复 | ❌ 不支持 | ✅ Checkpoint |
| 需要 Human-in-the-Loop | ⚠️ 需手动实现 | ✅ 原生 Interrupt |
| 需要并行执行多个查询 | ⚠️ 子 Agent 内自行并发 | ✅ 原生 Parallel 节点 |

---

## 十一、Agent 评估系统（G-Eval + RAGAS + Langfuse + Trajectory Eval）

### 11.1 为什么需要 Agent 评估系统？

| 理由 | 说明 |
|------|------|
| **质量回归** | Prompt / 工具 / 模型任何改动都可能引起能力退化，需自动化回归 |
| **多维度度量** | 不只是答案对错，还要看工具选择、Agent Transfer、检索决策、Trace 完整性 |
| **成本与延迟** | 每次调用的 Token / 费用 / 首 Token 延迟 / P95 必须可监控 |
| **线上可观测** | 生产环境每一次调用都能追溯：哪个 Agent、调用了什么工具、为什么这样回答 |

### 11.2 三层评估体系

```mermaid
graph TB
    subgraph "离线评估（CI/CD 回归）"
        TS["Gold Test Set<br>(50+ cases)"] --> GE["G-Eval<br>(自定义维度)"]
        TS --> RG["RAGAS<br>(RAG 专用)"]
        TS --> TE["Trajectory Eval<br>(工具调用链正确性)"]
    end

    subgraph "在线评估（生产环境）"
        Prod["生产流量"] --> LF["Langfuse Trace<br>(采样)"]
        LF --> HF["Human Feedback<br>(用户点赞/踩)"]
        LF --> LaaJ["LLM-as-Judge<br>(异步打分)"]
    end

    subgraph "可观测性"
        LF --> OTel["OpenTelemetry<br>GenAI Semantic Conv v1.30"]
        OTel --> Graf["Grafana / 伽利略<br>(Token/Cost/Latency)"]
    end
```

### 11.3 实现方式

```go
// agent/evaluation.go — 离线评估入口
package evaluation

import (
    "trpc.group/trpc-go/trpc-agent-go/evaluation"
    "trpc.group/trpc-go/trpc-agent-go/evaluation/metrics"
)

// 三层评估体系
func RunFullEvaluation(agent agent.Agent, testset []TestCase) *Report {
    eval := evaluation.New(
        // Layer 1: G-Eval 自定义维度（对标 DeepEval）
        evaluation.WithGEval(
            metrics.GEval{Name: "RootCauseAccuracy", Criteria: "诊断根因是否准确"},
            metrics.GEval{Name: "EvidenceSufficiency", Criteria: "证据是否充分"},
            metrics.GEval{Name: "ActionablityOfSuggestion", Criteria: "建议是否可执行"},
        ),
        // Layer 2: RAGAS RAG 专用
        evaluation.WithRAGAS(
            metrics.Faithfulness, metrics.AnswerRelevancy,
            metrics.ContextPrecision, metrics.ContextRecall,
            metrics.NoiseSensitivity,        // 2026 新增
            metrics.RetrievalDecision,       // Agentic RAG 专属
        ),
        // Layer 3: Trajectory Eval（工具调用链正确性 - Agent 专属）
        evaluation.WithTrajectoryEval(
            metrics.ToolSelectionAccuracy,   // 工具选择正确率
            metrics.AgentTransferCorrectness, // Multi-Agent Transfer 正确性
            metrics.StepwiseProgress,        // 每一步是否推进目标
        ),
        // 裁判模型（异源交叉评审）
        evaluation.WithJudgeModel("deepseek-v3.2-exp"),
    )
    return eval.Run(agent, testset)
}
```

```python
# eval/ragas_eval.py — RAGAS 评测（Python 生态更成熟）
from ragas import evaluate
from ragas.metrics import (
    faithfulness, answer_relevancy, context_precision, context_recall,
    noise_sensitivity_relevant,
)
from langfuse import Langfuse

# Langfuse Trace：每次评估自动上报
langfuse = Langfuse()
with langfuse.start_as_current_span(name="agent-eval-ci-run") as span:
    results = evaluate(
        dataset=test_dataset,
        metrics=[faithfulness, answer_relevancy, context_precision, context_recall,
                 noise_sensitivity_relevant],
    )
    span.update(metadata=dict(results))
    print(results)
```

### 11.4 OpenTelemetry GenAI Semantic Convention v1.30（2026 标配）

```go
// 在 Plugin 中自动注入 OTel Trace（符合 v1.30 语义规范）
import (
    "go.opentelemetry.io/otel"
    semconv "go.opentelemetry.io/otel/semconv/v1.30.0/genai"
)

func OTelPlugin() plugin.Plugin {
    return plugin.BeforeModel(func(ctx context.Context, req *model.Request) error {
        span := trace.SpanFromContext(ctx)
        span.SetAttributes(
            semconv.GenAISystem("openai"),
            semconv.GenAIRequestModel(req.Model),
            semconv.GenAIRequestMaxTokens(req.MaxTokens),
            semconv.GenAIRequestTemperature(req.Temperature),
            semconv.GenAIOperationName("chat"),
        )
        return nil
    })
}
// Trace 自动上报 Langfuse / Jaeger / 伽利略，一份 Span 多处消费
```

### 11.5 三件套评估指标目标

| 指标 | 目标值 | 度量方式 |
|------|--------|---------|
| G-Eval: RootCauseAccuracy | ≥ 0.85 | LLM-as-Judge 打分均值 |
| G-Eval: EvidenceSufficiency | ≥ 0.80 | LLM-as-Judge 打分均值 |
| RAGAS: Faithfulness | ≥ 0.90 | 答案对 context 的忠实度 |
| RAGAS: ContextRecall | ≥ 0.88 | 混合检索 + Rerank 后的召回率 |
| RAGAS: RetrievalDecision | ≥ 0.85 | Agentic RAG 检索决策正确率 |
| Trajectory: ToolSelectionAccuracy | ≥ 0.92 | 工具选择正确率（Hierarchical 分层后） |
| Trajectory: AgentTransferCorrectness | ≥ 0.90 | Transfer 正确率 |
| Latency: 首 Token P95 | ≤ 500ms | OTel Metric |
| Cost: 单次调用平均 Token | ≤ 5K | Langfuse Usage |

---

## 十二、模型微调项目集成（Qwen3 双模 + Agentic RAG）

### 12.1 为什么需要集成模型微调项目？

| 理由 | 说明 |
|------|------|
| **领域专家能力** | 通用大模型对 LetsGo 运维术语/指标/服务名不熟，微调后的 Qwen3-8B 能直接内化这些知识 |
| **Agentic RAG Planner** | 微调模型同时承担 "是否检索/检索什么/是否多跳" 的规划角色，比固定 pipeline 更灵活 |
| **成本优化** | 高频运维问题模型直答不触发检索，Token 成本降 70%、首 Token 延迟降 55% |
| **本地 Intent Router** | Qwen3-0.6B 本地部署做意图识别，替代云端大模型 API，月成本降 95% |
| **端侧延伸** | 模型工具链打通后，可复用于游戏内 AINPC 端侧部署（方案 A 方向二） |

### 12.2 实现方式

```go
// agent/knowledge.go — 融合微调模型 + Agentic RAG
import (
    "trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
    "trpc.group/trpc-go/trpc-agent-go/model/openai"
)

// Qwen3-8B 知识专家（vLLM V1 + FP8 + EAGLE-3 部署）
expertModel := openai.New(
    openai.WithBaseURL("http://localhost:8000/v1"),
    openai.WithModel("qwen3-8b-knowledge-expert"),
)

// Qwen3-0.6B 本地 Intent Router（Ollama）
routerModel := openai.New(
    openai.WithBaseURL("http://localhost:11434/v1"),
    openai.WithModel("qwen3-router"),
)

// DeepSeek-V3.2-Exp 兜底（长上下文跨文档推理）
fallbackModel := openai.New(
    openai.WithModel("deepseek-v3.2-exp"),
)

// KnowledgeAgent：Agentic RAG 模式
knowledgeAgent := llmagent.New("knowledge_agent",
    llmagent.WithModel(expertModel),          // 专家模型既做答主也做 Planner
    llmagent.WithInstruction(knowledgePrompt),
    llmagent.WithTools([]tool.Tool{retrieveKnowledgeTool, graphSearchTool}),
    llmagent.WithKnowledge(agenticRAGKnowledge),
)

// Coordinator：用 Qwen3-0.6B 本地 Router 做快速意图分类
coordinator := llmagent.New("coordinator",
    llmagent.WithModel(routerModel),          // 0.6B 本地路由，延迟 < 100ms
    llmagent.WithSubAgents(diagnosisAgent, knowledgeAgent, fileAnalystAgent, repairAgent),
)
```

### 12.3 集成收益（预期）

| 指标 | 集成前（通用大模型 + 朴素 RAG） | 集成后（微调 + Agentic RAG） |
|------|-------------------------------|------------------------------|
| 高频运维问题首 Token 延迟 | ~600ms | **~180ms**（-70%）|
| 意图路由延迟 | ~800ms（云端大模型） | **~90ms**（本地 Qwen3-0.6B）|
| 检索触发率 | 100% | **35%**（模型决策，高频直答）|
| 答案 Faithfulness (RAGAS) | 0.82 | **0.91**（+11%） |
| 月度 Token 成本 | 基线 100% | **~30%**（-70%） |

---

## 十三、四周排期计划（借鉴 oncall_agent 加速后）

> **加速策略**：以 oncall_agent 为骨架 fork，复用其 MCP 工具管理、SSE 服务层、A2A/AGUI 注册、ReAct Planner、回调链等经过生产验证的模块。预计节省约 6 天开发时间，可将更多精力投入到业务 Prompt 调优和全链路闭环验证上。

### 第一周：核心骨架 + 单 Agent 可用（借助 oncall_agent 加速）

| 天数 | 任务 | 关键交付物 | oncall_agent 复用项 |
|------|------|-----------|-------------------|
| **D1** | Fork oncall_agent + 适配项目骨架 | 项目结构搭建完成，目录布局参照 oncall_agent 的 `src/agents/` + `src/tools/` + `src/services/` + `src/config/` 四层分离模式 | 复用 main.go 多服务注册、reqFilter、react.go、common.go |
| **D2** | 蓝鲸 MCP 接入（6个核心） + 配置中心 | metrics/log/alarm/event/tracing/metadata 6 个 MCP 接入，借鉴 oncall_agent 的 target 映射管理 | 复用 mcptool.go + mcptool_impl.go 的 target→MCP 映射架构 |
| **D3** | RAG 知识库搭建 + KnowledgeAgent | 运维文档向量化入库，KnowledgeAgent 单独可用 | 复用 Agent 创建模式（embed prompt + 配置覆盖） |
| **D4** | DiagnosisAgent + BCS MCP 4 个 | DiagnosisAgent 按 target 加载蓝鲸+BCS 工具，诊断流程可用 | 复用动态工具过滤 createToolFilter 模式 |
| **D5** | Multi-Agent 基础协作 + SSE 服务 | Coordinator + DiagnosisAgent + KnowledgeAgent 三 Agent 协作，SSE 流式输出可用 | 复用 SSE 服务层（sse_impl.go）+ A2A/AGUI 注册 |

### 第二周：完善诊断 + 服务化 + 文件分析

| 天数 | 任务 | 关键交付物 | oncall_agent 复用项 |
|------|------|-----------|-------------------|
| **D6** | FileAnalystAgent + 文件上传 | /upload HTTP 接口 + file_analyze + analyze_image 工具 | 复用 util_tools 的 Base64 工具等通用工具 |
| **D7** | Skills 技能系统 + 图片多模态 | 2-3 个基础 Skill（日志分析/CSV对比/性能报告） | — |
| **D8** | AG-UI 前端 + A2A 协议 | AG-UI Web 前端可访问 + A2A 协议可被其他 Agent 调用 | 直接复用 services/a2a/ + services/agui/（零改动） |
| **D9** | Session/Memory + 自动总结 | inmemory Session + 对话过长自动总结 | 复用 oncall_agent 的 summarizer 配置模式 |
| **D10** | Prompt 精调 + 工具描述优化 | 各 Agent 的 System Prompt 首轮调优，工具描述精炼 | 参考 oncall_agent 的 system_prompt.md 编写规范 |

### 第三周：全链路修复闭环

| 天数 | 任务 | 关键交付物 |
|------|------|-----------|
| **D11** | TAPD MCP + 工蜂 Git 工具封装 | TAPD bugs_get/update 接入 + 工蜂 Git 4个 FunctionTool |
| **D12** | 蓝盾 MCP + 编译验证 | 蓝盾流水线触发 + 轮询编译状态 |
| **D13** | RepairAgent 核心实现 | 修复闭环 Agent + Graph 编排（条件分支+门禁） |
| **D14** | Diagnosis→Repair Transfer | 诊断完自动 Transfer 到修复，全链路打通 |
| **D15** | 修复报告生成 + 安全防护 | 报告模板 + safety_guard Plugin（危险操作拦截） |

### 第四周：打磨 + 测试 + 端到端验证

| 天数 | 任务 | 关键交付物 |
|------|------|-----------|
| **D16** | Webhook 自动触发 | 蓝鲸告警/TAPD新单 Webhook 自动触发流程 |
| **D17** | 评估数据集 + 自动化测试 | 50+ 测试用例 + LLM-as-Judge 评估 |
| **D18** | 端到端全链路测试 | 10+ 场景完整验证（诊断+修复+编译） |
| **D19** | Prompt 优化 + 边界处理 | System Prompt 打磨 + 异常路径处理 |
| **D20** | Demo 录制 + 文档整理 | 完整 Demo 视频 + 面试话术准备 |

### 里程碑节点

```
D1 ──── D5 ──── D7 ──── D10 ──── D13 ──── D15 ──── D18 ──── D20
 │       │       │       │        │        │        │        │
 ▼       ▼       ▼       ▼        ▼        ▼        ▼        ▼
骨架   Multi-   BCS     服务化   Repair   全链路    端到端    可交付
可对话  Agent   容器诊断  AG-UI   Agent    打通     测试     Demo可用
       协作     接入     可用     修复闭环  闭环完成  验证
```

---

## 十四、Demo 场景设计

### 场景 1：故障诊断（展示 MCP Function Calling + BCS容器诊断 + 多工具协调）

```
👤 用户：昨晚凌晨3点gamesvr Pod重启了3次，帮我分析一下原因

🤖 Agent（Coordinator → DiagnosisAgent）：
  📊 调用 bk-metrics: 查询 gamesvr 凌晨3点的内存/CPU/GC指标
  📋 调用 bk-alarm: 查询对应时段的告警记录
  📝 调用 bk-log: 搜索 gamesvr 凌晨3点的 error 日志
  📡 调用 bk-event: 查询重启事件
  🐳 调用 bcs-resource(GetPo): 查询 Pod 状态详情（OOMKilled/CrashLoopBackOff）

  分析结论：
  根据监控数据和容器状态分析，gamesvr 在凌晨 3:12、3:28、3:41 共重启3次，原因如下：

  1. **根因**：Old Gen 内存持续增长至 95%，触发 Full GC，GC 停顿超过 30s
  2. **证据**：
     - 堆内存使用率从 2:30 的 60% 持续攀升至 3:12 的 95%
     - 告警记录显示触发 weChatWarningHeapUsage 阈值
     - 日志中出现 "java.lang.OutOfMemoryError: Java heap space"
  3. **建议**：
     - 短期：调整 JVM 堆大小 -Xmx 从 8G 提升到 12G
     - 长期：排查是否存在 ThreadLocal 内存泄漏
```

### 场景 2：知识问答（展示 RAG）

```
👤 用户：routesvr 的四种路由模式分别是什么？什么场景下用哪种？

🤖 Agent（Coordinator → KnowledgeAgent + RAG）：
  📖 检索到：微服务架构设计文档 - 负载均衡策略

  routesvr 支持四种路由模式：
  1. **KeyHash** - 根据键值哈希路由，适用于同一玩家请求路由到同一实例
  2. **MetaData** - 根据元数据路由，适用于按区服/版本等标签分流
  3. **SpecDst** - 指定目标实例，适用于精确路由到特定服务实例
  4. **SpecDstKeyHash** - 结合指定实例和哈希，在指定范围内做哈希路由
```

### 场景 3：文件上传分析（展示文件上传 + Skills）

```
👤 用户：[上传 perf_data.csv] 帮我分析一下这个性能数据，对比上周和这周的变化

🤖 Agent（Coordinator → FileAnalystAgent）：
  📁 调用 file_analyze: 解析 CSV 文件结构和内容摘要
  🔧 调用 Skill csv_compare: 运行 Python 脚本做数据对比

  分析结果：
  CSV 文件包含 7 天的性能数据，共 168 条记录。关键变化：
  - 平均 QPS：上周 12,500 → 本周 15,200（+21.6%）
  - P99 延迟：上周 85ms → 本周 120ms（+41.2%）⚠️ 显著上升
  - 错误率：上周 0.02% → 本周 0.08%（+300%）⚠️ 需要关注
  建议关注 P99 延迟上升的原因，可能与 QPS 增长有关。
```

### 场景 4：截图分析（展示多模态）

```
👤 用户：[上传 monitor_screenshot.png] 这个监控面板截图显示了什么问题？

🤖 Agent（Coordinator → FileAnalystAgent）：
  🖼️ 调用 analyze_image: 多模态模型分析截图

  截图分析：
  这是一张 Grafana 监控面板截图，显示以下信息：
  - 时间范围：03-04 14:00 ~ 03-04 16:00
  - CPU 使用率从 14:30 开始急剧上升，从 30% 升至 85%
  - 同时段 GC 暂停时间从 50ms 升至 800ms
  - 请求队列长度从 0 增长到 500+
  结论：14:30 可能有突发流量或内存泄漏导致频繁 GC，建议查看同时段日志。
```

### 场景 5：全链路自动修复闭环（展示 RepairAgent + Git + 蓝盾 + TAPD）

```
👤 用户：gamesvr凌晨3点OOM重启3次，帮我自动排查并修复

🤖 Agent（Coordinator → DiagnosisAgent）：
  📋 调用 bk-alarm: 发现 weChatWarningHeapUsage 告警
  📊 调用 bk-metrics: 堆内存 60% → 95%（2:30 ~ 3:12）
  📝 调用 bk-log: "OutOfMemoryError: Java heap space"
  🐳 调用 bcs-resource: Pod 状态 OOMKilled, 重启3次

  诊断结论：Old Gen 内存泄漏导致 Full GC → OOM Kill

🤖 Agent（DiagnosisAgent → Transfer → RepairAgent）：
  📄 调用 git_get_file_content: 获取 session_mgr.go 源码
  💡 AI生成修复代码: 在 onSessionClose 添加 threadLocal.Remove()
  🔀 调用 git_create_branch: fix/tapd-1234567890-oom-threadlocal-leak
  📤 调用 git_commit_file: 提交修复代码
  🔗 调用 git_create_merge_request: 创建MR
  🔨 调用 v4_user_build_start: 触发蓝盾编译
  ⏳ 调用 v4_user_build_status: 轮询... 编译成功！
  📋 调用 bugs_update: 更新TAPD单描述

  ✅ 修复报告：
  ┌─────────────────────────────────────────────┐
  │  🔧 GameOps 修复报告                         │
  │  根因: ThreadLocal 未清理 → 内存泄漏 → OOM    │
  │  修复: session_mgr.go:128 添加清理逻辑        │
  │  MR: https://git.woa.com/.../merge_requests/42│
  │  编译: ✅ SUCCEED（耗时 2分38秒）              │
  │  全流程耗时: 4分30秒                          │
  │  ⚠️ 请审阅MR后合并代码                        │
  └─────────────────────────────────────────────┘
```

---

## 十五、常见实际问题预案

| 问题 | 影响 | 应对方案 |
|------|------|---------|
| **MCP 响应慢/超时** | 蓝鲸 MCP 查询大时间范围数据可能慢 | 设置 30s 超时 + Agent Prompt 引导先查小时间范围，再逐步扩大 |
| **MCP Token 过期** | Access Token 有效期限制 | 启动时检查 Token 有效性，配合环境变量轮换，加监控告警 |
| **MCP 工具列表太多** | 6个MCP可能暴露 30+ 工具，LLM 选择困难 | Coordinator 先做意图分类，每个子 Agent 只挂相关 MCP 工具 |
| **上传文件太大** | CSV/日志可能几百MB | 上传接口限制 50MB，大文件先截取关键部分再送 LLM |
| **图片 OCR 不准** | 监控截图数字密集 | 先用 Skills(OCR 脚本) 提取文本，再送 LLM 理解 |
| **LLM 幻觉** | 模型编造不存在的指标名/服务名 | RAG 注入真实服务列表和指标名到 System Prompt |
| **并发 Session 冲突** | 多人同时使用 | 用 userID + sessionID 隔离，Redis Session 加锁 |
| **MCP 调用权限** | 不同用户可访问不同业务数据 | 蓝鲸 Token 按用户/业务隔离，Agent 透传用户身份 |
| **运维操作安全** | Agent 不应随意执行危险操作 | 敏感操作走 Human-in-the-Loop 确认，Plugin BeforeTool 拦截 |
| **知识库更新** | 运维文档可能过时 | RAG 知识库支持增量更新，定期从 iwiki 同步最新文档 |
| **Git 操作失败** | 工蜂 API 调用失败/权限不足 | Git 三步操作任一步失败立即停止，输出详细错误信息，绝不自动合并 |
| **编译超时** | 蓝盾流水线排队或编译耗时长 | 30秒间隔轮询 + 30分钟超时上限，超时后输出报告终止流程 |
| **TAPD 权限** | 不同用户对 TAPD 项目的操作权限不同 | 通过 X-Tapd-Access-Token 透传用户身份，权限不足时降级为只读 |
| **自动修复误修** | AI 生成的修复代码可能有问题 | 绝不自动合并代码，只创建 MR 等待人工审阅；safety_guard Plugin 拦截危险操作 |

---

## 十六、技术亮点总结（对标 JD）

| JD 要求 | 项目体现 |
|---------|---------|
| **Agent 架构设计** | Multi-Agent（Coordinator + Diagnosis + Knowledge + FileAnalyst + Repair 5-Agent 协作）；借鉴 oncall_agent 的 Agent 创建模式（embed prompt + 配置覆盖 + 回调链） |
| **Agentic RAG** ⭐ | **Self-RAG + CRAG + GraphRAG 三范式融合**，由微调模型自主决策检索；Faithfulness 从 0.82 → 0.91 |
| **RAG 优化** | BGE-M3 Dense+Sparse+ColBERT 三合一 + BGE-Reranker-v2-M3 精排 + RRF 融合；Context Recall 从 0.72 → 0.91 |
| **GraphRAG** ⭐ | Neo4j 知识图谱 + Leiden 社区检测 + Local/Global Search；跨工单多跳推理召回率从 50% → 85% |
| **Function Calling / Tool Use** | 蓝鲸 MCP 7 + BCS MCP 4 + TAPD MCP + 蓝盾 MCP + 工蜂 Git 4 个 FunctionTool + 文件/图片分析 |
| **MCP 协议** | **16 个 Streamable HTTP MCP Server** 原生接入（2025-03 规范，替代 SSE）+ 配置化扩展 + 借鉴 oncall_agent 的 target 映射 + 动态工具过滤（dynamic tool selection） |
| **A2A v0.2 协议** ⭐ | 跨 Agent 互通 + skill 声明 + task 生命周期 + push notification；本项目可被外部 Agent 调用，也可调用外部 Agent |
| **全链路自动化闭环** | 告警→诊断→修复→编译→验证→通知，人工仅需最后一步确认合并 |
| **Deep Research 模式** ⭐ | 复杂疑难故障触发多轮检索+多源对比+综合推理，产出研究级诊断报告 |
| **Skills 技能系统** | 日志分析/CSV对比/性能报告/容器诊断 Python 技能 |
| **多模态文件分析** | CSV/Excel/日志/配置/图片多格式支持 |
| **Memory 管理** | Session + 自动总结（借鉴 oncall_agent 的 summarizer 模式） + 长期记忆 |
| **Prompt Engineering** | 场景化 System Prompt + Agent 路由策略；采用 oncall_agent 的 embed + 配置中心双层管理，支持运行时热更新 |
| **Graph 编排** | **StateGraph + Checkpoint + Interrupt**：DiagnosisAgent 并行查询 + RepairAgent 严格门禁 + Human-in-the-Loop 中断恢复 |
| **Multi-Agent** | Transfer/Handoff 机制，诊断完成自动 Transfer 到修复 Agent；借鉴 oncall_agent 的 Agent-as-Tool 封装模式 |
| **Human-in-Loop** | StateGraph Interrupt 原生支持 + safety_guard Plugin + 修复前强制人工确认合并 |
| **容器编排** | BCS 容器平台 MCP 接入，Pod/Deployment/Service 状态诊断 |
| **Go 语言工程能力** | tRPC 微服务 + 北极星 + 服务治理；项目目录结构借鉴 oncall_agent 的 `src/` 四层分离模式 |
| **模型工程能力** | **Qwen3-8B 微调（KnowledgeAgent）+ Qwen3-0.6B 本地 Router**（详见方案 A）；vLLM V1 + FP8 + EAGLE-3 部署 |
| **可观测性** | **OpenTelemetry GenAI Semantic Convention v1.30** + Langfuse Trace + 伽利略；Token/Cost/Latency/工具调用链全链路埋点 |
| **Agent 评估** | **G-Eval + RAGAS + Trajectory Eval + Langfuse 在线 Trace** 四位一体；50+ Gold Set + CI/CD 回归 |
| **安全对齐** | **Guardrails + OWASP LLM Top 10 2025** + safety_guard Plugin + Prompt Injection 检测 + 危险操作拦截 |
| **工程复用能力** | 基于 oncall_agent 生产级代码 fork 快速搭建，复用 8 个核心模块，节省约 6 天开发时间 |
| **业务理解力** | 深度贴合 LetsGo 服务器架构和运维体系 |

---

## 十七、Eino 集成扩展（可选模块）

> **说明**：tRPC-Agent-Go 内置了完整的 Eino 适配器（`trpc/agent/eino` 包），可以将 Eino 框架的 Agent、Tool、Callback 无缝桥接到 tRPC 生态。此模块为**可选扩展**，实际项目不一定使用，但作为技术储备和框架能力展示非常有价值。

### 17.1 为什么保留 Eino 扩展能力？

| 理由 | 说明 |
|------|------|
| **编排能力互补** | Eino 的 compose.Graph 编排（DAG + 流式自动处理 + CheckPoint 中断恢复）比 tRPC-Agent-Go 原生编排更强大 |
| **ReAct Agent** | Eino 的 ReAct Agent 实现（Thought-Action-Observation 循环）可以直接复用 |
| **工具生态复用** | Eino 生态的 Tool 可以通过适配器转换为 tRPC Tool |
| **流式回调** | Eino 的 StreamCallbackAgent 提供更细粒度的流式事件拦截 |
| **未来兼容** | 如果团队有其他项目用 Eino，可以无缝集成到本系统 |

### 17.2 Eino 适配器核心 API

tRPC-Agent-Go 提供 3 种 Eino 适配方式：

```
┌─────────────────────────────────────────┐
│          Eino 适配器 (teino 包)          │
│                                          │
│  1. Agent 迁移                           │
│     Chain:  teino.New(compiledChain)      │
│     Graph:  teino.New(compiledGraph)      │
│     ReAct:  teino.NewReAct(reactAgent)    │
│                                          │
│  2. 工具转换                              │
│     teino.NewTool(einoTool)              │
│                                          │
│  3. 回调桥接                              │
│     teino.NewToolCallbacks(handler)      │
│     teino.NewModelCallbacks(handler)     │
│     teino.NewStreamCallbackAgent(agent)  │
└─────────────────────────────────────────┘
```

### 17.3 场景 A：用 Eino ReAct Agent 替换 DiagnosisAgent

当需要更精细的 Thought-Action-Observation 推理循环时，可用 Eino ReAct 替换原生 LLMAgent：

```go
import (
    einoReact "github.com/cloudwego/eino/adk/prebuilt/react"
    einoCompose "github.com/cloudwego/eino/compose"
    einoTool "github.com/cloudwego/eino/components/tool"
    teino "trpc.group/trpc-go/trpc-agent-go/trpc/agent/eino"
)

// 1. 用 Eino 创建 ReAct Agent（更强的推理循环）
einoModel := einoOpenAI.NewChatModel(ctx, &einoOpenAI.ChatModelConfig{
    Model: "deepseek-v3.2-exp",  // DeepSeek-V3.2-Exp：Sparse Attention 长上下文 + 低成本
})
reactAgent, _ := einoReact.NewAgent(ctx, &einoReact.AgentConfig{
    Model:       einoModel,
    ToolsConfig: einoCompose.ToolsNodeConfig{Tools: mcpEinoTools},
    MaxStep:     5, // 最多 5 轮推理
})

// 2. 通过适配器转为 tRPC Agent
diagnosisAgent := teino.NewReAct(reactAgent, "diagnosis_agent",
    teino.WithChunkSize(1024),
    teino.WithBufferSize(100),
)

// 3. 直接注册到 Coordinator 的 SubAgents 中
coordinator := llmagent.New("coordinator",
    llmagent.WithSubAgents(diagnosisAgent, knowledgeAgent, fileAnalystAgent),
    // ...
)
```

### 17.4 场景 B：用 Eino Graph 编排复杂诊断流程

当诊断流程需要条件分支、并行查询、循环重试等复杂编排时：

```go
import (
    "github.com/cloudwego/eino/compose"
    teino "trpc.group/trpc-go/trpc-agent-go/trpc/agent/eino"
)

// 1. 用 Eino Graph 编排复杂诊断流程
graph := compose.NewGraph[string, string]()

// 添加节点
graph.AddLambdaNode("query_alerts", queryAlertsFunc)
graph.AddLambdaNode("query_metrics", queryMetricsFunc)
graph.AddLambdaNode("query_logs", queryLogsFunc)
graph.AddLambdaNode("analyze", analyzeFunc)

// 编排：先查告警 → 并行查指标和日志 → 汇总分析
graph.AddEdge(compose.START, "query_alerts")
graph.AddBranch("query_alerts", compose.NewBranch().
    AddCondition(hasAlert, "query_metrics").
    AddCondition(noAlert, compose.END))
graph.AddEdge("query_metrics", "query_logs") // 或用 Parallel
graph.AddEdge("query_logs", "analyze")
graph.AddEdge("analyze", compose.END)

compiled, _ := graph.Compile(ctx)

// 2. 通过适配器转为 tRPC Agent
diagnosisGraphAgent := teino.New(compiled, "diagnosis_graph")

// 3. 可以作为子 Agent 或直接使用
```

### 17.5 场景 C：复用 Eino Tool 生态

将 Eino 工具直接转换为 tRPC 工具：

```go
import teino "trpc.group/trpc-go/trpc-agent-go/trpc/agent/eino"

// Eino 工具 → tRPC 工具
trpcTool := teino.NewTool(einoCalculatorTool)

// 可以直接注册到 tRPC Agent
agent := llmagent.New("agent",
    llmagent.WithTools([]tool.Tool{trpcTool}),
)
```

### 17.6 场景 D：StreamCallbackAgent 流式事件监控

监控特定 Agent 节点的流式输出（如 Debug/审计）：

```go
import teino "trpc.group/trpc-go/trpc-agent-go/trpc/agent/eino"

// 包装 Agent，拦截流式事件
monitoredAgent := teino.NewStreamCallbackAgent(diagnosisAgent,
    teino.WithEinoCallbacks(streamingHandler),    // 接收流式事件通知
    teino.WithNodeFilter("diagnosis_agent"),        // 只监控特定节点
)
```

### 17.7 Eino 集成示例清单

tRPC-Agent-Go 提供了 **8 个渐进式 Eino 集成示例**（`trpc/agent/eino/examples/`）：

| 示例 | 内容 | 难度 |
|------|------|------|
| `00_quickstart` | 30 秒快速体验 Eino→tRPC 迁移 | ⭐ |
| `01_agent_migration` | 完整 Agent 迁移（Chain/Graph/ReAct） | ⭐⭐ |
| `02_multiagent_integration` | 在现有多 Agent 系统中集成 Eino Agent | ⭐⭐ |
| `03_tool_conversion` | Eino 工具转换为 tRPC 工具 | ⭐⭐ |
| `04_basic_callbacks` | 基础回调转换（OnStart/OnEnd/OnError） | ⭐⭐⭐ |
| `05_streaming_callbacks` | 高级流式回调（OnEndWithStreamOutput） | ⭐⭐⭐⭐ |
| `06_complete_scenario` | 生产级完整集成示例 | ⭐⭐⭐⭐⭐ |
| `07_real_llm_multiagent` | 真实 LLM 多 Agent 集成 | ⭐⭐⭐⭐⭐ |

### 17.8 项目目录补充

如启用 Eino 扩展，项目增加以下目录：

```
gameops-agent/
├── ...
├── eino/                                # Eino 扩展模块（可选）
│   ├── diagnosis_react.go               # Eino ReAct 诊断 Agent
│   ├── diagnosis_graph.go               # Eino Graph 复杂诊断流程
│   └── tool_adapter.go                  # Eino 工具适配器
└── ...
```

---

## 十八、面试难点准备（每个准备 2-3 个有深度的技术难题）

> 面试中"你项目遇到最难的问题"是必考题（TOP 9），以下是 GameOps Agent 项目可以讲的技术难点：

### 难点 1：MCP 工具列表过多导致 LLM 选择困难

**问题描述**：
6 个蓝鲸 MCP Server 暴露了 30+ 工具，全部挂到一个 Agent 上时，LLM 的工具选择准确率从 95% 降到 70%。

**解决方案**：
1. **Hierarchical 架构**：Coordinator 只做意图路由（不挂工具），每个子 Agent 只挂相关工具
2. **工具分组**：DiagnosisAgent 挂 metrics/log/alarm/event/tracing 5 个 MCP，KnowledgeAgent 只挂 RAG
3. **工具描述优化**：精炼每个工具的 description，加入使用示例，帮助 LLM 更准确地选择
4. **效果**：工具选择准确率恢复到 92%

**面试话术**：
> "蓝鲸 MCP 暴露了 30 多个工具，全挂一个 Agent 上工具选择准确率只有 70%。我的解决思路是 Hierarchical 分层：Coordinator 做意图路由不挂任何工具，每个子 Agent 只挂自己领域的 5-8 个工具，工具描述也做了精炼优化，最终准确率恢复到 92%。"

### 难点 2：RAG 检索运维文档时专业术语召回率低

**问题描述**：
用户问"GC 暂停太长怎么办"，向量检索检索到"垃圾回收"相关内容，但漏掉了包含"Full GC"、"STW"等专业缩写的文档。

**解决方案**：
1. **混合检索**：Dense（向量） + Sparse（BM25 关键词），BM25 能精确匹配专业术语
2. **RRF 融合**：两路结果用 Reciprocal Rank Fusion 算法融合
3. **查询改写**：用 Multi-Query 把"GC 暂停"改写为 ["Full GC", "STW", "GC 停顿", "垃圾回收暂停"]
4. **同义词扩展**：在索引阶段建立运维术语的同义词表
5. **效果**：Context Recall 从 0.72 提升到 0.91

**面试话术**：
> "运维文档有大量专业术语和缩写，纯向量检索会漏掉。我的方案是 Dense+Sparse 混合检索 + RRF 融合，BM25 精确匹配术语，向量检索捕捉语义。再配合 Multi-Query 查询改写，把一个问题改写成多个角度。RAGAS 评测显示 Context Recall 从 0.72 提升到 0.91。"

### 难点 3：Agent 诊断流程不够结构化，输出质量不稳定

**问题描述**：
DiagnosisAgent 有时跳过查告警直接查日志，有时查了指标不查日志就下结论，输出质量不稳定。

**解决方案**：
1. **Graph 图编排替代自由 ReAct**：用 StateGraph 强制定义诊断流程（告警→指标→日志→分析）
2. **条件边控制**：根据前一步结果决定下一步（有告警→并行查指标+日志，无告警→扩大时间范围查）
3. **最大重试限制**：Graph 中设置 RetryCount ≤ 3 防止无限循环
4. **结构化 Prompt**：要求输出遵循固定模板（时间线→证据→根因→建议）
5. **效果**：诊断报告质量评分从 3.5 提升到 4.3（满分 5）

**面试话术**：
> "一开始用 ReAct 自由推理，Agent 经常跳步骤或重复查询。改用 StateGraph 图编排后，强制定义诊断流程：先查告警确认异常时间，再并行查指标和日志定位根因，最后综合分析生成报告。加了条件边和重试限制，质量评分从 3.5 提到 4.3。"

### 难点 4：全链路修复闭环中 Git 操作和编译验证的可靠性

**问题描述**：
RepairAgent 执行修复闭环涉及多个外部系统（工蜂 Git → 蓝盾编译 → TAPD 更新），任何一环失败都可能导致状态不一致（比如分支已创建但提交失败）。

**解决方案**：
1. **严格门禁机制**：用 StateGraph 编排修复流程，每一步设置条件边——前一步成功才能进入下一步，任一步失败立即终止
2. **绝不自动合并**：RepairAgent 只创建 MR，绝不自动合并代码，绝不自动关闭 TAPD 单
3. **蓝盾轮询策略**：30 秒间隔轮询编译状态，设置 30 分钟超时上限，防止无限等待
4. **safety_guard Plugin**：在 BeforeTool 回调中拦截高危操作（如 force push、删除分支），需要人工确认
5. **幂等设计**：Git 分支名基于 TAPD 单号，重复执行不会创建多余分支

**面试话术**：
> "全链路修复涉及 Git、蓝盾、TAPD 多个外部系统，最大的挑战是中间步骤失败后的状态一致性。我用 StateGraph 做严格门禁——每一步都有条件边判断成功才继续，失败就停止并输出完整错误报告。最核心的安全原则是'绝不自动合并代码'，AI 只管生成修复和创建 MR，最后一步一定是人工确认。"

---

## 十九、项目目录结构更新（借鉴 oncall_agent 的生产级目录布局）

> 以 oncall_agent 为参考骨架，按照其成熟的 `src/agents/`、`src/tools/`、`src/services/`、`src/config/` 四层分离模式组织项目代码。

```
gameops-agent/
├── main.go                          # 入口（借鉴 oncall_agent 的多服务注册模式）
├── go.mod                           # 依赖管理
├── trpc_go.yaml                     # tRPC 框架配置
├── mcp_servers.yaml                 # MCP 服务配置（本地开发/测试用）
│
├── src/
│   ├── agents/                      # Agent 层（借鉴 oncall_agent/src/agents/）
│   │   ├── common.go                # 公共函数（FillSystemContextInfo + ConstructGenConfig）
│   │   ├── react.go                 # 自定义中文化 ReAct Planner（复用 oncall_agent）
│   │   │
│   │   ├── coordinator/             # 协调者 Agent
│   │   │   ├── agent.go             # Agent 创建逻辑（embed + 配置覆盖模式）
│   │   │   └── system_prompt.md     # 默认 System Prompt
│   │   │
│   │   ├── diagnosis_agent/         # 故障诊断 Agent
│   │   │   ├── agent.go             # Agent 创建 + MCP 工具按 target 加载
│   │   │   └── system_prompt.md
│   │   │
│   │   ├── knowledge_agent/         # 知识问答 Agent
│   │   │   ├── agent.go             # Agent 创建 + RAG 知识库
│   │   │   └── system_prompt.md
│   │   │
│   │   ├── file_analyst_agent/      # 文件分析 Agent
│   │   │   ├── agent.go             # Agent 创建 + Skills + 多模态
│   │   │   └── system_prompt.md
│   │   │
│   │   └── repair_agent/            # 自动修复 Agent
│   │       ├── agent.go             # Agent 创建 + 工蜂 Git + 蓝盾
│   │       ├── repair_graph.go      # StateGraph 修复流程编排
│   │       └── system_prompt.md
│   │
│   ├── tools/                       # 工具层（借鉴 oncall_agent/src/tools/）
│   │   ├── mcp_tools/              # MCP 工具管理（核心复用 oncall_agent）
│   │   │   ├── mcptool.go          # API 接口定义
│   │   │   └── mcptool_impl.go     # target 映射 + 并发加载实现
│   │   │
│   │   ├── file_analyze/           # 文件解析 FunctionTool
│   │   │   └── tool.go
│   │   │
│   │   ├── image_analyze/          # 图片分析 FunctionTool（多模态）
│   │   │   └── tool.go
│   │   │
│   │   ├── gongfeng_git/           # 工蜂 Git API 封装
│   │   │   ├── tool.go             # 4 个 FunctionTool（创建分支/提交/MR/获取文件）
│   │   │   └── gongfeng_api.go     # REST API 客户端
│   │   │
│   │   ├── devops_build/           # 蓝盾编译验证
│   │   │   └── tool.go             # 轮询编译状态
│   │   │
│   │   ├── health_check/           # 健康检查
│   │   │   └── tool.go
│   │   │
│   │   └── util_tools/             # 通用工具（时间戳转换、Base64 等）
│   │       ├── time_tool.go        # 复用 oncall_agent
│   │       └── base64_tool.go      # 复用 oncall_agent
│   │
│   ├── services/                    # 服务层（借鉴 oncall_agent/src/services/）
│   │   ├── a2a/                    # A2A 协议服务（直接复用 oncall_agent）
│   │   │   └── a2a.go
│   │   │
│   │   ├── agui/                   # AG-UI 服务（直接复用 oncall_agent）
│   │   │   └── agui.go
│   │   │
│   │   ├── sse/                    # SSE 流式服务（复用 oncall_agent + 适配前端格式）
│   │   │   ├── sse.go              # 接口定义
│   │   │   ├── sse_impl.go         # 流式事件处理（工具调用可视化）
│   │   │   ├── special_cmd.go      # 特殊命令处理（反馈、清除对话等）
│   │   │   └── types.go            # 请求/响应类型定义
│   │   │
│   │   └── webhook/                # Webhook 自动触发
│   │       ├── alarm_webhook.go    # 蓝鲸告警 Webhook
│   │       ├── tapd_webhook.go     # TAPD 新 Bug 单 Webhook
│   │       └── bcs_event.go        # BCS Pod 异常事件
│   │
│   ├── config/                      # 配置层（借鉴 oncall_agent/src/config/）
│   │   ├── rainbow/                # Rainbow 运行时配置（API Key、模型参数等）
│   │   │   ├── rainbow.go          # 初始化
│   │   │   ├── types.go            # 配置结构体
│   │   │   └── sample.toml         # 配置样例
│   │   │
│   │   └── config_center/          # Agent/工具配置中心（替代 oncall_agent 的无极）
│   │       ├── config.go           # 接口定义（GetAgentConfig、GetAllMCPTool 等）
│   │       └── config_impl.go      # 实现（支持无极/YAML/数据库多种后端）
│   │
│   └── store/                       # 存储层
│       └── feedback/               # 用户反馈存储（复用 oncall_agent）
│           ├── feedback.go
│           └── feedback_impl.go
│
├── skills/                          # Skills 技能仓库
│   ├── log_pattern/
│   │   ├── SKILL.md
│   │   └── scripts/analyze.py
│   ├── csv_compare/
│   │   ├── SKILL.md
│   │   └── scripts/compare.py
│   ├── perf_report/
│   │   ├── SKILL.md
│   │   └── scripts/report.py
│   ├── container_diagnose/
│   │   ├── SKILL.md
│   │   └── scripts/diagnose.py
│   └── code_review/
│       ├── SKILL.md
│       └── scripts/review.py
│
├── knowledge/
│   ├── loader.go                    # 运维文档加载器
│   ├── rag_optimizer.go             # RAG 优化（混合检索+查询改写+重排）
│   └── docs/                        # RAG 语料
│       ├── fault_diagnosis.md
│       ├── monitoring_alerts.md
│       ├── architecture.md
│       ├── common_faq.md
│       ├── performance_guide.md
│       └── container_ops.md
│
├── eval/                            # 评估体系
│   ├── testset.json
│   ├── evaluate_agent.py
│   └── eval_report.md
│
├── plugin/
│   ├── logging.go                   # 日志插件
│   └── safety_guard.go              # 安全防护 Plugin
│
├── adapter/                         # 适配器层（借鉴 oncall_agent/adapter/）
│   └── taiji_adapter.py            # 太极一站式平台适配（如需接入）
│
├── upload2test.sh                   # 测试环境一键部署脚本（复用 oncall_agent）
│
└── eino/                            # Eino 扩展模块（可选）
    ├── diagnosis_react.go
    ├── diagnosis_graph.go
    └── tool_adapter.go
```

---

## 二十、未来演进路线

| 版本 | 核心能力 | 预计工时 |
|------|---------|---------|
| **V1.0（当前）** | Agentic RAG（Self-RAG/CRAG）+ GraphRAG + 知识问答 + 故障诊断 + 文件分析 + 16 个 Streamable MCP（蓝鲸×7 + BCS×4 + TAPD + 蓝盾）+ 工蜂 Git + StateGraph 修复闭环（Checkpoint/Interrupt）+ G-Eval/RAGAS/Langfuse 三件套评估 + OTel GenAI v1.30 可观测 + Qwen3-8B 微调 KnowledgeAgent | 4 周 |
| V1.5 | Deep Research 模式 + 仪表盘自动生成 + 更多 Skills + Eino 扩展模块 + Webhook 自动触发优化 + A2A v0.2 与外部 Agent 互通 | +1.5 周 |
| V2.0 | 自动化运维操作（扩缩容/回滚/配置推送）+ Human-in-the-Loop 审批流程（基于 Graph Interrupt）+ 细粒度 RBAC + 多租户隔离 | +2 周 |
| V2.5 | 手机端 NPC-style 告警解释助手（Qwen3-0.6B ExecuTorch + 高通 QNN NPU，复用方案 A 端侧链路）+ 离线运维应急模式 | +1.5 周 |
| V3.0 | 预测性运维（流量预测 + 故障预警 + 容量规划，融合时序模型）+ GRPO 强化学习优化诊断策略（DeepSeek-R1 同款路径）+ 跨部门 A2A 生态互通 | +4 周 |

---

## 二十一、面试高频深度问答

> 以下按面试考察维度整理，每个问题附带完整答题框架，可直接用于面试准备。

### 21.1 架构设计类

**Q1：为什么选择 Multi-Agent 而不是单 Agent + 全部工具？**

> 蓝鲸 + BCS + TAPD + 蓝盾一共 16 个 MCP Server，暴露了 40+ 工具。全挂一个 Agent 上，LLM 的工具选择准确率从 95% 降到 70%。我采用 Hierarchical 分层：Coordinator 只做意图路由不挂工具，每个子 Agent 只挂自己领域的 5-10 个工具，职责清晰、互不干扰。DiagnosisAgent 挂蓝鲸+BCS 共 11 个 MCP，RepairAgent 挂工蜂 Git+蓝盾+TAPD。最终工具选择准确率恢复到 92%。

**Q2：Coordinator 的路由策略是怎么实现的？有没有兜底机制？**

> Coordinator 本质是一个零工具的 LLMAgent，只通过 System Prompt 定义路由规则。路由决策完全由 LLM 的 Transfer 机制驱动——LLM 分析用户意图后选择 Transfer 给对应子 Agent。兜底机制有两层：1）Prompt 中明确"不确定时优先路由到 knowledge_agent"；2）子 Agent 可以反向 Transfer 回 Coordinator 重新路由。

**Q3：DiagnosisAgent → RepairAgent 的 Transfer 是怎么触发的？**

> DiagnosisAgent 的 System Prompt 中定义了：当诊断完成且用户需要自动修复时，主动 Transfer 给 repair_agent。这是 tRPC-Agent-Go 的原生 Transfer/Handoff 机制——子 Agent 之间可以互相 Transfer，无需回到 Coordinator 中转。Transfer 时会携带完整的对话上下文和诊断结论，RepairAgent 接手后能直接读取根因分析结果。

**Q4：为什么 RepairAgent 用 StateGraph 编排而不是 ReAct 自由推理？**

> 修复流程涉及外部副作用操作（Git 提交、触发编译、更新工单），不同于只读的诊断分析。ReAct 模式下 LLM 可能跳步骤、重试时创建重复分支，后果不可控。StateGraph 强制定义执行顺序，每一步有条件边做门禁——前一步成功才能进入下一步，任一步失败立即停止。对于有副作用的流程，确定性编排比自由推理更安全。

### 21.2 RAG 与检索类

**Q5：运维文档的 RAG 和通用文档 RAG 有什么区别？你怎么处理的？**

> 运维文档有三个特殊性：1）大量专业术语和缩写（GC、STW、OOM、CrashLoopBackOff），纯向量检索会漏掉；2）很多问题是时序相关的（"凌晨 3 点"、"过去 1 小时"），需要理解时间语义；3）文档格式多样（Markdown、日志片段、配置文件、历史工单）。
>
> 我的方案是 **Agentic RAG + BGE-M3 三合一混合检索**。BGE-M3 同一模型同时产出 Dense / Sparse / ColBERT 三种向量：Dense 覆盖语义近似（"服务器挂了" ≈ "进程崩溃"）、Sparse 精确匹配术语缩写、ColBERT Late Interaction 捕捉词级对齐。三路 RRF 融合后由 BGE-Reranker-v2-M3 精排。再叠加 CRAG 的 Retrieval Evaluator——检索完先评估相关性，低于阈值就重写 query 或走网搜兜底。针对跨工单多跳推理问题，额外用 **GraphRAG**（Neo4j + Leiden 社区检测）。最终 RAGAS 评测 Context Recall 从 0.72 提升到 0.91，Faithfulness 从 0.82 提升到 0.91。

**Q6：RAG 知识库多大？怎么保证更新？**

> 初始语料包括故障排查手册、监控告警体系、系统架构、FAQ、性能优化指南、容器运维知识、历史故障工单等 7 大类文档，约 200+ 页。采用增量更新策略：对接 iwiki 的 Webhook，文档更新时自动触发重新切片和向量化入库，增量更新不影响已有索引。切块策略用语义切块（Markdown 标题感知）+ 父子文档；对长工单额外使用 **Late Chunking**——先 long-context embedding 整篇，再切块，每个 chunk 继承全局语义，指代消解效果显著提升。

### 21.3 MCP 与工具调用类

**Q7：16 个 MCP Server 是怎么管理的？加一个新 MCP 需要改代码吗？**

> 完全配置化。所有 MCP Server 定义在 `mcp_servers.yaml` 中，包括 URL、传输协议、超时、认证方式。2026 年我们统一使用 **Streamable HTTP MCP**（2025-03 MCP 规范，已替代旧 SSE 为默认传输层），相比 SSE 支持双向流、会话恢复、更好的断线重连。`mcp/loader.go` 在启动时遍历配置，为每个 MCP Server 创建连接并提取工具列表。新增 MCP 只需加一段 YAML 配置，零代码改动。认证方面支持三种模式：简单 Token（蓝鲸监控）、模板化认证（BCS/蓝盾）、Header 透传（TAPD）。

**Q8：MCP 和 FunctionTool 有什么区别？什么时候用 MCP 什么时候自建 FunctionTool？**

> MCP 是标准化的远程工具协议（2025-03 Streamable HTTP 规范），工具的定义和实现都在 MCP Server 端，Agent 只是调用方。FunctionTool 是本地定义的工具函数，逻辑完全由 Agent 控制。选择标准很简单：如果外部平台已提供 MCP Server（蓝鲸、BCS、TAPD、蓝盾），直接接入 MCP，零开发成本；如果没有标准 MCP（比如工蜂 Git 只有 REST API），就封装为 FunctionTool。本项目 16 个 MCP 覆盖了 80% 的数据查询需求，只有 4 个工蜂 Git 工具 + 1 个编译验证工具 + 2 个文件分析工具需要自建。另外对于"工具太多导致 LLM 选择困难"的问题，我引入了 **dynamic tool selection**——由微调的 Qwen3-0.6B Router 先做 Top-K 工具选择，只把最相关的 5-8 个工具塞给主模型。

### 21.4 全链路闭环类

**Q9：自动修复的安全性怎么保证？如果 AI 生成了错误的修复代码怎么办？**

> 安全机制有四层：
> 1. **绝不自动合并**：RepairAgent 只创建 MR，不自动合并代码，不自动关闭 TAPD 单，最后一步必须人工确认
> 2. **蓝盾编译门禁**：代码提交后自动触发编译，编译失败就不会产生可合并的 MR
> 3. **safety_guard Plugin**：在 BeforeTool 回调中拦截高危操作（force push、删除分支、直接推 master），直接拒绝
> 4. **幂等设计**：Git 分支名基于 TAPD 单号（`fix/tapd-{id}-{desc}`），重复执行不会创建多余分支
>
> 即使 AI 生成了错误代码，最差情况也只是一个编译失败的 MR 在那里，不会影响线上环境。

**Q10：编译失败了怎么处理？会自动重试修复吗？**

> 当前版本编译失败后不会自动重试修复。RepairAgent 会生成完整的失败报告（包含编译错误日志、修复代码 diff、原始诊断结论），更新到 TAPD 单描述中，推送给相关人员人工介入。这是有意为之——自动修复的置信度有限，一次修复失败后再自动重试可能产生越修越乱的效果。未来可以考虑有限次重试（最多 2 次），但必须有不同的修复策略。

**Q11：从告警触发到修复 MR 创建，整个流程耗时多少？瓶颈在哪？**

> 端到端平均 5 分钟，其中：
> - 蓝鲸 Webhook → Agent 接收：< 5 秒
> - DiagnosisAgent 诊断（并行查 5+ 个 MCP）：30-60 秒
> - RepairAgent 获取源码 + AI 生成修复：20-40 秒
> - Git 三步操作（创建分支+提交+创建 MR）：10-15 秒
> - 蓝盾编译验证：2-5 分钟（主要瓶颈）
>
> 瓶颈是蓝盾编译。采用 30 秒间隔轮询，设 30 分钟超时。传统人工流程 2-4 小时，主要耗在人工排查和等沟通上。

### 21.5 Go 工程能力类

**Q12：为什么用 tRPC-Agent-Go 而不是用 Python 的 LangChain / LangGraph？**

> 三个原因：1）团队技术栈是 Go + tRPC 微服务生态，生产部署和运维都已有成熟体系（北极星服务发现、伽利略监控、蓝盾 CI/CD），Python 需要另起一套基础设施；2）tRPC-Agent-Go 原生支持 **Streamable HTTP MCP（2025-03 规范）、A2A v0.2、StateGraph（checkpoint/interrupt）、Agentic RAG、OTel GenAI Semantic Conv v1.30**，能力对等 LangGraph 但延迟更低；3）Go 的并发模型（goroutine + channel）天然适合多 MCP 并行调用场景，DiagnosisAgent 并行查 5 个 MCP 时延比 Python asyncio 低约 40%，且 p99 更稳定。加上 tRPC-Agent-Go 内置了 Eino 适配器，如果有部分模块需要复用 Eino 生态（比如某个工具或 ReAct 实现），可以零成本桥接。

**Q13：项目怎么做测试？怎么评估 Agent 的输出质量？**

> 三层评估体系：1）**离线 CI/CD 回归**——50+ Gold Test Set，每次 Prompt / 工具 / 模型变更后自动跑一遍，用 **G-Eval 自定义维度**（根因准确性、证据充分性、建议可行性）+ **RAGAS 六指标**（Faithfulness/AnswerRelevancy/ContextPrecision/ContextRecall/NoiseSensitivity/RetrievalDecision）+ **Trajectory Eval**（工具选择正确率、Agent Transfer 正确率、每一步是否推进目标）；2）**在线生产 Trace**——**Langfuse** 采样 10% 生产流量，自动上报 OTel GenAI v1.30 Span，包含完整的 LLM 调用链、工具调用、Token/Cost/Latency；3）**人工反馈回路**——用户点赞/踩 → 每周生成 bad case 清单 → 回归到 Gold Set。裁判模型用 **DeepSeek-V3.2-Exp**（和业务模型异源，避免自偏好）。

### 21.6 2026 架构演进类

**Q14：Streamable HTTP MCP 相比 SSE 有什么优势？**

> 2025-03 版 MCP 规范把 Streamable HTTP 定为默认传输层，SSE 降级为可选。核心优势：1）**双向流**：SSE 只能服务端推客户端，Streamable HTTP 支持客户端中途发送消息（如取消任务、传递新参数）；2）**会话恢复**：连接断开后用 `Last-Event-ID` 续传，避免长任务重头开始；3）**更好的超时和代理兼容**：企业网络里 SSE 经常被 LB 的 keepalive 策略误杀，Streamable HTTP 更鲁棒；4）**生态迁移**：主流 SDK（Python/TS/Go）已全部切换，未来新工具基本不会再出 SSE-only 的版本。本项目 16 个 MCP 全部走 Streamable HTTP。

**Q15：StateGraph 的 Checkpoint 和 Interrupt 在你项目里怎么用？**

> 两个典型场景：1）**RepairAgent 修复前暂停**——Git 提交和触发编译是有副作用的操作，修复代码生成后立刻 Interrupt，把诊断结论 + 修复 diff 推送给人审核，审核通过再 Resume 走下一步；2）**长时诊断中断恢复**——DiagnosisAgent 并行查 5 个 MCP，蓝盾编译又要 2-5 分钟，如果中途 Agent 进程重启，传统方案整个流程重跑、Token 白花。我用 Redis 做 Checkpointer，State（已查到的 Metrics/Logs/Analysis）每步都落盘，重启后凭 `thread_id` 从最近一个 checkpoint 直接 Resume，Token 消耗降 60%。

**Q16：Agentic RAG 和传统 RAG 的本质区别？**

> 传统 RAG 是 retrieve-then-generate 固定 pipeline，不管问什么都先检索。Agentic RAG 把"是否检索 / 检索什么 / 是否够了 / 是否要再查"这几个决策交给 Agent 自己做。具体有三种主流范式：**Self-RAG**（模型用特殊 token 自我评估，如 retrieve/isrel/issup/isuse）、**CRAG**（检索后跑 Retrieval Evaluator，不相关就重写 query 或走网搜兜底）、**GraphRAG**（知识图谱 + 社区检测 + 局部/全局两种搜索）。我在项目里三种都用了——高频问题走 Self-RAG 让微调模型直答不触发检索；长尾走 CRAG 兜底；跨工单多跳走 GraphRAG。最终相比朴素 RAG：Faithfulness +11%、ContextRecall +26%、平均延迟 -40%、Token 成本 -70%。

---

## 二十二、核心技术指标与量化成果

| 指标维度 | 度量项 | 数值 | 说明 |
|---------|--------|------|------|
| **工具选择** | 工具选择准确率 | 92% | Hierarchical 分层 + dynamic tool selection，从 70% 提升到 92% |
| **Agentic RAG** | RetrievalDecision 准确率 | 0.89 | 模型自主决定是否检索，避免不必要的检索 |
| **RAG 检索** | Context Recall | 0.91 | BGE-M3 三合一混合检索 + CRAG 重写 + GraphRAG 多跳 |
| **RAG 检索** | Context Precision | 0.88 | RRF 融合 + BGE-Reranker-v2-M3 精排 |
| **RAG 质量** | Faithfulness | 0.91 | 答案忠实度，相比朴素 RAG 从 0.82 提升到 0.91 |
| **RAG 鲁棒性** | NoiseSensitivity | 0.85 | 对无关文档的抗噪声能力（2026 新指标） |
| **诊断质量** | G-Eval 根因准确率 | 0.87 | 自定义维度 LLM-as-Judge 评估 |
| **Agent 行为** | TrajectoryEval: AgentTransfer 正确率 | 0.90 | Multi-Agent Transfer 路径正确性 |
| **首 Token 延迟** | KnowledgeAgent P95 | ~180ms | Qwen3-8B + FP8 + EAGLE-3；比通用大模型 600ms 降 70% |
| **意图路由延迟** | Coordinator P95 | ~90ms | Qwen3-0.6B 本地 Ollama 部署 |
| **全链路耗时** | 端到端平均耗时 | ~5 分钟 | 传统人工流程 2-4 小时 |
| **Token 成本** | 月度 Token 消耗 | 基线 30% | 微调直答 + Agentic RAG 决策，相对纯云端大模型降 70% |
| **人工操作** | 人工操作步骤 | 1 步 | 仅最后确认合并 MR，传统流程 10+ 步 |
| **MCP 覆盖** | Streamable MCP Server 数量 | 16 个 | 蓝鲸 7 + BCS 4 + TAPD 1 + 蓝盾 1 + 备用 3 |
| **Agent 协作** | Agent 数量 | 5 个 | Coordinator + Diagnosis + Knowledge + FileAnalyst + Repair |
| **评估体系** | 评估用例数 | 50+ | 覆盖诊断、知识问答、文件分析、全链路修复 |
| **可观测性** | OTel GenAI Semantic Conv | v1.30 | Prompt/Response/Token/工具调用链全链路 Trace |
| **安全防护** | 自动合并代码 | 0 次 | 绝不自动合并，绝不自动关闭 TAPD 单；Guardrails 输入输出双向过滤 |

### 效率提升对比

```
传统人工流程                          GameOps Agent
─────────────────                    ─────────────────
1. 收到告警通知         5 min        1. Webhook自动触发     0 sec
2. 登录蓝鲸查监控       10 min       2. 并行查询5+MCP       30 sec
3. 翻日志找错误         20 min       3. AI分析根因          10 sec
4. 查容器状态           10 min       4. 获取源码+生成修复    30 sec
5. 查TAPD关联单         5 min        5. Git提交+创建MR      15 sec
6. 分析根因             30 min       6. 蓝盾编译验证        3 min
7. 写修复代码           30 min       7. 更新TAPD单          5 sec
8. 提交+创建MR          10 min       8. 人工确认合并        1 min
9. 触发编译             5 min        ─────────────────
10. 等编译结果          10 min       总计: ~5 分钟
11. 更新TAPD单          5 min
12. 通知相关人          5 min
─────────────────
总计: 2-4 小时
```

---

### 面试亮点话术

> "我设计的 GameOps Agent 是一个全链路自动化闭环的智能运维系统，覆盖 2026 年 Agent 工程的全部核心栈。技术上做了几件事：
>
> 1. **Multi-Agent 分层 + StateGraph 编排**：5 个 Agent 协作（Coordinator + Diagnosis + Knowledge + FileAnalyst + Repair），Coordinator 只做意图路由，RepairAgent 用 StateGraph + Checkpoint + Interrupt 做严格门禁，修复前强制 Human-in-the-Loop。
> 2. **Agentic RAG + GraphRAG**：告别朴素 retrieve-then-generate，改由微调后的 Qwen3-8B 自主决策是否检索；复杂跨工单问题走 GraphRAG（Neo4j + Leiden 社区检测）。Faithfulness 从 0.82 → 0.91，延迟降 40%，Token 成本降 70%。
> 3. **16 个 Streamable HTTP MCP**：蓝鲸 7 + BCS 4 + TAPD + 蓝盾 + 工蜂 Git，全部走 2025-03 MCP 新规范；配合 dynamic tool selection，工具选择准确率 92%。
> 4. **模型工程**：Qwen3-8B 微调做 KnowledgeAgent 主模型（vLLM V1 + FP8 + EAGLE-3 部署），Qwen3-0.6B 本地 Ollama 做 Intent Router，月度 Token 成本降 70%。
> 5. **三件套评估 + 全链路可观测**：G-Eval + RAGAS + Trajectory Eval 离线回归，Langfuse + OTel GenAI Semantic Conv v1.30 在线 Trace，每一次调用的 Prompt/工具/Token/延迟都可追溯。
>
> 最终效果：从传统 2-4 小时人工流程压缩到 5 分钟内完成，人工只需最后一步确认合并代码。"