package main

import (
	"bytes"
	_ "embed"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"git.woa.com/trpc-go/trpc-agent-go/openclaw/runtimehint"
)

const (
	runtimeDocHelperEnvName       = "TRPC_CLAW_DOC_HELPER"
	runtimeBrowserRuntimeEnvName  = runtimehint.BrowserRuntimeEnvName
	runtimeBrowserMCPBinEnvName   = runtimehint.BrowserMCPBinEnvName
	runtimeBrowserModeEnvName     = runtimehint.BrowserModeEnvName
	runtimeBrowserPathEnvName     = runtimehint.BrowserPathEnvName
	runtimeBrowserHeadlessEnvName = runtimehint.BrowserHeadlessEnvName
	runtimeBrowserNameEnvName     = runtimehint.BrowserNameEnvName
	runtimeBrowserExecPathEnvName = runtimehint.BrowserExecPathEnvName
	runtimeToolchainDirEnvName    = "TRPC_CLAW_TOOLCHAIN_DIR"
	runtimeFontsDirEnvName        = "TRPC_CLAW_FONT_DIR"
	runtimeTessdataDirEnvName     = "TRPC_CLAW_TESSDATA_DIR"
	runtimeManagedPythonEnvName   = "OPENCLAW_TOOLCHAIN_PYTHON"
	runtimeToolsDirName           = "tools"
	runtimeToolchainDirName       = "toolchain"
	runtimeToolchainBinName       = "bin"
	runtimeFontsDirName           = "fonts"
	runtimeTessdataDirName        = "tessdata"
	runtimePythonDirName          = "python"
	runtimePythonBinName          = "bin"
	runtimePythonExecName         = "python3"
	runtimeLegacyPythonExecName   = "python"
	runtimePlaywrightDirName      = "playwright"
	runtimePlaywrightMCPBinName   = runtimehint.PlaywrightMCPBinName
	runtimePlaywrightMCPPackage   = "@playwright/mcp@0.0.69"
	runtimeDocHelperName          = "trpc-claw-doc-helper"
	runtimeBrowserRuntimeName     = runtimehint.BrowserRuntimeName
	runtimeShellEnvScriptName     = "trpc-claw-runtime-env.sh"
	runtimeBashWrapperName        = "bash"
	runtimeShWrapperName          = "sh"
	runtimeBrowserNameChromium    = runtimehint.BrowserNameChromium
	runtimeDocHelperPython        = "trpc_claw_doc_helper.py"
	runtimeSupportFilePerm        = 0o755
	runtimeSupportPrivateFilePerm = 0o600
	runtimeSupportDirPerm         = 0o755
	runtimeShellScriptPath        = "/bin/sh"

	runtimeShellInternalEnvName       = "_"
	runtimeShellPWDEnvName            = "PWD"
	runtimeShellOldPWDEnvName         = "OLDPWD"
	runtimeShellSHLVLEnvName          = "SHLVL"
	runtimeShellPPIDEnvName           = "PPID"
	runtimeShellUIDEnvName            = "UID"
	runtimeShellEUIDEnvName           = "EUID"
	runtimeShellIFSName               = "IFS"
	runtimeShellOPTINDEnvName         = "OPTIND"
	runtimeShellSHELLOPTSEnvName      = "SHELLOPTS"
	runtimeShellBashOptsEnvName       = "BASHOPTS"
	runtimeShellBashCommandEnvName    = "BASH_COMMAND"
	runtimeShellBashExecStringEnvName = "BASH_EXECUTION_STRING"
	runtimeShellBashLineNoEnvName     = "BASH_LINENO"
	runtimeShellLineNoEnvName         = "LINENO"
	runtimeShellRandomEnvName         = "RANDOM"
	runtimeShellSecondsEnvName        = "SECONDS"

	runtimePlaywrightBrowsersEnvName      = "PLAYWRIGHT_BROWSERS_PATH"
	runtimeOpenClawBrowserHeadlessEnvName = "OPENCLAW_BROWSER_HEADLESS"
	runtimeOpenClawBrowserExecPathEnvName = "OPENCLAW_BROWSER_EXECUTABLE_PATH"
	runtimeBrowserLanePlaywrightMCP       = "playwright_mcp_stdio"
	runtimeDoctorStatusReady              = "ready"
	runtimeDoctorStatusDegraded           = "degraded"
	runtimeDoctorStatusUnavailable        = "unavailable"
	runtimeBrowserExecChromium            = "chromium"
	runtimeBrowserExecChromiumBrowser     = "chromium-browser"
	runtimeBrowserExecGoogleChrome        = "google-chrome"
	runtimeBrowserExecChromeStable        = "google-chrome-stable"
	runtimeBrowserExecChromiumMac         = "/Applications/Chromium.app" +
		"/Contents/MacOS/Chromium"
	runtimeBrowserExecGoogleChromeMac = "/Applications/Google Chrome.app" +
		"/Contents/MacOS/Google Chrome"

	runtimeBrowserModeAuto             = runtimehint.BrowserModeAuto
	runtimeBrowserHeadlessEnabledValue = runtimehint.
						BrowserHeadlessEnabledValue
	runtimeBrowserHeadlessDisabledValue = runtimehint.
						BrowserHeadlessDisabledValue
	runtimeBrowserModeHeadless    = runtimehint.BrowserModeHeadless
	runtimeBrowserModeInteractive = runtimehint.
					BrowserModeInteractive
)

var runtimeBrowserExecutableCandidates = []string{
	runtimeBrowserExecChromium,
	runtimeBrowserExecChromiumBrowser,
	runtimeBrowserExecGoogleChrome,
	runtimeBrowserExecChromeStable,
}

var runtimeBrowserAbsoluteExecutableCandidates = []string{
	runtimeBrowserExecChromiumMac,
	runtimeBrowserExecGoogleChromeMac,
}

var (
	//go:embed runtime_assets/trpc_claw_doc_helper.py
	runtimeDocHelperScript string
)

type runtimeSupportAssets struct {
	DocHelperPath      string
	BrowserRuntimePath string
	ShellEnvPath       string
	BashWrapperPath    string
	ShWrapperPath      string
}

var runtimeShellReservedEnvNames = map[string]struct{}{
	runtimeShellInternalEnvName:       {},
	runtimeShellPWDEnvName:            {},
	runtimeShellOldPWDEnvName:         {},
	runtimeShellSHLVLEnvName:          {},
	runtimeShellPPIDEnvName:           {},
	runtimeShellUIDEnvName:            {},
	runtimeShellEUIDEnvName:           {},
	runtimeShellIFSName:               {},
	runtimeShellOPTINDEnvName:         {},
	runtimeShellSHELLOPTSEnvName:      {},
	runtimeShellBashOptsEnvName:       {},
	runtimeShellBashCommandEnvName:    {},
	runtimeShellBashExecStringEnvName: {},
	runtimeShellBashLineNoEnvName:     {},
	runtimeShellLineNoEnvName:         {},
	runtimeShellRandomEnvName:         {},
	runtimeShellSecondsEnvName:        {},
}

func runtimeToolsDir(stateDir string) string {
	return runtimeStateSubdir(stateDir, runtimeToolsDirName)
}

func runtimeToolchainDir(stateDir string) string {
	return runtimeStateSubdir(stateDir, runtimeToolchainDirName)
}

func runtimeToolchainBinDir(stateDir string) string {
	return runtimeToolchainBinDirFromRoot(
		runtimeToolchainDir(stateDir),
	)
}

func runtimeToolchainBinDirFromRoot(root string) string {
	return runtimeToolchainSubdir(
		root,
		runtimeToolchainBinName,
	)
}

func runtimeFontsDir(stateDir string) string {
	return runtimeFontsDirFromRoot(
		runtimeToolchainDir(stateDir),
	)
}

func runtimeFontsDirFromRoot(root string) string {
	return runtimeToolchainSubdir(
		root,
		runtimeFontsDirName,
	)
}

func runtimeTessdataDir(stateDir string) string {
	return runtimeTessdataDirFromRoot(
		runtimeToolchainDir(stateDir),
	)
}

func runtimeTessdataDirFromRoot(root string) string {
	return runtimeToolchainSubdir(
		root,
		runtimeTessdataDirName,
	)
}

func runtimeManagedPythonRoot(stateDir string) string {
	return runtimeManagedPythonRootFromRoot(
		runtimeToolchainDir(stateDir),
	)
}

func runtimeManagedPythonRootFromRoot(root string) string {
	return runtimeToolchainSubdir(
		root,
		runtimePythonDirName,
	)
}

func runtimeManagedPythonBinDir(stateDir string) string {
	return runtimeManagedPythonBinDirFromRoot(
		runtimeToolchainDir(stateDir),
	)
}

func runtimeManagedPythonBinDirFromRoot(root string) string {
	return runtimeToolchainSubdir(
		runtimeManagedPythonRootFromRoot(root),
		runtimePythonBinName,
	)
}

func runtimeManagedPythonPath(stateDir string) string {
	return runtimeManagedPythonPathFromRoot(
		runtimeToolchainDir(stateDir),
	)
}

func runtimeManagedPythonPathFromRoot(root string) string {
	binDir := runtimeManagedPythonBinDirFromRoot(root)
	if binDir == "" {
		return ""
	}
	return filepath.Join(
		binDir,
		runtimePythonExecName,
	)
}

func runtimeToolchainSubdir(
	root string,
	name string,
) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	return filepath.Join(root, name)
}

func runtimeStateSubdir(
	stateDir string,
	name string,
) string {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return ""
	}
	return filepath.Join(stateDir, name)
}

func runtimeShellEnvPath(stateDir string) string {
	return runtimeStateSubdir(stateDir, filepath.Join(
		runtimeToolsDirName,
		runtimeShellEnvScriptName,
	))
}

func ensureRuntimeSupportAssets(
	stateDir string,
) runtimeSupportAssets {
	var assets runtimeSupportAssets
	dirs := []string{
		runtimeToolsDir(stateDir),
		runtimeToolchainDir(stateDir),
		runtimeToolchainBinDir(stateDir),
		runtimeFontsDir(stateDir),
		runtimeTessdataDir(stateDir),
	}
	if strings.TrimSpace(stateDir) == "" {
		return assets
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, runtimeSupportDirPerm); err != nil {
			return runtimeSupportAssets{}
		}
	}

	toolsDir := runtimeToolsDir(stateDir)
	shellEnvPath := filepath.Join(
		toolsDir,
		runtimeShellEnvScriptName,
	)
	if err := writeRuntimeSupportFileMode(
		shellEnvPath,
		runtimeShellEnvPlaceholderContent(),
		runtimeSupportPrivateFilePerm,
	); err != nil {
		return runtimeSupportAssets{}
	}
	bashWrapperPath := filepath.Join(
		toolsDir,
		runtimeBashWrapperName,
	)
	if err := writeRuntimeSupportFile(
		bashWrapperPath,
		runtimeBashWrapperContent(),
	); err != nil {
		return runtimeSupportAssets{}
	}
	shWrapperPath := filepath.Join(
		toolsDir,
		runtimeShWrapperName,
	)
	if err := writeRuntimeSupportFile(
		shWrapperPath,
		runtimeShWrapperContent(),
	); err != nil {
		return runtimeSupportAssets{}
	}
	docHelperPath := filepath.Join(toolsDir, runtimeDocHelperName)
	if err := writeRuntimeSupportFile(
		docHelperPath,
		runtimeDocHelperWrapperContent(),
	); err != nil {
		return runtimeSupportAssets{}
	}

	scriptPath := filepath.Join(toolsDir, runtimeDocHelperPython)
	if err := writeRuntimeSupportFile(
		scriptPath,
		runtimeDocHelperScript,
	); err != nil {
		return runtimeSupportAssets{}
	}
	browserRuntimePath := filepath.Join(
		toolsDir,
		runtimeBrowserRuntimeName,
	)
	if err := writeRuntimeSupportFile(
		browserRuntimePath,
		runtimeBrowserRuntimeContent(),
	); err != nil {
		return runtimeSupportAssets{}
	}
	assets.ShellEnvPath = shellEnvPath
	assets.BashWrapperPath = bashWrapperPath
	assets.ShWrapperPath = shWrapperPath
	assets.DocHelperPath = docHelperPath
	assets.BrowserRuntimePath = browserRuntimePath
	return assets
}

func runtimeShellEnvPlaceholderContent() string {
	return strings.Join([]string{
		"# Managed by trpc-claw.",
		"# Runtime shell exports are refreshed during startup.",
		"",
	}, "\n")
}

func runtimeBashWrapperContent() string {
	return runtimeShellWrapperContent(
		runtimeBashWrapperName,
		[]string{"/bin/bash", "/usr/bin/bash"},
	)
}

func runtimeShWrapperContent() string {
	return runtimeShellWrapperContent(
		runtimeShWrapperName,
		[]string{"/bin/sh", "/usr/bin/sh"},
	)
}

func runtimeShellWrapperContent(
	shellName string,
	realShellCandidates []string,
) string {
	lines := []string{
		"#!" + runtimeShellScriptPath,
		"set -eu",
		"",
		`SELF_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)`,
		`ENV_FILE_DEFAULT="$SELF_DIR/` + runtimeShellEnvScriptName + `"`,
		`ENV_FILE="${` + runtimeShellEnvFileEnvName +
			`:-$ENV_FILE_DEFAULT}"`,
		"",
		"load_runtime_env() {",
		"  if [ -f \"$ENV_FILE\" ]; then",
		"    # shellcheck disable=SC1090",
		"    . \"$ENV_FILE\"",
		"  fi",
		"}",
		"",
		"resolve_real_shell() {",
	}
	for _, candidate := range realShellCandidates {
		lines = append(
			lines,
			"  if [ -x \""+candidate+"\" ]; then",
			"    printf '%s\\n' \""+candidate+"\"",
			"    return 0",
			"  fi",
		)
	}
	lines = append(
		lines,
		"  printf '%s\\n' \""+shellName+" is required\" >&2",
		"  return 1",
		"}",
		"",
		"load_runtime_env",
		"export "+runtimeShellEnvFileEnvName+"=\"$ENV_FILE\"",
		"export "+runtimeBashEnvName+"=\"$ENV_FILE\"",
		"export "+runtimePosixShellEnvName+"=\"$ENV_FILE\"",
		"case \"${1:-}\" in",
		"  -c|-lc)",
		"    MODE=\"$1\"",
		"    shift",
		"    CMD=\"\"",
		"    if [ \"$#\" -gt 0 ]; then",
		"      CMD=\"$1\"",
		"      shift",
		"    fi",
		"    PREFIX=\". \\\"$ENV_FILE\\\" >/dev/null 2>&1 || true\"",
		"    if [ -n \"$CMD\" ]; then",
		"      CMD=\"$PREFIX",
		"$CMD\"",
		"    else",
		"      CMD=\"$PREFIX\"",
		"    fi",
		"    set -- \"$MODE\" \"$CMD\" \"$@\"",
		"    ;;",
		"esac",
		"REAL_SHELL=$(resolve_real_shell)",
		`exec "$REAL_SHELL" "$@"`,
	)
	return strings.Join(lines, "\n") + "\n"
}

func runtimeDocHelperWrapperContent() string {
	lines := []string{
		"#!/usr/bin/env sh",
		"set -eu",
		"",
		`SELF_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)`,
		`SCRIPT_PATH="$SELF_DIR/` + runtimeDocHelperPython + `"`,
		"",
		"find_python() {",
		"  if [ -n \"${" + runtimeManagedPythonEnvName + ":-}\" ] &&",
		"    [ -x \"${" + runtimeManagedPythonEnvName +
			"}\" ]; then",
		"    printf '%s\\n' \"${" + runtimeManagedPythonEnvName +
			"}\"",
		"    return 0",
		"  fi",
		"",
		"  if [ -n \"${" + runtimeStateDirEnvName + ":-}\" ]; then",
		"    MANAGED_PYTHON=\"" +
			runtimeManagedPythonShellPath() + "\"",
		"    if [ -x \"$MANAGED_PYTHON\" ]; then",
		"      printf '%s\\n' \"$MANAGED_PYTHON\"",
		"      return 0",
		"    fi",
		"  fi",
		"",
		"  if command -v " + runtimePythonExecName +
			" >/dev/null 2>&1; then",
		"    command -v " + runtimePythonExecName,
		"    return 0",
		"  fi",
		"  if command -v " + runtimeLegacyPythonExecName +
			" >/dev/null 2>&1; then",
		"    command -v " + runtimeLegacyPythonExecName,
		"    return 0",
		"  fi",
		"",
		"  printf '%s\\n' \"" + runtimePythonExecName + " or " +
			runtimeLegacyPythonExecName + " is required\" >&2",
		"  return 1",
		"}",
		"",
		"PYTHON_BIN=$(find_python)",
		`exec "$PYTHON_BIN" "$SCRIPT_PATH" "$@"`,
	}
	return strings.Join(lines, "\n") + "\n"
}

func runtimeManagedPythonShellPath() string {
	return "${" + runtimeStateDirEnvName + "}/" +
		runtimeToolchainDirName + "/" +
		runtimePythonDirName + "/" +
		runtimePythonBinName + "/" +
		runtimePythonExecName
}

func runtimeManagedBrowserMCPPath(stateDir string) string {
	return runtimeManagedBrowserMCPPathFromRoot(
		runtimeToolchainDir(stateDir),
	)
}

func runtimeManagedBrowserMCPPathFromRoot(root string) string {
	binDir := runtimeManagedPythonBinDirFromRoot(root)
	if binDir == "" {
		return ""
	}
	return filepath.Join(binDir, runtimePlaywrightMCPBinName)
}

func runtimeLegacyManagedBrowserMCPPath(stateDir string) string {
	return runtimeLegacyManagedBrowserMCPPathFromRoot(
		runtimeToolchainDir(stateDir),
	)
}

func runtimeLegacyManagedBrowserMCPPathFromRoot(root string) string {
	binDir := runtimeToolchainBinDirFromRoot(root)
	if binDir == "" {
		return ""
	}
	return filepath.Join(binDir, runtimePlaywrightMCPBinName)
}

func runtimeManagedBrowserMCPShellPath() string {
	return "${" + runtimeStateDirEnvName + "}/" +
		runtimeToolchainDirName + "/" +
		runtimePythonDirName + "/" +
		runtimePythonBinName + "/" +
		runtimePlaywrightMCPBinName
}

func runtimeLegacyManagedBrowserMCPShellPath() string {
	return "${" + runtimeStateDirEnvName + "}/" +
		runtimeToolchainDirName + "/" +
		runtimeToolchainBinName + "/" +
		runtimePlaywrightMCPBinName
}

func runtimeBrowserExecutableLookupLines() []string {
	lines := make([]string, 0, len(runtimeBrowserExecutableCandidates)*4)
	for _, name := range runtimeBrowserExecutableCandidates {
		lines = append(
			lines,
			"  if command -v "+name+" >/dev/null 2>&1; then",
			"    BROWSER_EXEC=\"$(command -v "+name+")\"",
			"    BROWSER_PATH_SOURCE=\"path:"+name+"\"",
			"    return 0",
			"  fi",
		)
	}
	return lines
}

func runtimeBrowserAbsoluteExecutableLookupLines() []string {
	lines := make(
		[]string,
		0,
		len(runtimeBrowserAbsoluteExecutableCandidates)*4,
	)
	for _, path := range runtimeBrowserAbsoluteExecutableCandidates {
		lines = append(
			lines,
			"  if [ -x \""+path+"\" ]; then",
			"    BROWSER_EXEC=\""+path+"\"",
			"    BROWSER_PATH_SOURCE=\"absolute:"+path+"\"",
			"    return 0",
			"  fi",
		)
	}
	return lines
}

func writeRuntimeSupportFile(path string, content string) error {
	return writeRuntimeSupportFileMode(
		path,
		content,
		runtimeSupportFilePerm,
	)
}

func writeRuntimeSupportFileMode(
	path string,
	content string,
	mode os.FileMode,
) error {
	current, err := os.ReadFile(path)
	if err == nil && bytes.Equal(current, []byte(content)) {
		return os.Chmod(path, mode)
	}
	return os.WriteFile(
		path,
		[]byte(content),
		mode,
	)
}

func runtimeShellEnvContent(environ []string) string {
	values := make(map[string]string, len(environ))
	for _, entry := range environ {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || !runtimeShellEnvNameAllowed(name) {
			continue
		}
		values[name] = value
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	lines := []string{
		"# Managed by trpc-claw.",
		"# Sourced by runtime shell wrappers to restore env.",
	}
	for _, name := range names {
		lines = append(
			lines,
			"export "+name+"="+runtimeShellQuote(values[name]),
		)
	}
	return strings.Join(lines, "\n") + "\n"
}

func runtimeShellEnvNameAllowed(name string) bool {
	if !shellIdentifier(name) {
		return false
	}
	_, reserved := runtimeShellReservedEnvNames[name]
	return !reserved
}

func shellIdentifier(name string) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}
	for i, r := range name {
		if i == 0 {
			if r == '_' ||
				(r >= 'A' && r <= 'Z') ||
				(r >= 'a' && r <= 'z') {
				continue
			}
			return false
		}
		if r == '_' ||
			(r >= 'A' && r <= 'Z') ||
			(r >= 'a' && r <= 'z') ||
			(r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

func runtimeShellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func runtimeDocHelperPromptLine() string {
	helperPath := strings.TrimSpace(
		os.Getenv(runtimeDocHelperEnvName),
	)
	if helperPath == "" {
		return ""
	}
	return "Runtime document helper: `" +
		runtimeDocHelperName + "` is available at " +
		helperPath + ". Use `" + runtimeDocHelperName +
		" probe` to inspect PDF/OCR/CJK capabilities, `" +
		runtimeDocHelperName +
		" ensure-python <package...>` to add managed Python " +
		"packages, `" + runtimeDocHelperName +
		" ensure-fonts` to download managed CJK fonts, `" +
		runtimeDocHelperName +
		" ensure-tessdata <lang...>` to download OCR " +
		"language data, and `" + runtimeDocHelperName +
		" verify-pdf --path <file> --expect-cjk` before " +
		"sending Chinese or other CJK PDFs."
}

func runtimeBrowserRuntimeContent() string {
	lines := []string{
		"#!/usr/bin/env sh",
		"set -eu",
		"",
		"PLAYWRIGHT_MCP_SOURCE=missing",
		"BROWSER_MODE_SOURCE=missing",
		"BROWSER_NAME_SOURCE=missing",
		"BROWSER_PATH_SOURCE=missing",
		"MCP_BIN=",
		"MODE=",
		"BROWSER_NAME=",
		"BROWSER_EXEC=",
		"BROWSER_FIND_ERROR=",
		"",
		"find_playwright_mcp() {",
		"  if [ -n \"${" + runtimeBrowserMCPBinEnvName +
			":-}\" ] &&",
		"    [ -x \"${" + runtimeBrowserMCPBinEnvName +
			"}\" ]; then",
		"    MCP_BIN=\"${" + runtimeBrowserMCPBinEnvName + "}\"",
		"    PLAYWRIGHT_MCP_SOURCE=\"" +
			runtimeBrowserMCPBinEnvName + "\"",
		"    return 0",
		"  fi",
		"",
		"  if [ -n \"${" + runtimeStateDirEnvName + ":-}\" ]; then",
		"    MANAGED_MCP=\"" +
			runtimeManagedBrowserMCPShellPath() + "\"",
		"    if [ -x \"$MANAGED_MCP\" ]; then",
		"      MCP_BIN=\"$MANAGED_MCP\"",
		"      PLAYWRIGHT_MCP_SOURCE=\"managed\"",
		"      return 0",
		"    fi",
		"    LEGACY_MANAGED_MCP=\"" +
			runtimeLegacyManagedBrowserMCPShellPath() + "\"",
		"    if [ -x \"$LEGACY_MANAGED_MCP\" ]; then",
		"      MCP_BIN=\"$LEGACY_MANAGED_MCP\"",
		"      PLAYWRIGHT_MCP_SOURCE=\"managed-legacy\"",
		"      return 0",
		"    fi",
		"  fi",
		"",
		"  if command -v " + runtimePlaywrightMCPBinName +
			" >/dev/null 2>&1; then",
		"    MCP_BIN=$(command -v " + runtimePlaywrightMCPBinName + ")",
		"    PLAYWRIGHT_MCP_SOURCE=\"path:" +
			runtimePlaywrightMCPBinName + "\"",
		"    return 0",
		"  fi",
		"",
		"  printf '%s\\n' \"" + runtimePlaywrightMCPBinName +
			" is required\" >&2",
		"  return 1",
		"}",
		"",
		"detected_browser_mode() {",
		"  if [ -n \"${DISPLAY:-}\" ] ||",
		"    [ -n \"${WAYLAND_DISPLAY:-}\" ]; then",
		"    BROWSER_MODE_SOURCE=\"display\"",
		"    MODE=\"" + runtimeBrowserModeInteractive + "\"",
		"    return 0",
		"  fi",
		"  if [ -f /.dockerenv ] || [ -f /run/.containerenv ]; then",
		"    BROWSER_MODE_SOURCE=\"container\"",
		"    MODE=\"" + runtimeBrowserModeHeadless + "\"",
		"    return 0",
		"  fi",
		"  case \"$(uname -s 2>/dev/null || printf unknown)\" in",
		"    Darwin)",
		"      BROWSER_MODE_SOURCE=\"os:darwin\"",
		"      MODE=\"" + runtimeBrowserModeInteractive + "\"",
		"      ;;",
		"    *)",
		"      BROWSER_MODE_SOURCE=\"default\"",
		"      MODE=\"" + runtimeBrowserModeHeadless + "\"",
		"      ;;",
		"  esac",
		"}",
		"",
		"browser_mode() {",
		"  case \"${" + runtimeBrowserModeEnvName + ":-}\" in",
		"    " + runtimeBrowserModeHeadless + "|" +
			runtimeBrowserModeInteractive + ")",
		"      BROWSER_MODE_SOURCE=\"" +
			runtimeBrowserModeEnvName + "\"",
		"      MODE=\"${" + runtimeBrowserModeEnvName + "}\"",
		"      return 0",
		"      ;;",
		"    " + runtimeBrowserModeAuto + ")",
		"      detected_browser_mode",
		"      return 0",
		"      ;;",
		"  esac",
		"  case \"${" + runtimeBrowserHeadlessEnvName + ":-}\" in",
		"    " + runtimeBrowserHeadlessDisabledValue +
			"|false|FALSE|no|NO)",
		"      BROWSER_MODE_SOURCE=\"" +
			runtimeBrowserHeadlessEnvName + "\"",
		"      MODE=\"" + runtimeBrowserModeInteractive + "\"",
		"      return 0",
		"      ;;",
		"    " + runtimeBrowserHeadlessEnabledValue +
			"|true|TRUE|yes|YES)",
		"      BROWSER_MODE_SOURCE=\"" +
			runtimeBrowserHeadlessEnvName + "\"",
		"      MODE=\"" + runtimeBrowserModeHeadless + "\"",
		"      return 0",
		"      ;;",
		"  esac",
		"  case \"${" + runtimeOpenClawBrowserHeadlessEnvName +
			":-}\" in",
		"    " + runtimeBrowserHeadlessDisabledValue +
			"|false|FALSE|no|NO)",
		"      BROWSER_MODE_SOURCE=\"" +
			runtimeOpenClawBrowserHeadlessEnvName + "\"",
		"      MODE=\"" + runtimeBrowserModeInteractive + "\"",
		"      return 0",
		"      ;;",
		"    *)",
		"      if [ -n \"${" + runtimeOpenClawBrowserHeadlessEnvName +
			":-}\" ]; then",
		"        BROWSER_MODE_SOURCE=\"" +
			runtimeOpenClawBrowserHeadlessEnvName + "\"",
		"        MODE=\"" + runtimeBrowserModeHeadless + "\"",
		"        return 0",
		"      fi",
		"      ;;",
		"  esac",
		"  detected_browser_mode",
		"}",
		"",
		"find_browser_name() {",
		"  if [ -n \"${" + runtimeBrowserNameEnvName +
			":-}\" ]; then",
		"    BROWSER_NAME=\"${" + runtimeBrowserNameEnvName + "}\"",
		"    BROWSER_NAME_SOURCE=\"" +
			runtimeBrowserNameEnvName + "\"",
		"    return 0",
		"  fi",
		"  BROWSER_NAME_SOURCE=\"default\"",
		"  BROWSER_NAME=\"" + runtimeBrowserNameChromium + "\"",
		"}",
		"",
		"find_browser_executable_from_root() {",
		"  ROOT=$1",
		"  SOURCE=$2",
		"  [ -n \"$ROOT\" ] || return 1",
		"  for CANDIDATE in \\",
		"    \"$ROOT\"/chromium-*/chrome-linux64/chrome \\",
		"    \"$ROOT\"/chromium-*/chrome-linux/chrome \\",
		"    \"$ROOT\"/chromium-*/chrome-mac/Chromium.app/Contents/MacOS/Chromium; do",
		"    if [ -x \"$CANDIDATE\" ]; then",
		"      BROWSER_EXEC=\"$CANDIDATE\"",
		"      BROWSER_PATH_SOURCE=\"$SOURCE\"",
		"      return 0",
		"    fi",
		"  done",
		"  return 1",
		"}",
		"",
		"find_browser_executable() {",
		"  if [ -n \"${" + runtimeBrowserPathEnvName + ":-}\" ]; then",
		"    if [ -x \"${" + runtimeBrowserPathEnvName + "}\" ]; then",
		"      BROWSER_EXEC=\"${" + runtimeBrowserPathEnvName + "}\"",
		"      BROWSER_PATH_SOURCE=\"" +
			runtimeBrowserPathEnvName + "\"",
		"      return 0",
		"    fi",
		"    printf '%s\\n' \"" + runtimeBrowserPathEnvName +
			" points to a non-executable path\" >&2",
		"    return 1",
		"  fi",
		"",
		"  if [ -n \"${" + runtimeBrowserExecPathEnvName +
			":-}\" ]; then",
		"    if [ -x \"${" + runtimeBrowserExecPathEnvName +
			"}\" ]; then",
		"      BROWSER_EXEC=\"${" +
			runtimeBrowserExecPathEnvName + "}\"",
		"      BROWSER_PATH_SOURCE=\"" +
			runtimeBrowserExecPathEnvName + "\"",
		"      return 0",
		"    fi",
		"    printf '%s\\n' \"" + runtimeBrowserExecPathEnvName +
			" points to a non-executable path\" >&2",
		"    return 1",
		"  fi",
		"",
		"  if [ -n \"${" + runtimeOpenClawBrowserExecPathEnvName +
			":-}\" ]; then",
		"    if [ -x \"${" + runtimeOpenClawBrowserExecPathEnvName +
			"}\" ]; then",
		"      BROWSER_EXEC=\"${" +
			runtimeOpenClawBrowserExecPathEnvName + "}\"",
		"      BROWSER_PATH_SOURCE=\"" +
			runtimeOpenClawBrowserExecPathEnvName + "\"",
		"      return 0",
		"    fi",
		"    printf '%s\\n' \"" +
			runtimeOpenClawBrowserExecPathEnvName +
			" points to a non-executable path\" >&2",
		"    return 1",
		"  fi",
		"",
	}
	lines = append(lines, runtimeBrowserExecutableLookupLines()...)
	lines = append(lines, runtimeBrowserAbsoluteExecutableLookupLines()...)
	lines = append(
		lines,
		"  if [ -n \"${"+runtimePlaywrightBrowsersEnvName+
			":-}\" ]; then",
		"    if find_browser_executable_from_root \"${"+
			runtimePlaywrightBrowsersEnvName+"}\" \""+
			runtimePlaywrightBrowsersEnvName+"\"; then",
		"      return 0",
		"    fi",
		"  fi",
		"  if [ -n \"${"+runtimeStateDirEnvName+":-}\" ]; then",
		"    if find_browser_executable_from_root \"${"+
			runtimeStateDirEnvName+"}/"+
			runtimePlaywrightDirName+"\" "+
			"\"managed-playwright\"; then",
		"      return 0",
		"    fi",
		"  fi",
		"  return 1",
		"}",
		"",
		"print_runtime_summary() {",
		"  printf '%s\\n' \"browser_runtime="+
			runtimeBrowserRuntimeName+"\"",
		"  printf '%s\\n' \"lane="+
			runtimeBrowserLanePlaywrightMCP+"\"",
		"  printf '%s\\n' \"mode=$MODE\"",
		"  printf '%s\\n' \"mode_source=$BROWSER_MODE_SOURCE\"",
		"  printf '%s\\n' \"playwright_mcp=${MCP_BIN:-missing}\"",
		"  printf '%s\\n' "+
			"\"playwright_mcp_source=$PLAYWRIGHT_MCP_SOURCE\"",
		"  printf '%s\\n' \"browser=$BROWSER_NAME\"",
		"  printf '%s\\n' \"browser_source=$BROWSER_NAME_SOURCE\"",
		"  printf '%s\\n' \"browser_path=${BROWSER_EXEC:-missing}\"",
		"  printf '%s\\n' "+
			"\"browser_path_source=$BROWSER_PATH_SOURCE\"",
		"  if command -v node >/dev/null 2>&1; then",
		"    NODE_BIN=$(command -v node)",
		"  else",
		"    NODE_BIN=missing",
		"  fi",
		"  printf '%s\\n' \"node=$NODE_BIN\"",
		"  printf '%s\\n' \"fetch_first=true\"",
		"}",
		"",
		"resolve_runtime_summary() {",
		"  MCP_BIN=",
		"  MODE=",
		"  BROWSER_NAME=",
		"  BROWSER_EXEC=",
		"  BROWSER_FIND_ERROR=",
		"  PLAYWRIGHT_MCP_SOURCE=missing",
		"  BROWSER_MODE_SOURCE=missing",
		"  BROWSER_NAME_SOURCE=missing",
		"  BROWSER_PATH_SOURCE=missing",
		"  find_playwright_mcp 2>/dev/null || true",
		"  browser_mode",
		"  find_browser_name",
		"  ERR_FILE=$(mktemp 2>/dev/null || printf '%s' "+
			"\"${TMPDIR:-/tmp}/trpc-claw-browser.err\")",
		"  if ! find_browser_executable 2>\"$ERR_FILE\"; then",
		"    if [ -f \"$ERR_FILE\" ]; then",
		"      BROWSER_FIND_ERROR=$(cat \"$ERR_FILE\")",
		"    fi",
		"    BROWSER_EXEC=",
		"  fi",
		"  rm -f \"$ERR_FILE\"",
		"}",
		"",
		"cmd_probe() {",
		"  resolve_runtime_summary",
		"  print_runtime_summary",
		"}",
		"",
		"cmd_doctor() {",
		"  resolve_runtime_summary",
		"  print_runtime_summary",
		"  if [ -z \"${MCP_BIN:-}\" ]; then",
		"    printf '%s\\n' \"doctor_status="+
			runtimeDoctorStatusUnavailable+"\"",
		"    printf '%s\\n' \"mcp_smoke=missing\"",
		"    printf '%s\\n' \"browser_smoke=skipped\"",
		"    printf '%s\\n' \"doctor_detail=playwright-mcp-missing\"",
		"    return 1",
		"  fi",
		"  if MCP_CHECK=$("+
			"\"$MCP_BIN\" --help 2>&1 >/dev/null); then",
		"    printf '%s\\n' \"mcp_smoke=ok\"",
		"  else",
		"    MCP_CHECK=$(printf '%s' \"$MCP_CHECK\" | tr '\\n' ' ')",
		"    printf '%s\\n' \"doctor_status="+
			runtimeDoctorStatusUnavailable+"\"",
		"    printf '%s\\n' \"mcp_smoke=failed\"",
		"    printf '%s\\n' \"browser_smoke=skipped\"",
		"    printf '%s\\n' \"doctor_detail=$MCP_CHECK\"",
		"    return 1",
		"  fi",
		"  if [ -z \"$BROWSER_EXEC\" ]; then",
		"    printf '%s\\n' \"doctor_status="+
			runtimeDoctorStatusUnavailable+"\"",
		"    printf '%s\\n' \"browser_smoke=missing\"",
		"    if [ -n \"$BROWSER_FIND_ERROR\" ]; then",
		"      BROWSER_FIND_ERROR=$(printf '%s' "+
			"\"$BROWSER_FIND_ERROR\" | tr '\\n' ' ')",
		"      printf '%s\\n' \"doctor_detail=$BROWSER_FIND_ERROR\"",
		"    else",
		"      printf '%s\\n' \"doctor_detail=browser-not-found\"",
		"    fi",
		"    return 1",
		"  fi",
		"  if \"$BROWSER_EXEC\" --version >/dev/null 2>&1; then",
		"    printf '%s\\n' \"doctor_status="+
			runtimeDoctorStatusReady+"\"",
		"    printf '%s\\n' \"browser_smoke=ok\"",
		"    printf '%s\\n' \"doctor_detail=ready\"",
		"    return 0",
		"  fi",
		"  BROWSER_CHECK=$("+
			"\"$BROWSER_EXEC\" --version 2>&1 >/dev/null)",
		"  BROWSER_CHECK=$(printf '%s' \"$BROWSER_CHECK\" | tr '\\n' ' ')",
		"  printf '%s\\n' \"doctor_status="+
			runtimeDoctorStatusUnavailable+"\"",
		"  printf '%s\\n' \"browser_smoke=failed\"",
		"  printf '%s\\n' \"doctor_detail=$BROWSER_CHECK\"",
		"  return 1",
		"}",
		"",
		"cmd_mcp_stdio() {",
		"  find_playwright_mcp",
		"  browser_mode",
		"  find_browser_name",
		"  set -- --browser \"$BROWSER_NAME\" \"$@\"",
		"  if find_browser_executable 2>/dev/null; then",
		"    set -- --executable-path \"$BROWSER_EXEC\" \"$@\"",
		"  fi",
		"  if [ \"$MODE\" = \""+runtimeBrowserModeHeadless+
			"\" ]; then",
		"    set -- --headless \"$@\"",
		"  fi",
		`  exec "$MCP_BIN" "$@"`,
		"}",
		"",
		`SUBCOMMAND="${1:-doctor}"`,
		"case \"$SUBCOMMAND\" in",
		"  probe)",
		"    shift || true",
		"    cmd_probe \"$@\"",
		"    ;;",
		"  doctor)",
		"    shift || true",
		"    cmd_doctor \"$@\"",
		"    ;;",
		"  mcp-stdio)",
		"    shift || true",
		"    cmd_mcp_stdio \"$@\"",
		"    ;;",
		"  *)",
		"    printf '%s\\n' \"usage: "+
			runtimeBrowserRuntimeName+
			" [probe|doctor|mcp-stdio]\" >&2",
		"    exit 2",
		"    ;;",
		"esac",
	)
	return strings.Join(lines, "\n") + "\n"
}

func runtimeBrowserRuntimePromptLine() string {
	return runtimehint.BrowserPromptLineFromEnv()
}
