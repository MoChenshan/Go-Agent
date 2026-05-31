// Package agents 包含各 Agent 的共享领域对象与公共函数。
//
// 本文件提供：
//   - FillSystemContextInfo：在每次 LLM 请求前自动向 System 消息注入当前时间戳，
//     便于模型理解「凌晨 3 点」「过去 1 小时」等时序语义。
//   - GenConfig / BuildGenConfig：大模型生成配置的轻量封装，集中管理 temperature / topP。
//   - NewDefaultModelCallbacks：统一出口，自动叠加 input_guard / output_guard（若已注册）。
//
// 参考：oncall_agent/domain/model/{context.go, genconfig.go}
package agents

import (
	"context"
	"fmt"
	"sync"
	"time"

	carbon "github.com/dromara/carbon/v2"
	agentmodel "trpc.group/trpc-go/trpc-agent-go/model"
)

// contextTemplate 在每次 LLM 调用前追加到 System 消息末尾的上下文模板。
const contextTemplate = `

## 上下文信息

- 当前日期: %s
- 当前时间戳(ms): %d
- 今日开始时间戳(ms): %d
`

// FillSystemContextInfo 是 model.BeforeModel 回调，
// 在 System 消息中注入当前时间，帮助 LLM 处理「凌晨 3 点」等时序问题。
var FillSystemContextInfo = func(ctx context.Context, req *agentmodel.Request) (*agentmodel.Response, error) {
	suffix := timeContextSuffix()
	for i, msg := range req.Messages {
		if msg.Role == agentmodel.RoleSystem {
			req.Messages[i].Content = req.Messages[i].Content + suffix
		}
	}
	return nil, nil
}

// timeContextSuffix 生成时间上下文片段（抽出便于单测）。
func timeContextSuffix() string {
	now := carbon.Now(carbon.Shanghai)
	return fmtTemplate(
		time.Now().Format("2006-01-02 15:04:05"),
		now.TimestampMilli(),
		now.StartOfDay().TimestampMilli(),
	)
}

// fmtTemplate 使用 fmt.Sprintf 渲染 contextTemplate（独立函数便于单测）。
func fmtTemplate(date string, now, startOfDay int64) string {
	return fmt.Sprintf(contextTemplate, date, now, startOfDay)
}

// GenConfig 大模型生成配置（从配置中心或 YAML 注入）。
type GenConfig struct {
	// Temperature 控制生成随机性，范围 [0.0, 2.0]，典型 0.1~0.8。
	Temperature float64
	// TopP 核采样参数，范围 [0.0, 1.0]。
	TopP float64
	// MaxTokens 最大生成 token 数，0 表示不限制。
	MaxTokens int
	// Stream 是否开启流式输出。
	Stream bool
}

// BuildGenConfig 把 GenConfig 转为 trpc-agent-go 原生的 GenerationConfig。
func BuildGenConfig(cfg GenConfig) agentmodel.GenerationConfig {
	genConfig := agentmodel.GenerationConfig{
		Stream: cfg.Stream,
	}
	if cfg.Temperature != 0 {
		temp := cfg.Temperature
		genConfig.Temperature = &temp
	}
	if cfg.TopP != 0 {
		topP := cfg.TopP
		genConfig.TopP = &topP
	}
	if cfg.MaxTokens > 0 {
		mt := cfg.MaxTokens
		genConfig.MaxTokens = &mt
	}
	return genConfig
}

// ---------------------------------------------------------------------------
// 全局 Model Callbacks 装饰（D14）
//
// 背景：input_guard（Prompt Injection 检测）与 output_guard（敏感信息打码）
//       需要挂到 *每个* LLM Agent 的 model.Callbacks 上才能生效；为避免
//       在 5 个 Agent 里重复样板代码，这里提供 app 层一次注入 + 各 Agent
//       统一入口调用的装配方式：
//
//   1. app.Init 最早阶段调用 agents.RegisterGlobalModelHooks(before, after)
//   2. 各 Agent 构造 modelCallbacks 时统一走 agents.NewDefaultModelCallbacks()
//   3. 该函数先 Register FillSystemContextInfo，再串入全局 hook；
//      app 未注册时退化为旧行为，100% 向下兼容。
// ---------------------------------------------------------------------------

var (
	globalModelMu          sync.RWMutex
	globalBeforeModelHooks []agentmodel.BeforeModelCallbackStructured
	globalAfterModelHooks  []agentmodel.AfterModelCallbackStructured
)

// RegisterGlobalModelHooks 追加一组全局 BeforeModel/AfterModel 钩子。
// 多次调用按注册顺序累积；app 层通常只在 Init 时调用一次。
//
// 线程安全：内部带锁；调用发生在 App 构造期，与 Agent 运行期无竞争。
func RegisterGlobalModelHooks(
	before []agentmodel.BeforeModelCallbackStructured,
	after []agentmodel.AfterModelCallbackStructured,
) {
	globalModelMu.Lock()
	defer globalModelMu.Unlock()
	globalBeforeModelHooks = append(globalBeforeModelHooks, before...)
	globalAfterModelHooks = append(globalAfterModelHooks, after...)
}

// ResetGlobalModelHooks 清空全局钩子；主要给单测用。
func ResetGlobalModelHooks() {
	globalModelMu.Lock()
	defer globalModelMu.Unlock()
	globalBeforeModelHooks = nil
	globalAfterModelHooks = nil
}

// NewDefaultModelCallbacks 构造一个默认 Agent 应使用的 model.Callbacks：
//   - BeforeModel：FillSystemContextInfo（时间上下文注入） + 全局 hook
//   - AfterModel：全局 hook
//
// 这是让 5 个 Agent 无感接入 input_guard / output_guard 的统一入口。
func NewDefaultModelCallbacks() *agentmodel.Callbacks {
	cb := agentmodel.NewCallbacks().
		RegisterBeforeModel(FillSystemContextInfo)

	globalModelMu.RLock()
	before := append([]agentmodel.BeforeModelCallbackStructured(nil),
		globalBeforeModelHooks...)
	after := append([]agentmodel.AfterModelCallbackStructured(nil),
		globalAfterModelHooks...)
	globalModelMu.RUnlock()

	for _, h := range before {
		cb.RegisterBeforeModel(h)
	}
	for _, h := range after {
		cb.RegisterAfterModel(h)
	}
	return cb
}
