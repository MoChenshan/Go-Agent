//go:build eval

// Package main 的 real 构建：`go build -tags eval ./eval/cmd/evalrun` 启用。
//
// 真实执行路径：
//  1. 用 project-agent 的 app.Init 构造 Coordinator Agent + 工具栈；
//  2. 包一层 runner.NewRunner；
//  3. 交给 trpc-agent-go/evaluation 的 evaluator 跑金标集；
//  4. 把 Metric 打分结果落盘并打印摘要；
//  5. D17.5：可选追加 LLMJudge（`--enable-llm-judge`）一轮质量抽样打分，
//     评审标准走 D17.2.1 的 YAML 热加载（`--judge-prompt`）。
//
// 默认构建（stub）不会进入此文件，避免拖入 evaluation 独立 module。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"git.woa.com/trpc-go/gameops-agent/eval"
	"git.woa.com/trpc-go/gameops-agent/src/app"
	"git.woa.com/trpc-go/gameops-agent/src/config"

	"trpc.group/trpc-go/trpc-agent-go/evaluation"
	"trpc.group/trpc-go/trpc-agent-go/evaluation/evalresult"
	evalresultlocal "trpc.group/trpc-go/trpc-agent-go/evaluation/evalresult/local"
	"trpc.group/trpc-go/trpc-agent-go/evaluation/evalset"
	evalsetlocal "trpc.group/trpc-go/trpc-agent-go/evaluation/evalset/local"
	"trpc.group/trpc-go/trpc-agent-go/evaluation/evaluator/registry"
	"trpc.group/trpc-go/trpc-agent-go/evaluation/metric"
	metriclocal "trpc.group/trpc-go/trpc-agent-go/evaluation/metric/local"
	"trpc.group/trpc-go/trpc-agent-go/runner"
)

var (
	dataDir     = flag.String("data-dir", eval.DefaultDataDir(), "评测数据根目录")
	outputDir   = flag.String("output-dir", "./eval/output", "评测结果输出目录")
	evalSetID   = flag.String("eval-set", eval.DefaultEvalSetID, "要执行的 eval set ID")
	numRuns     = flag.Int("runs", 1, "每个 case 重复次数（抗抖动）")
	parallelism = flag.Int("parallelism", 2, "case 级并行度")

	// D17.5：LLMJudge 追加打分 flag。
	enableLLMJudge = flag.Bool("enable-llm-judge", false,
		"evaluator 跑完后追加一轮 LLMJudge 质量打分")
	judgeModel = flag.String("judge-model", "",
		"Judge 专用 LLM 模型名（留空走 hunyuan-turbo-s；与业务 Agent 隔离）")
	judgeBaseURL = flag.String("judge-base-url", "",
		"Judge 专用 LLM BaseURL；留空走环境变量 OPENAI_BASE_URL")
	judgeAPIKey = flag.String("judge-api-key", "",
		"Judge 专用 LLM API Key；留空走环境变量 OPENAI_API_KEY")
	judgePrompt = flag.String("judge-prompt", "",
		"JudgePromptStore YAML 路径；填写则启用 D17.2.1 热加载，留空用内置默认 prompt")
	judgeWatchSec = flag.Int("judge-watch-interval-sec", 10,
		"JudgePrompt watcher 轮询间隔（秒）；仅当 --judge-prompt 非空时有效")
	judgeFail = flag.Bool("judge-fail-on-threshold", false,
		"任一 case AllPass=false 或 Judge 批次有错时退出码 1；默认仅 warn")
	// D30: ToolSelectionAccuracy 维度（纯算法，零 LLM 成本）。
	judgeIncludeToolSel = flag.Bool("judge-include-tool-selection", false,
		"追加一轮 ToolSelectionAccuracy 维度（对比 LLM 实际 tool trace 与 golden trace）；"+
			"纯算法打分，零 LLM 成本；与 --enable-llm-judge 正交（可单独启用）")
	// D30.1: Judge 批次结果 JSON 落盘（机器可解析格式）。
	judgeJSONOut = flag.String("judge-json-out", "",
		"把两个 Judge 的批次结果落盘为 JSON 到指定路径（供 CI/MR 机器人消费）；"+
			"留空则不落盘；父目录不存在会自动创建；schema 版本见 JudgeReportSchemaVersion")
)

func main() {
	flag.Parse()
	ctx := context.Background()

	// 0. 先做静态校验，工具名对不上直接退出，避免浪费 LLM 额度
	set, err := eval.LoadEvalSet(fmt.Sprintf("%s/%s/%s.evalset.json",
		*dataDir, *evalSetID, *evalSetID))
	if err != nil {
		log.Fatalf("static load evalset: %v", err)
	}
	if bad := set.ValidateAppName(eval.DefaultAppName); len(bad) > 0 {
		log.Fatalf("evalset appName 不一致（期望 %q）：%v", eval.DefaultAppName, bad)
	}

	// 1. 构造 App → 取入口 Agent（Coordinator）
	//    注：app.Init 需要显式传 *config.Config；评测场景走 config.Default()
	//    —— 凭据 / MCP 配置由环境变量注入，不读项目 trpc_go.yaml，避免把
	//    production 的 webhook/audit 远端在评测时误拉起来。
	a, err := app.Init(ctx, config.Default())
	if err != nil {
		log.Fatalf("app.Init: %v", err)
	}
	if a.Entrance == nil {
		log.Fatal("app.Entrance 未初始化，无法执行评测")
	}
	defer a.Close()

	// 2. 包一层 runner（复用 App 中的 session，保证多轮上下文跨 case 隔离由 session.ID 控制）
	r := runner.NewRunner(eval.DefaultAppName, a.Entrance, runner.WithSessionService(a.Session))
	defer r.Close()

	// 2.1 D17.5：提前构造 LLMJudge（若启用）。
	// 原因是 Judge 的 prompt YAML 不存在/解析失败会 fail-fast，
	// 与其让 evaluator 跑 10 分钟再在最后一步失败，不如开跑前验证环境 OK。
	judgeRT, err := buildJudge(judgeOptions{
		Enabled:              *enableLLMJudge,
		ModelName:            *judgeModel,
		BaseURL:              *judgeBaseURL,
		APIKey:               *judgeAPIKey,
		PromptPath:           *judgePrompt,
		WatchInterval:        time.Duration(*judgeWatchSec) * time.Second,
		FailOnThreshold:      *judgeFail,
		IncludeToolSelection: *judgeIncludeToolSel,
	})
	if err != nil {
		log.Fatalf("build llm judge: %v", err)
	}
	if judgeRT != nil && judgeRT.Cleanup != nil {
		defer judgeRT.Cleanup()
	}

	// 3. 装配 evaluator
	evalSetMgr := evalsetlocal.New(evalset.WithBaseDir(*dataDir))
	metricMgr := metriclocal.New(metric.WithBaseDir(*dataDir))
	evalResMgr := evalresultlocal.New(evalresult.WithBaseDir(*outputDir))
	reg := registry.New()

	evaluator, err := evaluation.New(
		eval.DefaultAppName,
		r,
		evaluation.WithEvalSetManager(evalSetMgr),
		evaluation.WithMetricManager(metricMgr),
		evaluation.WithEvalResultManager(evalResMgr),
		evaluation.WithRegistry(reg),
		evaluation.WithNumRuns(*numRuns),
		evaluation.WithEvalCaseParallelInferenceEnabled(true),
		evaluation.WithEvalCaseParallelism(*parallelism),
	)
	if err != nil {
		log.Fatalf("evaluation.New: %v", err)
	}
	defer func() { _ = evaluator.Close() }()

	// 4. 执行
	result, err := evaluator.Evaluate(ctx, *evalSetID)
	if err != nil {
		log.Fatalf("evaluator.Evaluate: %v", err)
	}

	// 5. 打印 evaluator 摘要
	printSummary(result, *outputDir)

	// 6. D17.5 + D30：追加 LLMJudge / ToolSelectionJudge（可选）。
	//    两者共用 inputs（evalresult → JudgeInput 的抽取只做一次）。
	judgeExitCode := 0
	var judgeInputs []eval.JudgeInput
	needInputs := (judgeRT != nil && judgeRT.Judge != nil) || *judgeIncludeToolSel
	if needInputs {
		judgeInputs = collectJudgeInputs(result)
	}

	// 提升到外层：D30.1 JSON 落盘共享使用；note 同样外提，保证 stdout 打印和
	// JSON 落盘里的 note 字段来自同一字符串。
	var (
		llmSum, toolSum   *eval.BatchJudgeSummary
		llmNote, toolNote string
	)

	// 6.1 LLMJudge（D17.5）。
	if judgeRT != nil && judgeRT.Judge != nil {
		jsum, jerr := runJudge(ctx, judgeRT, judgeInputs)
		if jerr != nil {
			log.Printf("[llm-judge] batch error: %v", jerr)
			if *judgeFail {
				judgeExitCode = 1
			}
		}
		llmSum = jsum
		llmNote = judgeRT.Note
		printJudgeSummary(jsum, llmNote)
		// 阈值不达时按 flag 决定是否升级为退出码 1。
		if jsum != nil && *judgeFail && jsum.Passed < jsum.Total {
			judgeExitCode = 1
		}
	}

	// 6.2 ToolSelectionJudge（D30）。纯算法，无 LLM 成本。
	if *judgeIncludeToolSel {
		tsum, terr := runToolSelectionJudge(ctx, true, judgeInputs)
		if terr != nil {
			log.Printf("[tool-selection-judge] batch error: %v", terr)
			if *judgeFail {
				judgeExitCode = 1
			}
		}
		toolSum = tsum
		toolNote = "judge=ToolSelectionJudge (pure algorithm, no LLM cost)"
		printJudgeSummary(tsum, toolNote)
		if tsum != nil && *judgeFail && tsum.Passed < tsum.Total {
			judgeExitCode = 1
		}
	}

	// 6.3 D30.1：把两个 Judge 的结果落盘为 JSON（仅在 --judge-json-out 指定时）。
	//     落盘失败只 warn 不改退出码——JSON 落盘是旁路产物，不应因它让整个 CI 红。
	if *judgeJSONOut != "" {
		if err := WriteJudgeReportJSON(*evalSetID, llmSum, llmNote,
			toolSum, toolNote, *judgeJSONOut); err != nil {
			log.Printf("[judge-json] write %s: %v", *judgeJSONOut, err)
		} else {
			fmt.Printf("Judge JSON : %s\n", *judgeJSONOut)
		}
	}

	// 7. 退出码
	// overall 非 passed 时退出码 1，便于 CI 判断；
	// Judge 在 --judge-fail-on-threshold 时一并参与判定，取 OR。
	exitCode := 0
	if result != nil && string(result.OverallStatus) != "passed" && string(result.OverallStatus) != "PASSED" {
		exitCode = 1
	}
	if judgeExitCode > exitCode {
		exitCode = judgeExitCode
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

func printSummary(result *evaluation.EvaluationResult, outDir string) {
	if result == nil {
		fmt.Println("⚠ evaluator 返回 nil result")
		return
	}
	fmt.Println("╔══════════════════════════════════════════════════════╗")
	fmt.Println("║  GameOps Agent — Evaluation Result                   ║")
	fmt.Println("╚══════════════════════════════════════════════════════╝")
	fmt.Printf("App        : %s\n", result.AppName)
	fmt.Printf("EvalSet    : %s\n", result.EvalSetID)
	fmt.Printf("Status     : %s\n", result.OverallStatus)
	runs := 0
	if len(result.EvalCases) > 0 {
		runs = len(result.EvalCases[0].EvalCaseResults)
	}
	fmt.Printf("Runs/case  : %d\n", runs)
	fmt.Println("────────────────────────────────────────────────────────")
	for _, c := range result.EvalCases {
		fmt.Printf("🧪 %-30s %s\n", c.EvalCaseID, c.OverallStatus)
		for _, m := range c.MetricResults {
			fmt.Printf("   - %-28s score=%.2f  threshold=%.2f  %s\n",
				m.MetricName, m.Score, m.Threshold, m.EvalStatus)
		}
	}
	fmt.Println("────────────────────────────────────────────────────────")
	fmt.Printf("详细结果已落盘：%s\n", outDir)
}
