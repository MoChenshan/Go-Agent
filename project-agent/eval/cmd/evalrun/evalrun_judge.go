//go:build eval

// evalrun_judge.go D17.5 — evalrun 接入 LLMJudge（离线评测追加 LLM-as-Judge 一轮）。
//
// 设计分层（单一入口 + 薄协调）：
//  1. collectJudgeInputs：纯函数。从 trpc-agent-go evaluator 产出的
//     *evalresult.EvalSetResult 抽出每个 case 的 (UserQuery, ActualAnswer, ExpectedAnswer)；
//     无 FinalResponse 的 case 直接跳过，不 panic。
//  2. buildJudge：可选创建 JudgePromptStore + JudgePromptWatcher（填了 --judge-prompt 才起）
//     + 构造 LLMJudge；独立生命周期，返回 cleanup 函数给 main 延迟调用。
//  3. printJudgeSummary：打印单独的 Judge 汇总区块，风格与 evaluator 摘要对齐。
//
// 为什么不把 LLMJudge 注册成 trpc-agent-go evaluator 的 metric：
//   - evaluator 原生也有 LLMJudge metric，但其 Criterion/Rubric schema 与我们
//     D17.2/D17.2.1 的"结构化 JSON + 三级容错解析 + YAML 热加载"协议不同；
//   - 改造协议 = 放弃稳住的链路。"追加一轮打分"零侵入上游，改动完全收敛在本文件。
//
// 为什么 Judge 失败不联动主 evaluator 退出码：
//   - Judge 本身走 LLM，存在短暂超时/限流。若因此把整个 CI 判红，噪音 > 信号。
//   - 走 "warn 不 fail" 默认，严格 CI 用 --judge-fail-on-threshold 显式开启。
package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/evaluation"
	"trpc.group/trpc-go/trpc-agent-go/evaluation/evalresult"

	openaimodel "trpc.group/trpc-go/trpc-agent-go/model/openai"

	"git.woa.com/trpc-go/gameops-agent/eval"
)

// judgeOptions D17.5 命令行 flag 聚合。
//
// Enabled=false 时 buildJudge/runJudge 均为 no-op，整个 Judge 分支对老 CI 零影响。
type judgeOptions struct {
	// Enabled 是否启用 LLMJudge 追加打分。
	Enabled bool
	// ModelName 评审 LLM 模型名；留空走环境变量默认。
	ModelName string
	// BaseURL / APIKey：Judge 专用 LLM 配置，不复用业务 Agent 的凭据。
	BaseURL string
	APIKey  string
	// PromptPath JudgePromptStore YAML 路径；非空时启用热加载 watcher。
	PromptPath string
	// WatchInterval prompt watcher 轮询间隔；<=0 视为 10s。
	WatchInterval time.Duration
	// FailOnThreshold true：任一 case AllPass=false 则退出码 1；默认仅 warn。
	FailOnThreshold bool
	// Temperature / MaxTokens 可选，默认 0 / 1024（与 LLMJudge 默认一致）。
	Temperature float64
	MaxTokens   int
	// IncludeToolSelection D30：在 LLMJudge 结果之外追加 ToolSelectionJudge
	// 一轮算法打分。该维度零 LLM 成本，可安全默认开启；但为了 D17.5 以来的
	// 调用方兼容（某些 CI 脚本精确断言 Judge 维度数），此处默认关闭，
	// 由 --judge-include-tool-selection 显式开启。
	IncludeToolSelection bool
}

// judgeRuntime buildJudge 的返回物，供 main 调度与清理。
type judgeRuntime struct {
	Judge   eval.JudgeClient
	Cleanup func()
	// Note 向人类汇报"Judge 启动时的关键状态"（如 prompt 来源、模型名），
	// 在 CI 日志里一目了然。留空表示 Judge 未启用。
	Note string
}

// buildJudge 根据 judgeOptions 构造 LLMJudge（含可选的 JudgePromptStore + Watcher）。
//
// 设计要点：
//   - Enabled=false → 返回零值 judgeRuntime，Judge=nil。main 自行 short-circuit。
//   - PromptPath 为空 → LLMJudge 走硬编码默认 prompt（与 D17.2 行为一致）。
//   - PromptPath 非空 → 启动 watcher；**若初次加载失败则返回 error**，这是刻意的：
//       SRE 显式传了路径却读不到，说明配置出错，应该让 CI 立刻红，而不是
//       静默回退默认 prompt（那样等于篡改了评审口径）。
//   - 其余可选参数都有兜底。
func buildJudge(opt judgeOptions) (*judgeRuntime, error) {
	if !opt.Enabled {
		return &judgeRuntime{}, nil
	}

	// 1. 构造独立 Judge 专用 LLM。
	modelName := strings.TrimSpace(opt.ModelName)
	if modelName == "" {
		modelName = "hunyuan-turbo-s"
	}
	var modelOpts []openaimodel.Option
	if opt.BaseURL != "" {
		modelOpts = append(modelOpts, openaimodel.WithBaseURL(opt.BaseURL))
	}
	if opt.APIKey != "" {
		modelOpts = append(modelOpts, openaimodel.WithAPIKey(opt.APIKey))
	}
	llm := openaimodel.New(modelName, modelOpts...)

	// 2. 可选：JudgePromptStore + Watcher（D17.2.1 能力的 CLI 入口）。
	var promptStore *eval.JudgePromptStore
	var stopWatcher func()
	noteBits := []string{fmt.Sprintf("model=%s", modelName)}
	if p := strings.TrimSpace(opt.PromptPath); p != "" {
		// 初次同步加载：Start 内部会 blocking 完成一次 load，失败也仅回调 logger，
		// 不会 panic。我们需要用 store.Get() 再判断一次。
		store := eval.NewJudgePromptStore()
		interval := opt.WatchInterval
		if interval <= 0 {
			interval = 10 * time.Second
		}
		watcher := eval.NewJudgePromptWatcher(eval.JudgePromptWatcherConfig{
			Path:     p,
			Store:    store,
			Interval: interval,
			Logger: func(event, msg string) {
				log.Printf("[judge-prompt] %s: %s", event, msg)
			},
		})
		watcher.Start()
		snap := store.Get()
		if snap == nil || snap.IsEmpty() {
			// 初次加载失败 → 显式 fail-fast（回滚 watcher，避免泄漏）。
			watcher.Stop()
			return nil, fmt.Errorf("judge prompt load failed: path=%s "+
				"(YAML syntax error? file missing? see [judge-prompt] error log above)", p)
		}
		promptStore = store
		stopWatcher = watcher.Stop
		noteBits = append(noteBits, fmt.Sprintf("prompt=%s", p))
	} else {
		noteBits = append(noteBits, "prompt=<default>")
	}

	// 3. 构造 LLMJudge（错误仅因 Model=nil；上面已保证非 nil）。
	judge, err := eval.NewLLMJudge(eval.LLMJudgeConfig{
		Model:       llm,
		Temperature: opt.Temperature,
		MaxTokens:   opt.MaxTokens,
		PromptStore: promptStore,
		Logger: func(event, caseID, msg string) {
			log.Printf("[llm-judge] %s case=%s %s", event, caseID, msg)
		},
	})
	if err != nil {
		if stopWatcher != nil {
			stopWatcher()
		}
		return nil, fmt.Errorf("build llm judge: %w", err)
	}

	cleanup := func() {
		if stopWatcher != nil {
			stopWatcher()
		}
	}
	return &judgeRuntime{
		Judge:   judge,
		Cleanup: cleanup,
		Note:    strings.Join(noteBits, ", "),
	}, nil
}

// collectJudgeInputs 从 EvaluationResult 抽出 Judge 所需输入。
//
// 纯函数设计（无 IO / 无日志），方便单测构造 fake result。
//
// 数据源优先级：
//  1. result.EvalResult.EvalCaseResults[i].EvalMetricResultPerInvocation[j].ActualInvocation.FinalResponse
//     → 作为 FinalAnswer（Agent 实际产出）
//  2. 同一 PerInvocation 的 ExpectedInvocation.FinalResponse → 作为 ExpectedAnswer
//  3. UserContent（Actual 优先，退回 Expected）→ 作为 UserQuery
//
// 跳过策略：
//   - FinalResponse 为空的 invocation 直接 skip（没东西可判）；
//   - 如果一个 case 的所有 invocation 都被 skip，该 case 不进 inputs；
//   - 多 run（numRuns>1）时默认取**第 0 个 run**的数据，避免重复打分成本翻倍；
//     需要覆盖所有 run 的质量评测不在 Judge 的设计目标内（MetricResults 已经聚合）。
//
// 为什么只取第一个 run：LLM 调用成本（时间 + 额度）是 Judge 关心的首要约束。
// 多 run 主要给 metric 做 run-level 均值/方差；Judge 这一层做"质量抽样"而非"质量全采"。
func collectJudgeInputs(result *evaluation.EvaluationResult) []eval.JudgeInput {
	if result == nil || result.EvalResult == nil {
		return nil
	}
	// 先按 EvalID 分组取第一个 run。
	firstRunByID := make(map[string]*evalresult.EvalCaseResult)
	for _, cr := range result.EvalResult.EvalCaseResults {
		if cr == nil || cr.EvalID == "" {
			continue
		}
		if _, ok := firstRunByID[cr.EvalID]; !ok {
			firstRunByID[cr.EvalID] = cr
		}
	}
	// 再按 result.EvalCases 的顺序产出，保证与 evaluator 摘要里的行序一致。
	inputs := make([]eval.JudgeInput, 0, len(result.EvalCases))
	for _, ec := range result.EvalCases {
		cr := firstRunByID[ec.EvalCaseID]
		if cr == nil {
			continue
		}
		in, ok := buildSingleJudgeInput(cr)
		if !ok {
			continue
		}
		inputs = append(inputs, in)
	}
	return inputs
}

// buildSingleJudgeInput 从单个 EvalCaseResult 的多 invocation 汇总出一条 JudgeInput。
//
// 目前策略：**拼接所有 invocation** 的 UserQuery / FinalAnswer / ExpectedAnswer；
// tool trace 按 invocation 顺序平铺。未来如要做 per-turn 细粒度打分，可拆成多条
// JudgeInput（CaseID 追加 -turnN 后缀）。
//
// D30：同时抽取 ActualInvocation.Tools / ExpectedInvocation.Tools 按顺序填入
// JudgeInput.ActualToolCalls / ExpectedToolCalls，喂给 ToolSelectionAccuracy 维度。
func buildSingleJudgeInput(cr *evalresult.EvalCaseResult) (eval.JudgeInput, bool) {
	var userQ, actualA, expectedA []string
	var actualTools, expectedTools []string
	for _, pi := range cr.EvalMetricResultPerInvocation {
		if pi == nil {
			continue
		}
		if pi.ActualInvocation != nil {
			if pi.ActualInvocation.UserContent != nil {
				if c := strings.TrimSpace(pi.ActualInvocation.UserContent.Content); c != "" {
					userQ = append(userQ, c)
				}
			}
			if pi.ActualInvocation.FinalResponse != nil {
				if c := strings.TrimSpace(pi.ActualInvocation.FinalResponse.Content); c != "" {
					actualA = append(actualA, c)
				}
			}
			// D30: 采集实际 tool trace 的工具名（忽略 arguments / result 这类对
			// 选择正确性无贡献的字段，降 token 消耗与对比噪声）。
			for _, t := range pi.ActualInvocation.Tools {
				if t != nil && strings.TrimSpace(t.Name) != "" {
					actualTools = append(actualTools, t.Name)
				}
			}
		}
		if pi.ExpectedInvocation != nil {
			// UserQuery 以 Actual 优先；Actual 为空时用 Expected 的 UserContent 兜底。
			if len(userQ) == 0 && pi.ExpectedInvocation.UserContent != nil {
				if c := strings.TrimSpace(pi.ExpectedInvocation.UserContent.Content); c != "" {
					userQ = append(userQ, c)
				}
			}
			if pi.ExpectedInvocation.FinalResponse != nil {
				if c := strings.TrimSpace(pi.ExpectedInvocation.FinalResponse.Content); c != "" {
					expectedA = append(expectedA, c)
				}
			}
			// D30: 采集 golden tool trace。
			for _, t := range pi.ExpectedInvocation.Tools {
				if t != nil && strings.TrimSpace(t.Name) != "" {
					expectedTools = append(expectedTools, t.Name)
				}
			}
		}
	}
	if len(actualA) == 0 {
		// Agent 没给出最终回复（可能中途失败）—— 跳过，让 evaluator 的状态机负责标失败。
		return eval.JudgeInput{}, false
	}
	return eval.JudgeInput{
		CaseID:            cr.EvalID,
		UserQuery:         strings.Join(userQ, "\n\n"),
		FinalAnswer:       strings.Join(actualA, "\n\n"),
		ExpectedAnswer:    strings.Join(expectedA, "\n\n"),
		ActualToolCalls:   actualTools,
		ExpectedToolCalls: expectedTools,
	}, true
}

// runJudge 执行批量打分；Judge=nil 时 short-circuit。
//
// 返回 summary 与 error 分离：LLM 批量打分过程中任一 case 失败，
// 上层根据 FailOnThreshold 决定是否升级为 exit 1。
func runJudge(ctx context.Context, rt *judgeRuntime,
	inputs []eval.JudgeInput) (*eval.BatchJudgeSummary, error) {
	if rt == nil || rt.Judge == nil {
		return nil, nil
	}
	if len(inputs) == 0 {
		return &eval.BatchJudgeSummary{DimAvg: map[string]float64{}}, nil
	}
	return eval.RunBatch(ctx, rt.Judge, inputs)
}

// runToolSelectionJudge D30：对 inputs 额外跑一轮纯算法 Tool 维度打分。
//
// 独立于 LLMJudge：
//   - LLMJudge 评"答案质量"（RootCause/Evidence/Helpfulness），需 LLM 推理；
//   - ToolSelectionJudge 评"工具选择对不对"（LCS+集合命中），纯算法；
//   - 两者正交，产出分别汇总。
//
// 为什么不塞回 LLMJudge 的 Dimensions 里一起打：LLMJudge 会把所有 Dimensions
// 传给 LLM 让它打分；而 Tool 维度是客观算法，LLM 评它既浪费 token 又不稳定。
func runToolSelectionJudge(ctx context.Context, enabled bool,
	inputs []eval.JudgeInput) (*eval.BatchJudgeSummary, error) {
	if !enabled {
		return nil, nil
	}
	if len(inputs) == 0 {
		return &eval.BatchJudgeSummary{DimAvg: map[string]float64{}}, nil
	}
	// 构造一份只包含 ToolSelectionAccuracy 维度的 inputs 副本，避免污染 LLMJudge
	// 已经打好分的 inputs（Dimensions 字段复用同一切片会让 LLMJudge 额外评一遍）。
	scoped := make([]eval.JudgeInput, len(inputs))
	for i, in := range inputs {
		copied := in
		copied.Dimensions = []eval.JudgeDimension{eval.ToolSelectionAccuracyDimension()}
		scoped[i] = copied
	}
	return eval.RunBatch(ctx, eval.NewToolSelectionJudge(), scoped)
}

// printJudgeSummary 打印 Judge 的汇总段；风格对齐 evaluator 摘要。
//
// 输出示例：
//
//	──── LLMJudge ────────────────────────────────────────
//	[llm-judge] model=hunyuan-turbo-s, prompt=eval/judge_prompt.yaml
//	total=5 passed=4 pass_rate=80.00%
//	  - RootCauseAccuracy        avg=0.91
//	  - EvidenceSufficiency      avg=0.83
//	  - HelpfulnessSafety        avg=0.77
//	────────────────────────────────────────────────────────
//	case=case_oom_diagnose       avg=0.92 all_pass=true
//	  - RootCauseAccuracy        0.95  PASS  含根因关键词
//	  - ...
func printJudgeSummary(summary *eval.BatchJudgeSummary, note string) {
	fmt.Println("──── LLMJudge ────────────────────────────────────────")
	if note != "" {
		fmt.Printf("[llm-judge] %s\n", note)
	}
	if summary == nil || summary.Total == 0 {
		fmt.Println("（无可评估用例：所有 case 均缺失 FinalResponse）")
		fmt.Println("────────────────────────────────────────────────────────")
		return
	}
	passRate := float64(summary.Passed) / float64(summary.Total) * 100
	fmt.Printf("total=%d passed=%d pass_rate=%.2f%%\n",
		summary.Total, summary.Passed, passRate)
	// 维度均分按字典序，保证输出稳定。
	dims := make([]string, 0, len(summary.DimAvg))
	for d := range summary.DimAvg {
		dims = append(dims, d)
	}
	sortStringsAscending(dims)
	for _, d := range dims {
		fmt.Printf("  - %-24s avg=%.2f\n", d, summary.DimAvg[d])
	}
	fmt.Println("────────────────────────────────────────────────────────")
	for _, rep := range summary.Reports {
		pass := "false"
		if rep.AllPass {
			pass = "true"
		}
		fmt.Printf("case=%-28s avg=%.2f all_pass=%s\n",
			rep.CaseID, rep.AvgScore, pass)
		for _, s := range rep.Scores {
			tag := "FAIL"
			if s.Pass {
				tag = "PASS"
			}
			reason := s.Reason
			// 防止超长 reason 把终端撑炸。
			if len(reason) > 120 {
				reason = reason[:117] + "..."
			}
			fmt.Printf("  - %-24s %.2f  %s  %s\n",
				s.Dimension, s.Score, tag, reason)
		}
	}
}

// sortStringsAscending 小工具：不想引 sort 包放在文件里，避免再加一个 import
// —— 实际上 sort 包在 main_real.go 中没用，但这里 runtime 如果引 sort 是
// 干净的。为了一致性，还是直接借用 sort：
func sortStringsAscending(ss []string) {
	// 简易插排足够；维度数一般 ≤ 10。避免 import sort。
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j-1] > ss[j]; j-- {
			ss[j-1], ss[j] = ss[j], ss[j-1]
		}
	}
}
