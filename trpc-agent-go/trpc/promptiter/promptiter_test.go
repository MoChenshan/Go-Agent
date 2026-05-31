package promptiter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	astructure "trpc.group/trpc-go/trpc-agent-go/agent/structure"
	promptiterengine "trpc.group/trpc-go/trpc-agent-go/evaluation/workflow/promptiter/engine"
	promptiterserver "trpc.group/trpc-go/trpc-agent-go/server/promptiter"
)

func TestAddPromptIterServerToMuxServesSubroutes(t *testing.T) {
	serverInstance := newTestPromptIterServer(t)
	mux := http.NewServeMux()
	require.NoError(t, AddPromptIterServerToMux(mux, serverInstance))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/promptiter/v1/apps/promptiter-test/structure", nil)
	mux.ServeHTTP(recorder, request)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"structure"`)
}

func TestAddPromptIterServerToMuxRejectsInvalidInputs(t *testing.T) {
	serverInstance := newTestPromptIterServer(t)
	err := AddPromptIterServerToMux(nil, serverInstance)
	assert.EqualError(t, err, "promptiter: mux cannot be nil")
	err = AddPromptIterServerToMux(http.NewServeMux(), nil)
	assert.EqualError(t, err, "promptiter: server cannot be nil")
}

func TestRegisterPromptIterServerToMuxRejectsNilTRPCServer(t *testing.T) {
	err := RegisterPromptIterServerToMux(nil, http.NewServeMux(), "trpc.test.promptiter", newTestPromptIterServer(t))
	assert.EqualError(t, err, "promptiter: trpc server cannot be nil")
}

func newTestPromptIterServer(t *testing.T) *promptiterserver.Server {
	t.Helper()
	serverInstance, err := promptiterserver.New(
		promptiterserver.WithAppName("promptiter-test"),
		promptiterserver.WithEngine(stubPromptIterEngine{}),
	)
	require.NoError(t, err)
	return serverInstance
}

type stubPromptIterEngine struct{}

func (stubPromptIterEngine) Describe(ctx context.Context) (*astructure.Snapshot, error) {
	return &astructure.Snapshot{}, nil
}

func (stubPromptIterEngine) Run(
	ctx context.Context,
	request *promptiterengine.RunRequest,
	opts ...promptiterengine.Option,
) (*promptiterengine.RunResult, error) {
	return &promptiterengine.RunResult{
		Status: promptiterengine.RunStatusSucceeded,
	}, nil
}
