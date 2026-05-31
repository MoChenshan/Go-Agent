package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	wecomchannel "git.woa.com/trpc-go/trpc-agent-go/openclaw/channel/wecom"
	"github.com/stretchr/testify/require"
	occhannel "trpc.group/trpc-go/trpc-agent-go/openclaw/channel"
)

const (
	testWeComActivateUserID = "T12345678"
	testWeComCreatorUserID  = "wineguo"
)

type stubWeComAdminTarget = wecomchannel.AdminTarget

type stubActivateStatus = wecomchannel.AdminActivationStatus

type stubWeComActivateDelivery struct {
	Target string
	Text   string
}

type stubWeComDebugSendDelivery struct {
	Target  string
	Message occhannel.OutboundMessage
}

type stubActChannel struct {
	target   stubWeComAdminTarget
	status   stubActivateStatus
	allowed  map[string]struct{}
	sendErr  error
	sent     []stubWeComActivateDelivery
	messages []stubWeComDebugSendDelivery
}

func (s *stubActChannel) ID() string {
	return channelTypeWeCom
}

func (s *stubActChannel) Run(
	ctx context.Context,
) error {
	return ctx.Err()
}

func (s *stubActChannel) WeComAdminTarget() stubWeComAdminTarget {
	return s.target
}

func (s *stubActChannel) WeComAdminActivationStatus() stubActivateStatus {
	return s.status
}

func (s *stubActChannel) WeComAdminAllowsUser(
	userID string,
) bool {
	if len(s.allowed) == 0 {
		return true
	}
	_, ok := s.allowed[userID]
	return ok
}

func (s *stubActChannel) SendText(
	ctx context.Context,
	target string,
	text string,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.sendErr != nil {
		return s.sendErr
	}
	s.sent = append(s.sent, stubWeComActivateDelivery{
		Target: target,
		Text:   text,
	})
	return nil
}

func (s *stubActChannel) SendMessage(
	ctx context.Context,
	target string,
	msg occhannel.OutboundMessage,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.sendErr != nil {
		return s.sendErr
	}
	s.messages = append(s.messages, stubWeComDebugSendDelivery{
		Target:  target,
		Message: msg,
	})
	return nil
}

func newStubWeComActivateChannel() *stubActChannel {
	return &stubActChannel{
		target: wecomchannel.AdminTarget{
			Name:           "corp",
			StateDir:       "/tmp/wecom-corp",
			BotMode:        wecomAIBotModeConfigValue,
			ConnectionMode: wecomWebSocketModeConfigValue,
			CallbackPath:   wecomDefaultCallbackPathConfigValue,
			AIBotID:        "bot-corp",
			ChatPolicy:     wecomDefaultChatPolicyConfigValue,
		},
		status: wecomchannel.AdminActivationStatus{
			Supported: true,
			Available: true,
		},
	}
}

func writeWeComActivateRuntimeEnv(
	t *testing.T,
	stateDir string,
	userID string,
) {
	t.Helper()

	path := filepath.Join(stateDir, runtimeEnvFileName)
	data := []byte(
		wecomActivateDefaultUserEnvName + "='" + userID + "'\n",
	)
	require.NoError(t, os.WriteFile(path, data, 0o600))
}

func postWeComActivateJSON(
	t *testing.T,
	handler http.Handler,
	request wecomActivateRequest,
) *httptest.ResponseRecorder {
	t.Helper()

	body, err := json.Marshal(request)
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodPost,
		wecomActivateActionPath,
		bytes.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	rsp := httptest.NewRecorder()
	handler.ServeHTTP(rsp, req)
	return rsp
}

func decodeWeComActivateRuntimeList(
	t *testing.T,
	rsp *httptest.ResponseRecorder,
) wecomActivateRuntimeList {
	t.Helper()

	var payload wecomActivateRuntimeList
	require.NoError(t, json.Unmarshal(
		rsp.Body.Bytes(),
		&payload,
	))
	return payload
}

func decodeWeComActivateResponse(
	t *testing.T,
	rsp *httptest.ResponseRecorder,
) wecomActivateResponse {
	t.Helper()

	var payload wecomActivateResponse
	require.NoError(t, json.Unmarshal(
		rsp.Body.Bytes(),
		&payload,
	))
	return payload
}

func decodeWeComActivateError(
	t *testing.T,
	rsp *httptest.ResponseRecorder,
) wecomActivateErrorResponse {
	t.Helper()

	var payload wecomActivateErrorResponse
	require.NoError(t, json.Unmarshal(
		rsp.Body.Bytes(),
		&payload,
	))
	return payload
}

func postWeComDebugSendJSON(
	t *testing.T,
	handler http.Handler,
	request wecomDebugSendRequest,
) *httptest.ResponseRecorder {
	t.Helper()

	body, err := json.Marshal(request)
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodPost,
		wecomDebugSendActionPath,
		bytes.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	rsp := httptest.NewRecorder()
	handler.ServeHTTP(rsp, req)
	return rsp
}

func decodeWeComDebugSendResponse(
	t *testing.T,
	rsp *httptest.ResponseRecorder,
) wecomDebugSendResponse {
	t.Helper()

	var payload wecomDebugSendResponse
	require.NoError(t, json.Unmarshal(
		rsp.Body.Bytes(),
		&payload,
	))
	return payload
}

func decodeWeComDebugSendError(
	t *testing.T,
	rsp *httptest.ResponseRecorder,
) wecomDebugSendErrorResponse {
	t.Helper()

	var payload wecomDebugSendErrorResponse
	require.NoError(t, json.Unmarshal(
		rsp.Body.Bytes(),
		&payload,
	))
	return payload
}

func TestCollectWeComActivateRuntimesDeduplicatesByIdentity(
	t *testing.T,
) {
	t.Parallel()

	first := newStubWeComActivateChannel()
	second := newStubWeComActivateChannel()
	second.target.AIBotID = "bot-other"

	runtimes := collectWeComActivateRuntimes(
		[]occhannel.Channel{first, second},
	)
	require.Len(t, runtimes, 1)
	require.Equal(t, "WeCom Runtime · corp", runtimes[0].title)
	require.NotEmpty(t, runtimes[0].key)
	require.NotContains(t, runtimes[0].key, first.target.StateDir)
}

func TestWeComActivateAdminHandlerListsRuntimes(t *testing.T) {
	t.Parallel()

	channel := newStubWeComActivateChannel()
	channel.target.StateDir = t.TempDir()
	writeWeComActivateRuntimeEnv(
		t,
		channel.target.StateDir,
		testWeComCreatorUserID,
	)
	service := newWeComActivateAdminService(
		[]occhannel.Channel{channel},
	)
	handler := wrapWeComActivateAdminHandler(nil, service)

	req := httptest.NewRequest(
		http.MethodGet,
		wecomActivateRuntimesPath,
		nil,
	)
	rsp := httptest.NewRecorder()
	handler.ServeHTTP(rsp, req)

	require.Equal(t, http.StatusOK, rsp.Code)
	payload := decodeWeComActivateRuntimeList(t, rsp)
	require.Len(t, payload.Runtimes, 1)
	require.Equal(t, "corp", payload.Runtimes[0].Name)
	require.True(t, payload.Runtimes[0].Activation.Supported)
	require.True(t, payload.Runtimes[0].Activation.Available)
	require.NotEmpty(t, payload.Runtimes[0].RuntimeKey)
	require.Equal(
		t,
		testWeComCreatorUserID,
		payload.Runtimes[0].DefaultWeComUserID,
	)
}

func TestWeComActivateAdminHandlerActivateJSON(t *testing.T) {
	t.Parallel()

	channel := newStubWeComActivateChannel()
	service := newWeComActivateAdminService(
		[]occhannel.Channel{channel},
	)
	handler := wrapWeComActivateAdminHandler(nil, service)
	runtimeKey := service.runtimeViewList()[0].RuntimeKey

	rsp := postWeComActivateJSON(
		t,
		handler,
		wecomActivateRequest{
			RuntimeKey:  runtimeKey,
			WeComUserID: testWeComActivateUserID,
			Scene:       wecomActivateSceneAPI,
		},
	)

	require.Equal(t, http.StatusOK, rsp.Code)
	payload := decodeWeComActivateResponse(t, rsp)
	require.True(t, payload.OK)
	require.Equal(t, runtimeKey, payload.RuntimeKey)
	require.Equal(t, testWeComActivateUserID, payload.WeComUserID)
	require.Equal(
		t,
		"single:"+testWeComActivateUserID,
		payload.Target,
	)
	require.Equal(t, wecomActivateKind, payload.MessageKind)
	require.False(t, payload.Deduplicated)
	require.Len(t, channel.sent, 1)
	require.Equal(t, wecomActivateMsg, channel.sent[0].Text)
}

func TestWeComActivateAdminHandlerActivateJSONUsesDefaultUserID(
	t *testing.T,
) {
	t.Parallel()

	channel := newStubWeComActivateChannel()
	channel.target.StateDir = t.TempDir()
	writeWeComActivateRuntimeEnv(
		t,
		channel.target.StateDir,
		testWeComCreatorUserID,
	)
	service := newWeComActivateAdminService(
		[]occhannel.Channel{channel},
	)
	handler := wrapWeComActivateAdminHandler(nil, service)
	runtimeKey := service.runtimeViewList()[0].RuntimeKey

	rsp := postWeComActivateJSON(
		t,
		handler,
		wecomActivateRequest{
			RuntimeKey: runtimeKey,
			Scene:      wecomActivateSceneAPI,
		},
	)

	require.Equal(t, http.StatusOK, rsp.Code)
	payload := decodeWeComActivateResponse(t, rsp)
	require.Equal(t, testWeComCreatorUserID, payload.WeComUserID)
	require.Equal(
		t,
		"single:"+testWeComCreatorUserID,
		payload.Target,
	)
	require.Len(t, channel.sent, 1)
}

func TestWeComActivateAdminHandlerActivateFailureModes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		channel    *stubActChannel
		request    wecomActivateRequest
		wantStatus int
		wantCode   string
	}{
		{
			name:       "invalid request",
			channel:    newStubWeComActivateChannel(),
			request:    wecomActivateRequest{},
			wantStatus: http.StatusBadRequest,
			wantCode:   wecomActivateErrInvalid,
		},
		{
			name:    "missing user without default",
			channel: newStubWeComActivateChannel(),
			request: wecomActivateRequest{
				RuntimeKey: "fill-from-runtime",
			},
			wantStatus: http.StatusBadRequest,
			wantCode:   wecomActivateErrInvalid,
		},
		{
			name:    "runtime not found",
			channel: newStubWeComActivateChannel(),
			request: wecomActivateRequest{
				RuntimeKey:  "wecom_rt_missing",
				WeComUserID: testWeComActivateUserID,
			},
			wantStatus: http.StatusNotFound,
			wantCode:   wecomActivateErrRuntimeGone,
		},
		{
			name: "unsupported",
			channel: func() *stubActChannel {
				channel := newStubWeComActivateChannel()
				channel.status = wecomchannel.AdminActivationStatus{}
				return channel
			}(),
			wantStatus: http.StatusConflict,
			wantCode:   wecomActivateErrUnsupported,
		},
		{
			name: "not connected",
			channel: func() *stubActChannel {
				channel := newStubWeComActivateChannel()
				channel.status = wecomchannel.AdminActivationStatus{
					Supported: true,
				}
				return channel
			}(),
			wantStatus: http.StatusConflict,
			wantCode:   wecomActivateErrDisconnected,
		},
		{
			name: "blocked",
			channel: func() *stubActChannel {
				channel := newStubWeComActivateChannel()
				channel.allowed = map[string]struct{}{
					"other-user": {},
				}
				return channel
			}(),
			wantStatus: http.StatusForbidden,
			wantCode:   wecomActivateErrBlocked,
		},
		{
			name: "delivery failed",
			channel: func() *stubActChannel {
				channel := newStubWeComActivateChannel()
				channel.sendErr = errors.New("boom")
				return channel
			}(),
			wantStatus: http.StatusBadGateway,
			wantCode:   wecomActivateErrDelivery,
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			service := newWeComActivateAdminService(
				[]occhannel.Channel{testCase.channel},
			)
			handler := wrapWeComActivateAdminHandler(nil, service)
			if testCase.request.RuntimeKey == "" ||
				testCase.request.RuntimeKey == "fill-from-runtime" {
				testCase.request.RuntimeKey = service.
					runtimeViewList()[0].RuntimeKey
			}
			if testCase.request.WeComUserID == "" &&
				testCase.wantCode != wecomActivateErrInvalid {
				testCase.request.WeComUserID = testWeComActivateUserID
			}

			rsp := postWeComActivateJSON(
				t,
				handler,
				testCase.request,
			)

			require.Equal(t, testCase.wantStatus, rsp.Code)
			payload := decodeWeComActivateError(t, rsp)
			require.False(t, payload.OK)
			require.Equal(t, testCase.wantCode, payload.Error.Code)
		})
	}
}

func TestWeComActivateAdminHandlerCooldownAndIdempotency(
	t *testing.T,
) {
	t.Parallel()

	channel := newStubWeComActivateChannel()
	service := newWeComActivateAdminService(
		[]occhannel.Channel{channel},
	)
	handler := wrapWeComActivateAdminHandler(nil, service)
	runtimeKey := service.runtimeViewList()[0].RuntimeKey

	first := postWeComActivateJSON(
		t,
		handler,
		wecomActivateRequest{
			RuntimeKey:      runtimeKey,
			WeComUserID:     testWeComActivateUserID,
			ClientRequestID: "req-1",
		},
	)
	require.Equal(t, http.StatusOK, first.Code)
	require.False(
		t,
		decodeWeComActivateResponse(t, first).Deduplicated,
	)
	require.Len(t, channel.sent, 1)

	second := postWeComActivateJSON(
		t,
		handler,
		wecomActivateRequest{
			RuntimeKey:      runtimeKey,
			WeComUserID:     testWeComActivateUserID,
			ClientRequestID: "req-1",
		},
	)
	require.Equal(t, http.StatusOK, second.Code)
	require.True(
		t,
		decodeWeComActivateResponse(t, second).Deduplicated,
	)
	require.Len(t, channel.sent, 1)

	third := postWeComActivateJSON(
		t,
		handler,
		wecomActivateRequest{
			RuntimeKey:      runtimeKey,
			WeComUserID:     testWeComActivateUserID,
			ClientRequestID: "req-2",
		},
	)
	require.Equal(t, http.StatusTooManyRequests, third.Code)
	require.Equal(
		t,
		wecomActivateErrCooldown,
		decodeWeComActivateError(t, third).Error.Code,
	)
	require.Len(t, channel.sent, 1)
}

func TestWeComActivateAdminHandlerFormRedirectsToChannels(
	t *testing.T,
) {
	t.Parallel()

	channel := newStubWeComActivateChannel()
	service := newWeComActivateAdminService(
		[]occhannel.Channel{channel},
	)
	handler := wrapWeComActivateAdminHandler(nil, service)
	runtimeKey := service.runtimeViewList()[0].RuntimeKey

	form := url.Values{}
	form.Set(wecomActivateFormRuntimeKey, runtimeKey)
	form.Set(wecomActivateFormUserID, testWeComActivateUserID)
	form.Set(wecomActivateFormScene, wecomActivateSceneAdmin)
	form.Set(
		wecomActivateFormReturnPath,
		channelsAdminPagePath+"#"+wecomActivateAnchor(runtimeKey),
	)

	req := httptest.NewRequest(
		http.MethodPost,
		wecomActivateActionPath,
		bytes.NewBufferString(form.Encode()),
	)
	req.Header.Set(
		"Content-Type",
		"application/x-www-form-urlencoded",
	)
	rsp := httptest.NewRecorder()
	handler.ServeHTTP(rsp, req)

	require.Equal(t, http.StatusSeeOther, rsp.Code)
	require.Contains(
		t,
		rsp.Header().Get("Location"),
		"../../../channels?notice=WeCom+activation+sent.",
	)
	require.Contains(
		t,
		rsp.Header().Get("Location"),
		"#"+wecomActivateAnchor(runtimeKey),
	)
}

func TestWeComDebugSendAdminHandlerSendJSON(t *testing.T) {
	t.Parallel()

	channel := newStubWeComActivateChannel()
	service := newWeComDebugSendAdminService(
		[]occhannel.Channel{channel},
	)
	handler := wrapWeComDebugSendAdminHandler(nil, service)
	runtimeKey := service.runtimes[0].key

	rsp := postWeComDebugSendJSON(
		t,
		handler,
		wecomDebugSendRequest{
			RuntimeKey: runtimeKey,
			Target:     "group:chat1",
			Text:       "hello",
			FilePath:   "/tmp/report.png",
			FileName:   "report.png",
		},
	)

	require.Equal(t, http.StatusOK, rsp.Code)
	payload := decodeWeComDebugSendResponse(t, rsp)
	require.True(t, payload.OK)
	require.Equal(t, runtimeKey, payload.RuntimeKey)
	require.Equal(t, "group:chat1", payload.Target)
	require.Equal(t, wecomDebugSendKind, payload.MessageKind)
	require.Len(t, channel.messages, 1)
	require.Equal(t, "group:chat1", channel.messages[0].Target)
	require.Equal(t, "hello", channel.messages[0].Message.Text)
	require.Len(t, channel.messages[0].Message.Files, 1)
	require.Equal(
		t,
		"/tmp/report.png",
		channel.messages[0].Message.Files[0].Path,
	)
	require.Equal(
		t,
		"report.png",
		channel.messages[0].Message.Files[0].Name,
	)
}

func TestWeComDebugSendAdminHandlerRejectsEmptyPayload(
	t *testing.T,
) {
	t.Parallel()

	channel := newStubWeComActivateChannel()
	service := newWeComDebugSendAdminService(
		[]occhannel.Channel{channel},
	)
	handler := wrapWeComDebugSendAdminHandler(nil, service)

	rsp := postWeComDebugSendJSON(
		t,
		handler,
		wecomDebugSendRequest{
			RuntimeKey: service.runtimes[0].key,
			Target:     "single:" + testWeComActivateUserID,
		},
	)

	require.Equal(t, http.StatusBadRequest, rsp.Code)
	payload := decodeWeComDebugSendError(t, rsp)
	require.False(t, payload.OK)
	require.Equal(t, wecomDebugSendErrInvalid, payload.Error.Code)
	require.Empty(t, channel.messages)
}

func TestWeComDebugSendAdminHandlerFormRedirectsToChannels(
	t *testing.T,
) {
	t.Parallel()

	channel := newStubWeComActivateChannel()
	service := newWeComDebugSendAdminService(
		[]occhannel.Channel{channel},
	)
	handler := wrapWeComDebugSendAdminHandler(nil, service)
	runtimeKey := service.runtimes[0].key

	form := url.Values{}
	form.Set(wecomDebugSendFormRuntimeKey, runtimeKey)
	form.Set(
		wecomDebugSendFormTarget,
		"single:"+testWeComActivateUserID,
	)
	form.Set(wecomDebugSendFormText, "hello")
	form.Set(
		wecomDebugSendFormReturnPath,
		channelsAdminPagePath+"#"+wecomActivateAnchor(runtimeKey),
	)

	req := httptest.NewRequest(
		http.MethodPost,
		wecomDebugSendActionPath,
		bytes.NewBufferString(form.Encode()),
	)
	req.Header.Set(
		"Content-Type",
		"application/x-www-form-urlencoded",
	)
	rsp := httptest.NewRecorder()
	handler.ServeHTTP(rsp, req)

	require.Equal(t, http.StatusSeeOther, rsp.Code)
	require.Contains(
		t,
		rsp.Header().Get("Location"),
		"../../../channels?notice=WeCom+debug+message+sent.",
	)
	require.Contains(
		t,
		rsp.Header().Get("Location"),
		"#"+wecomActivateAnchor(runtimeKey),
	)
	require.Len(t, channel.messages, 1)
}
