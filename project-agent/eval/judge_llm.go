// judge_llm.go 真实 LLM 打分实现（D17.2）。
//
// 把 D14 留下的 MockJudge 替换/并列为 LLMJudge：
//   - 复用 trpc-agent-go/model.Model 抽象（即 *openai.Model，实际后端走混元/DeepSeek）
//   - 一次 API 调用覆盖全部维度（成本 & 延迟最优）
//   - 结构化 JSON 输出 + 三级容错解析（见 judge_prompt.go）
//   - 失败时**不** panic / **不** 返回零报告，而是标记 AvgScore=0、AllPass=false
//     并把 err 附在 Reason 里，保证 RunBatch 仍可跑完整批。
//
// 零直接依赖 openai 具体类型：LLMJudge 只依赖 LLMModel 接口，
// 这让单测无需真实 API Key / 无需真实 HTTP —— 注入 fakeModel 即可。
package eval

import (
	"context"
	"fmt"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"

	"git.woa.com/trpc-go/gameops-agent/src/observability"
)

// LLMModel 是 LLMJudge 依赖的模型接口（与 trpc-agent-go/model.Model 兼容）。
//
// 定义为本地接口而非直接引 model.Model 的原因：
//   - 方便测试（fakeModel 只需实现这一方法）；
//   - 让未来切换到其他 LLM SDK 时 Judge 不用改；
//   - 接口面积越小越好（ISP 原则）。
type LLMModel interface {
	GenerateContent(ctx context.Context, request *model.Request) (
		<-chan *model.Response, error)
}

// LLMJudgeConfig LLMJudge 构造参数。
type LLMJudgeConfig struct {
	// Model 必填，真实调用 LLM 的后端。
	Model LLMModel
	// SystemPrompt 可选，覆盖默认的 SystemPrompt；空则使用 DefaultJudgeSystemPrompt。
	//
	// 注意：若同时设置了 PromptStore，且 store 的 snapshot 非空，
	// 则 PromptStore 优先（实现 D17.2.1 的 YAML 热加载）。
	SystemPrompt string
	// Temperature 打分温度，建议 0.0~0.3（评审应稳定）。默认 0.0。
	Temperature float64
	// MaxTokens 最大输出 token，默认 1024（覆盖 3~5 维度的 JSON 足够）。
	MaxTokens int
	// Logger 可选，输出一条 info 级别日志（event="scored"/"error"）。
	Logger func(event, caseID, msg string)
	// PromptStore D17.2.1：可选的 prompt 热加载 store。
	//   - 为 nil 时保持 D17.2 旧行为（硬编码 SystemPrompt + DefaultJudgeDimensions）；
	//   - 非 nil 且 snapshot.IsEmpty()==false 时，每次 Score 从 store 取最新 snapshot，
	//     SRE 改 YAML 后无需重启进程即可生效。
	//   - snapshot 空字段仍会各自回退到默认（部分覆盖）。
	PromptStore *JudgePromptStore
}

// LLMJudge JudgeClient 的真实 LLM 实现。
type LLMJudge struct {
	cfg LLMJudgeConfig
}

// NewLLMJudge 构造。Model 必填，其余字段有默认。
func NewLLMJudge(cfg LLMJudgeConfig) (*LLMJudge, error) {
	if cfg.Model == nil {
		return nil, fmt.Errorf("NewLLMJudge: Model is required")
	}
	if cfg.SystemPrompt == "" {
		cfg.SystemPrompt = DefaultJudgeSystemPrompt
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 1024
	}
	if cfg.Temperature < 0 {
		cfg.Temperature = 0
	}
	if cfg.Temperature > 1 {
		cfg.Temperature = 1
	}
	return &LLMJudge{cfg: cfg}, nil
}

// Score 实现 JudgeClient 接口。单次请求一次性拿到所有维度打分。
//
// 错误策略：
//   - 入参错误（CaseID 空）→ 返回 error（立即失败，属于调用方 bug）
//   - LLM 调用错误（网络/超时）→ 返回 error（调用方可选择重试）
//   - LLM 响应无法解析 → 返回 error（同上）
//   - 不会返回部分成功的 report —— 让 RunBatch 上层决定是否跳过。
func (j *LLMJudge) Score(ctx context.Context, in JudgeInput) (*JudgeReport, error) {
	if in.CaseID == "" {
		return nil, fmt.Errorf("judge: CaseID required")
	}

	// D17.2.1：解析当前生效的 system prompt / dimensions（三层优先级）。
	//   1. PromptStore 的 snapshot 字段非空 → 最高优先（SRE 热改 YAML）
	//   2. LLMJudgeConfig 里的硬编码字段非空 → 次优先（构造时显式传）
	//   3. 内置常量 / DefaultJudgeDimensions() → 兜底
	systemPrompt, dims := j.effectivePrompt(in.Dimensions)

	// D17.4 可观测：记录单次 Score 耗时与 status 标签。
	// 注意 defer 里读 status/err 的值 —— Go 的闭包捕获变量引用，正好实现"函数退出时读最新值"。
	start := time.Now()
	status := observability.StatusOK
	defer func() {
		observability.ObserveJudgeLatency(ctx, time.Since(start).Seconds())
		observability.IncJudgeCall(ctx, status)
	}()

	raw, err := j.callLLM(ctx, in, dims, systemPrompt)
	if err != nil {
		status = observability.StatusError
		j.emit("error", in.CaseID, err.Error())
		return nil, fmt.Errorf("llm call: %w", err)
	}

	scores, err := ParseJudgeResponse(raw, dims)
	if err != nil {
		status = "parse_error" // 区分 LLM 失败与解析失败，便于告警分流
		j.emit("error", in.CaseID, err.Error())
		return nil, fmt.Errorf("parse response: %w", err)
	}

	rep := &JudgeReport{CaseID: in.CaseID, Scores: scores, AllPass: true}
	var total float64
	for _, s := range scores {
		if !s.Pass {
			rep.AllPass = false
		}
		total += s.Score
	}
	if len(scores) > 0 {
		rep.AvgScore = total / float64(len(scores))
	}
	j.emit("scored", in.CaseID,
		fmt.Sprintf("avg=%.2f all_pass=%v", rep.AvgScore, rep.AllPass))
	return rep, nil
}

// effectivePrompt 汇总三层优先级，返回最终生效的 system prompt 与维度列表。
//
// 维度优先级特殊：若调用方在 JudgeInput 里显式传了 Dimensions，则忽略 store/config
// （调用方明确意图 > 全局热加载），这保持与 D17.2 的 API 语义一致。
func (j *LLMJudge) effectivePrompt(inputDims []JudgeDimension) (string, []JudgeDimension) {
	// 默认走 config 已经回填好的 SystemPrompt（NewLLMJudge 时若空则已填默认值）。
	systemPrompt := j.cfg.SystemPrompt

	// 优先级 1：Store 中的 snapshot（热加载覆盖）。
	if j.cfg.PromptStore != nil {
		if snap := j.cfg.PromptStore.Get(); snap != nil && !snap.IsEmpty() {
			if strings.TrimSpace(snap.SystemPrompt) != "" {
				systemPrompt = snap.SystemPrompt
			}
			if len(inputDims) == 0 && len(snap.Dimensions) > 0 {
				// 拷贝一份避免调用方误改；snap.Dimensions 本身是 immutable 视图。
				dims := make([]JudgeDimension, len(snap.Dimensions))
				copy(dims, snap.Dimensions)
				return systemPrompt, dims
			}
		}
	}

	// 输入显式维度 > 默认维度。
	if len(inputDims) > 0 {
		return systemPrompt, inputDims
	}
	return systemPrompt, DefaultJudgeDimensions()
}

// callLLM 构造 model.Request 并收集流式/非流式响应的完整文本。
//
// 兼容策略：即使底层模型返回的是流式 chunk，也通过拼接 Delta.Content 和
// 最终 Message.Content 得到完整结果；Done 标记触发退出循环。
//
// systemPrompt 由 effectivePrompt 决定（D17.2.1 后可能来自 PromptStore）。
func (j *LLMJudge) callLLM(ctx context.Context, in JudgeInput,
	dims []JudgeDimension, systemPrompt string) (string, error) {
	// temperature 取本地副本，因 model.GenerationConfig 要 *float64
	temperature := j.cfg.Temperature
	maxTokens := j.cfg.MaxTokens

	userPrompt := BuildJudgeUserPrompt(JudgeInput{
		CaseID:         in.CaseID,
		UserQuery:      in.UserQuery,
		FinalAnswer:    in.FinalAnswer,
		ExpectedAnswer: in.ExpectedAnswer,
		Dimensions:     dims,
	})

	req := &model.Request{
		Messages: []model.Message{
			model.NewSystemMessage(systemPrompt),
			model.NewUserMessage(userPrompt),
		},
		GenerationConfig: model.GenerationConfig{
			Temperature: &temperature,
			MaxTokens:   &maxTokens,
			Stream:      false,
		},
	}

	respCh, err := j.cfg.Model.GenerateContent(ctx, req)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	for resp := range respCh {
		if resp == nil {
			continue
		}
		if resp.Error != nil {
			return "", fmt.Errorf("llm error: %s", resp.Error.Message)
		}
		if len(resp.Choices) > 0 {
			c := resp.Choices[0]
			// 非流式返回走 Message.Content；流式返回走 Delta.Content。
			if c.Message.Content != "" {
				sb.WriteString(c.Message.Content)
			}
			if c.Delta.Content != "" {
				sb.WriteString(c.Delta.Content)
			}
		}
		if resp.Done {
			break
		}
	}
	out := strings.TrimSpace(sb.String())
	if out == "" {
		return "", fmt.Errorf("empty response from model")
	}
	return out, nil
}

// emit 安全调用 Logger。
func (j *LLMJudge) emit(event, caseID, msg string) {
	if j.cfg.Logger != nil {
		j.cfg.Logger(event, caseID, msg)
	}
}
