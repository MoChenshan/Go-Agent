// Package runtimehint exposes shared runtime guidance helpers.
package runtimehint

import (
	"fmt"
	"os"
	"strings"
)

const (
	BrowserRuntimeEnvName  = "TRPC_CLAW_BROWSER_RUNTIME"
	BrowserMCPBinEnvName   = "TRPC_CLAW_BROWSER_MCP_BIN"
	BrowserModeEnvName     = "TRPC_CLAW_BROWSER_MODE"
	BrowserPathEnvName     = "TRPC_CLAW_BROWSER_PATH"
	BrowserHeadlessEnvName = "TRPC_CLAW_BROWSER_HEADLESS"
	BrowserNameEnvName     = "TRPC_CLAW_BROWSER_NAME"
	BrowserExecPathEnvName = "TRPC_CLAW_BROWSER_EXECUTABLE_PATH"

	PlaywrightMCPBinName = "playwright-mcp"
	BrowserRuntimeName   = "trpc-claw-browser-runtime"
	BrowserNameChromium  = "chromium"

	BrowserModeAuto = "auto"

	BrowserHeadlessEnabledValue  = "1"
	BrowserHeadlessDisabledValue = "0"
	BrowserModeHeadless          = "headless"
	BrowserModeInteractive       = "interactive"

	browserDoctorStatusKey = "doctor_status"
	browserDoctorDetailKey = "doctor_detail"
	browserDoctorLaneKey   = "lane"
	browserDoctorModeKey   = "mode"
	browserDoctorNameKey   = "browser"
	browserDoctorPathKey   = "browser_path"

	browserDoctorPromptPrefix = "Current turn browser runtime fact: "
	browserDoctorHistoryRule  = "Treat this fresh doctor result " +
		"as newer than older browser or MCP failures in " +
		"session history."
	browserDoctorReadyRule = "When the user requests browser " +
		"automation in this turn and doctor_status=ready, " +
		"attempt the browser tool instead of repeating an " +
		"earlier failure."
)

// BrowserDoctorSnapshot stores structured doctor facts for prompt use.
type BrowserDoctorSnapshot struct {
	Status      string
	Detail      string
	Lane        string
	Mode        string
	BrowserName string
	BrowserPath string
}

// BrowserPromptLineFromEnv returns runtime browser guidance when the
// managed browser helper is available.
func BrowserPromptLineFromEnv() string {
	return BrowserPromptLine(
		os.Getenv(BrowserRuntimeEnvName),
		os.Getenv(BrowserModeEnvName),
		os.Getenv(BrowserHeadlessEnvName),
	)
}

// BrowserPromptLine returns runtime browser guidance for prompts.
func BrowserPromptLine(
	helperPath string,
	modeValue string,
	headlessValue string,
) string {
	helperPath = strings.TrimSpace(helperPath)
	if helperPath == "" {
		return ""
	}
	mode := BrowserPromptMode(modeValue, headlessValue)
	return "Managed browser runtime: `" +
		BrowserRuntimeName + "` is available at " +
		helperPath + ". Prefer `web_fetch` for ordinary " +
		"articles, docs, and other plain page content. " +
		"Use browser automation only for JavaScript-rendered " +
		"pages, interactions, login-gated flows, downloads, " +
		"screenshots, visual verification, or local webapp " +
		"testing. Use `" + BrowserRuntimeName +
		" doctor` to inspect runtime health and `" +
		BrowserRuntimeName +
		" mcp-stdio` as the stable MCP command instead of " +
		"`npx @playwright/mcp@latest`. In custom environments, " +
		"prefer `" + BrowserModeEnvName + "` and `" +
		BrowserPathEnvName + "` as the main overrides. " +
		"Treat prior browser or MCP failures in chat history " +
		"as stale unless they happen again in this turn. " +
		"When a new turn requires browser automation, retry " +
		"the browser tool or run `" + BrowserRuntimeName +
		" doctor` in the current turn before concluding " +
		"that browser is unavailable. " +
		"This runtime defaults to " + mode +
		" browser automation, so do not " +
		"promise a visible browser window or manual takeover " +
		"unless the environment explicitly supports it."
}

// BrowserPromptMode returns the prompt-facing browser mode label.
func BrowserPromptMode(
	modeValue string,
	headlessValue string,
) string {
	switch strings.TrimSpace(strings.ToLower(modeValue)) {
	case BrowserModeInteractive:
		return BrowserModeInteractive
	case BrowserModeHeadless:
		return BrowserModeHeadless
	}
	switch strings.TrimSpace(headlessValue) {
	case BrowserHeadlessDisabledValue:
		return BrowserModeInteractive
	default:
		return BrowserModeHeadless
	}
}

// ParseBrowserDoctorSnapshot parses key=value doctor output.
func ParseBrowserDoctorSnapshot(output string) BrowserDoctorSnapshot {
	snapshot := BrowserDoctorSnapshot{}
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(
			strings.TrimSpace(line),
			"=",
		)
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case browserDoctorStatusKey:
			snapshot.Status = strings.TrimSpace(value)
		case browserDoctorDetailKey:
			snapshot.Detail = strings.TrimSpace(value)
		case browserDoctorLaneKey:
			snapshot.Lane = strings.TrimSpace(value)
		case browserDoctorModeKey:
			snapshot.Mode = strings.TrimSpace(value)
		case browserDoctorNameKey:
			snapshot.BrowserName = strings.TrimSpace(value)
		case browserDoctorPathKey:
			snapshot.BrowserPath = strings.TrimSpace(value)
		default:
			continue
		}
	}
	return snapshot
}

// BrowserDoctorPromptLineFromOutput formats doctor output for prompts.
func BrowserDoctorPromptLineFromOutput(output string) string {
	snapshot := ParseBrowserDoctorSnapshot(output)
	if strings.TrimSpace(snapshot.Status) == "" {
		return ""
	}
	parts := make([]string, 0, 6)
	parts = append(
		parts,
		fmt.Sprintf("%s=%s", browserDoctorStatusKey, snapshot.Status),
	)
	if snapshot.Lane != "" {
		parts = append(
			parts,
			fmt.Sprintf("%s=%s", browserDoctorLaneKey, snapshot.Lane),
		)
	}
	if snapshot.Mode != "" {
		parts = append(
			parts,
			fmt.Sprintf("%s=%s", browserDoctorModeKey, snapshot.Mode),
		)
	}
	if snapshot.BrowserName != "" {
		parts = append(
			parts,
			fmt.Sprintf("%s=%s", browserDoctorNameKey, snapshot.BrowserName),
		)
	}
	if snapshot.BrowserPath != "" {
		parts = append(
			parts,
			fmt.Sprintf("%s=%s", browserDoctorPathKey, snapshot.BrowserPath),
		)
	}
	if snapshot.Detail != "" &&
		snapshot.Detail != snapshot.Status {
		parts = append(
			parts,
			fmt.Sprintf("%s=%s", browserDoctorDetailKey, snapshot.Detail),
		)
	}
	suffix := browserDoctorHistoryRule
	if snapshot.Status == "ready" {
		suffix += " " + browserDoctorReadyRule
	}
	return browserDoctorPromptPrefix +
		strings.Join(parts, ", ") + ". " + suffix
}
