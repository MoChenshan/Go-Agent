package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

type failingDialer struct {
	err   error
	calls int
}

func (d *failingDialer) DialContext(
	_ context.Context,
	_ string,
	_ http.Header,
) (*websocket.Conn, *http.Response, error) {
	d.calls++
	return nil, nil, d.err
}

func TestWebSocketFrameJSONHelpers(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(wsOutboundFrame{
		Command: "subscribe",
		Headers: wsFrameHeaders{ReqID: "req-1"},
		Body: map[string]string{
			"hello": "world",
		},
	})
	require.NoError(t, err)
	require.JSONEq(t, `{
		"cmd":"subscribe",
		"headers":{"req_id":"req-1"},
		"body":{"hello":"world"}
	}`, string(data))
	require.NotContains(t, string(data), `"command"`)

	var outbound wsOutboundFrame
	require.NoError(t, json.Unmarshal([]byte(`{
		"cmd":"subscribe",
		"headers":{"req_id":"req-1"},
		"body":{"hello":"world"}
	}`), &outbound))
	require.Equal(t, "subscribe", outbound.Command)
	require.Equal(t, "req-1", outbound.Headers.ReqID)

	body, ok := outbound.Body.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "world", body["hello"])

	var inbound wsInboundFrame
	require.NoError(t, json.Unmarshal([]byte(`{
		"cmd":"callback",
		"headers":{"req_id":"req-2"},
		"body":{"text":"hello"},
		"errcode":1,
		"errmsg":"boom"
	}`), &inbound))
	require.Equal(t, "callback", inbound.Command)
	require.Equal(t, "req-2", inbound.Headers.ReqID)
	require.Equal(t, 1, inbound.ErrCode)
	require.Equal(t, "boom", inbound.ErrMsg)
}

func TestRunStopsOnContextCancel(t *testing.T) {
	t.Parallel()

	s, err := New(&fakeRunner{}, Config{
		BotID:          "bot",
		Secret:         "secret",
		ReconnectDelay: time.Millisecond,
	})
	require.NoError(t, err)

	dialer := &failingDialer{err: errors.New("dial failed")}
	s.wsDialer = dialer

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = s.Run(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, dialer.calls)
}
