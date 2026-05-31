package wecome2e_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	wecomE2ETestEnableEnv            = "WECOM_OPENCLAW_E2E"
	wecomE2EAppName                  = "wecom-openclaw-e2e"
	wecomE2ERuntimeAppName           = "openclaw"
	wecomE2EClawIDEnv                = "WECOM_OPENCLAW_CLAW_ID"
	wecomE2ELegacyClawIDEnv          = "CLAW_ID"
	wecomE2EClawAuthHeadersEnv       = "WECOM_OPENCLAW_CLAW_AUTH_HEADERS"
	wecomE2ELegacyClawAuthHeadersEnv = "CLAW_CONFIG_AUTH_HEADERS"
	wecomE2EClawConfigURLEnv         = "WECOM_OPENCLAW_CLAW_CONFIG_URL"
	wecomE2ELegacyClawConfigURLEnv   = "CLAW_CONFIG_URL"
	wecomE2EImageEnv                 = "WECOM_OPENCLAW_DOCKER_IMAGE"
	wecomE2ERunnerURLEnv             = "WECOM_OPENCLAW_RUNNER_URL"
	wecomE2EModelEnv                 = "OPENAI_MODEL"
	wecomE2EAPIKeyEnv                = "OPENAI_API_KEY"
	wecomE2EBaseURLEnv               = "OPENAI_BASE_URL"
	wecomE2EJudgeModelEnv            = "WECOM_OPENCLAW_JUDGE_MODEL"
	wecomE2EJudgeAPIKeyEnv           = "WECOM_OPENCLAW_JUDGE_API_KEY"
	wecomE2EJudgeBaseEnv             = "WECOM_OPENCLAW_JUDGE_BASE_URL"
	wecomE2EEvalResultEnv            = "WECOM_OPENCLAW_EVAL_RESULT_DIR"
	wecomE2EDefaultClawConfigURL     = "http://api.apigw.oa.com/agui/claw/getClawConfig"
	wecomE2EDefaultImage             = "mirrors.tencent.com/todacc/agent_builder_claw:1.0.4"
	wecomE2EDefaultRunnerURL         = "https://mirrors.tencent.com/repository/generic/trpc-claw/v0.0.4/runner.tar.gz"
	wecomE2EReplyTimeout             = 90 * time.Second
	wecomE2EStartupTimeout           = 3 * time.Minute
	wecomE2ECronTimeout              = 2 * time.Minute
	wecomE2ECronQuietTime            = 5 * time.Second
	wecomE2EPollInterval             = 50 * time.Millisecond
	wecomE2EHeartbeatValue           = "1h"
)

type wecomE2EEnv struct {
	clawID                string
	clawConfigURL         string
	clawConfigAuthHeaders string
	image                 string
	runnerURL             string
	modelName             string
	apiKey                string
	baseURL               string
	judgeModelName        string
	judgeAPIKey           string
	judgeBaseURL          string
	remoteEnvVars         map[string]string
}

type remoteClawConfigPayload struct {
	Data           *remoteClawConfig `json:"data,omitempty"`
	EnvVars        map[string]string `json:"envVars,omitempty"`
	SkillDownloads []any             `json:"skillDownloads,omitempty"`
}

type remoteClawConfig struct {
	EnvVars map[string]string `json:"envVars,omitempty"`
}

func requireWeComE2EEnv(t *testing.T) wecomE2EEnv {
	t.Helper()
	if strings.TrimSpace(os.Getenv(wecomE2ETestEnableEnv)) != "1" {
		t.Skipf("skip wecom runtime e2e without %s=1", wecomE2ETestEnableEnv)
	}
	clawID := firstNonEmptyEnv(wecomE2EClawIDEnv, wecomE2ELegacyClawIDEnv)
	if clawID == "" {
		t.Skipf(
			"skip wecom runtime e2e without %s or %s",
			wecomE2EClawIDEnv,
			wecomE2ELegacyClawIDEnv,
		)
	}
	authHeaders := firstNonEmptyEnv(
		wecomE2EClawAuthHeadersEnv,
		wecomE2ELegacyClawAuthHeadersEnv,
	)
	if authHeaders == "" {
		t.Skipf(
			"skip wecom runtime e2e without %s or %s",
			wecomE2EClawAuthHeadersEnv,
			wecomE2ELegacyClawAuthHeadersEnv,
		)
	}
	clawConfigURL := firstNonEmptyEnv(
		wecomE2EClawConfigURLEnv,
		wecomE2ELegacyClawConfigURLEnv,
	)
	if clawConfigURL == "" {
		clawConfigURL = wecomE2EDefaultClawConfigURL
	}
	remoteCfg := fetchRemoteClawConfig(
		t,
		clawID,
		clawConfigURL,
		authHeaders,
	)
	modelName := strings.TrimSpace(remoteCfg.EnvVars[wecomE2EModelEnv])
	if modelName == "" {
		t.Skipf(
			"skip wecom runtime e2e because remote claw config does not contain %s",
			wecomE2EModelEnv,
		)
	}
	apiKey := strings.TrimSpace(remoteCfg.EnvVars[wecomE2EAPIKeyEnv])
	if apiKey == "" {
		t.Skipf(
			"skip wecom runtime e2e because remote claw config does not contain %s",
			wecomE2EAPIKeyEnv,
		)
	}
	baseURL := strings.TrimSpace(remoteCfg.EnvVars[wecomE2EBaseURLEnv])
	judgeModelName := strings.TrimSpace(os.Getenv(wecomE2EJudgeModelEnv))
	if judgeModelName == "" {
		judgeModelName = modelName
	}
	judgeAPIKey := strings.TrimSpace(os.Getenv(wecomE2EJudgeAPIKeyEnv))
	if judgeAPIKey == "" {
		judgeAPIKey = apiKey
	}
	judgeBaseURL := strings.TrimSpace(os.Getenv(wecomE2EJudgeBaseEnv))
	if judgeBaseURL == "" {
		judgeBaseURL = baseURL
	}
	return wecomE2EEnv{
		clawID:                clawID,
		clawConfigURL:         clawConfigURL,
		clawConfigAuthHeaders: authHeaders,
		image: firstNonEmptyValue(
			strings.TrimSpace(os.Getenv(wecomE2EImageEnv)),
			wecomE2EDefaultImage,
		),
		runnerURL: firstNonEmptyValue(
			strings.TrimSpace(os.Getenv(wecomE2ERunnerURLEnv)),
			wecomE2EDefaultRunnerURL,
		),
		modelName:      modelName,
		apiKey:         apiKey,
		baseURL:        baseURL,
		judgeModelName: judgeModelName,
		judgeAPIKey:    judgeAPIKey,
		judgeBaseURL:   judgeBaseURL,
		remoteEnvVars:  remoteCfg.EnvVars,
	}
}

func fetchRemoteClawConfig(
	t *testing.T,
	clawID string,
	clawConfigURL string,
	authHeaders string,
) remoteClawConfig {
	t.Helper()
	body, err := json.Marshal(map[string]string{
		"claw_id": clawID,
	})
	require.NoError(t, err)
	req, err := http.NewRequest(
		http.MethodPost,
		clawConfigURL,
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	applyHeaderPairs(req.Header, authHeaders)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var payload remoteClawConfigPayload
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	cfg := remoteClawConfig{
		EnvVars: make(map[string]string),
	}
	if payload.Data != nil && payload.Data.EnvVars != nil {
		cfg.EnvVars = payload.Data.EnvVars
		return cfg
	}
	if payload.EnvVars != nil {
		cfg.EnvVars = payload.EnvVars
	}
	return cfg
}

func applyHeaderPairs(header http.Header, raw string) {
	for _, pair := range strings.Fields(strings.TrimSpace(raw)) {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			continue
		}
		header.Set(key, value)
	}
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmptyValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
