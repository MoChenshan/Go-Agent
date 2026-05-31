package runtimehint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBrowserPromptMode(t *testing.T) {
	t.Parallel()

	require.Equal(
		t,
		BrowserModeInteractive,
		BrowserPromptMode("", BrowserHeadlessDisabledValue),
	)
	require.Equal(
		t,
		BrowserModeHeadless,
		BrowserPromptMode("", BrowserHeadlessEnabledValue),
	)
	require.Equal(
		t,
		BrowserModeHeadless,
		BrowserPromptMode("", ""),
	)
	require.Equal(
		t,
		BrowserModeInteractive,
		BrowserPromptMode(BrowserModeInteractive, ""),
	)
}

func TestBrowserPromptLine(t *testing.T) {
	t.Parallel()

	got := BrowserPromptLine(
		"/tmp/trpc-claw-browser-runtime",
		BrowserModeHeadless,
		BrowserHeadlessEnabledValue,
	)

	require.Contains(t, got, BrowserRuntimeName)
	require.Contains(t, got, "Prefer `web_fetch`")
	require.Contains(t, got, "doctor")
	require.Contains(t, got, BrowserModeEnvName)
	require.Contains(t, got, BrowserPathEnvName)
	require.Contains(t, got, "mcp-stdio")
	require.Contains(t, got, BrowserModeHeadless)
	require.Contains(t, got, "Treat prior browser or MCP failures")
	require.Contains(t, got, "before concluding")
}

func TestBrowserPromptLineEmptyHelper(t *testing.T) {
	t.Parallel()

	require.Empty(t, BrowserPromptLine("", "", BrowserHeadlessEnabledValue))
}

func TestParseBrowserDoctorSnapshot(t *testing.T) {
	t.Parallel()

	snapshot := ParseBrowserDoctorSnapshot(
		"doctor_status=ready\n" +
			"lane=playwright_mcp_stdio\n" +
			"mode=headless\n" +
			"browser=chromium\n" +
			"browser_path=/usr/bin/chromium-browser\n" +
			"doctor_detail=ready\n",
	)

	require.Equal(t, "ready", snapshot.Status)
	require.Equal(t, "playwright_mcp_stdio", snapshot.Lane)
	require.Equal(t, BrowserModeHeadless, snapshot.Mode)
	require.Equal(t, BrowserNameChromium, snapshot.BrowserName)
	require.Equal(
		t,
		"/usr/bin/chromium-browser",
		snapshot.BrowserPath,
	)
	require.Equal(t, "ready", snapshot.Detail)
}

func TestBrowserDoctorPromptLineFromOutput(t *testing.T) {
	t.Parallel()

	got := BrowserDoctorPromptLineFromOutput(
		"doctor_status=ready\n" +
			"lane=playwright_mcp_stdio\n" +
			"mode=headless\n" +
			"browser=chromium\n" +
			"browser_path=/usr/bin/chromium-browser\n",
	)

	require.Contains(t, got, "Current turn browser runtime fact:")
	require.Contains(t, got, "doctor_status=ready")
	require.Contains(t, got, "lane=playwright_mcp_stdio")
	require.Contains(t, got, "browser=chromium")
	require.Contains(
		t,
		got,
		"attempt the browser tool instead of repeating",
	)
}
