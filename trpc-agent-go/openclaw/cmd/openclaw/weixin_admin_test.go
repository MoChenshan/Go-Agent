package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	weixinchannel "git.woa.com/trpc-go/trpc-agent-go/openclaw/channel/weixin"
	"github.com/stretchr/testify/require"
	occhannel "trpc.group/trpc-go/trpc-agent-go/openclaw/channel"
)

type stubWXAdminChannel struct {
	target weixinchannel.AdminTarget
}

func (s stubWXAdminChannel) ID() string {
	return "weixin"
}

func (s stubWXAdminChannel) Run(
	context.Context,
) error {
	return nil
}

func (s stubWXAdminChannel) WeixinAdminTarget() weixinchannel.AdminTarget {
	return s.target
}

func TestCollectRuntimeWeixinAdminTargetsDedup(t *testing.T) {
	t.Parallel()

	channels := []occhannel.Channel{
		stubWXAdminChannel{
			target: weixinchannel.AdminTarget{
				StateDir: "/tmp/weixin-a",
			},
		},
		stubWXAdminChannel{
			target: weixinchannel.AdminTarget{
				StateDir:       "/tmp/weixin-a",
				DefaultBaseURL: "https://example.com",
			},
		},
		stubWXAdminChannel{
			target: weixinchannel.AdminTarget{
				StateDir: "/tmp/weixin-b",
			},
		},
	}

	targets := collectRuntimeWeixinAdminTargets(channels)
	require.Len(t, targets, 2)
	require.Equal(t, "/tmp/weixin-a", targets[0].StateDir)
	require.Equal(t, "https://example.com", targets[0].DefaultBaseURL)
	require.Equal(t, "/tmp/weixin-b", targets[1].StateDir)
}

func TestWeixinAdminHandlerStartLoginAndRemoveAccount(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	targets := []weixinchannel.AdminTarget{{
		StateDir:       stateDir,
		DefaultBaseURL: "https://ilink.example.com",
	}}
	runner := func(
		ctx context.Context,
		runtimeStateDir string,
		baseURL string,
		botType string,
		callbacks weixinchannel.LoginCallbacks,
	) (weixinchannel.Account, error) {
		if callbacks.OnQRCode != nil {
			callbacks.OnQRCode("https://qr.example.com")
		}
		if callbacks.OnStatus != nil {
			callbacks.OnStatus(weixinLoginStatusWait)
			callbacks.OnStatus(weixinLoginStatusConfirmed)
		}
		account := weixinchannel.Account{
			AccountID: "acc-1",
			Token:     "tok-1",
			BaseURL:   baseURL,
			UserID:    "user-1",
		}
		if err := weixinchannel.SaveAccount(
			runtimeStateDir,
			account,
		); err != nil {
			return weixinchannel.Account{}, err
		}
		return account, nil
	}

	svc := newWeixinAdminServiceWithRunner(targets, runner)
	handler := wrapWeixinAdminHandler(nil, svc)

	startReq := httptest.NewRequest(
		http.MethodPost,
		weixinAdminLoginStartPath,
		strings.NewReader(url.Values{
			weixinAdminFormRuntimeKey: {"weixin-1"},
			weixinAdminFormBaseURL:    {"https://ilink.example.com"},
			weixinAdminFormBotType:    {"3"},
		}.Encode()),
	)
	startReq.Header.Set(
		"Content-Type",
		"application/x-www-form-urlencoded",
	)
	startRsp := httptest.NewRecorder()
	handler.ServeHTTP(startRsp, startReq)
	require.Equal(t, http.StatusSeeOther, startRsp.Code)
	require.Equal(
		t,
		"../../../channels?notice=Started+Weixin+QR+login."+
			"#weixin-runtime-weixin-1",
		startRsp.Header().Get("Location"),
	)

	require.Eventually(t, func() bool {
		status := fetchWeixinAdminStatus(t, handler)
		if len(status.Runtimes) != 1 {
			return false
		}
		runtime := status.Runtimes[0]
		return runtime.Login.SavedAccountID == "acc-1" &&
			runtime.Login.Status == weixinLoginStatusConfirmed &&
			len(runtime.Accounts) == 1
	}, time.Second, 20*time.Millisecond)

	removeReq := httptest.NewRequest(
		http.MethodPost,
		weixinAdminAccountRemovePath,
		strings.NewReader(url.Values{
			weixinAdminFormRuntimeKey: {"weixin-1"},
			weixinAdminFormAccountID:  {"acc-1"},
		}.Encode()),
	)
	removeReq.Header.Set(
		"Content-Type",
		"application/x-www-form-urlencoded",
	)
	removeRsp := httptest.NewRecorder()
	handler.ServeHTTP(removeRsp, removeReq)
	require.Equal(t, http.StatusSeeOther, removeRsp.Code)

	accounts, err := weixinchannel.ListAccounts(stateDir)
	require.NoError(t, err)
	require.Empty(t, accounts)
}

func TestWeixinAdminQREntryRedirectsToLatestQRPage(t *testing.T) {
	t.Parallel()

	svc := newWeixinAdminServiceWithRunner(
		[]weixinchannel.AdminTarget{{
			StateDir:       t.TempDir(),
			DefaultBaseURL: "https://ilink.example.com",
		}},
		func(
			ctx context.Context,
			runtimeStateDir string,
			baseURL string,
			botType string,
			callbacks weixinchannel.LoginCallbacks,
		) (weixinchannel.Account, error) {
			if callbacks.OnStatus != nil {
				callbacks.OnStatus(weixinLoginStatusWait)
			}
			if callbacks.OnQRCode != nil {
				callbacks.OnQRCode(
					"https://qr.example.com/latest",
				)
			}
			<-ctx.Done()
			return weixinchannel.Account{}, ctx.Err()
		},
	)
	defer svc.Close()

	handler := wrapWeixinAdminHandler(nil, svc)
	req := httptest.NewRequest(
		http.MethodGet,
		weixinAdminQREntryPath,
		nil,
	)
	rsp := httptest.NewRecorder()
	handler.ServeHTTP(rsp, req)

	require.Equal(t, http.StatusSeeOther, rsp.Code)
	require.Equal(
		t,
		"https://qr.example.com/latest",
		rsp.Header().Get("Location"),
	)
	require.Equal(
		t,
		"no-store",
		rsp.Header().Get("Cache-Control"),
	)
}

func TestWeixinAdminQREntryShowsWaitingPage(t *testing.T) {
	t.Parallel()

	svc := newWeixinAdminServiceWithRunner(
		[]weixinchannel.AdminTarget{{
			StateDir:       t.TempDir(),
			DefaultBaseURL: "https://ilink.example.com",
		}},
		func(
			ctx context.Context,
			runtimeStateDir string,
			baseURL string,
			botType string,
			callbacks weixinchannel.LoginCallbacks,
		) (weixinchannel.Account, error) {
			if callbacks.OnStatus != nil {
				callbacks.OnStatus(weixinLoginStatusWait)
			}
			<-ctx.Done()
			return weixinchannel.Account{}, ctx.Err()
		},
	)
	defer svc.Close()

	handler := wrapWeixinAdminHandler(nil, svc)
	req := httptest.NewRequest(
		http.MethodGet,
		weixinAdminQREntryPath,
		nil,
	)
	rsp := httptest.NewRecorder()
	handler.ServeHTTP(rsp, req)

	require.Equal(t, http.StatusOK, rsp.Code)
	require.Contains(t, rsp.Body.String(), "Preparing Weixin QR page")
	require.Contains(
		t,
		rsp.Body.String(),
		`http-equiv="refresh" content="2"`,
	)
}

func TestWeixinAdminQREntryShowsLinkedState(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	require.NoError(t, weixinchannel.SaveAccount(
		stateDir,
		weixinchannel.Account{
			AccountID: "acc-1",
			Token:     "tok-1",
			BaseURL:   "https://ilink.example.com",
			UserID:    "user-1",
		},
	))

	svc := newWeixinAdminServiceWithRunner(
		[]weixinchannel.AdminTarget{{
			StateDir:       stateDir,
			DefaultBaseURL: "https://ilink.example.com",
		}},
		nil,
	)
	handler := wrapWeixinAdminHandler(nil, svc)
	req := httptest.NewRequest(
		http.MethodGet,
		weixinAdminQREntryPath,
		nil,
	)
	rsp := httptest.NewRecorder()
	handler.ServeHTTP(rsp, req)

	require.Equal(t, http.StatusOK, rsp.Code)
	require.Contains(
		t,
		rsp.Body.String(),
		"Weixin account already linked",
	)
	require.Contains(t, rsp.Body.String(), "acc-1")
	require.Contains(
		t,
		rsp.Body.String(),
		`href="../channels"`,
	)
}

func TestWeixinAdminQREntryNeedsRuntimeKeyForMultipleRuntimes(
	t *testing.T,
) {
	t.Parallel()

	svc := newWeixinAdminServiceWithRunner(
		[]weixinchannel.AdminTarget{
			{
				StateDir:       t.TempDir(),
				DefaultBaseURL: "https://ilink-a.example.com",
			},
			{
				StateDir:       t.TempDir(),
				DefaultBaseURL: "https://ilink-b.example.com",
			},
		},
		nil,
	)
	handler := wrapWeixinAdminHandler(nil, svc)
	req := httptest.NewRequest(
		http.MethodGet,
		weixinAdminQREntryPath,
		nil,
	)
	rsp := httptest.NewRecorder()
	handler.ServeHTTP(rsp, req)

	require.Equal(t, http.StatusBadRequest, rsp.Code)
	require.Contains(
		t,
		rsp.Body.String(),
		"multiple Weixin runtimes are configured",
	)
}

func TestWeixinAdminPageRendersAccountActions(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	require.NoError(t, weixinchannel.SaveAccount(
		stateDir,
		weixinchannel.Account{
			AccountID: "acc-1",
			Token:     "tok-1",
			BaseURL:   "https://ilinkai.weixin.qq.com",
			UserID:    "user-1",
		},
	))

	svc := newWeixinAdminServiceWithRunner(
		[]weixinchannel.AdminTarget{{
			StateDir:       stateDir,
			DefaultBaseURL: "https://ilinkai.weixin.qq.com",
		}},
		nil,
	)
	handler := wrapChannelsAdminHandler(
		wrapWeixinAdminHandler(nil, svc),
		newChannelsAdminService(nil, svc, nil),
	)

	req := httptest.NewRequest(
		http.MethodGet,
		channelsAdminPagePath,
		nil,
	)
	rsp := httptest.NewRecorder()
	handler.ServeHTTP(rsp, req)

	require.Equal(t, http.StatusOK, rsp.Code)
	require.Contains(
		t,
		rsp.Body.String(),
		`action="api/weixin/accounts/remove"`,
	)
	require.Contains(
		t,
		rsp.Body.String(),
		`action="api/weixin/login/start"`,
	)
	require.Contains(
		t,
		rsp.Body.String(),
		`href="channels/wx_qr?runtime_key=weixin-1"`,
	)
	require.NotContains(
		t,
		rsp.Body.String(),
		`action="/api/weixin/accounts/remove"`,
	)
	require.Contains(t, rsp.Body.String(), "Start QR Login")
	require.Contains(t, rsp.Body.String(), "runtime_key")
	require.NotContains(t, rsp.Body.String(), "Internal Server Error")
}

func TestWeixinAdminLegacyPageRedirectsToChannels(t *testing.T) {
	t.Parallel()

	svc := newWeixinAdminServiceWithRunner(
		[]weixinchannel.AdminTarget{{
			StateDir:       t.TempDir(),
			DefaultBaseURL: "https://ilinkai.weixin.qq.com",
		}},
		nil,
	)
	handler := wrapChannelsAdminHandler(
		wrapWeixinAdminHandler(nil, svc),
		newChannelsAdminService(nil, svc, nil),
	)

	req := httptest.NewRequest(
		http.MethodGet,
		weixinAdminPagePath+"?notice=test-notice",
		nil,
	)
	rsp := httptest.NewRecorder()
	handler.ServeHTTP(rsp, req)

	require.Equal(t, http.StatusSeeOther, rsp.Code)
	require.Equal(
		t,
		"channels?notice=test-notice",
		rsp.Header().Get("Location"),
	)
}

func TestWeixinAdminRedirectLocationUsesRequestRelativePath(
	t *testing.T,
) {
	t.Parallel()

	values := url.Values{}
	values.Set(weixinAdminQueryNotice, "ok")

	require.Equal(
		t,
		"../../../channels?notice=ok#runtime",
		weixinAdminRedirectLocation(
			weixinAdminAccountResumePath,
			values,
			"runtime",
		),
	)
	require.Equal(
		t,
		"channels?notice=ok",
		weixinAdminRedirectLocation(
			weixinAdminPagePath,
			values,
			"",
		),
	)
}

func fetchWeixinAdminStatus(
	t *testing.T,
	handler http.Handler,
) weixinAdminPageData {
	t.Helper()

	req := httptest.NewRequest(
		http.MethodGet,
		weixinAdminStatusPath,
		nil,
	)
	rsp := httptest.NewRecorder()
	handler.ServeHTTP(rsp, req)
	require.Equal(t, http.StatusOK, rsp.Code)

	var payload weixinAdminPageData
	require.NoError(
		t,
		json.Unmarshal(rsp.Body.Bytes(), &payload),
	)
	return payload
}
