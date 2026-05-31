package e2e

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	trpc "git.code.oa.com/trpc-go/trpc-go"
	tagui "git.woa.com/trpc-go/trpc-agent-go/trpc/agui"
	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	r3sse "github.com/r3labs/sse/v2"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	corerunner "trpc.group/trpc-go/trpc-agent-go/runner"
	aguiserver "trpc.group/trpc-go/trpc-agent-go/server/agui"
	"trpc.group/trpc-go/trpc-agent-go/session/inmemory"
)

const (
	testAppName    = "agui-e2e"
	trpcService    = "trpc.test.helloworld.agui"
	expectedToken  = "E2E_OK"
	defaultTimeout = 10 * time.Second
)

func TestAGUIServer_TRPC_MockModel_RunAndSnapshot(t *testing.T) {
	modelInstance := &QueueModel{}
	modelInstance.Push(Call{
		Responses: []*model.Response{
			{
				ID:     "mock-completion-1",
				Object: model.ObjectTypeChatCompletion,
				Done:   true,
				Choices: []model.Choice{{
					Message: model.Message{Role: model.RoleAssistant, Content: expectedToken},
				}},
			},
		},
	})

	baseURL, client := newAGUITRPCServer(t, modelInstance)

	runPayload := `{"threadId":"thread-1","runId":"run-1","messages":[{"role":"user","content":"reply now"}]}`
	res := postJSON(t, client, baseURL+"/agui", runPayload, 60*time.Second)

	require.Equal(t, http.StatusOK, res.StatusCode)
	require.Equal(t, "text/event-stream", res.Header.Get("Content-Type"))
	require.Equal(t, "no-cache", res.Header.Get("Cache-Control"))
	require.Equal(t, "keep-alive", res.Header.Get("Connection"))
	require.Equal(t, "*", res.Header.Get("Access-Control-Allow-Origin"))

	runEvents := readSSEEvents(t, res.Body)
	require.Len(t, runEvents, 5)
	require.NoError(t, aguievents.ValidateSequence(runEvents))

	runStarted, ok := runEvents[0].(*aguievents.RunStartedEvent)
	require.True(t, ok)
	require.Equal(t, "thread-1", runStarted.ThreadID())
	require.Equal(t, "run-1", runStarted.RunID())

	textStart, ok := runEvents[1].(*aguievents.TextMessageStartEvent)
	require.True(t, ok)
	require.NotEmpty(t, textStart.MessageID)
	require.NotNil(t, textStart.Role)
	require.Equal(t, "assistant", *textStart.Role)

	textContent, ok := runEvents[2].(*aguievents.TextMessageContentEvent)
	require.True(t, ok)
	require.Equal(t, textStart.MessageID, textContent.MessageID)
	require.Equal(t, expectedToken, textContent.Delta)

	textEnd, ok := runEvents[3].(*aguievents.TextMessageEndEvent)
	require.True(t, ok)
	require.Equal(t, textStart.MessageID, textEnd.MessageID)

	runFinished, ok := runEvents[4].(*aguievents.RunFinishedEvent)
	require.True(t, ok)
	require.Equal(t, "thread-1", runFinished.ThreadID())
	require.Equal(t, "run-1", runFinished.RunID())

	historyPayload := `{"threadId":"thread-1","runId":"snapshot-1","messages":[{"role":"user","content":""}]}`
	historyRes := postJSON(t, client, baseURL+"/history", historyPayload, 30*time.Second)
	historyEvents := readSSEEvents(t, historyRes.Body)

	require.Len(t, historyEvents, 3)
	require.NoError(t, aguievents.ValidateSequence(historyEvents))

	snapshotRunStarted, ok := historyEvents[0].(*aguievents.RunStartedEvent)
	require.True(t, ok)
	require.Equal(t, "thread-1", snapshotRunStarted.ThreadID())
	require.Equal(t, "snapshot-1", snapshotRunStarted.RunID())

	snapshot, ok := historyEvents[1].(*aguievents.MessagesSnapshotEvent)
	require.True(t, ok)
	require.NotEmpty(t, snapshot.Messages)

	var foundUser bool
	var foundAssistant bool
	for _, msg := range snapshot.Messages {
		if msg.Role == "user" && msg.Content != nil {
			content, ok := msg.ContentString()
			require.True(t, ok)
			require.Equal(t, "reply now", content)
			foundUser = true
		}
		if msg.Role == "assistant" && msg.Content != nil {
			content, ok := msg.ContentString()
			require.True(t, ok)
			require.Equal(t, expectedToken, content)
			foundAssistant = true
		}
	}
	require.True(t, foundUser)
	require.True(t, foundAssistant)

	snapshotRunFinished, ok := historyEvents[2].(*aguievents.RunFinishedEvent)
	require.True(t, ok)
	require.Equal(t, "thread-1", snapshotRunFinished.ThreadID())
	require.Equal(t, "snapshot-1", snapshotRunFinished.RunID())
}

func TestAGUIServer_TRPC_MockModel_CancelStopsRun(t *testing.T) {
	modelInstance := &QueueModel{}
	modelInstance.Push(Call{
		Responses: []*model.Response{
			{
				ID:        "mock-stream-1",
				Object:    model.ObjectTypeChatCompletionChunk,
				IsPartial: true,
				Choices: []model.Choice{{
					Delta: model.Message{Role: model.RoleAssistant, Content: "x"},
				}},
			},
		},
	})

	baseURL, client := newAGUITRPCServer(t, modelInstance)

	runPayload := `{"threadId":"thread-1","runId":"run-1","messages":[{"role":"user","content":"Generate at least 5000 characters."}]}`
	runCtx, cancelRun := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancelRun)

	runRequest, err := http.NewRequestWithContext(runCtx, http.MethodPost, baseURL+"/agui", strings.NewReader(runPayload))
	require.NoError(t, err)
	runRequest.Header.Set("Content-Type", "application/json")

	runResponse, err := client.Do(runRequest)
	require.NoError(t, err)
	t.Cleanup(func() { _ = runResponse.Body.Close() })

	started := make(chan struct{})
	streamDone := make(chan struct{})
	streamErr := make(chan error, 1)

	var startedOnce sync.Once
	go func() {
		defer close(streamDone)

		reader := r3sse.NewEventStreamReader(runResponse.Body, 1024*1024)
		var err error
		for {
			raw, readErr := reader.ReadEvent()
			if readErr != nil {
				if readErr == io.EOF {
					break
				}
				err = readErr
				break
			}
			data := bytes.TrimSpace(sseData(raw))
			if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
				continue
			}
			evt, evtErr := aguievents.EventFromJSON(data)
			if evtErr != nil {
				err = evtErr
				break
			}
			if evtErr := evt.Validate(); evtErr != nil {
				err = evtErr
				break
			}
			if _, ok := evt.(*aguievents.RunStartedEvent); ok {
				startedOnce.Do(func() { close(started) })
			}
		}

		streamErr <- err
		close(streamErr)
	}()

	select {
	case <-started:
	case err := <-streamErr:
		require.NoError(t, err)
		require.FailNow(t, "stream closed before RUN_STARTED")
	case <-time.After(defaultTimeout):
		require.FailNow(t, "timeout waiting for RUN_STARTED")
	}

	cancelPayload := `{"threadId":"thread-1","runId":"run-1","messages":[{"role":"user","content":""}]}`
	cancelResponse := postJSON(t, client, baseURL+"/cancel", cancelPayload, defaultTimeout)
	require.Equal(t, http.StatusOK, cancelResponse.StatusCode)

	select {
	case <-streamDone:
	case <-time.After(15 * time.Second):
		require.FailNow(t, "stream did not close after cancel")
	}

	err, ok := <-streamErr
	require.True(t, ok)
	require.NoError(t, err)
}

func newAGUITRPCServer(t *testing.T, modelInstance model.Model) (string, *http.Client) {
	t.Helper()

	port := allocateFreePort(t)
	configFile := writeTRPCConfig(t, port)

	originalConfigPath := trpc.ServerConfigPath
	trpc.ServerConfigPath = configFile
	t.Cleanup(func() {
		trpc.ServerConfigPath = originalConfigPath
	})

	sessionService := inmemory.NewSessionService()
	t.Cleanup(func() { _ = sessionService.Close() })

	ag := llmagent.New(
		"agui-agent",
		llmagent.WithModel(modelInstance),
	)

	run := corerunner.NewRunner(testAppName, ag, corerunner.WithSessionService(sessionService))
	t.Cleanup(func() { _ = run.Close() })

	serverInstance, err := aguiserver.New(
		run,
		aguiserver.WithPath("/agui"),
		aguiserver.WithCancelEnabled(true),
		aguiserver.WithCancelPath("/cancel"),
		aguiserver.WithMessagesSnapshotEnabled(true),
		aguiserver.WithMessagesSnapshotPath("/history"),
		aguiserver.WithAppName(testAppName),
		aguiserver.WithSessionService(sessionService),
	)
	require.NoError(t, err)

	trpcServer := trpc.NewServer()
	require.NoError(t, tagui.RegisterAGUIServer(trpcServer, trpcService, serverInstance))

	done := make(chan struct{})
	var serveErr error
	go func() {
		serveErr = trpcServer.Serve()
		close(done)
	}()

	t.Cleanup(func() {
		_ = trpcServer.Close(nil)
		select {
		case <-done:
		case <-time.After(defaultTimeout):
		}
	})

	waitForTCPListen(t, fmt.Sprintf("127.0.0.1:%d", port), done, &serveErr)
	return fmt.Sprintf("http://127.0.0.1:%d", port), &http.Client{}
}

func allocateFreePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	return port
}

func writeTRPCConfig(t *testing.T, port int) string {
	t.Helper()

	cfg := fmt.Sprintf(`server:
  service:
    - name: %s
      ip: 127.0.0.1
      port: %d
      protocol: http_no_protocol
`, trpcService, port)

	cfgPath := filepath.Join(t.TempDir(), "trpc_go.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0644))
	return cfgPath
}

func waitForTCPListen(t *testing.T, addr string, done <-chan struct{}, serveErr *error) {
	t.Helper()

	deadline := time.Now().Add(defaultTimeout)
	for {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		select {
		case <-done:
			require.NoError(t, *serveErr)
			require.FailNow(t, "server stopped before it started listening")
		default:
		}
		if time.Now().After(deadline) {
			require.FailNow(t, "timeout waiting for server to listen")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func postJSON(t *testing.T, client *http.Client, url string, payload string, timeout time.Duration) *http.Response {
	t.Helper()

	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		t.Cleanup(cancel)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func readSSEEvents(t *testing.T, r io.Reader) []aguievents.Event {
	t.Helper()

	var out []aguievents.Event
	reader := r3sse.NewEventStreamReader(r, 1024*1024)
	for {
		raw, err := reader.ReadEvent()
		if err != nil {
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
		}
		data := bytes.TrimSpace(sseData(raw))
		if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
			continue
		}
		evt, err := aguievents.EventFromJSON(data)
		require.NoError(t, err)
		require.NoError(t, evt.Validate())
		out = append(out, evt)
	}
	return out
}

func sseData(event []byte) []byte {
	var out []byte
	for _, line := range bytes.FieldsFunc(event, func(r rune) bool { return r == '\n' || r == '\r' }) {
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(out) > 0 {
			out = append(out, '\n')
		}
		out = append(out, data...)
	}
	return out
}
