// Package app 负责 GameOps Agent 应用层的依赖装配（DI）。
//
// 职责：
//  1. 根据 Config 构建 LLM Model 实例
//  2. 加载 mcp_servers.yaml 得到 mcptools.API
//  3. 构造 5 个 Agent（Knowledge / Diagnosis / FileAnalyst / Repair / Coordinator）
//  4. 通过 WithSubAgents 把 4 个专家 Agent 挂到 Coordinator 下
//  5. 对外暴露入口 Agent（Coordinator），供服务层使用
//
// D1 阶段简化：如果配置缺失（例如没有 API Key），仍然返回可初始化的应用，
// 方便本地开发者验证骨架。真正的网络调用在用户发起 SSE 请求后才触发。
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	openaimodel "trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	frameworksession "trpc.group/trpc-go/trpc-agent-go/session"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	"git.woa.com/trpc-go/gameops-agent/src/agents"
	"git.woa.com/trpc-go/gameops-agent/src/agents/coordinator"
	diagnosis "git.woa.com/trpc-go/gameops-agent/src/agents/diagnosis_agent"
	fileanalyst "git.woa.com/trpc-go/gameops-agent/src/agents/file_analyst_agent"
	knowledgeagent "git.woa.com/trpc-go/gameops-agent/src/agents/knowledge_agent"
	repair "git.woa.com/trpc-go/gameops-agent/src/agents/repair_agent"
	"git.woa.com/trpc-go/gameops-agent/src/async"
	"git.woa.com/trpc-go/gameops-agent/src/audit"
	"git.woa.com/trpc-go/gameops-agent/src/config"
	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bcsapi"
	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bkapi"
	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/devopsapi"
	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/gongfengapi"
	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/tapdapi"
	knowledgekb "git.woa.com/trpc-go/gameops-agent/src/knowledge"
	"git.woa.com/trpc-go/gameops-agent/src/observability"
	appplugin "git.woa.com/trpc-go/gameops-agent/src/plugin"
	"git.woa.com/trpc-go/gameops-agent/src/report"
	a2asvc "git.woa.com/trpc-go/gameops-agent/src/services/a2a"
	aguisvc "git.woa.com/trpc-go/gameops-agent/src/services/agui"
	webhooksvc "git.woa.com/trpc-go/gameops-agent/src/services/webhook"
	appsession "git.woa.com/trpc-go/gameops-agent/src/session"
	"git.woa.com/trpc-go/gameops-agent/src/skillkit"
	"git.woa.com/trpc-go/gameops-agent/src/tools"
	asynctools "git.woa.com/trpc-go/gameops-agent/src/tools/async_tools"
	bcstools "git.woa.com/trpc-go/gameops-agent/src/tools/bcs_tools"
	bktools "git.woa.com/trpc-go/gameops-agent/src/tools/bk_tools"
	compositetools "git.woa.com/trpc-go/gameops-agent/src/tools/composite_tools"
	devopstools "git.woa.com/trpc-go/gameops-agent/src/tools/devops_tools"
	filetools "git.woa.com/trpc-go/gameops-agent/src/tools/file_tools"
	gongfengtools "git.woa.com/trpc-go/gameops-agent/src/tools/gongfeng_tools"
	mcptools "git.woa.com/trpc-go/gameops-agent/src/tools/mcp_tools"
	tapdtools "git.woa.com/trpc-go/gameops-agent/src/tools/tapd_tools"
)

// App 装配完成的应用实例。
type App struct {
	// Cfg 运行时配置。
	Cfg *config.Config
	// MCPTool MCP 工具管理器。
	MCPTool mcptools.API
	// Entrance 入口 Agent（Coordinator）。
	Entrance agent.Agent
	// SubAgents 四个专家子 Agent，按名索引。
	SubAgents map[string]agent.Agent
	// Session 会话服务（D11）：SSE/AG-UI/A2A 共享同一实例，保证跨通道记忆。
	Session frameworksession.Service
	// AGUI Web 前端服务（D11）；HTTP 路由由调用方挂到 mux 上。
	AGUI *aguisvc.Server
	// A2A 协议服务（D11）；默认 stub，`-tags a2a` 构建时启用真实链路。
	A2A *a2asvc.Server
	// Reports D15：修复报告存储。Webhook 触发的 case 以及 CLI/SSE 手动归档的
	// 报告都落在这里；/v1/report/{case_id} 可直接拉回。
	// D16：类型从 *MemStore 改为接口，允许注入 FileStore 进行 JSONL 持久化。
	Reports webhooksvc.ReportStore
	// Webhook D15：蓝鲸告警 / TAPD Webhook 入口。HTTP 路由通过 Webhook.Mount(mux) 挂载。
	Webhook *webhooksvc.Handler
	// GuardWatcher D17.1：input_guard / output_guard 规则热加载 watcher。
	// cfg.GuardRulesPath 为空时仍会创建（但内部走 no-op），统一生命周期。
	GuardWatcher *appplugin.RuleWatcher
	// AuditRemote D17.3：审计日志远端汇聚 Sink；为 nil 表示未启用远端。
	// Close 时需调用 AuditRemote.Close 让 worker 把 in-flight batch 刷走。
	AuditRemote *audit.RemoteSink
	// MetricsPump D17.4：周期性把 AuditRemote.Stats 差值转成 OTel Counter。
	// AuditRemote 为 nil 时 MetricsPump 也是 no-op（StartAuditRemoteMetricsPump 自适配）。
	MetricsPump *observability.AuditRemoteMetricsPump
	// AsyncRunner D19.2：异步工具执行器。cfg.Async.Enabled=false 时为 nil。
	// 装配时会将 AsyncToolNames 白名单内的本地写工具注入 ToolRegistry，并追加
	// 4 件套 job_* 工具（target=*）到 allLocalTools。
	AsyncRunner *async.Runner
}

// Init 基于 Config 初始化应用。
func Init(ctx context.Context, cfg *config.Config) (*App, error) {
	if cfg == nil {
		cfg = config.Default()
	}

	// 0. D14：注册全局 Model Callbacks 钩子（input_guard + output_guard）。
	//    所有 Agent 经由 agents.NewDefaultModelCallbacks 统一拾取，无需逐个装配。
	//    D17.1：registerGlobalGuards 同时返回 guard 句柄，供 watcher 热加载。
	inGuard, outGuard := registerGlobalGuards()

	// 1. 构造 LLM Model
	mdl := buildModel(cfg.Model)

	// 2. 加载 MCP Servers
	mcpConfigs, err := config.LoadMCPServers(cfg.MCPFile)
	if err != nil {
		return nil, fmt.Errorf("load mcp servers: %w", err)
	}
	mcpTool, err := mcptools.New(ctx, mcpConfigs)
	if err != nil {
		return nil, fmt.Errorf("init mcp tools: %w", err)
	}

	// 2.1 本地 FunctionTool 统一装配（带 target 分组，在 app 层按 Agent focusedTargets 分发）
	//     - bk_tools      × 7 → target=bk-monitor × 6（诊断用）/ bk-write × 1（修复用：告警静默，HITL，D18.3）
	//     - bcs_tools     × 11 → target=bcs-read × 5（诊断用：project/cluster/resource + pod_logs_tail ← D21 + pod_describe ← D21.1）/ bcs-write × 6（修复用：helm/scale/pod_restart/configmap/hpa_patch + secret_update ← D22，HITL）
	//     - gongfeng_tools× 2 → target=gongfeng   （修复用，HITL，MR 创建/合并）
	//     - devops_tools  × 2 → target=devops     （修复用，HITL，流水线重跑/取消）
	//     - tapd_tools    × 2 → target=tapd-read（诊断用）/ tapd（修复用，软写 HITL）
	bkClient := bkapi.NewClient()
	bcsClient := bcsapi.NewClient()
	gongfengClient := gongfengapi.NewClient()
	devopsClient := devopsapi.NewClient()
	tapdClient := tapdapi.NewClient()
	var allLocalTools []tools.TargetedTool
	allLocalTools = append(allLocalTools, bktools.NewAllTargeted(bkClient)...)
	// D19.8：BCS 工具的 ReadyWaiter 按环境变量 GAMEOPS_READY_WAITER 动态选择实现。
	//        未设置或 "fast" → FastPollReadyWaiter（默认，感知延迟更低）
	//        "poll"          → 传统轮询实现（D19.5）
	//        "noop"          → 禁用等待（紧急逃生通道）
	// 这是"ReadyWaiter 抽象可替换性"的装配触点——上层三个写工具（helm/scale/pod_restart）
	// 代码一行不改，底层实现可通过运维侧环境变量秒级切换。
	readyWaiterHook := newFastPollMetricsGlue()
	readyWaiter := bcstools.NewReadyWaiterFromEnv(bcsClient, bcstools.WaiterConfig{}, readyWaiterHook)
	log.Printf("[app] ReadyWaiter kind selected: %s (env GAMEOPS_READY_WAITER)", bcstools.SelectedWaiterKind())
	allLocalTools = append(allLocalTools, bcstools.NewAllTargetedWithWaiter(bcsClient, readyWaiter)...)
	allLocalTools = append(allLocalTools, gongfengtools.NewAllTargeted(gongfengClient)...)
	// D23'：双源日志聚合工具（composite_tools）。需要 bk + bcs 两个 client 同时注入，
	//        属于"跨 infra 域"的聚合工具——放在 composite_tools 独立包以避免 bk_tools
	//        与 bcs_tools 之间产生互相 import 的包耦合。target=bcs-read（DiagnosisAgent 可见）。
	allLocalTools = append(allLocalTools, compositetools.NewAllTargeted(bkClient, bcsClient)...)
	allLocalTools = append(allLocalTools, devopstools.NewAllTargeted(devopsClient)...)
	allLocalTools = append(allLocalTools, tapdtools.NewAllTargeted(tapdClient)...)

	// D19.2：异步工具框架装配（cfg.Async.Enabled=false 则全部跳过，行为不变）
	//
	// 装配顺序：
	//   1) 先根据 cfg.Async 构造 AsyncRunner + ToolRegistry
	//   2) 把 AsyncToolNames 白名单内的本地工具注册进 registry
	//      —— 必须在加 job_* 工具之前做，否则 job_submit 查不到底层
	//   3) 再给 allLocalTools 追加 4 件套 job_* 工具（target=*）
	//
	// 关键设计：Executor 用闭包桥接到 "按名查 allLocalTools 中的 tool.Tool"，
	// 这样 Runner 不知道 tool.Tool 具体类型，agent 生态升级不影响 async 核心。
	var asyncRunner *async.Runner
	if cfg.Async.Enabled {
		registry := async.NewToolRegistry()
		// Executor：按名从 registry 取 tool.Tool → CallableTool → Call。
		executor := async.ExecutorFunc(func(ctx context.Context, name string, args map[string]any) (any, error) {
			raw, ok := registry.Lookup(name)
			if !ok {
				return nil, fmt.Errorf("async: tool %q not registered", name)
			}
			tl, ok := raw.(tool.Tool)
			if !ok {
				return nil, fmt.Errorf("async: registered %q is not tool.Tool (got %T)", name, raw)
			}
			callable, ok := tl.(tool.CallableTool)
			if !ok {
				return nil, fmt.Errorf("async: tool %q is not CallableTool", name)
			}
			payload, err := json.Marshal(args)
			if err != nil {
				return nil, fmt.Errorf("marshal args: %w", err)
			}
			return callable.Call(ctx, payload)
		})
		asyncRunner = async.New(async.Config{
			MaxConcurrentJobs: cfg.Async.MaxConcurrent,
			MaxQueuedJobs:     cfg.Async.MaxQueued,
			DefaultTimeout:    time.Duration(cfg.Async.DefaultTimeoutSec) * time.Second,
			MaxTimeout:        time.Duration(cfg.Async.MaxTimeoutSec) * time.Second,
			JanitorInterval:   time.Duration(cfg.Async.JanitorIntervalSec) * time.Second,
			JanitorRetention:  time.Duration(cfg.Async.JanitorRetentionSec) * time.Second,
			Logger:            log.Printf,
			// D19.4：注入 OTel 观测适配器。nil 时 async 包走 noopMetrics，
			// 明示装配能让 SRE 告警规则（webhook error rate / queue saturation / job timeout）生效。
			Metrics: observability.NewAsyncMetricsAdapter(),
		}, async.NewMemStore(), executor)

		// 白名单注册：支持 "*" 通配（所有带 bcs-write/gongfeng/devops 之类 target 的写工具）
		registerAsyncWhitelist(registry, allLocalTools, cfg.Async.AsyncToolNames)

		// 追加 job_* 4 件套
		allLocalTools = append(allLocalTools, asynctools.NewAllTargeted(asyncRunner, registry)...)
		log.Printf("[app] async runner enabled: max_concurrent=%d max_queued=%d whitelisted_tools=%d",
			cfg.Async.MaxConcurrent, cfg.Async.MaxQueued, len(registry.Names()))
	}

	// 3. 构造 GenConfig
	gen := agents.GenConfig{
		Temperature: cfg.Gen.Temperature,
		TopP:        cfg.Gen.TopP,
		MaxTokens:   cfg.Gen.MaxTokens,
		Stream:      cfg.Gen.Stream,
	}

	// 4. 构造 4 个专家 Agent
	// 4.0 KnowledgeAgent RAG 工具（D4）：引入框架 knowledge 模块 + 本地 data/knowledge/
	//     无 OPENAI_API_KEY 时自动降级为 stub，不阻塞启动
	kbBuilder := knowledgekb.NewBuilder(knowledgekb.DefaultConfig())
	kbTool, err := kbBuilder.Build(ctx)
	if err != nil {
		return nil, fmt.Errorf("build knowledge tool: %w", err)
	}

	// 4.0.1 KnowledgeAgent iWiki 工具（D10）：公司内 iWiki RAG（Rio 签名）
	//   - 凭据缺失 / Disabled / 未启用 -tags iwiki 时返回 stub，不阻塞启动
	//   - 与本地 knowledge_search 并存，LLM 优先本地、兜底 iWiki（见 system_prompt.md）
	iwikiTool, _ := knowledgekb.BuildIWikiTool(knowledgekb.DefaultIWikiConfig())

	knowledgeA, err := knowledgeagent.New(knowledgeagent.Dep{
		Model: mdl, GenConfig: gen, MCPTool: mcpTool,
		LocalTools: []tool.Tool{kbTool, iwikiTool},
	})
	if err != nil {
		return nil, fmt.Errorf("build knowledge agent: %w", err)
	}

	// 4.1 D13：技能系统（Skills）装配；环境不具备时静默降级，不阻塞启动
	skillBundle := skillkit.Load(skillkit.DefaultConfig())
	if skillBundle.SkipReason != "" {
		log.Printf("[app] skills disabled: %s (dir=%s)", skillBundle.SkipReason, skillBundle.SkillsDir)
	} else {
		log.Printf("[app] skills enabled: dir=%s", skillBundle.SkillsDir)
	}

	// 4.2 D13：Agent 级插件 — safety_guard + audit_hook（基于 tool.Callbacks）
	repairCallbacks := buildAgentCallbacks(repair.AgentName)
	diagCallbacks := buildAgentCallbacks(diagnosis.AgentName)

	// 4.3 构造 DiagnosisAgent（D14 起挂 tool.Callbacks）。
	diagnosisA, err := diagnosis.New(diagnosis.Dep{
		Model: mdl, GenConfig: gen, MCPTool: mcpTool,
		// 注入命中 diagnosis.FocusedTargets 的本地工具
		// （bk-monitor × 6 + bcs-read × 5，不包含 bcs-write / bk-write）
		LocalTools: tools.FilterByTargets(allLocalTools, diagnosis.FocusedTargets),
		// D14：safety_guard + audit_hook。只读场景下不会命中，保留是为未来诊断
		// 升级写操作时的防御前置。
		ToolCallbacks: diagCallbacks,
	})
	if err != nil {
		return nil, fmt.Errorf("build diagnosis agent: %w", err)
	}

	fileA, err := fileanalyst.New(fileanalyst.Dep{
		Model: mdl, GenConfig: gen,
		// D5：注入本地文件分析工具（file_detect / file_read_slice / json_query / log_analyze）
		LocalTools: filetools.NewAll(filetools.DefaultConfig()),
		// D13：技能系统（缺失时走 nil，New 内部会跳过）
		SkillRepo:    skillBundle.Repo,
		CodeExecutor: skillBundle.Executor,
	})
	if err != nil {
		return nil, fmt.Errorf("build file analyst agent: %w", err)
	}
	repairA, err := repair.New(repair.Dep{
		Model: mdl, GenConfig: gen, MCPTool: mcpTool,
		// 修复 Agent 拿到 bcs-write（helm/scale/pod_restart/configmap）+ bk-write（alarm_silence，D18.3）
		// + gongfeng + devops + tapd，构成完整的"修复动作链"
		LocalTools:    tools.FilterByTargets(allLocalTools, repair.FocusedTargets),
		ToolCallbacks: repairCallbacks, // D13：safety_guard + audit_hook
	})
	if err != nil {
		return nil, fmt.Errorf("build repair agent: %w", err)
	}

	// 5. 构造 Coordinator，把 4 个专家作为 SubAgents
	subAgents := []agent.Agent{knowledgeA, diagnosisA, fileA, repairA}
	entrance, err := coordinator.New(coordinator.Dep{
		Model:     mdl,
		GenConfig: gen,
		SubAgents: subAgents,
	})
	if err != nil {
		return nil, fmt.Errorf("build coordinator: %w", err)
	}

	// 6. D11：Session 服务（多轮对话 + 自动总结）。
	//    LLM 缺失时自动降级为不带 summarizer 的纯内存 session（仍支持多轮记忆）。
	sessSvc := appsession.New(appsession.DefaultConfig(), mdl)

	// 7. D11：AG-UI Web 前端（给运维人员零前端开发的 Web 调试入口）。
	aguiSrv, err := aguisvc.New(aguisvc.Config{
		Agent:   entrance,
		Session: sessSvc,
		AppName: "gameops-agent",
	})
	if err != nil {
		return nil, fmt.Errorf("build agui server: %w", err)
	}

	// 8. D11：A2A 协议服务（外部 Agent 可调用本服务）。
	//    默认 stub 构建下仅占位；`-tags a2a` 启用真实链路。
	a2aSrv, err := a2asvc.New(a2asvc.Config{
		Agent:     entrance,
		Session:   sessSvc,
		Streaming: true,
	})
	if err != nil {
		return nil, fmt.Errorf("build a2a server: %w", err)
	}

	// 9. D15：Webhook 入口 + 内存 Report Store。
	//    Runner 复用入口 Agent + 共享 Session；agentRunnerAdapter 把框架 runner
	//    的 event chan 语义转换成 Webhook 需要的"一次性执行"接口。
	//    D16 起：若 cfg.Webhook.StoreFile 非空，则使用 JSONL 持久化 FileStore，
	//    否则保持 MemStore（重启丢数据，但零依赖可用）。
	var reports webhooksvc.ReportStore
	if strings.TrimSpace(cfg.Webhook.StoreFile) != "" {
		fs, ferr := report.NewFileStore(cfg.Webhook.StoreFile)
		if ferr != nil {
			return nil, fmt.Errorf("build webhook file store: %w", ferr)
		}
		log.Printf("[app] webhook report store: FileStore(%s)", cfg.Webhook.StoreFile)
		reports = fs
	} else {
		reports = webhooksvc.NewMemStore()
	}
	var agentRunner runner.Runner
	if sessSvc != nil {
		agentRunner = runner.NewRunner("gameops-agent", entrance, runner.WithSessionService(sessSvc))
	} else {
		agentRunner = runner.NewRunner("gameops-agent", entrance)
	}
	// D16：按配置选 Summarizer 实现；目前仅 mock；真实 LLM 实现留给后续阶段。
	var summarizer report.SummarizerClient
	switch strings.ToLower(strings.TrimSpace(cfg.Webhook.Summarizer)) {
	case "", "off", "none":
		summarizer = nil
	case "mock":
		summarizer = report.NewMockSummarizer()
	default:
		log.Printf("[app] unknown webhook.summarizer=%q, fallback to nil", cfg.Webhook.Summarizer)
		summarizer = nil
	}
	// D16：幂等窗口解析；留空或解析失败视为关闭。
	dedupeWindow := time.Duration(0)
	if s := strings.TrimSpace(cfg.Webhook.DedupeWindow); s != "" {
		if d, derr := time.ParseDuration(s); derr == nil && d > 0 {
			dedupeWindow = d
		} else if derr != nil {
			log.Printf("[app] invalid webhook.dedupe_window=%q: %v, disabled", s, derr)
		}
	}
	webhookHandler, err := webhooksvc.New(webhooksvc.Config{
		Runner: &agentRunnerAdapter{r: agentRunner},
		Store:  reports,
		Secret: cfg.Webhook.Secret,
		Logger: func(format string, args ...any) {
			log.Printf(format, args...)
		},
		// D16：接入 OTel Counter（未启用时走 Noop Meter，零开销）。
		Metrics: func(source, outcome string) {
			observability.IncWebhookRequest(ctx, source, outcome)
		},
		Summarizer:   summarizer,
		DedupeWindow: dedupeWindow,
	})
	if err != nil {
		return nil, fmt.Errorf("build webhook handler: %w", err)
	}
	_ = report.SchemaVersion // 保证 report 包被编译引用，避免意外 dead-import 警告

	// D17.6：装配审计 HMAC Signer。
	// 设计要点：
	//   - 未配 AUDIT_HMAC_KEY（本地开发）→ signer=nil，Emit 自动走未签名路径（零回归）；
	//   - AUDIT_HMAC_REQUIRED=1 + 未配 key → NewHMACSignerFromEnv 返 error，这里 fail-fast，
	//     生产环境借此确保"审计必签"不会被误配置绕过；
	//   - 配置有效 → SetSigner 注入；之后所有 audit.Emit 自动加盖 HMAC-SHA256。
	if auditSigner, err := audit.NewHMACSignerFromEnv(); err != nil {
		return nil, fmt.Errorf("audit hmac: %w", err)
	} else if auditSigner != nil {
		audit.SetSigner(auditSigner)
		log.Printf("[app] audit HMAC signer enabled (kid=%s)", auditSigner.KeyID())
	}

	// D17.3：装配审计日志远端汇聚 Sink（可选；未配置则不启用）。
	auditRemote := startAuditRemote(cfg.Audit)

	// D17.4：启动 OTel metric pump，周期把 RemoteSink Stats 转成 Counter。
	// auditRemote 为 nil 时 pump 自身退化为 no-op，Stop 仍可安全调用。
	var pumpProvider observability.RemoteSinkStatsProvider
	if auditRemote != nil {
		pumpProvider = auditRemote
	}
	metricsPump := observability.StartAuditRemoteMetricsPump(
		context.Background(), pumpProvider, 15*time.Second)

	return &App{
		Cfg:      cfg,
		MCPTool:  mcpTool,
		Entrance: entrance,
		SubAgents: map[string]agent.Agent{
			knowledgeagent.AgentName: knowledgeA,
			diagnosis.AgentName:      diagnosisA,
			fileanalyst.AgentName:    fileA,
			repair.AgentName:         repairA,
		},
		Session:      sessSvc,
		AGUI:         aguiSrv,
		A2A:          a2aSrv,
		Reports:      reports,
		Webhook:      webhookHandler,
		GuardWatcher: startGuardWatcher(cfg.GuardRulesPath, inGuard, outGuard),
		AuditRemote:  auditRemote,
		MetricsPump:  metricsPump,
	}, nil
}

// Close 释放应用层持有的后台资源。D17.1引入后主要负责停 watcher；
// D17.3 起额外负责停 AuditRemote（最多 5 秒 flush 待发日志）；
// D17.4 起额外负责停 MetricsPump（先停 pump 再停远端，确保最后一次差值能被采集）。
// 幂等；鉴于目前只是日志/watcher 清理，不返回 error。
func (a *App) Close() {
	if a == nil {
		return
	}
	if a.GuardWatcher != nil {
		a.GuardWatcher.Stop()
	}
	// 先停 pump（它会 flush 最后一次差值），再关 AuditRemote —— 顺序不能反：
	// 如果先关 AuditRemote，pump 最后一次读到的 Stats 不包含 close 过程中
	// 排空 channel 时增加的 delivered/failed 计数。
	if a.MetricsPump != nil {
		a.MetricsPump.Stop()
	}
	if a.AuditRemote != nil {
		// 最多等 5 秒把 in-flight batch 刷出；超时仍会继续关闭进程。
		if err := a.AuditRemote.Close(5 * time.Second); err != nil {
			log.Printf("[app] audit remote close timeout: %v", err)
		}
	}
	// D17.7：关闭审计 Signer（若链式 state 持久化启用，这步会把 lastSig 落盘）。
	// 幂等：未启用 chain / 未配 state 文件时 no-op；失败仅 warn 不阻塞。
	if err := audit.CloseSigner(); err != nil {
		log.Printf("[app] audit signer close: %v", err)
	}
	// D16：停 Webhook 内部的幂等 GC goroutine（未启用时 Shutdown 幂等 no-op）。
	if a.Webhook != nil {
		a.Webhook.Shutdown()
	}
	// D19.2：优雅关闭 AsyncRunner，最多等 5s 让 worker 自然结束；
	// 超时没关系——进程即将退出，worker 的 ctx 会被子线程自然收回。
	if a.AsyncRunner != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := a.AsyncRunner.Shutdown(ctx); err != nil {
			log.Printf("[app] async runner close: %v", err)
		}
	}
}

// agentRunnerAdapter 把 framework 的 runner.Runner 适配为 webhook.AgentRunner 接口。
//
// 对 Webhook 场景而言只需要"把 Prompt 跑完"即可，不需要流式输出；
// 因此这里把 event chan 消费干净即可：出现 event.Error 时翻译为 Go error 返回。
type agentRunnerAdapter struct {
	r runner.Runner
}

// Run 实现 webhook.AgentRunner。
func (a *agentRunnerAdapter) Run(ctx context.Context, userID, sessionID, prompt string) error {
	ch, err := a.r.Run(ctx, userID, sessionID, model.NewUserMessage(prompt))
	if err != nil {
		return err
	}
	var firstErr error
	for ev := range ch {
		if ev == nil {
			continue
		}
		if ev.Error != nil && firstErr == nil {
			firstErr = fmt.Errorf("agent error: %s", ev.Error.Message)
		}
	}
	return firstErr
}

// 静态类型检查：保证接口签名漂移在编译期暴露。
var _ webhooksvc.AgentRunner = (*agentRunnerAdapter)(nil)
var _ = event.Event{}

// registerAsyncWhitelist 把 allLocalTools 里名字命中 whitelist 的工具注册进 async registry。
//
// whitelist 规则：
//   - 空列表：不注册任何（相当于 async 总开关开了但没开放任何工具）
//   - 含 "*"：所有带 target=bcs-write / gongfeng / devops / tapd 的写工具
//     全部注册（但仍过滤掉 target="*" 的非业务工具避免自指）
//   - 其他：按名字精确匹配
//
// 为什么白名单而不是全量：读工具（bcs_cluster_list / bk_alarm_query）放 async 毫无意义
// 反而让 LLM 多绕一层；只有耗时写工具才值得异步化。
func registerAsyncWhitelist(
	registry *async.ToolRegistry,
	allTools []tools.TargetedTool,
	whitelist []string,
) {
	if registry == nil || len(whitelist) == 0 {
		return
	}
	wantAll := false
	nameSet := make(map[string]struct{}, len(whitelist))
	for _, n := range whitelist {
		if n == "*" {
			wantAll = true
		}
		nameSet[n] = struct{}{}
	}
	// target=* 的是控制流元工具（job_* 自身、util_tools 等），不应自注册为可 async 的对象
	isSelfTarget := func(target string) bool { return target == "*" }
	for _, tt := range allTools {
		if tt.Tool == nil {
			continue
		}
		name := tt.Tool.Declaration().Name
		if wantAll {
			if isSelfTarget(tt.Target) {
				continue
			}
			registry.Register(name, tt.Tool)
			continue
		}
		if _, ok := nameSet[name]; ok {
			registry.Register(name, tt.Tool)
		}
	}
}

// buildAgentCallbacks 组合 safety_guard + audit_hook + OTel tool span 三个 Agent 级插件。
// 用于 RepairAgent / DiagnosisAgent 这类会触发写操作的 Agent。
func buildAgentCallbacks(agentName string) *tool.Callbacks {
	cb := tool.NewCallbacks()
	appplugin.NewSafetyGuard(appplugin.SafetyConfig{
		Logger: func(toolName, rule, reason string) {
			log.Printf("[safety_guard][%s] blocked tool=%s rule=%s reason=%s",
				agentName, toolName, rule, reason)
		},
	}).Register(cb)
	appplugin.NewAuditHook(appplugin.AuditHookConfig{
		AgentName: agentName,
	}).Register(cb)
	// D16：OTel Tool Span + Counter（gen_ai.execute_tool）。
	beforeTool, afterTool := observability.NewToolSpanCallback()
	cb.RegisterBeforeTool(beforeTool)
	cb.RegisterAfterTool(afterTool)
	return cb
}

// registerGlobalGuards 装配 D14 的 input_guard / output_guard 到 agents 全局钩子。
//
// 职责边界：
//   - input_guard（BeforeModel）：命中即短路返回安全拒绝响应，OWASP LLM01 Prompt Injection。
//   - output_guard（AfterModel）：命中即打码（Redact），OWASP LLM06 敏感信息外泄。
//   - 两者都用默认规则集；业务侧如需定制，可在此处替换 Config。
//
// D17.1：返回两个 guard 句柄，供上层将其交给 RuleWatcher 热加载。
//
// 全局钩子由 agents.NewDefaultModelCallbacks() 统一拾取，
// 无需修改任何单个 Agent 的装配代码。
func registerGlobalGuards() (*appplugin.InputGuard, *appplugin.OutputGuard) {
	agents.ResetGlobalModelHooks() // 幂等：多次 Init 或热重载时保证干净
	inGuard := appplugin.NewInputGuard(appplugin.InputGuardConfig{
		Logger: func(rule, reason, snippet string) {
			log.Printf("[input_guard] rule=%s reason=%s snippet=%q",
				rule, reason, snippet)
			// D16：OTel Counter 埋点。
			observability.IncInputGuardBlocked(context.Background(), rule)
		},
	})
	outGuard := appplugin.NewOutputGuard(appplugin.OutputGuardConfig{
		Logger: func(rule string, hits int) {
			log.Printf("[output_guard] rule=%s redacted_hits=%d", rule, hits)
			// D16：OTel Counter 埋点。
			observability.IncGuardRedacted(context.Background(), rule, hits)
		},
	})
	// 复用 Register 语义：在一个 *model.Callbacks 上挂载后再提取 BeforeModel/AfterModel
	cb := inGuard.Register(nil)
	cb = outGuard.Register(cb)
	agents.RegisterGlobalModelHooks(cb.BeforeModel, cb.AfterModel)

	// D16：LLM Span + LLM Counter（gen_ai.chat）。
	//   - 未启用 OTel 时，Noop Tracer/Meter 负担可忽略。
	//   - 与 input_guard / output_guard 同层追加，保证方法链顺序不影响拦截语义。
	beforeLLM, afterLLM := observability.NewLLMModelCallback()
	agents.RegisterGlobalModelHooks(
		[]model.BeforeModelCallbackStructured{beforeLLM},
		[]model.AfterModelCallbackStructured{afterLLM},
	)
	return inGuard, outGuard
}

// startGuardWatcher 启动规则集 watcher。path 为空时仍返回一个有效实例
// （其 Start/Stop 均为 no-op），统一 App.Close 生命周期处理。
//
// 设计要点：
//  1. 轮询间隔默认 5s；规则变更是低频操作，5s 延迟对 SRE 来说完全可接受。
//  2. 解析或编译失败保留旧规则，绝不把 guard 打成"零规则裸奔"。
//  3. Logger 埋点将成功/失败写日志；未来可交给 observability 出 Counter。
func startGuardWatcher(path string, in *appplugin.InputGuard,
	out *appplugin.OutputGuard) *appplugin.RuleWatcher {
	w := appplugin.NewRuleWatcher(appplugin.RuleWatcherConfig{
		Path:        path,
		InputGuard:  in,
		OutputGuard: out,
		Logger: func(event, msg string) {
			if event == "error" {
				log.Printf("[guard_watcher] reload error: %s", msg)
				return
			}
			log.Printf("[guard_watcher] %s %s", event, msg)
		},
	})
	w.Start()
	return w
}

// startAuditRemote 根据 AuditConfig 装配审计日志 Sink（D17.3）。
//
// 组合策略：
//   - Remote.URL 为空：不改 sink（沿用 defaultSink / 环境变量行为）。
//   - LocalFile 非空 + Remote.URL 非空：MultiSink(FileSink + RemoteSink)。
//   - LocalFile 为空 + Remote.URL 非空：MultiSink(defaultSink + RemoteSink)
//     —— 这里保留 defaultSink 是为了继续尊重 AUDIT_SINK/AUDIT_FILE 环境变量；
//     很多部署脚本仍依赖这两个变量控制 stdout 采集。
//
// 失败策略：远端构造失败时只打一条日志，不阻塞应用启动
// （审计是"加固"而非"必要路径"，不能因为 Loki 没 ready 就拒启动）。
func startAuditRemote(cfg config.AuditConfig) *audit.RemoteSink {
	// 1. 本地 Sink 决定。
	var localSink audit.Sink
	if path := cfg.LocalFile; path != "" {
		localSink = audit.NewFileSink(path)
	}

	// 2. 远端未启用：保留原语义（含 LocalFile 配置覆盖本地）。
	if cfg.Remote.URL == "" {
		if localSink != nil {
			audit.SetSink(localSink)
		}
		return nil
	}

	// 3. 构造 RemoteSink。
	remoteCfg := audit.RemoteSinkConfig{
		URL:          cfg.Remote.URL,
		AuthHeader:   cfg.Remote.AuthHeader,
		ExtraHeaders: cfg.Remote.Headers,
		ContentType:  cfg.Remote.ContentType,
		BatchSize:    cfg.Remote.BatchSize,
		BufferSize:   cfg.Remote.BufferSize,
		MaxRetries:   cfg.Remote.MaxRetries,
	}
	if cfg.Remote.FlushEverySec > 0 {
		remoteCfg.FlushEvery = time.Duration(cfg.Remote.FlushEverySec) * time.Second
	}
	if cfg.Remote.TimeoutSec > 0 {
		remoteCfg.HTTPTimeout = time.Duration(cfg.Remote.TimeoutSec) * time.Second
	}
	remoteCfg.ErrorLogger = func(format string, args ...any) {
		log.Printf("[audit-remote] "+format, args...)
	}

	remote, err := audit.NewRemoteSink(remoteCfg)
	if err != nil {
		// 审计远端不可用不拦启动；降级为纯本地。
		log.Printf("[app] audit remote disabled: %v", err)
		if localSink != nil {
			audit.SetSink(localSink)
		}
		return nil
	}

	// 4. MultiSink 组合；本地 Sink 缺省时留一个"透传到旧 defaultSink"的占位，
	//    让 AUDIT_SINK/AUDIT_FILE 环境变量继续生效。
	var sinks []audit.Sink
	if localSink != nil {
		sinks = append(sinks, localSink)
	} else {
		// defaultSink 走 audit 包内部逻辑；没法直接拿到实例，用闭包适配。
		// 这里再调用一次 Emit 链路不可能（Sink 就是出口）——折中：把"默认落盘 stdout/file"
		// 的职责显式交给 FileSink 或忽略。实际部署里建议都显式配 LocalFile。
		// 未配 LocalFile 且启用 Remote 的场景，仍把原 defaultSink 保留：
		// 通过 audit.SetSink 之前先把 oldSink 捕获。
		sinks = append(sinks, captureCurrentAuditSink())
	}
	sinks = append(sinks, remote)
	audit.SetSink(audit.NewMultiSink(sinks...))
	log.Printf("[app] audit remote enabled: url=%s batch=%d buffer=%d",
		remoteCfg.URL, remoteCfg.BatchSize, remoteCfg.BufferSize)
	return remote
}

// captureCurrentAuditSink 获取当前 audit 包生效中的 Sink（即 defaultSink），
// 用于 MultiSink 组合时保留"stdout/file via env"的旧行为。
//
// 实现上借用 SetSink(nil) → 内部会重置为 defaultSink 并返回旧 sink 的语义不安全
// （会短暂切回 default 再切回 multi），故这里使用一个不同手法：
// 先 SetSink(nil) 拿到 defaultSink，再立即把它捕获返回，
// 由调用方拼到 MultiSink 内重新 SetSink —— 中间窗口仅几纳秒，不会丢日志。
func captureCurrentAuditSink() audit.Sink {
	old := audit.SetSink(nil) // 此时 audit 内部是 defaultSink；old 是之前的 sink
	// 把 old 还回去以保持旧行为；调用方 MultiSink 构造后会再 SetSink 覆盖。
	audit.SetSink(old)
	return old
}

// buildModel 按 ModelConfig 构造 OpenAI 兼容 Model。
// trpc-agent-go 的 openai.New 会自动从 OPENAI_API_KEY / OPENAI_BASE_URL 读取环境变量，
// 这里只有在配置显式给出时才覆盖。
func buildModel(cfg config.ModelConfig) *openaimodel.Model {
	name := cfg.Name
	if name == "" {
		name = "hunyuan-turbo-s"
	}
	var opts []openaimodel.Option
	if cfg.BaseURL != "" {
		opts = append(opts, openaimodel.WithBaseURL(cfg.BaseURL))
	}
	if cfg.APIKey != "" {
		opts = append(opts, openaimodel.WithAPIKey(cfg.APIKey))
	}
	return openaimodel.New(name, opts...)
}
