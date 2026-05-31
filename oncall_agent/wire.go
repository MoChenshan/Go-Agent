//go:build wireinject
// +build wireinject

// Package main 启动oncall agent服务
package main

import (
	"context"
	"time"

	"github.com/google/wire"
	a2aserver "trpc.group/trpc-go/trpc-a2a-go/server"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	sagui "trpc.group/trpc-go/trpc-agent-go/server/agui"
	"trpc.group/trpc-go/trpc-agent-go/session"
	"trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/session/summary"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/webfetch/httpfetch"

	wujisdk "git.code.oa.com/trpc-go/trpc-config-wuji"
	"git.code.oa.com/trpc-go/trpc-database/gorm"
	"git.code.oa.com/trpc-go/trpc-database/mysql"
	"git.code.oa.com/trpc-go/trpc-go/log"
	pb "git.woa.com/trpcprotocol/magic/oncall_agent_oncall_agent_debug"
	"git.woa.com/video_pay_middle_platform/pay-go-comm/utils"

	cdkagent "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/agents/cdkagent"
	codeanalysis "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/agents/codeanalysis"
	magiconcallagent "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/agents/magiconcall"
	domainext "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/interfaces/external"
	domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
	cdkeyquery "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/tools/cdkeyquery"
	lingshanquery "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/tools/lingshanquery"
	logquery "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/tools/logquery"
	magictool "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/tools/magictool"
	mcptool "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/tools/mcptool"
	traceanalysis "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/tools/traceanalysis"
	"git.woa.com/video_pay_oss/magic_group/oncall_agent/infrastructure/config/rainbow"
	magicwuji "git.woa.com/video_pay_oss/magic_group/oncall_agent/infrastructure/config/wuji"
	cdkeycli "git.woa.com/video_pay_oss/magic_group/oncall_agent/infrastructure/external/http/cdkey"
	galileocli "git.woa.com/video_pay_oss/magic_group/oncall_agent/infrastructure/external/http/galileo"
	lingshancli "git.woa.com/video_pay_oss/magic_group/oncall_agent/infrastructure/external/http/lingshan"
	magiccli "git.woa.com/video_pay_oss/magic_group/oncall_agent/infrastructure/external/http/magiccli"
	conditionlog "git.woa.com/video_pay_oss/magic_group/oncall_agent/infrastructure/external/trpc/conditionlog"
	magicconfig "git.woa.com/video_pay_oss/magic_group/oncall_agent/infrastructure/repo/mysql/magic_config"
	"git.woa.com/video_pay_oss/magic_group/oncall_agent/services/a2a"
	"git.woa.com/video_pay_oss/magic_group/oncall_agent/services/agui"
	"git.woa.com/video_pay_oss/magic_group/oncall_agent/services/debug"
	"git.woa.com/video_pay_oss/magic_group/oncall_agent/services/sse"
	wecomsrv "git.woa.com/video_pay_oss/magic_group/oncall_agent/services/wecom"
	oncallutils "git.woa.com/video_pay_oss/magic_group/oncall_agent/utils"
)

// App holds all registered tRPC services
type App struct {
	A2AServers  map[string]*a2aserver.A2AServer
	SSEServers  map[string]sse.API
	AguiServers map[string]*sagui.Server
	WeComServer *wecomsrv.Server // 企微WeCom AI Bot服务, 可能为nil(未启用时)
	DebugSrv    pb.DebugService
}

// ---- infrastructure providers ----

func provideGenConfig(cfg rainbow.AppConfig) domainmodel.GenConfig {
	return domainmodel.GenConfig{
		Temperature: cfg.Temperature,
		TopP:        cfg.TopP,
	}
}

func provideModelInstance(cfg rainbow.AppConfig) *openai.Model {
	// 注册模型上下文窗口，供 token tailoring 使用
	if cfg.ModelContextWindow > 0 {
		model.RegisterModelContextWindow(cfg.OpenAIModelName, cfg.ModelContextWindow)
		log.Infof("registered model context window: model=%s, window=%d",
			cfg.OpenAIModelName, cfg.ModelContextWindow)
	}
	// 显式设置 token tailoring 参数，确保正确计算 maxOutputTokens
	// 默认值: ProtocolOverheadTokens=512, SafetyMarginRatio=0.10
	return openai.New(cfg.OpenAIModelName,
		openai.WithBaseURL(cfg.OpenAIBaseURL),
		openai.WithAPIKey(cfg.OpenAIAPIKey),
		openai.WithEnableTokenTailoring(true),
	)
}

func provideWujiCli() (domainmodel.WujiAPI, error) {
	wujiMCPCli, err := wujisdk.NewFilter("mcp_tool", []string{"valid"}, "valid=1", magicwuji.MCPTool{})
	if err != nil {
		return nil, err
	}
	agentConfigCli, err := wujisdk.NewFilter("agent_config", []string{"name"}, "is_valid=1", magicwuji.AgentConfig{})
	if err != nil {
		return nil, err
	}
	localToolCli, err := wujisdk.NewFilter("local_tool", []string{"name"}, "", magicwuji.LocalToolConfig{})
	if err != nil {
		return nil, err
	}
	return magicwuji.New(wujiMCPCli, agentConfigCli, localToolCli), nil
}

func provideMagicConfigAPI() (domainext.MagicConfigAPI, error) {
	// 创建测试库和正式库连接
	dbTest, err := gorm.NewClientProxy("trpc.magic.oncall_agent.magic_db_test")
	if err != nil {
		return nil, err
	}
	dbFormal, err := gorm.NewClientProxy("trpc.magic.oncall_agent.magic_db_formal")
	if err != nil {
		return nil, err
	}
	// 测试环境开启debug模式
	if !utils.IsFormalEnv() {
		dbTest = dbTest.Debug()
	}
	return magicconfig.New(dbTest, dbFormal), nil
}


func provideGalileoCli(cfg rainbow.AppConfig) domainext.GalileoAPI {
	return galileocli.New(cfg.BkAppCode, cfg.BkAppToken)
}

func provideLingshanCli(cfg rainbow.AppConfig) domainext.LingshanAPI {
	return lingshancli.New(cfg.XGatewaySecretID, cfg.XGatewaySecretKey)
}

func provideCdkeyCli(cfg rainbow.AppConfig) domainext.CdkeyAPI {
	return cdkeycli.New(cfg.ESUsername, cfg.ESPassword, cfg.FlowPath)
}

func provideConditionLogCli() domainext.ConditionLogAPI {
	return conditionlog.New()
}

func provideMagicCliAPI() domainext.MagicCliAPI {
	return magiccli.New()
}

// ---- domain tool providers ----
// Note: individual tool.Tool instances are NOT registered as Wire providers because Wire
// cannot disambiguate multiple providers with the same return type. Instead, each
// provide*AgentDep function receives the domain-client interfaces directly and
// builds its own []tool.Tool inline.

func provideMCPTool(wujiCli domainmodel.WujiAPI, cfg rainbow.AppConfig) (mcptool.API, error) {
	return mcptool.NewMCPToolImpl(context.Background(), wujiCli, &cfg)
}

func provideTraceConfig(cfg rainbow.AppConfig) traceanalysis.TraceConfig {
	return traceanalysis.TraceConfig{
		MaxTraceDepth:          cfg.MaxTraceDepth,
		MaxSpanNum:             cfg.MaxSpanNum,
		MaxSpanLogLength:       cfg.MaxSpanLogLength,
		SelfTeamName:           cfg.SelfTeamName,
		OtherTeamTruncateDepth: cfg.OtherTeamTruncateDepth,
	}
}

func provideMagicToolDep(w domainmodel.WujiAPI, mc domainext.MagicConfigAPI, m domainext.MagicCliAPI) magictool.Dep {
	return magictool.Dep{WujiCli: w, MagicConfigCli: mc, MagicCliAPI: m}
}

// getLocalToolDescWire is a helper (not a Wire provider) used inside provide*AgentDep functions.
func getLocalToolDescWire(wujiCli domainmodel.WujiAPI, name string) string {
	cfg := wujiCli.GetLocalToolConfig(name)
	if cfg != nil && cfg.Description != "" {
		return cfg.Description
	}
	return ""
}

// ---- agent dep providers ----
// Each function builds its own tool instances inline to avoid tool.Tool ambiguity in Wire.

// provideMagicAgentDep provides the unified magic oncall agent dependencies
// Merged from magiconcall and magic_config_agent
func provideMagicAgentDep(
	m *openai.Model,
	w domainmodel.WujiAPI,
	mcp mcptool.API,
	gc domainmodel.GenConfig,
	galileo domainext.GalileoAPI,
	lingshan domainext.LingshanAPI,
	condLog domainext.ConditionLogAPI,
	traceConfig traceanalysis.TraceConfig,
	magicToolDep magictool.Dep,
) (magiconcallagent.Dep, error) {
	// Tools from magiconcall
	traceTool := traceanalysis.New(traceanalysis.Dep{
		GalileoCli: galileo, LingshanCli: lingshan, ConditionLogCli: condLog, WujiCli: w, Cfg: traceConfig,
	})
	logQueryTool := logquery.New(logquery.Dep{GalileoCli: galileo, WujiCli: w})
	lingshanQueryTool := lingshanquery.New(lingshanquery.Dep{LingshanCli: lingshan, WujiCli: w})
	base64Tool := oncallutils.NewBase64Tool(getLocalToolDescWire(w, "base64_decode"))
	dt2tsTool := oncallutils.NewDateTimeToTimestampMSTool(getLocalToolDescWire(w, "date_time_to_timestamp_ms"))
	ts2dtTool := oncallutils.NewTimestampMSToDateTimeTool(getLocalToolDescWire(w, "timestamp_ms_to_date_time"))

	// Tools from magic_config_agent
	webFetchTool := httpfetch.NewTool(
		httpfetch.WithMaxContentLength(20000),
		httpfetch.WithMaxTotalContentLength(50000),
	)

	// Read-only tools for module configuration (no write permission)
	modTypeTool := magictool.NewMagicModTypeInfoTool(magicToolDep)
	actInfoTool := magictool.NewMagicActInfoTool(magicToolDep)
	// Note: proposeTool is NOT included - agent has read-only access to configs

	// Sub-agent: unified code analysis agent (span analysis + repo explanation)
	codeAnalysisTool, err := codeanalysis.NewCodeAnalysisAgentTool(codeanalysis.Dep{
		ModelInstance: m, WujiCli: w, MCPTool: mcp, GenConfig: gc,
		LocalTools: []tool.Tool{logQueryTool, lingshanQueryTool, dt2tsTool, ts2dtTool, base64Tool},
	})
	if err != nil {
		return magiconcallagent.Dep{}, err
	}

	return magiconcallagent.Dep{
		ModelInstance: m, WujiCli: w, MCPTool: mcp, GenConfig: gc,
		LocalTools: []tool.Tool{
			// Problem diagnosis tools
			traceTool, logQueryTool, codeAnalysisTool,
			base64Tool, lingshanQueryTool, dt2tsTool, ts2dtTool,
			// Configuration read-only tools
			modTypeTool, actInfoTool, webFetchTool,
		},
	}, nil
}

func provideCdkeyAgentDep(
	m *openai.Model,
	w domainmodel.WujiAPI,
	mcp mcptool.API,
	gc domainmodel.GenConfig,
	cdkey domainext.CdkeyAPI,
) cdkagent.Dep {
	cdkeyQueryTool := cdkeyquery.New(cdkeyquery.Dep{CdkeyCli: cdkey, WujiCli: w})
	dt2tsTool := oncallutils.NewDateTimeToTimestampMSTool(getLocalToolDescWire(w, "date_time_to_timestamp_ms"))
	ts2dtTool := oncallutils.NewTimestampMSToDateTimeTool(getLocalToolDescWire(w, "timestamp_ms_to_date_time"))
	return cdkagent.Dep{
		ModelInstance: m, WujiCli: w, MCPTool: mcp, GenConfig: gc,
		LocalTools: []tool.Tool{cdkeyQueryTool, dt2tsTool, ts2dtTool},
	}
}

// ---- service providers ----

func provideSessionService(cfg rainbow.AppConfig, m *openai.Model) session.Service {
	summarizer := summary.NewSummarizer(m,
		summary.WithChecksAny(
			summary.CheckEventThreshold(cfg.SummarizeEventThreshold),
			summary.CheckTokenThreshold(cfg.SummarizeTokenThreshold),
			summary.CheckTimeThreshold(time.Duration(cfg.SummarizeTimeThreshold)*time.Minute),
		),
	)
	return inmemory.NewSessionService(
		inmemory.WithSummarizer(summarizer),
		inmemory.WithSessionEventLimit(cfg.SummarizeEventThreshold*2),
	)
}

// provideApp constructs all agents and tRPC services, returning the complete App.
// All agent New() functions return the same agent.Agent interface, so Wire cannot
// disambiguate them. This single function receives each Dep struct (unique types) from Wire
// and calls New() manually for each agent.
func provideApp(
	cfg rainbow.AppConfig,
	sessionSvc session.Service,
	magicAgentDep magiconcallagent.Dep,
	cdkeyDep cdkagent.Dep,
	magicToolDep magictool.Dep,
) (*App, error) {
	magicAgt, err := magiconcallagent.New(magicAgentDep)
	if err != nil {
		return nil, err
	}
	cdkAgt, err := cdkagent.New(cdkeyDep)
	if err != nil {
		return nil, err
	}

	mysqlCli := mysql.NewClientProxy(mysqlFeedbackName)
	sseServers := map[string]sse.API{
		sseServiceName:      sse.NewSSEService(sessionSvc, magicAgt, mysqlCli, "magic_oncall_agent", cfg.Debug),
		cdkeySSEServiceName: sse.NewSSEService(sessionSvc, cdkAgt, mysqlCli, "cdkey_oncall_agent", cfg.Debug),
	}

	a2aSrv, err := a2a.NewA2AServer(a2aServiceName, magicAgt, sessionSvc)
	if err != nil {
		return nil, err
	}

	aguiSrv, err := agui.New(magicAgt, sessionSvc)
	if err != nil {
		return nil, err
	}

	// 企微WeCom AI Bot服务 (按配置决定是否启用)
	var wecomSrv *wecomsrv.Server
	if cfg.WeComEnabled {
		wecomSrv, err = wecomsrv.New("oncall_agent", magicAgt, wecomsrv.Config{
			BotID:         cfg.WeComStreamBotID,
			Secret:        cfg.WeComStreamSecret,
			BotName:       cfg.WeComBotName,
			WebSocketURL:  cfg.WeComStreamWSURL,
			EnableStream:  cfg.WeComEnableStream,
			ShowToolCalls: cfg.WeComShowToolCalls,
		})
		if err != nil {
			log.Warnf("WeCom server creation failed (disabled): %v", err)
			wecomSrv = nil // 创建失败不影响主服务
		}
	}

	return &App{
		A2AServers:  map[string]*a2aserver.A2AServer{a2aServiceName: a2aSrv},
		SSEServers:  sseServers,
		AguiServers: map[string]*sagui.Server{aguiServiceName: aguiSrv},
		WeComServer: wecomSrv,
		DebugSrv:    debug.New(magicToolDep),
	}, nil
}

// InitApp wires all dependencies and returns the App.
// cfg is provided by main() after rainbow.Init().
func InitApp(cfg rainbow.AppConfig) (*App, error) {
	wire.Build(
		// infra providers — each returns a unique interface type, no ambiguity
		provideGenConfig,
		provideModelInstance,
		provideWujiCli,
		provideMagicConfigAPI,
		provideGalileoCli,
		provideLingshanCli,
		provideCdkeyCli,
		provideConditionLogCli,
		provideMagicCliAPI,

		// shared domain tool deps (unique struct types, no ambiguity)
		provideMCPTool,
		provideTraceConfig,
		provideMagicToolDep,

		// agent dep providers — each returns a unique Dep struct type
		// (agent New() functions all return agent.Agent, so they are NOT
		// added to Wire; instead provideApp calls them manually)
		provideMagicAgentDep,
		provideCdkeyAgentDep,

		// session + app (provideApp builds agents and all services internally)
		provideSessionService,
		provideApp,
	)
	return nil, nil
}

// Ensure agent import is used (agent.Agent is referenced in provideApp return values).
var _ agent.Agent
