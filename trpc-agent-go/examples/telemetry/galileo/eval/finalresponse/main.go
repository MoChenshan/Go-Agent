//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//
//

// Package main implements functions for the final response evaluation.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"git.code.oa.com/trpc-go/trpc-go"
	gevaluation "git.woa.com/galileo/trpc-agent-go-galileo/evaluation"

	"trpc.group/trpc-go/trpc-agent-go/evaluation"
	"trpc.group/trpc-go/trpc-agent-go/evaluation/evalresult"
	"trpc.group/trpc-go/trpc-agent-go/evaluation/evalset"
	"trpc.group/trpc-go/trpc-agent-go/evaluation/evaluator/registry"
	"trpc.group/trpc-go/trpc-agent-go/evaluation/metric"
	metriclocal "trpc.group/trpc-go/trpc-agent-go/evaluation/metric/local"
	"trpc.group/trpc-go/trpc-agent-go/runner"

	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/galileo"
)

// datasetManagerWrapper wraps galileo's DatasetManager to implement evalset.Manager (adds Close).
type datasetManagerWrapper struct {
	*gevaluation.DatasetManager
}

func (w *datasetManagerWrapper) Close() error { return nil }

var _ evalset.Manager = (*datasetManagerWrapper)(nil)

// resultManagerWrapper wraps galileo's ResultManager to implement evalresult.Manager (adds Close).
type resultManagerWrapper struct {
	*gevaluation.ResultManager
}

func (w *resultManagerWrapper) Close() error { return nil }

var _ evalresult.Manager = (*resultManagerWrapper)(nil)

var (
	dataDir   = flag.String("data-dir", "./data", "Directory containing evaluation set and metric files")
	modelName = flag.String("model", "hunyuan-t1-latest", "Model to use for evaluation runs")
	streaming = flag.Bool("streaming", false, "Enable streaming responses from the agent")
	evalSetID = flag.String("eval-set", "ds_1770619375113380765_25d59b3d", "Evaluation set identifier to execute")
)

const appName = "final-response-app"

func main() {
	//setupGalileo()
	flag.Parse()
	trpc.NewServer()
	ctx := context.Background()
	runner := runner.NewRunner(appName, newQAAgent(*modelName, *streaming))
	defer runner.Close()

	evalSetManager := &datasetManagerWrapper{gevaluation.NewDatasetManager()}
	evalResultManager := &resultManagerWrapper{gevaluation.NewResultManager()}
	metricManager := metriclocal.New(metric.WithBaseDir(*dataDir))
	reg := registry.New()
	taskManager := gevaluation.NewTaskManager()
	callbacks := gevaluation.NewCallbacks(taskManager, evalResultManager.ResultManager)
	agentEvaluator, err := evaluation.New(
		appName,
		runner,
		evaluation.WithEvalSetManager(evalSetManager),
		evaluation.WithMetricManager(metricManager),
		evaluation.WithEvalResultManager(evalResultManager),
		evaluation.WithRegistry(reg),
		evaluation.WithCallbacks(callbacks),
		evaluation.WithEvalCaseParallelInferenceEnabled(true),
		evaluation.WithEvalCaseParallelism(10),
	)
	if err != nil {
		log.Fatalf("create evaluator: %v", err)
	}

	result, err := agentEvaluator.Evaluate(ctx, *evalSetID)
	if err != nil {
		log.Fatalf("evaluate: %v", err)
	}
	printSummary(result)
	time.Sleep(time.Second * 60)
}

func printSummary(result *evaluation.EvaluationResult) {
	fmt.Println("✅ Final-response evaluation completed with local storage")
	fmt.Printf("App: %s\n", result.AppName)
	fmt.Printf("Eval Set: %s\n", result.EvalSetID)
	fmt.Printf("Overall Status: %s\n", result.OverallStatus)
	runs := 0
	if len(result.EvalCases) > 0 {
		runs = len(result.EvalCases[0].EvalCaseResults)
	}
	fmt.Printf("Runs: %d\n", runs)

	for _, caseResult := range result.EvalCases {
		fmt.Printf("Case %s -> %s\n", caseResult.EvalCaseID, caseResult.OverallStatus)
		for _, metricResult := range caseResult.MetricResults {
			fmt.Printf("  Metric %s: score %.2f (threshold %.2f) => %s\n",
				metricResult.MetricName,
				metricResult.Score,
				metricResult.Threshold,
				metricResult.EvalStatus,
			)
		}
		fmt.Println()
	}
}
