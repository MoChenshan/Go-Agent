package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	thttp "git.code.oa.com/trpc-go/trpc-go/http"
	"git.code.oa.com/trpc-go/trpc-go/transport"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/assistantname"
	wecomchannel "git.woa.com/trpc-go/trpc-agent-go/openclaw/channel/wecom"
	weixinchannel "git.woa.com/trpc-go/trpc-agent-go/openclaw/channel/weixin"
	envprobeplugin "git.woa.com/trpc-go/trpc-agent-go/openclaw/plugins/envprobe"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/promptasset"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/releaseinfo"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/runtimectl"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/workspacecfg"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	ocadmin "trpc.group/trpc-go/trpc-agent-go/openclaw/admin"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/app"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/channel"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/runtimeprofile"
)

const (
	testMCPTokenIWikiEnvName = "MCP_IWIKI_ACCESS_TOKEN"

	testLangfuseEnabledEnvName     = "LANGFUSE_ENABLED"
	testLangfuseRequiredEnvName    = "LANGFUSE_REQUIRED"
	testLangfuseUIBaseURLEnvName   = "LANGFUSE_UI_BASE_URL"
	testLangfuseTraceURLEnvName    = "LANGFUSE_TRACE_URL_TEMPLATE"
	testLangfuseObservationEnvName = "LANGFUSE_OBSERVATION_LEAF_VALUE_MAX_BYTES"

	testObservabilityKey       = "observability"
	testLangfuseKey            = "langfuse"
	testLangfuseEnabledKey     = "enabled"
	testLangfuseRequiredKey    = "required"
	testLangfuseUIBaseURLKey   = "ui_base_url"
	testLangfuseTraceURLKey    = "trace_url_template"
	testLangfuseObservationKey = "observation_leaf_value_max_bytes"
)

func unsetEnvForTest(t *testing.T, name string) {
	t.Helper()
	value, ok := os.LookupEnv(name)
	require.NoError(t, os.Unsetenv(name))
	t.Cleanup(func() {
		if !ok {
			require.NoError(t, os.Unsetenv(name))
			return
		}
		require.NoError(t, os.Setenv(name, value))
	})
}

func captureCommandOutput(
	t *testing.T,
	run func(stdout io.Writer, stderr io.Writer) int,
) (int, string, string) {
	t.Helper()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(&stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestRegisterOpenClawHTTPServerTransports(t *testing.T) {
	registerOpenClawHTTPServerTransports()

	protocols := []string{
		serverProtocolHTTP,
		serverProtocolHTTPNoProtocol,
		serverProtocolHTTP2,
		serverProtocolHTTP2NoProto,
	}
	for _, protocol := range protocols {
		t.Run(protocol, func(t *testing.T) {
			got := transport.GetServerTransport(protocol)
			require.NotNil(t, got)
			require.False(
				t,
				httpServerTransportReusePort(t, got),
			)
		})
	}
}

func TestBuildServiceMuxes_MountsGatewayAndA2A(t *testing.T) {
	t.Parallel()

	surface, ok, err := extractRuntimeA2ASurface(
		&fakeRuntimeWithA2A{
			A2A: fakeA2ASurface{
				Handler: http.HandlerFunc(
					func(w http.ResponseWriter, r *http.Request) {
						if r.URL.Path !=
							"/a2a/.well-known/agent-card.json" {
							w.WriteHeader(http.StatusNotFound)
							return
						}
						_, _ = w.Write([]byte("card"))
					},
				),
				BasePath:      "/a2a",
				AgentCardPath: "/a2a/.well-known/agent-card.json",
			},
		},
	)
	require.NoError(t, err)
	require.True(t, ok)

	mux := http.NewServeMux()
	err = mountA2ASurface(surface, mux)
	require.NoError(t, err)

	cardReq := httptest.NewRequest(
		http.MethodGet,
		surface.AgentCardPath,
		nil,
	)
	cardRsp := httptest.NewRecorder()
	mux.ServeHTTP(cardRsp, cardReq)
	require.Equal(t, http.StatusOK, cardRsp.Code)
	require.Equal(t, "card", cardRsp.Body.String())
}

func TestBuildServiceMuxes_RuntimeWithoutA2AField(t *testing.T) {
	t.Parallel()

	rt := &app.Runtime{
		Gateway: app.Gateway{
			Handler: http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/healthz" {
						w.WriteHeader(http.StatusNotFound)
						return
					}
					_, _ = w.Write([]byte("gateway"))
				},
			),
		},
	}

	muxes, err := buildServiceMuxes(rt)
	require.NoError(t, err)

	mux := muxes[trpcServiceName]
	require.NotNil(t, mux)

	gatewayReq := httptest.NewRequest(
		http.MethodGet,
		"/healthz",
		nil,
	)
	gatewayRsp := httptest.NewRecorder()
	mux.ServeHTTP(gatewayRsp, gatewayReq)
	require.Equal(t, http.StatusOK, gatewayRsp.Code)
	require.Equal(t, "gateway", gatewayRsp.Body.String())

	_, hasA2A, err := extractRuntimeA2ASurface(rt)
	require.NoError(t, err)
	require.False(t, hasA2A)
}

func TestBuildServiceMuxes_MountsHTTPIngressServices(t *testing.T) {
	t.Parallel()

	rt := &app.Runtime{
		Gateway: app.Gateway{
			Handler: http.HandlerFunc(
				func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write([]byte("gateway"))
				},
			),
		},
		Channels: []channel.Channel{
			stubHTTPIngressChannel{
				id:      "default",
				pattern: "/callback",
			},
			stubHTTPIngressChannel{
				id:      "custom",
				service: "trpc.openclaw.callback",
				pattern: "/custom/callback",
			},
		},
	}

	muxes, err := buildServiceMuxes(rt)
	require.NoError(t, err)

	defaultMux := muxes[trpcServiceName]
	require.NotNil(t, defaultMux)

	defaultReq := httptest.NewRequest(
		http.MethodGet,
		"/callback",
		nil,
	)
	defaultRsp := httptest.NewRecorder()
	defaultMux.ServeHTTP(defaultRsp, defaultReq)
	require.Equal(t, http.StatusOK, defaultRsp.Code)
	require.Equal(t, "default", defaultRsp.Body.String())

	customMux := muxes["trpc.openclaw.callback"]
	require.NotNil(t, customMux)

	customReq := httptest.NewRequest(
		http.MethodGet,
		"/custom/callback",
		nil,
	)
	customRsp := httptest.NewRecorder()
	customMux.ServeHTTP(customRsp, customReq)
	require.Equal(t, http.StatusOK, customRsp.Code)
	require.Equal(t, "custom", customRsp.Body.String())
}

func TestResolveAdminRuntimeOptionsDefaults(t *testing.T) {
	t.Parallel()

	opts, err := resolveAdminRuntimeOptions("", nil)
	require.NoError(t, err)
	require.True(t, opts.Enabled)
	require.Equal(t, defaultAdminAddr, opts.Addr)
	require.True(t, opts.AutoPort)
}

func TestResolveAdminRuntimeOptionsReadsConfig(t *testing.T) {
	t.Parallel()

	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"admin:\n"+
				"  enabled: true\n"+
				"  addr: \"127.0.0.1:21000\"\n"+
				"  auto_port: false\n",
		),
		0o600,
	)
	require.NoError(t, err)

	opts, err := resolveAdminRuntimeOptions(cfgPath, nil)
	require.NoError(t, err)
	require.True(t, opts.Enabled)
	require.Equal(t, "127.0.0.1:21000", opts.Addr)
	require.False(t, opts.AutoPort)
}

func TestResolveAdminRuntimeOptionsFlagsOverrideConfig(t *testing.T) {
	t.Parallel()

	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"admin:\n"+
				"  enabled: true\n"+
				"  addr: \"127.0.0.1:21000\"\n"+
				"  auto_port: false\n",
		),
		0o600,
	)
	require.NoError(t, err)

	opts, err := resolveAdminRuntimeOptions(
		cfgPath,
		[]string{
			"-admin-enabled=false",
			"-admin-addr", "127.0.0.1:22000",
			"-admin-auto-port=true",
		},
	)
	require.NoError(t, err)
	require.False(t, opts.Enabled)
	require.Equal(t, "127.0.0.1:22000", opts.Addr)
	require.True(t, opts.AutoPort)
}

func TestRuntimeProfileOptionsDefaults(t *testing.T) {
	restore := resetRuntimeProfileProvidersForTest()
	defer restore()

	opts, err := runtimeProfileOptions(
		context.Background(),
		startupPaths{StateDir: "state"},
	)
	require.NoError(t, err)
	require.Empty(t, opts)
}

func TestRuntimeProfileOptionsCollectsProviders(t *testing.T) {
	restore := resetRuntimeProfileProvidersForTest()
	defer restore()

	catalog := runtimeprofile.StaticStore{
		Config: runtimeprofile.Config{
			Profiles: map[string]runtimeprofile.Profile{
				"retail": {AppName: "retail-app"},
			},
		},
	}
	registerRuntimeProfileProvider(runtimeProfileProviderFunc(
		func(
			ctx context.Context,
			paths startupPaths,
		) ([]app.RuntimeOption, error) {
			require.NotNil(t, ctx)
			require.Equal(t, "/tmp/state", paths.StateDir)
			return []app.RuntimeOption{
				nil,
				app.WithRuntimeProfileCatalog(catalog),
			}, nil
		},
	))

	opts, err := runtimeProfileOptions(
		context.Background(),
		startupPaths{StateDir: "/tmp/state"},
	)
	require.NoError(t, err)
	require.Len(t, opts, 1)
}

func TestRuntimeProfileOptionsWrapsProviderError(t *testing.T) {
	restore := resetRuntimeProfileProvidersForTest()
	defer restore()

	wantErr := errors.New("lookup failed")
	registerRuntimeProfileProvider(runtimeProfileProviderFunc(
		func(
			context.Context,
			startupPaths,
		) ([]app.RuntimeOption, error) {
			return nil, wantErr
		},
	))

	_, err := runtimeProfileOptions(context.Background(), startupPaths{})
	require.ErrorIs(t, err, wantErr)
	require.Contains(t, err.Error(), "runtime profile provider 1")
}

func TestRuntimeProfileOptionsFromConfigSelectsUserProfile(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(`runtime_profiles:
  default: base
  profiles:
    base:
      app_name: base-app
      prompt:
        instruction: base prompt
    retail:
      app_name: retail-app
      prompt:
        instruction: retail prompt
  selectors:
    - profile_id: retail
      channels: [wecom]
      users: [u1]
`),
		0o600,
	)
	require.NoError(t, err)

	opts, err := runtimeProfileOptions(
		context.Background(),
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	require.Len(t, opts, 2)

	cfg, ok, err := loadRuntimeProfileSelectorConfig(cfgPath)
	require.NoError(t, err)
	require.True(t, ok)
	resolver, catalog, required, err := runtimeProfileResolverFromConfig(cfg)
	require.NoError(t, err)
	require.True(t, required)

	profile, err := resolver.Resolve(
		context.Background(),
		runtimeprofile.Request{
			Channel: "wecom",
			UserID:  "u1",
		},
	)
	require.NoError(t, err)
	require.Equal(t, "retail", profile.ID)
	require.Equal(t, "retail prompt", profile.Prompt.Instruction)

	_, err = resolver.Resolve(
		context.Background(),
		runtimeprofile.Request{
			Channel: "wecom",
			UserID:  "u2",
		},
	)
	require.ErrorIs(t, err, runtimeprofile.ErrProfileSelectorDenied)

	profile, err = resolver.Resolve(
		context.Background(),
		runtimeprofile.Request{
			Channel:   "wecom",
			ProfileID: "retail",
			UserID:    "u1",
		},
	)
	require.NoError(t, err)
	require.Equal(t, "retail", profile.ID)

	_, err = resolver.Resolve(
		context.Background(),
		runtimeprofile.Request{
			Channel:   "wecom",
			ProfileID: "base",
			UserID:    "u1",
		},
	)
	require.ErrorIs(t, err, runtimeprofile.ErrProfileSelectorDenied)

	ids, err := catalog.ProfileIDs(context.Background())
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"base", "retail"}, ids)
}

func TestRuntimeProfileOptionsFromPreparedConfig(t *testing.T) {
	stateDir := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(`runtime_profiles:
  profiles:
    retail:
      prompt:
        instruction: "${TRPC_CLAW_STATE_DIR}/tenant prompt"
  selectors:
    - profile_id: retail
      users: [u1]
`),
		0o600,
	)
	require.NoError(t, err)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{
			OpenClawConfigPath: cfgPath,
			StateDir:           stateDir,
		},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)
	require.NotEqual(t, cfgPath, preparedPath)

	cfg, ok, err := loadRuntimeProfileSelectorConfig(preparedPath)
	require.NoError(t, err)
	require.True(t, ok)
	resolver, _, _, err := runtimeProfileResolverFromConfig(cfg)
	require.NoError(t, err)

	profile, err := resolver.Resolve(
		context.Background(),
		runtimeprofile.Request{UserID: "u1"},
	)
	require.NoError(t, err)
	require.Equal(
		t,
		filepath.Join(stateDir, "tenant prompt"),
		profile.Prompt.Instruction,
	)
}

func TestRuntimeProfileOptionsFromConfigSkipsPlainProfiles(
	t *testing.T,
) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(`runtime_profiles:
  default: base
  profiles:
    base:
      prompt:
        instruction: base prompt
`),
		0o600,
	)
	require.NoError(t, err)

	opts, err := runtimeProfileOptionsFromConfig(
		context.Background(),
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	require.Empty(t, opts)
}

func TestRuntimeProfileSelectorValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{
			name: "unknown profile",
			config: `runtime_profiles:
  profiles:
    base: {}
  selectors:
    - profile_id: missing
      users: [u1]
`,
			wantErr: `unknown profile_id "missing"`,
		},
		{
			name: "missing criteria",
			config: `runtime_profiles:
  profiles:
    base: {}
  selectors:
    - profile_id: base
`,
			wantErr: "at least one match field is required",
		},
		{
			name: "conflicting aliases",
			config: `runtime_profiles:
  profiles:
    base: {}
    retail: {}
  selectors:
    - profile_id: base
      profile: retail
      users: [u1]
`,
			wantErr: `profile_id "base" conflicts with profile "retail"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
			err := os.WriteFile(cfgPath, []byte(tt.config), 0o600)
			require.NoError(t, err)

			cfg, ok, err := loadRuntimeProfileSelectorConfig(cfgPath)
			require.NoError(t, err)
			require.True(t, ok)
			_, _, _, err = runtimeProfileResolverFromConfig(cfg)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestRunWeixinCommandListAndRemove(t *testing.T) {
	t.Parallel()

	rootStateDir := t.TempDir()
	stateDir := weixinchannel.ResolveStateDir(rootStateDir, "")
	require.NoError(t, weixinchannel.SaveAccount(stateDir, weixinchannel.Account{
		AccountID: "bot-1",
		Token:     "token-1",
		UserID:    "user-1@im.wechat",
	}))

	code, stdout, stderr := captureCommandOutput(
		t,
		func(stdout io.Writer, stderr io.Writer) int {
			return runWeixinCommandWithIO(
				[]string{weixinCmdList},
				rootStateDir,
				stdout,
				stderr,
			)
		},
	)
	require.Equal(t, 0, code)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "bot-1")
	require.Contains(t, stdout, "user-1@im.wechat")

	code, stdout, stderr = captureCommandOutput(
		t,
		func(stdout io.Writer, stderr io.Writer) int {
			return runWeixinCommandWithIO(
				[]string{weixinCmdRemove, "bot-1"},
				rootStateDir,
				stdout,
				stderr,
			)
		},
	)
	require.Equal(t, 0, code)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "Removed Weixin account bot-1")

	accounts, err := weixinchannel.ListAccounts(stateDir)
	require.NoError(t, err)
	require.Empty(t, accounts)
}

func TestRunWeixinCommandLogin(t *testing.T) {
	t.Parallel()

	rootStateDir := t.TempDir()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		switch r.URL.Path {
		case "/ilink/bot/get_bot_qrcode":
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"qrcode":             "qr-1",
				"qrcode_img_content": "https://example.com/qr",
			}))
		case "/ilink/bot/get_qrcode_status":
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"status":        "confirmed",
				"bot_token":     "token-1",
				"ilink_bot_id":  "bot-1",
				"baseurl":       server.URL,
				"ilink_user_id": "user-1@im.wechat",
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	code, stdout, stderr := captureCommandOutput(
		t,
		func(stdout io.Writer, stderr io.Writer) int {
			return runWeixinCommandWithIO([]string{
				weixinCmdLogin,
				"--base-url", server.URL,
				"--timeout", "2s",
			}, rootStateDir, stdout, stderr)
		},
	)
	require.Equal(t, 0, code)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "Saved Weixin account bot-1")

	stateDir := weixinchannel.ResolveStateDir(rootStateDir, "")
	accounts, err := weixinchannel.ListAccounts(stateDir)
	require.NoError(t, err)
	require.Len(t, accounts, 1)
	require.Equal(t, "bot-1", accounts[0].AccountID)
}

func TestResolveAdminConfigPathsSeparatesSourceAndRuntime(t *testing.T) {
	t.Parallel()

	paths, err := resolveAdminConfigPaths(
		"/cfg/openclaw.yaml",
		[]string{"-config", "/tmp/trpc-claw-config-1.yaml"},
	)
	require.NoError(t, err)
	require.Equal(t, "/cfg/openclaw.yaml", paths.Source)
	require.Equal(
		t,
		"/tmp/trpc-claw-config-1.yaml",
		paths.Runtime,
	)
}

func TestResolveAdminConfigPathsFallsBackToRuntimeWhenNeeded(t *testing.T) {
	t.Parallel()

	paths, err := resolveAdminConfigPaths(
		"",
		[]string{"-config", "/tmp/trpc-claw-config-2.yaml"},
	)
	require.NoError(t, err)
	require.Equal(
		t,
		"/tmp/trpc-claw-config-2.yaml",
		paths.Source,
	)
	require.Equal(
		t,
		"/tmp/trpc-claw-config-2.yaml",
		paths.Runtime,
	)
}

func TestWithAdminSourceConfigPathEnv(t *testing.T) {
	_ = os.Unsetenv(adminSourceConfigPathEnv)
	t.Cleanup(func() {
		_ = os.Unsetenv(adminSourceConfigPathEnv)
	})

	restore := withAdminSourceConfigPathEnv(" /cfg/openclaw.yaml ")
	require.Equal(
		t,
		"/cfg/openclaw.yaml",
		os.Getenv(adminSourceConfigPathEnv),
	)
	restore()
	_, ok := os.LookupEnv(adminSourceConfigPathEnv)
	require.False(t, ok)

	require.NoError(
		t,
		os.Setenv(adminSourceConfigPathEnv, "/existing/openclaw.yaml"),
	)
	restore = withAdminSourceConfigPathEnv("/cfg/next.yaml")
	require.Equal(t, "/cfg/next.yaml", os.Getenv(adminSourceConfigPathEnv))
	restore()
	require.Equal(
		t,
		"/existing/openclaw.yaml",
		os.Getenv(adminSourceConfigPathEnv),
	)

	restore = withAdminSourceConfigPathEnv(" ")
	require.Equal(
		t,
		"/existing/openclaw.yaml",
		os.Getenv(adminSourceConfigPathEnv),
	)
	restore()
}

func TestResolveSourceOpenClawConfigPathPrefersAdminEnv(t *testing.T) {
	_ = os.Unsetenv(adminSourceConfigPathEnv)
	t.Cleanup(func() {
		_ = os.Unsetenv(adminSourceConfigPathEnv)
	})

	require.NoError(
		t,
		os.Setenv(
			adminSourceConfigPathEnv,
			"/cfg/source-openclaw.yaml",
		),
	)
	path, err := resolveSourceOpenClawConfigPath(
		[]string{"-config", "/tmp/openclaw-sqlite-1.yaml"},
		"/tmp/openclaw-sqlite-1.yaml",
	)
	require.NoError(t, err)
	require.Equal(t, "/cfg/source-openclaw.yaml", path)
}

func TestResolveSourceOpenClawConfigPathUsesArgsByDefault(t *testing.T) {
	_ = os.Unsetenv(adminSourceConfigPathEnv)
	t.Cleanup(func() {
		_ = os.Unsetenv(adminSourceConfigPathEnv)
	})

	path, err := resolveSourceOpenClawConfigPath(
		[]string{"-config", "/cfg/runtime-openclaw.yaml"},
		"/fallback/openclaw.yaml",
	)
	require.NoError(t, err)
	require.Equal(t, "/cfg/runtime-openclaw.yaml", path)
}

func TestWrapRuntimeAdminHandlerStatus(t *testing.T) {
	t.Parallel()

	manager := newRuntimeLifecycleManager(
		startupPaths{StateDir: t.TempDir()},
		nil,
	)
	handler := wrapRuntimeAdminHandler(nil, manager)

	req := httptest.NewRequest(
		http.MethodGet,
		runtimeAdminStatusPath,
		nil,
	)
	rsp := httptest.NewRecorder()
	handler.ServeHTTP(rsp, req)

	require.Equal(t, http.StatusOK, rsp.Code)
	require.Contains(t, rsp.Body.String(), currentVersion())
}

func TestWrapRuntimeAdminHandlerAction(t *testing.T) {
	t.Parallel()

	manager := newRuntimeLifecycleManager(
		startupPaths{StateDir: t.TempDir()},
		nil,
	)
	handler := wrapRuntimeAdminHandler(nil, manager)

	req := httptest.NewRequest(
		http.MethodPost,
		runtimeAdminActionsPath,
		strings.NewReader(`{"kind":"restart","mode":"graceful"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rsp := httptest.NewRecorder()
	handler.ServeHTTP(rsp, req)

	require.Equal(t, http.StatusOK, rsp.Code)
	require.Contains(t, rsp.Body.String(), "restart")
	require.Contains(t, rsp.Body.String(), "graceful")
}

func TestWrapRuntimeAdminHandlerRuntimeControlActionAccepted(
	t *testing.T,
) {
	t.Parallel()

	manager := newRuntimeLifecycleManager(
		startupPaths{StateDir: t.TempDir()},
		nil,
	)
	handler := wrapRuntimeAdminHandler(nil, manager)

	values := url.Values{
		runtimeActionFormKind:       {"restart"},
		runtimeActionFormMode:       {"graceful"},
		runtimeActionFormReturnPath: {runtimeControlPagePath},
		runtimeActionFormReturnTo: {
			"runtime-control-quick-actions",
		},
	}
	req := httptest.NewRequest(
		http.MethodPost,
		runtimeControlActionPath,
		strings.NewReader(values.Encode()),
	)
	req.Header.Set(
		"Content-Type",
		"application/x-www-form-urlencoded",
	)
	rsp := httptest.NewRecorder()
	handler.ServeHTTP(rsp, req)

	require.Equal(t, http.StatusAccepted, rsp.Code)
	body := rsp.Body.String()
	require.Contains(
		t,
		body,
		runtimeActionTitleRestart,
	)
	require.Contains(
		t,
		body,
		"Requested graceful restart.",
	)
	require.Contains(
		t,
		body,
		"The restart request has been accepted.",
	)
	require.Contains(
		t,
		body,
		"Keep current version",
	)
	require.Contains(
		t,
		body,
		"Action Summary",
	)
	require.Contains(
		t,
		body,
		"Runtime Control is not ready yet.",
	)
	require.Contains(
		t,
		body,
		`href="../../../runtime-control#runtime-control-quick-actions"`,
	)
	require.Contains(
		t,
		body,
		`data-status-url="../status"`,
	)
	require.Contains(
		t,
		body,
		`data-action-id="1"`,
	)
	require.NotContains(t, body, "window.location.assign")

	status := manager.Status()
	require.NotNil(t, status.Pending)
	require.Equal(
		t,
		runtimectl.ActionRestart,
		status.Pending.Kind,
	)
	require.Equal(
		t,
		runtimectl.ModeGraceful,
		status.Pending.Mode,
	)
	require.Equal(t, "admin", status.Pending.Source)
}

func TestWrapRuntimeAdminHandlerUpgradeToLatestAccepted(
	t *testing.T,
) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/latest/VERSION":
				_, _ = w.Write([]byte("v0.0.71\n"))
			case "/releases/v0.0.71/CHANGELOG.md":
				_, _ = w.Write([]byte(`## v0.0.71 (2026-04-14)
- first change
`))
			default:
				http.NotFound(w, r)
			}
		},
	))
	defer server.Close()

	manager := runtimectl.NewManager(runtimectl.Options{
		CurrentVersion: "v0.0.70",
		StateDir:       t.TempDir(),
		ReleaseBaseURL: server.URL,
		HTTPClient:     server.Client(),
	})
	handler := wrapRuntimeAdminHandler(nil, manager)

	values := url.Values{
		runtimeActionFormKind:       {"upgrade"},
		runtimeActionFormMode:       {"graceful"},
		runtimeActionFormReturnPath: {runtimeControlPagePath},
		runtimeActionFormReturnTo: {
			"runtime-control-quick-actions",
		},
	}
	req := httptest.NewRequest(
		http.MethodPost,
		runtimeControlActionPath,
		strings.NewReader(values.Encode()),
	)
	req.Header.Set(
		"Content-Type",
		"application/x-www-form-urlencoded",
	)
	rsp := httptest.NewRecorder()
	handler.ServeHTTP(rsp, req)

	require.Equal(t, http.StatusAccepted, rsp.Code)
	body := rsp.Body.String()
	require.Contains(
		t,
		body,
		runtimeActionTitleUpgrade,
	)
	require.Contains(
		t,
		body,
		"Requested graceful switch to v0.0.71.",
	)
	require.Contains(
		t,
		body,
		`href="../../../runtime-control?version=`+
			`v0.0.71#runtime-control-quick-actions"`,
	)
	require.Contains(
		t,
		body,
		"The upgrade request has been accepted.",
	)
	require.Contains(
		t,
		body,
		`data-status-url="../status"`,
	)
	require.Contains(
		t,
		body,
		`data-action-id="1"`,
	)
	require.NotContains(t, body, "window.location.assign")

	status := manager.Status()
	require.NotNil(t, status.Pending)
	require.Equal(t, "v0.0.71", status.Pending.TargetVersion)
}

func TestWrapRuntimeAdminHandlerRuntimeControlActionError(
	t *testing.T,
) {
	t.Parallel()

	manager := newRuntimeLifecycleManager(
		startupPaths{StateDir: t.TempDir()},
		nil,
	)
	handler := wrapRuntimeAdminHandler(nil, manager)

	values := url.Values{
		runtimeActionFormKind:       {"restart"},
		runtimeActionFormReturnPath: {runtimeControlPagePath},
		runtimeActionFormReturnTo: {
			"runtime-control-quick-actions",
		},
	}
	req := httptest.NewRequest(
		http.MethodPost,
		runtimeControlActionPath,
		strings.NewReader(values.Encode()),
	)
	req.Header.Set(
		"Content-Type",
		"application/x-www-form-urlencoded",
	)
	rsp := httptest.NewRecorder()
	handler.ServeHTTP(rsp, req)

	require.Equal(t, http.StatusSeeOther, rsp.Code)
	require.Equal(
		t,
		"../../../runtime-control?error="+
			"runtime+action+mode+is+required"+
			"#runtime-control-quick-actions",
		rsp.Header().Get("Location"),
	)
}

func TestInjectRuntimeLifecycleController(t *testing.T) {
	t.Parallel()

	manager := runtimectl.NewManager(runtimectl.Options{
		CurrentVersion: "v0.0.48",
	})
	stub := &runtimeAwareTestChannel{}

	injectRuntimeLifecycleController(
		[]channel.Channel{stub},
		manager,
	)

	require.Same(t, manager, stub.manager)
}

func TestRuntimeLifecycleAdminProviderStatusAndAction(t *testing.T) {
	t.Parallel()

	manager := newRuntimeLifecycleManager(
		startupPaths{StateDir: t.TempDir()},
		nil,
	)
	provider := newRuntimeLifecycleAdminProvider(manager)

	status, err := provider.RuntimeLifecycleStatus()
	require.NoError(t, err)
	require.Equal(t, currentVersion(), status.CurrentVersion)
	require.Equal(t, runtimectl.DefaultLifecycleExitCode, status.ExitCode)

	result, err := provider.RequestRuntimeLifecycleAction(
		ocadmin.RuntimeLifecycleActionRequest{
			Kind: "restart",
			Mode: "graceful",
		},
	)
	require.NoError(t, err)
	require.True(t, result.Started)
	require.NotNil(t, result.Status.Pending)
	require.Equal(t, "restart", result.Status.Pending.Kind)
	require.Equal(t, "graceful", result.Status.Pending.Mode)
	require.Equal(t, "admin", result.Status.Pending.Source)
}

func TestRuntimeLifecycleAdminProviderVersionsAndChangelog(
	t *testing.T,
) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/latest/VERSION":
				_, _ = w.Write([]byte("v0.0.71\n"))
			case "/latest/releases.json":
				_, _ = w.Write([]byte(`{
  "latest_version": "v0.0.71",
  "min_supported_target": "v0.0.48",
  "versions": [
    {
      "version": "v0.0.71",
      "notes": ["latest note"]
    }
  ]
}`))
			case "/releases/v0.0.71/CHANGELOG.md":
				_, _ = w.Write([]byte(`## v0.0.71 (2026-04-14)
- first change
- second change
`))
			default:
				http.NotFound(w, r)
			}
		},
	))
	defer server.Close()

	manager := runtimectl.NewManager(runtimectl.Options{
		CurrentVersion: "v0.0.70",
		StateDir:       t.TempDir(),
		ReleaseBaseURL: server.URL,
		HTTPClient:     server.Client(),
	})
	provider := newRuntimeLifecycleAdminProvider(manager)

	index, err := provider.RuntimeLifecycleVersions()
	require.NoError(t, err)
	require.Equal(t, "v0.0.71", index.LatestVersion)
	require.Equal(t, "v0.0.48", index.MinSupportedTarget)
	require.Len(t, index.Versions, 1)
	require.Equal(t, "v0.0.71", index.Versions[0].Version)

	changelog, err := provider.RuntimeLifecycleChangelog("v0.0.71")
	require.NoError(t, err)
	require.Equal(t, "v0.0.71", changelog.Version)
	require.Contains(t, changelog.Changelog, "first change")
	require.NotEmpty(t, changelog.Summary)
}

func TestOpenAdminBindingRelocatesBusyPort(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() {
		require.NoError(t, busy.Close())
	}()

	binding, err := openAdminBinding(busy.Addr().String(), true)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, binding.listener.Close())
	}()

	require.NotEqual(t, busy.Addr().String(), binding.addr)
	require.True(t, binding.relocated)
	require.Equal(t, listenURL(binding.addr), binding.url)
}

func TestOpenAdminBindingFailsWhenBusyAndAutoPortOff(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() {
		require.NoError(t, busy.Close())
	}()

	binding, err := openAdminBinding(busy.Addr().String(), false)
	require.Nil(t, binding)
	require.Error(t, err)
	require.Contains(t, err.Error(), busy.Addr().String())
}

func TestForcedShutdownHandlerIgnoresFirstSignal(
	t *testing.T,
) {
	var exitCodes []int
	handler := newForcedShutdownHandler(func(code int) {
		exitCodes = append(exitCodes, code)
	})

	handler.Handle(os.Interrupt)

	require.Empty(t, exitCodes)
}

func TestForcedShutdownHandlerExitsOnSecondSignal(
	t *testing.T,
) {
	var exitCodes []int
	handler := newForcedShutdownHandler(func(code int) {
		exitCodes = append(exitCodes, code)
	})

	handler.Handle(os.Interrupt)
	handler.Handle(os.Interrupt)

	require.Equal(t, []int{forcedShutdownExitCode}, exitCodes)
}

type stubHTTPIngressChannel struct {
	id      string
	service string
	pattern string
}

type fakeRuntimeWithA2A struct {
	A2A fakeA2ASurface
}

type fakeA2ASurface struct {
	Handler       http.Handler
	BasePath      string
	AgentCardPath string
}

func (s stubHTTPIngressChannel) ID() string {
	return s.id
}

func (s stubHTTPIngressChannel) Run(context.Context) error {
	return nil
}

func (s stubHTTPIngressChannel) HTTPServiceName() string {
	return s.service
}

func (s stubHTTPIngressChannel) HTTPPatterns() []string {
	return []string{s.pattern}
}

func (s stubHTTPIngressChannel) MountHTTP(mux *http.ServeMux) error {
	if mux == nil {
		return fmt.Errorf("nil mux")
	}
	mux.Handle(
		s.pattern,
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(s.id))
		}),
	)
	return nil
}

func httpServerTransportReusePort(
	t *testing.T,
	got transport.ServerTransport,
) bool {
	t.Helper()

	serverTransport, ok := got.(*thttp.ServerTransport)
	require.True(t, ok)

	opts := reflect.ValueOf(serverTransport).Elem().
		FieldByName("opts")
	require.True(t, opts.IsValid())
	require.False(t, opts.IsNil())

	reusePort := opts.Elem().FieldByName("ReusePort")
	require.True(t, reusePort.IsValid())
	return reusePort.Bool()
}

func TestNormalizeOpenClawArgsExpandsStateDirFromConfig(
	t *testing.T,
) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfgPath := filepath.Join(home, "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte("state_dir: \"~/.trpc-agent-go/openclaw\"\n"),
		0o600,
	)
	require.NoError(t, err)

	args, err := normalizeOpenClawArgs([]string{
		"-config", cfgPath,
	})
	require.NoError(t, err)

	value, ok, err := flagValueFromArgs(args, flagStateDir)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(
		t,
		filepath.Join(home, ".trpc-agent-go", "openclaw"),
		value,
	)
}

func TestNormalizeOpenClawArgsInspectSkipsAutoStateDirFlag(
	t *testing.T,
) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfgPath := filepath.Join(home, "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte("state_dir: \"~/.trpc-agent-go/openclaw\"\n"),
		0o600,
	)
	require.NoError(t, err)

	args, paths, err := normalizeOpenClawArgsWithPaths([]string{
		"inspect",
		"plugins",
		"-config",
		cfgPath,
	})
	require.NoError(t, err)
	require.Equal(
		t,
		[]string{"inspect", "plugins", "-config", cfgPath},
		args,
	)
	require.Equal(
		t,
		filepath.Join(home, ".trpc-agent-go", "openclaw"),
		paths.StateDir,
	)

	_, ok, err := flagValueFromArgs(args, flagStateDir)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestNormalizeOpenClawArgsWeixinSkipsAutoStateDirFlag(
	t *testing.T,
) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfgPath := filepath.Join(home, "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte("state_dir: \"~/.trpc-agent-go/openclaw\"\n"),
		0o600,
	)
	require.NoError(t, err)

	args, paths, err := normalizeOpenClawArgsWithPaths([]string{
		subcmdWeixin,
		weixinCmdLogin,
		"-config",
		cfgPath,
	})
	require.NoError(t, err)
	require.Equal(
		t,
		[]string{subcmdWeixin, weixinCmdLogin, "-config", cfgPath},
		args,
	)
	require.Equal(
		t,
		filepath.Join(home, ".trpc-agent-go", "openclaw"),
		paths.StateDir,
	)

	_, ok, err := flagValueFromArgs(args, flagStateDir)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestNormalizeOpenClawArgsExpandsExplicitStateDir(
	t *testing.T,
) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	args, err := normalizeOpenClawArgs([]string{
		"-state-dir", "~/workspace-state",
	})
	require.NoError(t, err)

	value, ok, err := flagValueFromArgs(args, flagStateDir)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(
		t,
		filepath.Join(home, "workspace-state"),
		value,
	)
}

func TestNormalizeOpenClawArgsExpandsConfigEnvPath(
	t *testing.T,
) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfgDir := filepath.Join(home, ".trpc-agent-go", "openclaw")
	err := os.MkdirAll(cfgDir, 0o755)
	require.NoError(t, err)

	cfgPath := filepath.Join(cfgDir, "openclaw.yaml")
	err = os.WriteFile(
		cfgPath,
		[]byte("state_dir: \"~/.trpc-agent-go/openclaw\"\n"),
		0o600,
	)
	require.NoError(t, err)

	t.Setenv(
		openClawConfigEnvName,
		"~/.trpc-agent-go/openclaw/openclaw.yaml",
	)
	args, err := normalizeOpenClawArgs(nil)
	require.NoError(t, err)

	value, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, cfgPath, value)
}

func TestNormalizeOpenClawArgsWithPathsUsesDefaultStateDir(
	t *testing.T,
) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(openClawConfigEnvName, "")

	args, paths, err := normalizeOpenClawArgsWithPaths(nil)
	require.NoError(t, err)
	require.Empty(t, args)
	require.Equal(
		t,
		filepath.Join(home, ".trpc-agent-go", "openclaw"),
		paths.StateDir,
	)
	require.Empty(t, paths.OpenClawConfigPath)
}

func TestNormalizeOpenClawArgsWithPathsExpandsBundledDeps(
	t *testing.T,
) {
	stateDir := t.TempDir()
	root := filepath.Join(
		stateDir,
		skillsDirName,
		bundledSkillsDirName,
	)
	writeBundledSkillDoc(
		t,
		root,
		"anthropic-docx",
		"---\nname: anthropic-docx\nmetadata:\n  openclaw:\n"+
			"    homepage: https://example.com\n---\n",
	)
	writeBundledSkillDoc(
		t,
		root,
		"anthropic-creative",
		"---\nname: anthropic-creative\n---\n",
	)

	args, paths, err := normalizeOpenClawArgsWithPaths([]string{
		subcmdBootstrap,
		bootstrapCmdDeps,
		"-" + flagStateDir,
		stateDir,
		"--" + flagBundled,
	})
	require.NoError(t, err)
	require.Equal(t, stateDir, paths.StateDir)

	value, ok, err := flagValueFromArgs(args, flagSkillsRoot)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, root, value)

	value, ok, err = flagValueFromArgs(args, flagSkill)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "anthropic-docx", value)

	value, ok, err = flagValueFromArgs(args, flagProfile)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, defaultDepsProfile, value)
}

func TestNormalizeOpenClawArgsWithPathsFiltersBundledDepsByOS(
	t *testing.T,
) {
	stateDir := t.TempDir()
	root := filepath.Join(
		stateDir,
		skillsDirName,
		bundledSkillsDirName,
	)
	writeBundledSkillDoc(
		t,
		root,
		"portable-skill",
		"---\nname: portable-skill\nmetadata:\n  openclaw:\n"+
			"    homepage: https://example.com\n---\n",
	)
	writeBundledSkillDoc(
		t,
		root,
		"os-mismatch-skill",
		"---\nname: os-mismatch-skill\nmetadata:\n  openclaw:\n"+
			"    os:\n      - "+bundledDepsTestMismatchOS()+"\n---\n",
	)

	args, _, err := normalizeOpenClawArgsWithPaths([]string{
		subcmdBootstrap,
		bootstrapCmdDeps,
		"-" + flagStateDir,
		stateDir,
		"--" + flagBundled,
	})
	require.NoError(t, err)

	value, ok, err := flagValueFromArgs(args, flagSkill)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "portable-skill", value)
}

func TestNormalizeOpenClawArgsWithPathsKeepsExplicitProfile(
	t *testing.T,
) {
	stateDir := t.TempDir()
	root := filepath.Join(
		stateDir,
		skillsDirName,
		bundledSkillsDirName,
	)
	writeBundledSkillDoc(
		t,
		root,
		"anthropic-pdf",
		"---\nname: anthropic-pdf\nmetadata:\n  openclaw:\n"+
			"    homepage: https://example.com\n---\n",
	)

	args, _, err := normalizeOpenClawArgsWithPaths([]string{
		subcmdInspect,
		inspectCmdDeps,
		"-" + flagStateDir,
		stateDir,
		"--" + flagBundled,
		"-" + flagProfile,
		"pdf",
	})
	require.NoError(t, err)

	value, ok, err := flagValueFromArgs(args, flagProfile)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "pdf", value)
}

func TestNormalizeOpenClawArgsWithPathsRejectsBundledOverrides(
	t *testing.T,
) {
	stateDir := t.TempDir()
	root := filepath.Join(
		stateDir,
		skillsDirName,
		bundledSkillsDirName,
	)
	writeBundledSkillDoc(
		t,
		root,
		"anthropic-xlsx",
		"---\nname: anthropic-xlsx\nmetadata:\n  openclaw:\n"+
			"    homepage: https://example.com\n---\n",
	)

	_, _, err := normalizeOpenClawArgsWithPaths([]string{
		subcmdBootstrap,
		bootstrapCmdDeps,
		"-" + flagStateDir,
		stateDir,
		"--" + flagBundled,
		"-" + flagSkill,
		"manual",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--"+flagBundled)
	require.Contains(t, err.Error(), "--"+flagSkill)
}

func TestResolveBundledSkillsRootUsesRuntimeStateDirEnv(
	t *testing.T,
) {
	t.Setenv(sudoUserEnvName, "")

	stateDir := t.TempDir()
	root := filepath.Join(
		stateDir,
		skillsDirName,
		bundledSkillsDirName,
	)
	require.NoError(t, os.MkdirAll(root, 0o755))
	t.Setenv(runtimeStateDirEnvName, stateDir)

	got, err := resolveBundledSkillsRoot("")
	require.NoError(t, err)
	require.Equal(t, root, got)
}

func TestResolveBundledSkillsRootUsesSudoUserStateDir(
	t *testing.T,
) {
	t.Setenv(runtimeStateDirEnvName, "")
	t.Setenv(sudoUserEnvName, "alice")
	sudoHome := t.TempDir()
	stateDir := filepath.Join(
		sudoHome,
		defaultConfigRootDir,
		defaultConfigAppDir,
	)
	root := filepath.Join(
		stateDir,
		skillsDirName,
		bundledSkillsDirName,
	)
	require.NoError(t, os.MkdirAll(root, 0o755))
	overrideLookupUserFunc(
		t,
		func(name string) (*user.User, error) {
			require.Equal(t, "alice", name)
			return &user.User{HomeDir: sudoHome}, nil
		},
	)

	got, err := resolveBundledSkillsRoot("")
	require.NoError(t, err)
	require.Equal(t, root, got)
}

func TestResolveBundledSkillsRootUsesSourceTreeLayout(
	t *testing.T,
) {
	t.Setenv(runtimeStateDirEnvName, "")
	t.Setenv(sudoUserEnvName, "")

	cwd := t.TempDir()
	root := filepath.Join(
		cwd,
		defaultConfigAppDir,
		skillsDirName,
	)
	require.NoError(t, os.MkdirAll(root, 0o755))

	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(cwd))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(oldWD))
	})

	got, err := resolveBundledSkillsRoot("")
	require.NoError(t, err)
	require.Equal(t, root, got)
}

func TestResolveTRPCConfigPathUsesCurrentDirectory(
	t *testing.T,
) {
	tempDir := t.TempDir()
	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tempDir))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(oldWD))
	})

	cwd, err := os.Getwd()
	require.NoError(t, err)

	cfgPath := filepath.Join(cwd, defaultTRPCConfigFile)
	err = os.WriteFile(
		cfgPath,
		[]byte("server:\n  service: []\n"),
		0o600,
	)
	require.NoError(t, err)

	path, err := resolveTRPCConfigPath("")
	require.NoError(t, err)
	requireSamePath(t, cfgPath, path)
}

func TestResolveTRPCConfigPathUsesHomeDefault(
	t *testing.T,
) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tempDir := t.TempDir()
	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tempDir))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(oldWD))
	})

	cfgDir := filepath.Join(home, ".trpc-agent-go", "openclaw")
	err = os.MkdirAll(cfgDir, 0o755)
	require.NoError(t, err)

	cfgPath := filepath.Join(cfgDir, defaultTRPCConfigFile)
	err = os.WriteFile(
		cfgPath,
		[]byte("server:\n  service: []\n"),
		0o600,
	)
	require.NoError(t, err)

	path, err := resolveTRPCConfigPath("")
	require.NoError(t, err)
	requireSamePath(t, cfgPath, path)
}

func TestResolveTRPCConfigPathExpandsExplicitHomePath(
	t *testing.T,
) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := resolveTRPCConfigPath("~/trpc_go.yaml")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, "trpc_go.yaml"), path)
}

func requireSamePath(t *testing.T, expected string, actual string) {
	t.Helper()

	expectedPath := evalSymlinkPath(t, expected)
	actualPath := evalSymlinkPath(t, actual)
	require.Equal(t, expectedPath, actualPath)
}

func evalSymlinkPath(t *testing.T, path string) string {
	t.Helper()

	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(resolved)
}

func overrideLookupUserFunc(
	t *testing.T,
	fn func(string) (*user.User, error),
) {
	t.Helper()

	previous := lookupUserFunc
	lookupUserFunc = fn
	t.Cleanup(func() {
		lookupUserFunc = previous
	})
}

func TestIsTopLevelHelpRequest(t *testing.T) {
	t.Parallel()

	require.True(t, isTopLevelHelpRequest([]string{"--help"}))
	require.True(t, isTopLevelHelpRequest([]string{"-h"}))
	require.True(t, isTopLevelHelpRequest([]string{"help"}))
	require.False(t, isTopLevelHelpRequest([]string{"inspect", "--help"}))
	require.False(t, isTopLevelHelpRequest(nil))
}

func TestIsTopLevelVersionRequest(t *testing.T) {
	t.Parallel()

	require.True(t, isTopLevelVersionRequest([]string{"version"}))
	require.True(t, isTopLevelVersionRequest([]string{"--version"}))
	require.True(t, isTopLevelVersionRequest([]string{"-version"}))
	require.False(t, isTopLevelVersionRequest([]string{"inspect"}))
	require.False(t, isTopLevelVersionRequest(nil))
}

func TestIsTopLevelUpgradeRequest(t *testing.T) {
	t.Parallel()

	require.True(t, isTopLevelUpgradeRequest([]string{"upgrade"}))
	require.False(t, isTopLevelUpgradeRequest([]string{"version"}))
	require.False(t, isTopLevelUpgradeRequest(nil))
}

func TestIsOpenClawSubcommand(t *testing.T) {
	t.Parallel()

	require.True(t, isOpenClawSubcommand([]string{subcmdPairing}))
	require.True(t, isOpenClawSubcommand([]string{subcmdDoctor}))
	require.True(t, isOpenClawSubcommand([]string{subcmdBootstrap}))
	require.True(t, isOpenClawSubcommand([]string{subcmdInspect}))
	require.False(t, isOpenClawSubcommand([]string{subcmdUpgrade}))
	require.False(t, isOpenClawSubcommand(nil))
}

func TestNormalizeHelpExitCode(t *testing.T) {
	t.Parallel()

	require.Equal(t, 0, normalizeHelpExitCode(2))
	require.Equal(t, 0, normalizeHelpExitCode(0))
	require.Equal(t, 1, normalizeHelpExitCode(1))
}

func TestTopLevelHelpTextIncludesCommonCommandsAndPaths(
	t *testing.T,
) {
	text := topLevelHelpText(topLevelHelpInfo{
		BinaryPath:                "/tmp/bin/trpc-claw",
		OpenClawConfigPath:        "/tmp/openclaw.yaml",
		OpenClawConfigDefaultPath: "/tmp/default-openclaw.yaml",
		TRPCConfigPath:            "/tmp/trpc_go.yaml",
		TRPCConfigDefaultPath:     "/tmp/default-trpc_go.yaml",
		StateDir:                  "/tmp/openclaw-state",
	})

	require.Contains(t, text, "Commands and subcommands:")
	require.Contains(
		t,
		text,
		"trpc-claw upgrade -f --profile "+
			upgradeProfileWeComWS,
	)
	require.Contains(
		t,
		text,
		"trpc-claw bootstrap deps --bundled --apply",
	)
	require.Contains(
		t,
		text,
		"trpc-claw pairing list -config /tmp/openclaw.yaml",
	)
	require.Contains(
		t,
		text,
		"trpc-claw pairing approve <CODE> -config "+
			"/tmp/openclaw.yaml",
	)
	require.Contains(t, text, "trpc-claw version")
	require.Contains(t, text, "trpc-claw help inspect")
	require.Contains(t, text, "Auto-detected paths now:")
	require.Contains(t, text, "More help:")
	require.Contains(t, text, "/tmp/openclaw.yaml")
	require.Contains(t, text, "/tmp/trpc_go.yaml")
	require.Contains(t, text, "/tmp/openclaw-state")
}

func TestNormalizeHelpArgs(t *testing.T) {
	t.Parallel()

	require.Equal(
		t,
		[]string{subcmdInspect, inspectCmdDeps},
		normalizeHelpArgs([]string{
			subcmdInspect,
			inspectCmdDeps,
			helpFlagLong,
		}),
	)
	require.Equal(
		t,
		[]string{subcmdPairing, pairingCmdApprove, "123456"},
		normalizeHelpArgs([]string{
			subcmdHelp,
			subcmdPairing,
			pairingCmdApprove,
			"123456",
		}),
	)
}

func TestResolveHelpTopicInspectIncludesSubcommands(
	t *testing.T,
) {
	topic, ok := resolveHelpTopic(topLevelHelpInfo{
		OpenClawConfigPath: "/tmp/openclaw.yaml",
	}, []string{subcmdInspect})
	require.True(t, ok)

	text := helpTopicText(topic)
	require.Contains(t, text, "trpc-claw inspect")
	require.Contains(t, text, "Subcommands:")
	require.Contains(t, text, inspectCmdPlugins)
	require.Contains(t, text, inspectCmdConfigKeys)
	require.Contains(t, text, "trpc-claw inspect deps [flags]")
}

func TestResolveHelpTopicBootstrapDepsMentionsBundled(
	t *testing.T,
) {
	topic, ok := resolveHelpTopic(topLevelHelpInfo{
		StateDir: "/tmp/openclaw-state",
	}, []string{subcmdBootstrap, bootstrapCmdDeps})
	require.True(t, ok)

	text := helpTopicText(topic)
	require.Contains(
		t,
		text,
		"trpc-claw bootstrap deps --bundled --apply",
	)
	require.Contains(t, text, "`--bundled`")
}

func TestResolveHelpTopicUpgradeIncludesForceConfigAlias(
	t *testing.T,
) {
	topic, ok := resolveHelpTopic(topLevelHelpInfo{}, []string{
		subcmdUpgrade,
	})
	require.True(t, ok)

	text := helpTopicText(topic)
	require.Contains(
		t,
		text,
		"trpc-claw upgrade "+upgradeFlagVersionUsage+" "+
			upgradeFlagChannelUsage+" "+
			"[-f|--force-config] [--profile <name>]",
	)
	require.Contains(t, text, upgradeVersionExample)
	require.Contains(t, text, "trpc-claw upgrade -f")
}

func TestMaybeHandleCommandHelpExplicitTopic(t *testing.T) {
	var buf bytes.Buffer

	handled, code := maybeHandleCommandHelp(
		&buf,
		[]string{subcmdHelp, subcmdInspect},
	)
	require.True(t, handled)
	require.Equal(t, 0, code)
	require.Contains(t, buf.String(), "trpc-claw inspect")
}

func TestMaybeHandleCommandHelpDirectInspectHelp(t *testing.T) {
	var buf bytes.Buffer

	handled, code := maybeHandleCommandHelp(
		&buf,
		[]string{subcmdInspect, helpFlagLong},
	)
	require.True(t, handled)
	require.Equal(t, 0, code)
	require.Contains(t, buf.String(), "trpc-claw inspect")
	require.Contains(t, buf.String(), "Subcommands:")
}

func TestRewriteHelpBinaryName(t *testing.T) {
	t.Parallel()

	raw := "Usage of openclaw:\n" +
		"Usage of pairing:\n" +
		"  openclaw inspect config-keys [openclaw flags]\n" +
		"  openclaw pairing list\n"
	rewritten := rewriteHelpBinaryName(raw)

	require.Contains(t, rewritten, "Usage of trpc-claw:")
	require.Contains(t, rewritten, "Usage of trpc-claw pairing:")
	require.Contains(
		t,
		rewritten,
		"trpc-claw inspect config-keys [trpc-claw flags]",
	)
	require.Contains(t, rewritten, "trpc-claw pairing list")
}

func TestFormatHelpPathFallsBackToDefault(t *testing.T) {
	t.Parallel()

	got := formatHelpPath("", "/tmp/default-openclaw.yaml")
	require.Contains(t, got, "/tmp/default-openclaw.yaml")
	require.Contains(t, got, "(default)")
}

func TestDefaultConfigPathsUseHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	require.Equal(
		t,
		filepath.Join(
			home,
			defaultConfigRootDir,
			defaultConfigAppDir,
			defaultConfigFile,
		),
		defaultConfigPath(),
	)
	require.Equal(
		t,
		filepath.Join(
			home,
			defaultConfigRootDir,
			defaultConfigAppDir,
			defaultTRPCConfigFile,
		),
		defaultTRPCConfigPath(),
	)
}

func TestApplyRuntimeEnvDefaultsSetsFallbacks(t *testing.T) {
	unsetEnvForTest(t, "WECOM_TOKEN")
	unsetEnvForTest(t, testMCPTokenIWikiEnvName)
	t.Setenv(runtimeStateDirEnvName, "")
	t.Setenv(runtimeDocHelperEnvName, "")
	t.Setenv(runtimeBrowserRuntimeEnvName, "")
	t.Setenv(runtimeBrowserMCPBinEnvName, "")
	t.Setenv(runtimeBrowserModeEnvName, "")
	t.Setenv(runtimeBrowserPathEnvName, "")
	t.Setenv(runtimeBrowserHeadlessEnvName, "")
	t.Setenv(runtimeBrowserNameEnvName, "")
	t.Setenv(runtimeBrowserExecPathEnvName, "")
	t.Setenv(runtimeOpenClawBrowserHeadlessEnvName, "")
	t.Setenv(runtimeOpenClawBrowserExecPathEnvName, "")
	t.Setenv(runtimePlaywrightBrowsersEnvName, "")
	t.Setenv(runtimeToolchainDirEnvName, "")
	t.Setenv(runtimeFontsDirEnvName, "")
	t.Setenv(runtimeTessdataDirEnvName, "")
	t.Setenv(runtimeManagedPythonEnvName, "")
	t.Setenv(runtimeToolchainRootEnvName, "")
	t.Setenv(virtualEnvEnvName, "")
	t.Setenv(runtimePIPDisableEnvName, "")
	t.Setenv(runtimeShellEnvFileEnvName, "")
	t.Setenv(runtimeBashEnvName, "")
	t.Setenv(runtimePosixShellEnvName, "")
	t.Setenv(wecomAICallbackPathEnvName, "")
	t.Setenv(wecomNotificationCallbackPathEnvName, "")
	t.Setenv(runtimePathEnvName, "")
	t.Setenv(runtimeTmpDirEnvName, "")
	t.Setenv(runtimeTmpEnvName, "")
	t.Setenv(runtimeTempEnvName, "")

	stateDir := t.TempDir()
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(stateDir, runtimeEnvFileName),
			[]byte(
				"export MCP_IWIKI_ACCESS_TOKEN=runtime-token\n"+
					"WECOM_TOKEN=runtime-wecom-token\n",
			),
			0o600,
		),
	)
	paths := startupPaths{StateDir: stateDir}
	require.NoError(t, applyRuntimeEnvDefaults(paths))

	require.Equal(
		t,
		paths.StateDir,
		os.Getenv(runtimeStateDirEnvName),
	)
	require.Equal(
		t,
		defaultWeComAICallbackPath,
		os.Getenv(wecomAICallbackPathEnvName),
	)
	require.Equal(
		t,
		defaultWeComNotificationCallbackPath,
		os.Getenv(wecomNotificationCallbackPathEnvName),
	)
	require.Equal(
		t,
		runtimeToolchainDir(stateDir),
		os.Getenv(runtimeToolchainDirEnvName),
	)
	require.Equal(
		t,
		runtimeToolchainDir(stateDir),
		os.Getenv(runtimeToolchainRootEnvName),
	)
	require.Equal(
		t,
		runtimeFontsDir(stateDir),
		os.Getenv(runtimeFontsDirEnvName),
	)
	require.Equal(
		t,
		runtimeTessdataDir(stateDir),
		os.Getenv(runtimeTessdataDirEnvName),
	)
	require.Equal(
		t,
		runtimeManagedPythonPath(stateDir),
		os.Getenv(runtimeManagedPythonEnvName),
	)
	require.Equal(
		t,
		runtimeManagedPythonRoot(stateDir),
		os.Getenv(virtualEnvEnvName),
	)
	require.Equal(
		t,
		runtimePIPDisableEnvValue,
		os.Getenv(runtimePIPDisableEnvName),
	)
	tempRoot := workspacecfg.DefaultTempRoot(stateDir)
	require.Equal(t, tempRoot, os.Getenv(runtimeTmpDirEnvName))
	require.Equal(t, tempRoot, os.Getenv(runtimeTmpEnvName))
	require.Equal(t, tempRoot, os.Getenv(runtimeTempEnvName))
	tempInfo, err := os.Stat(tempRoot)
	require.NoError(t, err)
	require.True(t, tempInfo.IsDir())
	helperPath := os.Getenv(runtimeDocHelperEnvName)
	require.Equal(
		t,
		filepath.Join(
			stateDir,
			runtimeToolsDirName,
			runtimeDocHelperName,
		),
		helperPath,
	)
	info, err := os.Stat(helperPath)
	require.NoError(t, err)
	require.False(t, info.IsDir())
	require.Equal(
		t,
		os.FileMode(runtimeSupportFilePerm),
		info.Mode().Perm(),
	)
	shellEnvPath := os.Getenv(runtimeShellEnvFileEnvName)
	require.Equal(
		t,
		filepath.Join(
			stateDir,
			runtimeToolsDirName,
			runtimeShellEnvScriptName,
		),
		shellEnvPath,
	)
	require.Equal(t, shellEnvPath, os.Getenv(runtimeBashEnvName))
	require.Equal(t, shellEnvPath, os.Getenv(runtimePosixShellEnvName))
	shellEnvInfo, err := os.Stat(shellEnvPath)
	require.NoError(t, err)
	require.Equal(
		t,
		os.FileMode(runtimeSupportPrivateFilePerm),
		shellEnvInfo.Mode().Perm(),
	)
	shellEnvBody, err := os.ReadFile(shellEnvPath)
	require.NoError(t, err)
	require.Equal(
		t,
		runtimeShellEnvContent(os.Environ()),
		string(shellEnvBody),
	)
	bashWrapperPath := filepath.Join(
		stateDir,
		runtimeToolsDirName,
		runtimeBashWrapperName,
	)
	bashWrapperBody, err := os.ReadFile(bashWrapperPath)
	require.NoError(t, err)
	require.Equal(
		t,
		runtimeBashWrapperContent(),
		string(bashWrapperBody),
	)
	bashLookPath, err := exec.LookPath(runtimeBashWrapperName)
	require.NoError(t, err)
	require.Equal(t, bashWrapperPath, bashLookPath)
	shWrapperPath := filepath.Join(
		stateDir,
		runtimeToolsDirName,
		runtimeShWrapperName,
	)
	shWrapperBody, err := os.ReadFile(shWrapperPath)
	require.NoError(t, err)
	require.Equal(
		t,
		runtimeShWrapperContent(),
		string(shWrapperBody),
	)
	shLookPath, err := exec.LookPath(runtimeShWrapperName)
	require.NoError(t, err)
	require.Equal(t, shWrapperPath, shLookPath)
	helperBody, err := os.ReadFile(helperPath)
	require.NoError(t, err)
	require.Equal(
		t,
		runtimeDocHelperWrapperContent(),
		string(helperBody),
	)
	scriptBody, err := os.ReadFile(
		filepath.Join(
			stateDir,
			runtimeToolsDirName,
			runtimeDocHelperPython,
		),
	)
	require.NoError(t, err)
	require.Equal(t, runtimeDocHelperScript, string(scriptBody))
	_, err = os.Stat(
		filepath.Join(
			stateDir,
			runtimeToolsDirName,
			runtimeDocHelperPython,
		),
	)
	require.NoError(t, err)
	browserRuntimePath := os.Getenv(runtimeBrowserRuntimeEnvName)
	require.Equal(
		t,
		filepath.Join(
			stateDir,
			runtimeToolsDirName,
			runtimeBrowserRuntimeName,
		),
		browserRuntimePath,
	)
	browserRuntimeBody, err := os.ReadFile(browserRuntimePath)
	require.NoError(t, err)
	require.Equal(
		t,
		runtimeBrowserRuntimeContent(),
		string(browserRuntimeBody),
	)
	require.Equal(
		t,
		runtimeManagedBrowserMCPPath(stateDir),
		os.Getenv(runtimeBrowserMCPBinEnvName),
	)
	require.Equal(
		t,
		runtimePlaywrightDir(stateDir),
		os.Getenv(runtimePlaywrightBrowsersEnvName),
	)
	require.Equal(
		t,
		defaultRuntimeBrowserName(),
		os.Getenv(runtimeBrowserNameEnvName),
	)
	require.Equal(
		t,
		defaultRuntimeBrowserMode(),
		os.Getenv(runtimeBrowserModeEnvName),
	)
	require.Equal(
		t,
		detectRuntimeBrowserExecutablePathForStateDir(stateDir),
		os.Getenv(runtimeBrowserPathEnvName),
	)
	require.Equal(
		t,
		detectRuntimeBrowserExecutablePathForStateDir(stateDir),
		os.Getenv(runtimeBrowserExecPathEnvName),
	)
	require.Equal(
		t,
		detectRuntimeBrowserHeadlessDefault(),
		os.Getenv(runtimeBrowserHeadlessEnvName),
	)
	require.Contains(
		t,
		filepath.SplitList(os.Getenv(runtimePathEnvName)),
		filepath.Join(stateDir, runtimeToolsDirName),
	)
	require.Contains(
		t,
		filepath.SplitList(os.Getenv(runtimePathEnvName)),
		runtimeToolchainBinDir(stateDir),
	)
	require.Contains(
		t,
		filepath.SplitList(os.Getenv(runtimePathEnvName)),
		runtimeManagedPythonBinDir(stateDir),
	)
	require.Equal(
		t,
		"runtime-token",
		os.Getenv(testMCPTokenIWikiEnvName),
	)
	require.Equal(t, "runtime-wecom-token", os.Getenv("WECOM_TOKEN"))
}

func TestApplyRuntimeEnvDefaultsKeepsExistingValues(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv(runtimeStateDirEnvName, stateDir)
	t.Setenv(runtimeDocHelperEnvName, "/tmp/custom-helper")
	t.Setenv(
		runtimeBrowserRuntimeEnvName,
		"/tmp/custom-browser-runtime",
	)
	t.Setenv(runtimeBrowserMCPBinEnvName, "/tmp/custom-playwright-mcp")
	t.Setenv(runtimeBrowserModeEnvName, runtimeBrowserModeInteractive)
	t.Setenv(runtimeBrowserPathEnvName, "/tmp/custom-public-browser")
	t.Setenv(
		runtimeBrowserHeadlessEnvName,
		runtimeBrowserHeadlessDisabledValue,
	)
	t.Setenv(runtimeBrowserNameEnvName, "chrome")
	t.Setenv(
		runtimeBrowserExecPathEnvName,
		"/tmp/custom-browser",
	)
	t.Setenv(runtimeToolchainDirEnvName, "/tmp/custom-toolchain")
	t.Setenv(runtimeFontsDirEnvName, "/tmp/custom-fonts")
	t.Setenv(runtimeTessdataDirEnvName, "/tmp/custom-tessdata")
	t.Setenv(runtimeManagedPythonEnvName, "/tmp/custom-python")
	t.Setenv(runtimeToolchainRootEnvName, "/tmp/custom-toolchain-root")
	t.Setenv(virtualEnvEnvName, "/tmp/custom-venv")
	t.Setenv(runtimePIPDisableEnvName, "0")
	t.Setenv(runtimeShellEnvFileEnvName, "/tmp/custom-shell-env")
	t.Setenv(runtimeBashEnvName, "/tmp/custom-bash-env")
	t.Setenv(runtimePosixShellEnvName, "/tmp/custom-posix-env")
	t.Setenv(wecomAICallbackPathEnvName, "/custom/ai")
	t.Setenv(wecomNotificationCallbackPathEnvName, "/custom/notification")
	t.Setenv(runtimeTmpDirEnvName, "/tmp/custom-tmpdir")
	t.Setenv(runtimeTmpEnvName, "/tmp/custom-tmp")
	t.Setenv(runtimeTempEnvName, "/tmp/custom-temp")
	t.Setenv(testMCPTokenIWikiEnvName, "shell-token")

	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(stateDir, runtimeEnvFileName),
			[]byte(
				testMCPTokenIWikiEnvName+"=runtime-token\n"+
					wecomAICallbackPathEnvName+"=/runtime/ai\n",
			),
			0o600,
		),
	)

	require.NoError(t, applyRuntimeEnvDefaults(startupPaths{
		StateDir: stateDir,
	}))

	require.Equal(
		t,
		stateDir,
		os.Getenv(runtimeStateDirEnvName),
	)
	require.Equal(
		t,
		"/custom/ai",
		os.Getenv(wecomAICallbackPathEnvName),
	)
	require.Equal(
		t,
		"/custom/notification",
		os.Getenv(wecomNotificationCallbackPathEnvName),
	)
	require.Equal(
		t,
		"/tmp/custom-helper",
		os.Getenv(runtimeDocHelperEnvName),
	)
	require.Equal(
		t,
		"/tmp/custom-toolchain",
		os.Getenv(runtimeToolchainDirEnvName),
	)
	require.Equal(
		t,
		"/tmp/custom-browser-runtime",
		os.Getenv(runtimeBrowserRuntimeEnvName),
	)
	require.Equal(
		t,
		"/tmp/custom-playwright-mcp",
		os.Getenv(runtimeBrowserMCPBinEnvName),
	)
	require.Equal(
		t,
		runtimeBrowserModeInteractive,
		os.Getenv(runtimeBrowserModeEnvName),
	)
	require.Equal(
		t,
		"/tmp/custom-public-browser",
		os.Getenv(runtimeBrowserPathEnvName),
	)
	require.Equal(
		t,
		runtimeBrowserHeadlessDisabledValue,
		os.Getenv(runtimeBrowserHeadlessEnvName),
	)
	require.Equal(
		t,
		"chrome",
		os.Getenv(runtimeBrowserNameEnvName),
	)
	require.Equal(
		t,
		"/tmp/custom-browser",
		os.Getenv(runtimeBrowserExecPathEnvName),
	)
	require.Equal(
		t,
		"/tmp/custom-fonts",
		os.Getenv(runtimeFontsDirEnvName),
	)
	require.Equal(
		t,
		"/tmp/custom-tessdata",
		os.Getenv(runtimeTessdataDirEnvName),
	)
	require.Equal(
		t,
		"/tmp/custom-python",
		os.Getenv(runtimeManagedPythonEnvName),
	)
	require.Equal(
		t,
		"/tmp/custom-toolchain-root",
		os.Getenv(runtimeToolchainRootEnvName),
	)
	require.Equal(
		t,
		"/tmp/custom-venv",
		os.Getenv(virtualEnvEnvName),
	)
	require.Equal(
		t,
		"0",
		os.Getenv(runtimePIPDisableEnvName),
	)
	require.Equal(
		t,
		runtimeShellEnvPath(stateDir),
		os.Getenv(runtimeShellEnvFileEnvName),
	)
	require.Equal(
		t,
		runtimeShellEnvPath(stateDir),
		os.Getenv(runtimeBashEnvName),
	)
	require.Equal(
		t,
		runtimeShellEnvPath(stateDir),
		os.Getenv(runtimePosixShellEnvName),
	)
	require.Equal(
		t,
		"/tmp/custom-tmpdir",
		os.Getenv(runtimeTmpDirEnvName),
	)
	require.Equal(t, "/tmp/custom-tmp", os.Getenv(runtimeTmpEnvName))
	require.Equal(t, "/tmp/custom-temp", os.Getenv(runtimeTempEnvName))
	require.Equal(
		t,
		"shell-token",
		os.Getenv(testMCPTokenIWikiEnvName),
	)
}

func TestRuntimeShellEnvContentFiltersAndQuotesValues(t *testing.T) {
	content := runtimeShellEnvContent([]string{
		"PATH=/custom/bin:/usr/bin",
		"OPENAI_API_KEY=key with spaces",
		"QUOTE=it's fine",
		"EMPTY=",
		"1INVALID=value",
		"BAD-NAME=value",
		runtimeShellPWDEnvName + "=/tmp/work",
		"BASH_FUNC_echo%%=() { :; }",
	})

	require.Contains(
		t,
		content,
		"export PATH='/custom/bin:/usr/bin'\n",
	)
	require.Contains(
		t,
		content,
		"export OPENAI_API_KEY='key with spaces'\n",
	)
	require.Contains(
		t,
		content,
		`export QUOTE='it'"'"'s fine'`+"\n",
	)
	require.Contains(t, content, "export EMPTY=''\n")
	require.NotContains(t, content, "1INVALID")
	require.NotContains(t, content, "BAD-NAME")
	require.NotContains(t, content, runtimeShellPWDEnvName+"=")
	require.NotContains(t, content, "BASH_FUNC_echo")
}

func TestRuntimeShellWrappersRestoreEnvForLoginShells(t *testing.T) {
	stateDir := t.TempDir()
	assets := ensureRuntimeSupportAssets(stateDir)
	require.NotEmpty(t, assets.ShellEnvPath)

	fakeBin := filepath.Join(t.TempDir(), "bin")
	require.NoError(t, os.MkdirAll(fakeBin, 0o755))
	fakePythonPath := filepath.Join(fakeBin, runtimePythonExecName)
	require.NoError(
		t,
		os.WriteFile(
			fakePythonPath,
			[]byte("#!/bin/sh\nexit 0\n"),
			0o755,
		),
	)

	const (
		testShellEnvName  = "TRPC_CLAW_TEST_RUNTIME_ENV"
		testShellEnvValue = "shell-env-ok"
	)
	restoredPath := prependPathEntries(
		"/usr/bin:/bin",
		[]string{fakeBin},
	)

	shellEnvContent := runtimeShellEnvContent([]string{
		runtimeShellEnvFileEnvName + "=" + assets.ShellEnvPath,
		runtimeBashEnvName + "=" + assets.ShellEnvPath,
		runtimePosixShellEnvName + "=" + assets.ShellEnvPath,
		runtimePathEnvName + "=" + restoredPath,
		testShellEnvName + "=" + testShellEnvValue,
	})
	require.NoError(
		t,
		writeRuntimeSupportFileMode(
			assets.ShellEnvPath,
			shellEnvContent,
			runtimeSupportPrivateFilePerm,
		),
	)

	testCases := []struct {
		name string
		path string
	}{
		{
			name: runtimeBashWrapperName,
			path: assets.BashWrapperPath,
		},
		{
			name: runtimeShWrapperName,
			path: assets.ShWrapperPath,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(
				tc.path,
				"-lc",
				fmt.Sprintf(
					`printf '%%s\n%%s\n' \
"$(command -v python3)" \
"${%s:-}"`,
					testShellEnvName,
				),
			)
			cmd.Env = []string{
				runtimeShellEnvFileEnvName + "=" + assets.ShellEnvPath,
				runtimePathEnvName + "=/usr/bin:/bin",
				"HOME=" + t.TempDir(),
			}

			output, err := cmd.CombinedOutput()
			require.NoError(t, err, string(output))

			lines := make([]string, 0, 8)
			for _, line := range strings.Split(
				strings.TrimSpace(string(output)),
				"\n",
			) {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				lines = append(lines, line)
			}
			require.GreaterOrEqual(t, len(lines), 2)
			require.Equal(
				t,
				fakePythonPath,
				lines[len(lines)-2],
			)
			require.Equal(
				t,
				testShellEnvValue,
				lines[len(lines)-1],
			)
		})
	}
}

func TestDefaultRuntimeBrowserHeadless(t *testing.T) {
	t.Parallel()

	require.Equal(
		t,
		runtimeBrowserHeadlessEnabledValue,
		defaultRuntimeBrowserHeadless("linux", "", "", true),
	)
	require.Equal(
		t,
		runtimeBrowserHeadlessDisabledValue,
		defaultRuntimeBrowserHeadless("linux", ":0", "", false),
	)
	require.Equal(
		t,
		runtimeBrowserHeadlessDisabledValue,
		defaultRuntimeBrowserHeadless("darwin", "", "", false),
	)
	require.Equal(
		t,
		runtimeBrowserHeadlessDisabledValue,
		defaultRuntimeBrowserHeadless("windows", "", "", false),
	)
}

func TestDefaultRuntimeBrowserMode(t *testing.T) {
	t.Parallel()

	require.Equal(
		t,
		runtimeBrowserModeAuto,
		defaultRuntimeBrowserMode(),
	)
}

func TestDetectRuntimeBrowserExecutablePathWith(t *testing.T) {
	t.Parallel()

	lookups := map[string]string{
		runtimeBrowserExecChromiumBrowser: "/usr/bin/chromium-browser",
	}
	path := detectRuntimeBrowserExecutablePathWith(
		func(name string) (string, error) {
			if value, ok := lookups[name]; ok {
				return value, nil
			}
			return "", exec.ErrNotFound
		},
		func(string) bool { return false },
	)
	require.Equal(t, "/usr/bin/chromium-browser", path)
}

func TestDetectRuntimeBrowserExecutablePathWithNoMatch(t *testing.T) {
	t.Parallel()

	path := detectRuntimeBrowserExecutablePathWith(
		func(string) (string, error) {
			return "", exec.ErrNotFound
		},
		func(string) bool { return false },
	)
	require.Empty(t, path)
}

func TestDetectRuntimeBrowserExecutablePathWithBundleRoot(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), runtimePlaywrightDirName)
	path := filepath.Join(
		root,
		"chromium-1208",
		"chrome-linux64",
		"chrome",
	)
	require.NoError(
		t,
		os.MkdirAll(filepath.Dir(path), 0o755),
	)
	require.NoError(
		t,
		os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755),
	)

	got := detectRuntimeBrowserExecutablePathWith(
		func(string) (string, error) {
			return "", exec.ErrNotFound
		},
		fileExecutable,
		root,
	)
	require.Equal(t, path, got)
}

func TestResolveRuntimeBrowserMode(t *testing.T) {
	t.Parallel()

	require.Equal(
		t,
		runtimeBrowserModeHeadless,
		resolveRuntimeBrowserMode(
			runtimeBrowserModeHeadless,
			"",
			"",
		),
	)
	require.Equal(
		t,
		runtimeBrowserModeInteractive,
		resolveRuntimeBrowserMode(
			"",
			runtimeBrowserHeadlessDisabledValue,
			"",
		),
	)
	require.Equal(
		t,
		runtimeBrowserModeHeadless,
		resolveRuntimeBrowserMode(
			"",
			"",
			runtimeBrowserHeadlessEnabledValue,
		),
	)
}

func TestResolveRuntimeBrowserHeadlessValue(t *testing.T) {
	t.Parallel()

	require.Equal(
		t,
		runtimeBrowserHeadlessEnabledValue,
		resolveRuntimeBrowserHeadlessValue(
			runtimeBrowserModeHeadless,
			"",
			"",
			runtimeBrowserHeadlessDisabledValue,
		),
	)
	require.Equal(
		t,
		runtimeBrowserHeadlessDisabledValue,
		resolveRuntimeBrowserHeadlessValue(
			runtimeBrowserModeAuto,
			"",
			runtimeBrowserHeadlessDisabledValue,
			runtimeBrowserHeadlessEnabledValue,
		),
	)
}

func TestResolveRuntimeBrowserPath(t *testing.T) {
	t.Parallel()

	require.Equal(
		t,
		"/tmp/browser",
		resolveRuntimeBrowserPath(
			"",
			"/tmp/browser",
			"",
		),
	)
	require.Empty(t, resolveRuntimeBrowserPath("", "", ""))
}

func TestDetectRuntimeBrowserMCPPathWith(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	toolchainRoot := runtimeToolchainDir(stateDir)
	managedPath := runtimeManagedBrowserMCPPathFromRoot(toolchainRoot)
	require.NoError(
		t,
		os.MkdirAll(filepath.Dir(managedPath), 0o755),
	)
	require.NoError(
		t,
		os.WriteFile(managedPath, []byte("#!/bin/sh\n"), 0o755),
	)

	got := detectRuntimeBrowserMCPPathWith(
		stateDir,
		func(string) (string, error) {
			return "", exec.ErrNotFound
		},
		fileExecutable,
	)
	require.Equal(t, managedPath, got)
}

func TestDetectRuntimeBrowserMCPPathWithLegacyPath(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	toolchainRoot := runtimeToolchainDir(stateDir)
	legacyPath := runtimeLegacyManagedBrowserMCPPathFromRoot(
		toolchainRoot,
	)
	require.NoError(
		t,
		os.MkdirAll(filepath.Dir(legacyPath), 0o755),
	)
	require.NoError(
		t,
		os.WriteFile(legacyPath, []byte("#!/bin/sh\n"), 0o755),
	)

	got := detectRuntimeBrowserMCPPathWith(
		stateDir,
		func(string) (string, error) {
			return "", exec.ErrNotFound
		},
		fileExecutable,
	)
	require.Equal(t, legacyPath, got)
}

func TestEnsureManagedBrowserRuntimeWhenManagedMCPExists(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	toolchainRoot := runtimeToolchainDir(stateDir)
	managedPath := runtimeManagedBrowserMCPPathFromRoot(toolchainRoot)
	require.NoError(
		t,
		os.MkdirAll(filepath.Dir(managedPath), 0o755),
	)
	require.NoError(
		t,
		os.WriteFile(managedPath, []byte("#!/bin/sh\n"), 0o755),
	)

	ready, warning := ensureManagedBrowserRuntime(stateDir)
	require.True(t, ready)
	require.Empty(t, warning)
}

func TestRuntimeBrowserRuntimeMCPStdioUsesResolvedValues(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	wrapperPath := filepath.Join(
		dir,
		runtimeBrowserRuntimeName,
	)
	require.NoError(
		t,
		os.WriteFile(
			wrapperPath,
			[]byte(runtimeBrowserRuntimeContent()),
			0o755,
		),
	)

	mcpPath := filepath.Join(dir, "playwright-mcp")
	require.NoError(
		t,
		os.WriteFile(
			mcpPath,
			[]byte(
				"#!/bin/sh\n"+
					"printf '%s\\n' \"$@\"\n",
			),
			0o755,
		),
	)

	browserPath := filepath.Join(dir, "chromium-browser")
	require.NoError(
		t,
		os.WriteFile(
			browserPath,
			[]byte("#!/bin/sh\nexit 0\n"),
			0o755,
		),
	)

	cmd := exec.Command(wrapperPath, "mcp-stdio")
	cmd.Env = append(
		os.Environ(),
		runtimeBrowserMCPBinEnvName+"="+mcpPath,
		runtimeBrowserModeEnvName+"="+runtimeBrowserModeHeadless,
		runtimeBrowserPathEnvName+"="+browserPath,
	)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	got := string(output)
	require.Contains(t, got, "--browser")
	require.Contains(t, got, runtimeBrowserNameChromium)
	require.Contains(t, got, "--executable-path")
	require.Contains(t, got, browserPath)
	require.Contains(t, got, "--headless")
}

func TestPrepareOpenClawConfigInjectsBrowserToolProviderWhenReady(
	t *testing.T,
) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"tools:\n"+
				"  providers:\n"+
				"    - type: duckduckgo\n"+
				"      config:\n"+
				"        timeout: 30s\n",
		),
		0o600,
	)
	require.NoError(t, err)

	stateDir := filepath.Join(t.TempDir(), "state")
	toolchainRoot := runtimeToolchainDir(stateDir)
	managedPath := runtimeManagedBrowserMCPPathFromRoot(toolchainRoot)
	require.NoError(
		t,
		os.MkdirAll(filepath.Dir(managedPath), 0o755),
	)
	require.NoError(
		t,
		os.WriteFile(managedPath, []byte("#!/bin/sh\n"), 0o755),
	)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{
			OpenClawConfigPath: cfgPath,
			StateDir:           stateDir,
		},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	toolsNode := mappingValue(documentNode(&root), toolsKey)
	require.NotNil(t, toolsNode)
	providersNode := mappingValue(toolsNode, toolProvidersKey)
	require.NotNil(t, providersNode)
	require.Len(t, providersNode.Content, 3)
	require.True(
		t,
		hasToolProviderType(
			providersNode,
			browserToolProviderTypeName,
		),
	)

	var browserNode *yaml.Node
	for _, provider := range providersNode.Content {
		if mappingStringValue(provider, toolTypeKey) !=
			browserToolProviderTypeName {
			continue
		}
		browserNode = provider
		break
	}
	require.NotNil(t, browserNode)

	configNode := mappingValue(browserNode, toolConfigKey)
	require.NotNil(t, configNode)

	profilesNode := mappingValue(configNode, toolProfilesKey)
	require.NotNil(t, profilesNode)
	require.Len(t, profilesNode.Content, 1)
	require.Equal(
		t,
		browserToolDefaultTimeout,
		mappingStringValue(
			profilesNode.Content[0],
			toolTimeoutKey,
		),
	)
}

func TestPrepareOpenClawConfigSkipsDuplicateBrowserProvider(
	t *testing.T,
) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"tools:\n"+
				"  providers:\n"+
				"    - type: "+browserToolProviderTypeName+"\n"+
				"      config:\n"+
				"        default_profile: openclaw\n"+
				"        profiles:\n"+
				"          - name: openclaw\n"+
				"            transport: stdio\n"+
				"            command: trpc-claw-browser-runtime\n"+
				"            args:\n"+
				"              - mcp-stdio\n",
		),
		0o600,
	)
	require.NoError(t, err)

	stateDir := filepath.Join(t.TempDir(), "state")
	toolchainRoot := runtimeToolchainDir(stateDir)
	managedPath := runtimeManagedBrowserMCPPathFromRoot(toolchainRoot)
	require.NoError(
		t,
		os.MkdirAll(filepath.Dir(managedPath), 0o755),
	)
	require.NoError(
		t,
		os.WriteFile(managedPath, []byte("#!/bin/sh\n"), 0o755),
	)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{
			OpenClawConfigPath: cfgPath,
			StateDir:           stateDir,
		},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	toolsNode := mappingValue(documentNode(&root), toolsKey)
	require.NotNil(t, toolsNode)
	providersNode := mappingValue(toolsNode, toolProvidersKey)
	require.NotNil(t, providersNode)
	require.Len(t, providersNode.Content, 2)
}

func TestSetRuntimeToolchainEnvDefaultsUsesConfiguredRoot(
	t *testing.T,
) {
	customRoot := filepath.Join(
		t.TempDir(),
		"custom-toolchain",
	)
	t.Setenv(runtimeToolchainDirEnvName, customRoot)
	t.Setenv(runtimeFontsDirEnvName, "")
	t.Setenv(runtimeTessdataDirEnvName, "")
	t.Setenv(runtimeManagedPythonEnvName, "")

	setRuntimeToolchainEnvDefaults("/tmp/ignored-state")

	require.Equal(
		t,
		customRoot,
		os.Getenv(runtimeToolchainDirEnvName),
	)
	require.Equal(
		t,
		runtimeFontsDirFromRoot(customRoot),
		os.Getenv(runtimeFontsDirEnvName),
	)
	require.Equal(
		t,
		runtimeTessdataDirFromRoot(customRoot),
		os.Getenv(runtimeTessdataDirEnvName),
	)
	require.Equal(
		t,
		runtimeManagedPythonPathFromRoot(customRoot),
		os.Getenv(runtimeManagedPythonEnvName),
	)
}

func TestEffectiveStartupStateDirPrefersRuntimeEnv(t *testing.T) {
	t.Setenv(runtimeStateDirEnvName, "/tmp/runtime-state")

	require.Equal(
		t,
		"/tmp/runtime-state",
		effectiveStartupStateDir(startupPaths{
			StateDir: "/tmp/config-state",
		}),
	)
}

func TestEffectiveStartupStateDirFallsBackToPaths(t *testing.T) {
	t.Setenv(runtimeStateDirEnvName, "")

	require.Equal(
		t,
		"/tmp/config-state",
		effectiveStartupStateDir(startupPaths{
			StateDir: "/tmp/config-state",
		}),
	)
}

func TestPrepareOpenClawConfigInjectsMemoryInstruction(
	t *testing.T,
) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"agent:\n"+
				"  instruction: \"You are a helpful assistant.\"\n"+
				"memory:\n"+
				"  backend: file\n",
		),
		0o600,
	)
	require.NoError(t, err)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)
	require.NotEqual(t, cfgPath, preparedPath)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))
	agentNode := mappingValue(documentNode(&root), agentKey)
	require.NotNil(t, agentNode)
	require.Equal(
		t,
		appendPromptText(
			"You are a helpful assistant.",
			defaultMemoryInstruction,
		),
		mappingStringValue(agentNode, instructionKey),
	)
}

func TestPrepareOpenClawConfigAppendsMemoryToCustomInstruction(
	t *testing.T,
) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"agent:\n"+
				"  instruction: \"You are a helpful sandbox assistant.\"\n"+
				"memory:\n"+
				"  backend: file\n",
		),
		0o600,
	)
	require.NoError(t, err)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)
	require.NotEqual(t, cfgPath, preparedPath)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))
	agentNode := mappingValue(documentNode(&root), agentKey)
	require.NotNil(t, agentNode)
	require.Equal(
		t,
		appendPromptText(
			"You are a helpful sandbox assistant.",
			defaultMemoryInstruction,
		),
		mappingStringValue(agentNode, instructionKey),
	)
}

func TestDefaultMemoryInstructionExplainsMemoryContract(t *testing.T) {
	t.Parallel()

	require.Contains(t, defaultMemoryInstruction, "fresh instance each session")
	require.Contains(t, defaultMemoryInstruction, "not hidden internal state")
	require.Contains(t, defaultMemoryInstruction, "remember this")
	require.Contains(t, defaultMemoryInstruction, "quote, or summarize it")
	require.Contains(t, defaultMemoryInstruction, "current scope")
	require.Contains(t, defaultMemoryInstruction, "workflow/default rule")
	require.Contains(t, defaultMemoryInstruction, "concrete time schedule")
	require.Contains(t, defaultMemoryInstruction, "inventing a cron")
}

func TestPrepareOpenClawConfigSkipsMemoryInstructionWhenBackendOn(
	t *testing.T,
) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"agent:\n"+
				"  instruction: \"You are a helpful assistant.\"\n"+
				"memory:\n"+
				"  backend: sqlitevec\n",
		),
		0o600,
	)
	require.NoError(t, err)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))
	agentNode := mappingValue(documentNode(&root), agentKey)
	require.NotNil(t, agentNode)
	require.Equal(
		t,
		"You are a helpful assistant.",
		mappingStringValue(agentNode, instructionKey),
	)
}

func TestPrepareOpenClawConfigInjectsPersonaPreset(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"agent:\n"+
				"  persona: concise\n"+
				"  system_prompt: base system\n",
		),
		0o600,
	)
	require.NoError(t, err)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)
	require.NotEqual(t, cfgPath, preparedPath)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))
	agentNode := mappingValue(documentNode(&root), agentKey)
	require.NotNil(t, agentNode)
	require.Nil(t, mappingValue(agentNode, personaKey))
	require.Contains(
		t,
		mappingStringValue(agentNode, systemPromptKey),
		"base system",
	)
	require.Contains(
		t,
		mappingStringValue(agentNode, systemPromptKey),
		"Be direct, brief, and low-friction.",
	)
	require.NotContains(
		t,
		mappingStringValue(agentNode, systemPromptKey),
		"Active preset persona:",
	)
}

func TestPrepareOpenClawConfigInjectsFriendlyPersonaPrompt(
	t *testing.T,
) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte("agent:\n  persona: friendly\n"),
		0o600,
	)
	require.NoError(t, err)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	agentNode := mappingValue(documentNode(&root), agentKey)
	require.NotNil(t, agentNode)
	require.Contains(
		t,
		mappingStringValue(agentNode, systemPromptKey),
		"reduce anxiety",
	)
	require.NotContains(
		t,
		mappingStringValue(agentNode, systemPromptKey),
		"Active preset persona:",
	)
}

func TestPrepareOpenClawConfigPlacesPersonaAfterCodingGuidance(
	t *testing.T,
) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"agent:\n"+
				"  persona: concise\n"+
				"skills:\n"+
				"  coding_agent:\n"+
				"    execution_mode: host\n",
		),
		0o600,
	)
	require.NoError(t, err)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	agentNode := mappingValue(documentNode(&root), agentKey)
	require.NotNil(t, agentNode)

	prompt := mappingStringValue(agentNode, systemPromptKey)
	personaIndex := strings.Index(
		prompt,
		"Be direct, brief, and low-friction.",
	)
	codingIndex := strings.Index(
		prompt,
		runtimeCodingPromptHeader,
	)
	require.NotEqual(t, -1, personaIndex)
	require.NotEqual(t, -1, codingIndex)
	require.Greater(t, personaIndex, codingIndex)
}

func TestPrepareOpenClawConfigInjectsDefaultPersonaWhenUnset(
	t *testing.T,
) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte("agent:\n  system_prompt: base system\n"),
		0o600,
	)
	require.NoError(t, err)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))
	agentNode := mappingValue(documentNode(&root), agentKey)
	require.NotNil(t, agentNode)
	require.Nil(t, mappingValue(agentNode, personaKey))
	require.Contains(
		t,
		mappingStringValue(agentNode, systemPromptKey),
		"Be deeply pragmatic and task-focused.",
	)
}

func TestPrepareOpenClawConfigMapsLegacyDefaultPersona(
	t *testing.T,
) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte("agent:\n  persona: default\n"),
		0o600,
	)
	require.NoError(t, err)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))
	agentNode := mappingValue(documentNode(&root), agentKey)
	require.NotNil(t, agentNode)
	require.Contains(
		t,
		mappingStringValue(agentNode, systemPromptKey),
		"Be deeply pragmatic and task-focused.",
	)
}

func TestPrepareOpenClawConfigRemovesOffPersonaField(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"agent:\n"+
				"  persona: off\n"+
				"  system_prompt: base system\n",
		),
		0o600,
	)
	require.NoError(t, err)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)
	require.NotEqual(t, cfgPath, preparedPath)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))
	agentNode := mappingValue(documentNode(&root), agentKey)
	require.NotNil(t, agentNode)
	require.Nil(t, mappingValue(agentNode, personaKey))
	require.Contains(
		t,
		mappingStringValue(agentNode, systemPromptKey),
		"base system",
	)
	require.NotContains(
		t,
		mappingStringValue(agentNode, systemPromptKey),
		"Active preset persona:",
	)
}

func TestPrepareOpenClawConfigRejectsUnknownPersonaPreset(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte("agent:\n  persona: mystery\n"),
		0o600,
	)
	require.NoError(t, err)

	_, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown agent.persona")
}

func TestPrepareOpenClawConfigRejectsLegacyGFAlias(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte("agent:\n  persona: gf\n"),
		0o600,
	)
	require.NoError(t, err)

	_, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	require.Error(t, err)
	require.Contains(t, err.Error(), `unknown agent.persona "gf"`)
	require.NotContains(t, err.Error(), "friendly, gf")
}

func TestPrepareOpenClawConfigInjectsRuntimeIdentityPrompt(
	t *testing.T,
) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"agent:\n"+
				"  system_prompt: base system\n"+
				"model:\n"+
				"  mode: openai\n"+
				"  name: gpt-5.2\n",
		),
		0o600,
	)
	require.NoError(t, err)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{
			"-config", cfgPath,
			"-model", "deepseek-chat",
			"-openai-variant", "deepseek",
			"-openai-base-url",
			"https://api.deepseek.com/v1",
		},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)

	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	agentNode := mappingValue(documentNode(&root), agentKey)
	require.NotNil(t, agentNode)

	systemPrompt := mappingStringValue(agentNode, systemPromptKey)
	require.Contains(t, systemPrompt, "base system")
	require.Contains(t, systemPrompt, runtimeIdentityPromptHeader)
	require.Contains(t, systemPrompt, "Runtime model name: deepseek-chat")
	require.Contains(
		t,
		systemPrompt,
		"Runtime OpenAI variant: deepseek",
	)
	require.Contains(
		t,
		systemPrompt,
		"https://api.deepseek.com/v1",
	)
}

func TestPrepareOpenClawConfigStripsLegacyInlineSystemPrompt(
	t *testing.T,
) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"agent:\n"+
				"  system_prompt: You are tRPC-Claw....\n",
		),
		0o600,
	)
	require.NoError(t, err)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)

	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	agentNode := mappingValue(documentNode(&root), agentKey)
	require.NotNil(t, agentNode)

	systemPrompt := mappingStringValue(agentNode, systemPromptKey)
	require.NotContains(t, systemPrompt, "You are tRPC-Claw....")
	require.NotContains(
		t,
		systemPrompt,
		"Runtime identity: You are trpc-claw.",
	)
	require.Contains(t, systemPrompt, "Current assistant name:")
	require.Contains(t, systemPrompt, "Runtime product: trpc-claw")
}

func TestPrepareOpenClawConfigUsesExpandedStateDirForName(
	t *testing.T,
) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	stateDir := filepath.Join(home, ".trpc-agent-go", "openclaw")
	require.NoError(t, assistantname.WriteFile(
		promptasset.DefaultPaths(stateDir).IdentityFile,
		"chord",
	))

	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"state_dir: \"~/.trpc-agent-go/openclaw\"\n"+
				"agent:\n"+
				"  system_prompt: You are tRPC-Claw....\n",
		),
		0o600,
	)
	require.NoError(t, err)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{
			OpenClawConfigPath: cfgPath,
			StateDir:           stateDir,
		},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)

	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	agentNode := mappingValue(documentNode(&root), agentKey)
	require.NotNil(t, agentNode)

	systemPrompt := mappingStringValue(agentNode, systemPromptKey)
	require.Contains(t, systemPrompt, "Current assistant name: chord")
	require.NotContains(t, systemPrompt, "Current assistant name: trpc-claw")
}

func TestPrepareOpenClawConfigUsesCustomSystemPromptDir(t *testing.T) {
	cfgDir := t.TempDir()
	systemDir := filepath.Join(cfgDir, "system")
	err := os.MkdirAll(systemDir, 0o755)
	require.NoError(t, err)
	err = os.WriteFile(
		filepath.Join(systemDir, "01_custom.md"),
		[]byte("Only the custom system prompt should remain."),
		0o600,
	)
	require.NoError(t, err)

	cfgPath := filepath.Join(cfgDir, "openclaw.yaml")
	err = os.WriteFile(
		cfgPath,
		[]byte(
			"agent:\n"+
				"  system_prompt_dir: ./system\n",
		),
		0o600,
	)
	require.NoError(t, err)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)

	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	agentNode := mappingValue(documentNode(&root), agentKey)
	require.NotNil(t, agentNode)

	systemPrompt := mappingStringValue(agentNode, systemPromptKey)
	require.Contains(
		t,
		systemPrompt,
		"Only the custom system prompt should remain.",
	)
	require.NotContains(t, systemPrompt, runtimeIdentityPromptHeader)
	require.NotContains(t, systemPrompt, runtimeCodingPromptHeader)
}

func TestPrepareOpenClawConfigExpandsModelEnvRefs(
	t *testing.T,
) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"model:\n"+
				"  mode: openai\n"+
				"  name: ${OPENAI_MODEL}\n"+
				"  base_url: ${OPENAI_BASE_URL}\n",
		),
		0o600,
	)
	require.NoError(t, err)
	t.Setenv(openAIModelEnvName, "deepseek-v3.2")
	t.Setenv(openAIBaseURLEnvName, "http://v2.open.venus.oa.com/llmproxy")

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)

	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	modelNode := mappingValue(documentNode(&root), modelSectionKey)
	require.NotNil(t, modelNode)
	require.Equal(
		t,
		"deepseek-v3.2",
		mappingStringValue(modelNode, modelNameKey),
	)
	require.Equal(
		t,
		"http://v2.open.venus.oa.com/llmproxy",
		mappingStringValue(modelNode, modelBaseURLKey),
	)
}

func TestPrepareOpenClawConfigExpandsLangfuseEnvRefs(
	t *testing.T,
) {
	const (
		testLangfuseUIBaseURLValue = "https://langfuse.example.com"
		testLangfuseTraceURLValue  = "https://langfuse.example.com/trace/{{trace_id}}"
	)

	testCases := []struct {
		name            string
		env             map[string]string
		wantEnabled     string
		wantRequired    string
		wantUIBaseURL   string
		wantTraceURL    string
		wantObservation string
	}{
		{
			name:            "defaults",
			wantEnabled:     "true",
			wantRequired:    "false",
			wantObservation: "4096",
		},
		{
			name: "overrides",
			env: map[string]string{
				testLangfuseEnabledEnvName:     "false",
				testLangfuseRequiredEnvName:    "true",
				testLangfuseUIBaseURLEnvName:   testLangfuseUIBaseURLValue,
				testLangfuseTraceURLEnvName:    testLangfuseTraceURLValue,
				testLangfuseObservationEnvName: "8192",
			},
			wantEnabled:     "false",
			wantRequired:    "true",
			wantUIBaseURL:   testLangfuseUIBaseURLValue,
			wantTraceURL:    testLangfuseTraceURLValue,
			wantObservation: "8192",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
			err := os.WriteFile(
				cfgPath,
				[]byte(
					"observability:\n"+
						"  langfuse:\n"+
						"    enabled: ${"+testLangfuseEnabledEnvName+":-true}\n"+
						"    required: ${"+testLangfuseRequiredEnvName+":-false}\n"+
						`    ui_base_url: "${`+testLangfuseUIBaseURLEnvName+`:-}"`+"\n"+
						`    trace_url_template: "${`+testLangfuseTraceURLEnvName+`:-}"`+"\n"+
						"    observation_leaf_value_max_bytes: "+
						"${"+testLangfuseObservationEnvName+":-4096}\n",
				),
				0o600,
			)
			require.NoError(t, err)

			unsetEnvForTest(t, testLangfuseEnabledEnvName)
			unsetEnvForTest(t, testLangfuseRequiredEnvName)
			unsetEnvForTest(t, testLangfuseUIBaseURLEnvName)
			unsetEnvForTest(t, testLangfuseTraceURLEnvName)
			unsetEnvForTest(t, testLangfuseObservationEnvName)
			for key, value := range tc.env {
				t.Setenv(key, value)
			}

			args, cleanup, err := prepareOpenClawConfig(
				[]string{"-config", cfgPath},
				startupPaths{OpenClawConfigPath: cfgPath},
			)
			if cleanup != nil {
				t.Cleanup(cleanup)
			}
			require.NoError(t, err)

			preparedPath, ok, err := flagValueFromArgs(
				args,
				flagConfig,
			)
			require.NoError(t, err)
			require.True(t, ok)

			prepared, err := os.ReadFile(preparedPath)
			require.NoError(t, err)

			var root yaml.Node
			require.NoError(t, yaml.Unmarshal(prepared, &root))

			observabilityNode := mappingValue(
				documentNode(&root),
				testObservabilityKey,
			)
			require.NotNil(t, observabilityNode)
			langfuseNode := mappingValue(
				observabilityNode,
				testLangfuseKey,
			)
			require.NotNil(t, langfuseNode)
			require.Equal(
				t,
				tc.wantEnabled,
				mappingStringValue(
					langfuseNode,
					testLangfuseEnabledKey,
				),
			)
			require.Equal(
				t,
				tc.wantRequired,
				mappingStringValue(
					langfuseNode,
					testLangfuseRequiredKey,
				),
			)
			require.Equal(
				t,
				tc.wantUIBaseURL,
				mappingStringValue(
					langfuseNode,
					testLangfuseUIBaseURLKey,
				),
			)
			require.Equal(
				t,
				tc.wantTraceURL,
				mappingStringValue(
					langfuseNode,
					testLangfuseTraceURLKey,
				),
			)
			require.Equal(
				t,
				tc.wantObservation,
				mappingStringValue(
					langfuseNode,
					testLangfuseObservationKey,
				),
			)
		})
	}
}

func TestPrepareOpenClawConfigRejectsMissingModelEnvRefs(
	t *testing.T,
) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"model:\n"+
				"  mode: openai\n"+
				"  name: ${OPENAI_MODEL}\n"+
				"  base_url: ${OPENAI_BASE_URL}\n",
		),
		0o600,
	)
	require.NoError(t, err)
	unsetEnvForTest(t, openAIModelEnvName)
	unsetEnvForTest(t, openAIBaseURLEnvName)

	_, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	require.Error(t, err)
	require.Contains(
		t,
		err.Error(),
		"config: env var "+openAIModelEnvName+" is not set",
	)
}

func TestPrepareOpenClawConfigRejectsMissingModelBaseURLEnvRef(
	t *testing.T,
) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"model:\n"+
				"  mode: openai\n"+
				"  name: ${OPENAI_MODEL}\n"+
				"  base_url: ${OPENAI_BASE_URL}\n",
		),
		0o600,
	)
	require.NoError(t, err)
	t.Setenv(openAIModelEnvName, "deepseek-v3.2")
	unsetEnvForTest(t, openAIBaseURLEnvName)

	_, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	require.Error(t, err)
	require.Contains(
		t,
		err.Error(),
		"config: env var "+openAIBaseURLEnvName+" is not set",
	)
}

func TestResolveConfigEnvRefAllowsMissingWeComGroupSessionMode(
	t *testing.T,
) {
	unsetEnvForTest(t, wecomGroupSessionModeEnvName)

	value, err := resolveConfigEnvRef(
		wecomGroupSessionModeEnvName,
		"",
	)
	require.NoError(t, err)
	require.Empty(t, value)
}

func TestPrepareOpenClawConfigExpandsGroupSessionModeEnvRef(
	t *testing.T,
) {
	testCases := []struct {
		name      string
		envValue  string
		setEnv    bool
		wantValue string
	}{
		{
			name:      "default",
			wantValue: "group_session_mode: \"\"",
		},
		{
			name:      "override",
			envValue:  "isolated",
			setEnv:    true,
			wantValue: "group_session_mode: isolated",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
			err := os.WriteFile(
				cfgPath,
				[]byte(
					"channels:\n"+
						"  - type: wecom\n"+
						"    name: test\n"+
						"    config:\n"+
						"      bot_mode: ai\n"+
						`      group_session_mode: `+
						"${WECOM_GROUP_SESSION_MODE}\n",
				),
				0o600,
			)
			require.NoError(t, err)

			unsetEnvForTest(t, wecomGroupSessionModeEnvName)
			if tc.setEnv {
				t.Setenv(
					wecomGroupSessionModeEnvName,
					tc.envValue,
				)
			}

			args, cleanup, err := prepareOpenClawConfig(
				[]string{"-config", cfgPath},
				startupPaths{
					OpenClawConfigPath: cfgPath,
				},
			)
			if cleanup != nil {
				t.Cleanup(cleanup)
			}
			require.NoError(t, err)

			preparedPath, ok, err := flagValueFromArgs(
				args,
				flagConfig,
			)
			require.NoError(t, err)
			require.True(t, ok)

			prepared, err := os.ReadFile(preparedPath)
			require.NoError(t, err)
			require.Contains(
				t,
				string(prepared),
				tc.wantValue,
			)
		})
	}
}

func TestRunCleanup(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		require.NotPanics(t, func() {
			runCleanup(nil)
		})
	})

	t.Run("non-nil", func(t *testing.T) {
		called := false
		require.NotPanics(t, func() {
			runCleanup(func() {
				called = true
			})
		})
		require.True(t, called)
	})
}

func TestPrepareOpenClawConfigPrefersConfigAdjacentSkills(
	t *testing.T,
) {
	const adjacentSkillName = "find-skills"

	tempRoot := t.TempDir()
	cfgDir := filepath.Join(tempRoot, "openclaw")
	stateDir := filepath.Join(tempRoot, "state")
	sourceSkillsDir := filepath.Join(cfgDir, skillsDirName)
	bundledSkillsDir := filepath.Join(
		stateDir,
		skillsDirName,
		bundledSkillsDirName,
	)

	require.NoError(t, os.MkdirAll(sourceSkillsDir, 0o755))
	require.NoError(t, os.MkdirAll(bundledSkillsDir, 0o755))
	require.NoError(
		t,
		os.MkdirAll(
			filepath.Join(
				sourceSkillsDir,
				adjacentSkillName,
			),
			0o755,
		),
	)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(
				sourceSkillsDir,
				adjacentSkillName,
				skillDocFileName,
			),
			[]byte("name: "+adjacentSkillName+"\n"),
			0o600,
		),
	)

	cfgPath := filepath.Join(cfgDir, "openclaw.yaml")
	require.NoError(
		t,
		os.WriteFile(
			cfgPath,
			[]byte(
				"skills:\n"+
					"  root: ${TRPC_CLAW_STATE_DIR}/skills/bundled\n",
			),
			0o600,
		),
	)
	t.Setenv(
		codexHomeEnvName,
		filepath.Join(tempRoot, "missing-codex-home"),
	)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{
			OpenClawConfigPath: cfgPath,
			StateDir:           stateDir,
		},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)
	require.NotEqual(t, cfgPath, preparedPath)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)

	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	skillsNode := mappingValue(documentNode(&root), skillsKey)
	require.NotNil(t, skillsNode)
	require.Equal(
		t,
		sourceSkillsDir,
		mappingStringValue(skillsNode, skillsRootKey),
	)
	extraDirsNode := mappingValue(skillsNode, extraDirsKey)
	require.NotNil(t, extraDirsNode)
	require.True(
		t,
		sequenceContainsPath(extraDirsNode, bundledSkillsDir),
	)
}

func TestPrepareOpenClawConfigInjectsConfiguredSkillRootsIntoGuidance(
	t *testing.T,
) {
	const adjacentSkillName = "find-skills"

	tempRoot := t.TempDir()
	cfgDir := filepath.Join(tempRoot, "openclaw")
	stateDir := filepath.Join(tempRoot, "state")
	sourceSkillsDir := filepath.Join(cfgDir, skillsDirName)
	bundledSkillsDir := filepath.Join(
		stateDir,
		skillsDirName,
		bundledSkillsDirName,
	)

	require.NoError(t, os.MkdirAll(sourceSkillsDir, 0o755))
	require.NoError(t, os.MkdirAll(bundledSkillsDir, 0o755))
	require.NoError(
		t,
		os.MkdirAll(
			filepath.Join(
				sourceSkillsDir,
				adjacentSkillName,
			),
			0o755,
		),
	)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(
				sourceSkillsDir,
				adjacentSkillName,
				skillDocFileName,
			),
			[]byte("name: "+adjacentSkillName+"\n"),
			0o600,
		),
	)

	cfgPath := filepath.Join(cfgDir, "openclaw.yaml")
	require.NoError(
		t,
		os.WriteFile(
			cfgPath,
			[]byte(
				"skills:\n"+
					"  root: ${TRPC_CLAW_STATE_DIR}/skills/bundled\n"+
					"  coding_agent:\n"+
					"    execution_mode: host\n",
			),
			0o600,
		),
	)
	t.Setenv(
		codexHomeEnvName,
		filepath.Join(tempRoot, "missing-codex-home"),
	)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{
			OpenClawConfigPath: cfgPath,
			StateDir:           stateDir,
		},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)

	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	skillsNode := mappingValue(documentNode(&root), skillsKey)
	require.NotNil(t, skillsNode)

	guidance := mappingStringValue(skillsNode, toolingGuidanceKey)
	require.Contains(
		t,
		guidance,
		"Treat the skill overview below as the skills "+
			"available in this session.",
	)
	require.Contains(
		t,
		guidance,
		"Do not answer a matching skill task from the short "+
			"summary, prior knowledge, or partial memory.",
	)
	require.Contains(
		t,
		guidance,
		"Each entry includes a path to that skill's "+
			"`SKILL.md` on disk.",
	)
	require.Contains(t, guidance, "Keep exploring nearby runtime facts")
}

func TestPrepareOpenClawConfigKeepsInstalledBundledSkillsRoot(
	t *testing.T,
) {
	tempRoot := t.TempDir()
	stateDir := filepath.Join(tempRoot, "state")
	bundledSkillDir := filepath.Join(
		stateDir,
		skillsDirName,
		bundledSkillsDirName,
		"pdf",
	)
	require.NoError(t, os.MkdirAll(bundledSkillDir, 0o755))
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(bundledSkillDir, skillDocFileName),
			[]byte("name: pdf\n"),
			0o600,
		),
	)

	cfgPath := filepath.Join(stateDir, "openclaw.yaml")
	require.NoError(
		t,
		os.WriteFile(
			cfgPath,
			[]byte(
				"skills:\n"+
					"  root: ${TRPC_CLAW_STATE_DIR}/skills/bundled\n",
			),
			0o600,
		),
	)
	t.Setenv(
		codexHomeEnvName,
		filepath.Join(tempRoot, "missing-codex-home"),
	)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{
			OpenClawConfigPath: cfgPath,
			StateDir:           stateDir,
		},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)

	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	skillsNode := mappingValue(documentNode(&root), skillsKey)
	require.NotNil(t, skillsNode)
	require.Equal(
		t,
		filepath.Join(
			stateDir,
			skillsDirName,
			bundledSkillsDirName,
		),
		mappingStringValue(skillsNode, skillsRootKey),
	)
}

func TestPrepareOpenClawConfigReturnsNoopCleanupOnError(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"model:\n"+
				"  mode: openai\n"+
				"  name: ${OPENAI_MODEL}\n",
		),
		0o600,
	)
	require.NoError(t, err)
	unsetEnvForTest(t, openAIModelEnvName)

	_, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.Error(t, err)
	require.NotNil(t, cleanup)
	require.NotPanics(t, cleanup)
}

func TestPrepareOpenClawConfigFallsBackSQLiteForDeepSeek(
	t *testing.T,
) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"memory:\n"+
				"  backend: sqlitevec\n"+
				"  config:\n"+
				"    path: ${TRPC_CLAW_STATE_DIR}/memories_vec.sqlite\n"+
				"    embedder:\n"+
				"      type: openai\n"+
				"      model: text-embedding-3-small\n",
		),
		0o600,
	)
	require.NoError(t, err)

	t.Setenv(openAIBaseURLEnvName, "https://api.deepseek.com/v1")
	t.Setenv(openAIAPIKeyEnvName, "")

	var warningBuf bytes.Buffer
	prevEmitter := configWarningEmitter
	configWarningEmitter = func(message string) {
		_, _ = warningBuf.WriteString(message + "\n")
	}
	t.Cleanup(func() {
		configWarningEmitter = prevEmitter
	})

	args, cleanup, err := prepareOpenClawConfig(
		[]string{
			"-config", cfgPath,
			"-model", "deepseek-chat",
			"-memory-backend", memoryBackendSQLiteVecName,
		},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	backendValue, ok, err := flagValueFromArgs(args, flagMemoryBackend)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, memoryBackendSQLiteName, backendValue)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)

	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	memoryNode := mappingValue(documentNode(&root), memoryKey)
	require.NotNil(t, memoryNode)
	require.Equal(
		t,
		memoryBackendSQLiteName,
		mappingStringValue(memoryNode, memoryBackendKey),
	)

	configNode := mappingValue(memoryNode, memoryConfigKey)
	require.NotNil(t, configNode)
	require.Equal(
		t,
		runtimeStateDirEnvRef+"/"+defaultSQLiteMemoryDBFileName,
		mappingStringValue(configNode, sqliteConfigPathKey),
	)
	require.Nil(t, mappingValue(configNode, embedderKey))
	require.NotContains(t, warningBuf.String(), "DeepSeek chat model")
	require.Contains(
		t,
		warningBuf.String(),
		"switched sqlitevec to sqlite",
	)
}

func TestPrepareOpenClawConfigKeepsSQLiteVecWhenFallbackDisabled(
	t *testing.T,
) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"memory:\n"+
				"  backend: sqlitevec\n"+
				"  fallback_to_sqlite_on_embedding_unsupported: false\n"+
				"  config:\n"+
				"    path: ${TRPC_CLAW_STATE_DIR}/memories_vec.sqlite\n",
		),
		0o600,
	)
	require.NoError(t, err)

	t.Setenv(openAIBaseURLEnvName, "https://api.deepseek.com/v1")

	var warningBuf bytes.Buffer
	prevEmitter := configWarningEmitter
	configWarningEmitter = func(message string) {
		_, _ = warningBuf.WriteString(message + "\n")
	}
	t.Cleanup(func() {
		configWarningEmitter = prevEmitter
	})

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)

	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	memoryNode := mappingValue(documentNode(&root), memoryKey)
	require.NotNil(t, memoryNode)
	require.Equal(
		t,
		memoryBackendSQLiteVecName,
		mappingStringValue(memoryNode, memoryBackendKey),
	)
	require.Nil(t, mappingValue(memoryNode, memoryFallbackKey))
	require.Empty(t, warningBuf.String())
}

func TestPrepareOpenClawConfigInjectsCodingAgentGuidance(
	t *testing.T,
) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"skills:\n"+
				"  coding_agent:\n"+
				"    execution_mode: host\n",
		),
		0o600,
	)
	require.NoError(t, err)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	skillsNode := mappingValue(documentNode(&root), skillsKey)
	require.NotNil(t, skillsNode)
	require.Nil(t, mappingValue(skillsNode, codingAgentKey))
	entriesNode := mappingValue(skillsNode, skillsEntriesKey)
	require.NotNil(t, entriesNode)
	entryNode := mappingValue(entriesNode, codingAgentSkillName)
	require.NotNil(t, entryNode)
	require.Contains(
		t,
		mappingStringValue(skillsNode, toolingGuidanceKey),
		"Treat the skill overview below as the skills "+
			"available in this session.",
	)
	require.Contains(
		t,
		mappingStringValue(skillsNode, toolingGuidanceKey),
		"Do not answer a matching skill task from the short "+
			"summary, prior knowledge, or partial memory.",
	)
	require.Contains(
		t,
		mappingStringValue(skillsNode, toolingGuidanceKey),
		"Each entry includes a path to that skill's "+
			"`SKILL.md` on disk.",
	)
	require.Contains(
		t,
		mappingStringValue(skillsNode, toolingGuidanceKey),
		"Never say that you could read or load a matching "+
			"skill later",
	)
	require.Contains(
		t,
		mappingStringValue(skillsNode, toolingGuidanceKey),
		"Read only the supporting docs, scripts, assets, "+
			"examples, or templates",
	)
	require.Equal(t, "false", mappingStringValue(entryNode, skillEnabledKey))
	require.Contains(
		t,
		mappingStringValue(skillsNode, toolingGuidanceKey),
		"Keep exploring nearby runtime facts, retries, "+
			"and recovery paths",
	)
	require.Contains(
		t,
		mappingStringValue(skillsNode, toolingGuidanceKey),
		"obvious next recovery step such as a canonical "+
			"identifier",
	)
	require.Contains(
		t,
		mappingStringValue(skillsNode, toolingGuidanceKey),
		"treat it as the working value and continue in "+
			"this turn without asking the user to confirm "+
			"it first",
	)
	require.NotContains(
		t,
		mappingStringValue(skillsNode, toolingGuidanceKey),
		"continue with the best fallback",
	)

	agentNode := mappingValue(documentNode(&root), agentKey)
	require.NotNil(t, agentNode)
	require.Contains(
		t,
		mappingStringValue(agentNode, systemPromptKey),
		runtimeCodingPromptHeader,
	)
	require.Contains(
		t,
		mappingStringValue(agentNode, systemPromptKey),
		runtimeProgressProtocolHeader,
	)
	require.Contains(
		t,
		mappingStringValue(agentNode, systemPromptKey),
		runtimeWorkflowProtocolHeader,
	)
	require.Contains(
		t,
		mappingStringValue(agentNode, systemPromptKey),
		runtimeTruthProtocolHeader,
	)
	require.Contains(
		t,
		mappingStringValue(agentNode, systemPromptKey),
		"The built-in fs_* tools are scoped to their configured",
	)
	require.Contains(
		t,
		mappingStringValue(agentNode, systemPromptKey),
		"keep using repo-aware runtime execution tools directly",
	)
	require.NotContains(
		t,
		mappingStringValue(agentNode, systemPromptKey),
		"coding-agent skill",
	)
	require.Contains(
		t,
		mappingStringValue(agentNode, systemPromptKey),
		"Users usually cannot see tool calls or internal "+
			"reasoning",
	)
	require.Contains(
		t,
		mappingStringValue(agentNode, systemPromptKey),
		"Before the first non-trivial tool call",
	)
	require.Contains(
		t,
		mappingStringValue(agentNode, systemPromptKey),
		"part of acting immediately",
	)
	require.Contains(
		t,
		mappingStringValue(agentNode, systemPromptKey),
		"Do not turn that preamble into a confirmation "+
			"request",
	)
	require.Contains(
		t,
		mappingStringValue(agentNode, systemPromptKey),
		"A preamble-only message is not a completed turn",
	)
	require.Contains(
		t,
		mappingStringValue(agentNode, systemPromptKey),
		"Group related tool calls under one preamble",
	)
	require.Contains(
		t,
		mappingStringValue(agentNode, systemPromptKey),
		"send short progress updates at natural milestones",
	)
	require.Contains(
		t,
		mappingStringValue(agentNode, systemPromptKey),
		"interactive session emits a meaningful new stage",
	)
	require.Contains(
		t,
		mappingStringValue(agentNode, systemPromptKey),
		"Do not narrate every empty poll",
	)
	require.Contains(
		t,
		mappingStringValue(agentNode, systemPromptKey),
		"send one brief waiting update",
	)
	require.Contains(
		t,
		mappingStringValue(agentNode, systemPromptKey),
		"lead the wording, cadence, and attitude",
	)
	require.Contains(
		t,
		mappingStringValue(agentNode, systemPromptKey),
		"perform a fresh inspection before planning",
	)
	require.Contains(
		t,
		mappingStringValue(agentNode, systemPromptKey),
		"prefer `rg --files` for file inventory",
	)
	require.Contains(
		t,
		mappingStringValue(agentNode, systemPromptKey),
		"grep -R ..",
	)
}

func TestPrepareOpenClawConfigKeepsExplicitCodingAgentEntry(
	t *testing.T,
) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"skills:\n"+
				"  entries:\n"+
				"    coding-agent:\n"+
				"      enabled: true\n",
		),
		0o600,
	)
	require.NoError(t, err)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	skillsNode := mappingValue(documentNode(&root), skillsKey)
	require.NotNil(t, skillsNode)
	entriesNode := mappingValue(skillsNode, skillsEntriesKey)
	require.NotNil(t, entriesNode)
	entryNode := mappingValue(entriesNode, codingAgentSkillName)
	require.NotNil(t, entryNode)
	require.Equal(t, "true", mappingStringValue(entryNode, skillEnabledKey))
}

func TestPrepareOpenClawConfigInjectsCodingAgentWorkdirContext(
	t *testing.T,
) {
	repoDir := t.TempDir()
	require.NoError(
		t,
		os.Mkdir(filepath.Join(repoDir, gitDirName), 0o755),
	)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(repoDir, agentsDocFileName),
			[]byte("repo instructions"),
			0o600,
		),
	)
	home := t.TempDir()
	t.Setenv("HOME", home)
	globalAgentsPath := filepath.Join(home, ".trpc-agent-go", "AGENTS.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(globalAgentsPath), 0o755))
	require.NoError(
		t,
		os.WriteFile(globalAgentsPath, []byte("global instructions"), 0o600),
	)
	scratchRoot := filepath.Join(t.TempDir(), "scratch")
	docHelperPath := filepath.Join(
		t.TempDir(),
		runtimeDocHelperName,
	)
	t.Setenv(runtimeDocHelperEnvName, docHelperPath)

	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"agent:\n"+
				"  system_prompt: base system\n"+
				"skills:\n"+
				"  coding_agent:\n"+
				"    execution_mode: auto\n"+
				"    default_workdir: "+repoDir+"\n"+
				"    scratch_root: "+scratchRoot+"\n",
		),
		0o600,
	)
	require.NoError(t, err)

	outputRoot := filepath.Join(scratchRoot, scratchOutputDirName)
	tempRoot := workspacecfg.DefaultTempRoot(defaultStateDir())
	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{
			OpenClawConfigPath: cfgPath,
			StateDir:           defaultStateDir(),
		},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	agentNode := mappingValue(documentNode(&root), agentKey)
	require.NotNil(t, agentNode)
	prompt := mappingStringValue(agentNode, systemPromptKey)
	require.Contains(t, prompt, runtimeCodingPromptHeader)
	require.Contains(t, prompt, runtimeProgressProtocolHeader)
	require.Contains(t, prompt, "Default coding workdir: "+repoDir)
	require.Contains(
		t,
		prompt,
		"Treat current repo, current workspace, or this repo",
	)
	require.Contains(
		t,
		prompt,
		"probe local capabilities first",
	)
	require.Contains(
		t,
		prompt,
		"missing user-space dependency",
	)
	require.Contains(
		t,
		prompt,
		"`trpc-claw inspect deps` and `trpc-claw bootstrap deps --apply`",
	)
	require.Contains(
		t,
		prompt,
		"self-contained user-space toolchains",
	)
	require.Contains(
		t,
		prompt,
		"managed CJK fonts and OCR language data",
	)
	require.Contains(
		t,
		prompt,
		"explicit CJK-capable font",
	)
	require.Contains(
		t,
		prompt,
		"switch to a more self-contained toolchain",
	)
	require.Contains(
		t,
		prompt,
		runtimeDocHelperName+"` is available at ",
	)
	require.Contains(
		t,
		prompt,
		runtimeDocHelperName+" probe",
	)
	require.Contains(
		t,
		prompt,
		runtimeDocHelperName+" ensure-fonts",
	)
	require.Contains(
		t,
		prompt,
		runtimeDocHelperName+" ensure-tessdata",
	)
	require.Contains(
		t,
		prompt,
		"inspect available encoders and formats",
	)
	require.Contains(
		t,
		prompt,
		"Users usually cannot see tool calls or internal "+
			"reasoning",
	)
	require.Contains(
		t,
		prompt,
		"Before the first non-trivial tool call",
	)
	require.Contains(
		t,
		prompt,
		"Group related tool calls under one preamble",
	)
	require.Contains(
		t,
		prompt,
		"send short progress updates at natural milestones",
	)
	require.Contains(
		t,
		prompt,
		"interactive session emits a meaningful new stage",
	)
	require.Contains(
		t,
		prompt,
		"Do not narrate every empty poll",
	)
	require.Contains(
		t,
		prompt,
		"send one brief waiting update",
	)
	require.Contains(
		t,
		prompt,
		"lead the wording, cadence, and attitude",
	)
	require.Contains(
		t,
		prompt,
		"perform a fresh inspection before planning",
	)
	require.Contains(
		t,
		prompt,
		"prefer `rg --files` for file inventory",
	)
	require.Contains(
		t,
		prompt,
		"read the smallest relevant slices first",
	)
	require.Contains(
		t,
		prompt,
		"prefer a file-writing tool or redirected stdin",
	)
	require.Contains(
		t,
		prompt,
		"Effective AGENTS.md for the default coding workspace: "+
			filepath.Join(repoDir, agentsDocFileName),
	)
	require.NotContains(t, prompt, globalAgentsPath)
	require.Contains(
		t,
		prompt,
		"Scratch repo root for standalone toy projects "+
			"or no-repo tasks: "+scratchRoot,
	)
	require.Contains(
		t,
		prompt,
		"Runtime artifact output root: "+outputRoot,
	)
	require.Contains(
		t,
		prompt,
		"default home for direct user-facing generated files",
	)
	require.Contains(t, prompt, "Runtime temp root: "+tempRoot)
	require.Contains(t, prompt, "working copies of uploads")

	skillsNode := mappingValue(documentNode(&root), skillsKey)
	require.NotNil(t, skillsNode)
	guidance := mappingStringValue(skillsNode, toolingGuidanceKey)
	require.Contains(
		t,
		guidance,
		"Keep exploring nearby runtime facts, retries, "+
			"and recovery paths",
	)
	info, err := os.Stat(scratchRoot)
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

func TestPrepareOpenClawConfigUsesCurrentWorkingDirectory(
	t *testing.T,
) {
	workdir := t.TempDir()
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(workdir, agentsDocFileName),
			[]byte("workspace instructions"),
			0o600,
		),
	)
	cfgPath := filepath.Join(workdir, "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"skills:\n"+
				"  coding_agent:\n"+
				"    execution_mode: sandbox\n",
		),
		0o600,
	)
	require.NoError(t, err)

	stateDir := filepath.Join(t.TempDir(), "state")
	wd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(workdir))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(wd))
	})

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{
			OpenClawConfigPath: cfgPath,
			StateDir:           stateDir,
		},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	agentNode := mappingValue(documentNode(&root), agentKey)
	require.NotNil(t, agentNode)
	prompt := mappingStringValue(agentNode, systemPromptKey)
	require.Contains(t, prompt, "Default coding workdir: "+workdir)
	require.Contains(
		t,
		prompt,
		"Effective AGENTS.md for the default coding workspace: "+
			filepath.Join(workdir, agentsDocFileName),
	)
	require.Contains(
		t,
		prompt,
		"Runtime temp root: "+
			workspacecfg.DefaultTempRoot(stateDir),
	)
}

func TestPrepareOpenClawConfigFallsBackToUserAgentsDoc(
	t *testing.T,
) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	globalAgentsPath := filepath.Join(home, ".trpc-agent-go", "AGENTS.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(globalAgentsPath), 0o755))
	require.NoError(
		t,
		os.WriteFile(globalAgentsPath, []byte("global instructions"), 0o600),
	)

	workdir := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"skills:\n"+
				"  coding_agent:\n"+
				"    execution_mode: auto\n"+
				"    default_workdir: "+workdir+"\n",
		),
		0o600,
	)
	require.NoError(t, err)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	agentNode := mappingValue(documentNode(&root), agentKey)
	require.NotNil(t, agentNode)
	prompt := mappingStringValue(agentNode, systemPromptKey)
	require.Contains(
		t,
		prompt,
		"Effective AGENTS.md for the default coding workspace: "+
			globalAgentsPath,
	)
}

func TestPrepareOpenClawConfigInjectsWeComRuntimeDefaults(
	t *testing.T,
) {
	workdir := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"model:\n"+
				"  name: gpt-5.2\n"+
				"channels:\n"+
				"  - type: wecom\n"+
				"    name: corp\n"+
				"    config:\n"+
				"      bot_mode: notification\n"+
				"      token: t\n"+
				"      encoding_aes_key: k\n"+
				"      webhook_url: https://example.com\n",
		),
		0o600,
	)
	require.NoError(t, err)

	stateDir := filepath.Join(t.TempDir(), "state")
	wd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(workdir))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(wd))
	})

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{
			OpenClawConfigPath: cfgPath,
			StateDir:           stateDir,
		},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	channelsNode := mappingValue(documentNode(&root), channelsKey)
	require.NotNil(t, channelsNode)

	wecomNode := findChannelNodeByType(channelsNode, channelTypeWeCom)
	require.NotNil(t, wecomNode)

	configNode := mappingValue(wecomNode, channelConfigKey)
	require.NotNil(t, configNode)
	require.Equal(
		t,
		workdir,
		mappingStringValue(
			configNode,
			wecomchannel.RuntimeDefaultWorkdirConfigKey,
		),
	)
	require.Equal(
		t,
		filepath.Join(stateDir, "workspaces", "scratch"),
		mappingStringValue(
			configNode,
			wecomchannel.RuntimeScratchRootConfigKey,
		),
	)
	require.Equal(
		t,
		"gpt-5.2",
		mappingStringValue(
			configNode,
			wecomchannel.RuntimeModelNameConfigKey,
		),
	)
}

func TestPrepareOpenClawConfigInjectsWeComReplyDeliveryRoots(
	t *testing.T,
) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"tools:\n"+
				"  toolsets:\n"+
				"    - type: file\n"+
				"      name: fs\n"+
				"      config:\n"+
				"        base_dir: "+runtimeStateDirEnvRef+"\n"+
				"        read_only: false\n"+
				"channels:\n"+
				"  - type: wecom\n"+
				"    name: corp\n"+
				"    config:\n"+
				"      bot_mode: notification\n"+
				"      token: t\n"+
				"      encoding_aes_key: k\n"+
				"      webhook_url: https://example.com\n",
		),
		0o600,
	)
	require.NoError(t, err)

	stateDir := filepath.Join(t.TempDir(), "state")
	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{
			OpenClawConfigPath: cfgPath,
			StateDir:           stateDir,
		},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	channelsNode := mappingValue(documentNode(&root), channelsKey)
	require.NotNil(t, channelsNode)

	wecomNode := findChannelNodeByType(channelsNode, channelTypeWeCom)
	require.NotNil(t, wecomNode)

	configNode := mappingValue(wecomNode, channelConfigKey)
	require.NotNil(t, configNode)

	rootsNode := mappingValue(
		configNode,
		wecomchannel.RuntimeReplyDeliveryRootsConfigKey,
	)
	require.NotNil(t, rootsNode)
	require.Len(t, rootsNode.Content, 1)
	require.True(t, sequenceContainsPath(rootsNode, stateDir))
}

func TestPrepareOpenClawConfigSkipsImplicitStateDirReplyRoots(
	t *testing.T,
) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"tools:\n"+
				"  toolsets:\n"+
				"    - type: file\n"+
				"      name: fs\n"+
				"      config:\n"+
				"        read_only: false\n"+
				"channels:\n"+
				"  - type: wecom\n"+
				"    name: corp\n"+
				"    config:\n"+
				"      bot_mode: notification\n"+
				"      token: t\n"+
				"      encoding_aes_key: k\n"+
				"      webhook_url: https://example.com\n",
		),
		0o600,
	)
	require.NoError(t, err)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{
			OpenClawConfigPath: cfgPath,
			StateDir: filepath.Join(
				t.TempDir(),
				"state",
			),
		},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	channelsNode := mappingValue(documentNode(&root), channelsKey)
	require.NotNil(t, channelsNode)

	wecomNode := findChannelNodeByType(channelsNode, channelTypeWeCom)
	require.NotNil(t, wecomNode)

	configNode := mappingValue(wecomNode, channelConfigKey)
	require.NotNil(t, configNode)
	require.Nil(
		t,
		mappingValue(
			configNode,
			wecomchannel.RuntimeReplyDeliveryRootsConfigKey,
		),
	)
}

func TestPrepareOpenClawConfigInjectsEnvProbeToolProvider(
	t *testing.T,
) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"tools:\n"+
				"  providers:\n"+
				"    - type: duckduckgo\n"+
				"      config:\n"+
				"        timeout: 30s\n",
		),
		0o600,
	)
	require.NoError(t, err)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	toolsNode := mappingValue(documentNode(&root), toolsKey)
	require.NotNil(t, toolsNode)
	providersNode := mappingValue(toolsNode, toolProvidersKey)
	require.NotNil(t, providersNode)
	require.Len(t, providersNode.Content, 2)
	require.True(
		t,
		hasToolProviderType(
			providersNode,
			envprobeplugin.PluginType,
		),
	)
}

func TestPrepareOpenClawConfigSkipsDuplicateEnvProbeProvider(
	t *testing.T,
) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"tools:\n"+
				"  providers:\n"+
				"    - type: "+envprobeplugin.PluginType+"\n",
		),
		0o600,
	)
	require.NoError(t, err)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	toolsNode := mappingValue(documentNode(&root), toolsKey)
	require.NotNil(t, toolsNode)
	providersNode := mappingValue(toolsNode, toolProvidersKey)
	require.NotNil(t, providersNode)
	require.Len(t, providersNode.Content, 1)
	require.True(
		t,
		hasToolProviderType(
			providersNode,
			envprobeplugin.PluginType,
		),
	)
}

func TestPrepareOpenClawConfigAddsAssistantNameToolProvider(
	t *testing.T,
) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"tools:\n"+
				"  providers:\n"+
				"    - type: "+envprobeplugin.PluginType+"\n"+
				"channels:\n"+
				"  - type: "+wecomchannel.PluginType+"\n"+
				"    config:\n"+
				"      bot_mode: \"ai\"\n"+
				"      connection_mode: \"websocket\"\n"+
				"      aibotid: \"bot\"\n"+
				"      secret: \"secret\"\n",
		),
		0o600,
	)
	require.NoError(t, err)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	toolsNode := mappingValue(documentNode(&root), toolsKey)
	require.NotNil(t, toolsNode)
	providersNode := mappingValue(toolsNode, toolProvidersKey)
	require.NotNil(t, providersNode)
	require.True(
		t,
		hasToolProviderType(
			providersNode,
			wecomchannel.AssistantNameToolProviderType,
		),
	)
}

func TestBuildCodingAgentSystemPromptIncludesEnvProbeRule(
	t *testing.T,
) {
	prompt := buildCodingAgentSystemPrompt(codingAgentDefaults{})
	require.Contains(t, prompt, "call env_probe")
	require.Contains(t, prompt, "Never reveal secret values")
}

func TestBuildCodingAgentPromptIncludesLanguageRules(
	t *testing.T,
) {
	prompt := buildCodingAgentSystemPrompt(codingAgentDefaults{})
	require.Contains(t, prompt, runtimeLanguageProtocolHeader)
	require.Contains(t, prompt, runtimeLanguageFollowUserRule)
	require.Contains(t, prompt, runtimeLanguagePreserveTermsRule)
	require.Contains(t, prompt, runtimeLanguageNoSentenceMixRule)

	guidance := buildSkillsToolingGuidance(codingAgentDefaults{})
	require.Contains(t, guidance, runtimeLanguageFollowUserRule)
	require.Contains(t, guidance, runtimeLanguagePreserveTermsRule)
	require.Contains(t, guidance, runtimeLanguageNoSentenceMixRule)
}

func TestDefaultCodingAgentPromptAssetRendersWithRuntimeVars(
	t *testing.T,
) {
	raw, err := promptasset.ReadEmbeddedFiles(
		promptasset.DefaultSystemEmbeddedDir,
	)
	require.NoError(t, err)
	codingRaw, ok := raw[promptasset.DefaultCodingAgentFileName]
	require.True(t, ok)

	rendered, err := promptasset.Render(
		codingRaw,
		buildSystemPromptTemplateVars(
			runtimeModelIdentity{},
			codingAgentDefaults{},
			runtimeProductName,
		),
	)
	require.NoError(t, err)
	require.Contains(
		t,
		rendered,
		"prefer creating or updating a local skill over "+
			"treating it as a one-off answer",
	)
	require.Contains(
		t,
		rendered,
		"Prefer that config file over asking the user "+
			"to re-enter the same value as an environment "+
			"variable",
	)
	require.Contains(
		t,
		rendered,
		"Avoid sentence-level language mixing",
	)
	require.NotContains(t, rendered, "TRPC_CLAW_RUNTIME_SKILL_")
	require.NotContains(t, rendered, "TRPC_CLAW_RUNTIME_PRIVATE_CONFIG_RULE")
}

func TestBuildCodingAgentPromptPrefersAutonomousExecution(
	t *testing.T,
) {
	prompt := buildCodingAgentSystemPrompt(codingAgentDefaults{})
	require.Contains(
		t,
		prompt,
		"Default to taking the next concrete step yourself",
	)
	require.Contains(
		t,
		prompt,
		"without asking follow-up questions",
	)
	require.Contains(
		t,
		prompt,
		"do not ask the user for more input",
	)
	require.Contains(
		t,
		prompt,
		"spoken and strongly implied requirements",
	)
	require.Contains(
		t,
		prompt,
		"Keep this intent expansion internal",
	)
	require.Contains(
		t,
		prompt,
		"try the next reasonable recovery step yourself",
	)
	require.Contains(
		t,
		prompt,
		"user's actual request is resolved, not merely "+
			"diagnosed",
	)
	require.Contains(
		t,
		prompt,
		"resolve the canonical identifier yourself",
	)
	require.Contains(
		t,
		prompt,
		"continue without asking the user to confirm "+
			"it first",
	)
	require.Contains(
		t,
		prompt,
		"For external search, latest/current facts",
	)
	require.Contains(
		t,
		prompt,
		"Do not redirect the user to another app/site",
	)
	require.Contains(
		t,
		prompt,
		"Default to zero follow-up questions",
	)
	require.Contains(
		t,
		prompt,
		"Do not hand routine next-step choices back to the user",
	)
	require.Contains(
		t,
		prompt,
		"state the exact missing piece tersely as a "+
			"factual status line",
	)
	require.NotContains(
		t,
		prompt,
		"Treat asking the user as a last resort",
	)
	require.Contains(
		t,
		prompt,
		"Do not end a successful or recoverable turn",
	)
	require.Contains(
		t,
		prompt,
		"Keep the coding workspace for repo files",
	)
	require.Contains(
		t,
		prompt,
		"inspect both the coding workspace and the runtime-managed artifact roots",
	)
	require.Contains(
		t,
		prompt,
		"Never tell the user a generated file is ready",
	)
	require.Contains(t, prompt, "`test -f`")
	require.Contains(
		t,
		prompt,
		"keep using repo-aware runtime execution tools directly",
	)
	require.Contains(
		t,
		prompt,
		"prefer exec_command with an explicit workdir",
	)
	require.Contains(
		t,
		prompt,
		"prefer creating or updating a local skill over "+
			"treating it as a one-off answer",
	)
	require.Contains(
		t,
		prompt,
		"For lightweight facts, preferences, or simple "+
			"standing rules, use memory instead",
	)
	require.Contains(
		t,
		prompt,
		"Use platform code and tools for stable safety "+
			"boundaries",
	)
	require.Contains(
		t,
		prompt,
		"treat it as authorization to save a local private "+
			"runtime config file",
	)
	require.Contains(
		t,
		prompt,
		"excluded from source control and packaging",
	)
	require.Contains(
		t,
		prompt,
		"do not edit shell startup or trusted env files",
	)
	require.Contains(
		t,
		prompt,
		"Inspect existing local config before treating a "+
			"missing environment variable as blocking",
	)
	require.NotContains(t, prompt, "coding-agent skill")

	guidance := buildSkillsToolingGuidance(codingAgentDefaults{})
	require.Contains(
		t,
		guidance,
		"Treat the skill overview below as the skills available in this session.",
	)
	require.Contains(
		t,
		guidance,
		"Do not answer a matching skill task from the short "+
			"summary, prior knowledge, or partial memory.",
	)
	require.Contains(
		t,
		guidance,
		"you must use that skill in the same turn",
	)
	require.Contains(
		t,
		guidance,
		"Each entry includes a path to that skill's `SKILL.md` on disk.",
	)
	require.Contains(
		t,
		guidance,
		"Never say that you could read or load a matching skill later",
	)
	require.Contains(
		t,
		guidance,
		"Read only the supporting docs, scripts, assets, examples, or templates",
	)
	require.Contains(
		t,
		guidance,
		"prefer creating or updating a local skill over "+
			"treating it as a one-off answer",
	)
	require.Contains(
		t,
		guidance,
		"For lightweight facts, preferences, or simple "+
			"standing rules, use memory instead",
	)
	require.Contains(
		t,
		guidance,
		"Use platform code and tools for stable safety "+
			"boundaries",
	)
	require.Contains(
		t,
		guidance,
		"Prefer that config file over asking the user "+
			"to re-enter the same value as an environment "+
			"variable",
	)
	require.Contains(
		t,
		guidance,
		"excluded from source control and packaging",
	)
	require.Contains(
		t,
		guidance,
		"do not edit shell startup or trusted env files",
	)
	require.Contains(
		t,
		guidance,
		"choose a writable user-managed skill root",
	)
	require.Contains(
		t,
		guidance,
		"keep shared or published credentials out of "+
			"`SKILL.md`",
	)
	require.Contains(t, guidance, "not bundled skills unless")
	require.Contains(
		t,
		guidance,
		"Keep exploring nearby runtime facts, retries, "+
			"and recovery paths",
	)
	require.Contains(
		t,
		guidance,
		"obvious next recovery step such as a canonical "+
			"identifier",
	)
	require.Contains(
		t,
		guidance,
		"treat it as the working value and continue in "+
			"this turn without asking the user to confirm "+
			"it first",
	)
	require.NotContains(
		t,
		guidance,
		"continue with the best fallback",
	)
	require.NotContains(t, guidance, "coding-agent skill")
	require.NotContains(t, guidance, "trpc-claw-browser-runtime mcp-stdio")
}

func TestBuildCodingAgentSystemPromptIncludesBrowserRuntimeRule(
	t *testing.T,
) {
	t.Setenv(
		runtimeBrowserRuntimeEnvName,
		"/tmp/trpc-claw-browser-runtime",
	)
	t.Setenv(
		runtimeBrowserModeEnvName,
		runtimeBrowserModeInteractive,
	)

	prompt := buildCodingAgentSystemPrompt(codingAgentDefaults{})
	require.Contains(t, prompt, "Managed browser runtime:")
	require.Contains(t, prompt, "Prefer `web_fetch`")
	require.Contains(t, prompt, "trpc-claw-browser-runtime doctor")
	require.Contains(t, prompt, "interactive browser automation")
	require.Contains(t, prompt, runtimeBrowserModeEnvName)
	require.Contains(t, prompt, runtimeBrowserPathEnvName)
	require.Contains(
		t,
		prompt,
		"Treat prior browser or MCP failures in chat history",
	)
}

func TestBuildSkillsToolingGuidanceIncludesSkillLoadingRules(
	t *testing.T,
) {
	guidance := buildSkillsToolingGuidanceWithRoots(
		codingAgentDefaults{},
		[]string{
			"/tmp/openclaw/skills/bundled",
			"/tmp/openclaw/skills/local",
		},
	)

	require.Contains(
		t,
		guidance,
		"Treat the skill overview below as the skills "+
			"available in this session.",
	)
	require.Contains(
		t,
		guidance,
		"Do not answer a matching skill task from the short "+
			"summary, prior knowledge, or partial memory.",
	)
	require.Contains(
		t,
		guidance,
		"This is a blocking requirement for matching skills.",
	)
	require.Contains(
		t,
		guidance,
		"you must use that skill in the same turn",
	)
	require.Contains(
		t,
		guidance,
		"Start with one brief user-visible preamble "+
			"about the immediate next step",
	)
	require.Contains(
		t,
		guidance,
		"not a pause to ask what to do next",
	)
	require.Contains(
		t,
		guidance,
		"A preamble-only skill response is invalid",
	)
	require.Contains(
		t,
		guidance,
		"Do not stop after announcing the skill-backed "+
			"next step",
	)
	require.Contains(
		t,
		guidance,
		"Do not turn that preamble into a request for "+
			"confirmation",
	)
	require.Contains(
		t,
		guidance,
		"Each entry includes a path to that skill's "+
			"`SKILL.md` on disk.",
	)
	require.Contains(
		t,
		guidance,
		"Never say that you could read or load a matching "+
			"skill later",
	)
	require.Contains(
		t,
		guidance,
		"Never mention reading, loading, or using a matching "+
			"skill unless you already called `skill_load` for "+
			"it in this turn.",
	)
	require.Contains(
		t,
		guidance,
		"Do not respond with capability disclaimers such "+
			"as `I can read the skill`",
	)
	require.Contains(
		t,
		guidance,
		"prefer creating or updating a local skill over "+
			"treating it as a one-off answer",
	)
	require.Contains(
		t,
		guidance,
		"For lightweight facts, preferences, or simple "+
			"standing rules, use memory instead",
	)
	require.Contains(
		t,
		guidance,
		"Use platform code and tools for stable safety "+
			"boundaries",
	)
	require.Contains(
		t,
		guidance,
		"treat it as authorization to save a local private "+
			"runtime config file",
	)
	require.Contains(
		t,
		guidance,
		"excluded from source control and packaging",
	)
	require.Contains(
		t,
		guidance,
		"do not edit shell startup or trusted env files",
	)
	require.Contains(
		t,
		guidance,
		"choose a writable user-managed skill root",
	)
	require.Contains(
		t,
		guidance,
		"keep shared or published credentials out of "+
			"`SKILL.md`",
	)
	require.Contains(t, guidance, "not bundled skills unless")
	require.Contains(
		t,
		guidance,
		"Read only the supporting docs, scripts, assets, "+
			"examples, or templates",
	)
	require.Contains(
		t,
		guidance,
		"Keep exploring nearby runtime facts, retries, "+
			"and recovery paths",
	)
	require.Contains(
		t,
		guidance,
		"obvious next recovery step such as a canonical "+
			"identifier",
	)
	require.Contains(
		t,
		guidance,
		"treat it as the working value and continue in "+
			"this turn without asking the user to confirm "+
			"it first",
	)
	require.NotContains(
		t,
		guidance,
		"continue with the best fallback",
	)
}

func TestPrepareOpenClawConfigSkipsReadOnlyReplyDeliveryRoots(
	t *testing.T,
) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"tools:\n"+
				"  toolsets:\n"+
				"    - type: file\n"+
				"      name: fs\n"+
				"      config:\n"+
				"        base_dir: "+runtimeStateDirEnvRef+"\n"+
				"        read_only: true\n"+
				"channels:\n"+
				"  - type: wecom\n"+
				"    name: corp\n"+
				"    config:\n"+
				"      bot_mode: notification\n"+
				"      token: t\n"+
				"      encoding_aes_key: k\n"+
				"      webhook_url: https://example.com\n",
		),
		0o600,
	)
	require.NoError(t, err)

	stateDir := filepath.Join(t.TempDir(), "state")
	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{
			OpenClawConfigPath: cfgPath,
			StateDir:           stateDir,
		},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	channelsNode := mappingValue(documentNode(&root), channelsKey)
	require.NotNil(t, channelsNode)

	wecomNode := findChannelNodeByType(channelsNode, channelTypeWeCom)
	require.NotNil(t, wecomNode)

	configNode := mappingValue(wecomNode, channelConfigKey)
	require.NotNil(t, configNode)

	require.Nil(
		t,
		mappingValue(
			configNode,
			wecomchannel.RuntimeReplyDeliveryRootsConfigKey,
		),
	)
}

func TestPrepareOpenClawConfigDoesNotSyncFileToolBaseDir(
	t *testing.T,
) {
	repoDir := t.TempDir()
	require.NoError(
		t,
		os.Mkdir(filepath.Join(repoDir, gitDirName), 0o755),
	)

	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"tools:\n"+
				"  toolsets:\n"+
				"    - type: file\n"+
				"      name: fs\n"+
				"      config:\n"+
				"        base_dir: "+runtimeStateDirEnvRef+"\n"+
				"skills:\n"+
				"  coding_agent:\n"+
				"    execution_mode: host\n"+
				"    default_workdir: "+repoDir+"\n",
		),
		0o600,
	)
	require.NoError(t, err)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	toolsetsNode := mappingValue(
		mappingValue(documentNode(&root), toolsKey),
		toolsetsKey,
	)
	require.NotNil(t, toolsetsNode)
	require.NotEmpty(t, toolsetsNode.Content)

	configNode := mappingValue(
		toolsetsNode.Content[0],
		toolConfigKey,
	)
	require.NotNil(t, configNode)
	require.Equal(
		t,
		runtimeStateDirEnvRef,
		mappingStringValue(configNode, "base_dir"),
	)
}

func TestPrepareOpenClawConfigSkipsEnvGatedChannelBeforeEnvExpand(
	t *testing.T,
) {
	const (
		testGateBotID  = "TRPC_CLAW_TEST_GATE_BOT_ID"
		testGateSecret = "TRPC_CLAW_TEST_GATE_SECRET"
	)

	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"channels:\n"+
				"  - type: wecom\n"+
				"    enabled: true\n"+
				"    enabled_if_env_all:\n"+
				"      - "+testGateBotID+"\n"+
				"      - "+testGateSecret+"\n"+
				"    config:\n"+
				"      bot_mode: ai\n"+
				"      connection_mode: websocket\n"+
				"      aibotid: ${"+testGateBotID+"}\n"+
				"      secret: ${"+testGateSecret+"}\n"+
				"  - type: weixin\n"+
				"    enabled: true\n"+
				"    config:\n"+
				"      base_url: https://ilinkai.weixin.qq.com\n",
		),
		0o600,
	)
	require.NoError(t, err)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	channelsNode := mappingValue(documentNode(&root), channelsKey)
	require.NotNil(t, channelsNode)
	require.Len(t, channelsNode.Content, 1)
	require.Equal(
		t,
		channelTypeWeixin,
		mappingStringValue(channelsNode.Content[0], channelTypeKey),
	)
}

func TestPrepareOpenClawConfigInjectsImplicitWeixinForLegacyWeCom(
	t *testing.T,
) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"channels:\n"+
				"  - type: wecom\n"+
				"    name: corp\n"+
				"    config:\n"+
				"      bot_mode: ai\n"+
				"      connection_mode: websocket\n",
		),
		0o600,
	)
	require.NoError(t, err)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)

	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	channelsNode := mappingValue(documentNode(&root), channelsKey)
	require.NotNil(t, channelsNode)
	require.Len(t, channelsNode.Content, 2)
	require.Equal(
		t,
		channelTypeWeCom,
		mappingStringValue(channelsNode.Content[0], channelTypeKey),
	)
	require.Equal(
		t,
		channelTypeWeixin,
		mappingStringValue(channelsNode.Content[1], channelTypeKey),
	)
	require.Equal(
		t,
		implicitWeixinChannelName,
		mappingStringValue(channelsNode.Content[1], toolNameKey),
	)
}

func TestPrepareOpenClawConfigKeepsExplicitDisabledWeixinDisabled(
	t *testing.T,
) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"channels:\n"+
				"  - type: wecom\n"+
				"    name: corp\n"+
				"    config:\n"+
				"      bot_mode: ai\n"+
				"      connection_mode: websocket\n"+
				"  - type: weixin\n"+
				"    enabled: false\n"+
				"    name: direct\n",
		),
		0o600,
	)
	require.NoError(t, err)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)

	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	channelsNode := mappingValue(documentNode(&root), channelsKey)
	require.NotNil(t, channelsNode)
	require.Len(t, channelsNode.Content, 1)
	require.Equal(
		t,
		channelTypeWeCom,
		mappingStringValue(channelsNode.Content[0], channelTypeKey),
	)
}

func findChannelNodeByType(
	channelsNode *yaml.Node,
	typeName string,
) *yaml.Node {
	if channelsNode == nil || channelsNode.Kind != yaml.SequenceNode {
		return nil
	}
	for _, channelNode := range channelsNode.Content {
		if configuredChannelTypeName(channelNode) != typeName {
			continue
		}
		return channelNode
	}
	return nil
}

func TestPrepareOpenClawConfigKeepsExplicitFileToolBaseDir(
	t *testing.T,
) {
	repoDir := t.TempDir()
	require.NoError(
		t,
		os.Mkdir(filepath.Join(repoDir, gitDirName), 0o755),
	)
	explicitBaseDir := filepath.Join(t.TempDir(), "files")
	require.NoError(t, os.MkdirAll(explicitBaseDir, 0o755))

	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"tools:\n"+
				"  toolsets:\n"+
				"    - type: file\n"+
				"      name: fs\n"+
				"      config:\n"+
				"        base_dir: "+explicitBaseDir+"\n"+
				"skills:\n"+
				"  coding_agent:\n"+
				"    execution_mode: host\n"+
				"    default_workdir: "+repoDir+"\n",
		),
		0o600,
	)
	require.NoError(t, err)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	toolsetsNode := mappingValue(
		mappingValue(documentNode(&root), toolsKey),
		toolsetsKey,
	)
	require.NotNil(t, toolsetsNode)
	require.NotEmpty(t, toolsetsNode.Content)

	configNode := mappingValue(
		toolsetsNode.Content[0],
		toolConfigKey,
	)
	require.NotNil(t, configNode)
	require.Equal(
		t,
		explicitBaseDir,
		mappingStringValue(configNode, "base_dir"),
	)
}

func TestDefaultOpenClawProfilesDisableRefreshToolsetsOnRun(t *testing.T) {
	profiles := []string{
		filepath.Join("..", "..", "openclaw.yaml"),
		filepath.Join("..", "..", "openclaw.wecom.ai.yaml"),
		filepath.Join("..", "..", "openclaw.wecom.ai.websocket.yaml"),
		filepath.Join("..", "..", "openclaw.wecom.notification.yaml"),
	}
	for _, profilePath := range profiles {
		profilePath := profilePath
		t.Run(profilePath, func(t *testing.T) {
			data, err := os.ReadFile(profilePath)
			require.NoError(t, err)
			require.Contains(t, string(data), "refresh_toolsets_on_run: false")
		})
	}
}

func TestDefaultOpenClawProfilesUseSessionSkillDefaults(t *testing.T) {
	profiles := []string{
		filepath.Join("..", "..", "openclaw.yaml"),
		filepath.Join("..", "..", "openclaw.wecom.ai.yaml"),
		filepath.Join("..", "..", "openclaw.wecom.ai.websocket.yaml"),
		filepath.Join("..", "..", "openclaw.wecom.notification.yaml"),
	}
	for _, profilePath := range profiles {
		profilePath := profilePath
		t.Run(profilePath, func(t *testing.T) {
			data, err := os.ReadFile(profilePath)
			require.NoError(t, err)
			require.Contains(
				t,
				string(data),
				`load_mode: "session"`,
			)
			require.Contains(
				t,
				string(data),
				"max_loaded_skills: 10",
			)
			require.Contains(
				t,
				string(data),
				"skip_fallback_on_session_summary: false",
			)
		})
	}
}

func TestDefaultWeComProfilesUseEnvBackedGroupSessionMode(
	t *testing.T,
) {
	const expected = `group_session_mode: "${WECOM_GROUP_SESSION_MODE}"`

	profiles := []string{
		filepath.Join("..", "..", "openclaw.wecom.ai.yaml"),
		filepath.Join(
			"..",
			"..",
			"openclaw.wecom.ai.websocket.yaml",
		),
	}
	for _, profilePath := range profiles {
		profilePath := profilePath
		t.Run(profilePath, func(t *testing.T) {
			data, err := os.ReadFile(profilePath)
			require.NoError(t, err)
			require.Contains(t, string(data), expected)
		})
	}
}

func TestDefaultWeComWebSocketProfileIncludesDualChannels(
	t *testing.T,
) {
	data, err := os.ReadFile(filepath.Join(
		"..",
		"..",
		"openclaw.wecom.ai.websocket.yaml",
	))
	require.NoError(t, err)
	require.Contains(t, string(data), `- type: "weixin"`)
	require.Contains(t, string(data), `enabled_if_env_all:`)
	require.Contains(t, string(data), "WECOM_STREAM_BOT_ID")
	require.Contains(t, string(data), "WECOM_STREAM_SECRET")
}

func TestPrepareOpenClawConfigDefaultsSkillsOptions(
	t *testing.T,
) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"skills:\n"+
				"  root: ./skills\n",
		),
		0o600,
	)
	require.NoError(t, err)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	skillsNode := mappingValue(documentNode(&root), skillsKey)
	require.NotNil(t, skillsNode)
	require.Equal(
		t,
		skillsLoadModeSession,
		mappingStringValue(skillsNode, skillsLoadModeKey),
	)
	maxLoadedNode := mappingValue(skillsNode, skillsMaxLoadedKey)
	require.NotNil(t, maxLoadedNode)
	require.Equal(
		t,
		strconv.Itoa(defaultSkillsMaxLoaded),
		strings.TrimSpace(maxLoadedNode.Value),
	)
	skipFallbackNode := mappingValue(skillsNode, skillsSkipFallbackKey)
	require.NotNil(t, skipFallbackNode)
	require.Equal(
		t,
		strconv.FormatBool(defaultSkillsSkipFallback),
		strings.TrimSpace(skipFallbackNode.Value),
	)
}

func TestPrepareOpenClawConfigKeepsExplicitSkillsOptions(
	t *testing.T,
) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"skills:\n"+
				"  load_mode: turn\n"+
				"  max_loaded_skills: 3\n"+
				"  skip_fallback_on_session_summary: true\n",
		),
		0o600,
	)
	require.NoError(t, err)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	skillsNode := mappingValue(documentNode(&root), skillsKey)
	require.NotNil(t, skillsNode)
	require.Equal(
		t,
		skillsLoadModeTurn,
		mappingStringValue(skillsNode, skillsLoadModeKey),
	)
	maxLoadedNode := mappingValue(skillsNode, skillsMaxLoadedKey)
	require.NotNil(t, maxLoadedNode)
	require.Equal(t, "3", strings.TrimSpace(maxLoadedNode.Value))
	skipFallbackNode := mappingValue(skillsNode, skillsSkipFallbackKey)
	require.NotNil(t, skipFallbackNode)
	require.Equal(t, "true", strings.TrimSpace(skipFallbackNode.Value))
}

func TestPrepareOpenClawConfigAddsCodexSkillsDir(
	t *testing.T,
) {
	codexHome := t.TempDir()
	codexSkillsDir := filepath.Join(codexHome, skillsDirName)
	require.NoError(t, os.MkdirAll(codexSkillsDir, 0o755))
	t.Setenv(codexHomeEnvName, codexHome)

	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"skills:\n"+
				"  extra_dirs:\n"+
				"    - ./.agents/skills\n",
		),
		0o600,
	)
	require.NoError(t, err)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	skillsNode := mappingValue(documentNode(&root), skillsKey)
	require.NotNil(t, skillsNode)
	extraDirsNode := mappingValue(skillsNode, extraDirsKey)
	require.NotNil(t, extraDirsNode)
	require.True(
		t,
		sequenceContainsPath(extraDirsNode, "./.agents/skills"),
	)
	require.True(
		t,
		sequenceContainsPath(extraDirsNode, codexSkillsDir),
	)
}

func TestPrepareOpenClawConfigKeepsCustomToolingGuidance(
	t *testing.T,
) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"skills:\n"+
				"  tooling_guidance: keep me\n"+
				"  coding_agent:\n"+
				"    execution_mode: sandbox\n",
		),
		0o600,
	)
	require.NoError(t, err)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	preparedPath, ok, err := flagValueFromArgs(args, flagConfig)
	require.NoError(t, err)
	require.True(t, ok)

	prepared, err := os.ReadFile(preparedPath)
	require.NoError(t, err)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(prepared, &root))

	skillsNode := mappingValue(documentNode(&root), skillsKey)
	require.NotNil(t, skillsNode)
	require.Nil(t, mappingValue(skillsNode, codingAgentKey))
	require.Equal(
		t,
		"keep me",
		mappingStringValue(skillsNode, toolingGuidanceKey),
	)
}

func TestPrepareOpenClawConfigRejectsUnknownCodingAgentMode(
	t *testing.T,
) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte(
			"skills:\n"+
				"  coding_agent:\n"+
				"    execution_mode: mystery\n",
		),
		0o600,
	)
	require.NoError(t, err)

	_, cleanup, err := prepareOpenClawConfig(
		[]string{"-config", cfgPath},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	require.Error(t, err)
	require.Contains(
		t,
		err.Error(),
		"unknown skills.coding_agent.execution_mode",
	)
}

func TestPrepareOpenClawConfigSkipsInspectPlugins(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte("agent:\n  persona: concise\n"),
		0o600,
	)
	require.NoError(t, err)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{subcmdInspect, inspectCmdPlugins},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	require.Equal(
		t,
		[]string{subcmdInspect, inspectCmdPlugins},
		args,
	)
}

func TestPrepareOpenClawConfigSkipsBootstrapDeps(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "openclaw.yaml")
	err := os.WriteFile(
		cfgPath,
		[]byte("agent:\n  persona: concise\n"),
		0o600,
	)
	require.NoError(t, err)

	args, cleanup, err := prepareOpenClawConfig(
		[]string{subcmdBootstrap, bootstrapCmdDeps},
		startupPaths{OpenClawConfigPath: cfgPath},
	)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	require.Equal(
		t,
		[]string{subcmdBootstrap, bootstrapCmdDeps},
		args,
	)
}

func TestPrependPathEntriesPrependsWithoutDuplicates(t *testing.T) {
	sep := string(os.PathListSeparator)
	current := strings.Join(
		[]string{"/usr/bin", "/opt/bin", "/usr/bin"},
		sep,
	)
	got := prependPathEntries(
		current,
		[]string{"/opt/bin", "/custom/bin"},
	)
	require.Equal(
		t,
		strings.Join(
			[]string{"/opt/bin", "/custom/bin", "/usr/bin"},
			sep,
		),
		got,
	)
}

func TestRuntimePathDefaultsIncludesConventionalUserBinDirs(
	t *testing.T,
) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(goBinEnvName, "")
	t.Setenv(goPathEnvName, "")
	t.Setenv(cargoHomeEnvName, "")
	t.Setenv(pnpmHomeEnvName, "")
	t.Setenv(nodeHomeEnvName, "")
	t.Setenv(nodePrefixEnvName, "")
	t.Setenv(virtualEnvEnvName, "")

	localBin := filepath.Join(home, defaultUserLocalBinDir)
	userBin := filepath.Join(home, defaultUserBinDir)
	goBin := filepath.Join(
		home,
		defaultUserGoDir,
		defaultUserBinDir,
	)
	cargoBin := filepath.Join(
		home,
		defaultCargoDir,
		defaultUserBinDir,
	)

	got := runtimePathDefaults("", "")
	require.Contains(t, got, localBin)
	require.Contains(t, got, userBin)
	require.Contains(
		t,
		got,
		goBin,
	)
	require.Contains(
		t,
		got,
		cargoBin,
	)
}

func TestRuntimePathDefaultsUsesConfiguredToolchainRoot(
	t *testing.T,
) {
	customRoot := filepath.Join(
		t.TempDir(),
		"custom-toolchain",
	)
	t.Setenv(runtimeToolchainDirEnvName, customRoot)
	t.Setenv(runtimeManagedPythonEnvName, "")

	got := runtimePathDefaults("", "")

	require.Contains(
		t,
		got,
		runtimeToolchainBinDirFromRoot(customRoot),
	)
	require.Contains(
		t,
		got,
		runtimeManagedPythonBinDirFromRoot(customRoot),
	)
}

func TestRuntimePathDefaultsUsesConfiguredManagedPythonPath(
	t *testing.T,
) {
	pythonPath := filepath.Join(
		t.TempDir(),
		"venv",
		defaultUserBinDir,
		runtimePythonExecName,
	)
	t.Setenv(runtimeManagedPythonEnvName, pythonPath)

	got := runtimePathDefaults("", "")

	require.Contains(t, got, filepath.Dir(pythonPath))
}

func TestRuntimePathDefaultsIncludesStateToolsDir(t *testing.T) {
	stateDir := t.TempDir()
	got := runtimePathDefaults("", stateDir)
	require.Contains(t, got, runtimeToolsDir(stateDir))
	require.Contains(t, got, runtimeToolchainBinDir(stateDir))
	require.Contains(
		t,
		got,
		runtimeManagedPythonBinDir(stateDir),
	)
}

func TestRuntimePathDefaultsIncludesCommonToolHomeDirs(
	t *testing.T,
) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	goBin := filepath.Join(
		home,
		defaultUserGoDir,
		defaultUserBinDir,
	)
	cargoBin := filepath.Join(
		home,
		defaultCargoDir,
		defaultUserBinDir,
	)
	require.NoError(t, os.MkdirAll(goBin, 0o755))
	require.NoError(t, os.MkdirAll(cargoBin, 0o755))

	got := runtimePathDefaults("", "")

	require.Contains(t, got, goBin)
	require.Contains(t, got, cargoBin)
}

func TestRuntimePathDefaultsIncludesCommonToolEnvDirs(
	t *testing.T,
) {
	sep := string(os.PathListSeparator)
	t.Setenv(goBinEnvName, "/tmp/gobin")
	t.Setenv(
		goPathEnvName,
		strings.Join(
			[]string{"/tmp/gopath-a", "/tmp/gopath-b"},
			sep,
		),
	)
	t.Setenv(goRootEnvName, "/tmp/goroot")
	t.Setenv(cargoHomeEnvName, "/tmp/cargo")
	t.Setenv(pnpmHomeEnvName, "/tmp/pnpm")
	t.Setenv(nodeHomeEnvName, "/tmp/node-home")
	t.Setenv(nodePrefixEnvName, "/tmp/node-prefix")
	t.Setenv(virtualEnvEnvName, "/tmp/venv")

	got := runtimePathDefaults("", "")

	require.Contains(t, got, "/tmp/gobin")
	require.Contains(
		t,
		got,
		filepath.Join("/tmp/gopath-a", defaultUserBinDir),
	)
	require.Contains(
		t,
		got,
		filepath.Join("/tmp/gopath-b", defaultUserBinDir),
	)
	require.Contains(
		t,
		got,
		filepath.Join("/tmp/goroot", defaultUserBinDir),
	)
	require.Contains(
		t,
		got,
		filepath.Join("/tmp/cargo", defaultUserBinDir),
	)
	require.Contains(t, got, "/tmp/pnpm")
	require.Contains(
		t,
		got,
		filepath.Join("/tmp/node-home", defaultUserBinDir),
	)
	require.Contains(
		t,
		got,
		filepath.Join("/tmp/node-prefix", defaultUserBinDir),
	)
	require.Contains(
		t,
		got,
		filepath.Join("/tmp/venv", defaultUserBinDir),
	)
}

func TestRuntimePathDefaultsIncludesConfiguredExtraPathDirs(
	t *testing.T,
) {
	sep := string(os.PathListSeparator)
	t.Setenv(
		runtimeExtraPathEnvName,
		strings.Join(
			[]string{"/tmp/extra-a", "/tmp/extra-b"},
			sep,
		),
	)

	got := runtimePathDefaults("", "")

	require.Contains(t, got, "/tmp/extra-a")
	require.Contains(t, got, "/tmp/extra-b")
}

func TestRuntimePathDefaultsNormalizesExtraPathDirs(
	t *testing.T,
) {
	homeDir := t.TempDir()
	workDir := filepath.Join(t.TempDir(), "workspace")
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(workDir))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(oldWD))
	})
	t.Setenv("HOME", homeDir)
	t.Setenv(
		runtimeExtraPathEnvName,
		strings.Join(
			[]string{"~/bin", "tools/bin"},
			string(os.PathListSeparator),
		),
	)

	got := runtimePathDefaults("", "")

	require.Contains(
		t,
		got,
		filepath.Join(homeDir, defaultUserBinDir),
	)
	require.Contains(
		t,
		got,
		filepath.Join(workDir, "tools", defaultUserBinDir),
	)
}

func TestValidateUpgradeArgs(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		args []string
		err  string
	}{
		{
			name: "accepts config and state dir",
			args: []string{
				"--config=/tmp/openclaw.yaml",
				"-state-dir",
				"/tmp/state",
			},
		},
		{
			name: "accepts short config flag",
			args: []string{
				"-config",
				"/tmp/openclaw.yaml",
			},
		},
		{
			name: "accepts force config",
			args: []string{"--force-config"},
		},
		{
			name: "accepts requested version",
			args: []string{"--version", "v0.0.39"},
		},
		{
			name: "accepts requested version in equals form",
			args: []string{"--version=v0.0.39"},
		},
		{
			name: "accepts preview channel",
			args: []string{"--channel", releaseinfo.ChannelPreview},
		},
		{
			name: "rejects unknown channel",
			args: []string{"--channel", "nightly"},
			err:  "unsupported release channel",
		},
		{
			name: "accepts short force config",
			args: []string{"-f"},
		},
		{
			name: "accepts profile when forcing config",
			args: []string{
				"-f",
				"--profile",
				"mock",
			},
		},
		{
			name: "accepts weixin profile when forcing config",
			args: []string{
				"-f",
				"--profile",
				upgradeProfileWeixin,
			},
		},
		{
			name: "rejects profile without force config",
			args: []string{"--profile", "mock"},
			err:  "requires -f, --force-config",
		},
		{
			name: "rejects missing config value",
			args: []string{"--config"},
			err:  "requires a value",
		},
		{
			name: "rejects missing state dir value",
			args: []string{"--state-dir"},
			err:  "requires a value",
		},
		{
			name: "rejects missing version value",
			args: []string{"--version"},
			err:  "requires a value",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := validateUpgradeArgs(tc.args)
			if tc.err == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.err)
		})
	}
}

func TestParseUpgradeArgs(t *testing.T) {
	t.Parallel()

	opts, err := parseUpgradeArgs([]string{
		"--version=v0.0.39",
		"--channel=" + releaseinfo.ChannelPreview,
		"-f",
		"--profile=" + upgradeProfileWeComWS,
	})
	require.NoError(t, err)
	require.Equal(t, "v0.0.39", opts.Version)
	require.Equal(t, releaseinfo.ChannelPreview, opts.Channel)
	require.True(t, opts.ForceConfig)
	require.Equal(t, upgradeProfileWeComWS, opts.Profile)
}

func TestParseUpgradeArgsAcceptsWeixinProfile(t *testing.T) {
	t.Parallel()

	opts, err := parseUpgradeArgs([]string{
		"-f",
		"--profile",
		upgradeProfileWeixin,
	})
	require.NoError(t, err)
	require.True(t, opts.ForceConfig)
	require.Equal(t, upgradeProfileWeixin, opts.Profile)
}

func TestCompareReleaseVersions(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		left     string
		right    string
		expected int
	}{
		{
			name:     "semantic version ordering",
			left:     "v0.0.10",
			right:    "v0.0.2",
			expected: 1,
		},
		{
			name:     "missing patch equals zero patch",
			left:     "v1.2",
			right:    "v1.2.0",
			expected: 0,
		},
		{
			name:     "lower semantic version",
			left:     "v0.0.3",
			right:    "v0.1.0",
			expected: -1,
		},
		{
			name:     "fallback string compare",
			left:     "dev-build-2",
			right:    "dev-build-1",
			expected: 1,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(
				t,
				tc.expected,
				compareReleaseVersions(tc.left, tc.right),
			)
		})
	}
}

func TestResolveUpgradeConfigDir(t *testing.T) {
	t.Run("uses openclaw config directory", func(t *testing.T) {
		dir, err := resolveUpgradeConfigDir(startupPaths{
			OpenClawConfigPath: "/tmp/openclaw/openclaw.yaml",
			TRPCConfigPath:     "/tmp/openclaw/trpc_go.yaml",
		})
		require.NoError(t, err)
		require.Equal(t, "/tmp/openclaw", dir)
	})

	t.Run("uses trpc config directory", func(t *testing.T) {
		dir, err := resolveUpgradeConfigDir(startupPaths{
			TRPCConfigPath: "/tmp/openclaw/trpc_go.yaml",
		})
		require.NoError(t, err)
		require.Equal(t, "/tmp/openclaw", dir)
	})

	t.Run("falls back to default config directory", func(t *testing.T) {
		old := userHomeDirFunc
		userHomeDirFunc = func() (string, error) {
			return "/tmp/test-home", nil
		}
		t.Cleanup(func() {
			userHomeDirFunc = old
		})

		dir, err := resolveUpgradeConfigDir(startupPaths{})
		require.NoError(t, err)
		require.Equal(
			t,
			filepath.Join(
				"/tmp/test-home",
				defaultConfigRootDir,
				defaultConfigAppDir,
			),
			dir,
		)
	})
}

func TestUpgradeCheckDisabled(t *testing.T) {
	t.Run("enabled by default", func(t *testing.T) {
		t.Setenv(upgradeCheckDisableEnvName, "")
		require.False(t, upgradeCheckDisabled())
	})

	t.Run("supports common truthy values", func(t *testing.T) {
		for _, value := range []string{"1", "true", "yes", "on"} {
			t.Setenv(upgradeCheckDisableEnvName, value)
			require.True(t, upgradeCheckDisabled())
		}
	})
}

func TestExtractReleaseChanges(t *testing.T) {
	t.Parallel()

	changelog := "# trpc-claw\n\n" +
		"## v0.0.21 (2026-03-16)\n\n" +
		"- First change wraps\n" +
		"  into the next line.\n" +
		"- Second change.\n" +
		"- Third change.\n\n" +
		"## v0.0.20 (2026-03-16)\n\n" +
		"- Older change.\n"

	require.Equal(
		t,
		[]string{
			"First change wraps into the next line.",
			"Second change.",
		},
		extractReleaseChanges(changelog, "v0.0.21", 2),
	)
	require.Empty(
		t,
		extractReleaseChanges(changelog, "v9.9.9", 2),
	)
}

func TestLookupUpgradeSuggestionIncludesRecentChanges(t *testing.T) {
	server := newReleaseMetadataServer(
		t,
		"v9.9.9",
		"# trpc-claw\n\n"+
			"## v9.9.9\n\n"+
			"- Added startup version checks.\n"+
			"- Added recent release notes.\n",
	)

	oldBaseURL := releaseBaseURL
	releaseBaseURL = server.URL
	t.Cleanup(func() {
		releaseBaseURL = oldBaseURL
	})

	suggestion, ok, err := lookupUpgradeSuggestion(
		context.Background(),
	)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, currentVersion(), suggestion.CurrentVersion)
	require.Equal(t, "v9.9.9", suggestion.LatestVersion)
	require.Equal(
		t,
		[]string{
			"Added startup version checks.",
			"Added recent release notes.",
		},
		suggestion.RecentChanges,
	)
}

func TestBuildUpgradeSuggestionText(t *testing.T) {
	t.Parallel()

	text := buildUpgradeSuggestionText(upgradeSuggestion{
		CurrentVersion: "v0.0.20",
		LatestVersion:  "v0.0.21",
		RecentChanges: []string{
			"Added startup version checks.",
			"Added recent release notes.",
		},
	})

	require.Contains(t, text, "Current: v0.0.20")
	require.Contains(t, text, "Latest:  v0.0.21")
	require.Contains(t, text, "trpc-claw upgrade")
	require.Contains(t, text, "trpc-claw upgrade -f")
	require.Contains(t, text, "Recent changes:")
	require.Contains(t, text, "- Added startup version checks.")
}

func TestUpgradeReleaseAlreadyUpToDate(t *testing.T) {
	server := newReleaseServer(t, currentVersion())

	oldBaseURL := releaseBaseURL
	releaseBaseURL = server.URL
	t.Cleanup(func() {
		releaseBaseURL = oldBaseURL
	})

	oldInstall := installReleaseFunc
	installReleaseFunc = func(
		_ context.Context,
		_ string,
		_ string,
		_ string,
		_ upgradeOptions,
		_ io.Writer,
		_ io.Writer,
	) error {
		t.Fatal("installRelease should not be called")
		return nil
	}
	t.Cleanup(func() {
		installReleaseFunc = oldInstall
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := upgradeRelease(
		context.Background(),
		&stdout,
		&stderr,
		startupPaths{},
		upgradeOptions{},
	)
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "already up to date")
	require.Empty(t, stderr.String())
}

func TestUpgradeReleaseInstallsLatestRelease(t *testing.T) {
	server := newReleaseServer(t, "v9.9.9")

	oldBaseURL := releaseBaseURL
	releaseBaseURL = server.URL
	t.Cleanup(func() {
		releaseBaseURL = oldBaseURL
	})

	oldExecPath := executablePathFunc
	executablePathFunc = func() (string, error) {
		return "/tmp/openclaw/bin/trpc-claw", nil
	}
	t.Cleanup(func() {
		executablePathFunc = oldExecPath
	})

	var (
		gotVersion   string
		gotBinDir    string
		gotConfigDir string
	)
	oldInstall := installReleaseFunc
	installReleaseFunc = func(
		_ context.Context,
		version string,
		binDir string,
		configDir string,
		_ upgradeOptions,
		_ io.Writer,
		_ io.Writer,
	) error {
		gotVersion = version
		gotBinDir = binDir
		gotConfigDir = configDir
		return nil
	}
	t.Cleanup(func() {
		installReleaseFunc = oldInstall
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := upgradeRelease(
		context.Background(),
		&stdout,
		&stderr,
		startupPaths{
			OpenClawConfigPath: "/tmp/openclaw/conf/openclaw.yaml",
		},
		upgradeOptions{},
	)
	require.NoError(t, err)
	require.Equal(t, "v9.9.9", gotVersion)
	require.Equal(t, "/tmp/openclaw/bin", gotBinDir)
	require.Equal(t, "/tmp/openclaw/conf", gotConfigDir)
	require.Contains(t, stdout.String(), "Upgrading trpc-claw")
	require.Empty(t, stderr.String())
}

func TestUpgradeReleaseInstallsPreviewRelease(t *testing.T) {
	const previewVersion = "v9.9.10-preview.1"

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/preview/VERSION":
				_, _ = w.Write([]byte(previewVersion))
			default:
				http.NotFound(w, r)
			}
		},
	))
	defer server.Close()

	oldBaseURL := releaseBaseURL
	releaseBaseURL = server.URL
	t.Cleanup(func() {
		releaseBaseURL = oldBaseURL
	})

	oldExecPath := executablePathFunc
	executablePathFunc = func() (string, error) {
		return "/tmp/openclaw/bin/trpc-claw", nil
	}
	t.Cleanup(func() {
		executablePathFunc = oldExecPath
	})

	var gotVersion string
	oldInstall := installReleaseFunc
	installReleaseFunc = func(
		_ context.Context,
		version string,
		_ string,
		_ string,
		_ upgradeOptions,
		_ io.Writer,
		_ io.Writer,
	) error {
		gotVersion = version
		return nil
	}
	t.Cleanup(func() {
		installReleaseFunc = oldInstall
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := upgradeRelease(
		context.Background(),
		&stdout,
		&stderr,
		startupPaths{},
		upgradeOptions{
			Channel: releaseinfo.ChannelPreview,
		},
	)
	require.NoError(t, err)
	require.Equal(t, previewVersion, gotVersion)
	require.Contains(t, stdout.String(), "Upgrading trpc-claw")
	require.Empty(t, stderr.String())
}

func TestUpgradeReleaseReappliesConfigWhenForced(t *testing.T) {
	server := newReleaseServer(t, currentVersion())

	oldBaseURL := releaseBaseURL
	releaseBaseURL = server.URL
	t.Cleanup(func() {
		releaseBaseURL = oldBaseURL
	})

	oldExecPath := executablePathFunc
	executablePathFunc = func() (string, error) {
		return "/tmp/openclaw/bin/trpc-claw", nil
	}
	t.Cleanup(func() {
		executablePathFunc = oldExecPath
	})

	var (
		gotVersion string
		gotOpts    upgradeOptions
	)
	oldInstall := installReleaseFunc
	installReleaseFunc = func(
		_ context.Context,
		version string,
		_ string,
		_ string,
		opts upgradeOptions,
		_ io.Writer,
		_ io.Writer,
	) error {
		gotVersion = version
		gotOpts = opts
		return nil
	}
	t.Cleanup(func() {
		installReleaseFunc = oldInstall
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := upgradeRelease(
		context.Background(),
		&stdout,
		&stderr,
		startupPaths{},
		upgradeOptions{
			ForceConfig: true,
			Profile:     upgradeProfileWeComWS,
		},
	)
	require.NoError(t, err)
	require.Equal(t, currentVersion(), gotVersion)
	require.True(t, gotOpts.ForceConfig)
	require.Equal(t, upgradeProfileWeComWS, gotOpts.Profile)
	require.Contains(t, stdout.String(), "already up to date")
	require.Contains(
		t,
		stdout.String(),
		"Reapplying install script because --force-config was set",
	)
	require.Empty(t, stderr.String())
}

func TestUpgradeReleaseInstallsRequestedVersion(t *testing.T) {
	oldBaseURL := releaseBaseURL
	releaseBaseURL = "http://127.0.0.1:1"
	t.Cleanup(func() {
		releaseBaseURL = oldBaseURL
	})

	oldExecPath := executablePathFunc
	executablePathFunc = func() (string, error) {
		return "/tmp/openclaw/bin/trpc-claw", nil
	}
	t.Cleanup(func() {
		executablePathFunc = oldExecPath
	})

	var gotVersion string
	oldInstall := installReleaseFunc
	installReleaseFunc = func(
		_ context.Context,
		version string,
		_ string,
		_ string,
		_ upgradeOptions,
		_ io.Writer,
		_ io.Writer,
	) error {
		gotVersion = version
		return nil
	}
	t.Cleanup(func() {
		installReleaseFunc = oldInstall
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := upgradeRelease(
		context.Background(),
		&stdout,
		&stderr,
		startupPaths{},
		upgradeOptions{
			Version: "v9.9.8",
		},
	)
	require.NoError(t, err)
	require.Equal(t, "v9.9.8", gotVersion)
	require.Contains(
		t,
		stdout.String(),
		"Installing requested trpc-claw version v9.9.8",
	)
	require.Empty(t, stderr.String())
}

func TestUpgradeReleaseRequestedVersionAlreadyInstalled(t *testing.T) {
	oldInstall := installReleaseFunc
	installReleaseFunc = func(
		_ context.Context,
		_ string,
		_ string,
		_ string,
		_ upgradeOptions,
		_ io.Writer,
		_ io.Writer,
	) error {
		t.Fatal("installRelease should not be called")
		return nil
	}
	t.Cleanup(func() {
		installReleaseFunc = oldInstall
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := upgradeRelease(
		context.Background(),
		&stdout,
		&stderr,
		startupPaths{},
		upgradeOptions{
			Version: currentVersion(),
		},
	)
	require.NoError(t, err)
	require.Contains(
		t,
		stdout.String(),
		"already at requested version",
	)
	require.Empty(t, stderr.String())
}

func TestBuildInstallReleaseCommandUsesTempDir(t *testing.T) {
	workDir := t.TempDir()

	oldTempDir := tempDirFunc
	tempDirFunc = func() string {
		return workDir
	}
	t.Cleanup(func() {
		tempDirFunc = oldTempDir
	})

	cmd, err := buildInstallReleaseCommand(
		context.Background(),
		"/tmp/install.sh",
		"v9.9.9",
		"/tmp/bin",
		"/tmp/config",
		upgradeOptions{
			Profile:     upgradeProfileWeComWS,
			ForceConfig: true,
		},
		io.Discard,
		io.Discard,
	)
	require.NoError(t, err)
	require.Equal(t, workDir, cmd.Dir)
	require.Equal(t, upgradeShellName, filepath.Base(cmd.Path))
	require.Contains(t, cmd.Args, "--force-config")
	require.Contains(t, cmd.Args, "--profile")
	require.Contains(t, cmd.Args, upgradeProfileWeComWS)
}

func TestBuildInstallReleaseCommandFallsBackToHome(t *testing.T) {
	homeDir := t.TempDir()

	oldTempDir := tempDirFunc
	tempDirFunc = func() string {
		return ""
	}
	t.Cleanup(func() {
		tempDirFunc = oldTempDir
	})

	oldHomeDir := userHomeDirFunc
	userHomeDirFunc = func() (string, error) {
		return homeDir, nil
	}
	t.Cleanup(func() {
		userHomeDirFunc = oldHomeDir
	})

	cmd, err := buildInstallReleaseCommand(
		context.Background(),
		"/tmp/install.sh",
		"v9.9.9",
		"/tmp/bin",
		"/tmp/config",
		upgradeOptions{},
		io.Discard,
		io.Discard,
	)
	require.NoError(t, err)
	require.Equal(t, homeDir, cmd.Dir)
}

func newReleaseServer(t *testing.T, version string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/latest/VERSION", r.URL.Path)
			_, err := w.Write([]byte(version))
			require.NoError(t, err)
		},
	))
}

func newReleaseMetadataServer(
	t *testing.T,
	version string,
	changelog string,
) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/" + latestVersionRelPath:
				_, err := w.Write([]byte(version))
				require.NoError(t, err)
			case "/" + latestChangelogRelPath:
				_, err := w.Write([]byte(changelog))
				require.NoError(t, err)
			default:
				http.NotFound(w, r)
			}
		},
	))
}

func TestInstallScriptKeepsOnlyTRPCClawBinary(t *testing.T) {
	release := newInstallReleaseFixture(t, "v9.9.9")
	server := release.newServer(t)
	pathDir := makeLegacySha256sum(t)

	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	configDir := filepath.Join(home, "config")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	require.NoError(
		t,
		os.Symlink(
			installTestBinaryName,
			filepath.Join(binDir, installTestLegacyName),
		),
	)

	cmd := exec.Command(
		"bash",
		installScriptPath(t),
		"--version", release.Version,
		"--base-url", server.URL,
		"--profile", "mock",
		"--bin-dir", binDir,
		"--config-dir", configDir,
	)
	cmd.Env = append(
		os.Environ(),
		"HOME="+home,
		"PATH="+pathDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)

	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	require.Contains(t, string(output), "bootstrap deps")
	require.NotContains(t, string(output), "GitHub-compatible alias")
	require.FileExists(
		t,
		filepath.Join(binDir, installTestBinaryName),
	)
	_, err = os.Lstat(filepath.Join(binDir, installTestLegacyName))
	require.ErrorIs(t, err, os.ErrNotExist)

}

func TestInstallScriptCanBootstrapDeps(t *testing.T) {
	release := newInstallReleaseFixture(t, "v9.9.10")
	server := release.newServer(t)
	pathDir := makeLegacySha256sum(t)

	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	configDir := filepath.Join(home, "config")

	cmd := exec.Command(
		"bash",
		installScriptPath(t),
		"--version", release.Version,
		"--base-url", server.URL,
		"--profile", "mock",
		"--bin-dir", binDir,
		"--config-dir", configDir,
		"--bootstrap-deps",
		"--deps-profile", "pdf,office",
	)
	cmd.Env = append(
		os.Environ(),
		"HOME="+home,
		"PATH="+pathDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)

	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	require.Contains(
		t,
		string(output),
		"Bootstrapping bundled skill deps (pdf,office)",
	)

	logData, err := os.ReadFile(release.LogPath)
	require.NoError(t, err)
	require.Contains(
		t,
		string(logData),
		fmt.Sprintf(
			"bootstrap deps --state-dir %s --bundled "+
				"--profile pdf,office --apply",
			configDir,
		),
	)
}

func TestInstallScriptInstallsBundledSkills(t *testing.T) {
	release := newInstallReleaseFixture(t, "v9.9.11")
	server := release.newServer(t)
	pathDir := makeLegacySha256sum(t)

	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	configDir := filepath.Join(home, "config")

	cmd := exec.Command(
		"bash",
		installScriptPath(t),
		"--version", release.Version,
		"--base-url", server.URL,
		"--profile", "mock",
		"--bin-dir", binDir,
		"--config-dir", configDir,
	)
	cmd.Env = append(
		os.Environ(),
		"HOME="+home,
		"PATH="+pathDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)

	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	require.FileExists(
		t,
		filepath.Join(
			configDir,
			installTestSkillsDirName,
			installTestBundledSkillsDirName,
			installTestBundledSkillName,
			installTestSkillDocName,
		),
	)
	require.DirExists(
		t,
		filepath.Join(
			configDir,
			installTestSkillsDirName,
			installTestBundledSkillsDirName,
			installTestAnthropicSkillName,
		),
	)
	require.DirExists(
		t,
		filepath.Join(
			configDir,
			installTestSkillsDirName,
			installTestLocalSkillsDirName,
		),
	)

	scriptInfo, err := os.Stat(filepath.Join(
		configDir,
		installTestSkillsDirName,
		installTestBundledSkillsDirName,
		installTestScriptSkillName,
		installTestScriptsDirName,
		installTestSkillScriptName,
	))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o755), scriptInfo.Mode().Perm())
}

func TestInstallScriptCopiesWebSocketProfileWithModelEnvDefaults(
	t *testing.T,
) {
	release := newInstallReleaseFixture(t, "v9.9.12")
	server := release.newServer(t)
	pathDir := makeLegacySha256sum(t)

	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	configDir := filepath.Join(home, "config")

	cmd := exec.Command(
		"bash",
		installScriptPath(t),
		"--version", release.Version,
		"--base-url", server.URL,
		"--profile", upgradeProfileWeComWS,
		"--bin-dir", binDir,
		"--config-dir", configDir,
	)
	cmd.Env = append(
		os.Environ(),
		"HOME="+home,
		"PATH="+pathDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)

	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	require.Contains(t, string(output), "export OPENAI_MODEL='gpt-5.2'")
	require.Contains(
		t,
		string(output),
		"export OPENAI_BASE_URL=",
	)

	configData, err := os.ReadFile(
		filepath.Join(configDir, defaultConfigFile),
	)
	require.NoError(t, err)
	require.Equal(t, installTestDualConfigYAML, string(configData))

	profileData, err := os.ReadFile(filepath.Join(
		configDir,
		installTestProfileDirName,
		"openclaw.wecom.ai.websocket.yaml",
	))
	require.NoError(t, err)
	require.Equal(t, installTestDualConfigYAML, string(profileData))
}

func TestInstallScriptUsesDualProfileByDefault(t *testing.T) {
	release := newInstallReleaseFixture(t, "v9.9.12-default")
	server := release.newServer(t)
	pathDir := makeLegacySha256sum(t)

	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	configDir := filepath.Join(home, "config")

	cmd := exec.Command(
		"bash",
		installScriptPath(t),
		"--version", release.Version,
		"--base-url", server.URL,
		"--bin-dir", binDir,
		"--config-dir", configDir,
	)
	cmd.Env = append(
		os.Environ(),
		"HOME="+home,
		"PATH="+pathDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)

	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	require.Contains(
		t,
		string(output),
		"Profile: "+upgradeProfileWeComWS,
	)

	configData, err := os.ReadFile(
		filepath.Join(configDir, defaultConfigFile),
	)
	require.NoError(t, err)
	require.Equal(t, installTestDualConfigYAML, string(configData))
}

func TestInstallScriptCopiesWeixinProfile(t *testing.T) {
	release := newInstallReleaseFixture(t, "v9.9.13")
	server := release.newServer(t)
	pathDir := makeLegacySha256sum(t)

	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	configDir := filepath.Join(home, "config")

	cmd := exec.Command(
		"bash",
		installScriptPath(t),
		"--version", release.Version,
		"--base-url", server.URL,
		"--profile", upgradeProfileWeixin,
		"--bin-dir", binDir,
		"--config-dir", configDir,
	)
	cmd.Env = append(
		os.Environ(),
		"HOME="+home,
		"PATH="+pathDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)

	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	require.Contains(t, string(output), "Profile: "+upgradeProfileWeixin)

	configData, err := os.ReadFile(
		filepath.Join(configDir, defaultConfigFile),
	)
	require.NoError(t, err)
	require.Equal(t, installTestWeixinConfigYAML, string(configData))

	profileData, err := os.ReadFile(filepath.Join(
		configDir,
		installTestProfileDirName,
		"openclaw.weixin.yaml",
	))
	require.NoError(t, err)
	require.Equal(t, installTestWeixinConfigYAML, string(profileData))
}

const (
	installTestPackageRootName      = "trpc-claw"
	installTestBinaryName           = "trpc-claw"
	installTestLegacyName           = "openclaw"
	installTestProfileDirName       = "profiles"
	installTestSkillsDirName        = "skills"
	installTestBundledSkillName     = "weather"
	installTestAnthropicSkillName   = "anthropic-docx"
	installTestScriptSkillName      = "tmux"
	installTestSkillDocName         = "SKILL.md"
	installTestScriptsDirName       = "scripts"
	installTestSkillScriptName      = "find-sessions.sh"
	installTestBundledSkillsDirName = "bundled"
	installTestLocalSkillsDirName   = "local"
)

type installReleaseFixture struct {
	Version      string
	LogPath      string
	ArchiveName  string
	ArchiveBytes []byte
	Checksums    []byte
}

func newInstallReleaseFixture(
	t *testing.T,
	version string,
) installReleaseFixture {
	t.Helper()

	logPath := filepath.Join(t.TempDir(), "binary.log")
	archiveName := installArchiveName(t, version)
	archiveBytes := installArchiveBytes(t, version, logPath)
	sum := sha256.Sum256(archiveBytes)
	checksums := []byte(fmt.Sprintf(
		"%x  ./%s\n",
		sum,
		archiveName,
	))

	return installReleaseFixture{
		Version:      version,
		LogPath:      logPath,
		ArchiveName:  archiveName,
		ArchiveBytes: archiveBytes,
		Checksums:    checksums,
	}
}

func (f installReleaseFixture) newServer(
	t *testing.T,
) *httptest.Server {
	t.Helper()

	latestPath := "/latest/VERSION"
	checksumsPath := filepath.Join(
		"/releases",
		f.Version,
		"checksums.txt",
	)
	archivePath := filepath.Join(
		"/releases",
		f.Version,
		f.ArchiveName,
	)

	return httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case latestPath:
				_, err := w.Write([]byte(f.Version))
				require.NoError(t, err)
			case checksumsPath:
				_, err := w.Write(f.Checksums)
				require.NoError(t, err)
			case archivePath:
				_, err := w.Write(f.ArchiveBytes)
				require.NoError(t, err)
			default:
				http.NotFound(w, r)
			}
		},
	))
}

func installArchiveName(t *testing.T, version string) string {
	t.Helper()

	goos := runtime.GOOS
	switch goos {
	case "linux", "darwin":
	default:
		t.Skipf("unsupported test GOOS: %s", goos)
	}

	goarch := runtime.GOARCH
	switch goarch {
	case "amd64", "arm64":
	default:
		t.Skipf("unsupported test GOARCH: %s", goarch)
	}

	return fmt.Sprintf(
		"%s-%s-%s-%s.tar.gz",
		installTestBinaryName,
		version,
		goos,
		goarch,
	)
}

func installArchiveBytes(
	t *testing.T,
	version string,
	logPath string,
) []byte {
	t.Helper()

	var raw bytes.Buffer
	gzipWriter := gzip.NewWriter(&raw)
	tarWriter := tar.NewWriter(gzipWriter)

	files := map[string]struct {
		mode    int64
		content string
	}{
		filepath.ToSlash(filepath.Join(
			installTestPackageRootName,
			"bin",
			installTestBinaryName,
		)): {
			mode: 0o755,
			content: installTestBinaryScript(
				logPath,
			),
		},
		filepath.ToSlash(filepath.Join(
			installTestPackageRootName,
			"config",
			"trpc_go.yaml",
		)): {
			mode:    0o644,
			content: "server:\n  service: []\n",
		},
		filepath.ToSlash(filepath.Join(
			installTestPackageRootName,
			"config",
			"openclaw.mock.yaml",
		)): {
			mode:    0o644,
			content: installTestConfigYAML,
		},
		filepath.ToSlash(filepath.Join(
			installTestPackageRootName,
			"config",
			"openclaw.wecom.ai.yaml",
		)): {
			mode:    0o644,
			content: installTestOpenAIConfigYAML,
		},
		filepath.ToSlash(filepath.Join(
			installTestPackageRootName,
			"config",
			"openclaw.wecom.ai.websocket.yaml",
		)): {
			mode:    0o644,
			content: installTestDualConfigYAML,
		},
		filepath.ToSlash(filepath.Join(
			installTestPackageRootName,
			"config",
			"openclaw.wecom.notification.yaml",
		)): {
			mode:    0o644,
			content: installTestOpenAIConfigYAML,
		},
		filepath.ToSlash(filepath.Join(
			installTestPackageRootName,
			"config",
			"openclaw.weixin.yaml",
		)): {
			mode:    0o644,
			content: installTestWeixinConfigYAML,
		},
		filepath.ToSlash(filepath.Join(
			installTestPackageRootName,
			installTestSkillsDirName,
			installTestBundledSkillName,
			installTestSkillDocName,
		)): {
			mode:    0o644,
			content: "name: weather\n",
		},
		filepath.ToSlash(filepath.Join(
			installTestPackageRootName,
			installTestSkillsDirName,
			installTestAnthropicSkillName,
			installTestSkillDocName,
		)): {
			mode: 0o644,
			content: "---\n" +
				"name: anthropic-docx\n" +
				"metadata:\n" +
				"  openclaw:\n" +
				"    homepage: https://example.com\n" +
				"---\n",
		},
		filepath.ToSlash(filepath.Join(
			installTestPackageRootName,
			installTestSkillsDirName,
			installTestScriptSkillName,
			installTestScriptsDirName,
			installTestSkillScriptName,
		)): {
			mode:    0o755,
			content: "#!/usr/bin/env bash\nexit 0\n",
		},
		filepath.ToSlash(filepath.Join(
			installTestPackageRootName,
			"metadata.env",
		)): {
			mode: 0o644,
			content: fmt.Sprintf(
				"PACKAGE_VERSION='%s'\n"+
					"SQLITE_MEMORY_BACKEND='enabled'\n"+
					"SQLITEVEC_MEMORY_BACKEND='enabled'\n",
				version,
			),
		},
	}

	for name, file := range files {
		writeTarFile(t, tarWriter, name, file.mode, file.content)
	}

	require.NoError(t, tarWriter.Close())
	require.NoError(t, gzipWriter.Close())
	return raw.Bytes()
}

func writeTarFile(
	t *testing.T,
	tarWriter *tar.Writer,
	name string,
	mode int64,
	content string,
) {
	t.Helper()

	data := []byte(content)
	header := &tar.Header{
		Name: name,
		Mode: mode,
		Size: int64(len(data)),
	}
	require.NoError(t, tarWriter.WriteHeader(header))
	_, err := tarWriter.Write(data)
	require.NoError(t, err)
}

const installTestConfigYAML = "state_dir: \"~/.trpc-agent-go/openclaw\"\n"

const installTestOpenAIConfigYAML = "" +
	"state_dir: \"~/.trpc-agent-go/openclaw\"\n" +
	"model:\n" +
	"  mode: \"openai\"\n" +
	"  name: \"${OPENAI_MODEL}\"\n" +
	"  base_url: " +
	"\"${OPENAI_BASE_URL}\"\n"

const installTestDualConfigYAML = "" +
	"state_dir: \"~/.trpc-agent-go/openclaw\"\n" +
	"model:\n" +
	"  mode: \"openai\"\n" +
	"  name: \"${OPENAI_MODEL}\"\n" +
	"  base_url: " +
	"\"${OPENAI_BASE_URL}\"\n" +
	"channels:\n" +
	"  - type: \"weixin\"\n" +
	"    enabled: true\n" +
	"    name: \"weixin-direct\"\n" +
	"  - type: \"wecom\"\n" +
	"    enabled: true\n" +
	"    enabled_if_env_all:\n" +
	"      - \"WECOM_STREAM_BOT_ID\"\n" +
	"      - \"WECOM_STREAM_SECRET\"\n" +
	"    name: \"wecom-ai-websocket\"\n"

const installTestWeixinConfigYAML = "" +
	"state_dir: \"~/.trpc-agent-go/openclaw\"\n" +
	"model:\n" +
	"  mode: \"openai\"\n" +
	"  name: \"${OPENAI_MODEL}\"\n" +
	"  base_url: " +
	"\"${OPENAI_BASE_URL}\"\n" +
	"channels:\n" +
	"  - type: \"weixin\"\n" +
	"    name: \"weixin-direct\"\n"

func installTestBinaryScript(logPath string) string {
	return fmt.Sprintf(`#!/usr/bin/env bash
set -eu
printf '%%s\n' "$*" >>%q
case "${1:-}" in
  inspect)
    if [ "${2:-}" = "plugins" ]; then
      printf '%%s\n' '- sqlite'
      exit 0
    fi
    if [ "${2:-}" = "deps" ]; then
      printf '%%s\n' '{"missing":{}}'
      exit 0
    fi
    ;;
  bootstrap)
    if [ "${2:-}" = "deps" ]; then
      exit 0
    fi
    ;;
esac
exit 0
`, logPath)
}

func makeLegacySha256sum(t *testing.T) string {
	t.Helper()

	realPath, err := exec.LookPath("sha256sum")
	if err != nil {
		t.Skip("sha256sum is required for this compatibility test")
	}

	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "sha256sum")
	script := fmt.Sprintf(`#!/usr/bin/env bash
set -eu
if [ "$#" -ne 1 ]; then
  printf 'sha256sum: unrecognized option %s\n' "$1" >&2
  exit 1
fi
exec %q "$@"
`, "'$1'", realPath)
	err = os.WriteFile(scriptPath, []byte(script), 0o755)
	require.NoError(t, err)
	return dir
}

func writeBundledSkillDoc(
	t *testing.T,
	root string,
	name string,
	content string,
) {
	t.Helper()

	skillDir := filepath.Join(root, name)
	err := os.MkdirAll(skillDir, 0o755)
	require.NoError(t, err)
	err = os.WriteFile(
		filepath.Join(skillDir, skillDocFileName),
		[]byte(content),
		0o644,
	)
	require.NoError(t, err)
}

func bundledDepsTestMismatchOS() string {
	switch runtime.GOOS {
	case "darwin":
		return "linux"
	case "linux":
		return "darwin"
	default:
		return "darwin"
	}
}

func installScriptPath(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	require.NoError(t, err)
	return filepath.Clean(
		filepath.Join(wd, "..", "..", "install.sh"),
	)
}

type runtimeAwareTestChannel struct {
	manager *runtimectl.Manager
}

func (c *runtimeAwareTestChannel) ID() string {
	return "runtime-aware-test"
}

func (c *runtimeAwareTestChannel) Run(context.Context) error {
	return nil
}

func (c *runtimeAwareTestChannel) Close(chan struct{}) error {
	return nil
}

func (c *runtimeAwareTestChannel) SetRuntimeLifecycleController(
	controller *runtimectl.Manager,
) {
	c.manager = controller
}
