package wecome2e_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	wecomE2EDockerHostAlias        = "host.docker.internal"
	wecomE2EMediaHostAlias         = "wework.qpic.cn"
	wecomE2EContainerStateDir      = "/root/.trpc-agent-go/openclaw"
	wecomE2EContainerWorkspaceDir  = "/data/cic/workspace"
	wecomE2EContainerHookDir       = "/root/.trpc-agent-go/openclaw/hooks"
	wecomE2EContainerBinaryMount   = "/opt/openclaw-e2e-bin"
	wecomE2EContainerStartLogPath  = "/app/start.log"
	wecomE2EContainerGatewayHealth = "http://127.0.0.1:8080/healthz"
)

type wecomE2EHarness struct {
	stateDir      string
	workspaceDir  string
	containerName string
	hookDir       string
	rootDir       string
	ws            *fakeWeComWebSocketServer
	media         *fakeMediaServer
}

var (
	dockerCheckOnce         sync.Once
	dockerCheckErr          error
	openClawBinaryBuildOnce sync.Once
	openClawBinaryPath      string
	openClawBinaryBuildErr  error
	sharedHarnessOnce       sync.Once
	sharedHarness           *wecomE2EHarness
	sharedHarnessErr        error
)

func newWeComE2EHarness(t *testing.T, env wecomE2EEnv) *wecomE2EHarness {
	t.Helper()
	sharedHarnessOnce.Do(func() {
		sharedHarness, sharedHarnessErr = startSharedWeComE2EHarness(env)
	})
	require.NoError(t, sharedHarnessErr)
	require.NotNil(t, sharedHarness)
	return sharedHarness
}

func startSharedWeComE2EHarness(env wecomE2EEnv) (*wecomE2EHarness, error) {
	if err := dockerAvailableErr(); err != nil {
		return nil, err
	}
	rootDir, err := os.MkdirTemp("", "openclaw-e2e-suite-*")
	if err != nil {
		return nil, err
	}
	hookDir := filepath.Join(rootDir, "hooks")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		_ = os.RemoveAll(rootDir)
		return nil, err
	}
	if err := writePrestartHookFile(filepath.Join(hookDir, "prestart.sh")); err != nil {
		_ = os.RemoveAll(rootDir)
		return nil, err
	}
	localBinaryPath, err := openClawE2EBinaryPath()
	if err != nil {
		_ = os.RemoveAll(rootDir)
		return nil, err
	}
	wsServer, err := newFakeWeComWebSocketServer(wecomE2EDockerHostAlias)
	if err != nil {
		_ = os.RemoveAll(rootDir)
		return nil, err
	}
	mediaServer, err := newFakeMediaServer(wecomE2EMediaHostAlias)
	if err != nil {
		wsServer.close()
		_ = os.RemoveAll(rootDir)
		return nil, err
	}
	h := &wecomE2EHarness{
		stateDir:      wecomE2EContainerStateDir,
		workspaceDir:  wecomE2EContainerWorkspaceDir,
		containerName: wecomE2ESuiteContainerName(),
		hookDir:       hookDir,
		rootDir:       rootDir,
		ws:            wsServer,
		media:         mediaServer,
	}
	if err := startOnlineLikeContainer(
		env,
		h.containerName,
		h.hookDir,
		filepath.Dir(localBinaryPath),
		h.ws.containerWSURL,
	); err != nil {
		h.close()
		return nil, err
	}
	if err := waitForOpenClawSubscribeErr(h, 0); err != nil {
		h.close()
		return nil, err
	}
	if err := waitForContainerHealthErr(h); err != nil {
		h.close()
		return nil, err
	}
	return h, nil
}

func repoOpenClawRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("failed to locate openclaw e2e root")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func dockerAvailableErr() error {
	dockerCheckOnce.Do(func() {
		cmd := exec.Command("docker", "version", "--format", "{{.Server.Version}}")
		output, err := cmd.CombinedOutput()
		if err != nil {
			dockerCheckErr = fmt.Errorf(
				"docker is unavailable: %w\n%s",
				err,
				strings.TrimSpace(string(output)),
			)
			return
		}
		if strings.TrimSpace(string(output)) == "" {
			dockerCheckErr = fmt.Errorf("docker returned empty server version")
		}
	})
	return dockerCheckErr
}

func openClawE2EBinaryPath() (string, error) {
	openClawBinaryBuildOnce.Do(func() {
		buildDir, err := os.MkdirTemp("", "openclaw-e2e-bin-*")
		if err != nil {
			openClawBinaryBuildErr = err
			return
		}
		binaryName := "openclaw-e2e"
		if runtime.GOOS == "windows" {
			binaryName += ".exe"
		}
		openClawBinaryPath = filepath.Join(buildDir, binaryName)
		cmd := exec.Command("go", "build", "-o", openClawBinaryPath, "./cmd/openclaw")
		cmd.Dir = repoOpenClawRoot()
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		output, err := cmd.CombinedOutput()
		if err != nil {
			openClawBinaryBuildErr = fmt.Errorf(
				"build cmd/openclaw failed: %w\n%s",
				err,
				strings.TrimSpace(string(output)),
			)
		}
	})
	if openClawBinaryBuildErr != nil {
		return "", openClawBinaryBuildErr
	}
	if strings.TrimSpace(openClawBinaryPath) == "" {
		return "", errors.New("openclaw e2e binary path is empty")
	}
	return openClawBinaryPath, nil
}

func writePrestartHookFile(path string) error {
	const script = `#!/usr/bin/env bash
set -euo pipefail
config_path="${TRPC_CLAW_CONFIG_PATH:-${TRPC_CLAW_STATE_DIR}/openclaw.yaml}"
deps_stamp="${TRPC_CLAW_STATE_DIR}/runtime/.pdf-skill-deps-ready"
local_binary="` + wecomE2EContainerBinaryMount + `/openclaw-e2e"
if [ -f "$local_binary" ]; then
  cp "$local_binary" /root/.local/bin/trpc-claw
  chmod 0755 /root/.local/bin/trpc-claw
fi
if [ ! -f "$deps_stamp" ]; then
  mkdir -p "$(dirname "$deps_stamp")"
  trpc-claw bootstrap deps \
    --state-dir "${TRPC_CLAW_STATE_DIR}" \
    --skills-root "${TRPC_CLAW_STATE_DIR}/skills/bundled" \
    --skill anthropic-pdf,nano-pdf \
    --apply
  touch "$deps_stamp"
fi
python3 - "$config_path" <<'PY'
import pathlib
import re
import sys

path = pathlib.Path(sys.argv[1])
text = path.read_text(encoding="utf-8")
text = re.sub(r'(?m)^[ \t]*#?[ \t]*ws_url:.*$', '      ws_url: "${WECOM_E2E_WS_URL}"', text, count=1)
text = re.sub(r'(?m)^[ \t]*heartbeat_interval:.*$', '      heartbeat_interval: "1h"', text, count=1)
text = re.sub(r'(?m)^[ \t]*enter_chat_welcome:.*$', '      enter_chat_welcome: false', text, count=1)
text = re.sub(r'(?m)^[ \t]*group_session_mode:.*$', '      group_session_mode: "shared"', text, count=1)
text = re.sub(r'(?m)^[ \t]*add_session_summary:.*$', '  add_session_summary: false', text, count=1)
text = re.sub(r'(?m)^[ \t]*enable_context_compaction:.*\n', '', text)
text = re.sub(r'(?m)^[ \t]*watch:.*\n', '', text)
text = re.sub(r'(?m)^[ \t]*watch_bundled:.*\n', '', text)
text = re.sub(r'(?m)^[ \t]*watch_debounce_ms:.*\n', '', text)
text = re.sub(r'(?m)^[ \t]*mode:.*# auto:.*\n', '', text)
text = re.sub(r'(?m)^[ \t]*approx_runes_per_token:.*\n', '', text)
text = re.sub(r'(?m)^[ \t]*backend: "sqlite"$', '  backend: "inmemory"', text, count=1)
text = re.sub(r'(?ms)(session:\n\s*backend: "inmemory"\n\s*summary:\n\s*)enabled: true', r'\1enabled: false', text, count=1)
marker = '      embed_file_url: false'
injection = marker + '\n      runtime_default_workdir: "/data/cic/workspace"\n      runtime_reply_delivery_roots:\n        - "/data/cic/workspace"\n        - "${TRPC_CLAW_STATE_DIR}"'
if injection not in text:
    if marker not in text:
        raise SystemExit("missing embed_file_url marker in openclaw.yaml")
    text = text.replace(marker, injection, 1)
path.write_text(text, encoding="utf-8")
PY
`
	return os.WriteFile(path, []byte(script), 0o755)
}

func wecomE2ESuiteContainerName() string {
	return fmt.Sprintf("openclaw-e2e-suite-%d", time.Now().UnixNano())
}

func startOnlineLikeContainer(
	env wecomE2EEnv,
	containerName string,
	hookDir string,
	localBinaryDir string,
	wsURL string,
) error {
	_, _ = dockerOutput("rm", "-f", containerName)
	bootstrap := fmt.Sprintf(`set -euo pipefail
mkdir -p /app
cd /app
curl -fsSL -o runner.tar.gz %q
tar -xzf runner.tar.gz
mkdir -p /data/cic/workspace
cd /data/cic/workspace
/app/start.sh > %s 2>&1`, env.runnerURL, wecomE2EContainerStartLogPath)
	args := []string{
		"run",
		"-d",
		"--name", containerName,
		"--add-host", wecomE2EDockerHostAlias + ":host-gateway",
		"--add-host", wecomE2EMediaHostAlias + ":host-gateway",
		"-v", hookDir + ":" + wecomE2EContainerHookDir + ":ro",
		"-v", localBinaryDir + ":" + wecomE2EContainerBinaryMount + ":ro",
		"-e", "CLAW_ID=" + env.clawID,
		"-e", "CLAW_CONFIG_AUTH_HEADERS=" + env.clawConfigAuthHeaders,
		"-e", "WECOM_GROUP_SESSION_MODE=shared",
		"-e", "WECOM_E2E_WS_URL=" + wsURL,
	}
	if strings.TrimSpace(env.clawConfigURL) != "" {
		args = append(args, "-e", "CLAW_CONFIG_URL="+env.clawConfigURL)
	}
	args = append(
		args,
		env.image,
		"/bin/bash",
		"-lc",
		bootstrap,
	)
	output, err := dockerOutput(args...)
	if err != nil {
		return fmt.Errorf(
			"start online-like container failed: %w: %s",
			err,
			strings.TrimSpace(output),
		)
	}
	return nil
}

func waitForOpenClawSubscribeErr(
	h *wecomE2EHarness,
	start int,
) error {
	deadline := time.Now().Add(wecomE2EStartupTimeout)
	for {
		if err := h.ws.backgroundErrValue(); err != nil {
			return fmt.Errorf(
				"fake wecom websocket server failed before subscribe: %w\n%s",
				err,
				h.debugSnapshot(),
			)
		}
		frames := h.ws.matchingFrames(start, func(frame capturedWSFrame) bool {
			return frame.Command == wsCommandSubscribe
		})
		if len(frames) > 0 {
			return nil
		}
		status := h.containerStatus()
		if status == "exited" || status == "dead" {
			return fmt.Errorf(
				"openclaw container exited before websocket subscribe\n%s",
				h.debugSnapshot(),
			)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf(
				"timeout waiting openclaw websocket subscribe\n%s",
				h.debugSnapshot(),
			)
		}
		time.Sleep(wecomE2EPollInterval)
	}
}

func waitForContainerHealthErr(h *wecomE2EHarness) error {
	deadline := time.Now().Add(wecomE2EReplyTimeout)
	for time.Now().Before(deadline) {
		output, err := dockerOutput(
			"exec",
			h.containerName,
			"sh",
			"-lc",
			"curl -fsS "+wecomE2EContainerGatewayHealth,
		)
		if err == nil && strings.Contains(output, `"status":"ok"`) {
			return nil
		}
		time.Sleep(wecomE2EPollInterval)
	}
	return fmt.Errorf("timeout waiting container health\n%s", h.debugSnapshot())
}

func (h *wecomE2EHarness) close() {
	if h == nil {
		return
	}
	if h.media != nil {
		h.media.close()
	}
	if h.ws != nil {
		h.ws.close()
	}
	if strings.TrimSpace(h.containerName) != "" {
		_, _ = dockerOutput("rm", "-f", h.containerName)
	}
	if strings.TrimSpace(h.rootDir) != "" {
		_ = os.RemoveAll(h.rootDir)
	}
}

func (h *wecomE2EHarness) containerStatus() string {
	if h == nil || strings.TrimSpace(h.containerName) == "" {
		return ""
	}
	output, err := dockerOutput(
		"inspect",
		"-f",
		"{{.State.Status}}",
		h.containerName,
	)
	if err != nil {
		return "missing"
	}
	return strings.TrimSpace(output)
}

func (h *wecomE2EHarness) debugSnapshot() string {
	var builder strings.Builder
	builder.WriteString("== docker inspect ==\n")
	builder.WriteString(strings.TrimSpace(h.inspectOutput()))
	builder.WriteString("\n\n== start.log ==\n")
	builder.WriteString(strings.TrimSpace(h.readStartLog()))
	builder.WriteString("\n\n== process ==\n")
	builder.WriteString(strings.TrimSpace(h.processOutput()))
	return builder.String()
}

func (h *wecomE2EHarness) inspectOutput() string {
	output, err := dockerOutput(
		"inspect",
		h.containerName,
		"--format",
		"status={{.State.Status}} running={{.State.Running}} exit={{.State.ExitCode}} started={{.State.StartedAt}} finished={{.State.FinishedAt}}",
	)
	if err != nil {
		return strings.TrimSpace(output)
	}
	return strings.TrimSpace(output)
}

func (h *wecomE2EHarness) processOutput() string {
	output, err := dockerOutput(
		"exec",
		h.containerName,
		"sh",
		"-lc",
		"ps -ef | sed -n '1,120p'",
	)
	if err != nil {
		return strings.TrimSpace(output)
	}
	return strings.TrimSpace(output)
}

func (h *wecomE2EHarness) readStartLog() string {
	output, err := dockerOutput(
		"exec",
		h.containerName,
		"sh",
		"-lc",
		"tail -n 200 "+wecomE2EContainerStartLogPath,
	)
	if err == nil {
		return output
	}
	tempDir, tempErr := os.MkdirTemp("", "openclaw-e2e-log-*")
	if tempErr == nil {
		defer os.RemoveAll(tempDir)
		localPath := filepath.Join(tempDir, "start.log")
		if copyOutput, copyErr := dockerOutput(
			"cp",
			h.containerName+":"+wecomE2EContainerStartLogPath,
			localPath,
		); copyErr == nil {
			raw, readErr := os.ReadFile(localPath)
			if readErr == nil {
				return string(raw)
			}
		} else if strings.TrimSpace(copyOutput) != "" {
			output = copyOutput
		}
	}
	logOutput, logErr := dockerOutput("logs", h.containerName)
	if logErr == nil && strings.TrimSpace(logOutput) != "" {
		return logOutput
	}
	return strings.TrimSpace(output)
}

func (h *wecomE2EHarness) readFile(t *testing.T, path string) string {
	t.Helper()
	output, err := dockerOutput(
		"exec",
		h.containerName,
		"sh",
		"-lc",
		`cat "$1"`,
		"sh",
		path,
	)
	require.NoError(t, err, h.debugSnapshot())
	return output
}

func (h *wecomE2EHarness) waitForFileContains(
	t *testing.T,
	path string,
	substring string,
	timeout time.Duration,
) {
	t.Helper()
	require.Eventually(t, func() bool {
		output, err := dockerOutput(
			"exec",
			h.containerName,
			"sh",
			"-lc",
			`cat "$1"`,
			"sh",
			path,
		)
		return err == nil && strings.Contains(output, substring)
	}, timeout, wecomE2EPollInterval, h.debugSnapshot())
}

func dockerOutput(args ...string) (string, error) {
	cmd := exec.Command("docker", args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}
