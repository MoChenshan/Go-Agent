package wecome2e_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/evaluation"
	evalresult "trpc.group/trpc-go/trpc-agent-go/evaluation/evalresult"
	evalresultlocal "trpc.group/trpc-go/trpc-agent-go/evaluation/evalresult/local"
	"trpc.group/trpc-go/trpc-agent-go/evaluation/evalset"
	evalsetinmemory "trpc.group/trpc-go/trpc-agent-go/evaluation/evalset/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/evaluation/metric"
	metriccriterion "trpc.group/trpc-go/trpc-agent-go/evaluation/metric/criterion"
	metricllm "trpc.group/trpc-go/trpc-agent-go/evaluation/metric/criterion/llm"
	metricinmemory "trpc.group/trpc-go/trpc-agent-go/evaluation/metric/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/evaluation/status"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

type traceCriticEvalInput struct {
	evalSetID         string
	evalCaseID        string
	userID            string
	userPrompt        string
	actualResponse    string
	referenceResponse string
	threshold         float64
	rubrics           []*metricllm.Rubric
}

type traceOnlyRunner struct {
}

func (r *traceOnlyRunner) Run(
	_ context.Context,
	_ string,
	_ string,
	_ model.Message,
	_ ...agent.RunOption,
) (<-chan *event.Event, error) {
	return nil, fmt.Errorf("trace evaluation runner must not be called")
}

func (r *traceOnlyRunner) Close() error {
	return nil
}

func runTraceCriticEvaluation(
	t *testing.T,
	env wecomE2EEnv,
	input traceCriticEvalInput,
) *evaluation.EvaluationResult {
	t.Helper()
	ctx := context.Background()
	evalSetManager := evalsetinmemory.New()
	metricManager := metricinmemory.New()
	evalResultDir := wecomEvalResultDir(t)
	evalResultManager := evalresultlocal.New(
		evalresult.WithBaseDir(evalResultDir),
	)
	_, err := evalSetManager.Create(ctx, wecomE2EAppName, input.evalSetID)
	require.NoError(t, err)
	expectedInvocation := &evalset.Invocation{
		InvocationID: input.evalCaseID + "-1",
		UserContent:  messagePtr(model.NewUserMessage(input.userPrompt)),
		FinalResponse: messagePtr(
			model.NewAssistantMessage(input.referenceResponse),
		),
	}
	actualInvocation := &evalset.Invocation{
		InvocationID: input.evalCaseID + "-1",
		UserContent:  messagePtr(model.NewUserMessage(input.userPrompt)),
		FinalResponse: messagePtr(
			model.NewAssistantMessage(input.actualResponse),
		),
	}
	err = evalSetManager.AddCase(ctx, wecomE2EAppName, input.evalSetID, &evalset.EvalCase{
		EvalID:             input.evalCaseID,
		EvalMode:           evalset.EvalModeTrace,
		Conversation:       []*evalset.Invocation{expectedInvocation},
		ActualConversation: []*evalset.Invocation{actualInvocation},
		SessionInput: &evalset.SessionInput{
			AppName: wecomE2EAppName,
			UserID:  input.userID,
		},
	})
	require.NoError(t, err)
	judgeTemperature := 0.0
	judgeMaxTokens := 512
	err = metricManager.Add(ctx, wecomE2EAppName, input.evalSetID, &metric.EvalMetric{
		MetricName: "llm_rubric_critic",
		Threshold:  input.threshold,
		Criterion: &metriccriterion.Criterion{
			LLMJudge: metricllm.New(
				"openai",
				env.judgeModelName,
				metricllm.WithAPIKey(env.judgeAPIKey),
				metricllm.WithBaseURL(env.judgeBaseURL),
				metricllm.WithNumSamples(1),
				metricllm.WithGeneration(&model.GenerationConfig{
					MaxTokens:   &judgeMaxTokens,
					Temperature: &judgeTemperature,
					Stream:      false,
				}),
				metricllm.WithRubrics(input.rubrics),
			),
		},
	})
	require.NoError(t, err)
	evaluator, err := evaluation.New(
		wecomE2EAppName,
		&traceOnlyRunner{},
		evaluation.WithEvalSetManager(evalSetManager),
		evaluation.WithMetricManager(metricManager),
		evaluation.WithEvalResultManager(evalResultManager),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, evaluator.Close())
	})
	result, err := evaluator.Evaluate(ctx, input.evalSetID)
	require.NoError(t, err)
	require.Len(t, result.EvalCases, 1)
	require.Equal(t, status.EvalStatusPassed, result.OverallStatus)
	require.Len(t, result.EvalCases[0].MetricResults, 1)
	require.Equal(t, status.EvalStatusPassed, result.EvalCases[0].OverallStatus)
	require.Equal(
		t,
		"llm_rubric_critic",
		result.EvalCases[0].MetricResults[0].MetricName,
	)
	require.Equal(
		t,
		status.EvalStatusPassed,
		result.EvalCases[0].MetricResults[0].EvalStatus,
	)
	require.GreaterOrEqual(
		t,
		result.EvalCases[0].MetricResults[0].Score,
		input.threshold,
	)
	require.NotNil(t, result.EvalResult)
	resultPath := filepath.Join(
		evalResultDir,
		wecomE2EAppName,
		result.EvalResult.EvalSetResultID+".evalset_result.json",
	)
	require.FileExists(t, resultPath)
	t.Logf("evaluation result saved to %s", resultPath)
	return result
}

func messagePtr(msg model.Message) *model.Message {
	return &msg
}

func wecomEvalResultDir(t *testing.T) string {
	t.Helper()
	if raw := strings.TrimSpace(os.Getenv(wecomE2EEvalResultEnv)); raw != "" {
		require.NoError(t, os.MkdirAll(raw, 0o755))
		return raw
	}
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	dir := filepath.Join(filepath.Dir(file), "..", "output", "wecom", "evalresult")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	return dir
}
