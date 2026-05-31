package evaluation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	coreevaluation "trpc.group/trpc-go/trpc-agent-go/evaluation"
	evaluationserver "trpc.group/trpc-go/trpc-agent-go/server/evaluation"
)

func TestAddEvaluationServerToMuxServesSubroutes(t *testing.T) {
	serverInstance := newTestEvaluationServer(t)
	mux := http.NewServeMux()
	require.NoError(t, AddEvaluationServerToMux(mux, serverInstance))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/evaluation/runs", strings.NewReader(`{"setId":"set-1"}`))
	request.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(recorder, request)
	assert.Equal(t, http.StatusCreated, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"evalSetId":"set-1"`)
}

func TestAddEvaluationServerToMuxRejectsInvalidInputs(t *testing.T) {
	serverInstance := newTestEvaluationServer(t)
	err := AddEvaluationServerToMux(nil, serverInstance)
	assert.EqualError(t, err, "evaluation: mux cannot be nil")
	err = AddEvaluationServerToMux(http.NewServeMux(), nil)
	assert.EqualError(t, err, "evaluation: server cannot be nil")
}

func TestRegisterEvaluationServerToMuxRejectsNilTRPCServer(t *testing.T) {
	err := RegisterEvaluationServerToMux(nil, http.NewServeMux(), "trpc.test.evaluation", newTestEvaluationServer(t))
	assert.EqualError(t, err, "evaluation: trpc server cannot be nil")
}

func newTestEvaluationServer(t *testing.T) *evaluationserver.Server {
	t.Helper()
	serverInstance, err := evaluationserver.New(
		evaluationserver.WithAppName("evaluation-test"),
		evaluationserver.WithAgentEvaluator(stubAgentEvaluator{}),
	)
	require.NoError(t, err)
	return serverInstance
}

type stubAgentEvaluator struct{}

func (stubAgentEvaluator) Evaluate(
	ctx context.Context,
	evalSetID string,
	opt ...coreevaluation.Option,
) (*coreevaluation.EvaluationResult, error) {
	return &coreevaluation.EvaluationResult{
		AppName:   "evaluation-test",
		EvalSetID: evalSetID,
	}, nil
}

func (stubAgentEvaluator) Close() error {
	return nil
}
