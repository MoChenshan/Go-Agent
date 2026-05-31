// Package main provides the internal OpenClaw distribution entrypoint.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"git.code.oa.com/trpc-go/trpc-go"
	thttp "git.code.oa.com/trpc-go/trpc-go/http"
	tlog "git.code.oa.com/trpc-go/trpc-go/log"
	"git.code.oa.com/trpc-go/trpc-go/transport"

	_ "git.woa.com/trpc-go/trpc-agent-go/openclaw/backends"
	wecomchannel "git.woa.com/trpc-go/trpc-agent-go/openclaw/channel/wecom"
	weixinchannel "git.woa.com/trpc-go/trpc-agent-go/openclaw/channel/weixin"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/ingress"
	personaapi "git.woa.com/trpc-go/trpc-agent-go/openclaw/persona"
	envprobeplugin "git.woa.com/trpc-go/trpc-agent-go/openclaw/plugins/envprobe"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/promptasset"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/releaseinfo"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/runtimectl"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/runtimepolicy"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/workspacecfg"
	ocadmin "trpc.group/trpc-go/trpc-agent-go/openclaw/admin"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/app"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/gwclient"
	"trpc.group/trpc-go/trpc-agent-go/skill"

	// Upstream demo plugins.
	_ "trpc.group/trpc-go/trpc-agent-go/openclaw/plugins/echotool"
	_ "trpc.group/trpc-go/trpc-agent-go/openclaw/plugins/stdin"

	// Intranet knowledge providers.
	_ "git.woa.com/trpc-go/trpc-agent-go/openclaw/plugins/lingshan"

	// Enable intranet-only enhancements (telemetry, reporting, etc.).
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"

	"gopkg.in/yaml.v3"
)

const (
	trpcServiceName = ingress.DefaultHTTPServiceName

	subcmdHelp      = "help"
	subcmdPairing   = "pairing"
	subcmdDoctor    = "doctor"
	subcmdBootstrap = "bootstrap"
	subcmdInspect   = "inspect"
	subcmdWeixin    = "weixin"
	subcmdUpgrade   = "upgrade"
	subcmdVersion   = "version"

	helpFlagShort = "-h"
	helpFlagLong  = "--help"

	inspectCmdPlugins    = "plugins"
	inspectCmdConfigKeys = "config-keys"
	inspectCmdDeps       = "deps"

	weixinCmdLogin  = "login"
	weixinCmdList   = "list"
	weixinCmdRemove = "remove"

	weixinDefaultLoginBotType = "3"
	weixinDefaultLoginTimeout = 8 * time.Minute

	bootstrapCmdDeps = "deps"

	pairingCmdList    = "list"
	pairingCmdApprove = "approve"

	flagConf = "conf"

	flagConfig        = "config"
	flagStateDir      = "state-dir"
	flagMode          = "mode"
	flagModel         = "model"
	flagMemoryBackend = "memory-backend"

	flagOpenAIVariant = "openai-variant"
	flagOpenAIBaseURL = "openai-base-url"

	flagProfile            = "profile"
	flagSkill              = "skill"
	flagSkillsRoot         = "skills-root"
	flagSkillsExtraDirs    = "skills-extra-dirs"
	flagSkillsAllowBundled = "skills-allow-bundled"
	flagBundled            = "bundled"

	openClawConfigEnvName = "OPENCLAW_CONFIG"
	codexHomeEnvName      = "CODEX_HOME"
	sudoUserEnvName       = "SUDO_USER"

	defaultConfigRootDir  = ".trpc-agent-go"
	defaultConfigAppDir   = "openclaw"
	defaultConfigFile     = "openclaw.yaml"
	defaultTRPCConfigFile = "trpc_go.yaml"
	defaultDepsProfile    = "common-file-tools"
	skillsDirName         = "skills"
	bundledSkillsDirName  = "bundled"
	skillDocFileName      = "SKILL.md"
	skillMetadataKey      = "metadata"
	openClawMetadataKey   = "openclaw"
	skillMetadataOSKey    = "os"
	extraDirsKey          = "extra_dirs"
	codexDirName          = ".codex"

	installScriptURL = "https://mirrors.tencent.com/" +
		"repository/generic/trpc-agent-go/trpc-claw/" +
		"latest/install.sh"

	channelShutdownTimeout = 5 * time.Second

	forcedShutdownExitCode     = 130
	forcedShutdownSignalBuffer = 2
	gracefulShutdownLogFormat  = "received %v, " +
		"shutting down gracefully; press Ctrl-C again to " +
		"force exit"
	forcedShutdownLogFormat = "received %v again, " +
		"forcing process exit"

	runtimeStateDirEnvName = "TRPC_CLAW_STATE_DIR"
	runtimeEnvFileName     = ".runtime.env"

	runtimePathEnvName           = "PATH"
	runtimeExtraPathEnvName      = "TRPC_CLAW_EXTRA_PATH_DIRS"
	runtimeShellEnvFileEnvName   = "TRPC_CLAW_RUNTIME_SHELL_ENV"
	runtimeBashEnvName           = "BASH_ENV"
	runtimePosixShellEnvName     = "ENV"
	runtimeBinEnvName            = "TRPC_CLAW_BIN"
	runtimeBinDirEnvName         = "TRPC_CLAW_BIN_DIR"
	runtimeOpenClawBinEnvName    = "OPENCLAW_BIN"
	runtimeOpenClawBinDirEnvName = "OPENCLAW_BIN_DIR"
	runtimeTmpDirEnvName         = "TMPDIR"
	runtimeTmpEnvName            = "TMP"
	runtimeTempEnvName           = "TEMP"
	runtimeToolchainRootEnvName  = "OPENCLAW_TOOLCHAIN_ROOT"
	runtimePIPDisableEnvName     = "PIP_DISABLE_PIP_VERSION_CHECK"
	runtimePIPDisableEnvValue    = "1"

	defaultUserLocalBinDir = ".local/bin"
	defaultUserBinDir      = "bin"
	defaultUserGoDir       = "go"
	defaultCargoDir        = ".cargo"

	goBinEnvName      = "GOBIN"
	goPathEnvName     = "GOPATH"
	goRootEnvName     = "GOROOT"
	cargoHomeEnvName  = "CARGO_HOME"
	nodeHomeEnvName   = "NODE_HOME"
	nodePrefixEnvName = "N_PREFIX"
	pnpmHomeEnvName   = "PNPM_HOME"
	virtualEnvEnvName = "VIRTUAL_ENV"

	defaultAdminAddr         = "127.0.0.1:19789"
	defaultAdminAutoPort     = true
	adminAutoPortSearchSpan  = 32
	adminSourceConfigPathEnv = "TRPC_CLAW_ADMIN_SOURCE_CONFIG_PATH"
	flagAdminEnabled         = "admin-enabled"
	flagAdminAddr            = "admin-addr"
	flagAdminAutoPort        = "admin-auto-port"
	adminReadHeaderTimeout   = 5 * time.Second

	agentKey                   = "agent"
	instructionKey             = "instruction"
	instructionFilesKey        = "instruction_files"
	instructionDirKey          = "instruction_dir"
	stateDirKey                = "state_dir"
	personaKey                 = "persona"
	agentPersonaDirKey         = "persona_dir"
	systemPromptKey            = "system_prompt"
	systemPromptFilesKey       = "system_prompt_files"
	systemPromptDirKey         = "system_prompt_dir"
	modelSectionKey            = "model"
	modelModeKey               = "mode"
	modelNameKey               = "name"
	modelBaseURLKey            = "base_url"
	modelOpenAIVariantKey      = "openai_variant"
	skillsRootKey              = "root"
	memoryKey                  = "memory"
	memoryBackendKey           = "backend"
	memoryConfigKey            = "config"
	memoryFallbackKey          = "fallback_to_sqlite_on_embedding_unsupported"
	memoryFallbackCamelKey     = "fallbackToSqliteOnEmbeddingUnsupported"
	memoryBackendFileName      = "file"
	memoryBackendSQLiteName    = "sqlite"
	memoryBackendSQLiteVecName = "sqlitevec"
	embedderKey                = "embedder"
	embedderModelKey           = "model"
	embedderBaseURLKey         = "base_url"
	embedderAPIKeyKey          = "api_key"
	sqliteConfigPathKey        = "path"
	sqliteConfigDSNKey         = "dsn"
	sqliteTableNameKey         = "table_name"
	sqliteSkipDBInitKey        = "skip_db_init"
	sqliteSoftDeleteKey        = "soft_delete"
	skillsKey                  = "skills"
	skillsLoadModeKey          = "load_mode"
	skillsLoadModeCamelKey     = "loadMode"
	skillsMaxLoadedKey         = "max_loaded_skills"
	skillsMaxLoadedCamelKey    = "maxLoadedSkills"
	skillsSkipFallbackKey      = "skip_fallback_on_session_summary"
	skillsSkipFallbackCamelKey = "skipFallbackOnSessionSummary"
	skillsEntriesKey           = "entries"
	codingAgentKey             = "coding_agent"
	codingAgentCamelKey        = "codingAgent"
	codingAgentSkillName       = "coding-agent"
	skillEnabledKey            = "enabled"
	executionModeKey           = "execution_mode"
	executionModeCamelKey      = "executionMode"
	defaultWorkdirKey          = "default_workdir"
	defaultWorkdirCamelKey     = "defaultWorkdir"
	scratchRootKey             = "scratch_root"
	scratchRootCamelKey        = "scratchRoot"
	toolingGuidanceKey         = "tooling_guidance"
	toolingGuidanceCamelKey    = "toolingGuidance"
	channelsKey                = "channels"
	channelTypeKey             = "type"
	channelConfigKey           = "config"
	toolsKey                   = "tools"
	toolProvidersKey           = "providers"
	toolsetsKey                = "toolsets"
	toolTypeKey                = "type"
	toolNameKey                = "name"
	toolConfigKey              = "config"
	toolArgsKey                = "args"
	toolCommandKey             = "command"
	toolDefaultProfileKey      = "default_profile"
	toolProfilesKey            = "profiles"
	toolTimeoutKey             = "timeout"
	toolTransportKey           = "transport"
	fileToolTypeName           = "file"
	fileToolBaseDirKey         = "base_dir"
	fileToolBaseDirCamelKey    = "baseDir"
	fileToolReadOnlyKey        = "read_only"
	fileToolReadOnlyCamelKey   = "readOnly"
	scratchOutputDirName       = "out"

	defaultCodingAgentExecutionMode = codingAgentModeHost
	browserToolProviderTypeName     = "browser"
	browserToolProviderName         = "browser"
	browserToolDefaultProfileName   = "openclaw"
	browserToolDefaultTimeout       = "5m"
	browserToolTransportSTDIO       = "stdio"
	browserRuntimeMissingNode       = "node-missing"
	browserRuntimeMissingNPM        = "npm-missing"
	browserRuntimeMissingBrowser    = "browser-not-found"
	browserRuntimeInstallFailed     = "managed-playwright-install-failed"
	runtimeNodeExecName             = "node"
	runtimeNPMExecName              = "npm"
	codingAgentModeAuto             = "auto"
	codingAgentModeSandbox          = "sandbox"
	codingAgentModeHost             = "host"
	skillsLoadModeOnce              = "once"
	skillsLoadModeTurn              = "turn"
	skillsLoadModeSession           = "session"
	defaultSkillsMaxLoaded          = 10
	defaultSkillsSkipFallback       = false

	supportedCodingAgentModes = "sandbox, auto, host"

	wecomAICallbackPathEnvName           = "WECOM_AI_CALLBACK_PATH"
	defaultWeComAICallbackPath           = "/wecom/ai/callback"
	wecomNotificationCallbackPathEnvName = "WECOM_NOTIFICATION_CALLBACK_PATH"
	defaultWeComNotificationCallbackPath = "/wecom/notification/callback"
	wecomGroupSessionModeEnvName         = "WECOM_GROUP_SESSION_MODE"

	serverProtocolHTTP           = "http"
	serverProtocolHTTP2          = "http2"
	serverProtocolHTTPNoProtocol = "http_no_protocol"
	serverProtocolHTTP2NoProto   = "http2_no_protocol"

	runtimeProductName       = "trpc-claw"
	legacySystemPromptPrefix = "you are "

	defaultRuntimeModelMode       = "openai"
	defaultRuntimeOpenAIVariant   = "auto"
	openAIModelEnvName            = "OPENAI_MODEL"
	openAIBaseURLEnvName          = "OPENAI_BASE_URL"
	openAIAPIKeyEnvName           = "OPENAI_API_KEY"
	configEnvRefPrefix            = "${"
	configEnvRefSuffix            = "}"
	configEnvDefaultSep           = ":-"
	runtimeStateDirEnvRef         = "${TRPC_CLAW_STATE_DIR}"
	deepSeekAPIHost               = "api.deepseek.com"
	deepSeekModelPrefix           = "deepseek-"
	runtimeIdentityPromptHeader   = "Current assistant name:"
	runtimeIdentityWarningPrefix  = "trpc-claw warning: "
	runtimeCodingPromptHeader     = "Coding runtime guidance:"
	runtimeLanguageProtocolHeader = "Response language protocol:"
	runtimeProgressProtocolHeader = "Preamble and progress protocol:"
	runtimeWorkflowProtocolHeader = "Coding workflow protocol:"
	runtimeTruthProtocolHeader    = "Truthfulness protocol:"
	runtimeLanguageFollowUserRule = "Use the user's dominant " +
		"language for public preambles, progress updates, " +
		"and final replies unless the user explicitly asks for " +
		"another language."
	runtimeLanguagePreserveTermsRule = "Preserve code, commands, " +
		"file paths, API names, identifiers, error text, exact " +
		"quotes, and established technical terms in their " +
		"original form."
	runtimeLanguageNoSentenceMixRule = "Avoid sentence-level " +
		"language mixing in public text. For example, when the " +
		"user's dominant language is Chinese, do not add " +
		"standalone English sentences unless quoting source " +
		"text or the user asks for English."
	runtimePreambleVisibilityRule = "Users usually cannot " +
		"see tool calls or internal reasoning. If you do " +
		"not briefly say what you are about to do, the " +
		"user may see silence."
	runtimePreambleBeforeToolRule = "Before the first " +
		"non-trivial tool call, send one short " +
		"user-visible preamble that says what you are " +
		"about to do."
	runtimePreambleImmediateRule = "That brief preamble " +
		"is part of acting immediately, not a pause to " +
		"ask what to do next."
	runtimePreambleNoConfirmRule = "Do not turn that " +
		"preamble into a confirmation request, options " +
		"menu, or summary of what you could do. State " +
		"the immediate next step and then do it."
	runtimePreambleRequiresActionRule = "A preamble-only " +
		"message is not a completed turn. If tool work is " +
		"needed, the same assistant message must include " +
		"the tool call; if no tool is needed, skip the " +
		"setup line and return the requested content or " +
		"completed result."
	runtimePreambleGroupingRule = "Group related tool " +
		"calls under one preamble instead of narrating " +
		"every trivial read."
	runtimePreambleTrivialRule = "Skip a standalone " +
		"preamble for a single trivial read unless it is " +
		"part of a larger step."
	runtimeProgressMilestoneRule = "For longer tasks, " +
		"send short progress updates at natural " +
		"milestones when you find something load-bearing, " +
		"change direction, or finish a meaningful " +
		"subtask."
	runtimeProgressLongRunningRule = "When a long-running " +
		"command, deployment, upload, build, or " +
		"interactive session emits a meaningful new " +
		"stage, treat that change as a user-visible " +
		"progress milestone."
	runtimeProgressQuietPollRule = "Do not narrate " +
		"every empty poll, unchanged wait, or repeated " +
		"status check when nothing changed."
	runtimeProgressWaitingRule = "If a long-running " +
		"task stays quiet for a while, send one brief " +
		"waiting update before you keep polling or " +
		"waiting."
	runtimeProgressContentRule = "Progress updates " +
		"should say what changed and what you are doing " +
		"next."
	runtimeProgressExamplesRule = "Valid preambles are " +
		"short and immediately followed by tool work, for " +
		"example: \"I'm checking the repo layout first.\" " +
		"\"I'm reading the matching docs next.\" Do not " +
		"send those sentences as the whole reply."
	runtimeProgressPersonaRule = "Let the active persona " +
		"lead the wording, cadence, and attitude of " +
		"preambles, progress updates, and the final " +
		"answer. Treat the other runtime rules as " +
		"channel and " +
		"correctness guardrails instead of a competing " +
		"writing style."
	activePresetPersonaPromptHeader = "Active preset persona: "
	runtimePersonaPrimaryStyleRule  = "This persona is " +
		"the primary style guide for public progress " +
		"updates and final replies. Other runtime " +
		"notes only constrain channel protocol, tool " +
		"use, and factual accuracy."
	runtimeFreshInspectionRule = "When a request is " +
		"grounded in a local repo, path, file, or the " +
		"current source, perform a fresh inspection " +
		"before planning, editing, or answering. Do " +
		"not rely only on earlier turns, summaries, " +
		"or memory."
	runtimeSearchPriorityRule = "For repo search, " +
		"prefer `rg --files` for file inventory and " +
		"`rg -n` for text search. If `rg` is " +
		"unavailable, fall back to `find` or `grep`."
	runtimeSearchScopeRule = "Keep searches inside " +
		"the target repo or path. Avoid broad " +
		"parent-directory scans such as `grep -R ..` " +
		"unless the user explicitly asks for a wider " +
		"scope."
	runtimeReadNarrowRule = "After locating candidate " +
		"files, read the smallest relevant slices " +
		"first and expand only as needed."
	runtimeSkillFirstCapabilityRule = "When the user asks " +
		"you to add, teach, configure, preserve, " +
		"or reuse a durable capability, workflow, " +
		"integration, domain rule, team process, " +
		"API, CLI, MCP endpoint, document convention, " +
		"or tool usage pattern, or to remember an " +
		"executable workflow or integration, prefer " +
		"creating or updating a local skill over " +
		"treating it as a one-off answer. For " +
		"lightweight facts, preferences, or simple " +
		"standing rules, use memory instead."
	runtimeSkillPlatformBoundaryRule = "Use platform code " +
		"and tools for stable safety boundaries, " +
		"secret redaction, permissions, file paths, " +
		"validation, and execution guarantees; use skill context " +
		"for evolving behavior, triggers, constraints, " +
		"examples, recovery paths, and domain knowledge."
	runtimePrivateConfigRule = "When the user explicitly " +
		"provides a complete private API, CLI, or MCP " +
		"configuration, including a credential-bearing " +
		"endpoint, treat it as authorization to save a " +
		"local private runtime config file such as a " +
		"skill-local `mcp.json` in a writable user-managed " +
		"skill root, or another dedicated private config " +
		"path. Keep credential-bearing config non-shared " +
		"and excluded from source control and packaging. " +
		"Prefer that config file " +
		"over asking the user to re-enter the same value " +
		"as an environment variable. Set restrictive file " +
		"permissions when possible, never echo the secret " +
		"value back in output, logs, or errors, and do not " +
		"edit shell startup or trusted " +
		"env files just to persist it. Inspect existing local " +
		"config before treating a missing environment " +
		"variable as blocking. Keep shared, bundled, " +
		"published, or repo-tracked skills secret-free."
	runtimeSkillFollowThroughRule = "If you create or " +
		"update a skill, do not stop after describing " +
		"the idea: choose a writable user-managed skill " +
		"root, not bundled skills unless explicitly asked " +
		"to edit them, write the skill files, keep shared " +
		"or published credentials out of `SKILL.md`, validate " +
		"or inspect the skill, refresh or reload skills when " +
		"the runtime provides that path, and then use the " +
		"skill to complete the current task."
	runtimeSelfRecoveryRule = "When an approach fails, " +
		"try the next reasonable recovery step yourself " +
		"before asking what to do next. Prefer retries, " +
		"fresh inspection, smaller scope, alternative " +
		"tools, format conversion, dependency bootstrap, " +
		"or writing the artifact another way over " +
		"stopping to ask for confirmation."
	runtimeExternalLookupRule = "For external search, " +
		"latest/current facts, realtime data, market " +
		"quotes, or other web lookup requests, use " +
		"available browser/search tools yourself " +
		"before replying. Prefer the most obvious " +
		"primary entity, listing, venue, or source " +
		"first. Do not redirect the user to another " +
		"app/site or hand ordinary disambiguation " +
		"back to the user before you have checked the " +
		"obvious primary target and closely related " +
		"variants you can inspect yourself."
	runtimeWorkspaceSeparationRule = "Default to " +
		"classifying direct uploads and user-facing " +
		"generated artifacts as non-repo inputs or " +
		"outputs. Keep the coding workspace for repo " +
		"files. Unless the user explicitly names a repo " +
		"path or asks to read, edit, or create files " +
		"there, do not place " +
		"direct uploads, generated docs or media, " +
		"exports, OCR text, or disposable intermediates " +
		"inside the coding workspace."
	runtimeCrossRootInspectionRule = "When a task mixes " +
		"repository context with uploads or generated " +
		"artifacts, inspect both the coding workspace " +
		"and the runtime-managed artifact roots in the " +
		"same run instead of forcing everything into " +
		"one directory."
	runtimeArtifactVerificationRule = "Never tell the " +
		"user a generated file is ready, and never add " +
		"an attachment marker, until you actually " +
		"create the file and verify the exact path " +
		"with a tool such as `stat`, `ls`, or " +
		"`test -f`."
	runtimeDepsBootstrapRule = "When a task maps cleanly " +
		"to dependency profiles or skill metadata, " +
		"prefer `trpc-claw inspect deps` and " +
		"`trpc-claw bootstrap deps --apply` before " +
		"scattered ad hoc installs."
	runtimeManagedInstallRule = "When several install " +
		"paths are possible, prefer self-contained " +
		"user-space toolchains, downloads, and " +
		"managed Python packages over host-global " +
		"libraries or implicit font discovery."
	runtimeManagedCJKAssetsRule = "For Chinese or other " +
		"CJK OCR and document export, if coverage or " +
		"language support is uncertain, install " +
		"managed CJK fonts and OCR language data " +
		"under the runtime toolchain instead of " +
		"depending only on host defaults."
	runtimeCJKVerificationRule = "For Chinese or other " +
		"CJK artifacts, treat font coverage and " +
		"encoding as required capabilities. Use an " +
		"explicit CJK-capable font, then render or " +
		"extract a smoke sample to verify the output " +
		"is not garbled before replying."
	runtimeEnvProbeRule = "When the user asks whether " +
		"a specific environment variable, token, " +
		"secret, API key, or shell credential is " +
		"visible, call env_probe instead of guessing " +
		"or reading shell rc files directly. If " +
		"env_probe activates a safe static declaration, " +
		"treat it as available for future tool calls. " +
		"Never reveal secret values."
	runtimeSelfContainedDocRule = "If a PDF, OCR, or " +
		"office workflow depends on host libraries or " +
		"default fonts and fails, switch to a more " +
		"self-contained toolchain instead of giving up."

	defaultSQLiteMemoryDBFileName = "memories.sqlite"
	defaultSQLiteVecDBFileName    = "memories_vec.sqlite"
	sqliteFileExtension           = ".sqlite"
	sqliteFallbackFileSuffix      = "_fallback.sqlite"
	gitDirName                    = ".git"
	agentsDocFileName             = "AGENTS.md"
)

var (
	runtimeAutonomyRule       = runtimepolicy.AutonomyRule()
	runtimeGoalCompletionRule = runtimepolicy.
					GoalCompletionRule()
	runtimeMinimalQuestionRule = runtimepolicy.
					MinimalQuestionRule()
	runtimeNoChoiceTailRule = runtimepolicy.NoChoiceTailRule()
)

type signalExitFunc func(int)

type forcedShutdownHandler struct {
	exit signalExitFunc

	mu    sync.Mutex
	count int
}

func newForcedShutdownHandler(
	exit signalExitFunc,
) *forcedShutdownHandler {
	if exit == nil {
		exit = os.Exit
	}
	return &forcedShutdownHandler{exit: exit}
}

func (h *forcedShutdownHandler) Handle(
	sig os.Signal,
) {
	if sig == nil {
		return
	}

	h.mu.Lock()
	h.count++
	count := h.count
	h.mu.Unlock()

	if count == 1 {
		tlog.Infof(gracefulShutdownLogFormat, sig)
		return
	}

	tlog.Warnf(forcedShutdownLogFormat, sig)
	h.exit(forcedShutdownExitCode)
}

func startForcedShutdownWatcher(
	exit signalExitFunc,
) func() {
	handler := newForcedShutdownHandler(exit)
	done := make(chan struct{})
	sigCh := make(chan os.Signal, forcedShutdownSignalBuffer)

	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for {
			select {
			case <-done:
				return
			case sig := <-sigCh:
				handler.Handle(sig)
			}
		}
	}()

	var stopOnce sync.Once
	return func() {
		stopOnce.Do(func() {
			signal.Stop(sigCh)
			close(done)
		})
	}
}

type runtimeModelIdentity struct {
	ModelMode     string
	ModelName     string
	OpenAIVariant string
	OpenAIBaseURL string
}

type embedderTarget struct {
	Model           string
	BaseURL         string
	ExplicitBaseURL string
	ExplicitAPIKey  string
	EnvAPIKeySet    bool
}

type sqliteFallbackDecision struct {
	Reason string
}

type codingAgentDefaults struct {
	ExecutionMode  string
	DefaultWorkdir string
	ScratchRoot    string
	OutputRoot     string
	TempRoot       string
}

type codingWorkspaceFacts struct {
	Workdir    string
	GitRoot    string
	AgentsPath string
}

type helpCommand struct {
	Command     string
	Description string
}

type helpTopic struct {
	Title        string
	Summary      string
	Usage        []string
	Subcommands  []helpCommand
	Examples     []string
	Notes        []string
	FlagHelpArgs []string
}

type topLevelHelpInfo struct {
	BinaryPath                string
	OpenClawConfigPath        string
	OpenClawConfigDefaultPath string
	TRPCConfigPath            string
	TRPCConfigDefaultPath     string
	StateDir                  string
}

type runtimeA2ASurface struct {
	Handler       http.Handler
	BasePath      string
	AgentCardPath string
}

type adminRuntimeOptions struct {
	Enabled  bool
	Addr     string
	AutoPort bool
}

type adminConfigPaths struct {
	Source  string
	Runtime string
}

type adminBinding struct {
	listener  net.Listener
	addr      string
	url       string
	relocated bool
}

type adminRuntimeConfigFile struct {
	Admin *adminRuntimeConfig `yaml:"admin,omitempty"`
}

type adminRuntimeConfig struct {
	Enabled  *bool   `yaml:"enabled,omitempty"`
	Addr     *string `yaml:"addr,omitempty"`
	AutoPort *bool   `yaml:"auto_port,omitempty"`
}

var supportedPersonaPresets = personaapi.BuiltinIDList() +
	", off"

var defaultMemoryInstruction = mustEmbeddedPromptBundle(
	promptasset.DefaultInstructionEmbeddedDir,
	[]string{promptasset.DefaultMemoryFileName},
)

var lookupUserFunc = user.Lookup
var configWarningEmitter = func(message string) {
	tlog.Warnf("%s", message)
}

func main() {
	args := os.Args[1:]
	trpcConfPath, openClawArgs, err := stripTRPCConf(args)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if handled, code := maybeHandleCommandHelp(
		os.Stdout,
		openClawArgs,
	); handled {
		os.Exit(code)
	}
	if isTopLevelVersionRequest(openClawArgs) {
		_, _ = fmt.Fprintln(os.Stdout, currentVersion())
		os.Exit(0)
	}
	if isTopLevelUpgradeRequest(openClawArgs) {
		os.Exit(runUpgradeCommand(openClawArgs[1:]))
	}
	if isTopLevelHelpRequest(openClawArgs) {
		os.Exit(printTopLevelHelp(os.Stdout))
	}
	rawOpenClawArgs := append([]string(nil), openClawArgs...)
	openClawArgs, paths, err := normalizeOpenClawArgsWithPaths(
		openClawArgs,
	)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if isTopLevelWeixinCommand(openClawArgs) {
		os.Exit(runWeixinCommand(openClawArgs[1:], paths.StateDir))
	}
	if err := applyRuntimeEnvDefaults(paths); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	sourceConfigPath, err := resolveSourceOpenClawConfigPath(
		rawOpenClawArgs,
		paths.OpenClawConfigPath,
	)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	var cleanupConfig func()
	openClawArgs, cleanupConfig, err = prepareOpenClawConfig(
		openClawArgs,
		paths,
	)
	if err != nil {
		runCleanup(cleanupConfig)
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	adminPaths, err := resolveAdminConfigPaths(
		sourceConfigPath,
		openClawArgs,
	)
	if err != nil {
		runCleanup(cleanupConfig)
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if strings.TrimSpace(sourceConfigPath) != "" {
		paths.OpenClawConfigPath = sourceConfigPath
	}
	adminOpts, err := resolveAdminRuntimeOptions(
		adminPaths.Runtime,
		openClawArgs,
	)
	if err != nil {
		runCleanup(cleanupConfig)
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if isOpenClawSubcommand(openClawArgs) {
		restoreAdminConfigEnv := withAdminSourceConfigPathEnv(
			adminPaths.Source,
		)
		code := app.Main(openClawArgs)
		restoreAdminConfigEnv()
		runCleanup(cleanupConfig)
		os.Exit(code)
	}
	restoreAdminConfigEnv := withAdminSourceConfigPathEnv(
		adminPaths.Source,
	)
	trpcConfPath, err = resolveTRPCConfigPath(trpcConfPath)
	if err != nil {
		restoreAdminConfigEnv()
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if trpcConfPath != "" {
		trpc.ServerConfigPath = trpcConfPath
	}
	paths.TRPCConfigPath = trpcConfPath

	if !flag.Parsed() {
		_ = flag.CommandLine.Parse([]string{})
	}

	registerOpenClawHTTPServerTransports()

	s := trpc.NewServer()
	var lifecycleExitCode atomic.Int32
	var lifecycleCloseOnce sync.Once
	scheduleLifecycleExit := func(intent runtimectl.Intent) {
		code := intent.ExitCode
		if code == 0 {
			code = runtimectl.DefaultLifecycleExitCode
		}
		lifecycleExitCode.Store(int32(code))
		lifecycleCloseOnce.Do(func() {
			go func() {
				time.Sleep(runtimeActionCloseDelay)
				if err := s.Close(nil); err != nil {
					tlog.Errorf(
						"runtime lifecycle close failed: %v",
						err,
					)
				}
			}()
		})
	}

	runtimeCtx := context.Background()
	runtimeProfilePaths := paths
	runtimeProfilePaths.OpenClawConfigPath = adminPaths.Runtime
	runtimeProfileOpts, err := runtimeProfileOptions(
		runtimeCtx,
		runtimeProfilePaths,
	)
	if err != nil {
		restoreAdminConfigEnv()
		runCleanup(cleanupConfig)
		tlog.Errorf("%v", err)
		os.Exit(1)
	}

	rt, err := app.NewRuntimeWithOptions(
		runtimeCtx,
		openClawArgs,
		runtimeProfileOpts...,
	)
	restoreAdminConfigEnv()
	runCleanup(cleanupConfig)
	if err != nil {
		tlog.Errorf("%v", err)
		os.Exit(exitCode(err))
	}
	maybeSuggestUpgrade()
	lifecycleManager := newRuntimeLifecycleManager(
		paths,
		scheduleLifecycleExit,
	)
	injectRuntimeLifecycleController(
		rt.Channels,
		lifecycleManager,
	)
	injectSubagentService(
		rt.Channels,
		rt.SubagentService(),
	)
	promptAdminSvc, closePromptAdmin := newRuntimeAdminProvider(
		sourceConfigPath,
		paths.StateDir,
		rt.AppName(),
		openClawArgs,
		rt.SessionService(),
		rt.PromptController(),
		rt.Channels,
		collectRuntimeWeComPromptTargets(rt.Channels),
	)
	memoryUserLabels := newRuntimeMemoryUserLabelResolver(
		rt.Channels,
	)
	rt.ConfigureAdmin(func(cfg *ocadmin.Config) {
		cfg.Prompts = promptAdminSvc
		cfg.Identity = promptAdminSvc
		cfg.Personas = promptAdminSvc
		cfg.Chats = promptAdminSvc
		cfg.MemoryUserLabels = memoryUserLabels
	})
	rt.AddAdminOptions(
		ocadmin.WithRuntimeConfigProvider(
			promptAdminSvc,
		),
		ocadmin.WithRuntimeLifecycleProvider(
			newRuntimeLifecycleAdminProvider(
				lifecycleManager,
			),
		),
	)
	weixinAdminSvc := newWeixinAdminService(
		collectRuntimeWeixinAdminTargets(rt.Channels),
	)
	wecomActivateAdminSvc := newWeComActivateAdminService(
		rt.Channels,
	)
	wecomDebugSendAdminSvc := newWeComDebugSendAdminService(
		rt.Channels,
	)
	adminChatGateway, err := gwclient.New(
		rt.Gateway.Handler,
		rt.Gateway.MessagesPath,
		rt.Gateway.CancelPath,
	)
	var adminWebChatSvc *adminWebChatService
	if err != nil {
		tlog.Warnf("admin web chat disabled: %v", err)
	} else {
		adminWebChatSvc = newAdminWebChatService(
			rt.AppName(),
			adminChatGateway,
			rt.SessionService(),
		)
	}
	channelsAdminSvc := newChannelsAdminService(
		promptAdminSvc,
		weixinAdminSvc,
		wecomActivateAdminSvc,
		wecomDebugSendAdminSvc,
	)
	rt.Admin.Handler = wrapOpenClawAdminHandler(
		rt.Admin.Handler,
		lifecycleManager,
		adminWebChatSvc,
		channelsAdminSvc,
		weixinAdminSvc,
		wecomActivateAdminSvc,
		wecomDebugSendAdminSvc,
	)

	var closeOnce sync.Once
	closeRuntime := func() {
		closeOnce.Do(func() {
			_ = rt.Close()
		})
	}

	muxes, err := buildServiceMuxes(rt)
	if err != nil {
		closeRuntime()
		tlog.Fatalf("%v", err)
	}
	for _, name := range sortedMapKeys(muxes) {
		svc := s.Service(name)
		if svc == nil {
			closeRuntime()
			tlog.Fatalf(
				"missing %q service config in trpc_go.yaml",
				name,
			)
		}
		thttp.RegisterNoProtocolServiceMux(svc, muxes[name])
	}

	runCtx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	closeAdmin := func() {}
	adminAddr := strings.TrimSpace(rt.Admin.Addr)
	if adminAddr == "" {
		adminAddr = adminOpts.Addr
	}
	if rt.Admin.Handler != nil {
		adminBinding, bindErr := openAdminBinding(
			adminAddr,
			adminOpts.AutoPort,
		)
		if bindErr != nil {
			cancel()
			closeRuntime()
			tlog.Errorf("admin server failed to start: %v", bindErr)
			os.Exit(1)
		}
		adminSrv := &http.Server{
			Handler:           rt.Admin.Handler,
			ReadHeaderTimeout: adminReadHeaderTimeout,
		}
		var adminCloseOnce sync.Once
		closeAdmin = func() {
			adminCloseOnce.Do(func() {
				closePromptAdmin()
				if weixinAdminSvc != nil {
					weixinAdminSvc.Close()
				}
				shutdownCtx, shutdownCancel :=
					context.WithTimeout(
						context.Background(),
						channelShutdownTimeout,
					)
				defer shutdownCancel()
				err := adminSrv.Shutdown(shutdownCtx)
				if err != nil &&
					!errors.Is(err, http.ErrServerClosed) {
					tlog.Errorf(
						"admin server shutdown failed: %v",
						err,
					)
				}
			})
		}
		go func() {
			err := adminSrv.Serve(adminBinding.listener)
			if err == nil ||
				errors.Is(err, http.ErrServerClosed) {
				return
			}
			tlog.Errorf("admin server stopped: %v", err)
		}()
		logAdminStartup(adminAddr, adminBinding)
		logWeixinAdminStartup(adminBinding.url, weixinAdminSvc)
	} else {
		closeAdmin = func() {
			closePromptAdmin()
			if weixinAdminSvc != nil {
				weixinAdminSvc.Close()
			}
		}
	}

	for _, ch := range rt.Channels {
		ch := ch
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := ch.Run(runCtx); err != nil {
				if runCtx.Err() != nil {
					return
				}
				tlog.Errorf("channel %q failed: %v", ch.ID(), err)
			}
		}()
	}

	s.RegisterOnShutdown(func() {
		closeAdmin()
		cancel()

		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(channelShutdownTimeout):
		}
	})

	stopForcedShutdownWatcher := startForcedShutdownWatcher(os.Exit)
	defer stopForcedShutdownWatcher()

	tlog.Infof("OpenClaw gateway is registered to %q", trpcServiceName)
	tlog.Infof("Health:   GET  %s", rt.Gateway.HealthPath)
	tlog.Infof("Messages: POST %s", rt.Gateway.MessagesPath)
	tlog.Infof(
		"Status:   GET  %s?request_id=...",
		rt.Gateway.StatusPath,
	)
	tlog.Infof("Cancel:   POST %s", rt.Gateway.CancelPath)
	a2aSurface, hasA2A, err := extractRuntimeA2ASurface(rt)
	if err != nil {
		tlog.Fatalf("extract OpenClaw A2A surface failed: %v", err)
	}
	if hasA2A {
		tlog.Infof("OpenClaw A2A is registered to %q", trpcServiceName)
		tlog.Infof("A2A base: GET  %s", a2aSurface.BasePath)
		tlog.Infof("A2A card: GET  %s", a2aSurface.AgentCardPath)
	}
	logStartupPaths(paths)

	if err := s.Serve(); err != nil {
		stopForcedShutdownWatcher()
		tlog.Errorf("server failed to start: %v", err)
		closeAdmin()
		closeRuntime()
		os.Exit(1)
	}
	stopForcedShutdownWatcher()
	closeAdmin()
	closeRuntime()
	if code := int(lifecycleExitCode.Load()); code != 0 {
		os.Exit(code)
	}
}

func resolveAdminRuntimeOptions(
	configPath string,
	args []string,
) (adminRuntimeOptions, error) {
	opts := adminRuntimeOptions{
		Enabled:  true,
		Addr:     defaultAdminAddr,
		AutoPort: defaultAdminAutoPort,
	}

	flagAdminEnabledSet, err := boolFlagWasSet(
		args,
		flagAdminEnabled,
	)
	if err != nil {
		return adminRuntimeOptions{}, err
	}
	flagAdminAddrSet, err := stringFlagWasSet(
		args,
		flagAdminAddr,
	)
	if err != nil {
		return adminRuntimeOptions{}, err
	}
	flagAdminAutoPortSet, err := boolFlagWasSet(
		args,
		flagAdminAutoPort,
	)
	if err != nil {
		return adminRuntimeOptions{}, err
	}

	if err := applyAdminRuntimeConfigFile(
		&opts,
		configPath,
		flagAdminEnabledSet,
		flagAdminAddrSet,
		flagAdminAutoPortSet,
	); err != nil {
		return adminRuntimeOptions{}, err
	}

	if value, ok, err := boolFlagValueFromArgs(
		args,
		flagAdminEnabled,
	); err != nil {
		return adminRuntimeOptions{}, err
	} else if ok {
		opts.Enabled = value
	}
	if value, ok, err := flagValueFromArgs(
		args,
		flagAdminAddr,
	); err != nil {
		return adminRuntimeOptions{}, err
	} else if ok {
		opts.Addr = value
	}
	if value, ok, err := boolFlagValueFromArgs(
		args,
		flagAdminAutoPort,
	); err != nil {
		return adminRuntimeOptions{}, err
	} else if ok {
		opts.AutoPort = value
	}

	opts.Addr = strings.TrimSpace(opts.Addr)
	if opts.Enabled && opts.Addr == "" {
		opts.Addr = defaultAdminAddr
	}
	return opts, nil
}

func resolveAdminConfigPaths(
	sourceConfigPath string,
	args []string,
) (adminConfigPaths, error) {
	paths := adminConfigPaths{
		Source:  strings.TrimSpace(sourceConfigPath),
		Runtime: strings.TrimSpace(sourceConfigPath),
	}
	value, ok, err := flagValueFromArgs(args, flagConfig)
	if err != nil {
		return adminConfigPaths{}, err
	}
	if ok {
		paths.Runtime = strings.TrimSpace(value)
	}
	if paths.Source == "" {
		paths.Source = paths.Runtime
	}
	if paths.Runtime == "" {
		paths.Runtime = paths.Source
	}
	return paths, nil
}

func withAdminSourceConfigPathEnv(path string) func() {
	path = strings.TrimSpace(path)
	if path == "" {
		return func() {}
	}

	value, ok := os.LookupEnv(adminSourceConfigPathEnv)
	_ = os.Setenv(adminSourceConfigPathEnv, path)
	return func() {
		if ok {
			_ = os.Setenv(adminSourceConfigPathEnv, value)
			return
		}
		_ = os.Unsetenv(adminSourceConfigPathEnv)
	}
}

func boolFlagValueFromArgs(
	args []string,
	name string,
) (bool, bool, error) {
	for _, raw := range args {
		value, ok := matchFlagValue(raw, name)
		if !ok {
			continue
		}
		switch raw {
		case "-" + name, "--" + name:
			return true, true, nil
		default:
			parsed, err := parseBoolFlagValue(name, value)
			if err != nil {
				return false, false, err
			}
			return parsed, true, nil
		}
	}
	return false, false, nil
}

func boolFlagWasSet(
	args []string,
	name string,
) (bool, error) {
	_, ok, err := boolFlagValueFromArgs(args, name)
	return ok, err
}

func stringFlagWasSet(
	args []string,
	name string,
) (bool, error) {
	_, ok, err := flagValueFromArgs(args, name)
	return ok, err
}

func applyAdminRuntimeConfigFile(
	opts *adminRuntimeOptions,
	configPath string,
	flagAdminEnabledSet bool,
	flagAdminAddrSet bool,
	flagAdminAutoPortSet bool,
) error {
	if opts == nil {
		return nil
	}
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	var cfg adminRuntimeConfigFile
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return err
	}
	if cfg.Admin == nil {
		return nil
	}
	if cfg.Admin.Enabled != nil && !flagAdminEnabledSet {
		opts.Enabled = *cfg.Admin.Enabled
	}
	if cfg.Admin.Addr != nil && !flagAdminAddrSet {
		opts.Addr = strings.TrimSpace(*cfg.Admin.Addr)
	}
	if cfg.Admin.AutoPort != nil && !flagAdminAutoPortSet {
		opts.AutoPort = *cfg.Admin.AutoPort
	}
	return nil
}

func openAdminBinding(
	addr string,
	autoPort bool,
) (*adminBinding, error) {
	preferred := strings.TrimSpace(addr)
	if preferred == "" {
		return nil, fmt.Errorf("admin: empty listen address")
	}

	listener, err := net.Listen("tcp", preferred)
	if err == nil {
		actual := listener.Addr().String()
		return &adminBinding{
			listener: listener,
			addr:     actual,
			url:      listenURL(actual),
		}, nil
	}
	if !autoPort || !isAddressInUse(err) {
		return nil, fmt.Errorf(
			"admin: listen on %s: %w",
			preferred,
			err,
		)
	}

	host, portRaw, splitErr := net.SplitHostPort(preferred)
	if splitErr != nil {
		return nil, fmt.Errorf(
			"admin: listen on %s: %w",
			preferred,
			err,
		)
	}
	basePort, convErr := strconv.Atoi(portRaw)
	if convErr != nil || basePort <= 0 || basePort >= 65535 {
		return nil, fmt.Errorf(
			"admin: listen on %s: %w",
			preferred,
			err,
		)
	}

	maxPort := basePort + adminAutoPortSearchSpan
	if maxPort > 65535 {
		maxPort = 65535
	}
	for port := basePort + 1; port <= maxPort; port++ {
		candidate := net.JoinHostPort(
			host,
			strconv.Itoa(port),
		)
		listener, err = net.Listen("tcp", candidate)
		if err == nil {
			actual := listener.Addr().String()
			return &adminBinding{
				listener:  listener,
				addr:      actual,
				url:       listenURL(actual),
				relocated: actual != preferred,
			}, nil
		}
		if !isAddressInUse(err) {
			return nil, fmt.Errorf(
				"admin: listen on %s: %w",
				candidate,
				err,
			)
		}
	}
	return nil, fmt.Errorf(
		"admin: listen on %s failed and no free port was "+
			"found in the next %d ports: %w",
		preferred,
		adminAutoPortSearchSpan,
		err,
	)
}

func isAddressInUse(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE)
}

func listenURL(addr string) string {
	host, port, err := net.SplitHostPort(
		strings.TrimSpace(addr),
	)
	if err != nil {
		trimmed := strings.TrimSpace(addr)
		if trimmed == "" {
			return ""
		}
		return "http://" + trimmed
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func logAdminStartup(
	preferredAddr string,
	binding *adminBinding,
) {
	if binding == nil {
		return
	}
	if binding.relocated {
		tlog.Warnf(
			"Admin UI preferred address %s was busy; using %s "+
				"instead",
			preferredAddr,
			binding.addr,
		)
	}
	tlog.Infof("Admin UI listening on %s", binding.addr)
	tlog.Infof("Admin UI: %s", binding.url)
}

func registerOpenClawHTTPServerTransports() {
	httpTransport := thttp.NewServerTransport(
		transport.WithReusePort(false),
	)
	http2Transport := thttp.NewServerTransport(
		transport.WithReusePort(false),
	)

	transport.RegisterServerTransport(
		serverProtocolHTTP,
		httpTransport,
	)
	transport.RegisterServerTransport(
		serverProtocolHTTPNoProtocol,
		httpTransport,
	)
	transport.RegisterServerTransport(
		serverProtocolHTTP2,
		http2Transport,
	)
	transport.RegisterServerTransport(
		serverProtocolHTTP2NoProto,
		http2Transport,
	)
}

func isOpenClawSubcommand(args []string) bool {
	if len(args) == 0 {
		return false
	}

	switch strings.TrimSpace(args[0]) {
	case subcmdPairing, subcmdDoctor, subcmdBootstrap, subcmdInspect:
		return true
	default:
		return false
	}
}

func isTopLevelVersionRequest(args []string) bool {
	if len(args) == 0 {
		return false
	}

	switch strings.TrimSpace(args[0]) {
	case subcmdVersion, "-version", "--version":
		return true
	default:
		return false
	}
}

func isTopLevelHelpRequest(args []string) bool {
	if len(args) == 0 {
		return false
	}

	switch strings.TrimSpace(args[0]) {
	case subcmdHelp, helpFlagShort, helpFlagLong:
		return true
	default:
		return false
	}
}

func normalizeHelpExitCode(code int) int {
	if code == 2 {
		return 0
	}
	return code
}

func printTopLevelHelp(w io.Writer) int {
	prefix := topLevelHelpText(resolveTopLevelHelpInfo())
	helpText, code, err := captureAppHelpText()
	if err != nil {
		_, _ = fmt.Fprint(w, prefix)
		return normalizeHelpExitCode(app.Main([]string{"--help"}))
	}
	_, _ = fmt.Fprint(w, prefix)
	_, _ = fmt.Fprint(w, helpText)
	return normalizeHelpExitCode(code)
}

func topLevelHelpText(info topLevelHelpInfo) string {
	var b strings.Builder

	b.WriteString("trpc-claw\n\n")
	b.WriteString("Commands and subcommands:\n")
	for _, cmd := range commonHelpCommands(info) {
		_, _ = fmt.Fprintf(
			&b,
			"  %-58s %s\n",
			cmd.Command,
			cmd.Description,
		)
	}

	b.WriteString("\n")
	b.WriteString("Auto-detected paths now:\n")
	writeHelpPath(
		&b,
		"Binary",
		formatHelpPath(
			info.BinaryPath,
			"",
		),
	)
	writeHelpPath(
		&b,
		"OpenClaw config",
		formatHelpPath(
			info.OpenClawConfigPath,
			info.OpenClawConfigDefaultPath,
		),
	)
	writeHelpPath(
		&b,
		"tRPC config",
		formatHelpPath(
			info.TRPCConfigPath,
			info.TRPCConfigDefaultPath,
		),
	)
	writeHelpPath(
		&b,
		"state_dir",
		formatHelpPath(info.StateDir, ""),
	)

	b.WriteString("\n")
	b.WriteString("Notes:\n")
	b.WriteString(
		"  --profile on `trpc-claw upgrade` only takes effect " +
			"with `-f` or `--force-config`.\n",
	)
	b.WriteString(
		"  `upgrade -f --profile " +
			upgradeProfileWeComWS + "` will overwrite " +
			"~/.trpc-agent-go/openclaw/openclaw.yaml and " +
			"trpc_go.yaml with the default long-connection " +
			"profile.\n\n",
	)
	b.WriteString(
		"  `upgrade -f --profile " +
			upgradeProfileWeixin + "` will switch the " +
			"main config to the Weixin QR-login profile.\n\n",
	)
	b.WriteString("More help:\n")
	b.WriteString(
		"  `trpc-claw help inspect` shows the inspect " +
			"subcommands and examples.\n",
	)
	b.WriteString(
		"  `trpc-claw help bootstrap deps` shows the " +
			"dependency bootstrap flags and profiles.\n",
	)
	b.WriteString(
		"  `trpc-claw help pairing` shows Telegram " +
			"pairing workflows.\n\n",
	)
	return b.String()
}

func writeHelpPath(
	b *strings.Builder,
	label string,
	value string,
) {
	_, _ = fmt.Fprintf(b, "  %-16s %s\n", label+":", value)
}

func commonHelpCommands(info topLevelHelpInfo) []helpCommand {
	configPath := preferredHelpConfigPath(info)
	return []helpCommand{
		{
			Command:     "trpc-claw",
			Description: "Start with auto-detected config",
		},
		{
			Command:     "trpc-claw doctor",
			Description: "Run basic runtime and config checks",
		},
		{
			Command:     "trpc-claw inspect plugins",
			Description: "Show compiled channels, tools, and backends",
		},
		{
			Command:     "trpc-claw inspect deps --bundled",
			Description: "Check default bundled skill dependencies",
		},
		{
			Command: "trpc-claw inspect config-keys -config " +
				configPath,
			Description: "List supported YAML config keys",
		},
		{
			Command: "trpc-claw bootstrap deps --bundled " +
				"--apply",
			Description: "Install the safe default deps for " +
				"bundled skills",
		},
		{
			Command: "trpc-claw pairing list -config " +
				configPath,
			Description: "List pending Telegram pairing codes",
		},
		{
			Command: "trpc-claw pairing approve <CODE> " +
				"-config " + configPath,
			Description: "Approve a pending Telegram pairing code",
		},
		{
			Command:     "trpc-claw weixin login",
			Description: "Start Weixin QR login and save the account",
		},
		{
			Command:     "trpc-claw weixin list",
			Description: "List saved Weixin accounts and statuses",
		},
		{
			Command:     "trpc-claw upgrade",
			Description: "Upgrade binary, keep current config",
		},
		{
			Command: "trpc-claw upgrade -f --profile " +
				upgradeProfileWeComWS,
			Description: "Reset main config to default websocket " +
				"profile",
		},
		{
			Command: "trpc-claw upgrade -f --profile " +
				upgradeProfileWeixin,
			Description: "Reset main config to the Weixin profile",
		},
		{
			Command:     "trpc-claw version",
			Description: "Print the current installed version",
		},
		{
			Command:     "trpc-claw help inspect",
			Description: "Show detailed help for a command tree",
		},
		{
			Command: "curl -fsSL '" + installScriptURL + "' | " +
				"bash -s -- -f --profile " +
				upgradeProfileWeComWS,
			Description: "Remote reinstall and overwrite config",
		},
	}
}

func preferredHelpConfigPath(info topLevelHelpInfo) string {
	path := strings.TrimSpace(info.OpenClawConfigPath)
	if path != "" {
		return path
	}
	path = strings.TrimSpace(info.OpenClawConfigDefaultPath)
	if path != "" {
		return path
	}
	return "~/.trpc-agent-go/openclaw/openclaw.yaml"
}

func maybeHandleCommandHelp(
	w io.Writer,
	args []string,
) (bool, int) {
	if len(args) == 0 {
		return false, 0
	}

	root := strings.TrimSpace(args[0])
	switch root {
	case helpFlagShort, helpFlagLong:
		return true, printTopLevelHelp(w)
	case subcmdHelp:
		helpArgs := normalizeHelpArgs(args[1:])
		if len(helpArgs) == 0 {
			return true, printTopLevelHelp(w)
		}
		return true, printCommandHelp(w, helpArgs)
	}

	if !isKnownHelpRoot(root) || !containsHelpToken(args[1:]) {
		return false, 0
	}
	return true, printCommandHelp(w, normalizeHelpArgs(args))
}

func isKnownHelpRoot(raw string) bool {
	switch strings.TrimSpace(raw) {
	case subcmdInspect,
		subcmdBootstrap,
		subcmdPairing,
		subcmdDoctor,
		subcmdUpgrade,
		subcmdVersion:
		return true
	default:
		return false
	}
}

func containsHelpToken(args []string) bool {
	for _, arg := range args {
		if isHelpToken(arg) {
			return true
		}
	}
	return false
}

func isHelpToken(raw string) bool {
	switch strings.TrimSpace(raw) {
	case subcmdHelp, helpFlagShort, helpFlagLong:
		return true
	default:
		return false
	}
}

func normalizeHelpArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" || isHelpToken(arg) {
			continue
		}
		out = append(out, arg)
	}
	return out
}

func printCommandHelp(w io.Writer, args []string) int {
	info := resolveTopLevelHelpInfo()
	topic, ok := resolveHelpTopic(info, args)
	if !ok {
		_, _ = fmt.Fprintf(
			w,
			"Unknown help topic: %s\n\n",
			strings.Join(args, " "),
		)
		return printTopLevelHelp(w)
	}

	_, _ = fmt.Fprint(w, helpTopicText(topic))
	if len(topic.FlagHelpArgs) == 0 {
		return 0
	}

	flagText, code, err := captureCommandHelpText(topic.FlagHelpArgs)
	if err != nil || strings.TrimSpace(flagText) == "" {
		return 0
	}

	_, _ = fmt.Fprintln(w, "Flags:")
	_, _ = fmt.Fprint(w, indentHelpText(flagText, "  "))
	return normalizeHelpExitCode(code)
}

func resolveHelpTopic(
	info topLevelHelpInfo,
	args []string,
) (helpTopic, bool) {
	if len(args) == 0 {
		return helpTopic{}, false
	}

	switch strings.TrimSpace(args[0]) {
	case subcmdInspect:
		return resolveInspectHelpTopic(info, args[1:]), true
	case subcmdBootstrap:
		return resolveBootstrapHelpTopic(info, args[1:]), true
	case subcmdPairing:
		return resolvePairingHelpTopic(info, args[1:]), true
	case subcmdDoctor:
		return doctorHelpTopic(info), true
	case subcmdUpgrade:
		return upgradeHelpTopic(), true
	case subcmdVersion:
		return versionHelpTopic(), true
	case subcmdHelp:
		return helpHelpTopic(), true
	default:
		return helpTopic{}, false
	}
}

func resolveInspectHelpTopic(
	info topLevelHelpInfo,
	args []string,
) helpTopic {
	if len(args) == 0 {
		return inspectHelpTopic(info)
	}

	switch strings.TrimSpace(args[0]) {
	case inspectCmdPlugins:
		return inspectPluginsHelpTopic()
	case inspectCmdDeps:
		return inspectDepsHelpTopic(info)
	case inspectCmdConfigKeys:
		return inspectConfigKeysHelpTopic(info)
	default:
		return inspectHelpTopic(info)
	}
}

func resolveBootstrapHelpTopic(
	info topLevelHelpInfo,
	args []string,
) helpTopic {
	if len(args) == 0 {
		return bootstrapHelpTopic(info)
	}

	switch strings.TrimSpace(args[0]) {
	case bootstrapCmdDeps:
		return bootstrapDepsHelpTopic(info)
	default:
		return bootstrapHelpTopic(info)
	}
}

func resolvePairingHelpTopic(
	info topLevelHelpInfo,
	args []string,
) helpTopic {
	if len(args) == 0 {
		return pairingHelpTopic(info)
	}

	switch strings.TrimSpace(args[0]) {
	case pairingCmdList:
		return pairingListHelpTopic(info)
	case pairingCmdApprove:
		return pairingApproveHelpTopic(info)
	default:
		return pairingHelpTopic(info)
	}
}

func inspectHelpTopic(info topLevelHelpInfo) helpTopic {
	configPath := preferredHelpConfigPath(info)
	return helpTopic{
		Title: "trpc-claw inspect",
		Summary: "Inspect what the current binary supports " +
			"and what the current host is missing.",
		Usage: []string{
			"trpc-claw inspect plugins",
			"trpc-claw inspect deps [flags]",
			"trpc-claw inspect config-keys -config " +
				"<OPENCLAW_CONFIG>",
		},
		Subcommands: []helpCommand{
			{
				Command: inspectCmdPlugins,
				Description: "List compiled channels, models, " +
					"tool providers, and backends",
			},
			{
				Command: inspectCmdDeps,
				Description: "Check optional host binaries " +
					"and Python modules used by skills",
			},
			{
				Command: inspectCmdConfigKeys,
				Description: "Print supported YAML keys for " +
					"the current build",
			},
		},
		Examples: []string{
			"trpc-claw inspect plugins",
			"trpc-claw inspect deps",
			"trpc-claw inspect config-keys -config " +
				configPath,
		},
	}
}

func inspectPluginsHelpTopic() helpTopic {
	return helpTopic{
		Title: "trpc-claw inspect plugins",
		Summary: "List what was compiled into the current " +
			"trpc-claw binary.",
		Usage: []string{
			"trpc-claw inspect plugins",
		},
		Examples: []string{
			"trpc-claw inspect plugins",
		},
	}
}

func inspectDepsHelpTopic(info topLevelHelpInfo) helpTopic {
	stateDir := formatInspectablePath(info.StateDir)
	return helpTopic{
		Title: "trpc-claw inspect deps",
		Summary: "Check optional host dependencies used by " +
			"skills and local file workflows.",
		Usage: []string{
			"trpc-claw inspect deps [flags]",
		},
		Examples: []string{
			"trpc-claw inspect deps --bundled",
			"trpc-claw inspect deps --state-dir " + stateDir,
		},
		Notes: []string{
			"Use `--bundled` to inspect the default bundled " +
				"skill pack shipped with this release.",
			"`--bundled` expands to the current bundled " +
				"skills root and auto-adds `--profile " +
				defaultDepsProfile + "` unless you already " +
				"picked profiles yourself.",
		},
		FlagHelpArgs: []string{
			subcmdInspect,
			inspectCmdDeps,
			helpFlagLong,
		},
	}
}

func inspectConfigKeysHelpTopic(
	info topLevelHelpInfo,
) helpTopic {
	configPath := preferredHelpConfigPath(info)
	return helpTopic{
		Title: "trpc-claw inspect config-keys",
		Summary: "Print the YAML keys supported by the " +
			"current trpc-claw build.",
		Usage: []string{
			"trpc-claw inspect config-keys -config " +
				"<OPENCLAW_CONFIG>",
		},
		Examples: []string{
			"trpc-claw inspect config-keys -config " +
				configPath,
		},
		FlagHelpArgs: []string{
			subcmdInspect,
			inspectCmdConfigKeys,
			helpFlagLong,
		},
	}
}

func bootstrapHelpTopic(info topLevelHelpInfo) helpTopic {
	stateDir := formatInspectablePath(info.StateDir)
	return helpTopic{
		Title: "trpc-claw bootstrap",
		Summary: "Install optional host dependencies used " +
			"by skills and file-processing tools.",
		Usage: []string{
			"trpc-claw bootstrap deps [flags]",
		},
		Subcommands: []helpCommand{
			{
				Command: bootstrapCmdDeps,
				Description: "Install dependency profiles " +
					"such as file and office helpers",
			},
		},
		Examples: []string{
			"trpc-claw bootstrap deps --bundled --apply",
			"trpc-claw bootstrap deps --state-dir " +
				stateDir + " --profile pdf,office --apply",
		},
	}
}

func bootstrapDepsHelpTopic(info topLevelHelpInfo) helpTopic {
	stateDir := formatInspectablePath(info.StateDir)
	return helpTopic{
		Title: "trpc-claw bootstrap deps",
		Summary: "Install one or more dependency profiles " +
			"onto the current host.",
		Usage: []string{
			"trpc-claw bootstrap deps [flags]",
		},
		Examples: []string{
			"trpc-claw bootstrap deps --bundled --apply",
			"trpc-claw bootstrap deps --state-dir " +
				stateDir + " --profile pdf,office --apply",
		},
		Notes: []string{
			"`--bundled` targets the bundled skill pack " +
				"installed under the current state dir.",
			"It is intentionally conservative: it only " +
				"plans safe system packages and managed " +
				"Python packages. Browser runtimes, global " +
				"npm tools, and credentials still stay " +
				"manual.",
		},
		FlagHelpArgs: []string{
			subcmdBootstrap,
			bootstrapCmdDeps,
			helpFlagLong,
		},
	}
}

func pairingHelpTopic(info topLevelHelpInfo) helpTopic {
	configPath := preferredHelpConfigPath(info)
	stateDir := formatInspectablePath(info.StateDir)
	return helpTopic{
		Title: "trpc-claw pairing",
		Summary: "Manage Telegram DM pairing approvals " +
			"when a Telegram channel uses pairing mode.",
		Usage: []string{
			"trpc-claw pairing list -config <CONFIG> " +
				"[-state-dir <DIR>] [-channel <NAME>]",
			"trpc-claw pairing approve <CODE> " +
				"-config <CONFIG> [-state-dir <DIR>] " +
				"[-channel <NAME>]",
		},
		Subcommands: []helpCommand{
			{
				Command: pairingCmdList,
				Description: "List pending Telegram pairing " +
					"codes",
			},
			{
				Command: pairingCmdApprove + " <CODE>",
				Description: "Approve a pending Telegram " +
					"pairing code",
			},
		},
		Examples: []string{
			"trpc-claw pairing list -config " + configPath,
			"trpc-claw pairing approve 123456 -config " +
				configPath + " -state-dir " + stateDir,
		},
		Notes: []string{
			"These commands are only relevant when a " +
				"Telegram channel uses pairing-based DM " +
				"access control.",
		},
		FlagHelpArgs: []string{
			subcmdPairing,
			helpFlagLong,
		},
	}
}

func pairingListHelpTopic(info topLevelHelpInfo) helpTopic {
	configPath := preferredHelpConfigPath(info)
	return helpTopic{
		Title:   "trpc-claw pairing list",
		Summary: "List pending Telegram pairing requests.",
		Usage: []string{
			"trpc-claw pairing list -config <CONFIG> " +
				"[-state-dir <DIR>] [-channel <NAME>]",
		},
		Examples: []string{
			"trpc-claw pairing list -config " + configPath,
		},
		FlagHelpArgs: []string{
			subcmdPairing,
			helpFlagLong,
		},
	}
}

func pairingApproveHelpTopic(
	info topLevelHelpInfo,
) helpTopic {
	configPath := preferredHelpConfigPath(info)
	return helpTopic{
		Title: "trpc-claw pairing approve",
		Summary: "Approve one pending Telegram pairing " +
			"code.",
		Usage: []string{
			"trpc-claw pairing approve <CODE> -config " +
				"<CONFIG> [-state-dir <DIR>] " +
				"[-channel <NAME>]",
		},
		Examples: []string{
			"trpc-claw pairing approve 123456 -config " +
				configPath,
		},
		FlagHelpArgs: []string{
			subcmdPairing,
			helpFlagLong,
		},
	}
}

func doctorHelpTopic(info topLevelHelpInfo) helpTopic {
	configPath := preferredHelpConfigPath(info)
	return helpTopic{
		Title: "trpc-claw doctor",
		Summary: "Run basic runtime, config, and host " +
			"sanity checks before first launch or release.",
		Usage: []string{
			"trpc-claw doctor [flags]",
		},
		Examples: []string{
			"trpc-claw doctor",
			"trpc-claw doctor -config " + configPath,
		},
		FlagHelpArgs: []string{
			subcmdDoctor,
			helpFlagLong,
		},
	}
}

func upgradeHelpTopic() helpTopic {
	return helpTopic{
		Title: "trpc-claw upgrade",
		Summary: "Upgrade the current binary to the " +
			"latest mirror release, or install a specified " +
			"release tag.",
		Usage: []string{
			"trpc-claw upgrade " + upgradeFlagVersionUsage + " " +
				upgradeFlagChannelUsage + " " +
				upgradeFlagForceConfigUsage +
				" [--profile <name>]",
		},
		Examples: []string{
			"trpc-claw upgrade",
			upgradeVersionExample,
			"trpc-claw upgrade --channel " +
				releaseinfo.ChannelPreview,
			"trpc-claw upgrade -f",
			"trpc-claw upgrade -f --profile " +
				upgradeProfileWeComWS,
			"trpc-claw upgrade -f --profile " +
				upgradeProfileWeixin,
		},
		Notes: []string{
			"`--version` installs that exact release tag " +
				"instead of resolving the latest mirror " +
				"version.",
			"`--channel preview` resolves preview/VERSION; " +
				"the default channel remains latest.",
			"`--profile` only takes effect together with " +
				"`" + upgradeFlagForceConfigHelp + "`.",
			"`" + upgradeFlagForceConfigHelp + "` will " +
				"overwrite the main " +
				"openclaw.yaml and trpc_go.yaml files.",
		},
		FlagHelpArgs: []string{
			subcmdUpgrade,
			helpFlagLong,
		},
	}
}

func versionHelpTopic() helpTopic {
	return helpTopic{
		Title: "trpc-claw version",
		Summary: "Print the current installed release " +
			"version.",
		Usage: []string{
			"trpc-claw version",
			"trpc-claw --version",
		},
	}
}

func helpHelpTopic() helpTopic {
	return helpTopic{
		Title: "trpc-claw help",
		Summary: "Show detailed help for a command or a " +
			"command tree.",
		Usage: []string{
			"trpc-claw help",
			"trpc-claw help inspect",
			"trpc-claw help bootstrap deps",
			"trpc-claw help pairing approve",
		},
		Examples: []string{
			"trpc-claw help inspect",
			"trpc-claw help bootstrap deps",
			"trpc-claw help pairing",
		},
	}
}

func helpTopicText(topic helpTopic) string {
	var b strings.Builder

	_, _ = fmt.Fprintf(&b, "%s\n\n", topic.Title)
	if summary := strings.TrimSpace(topic.Summary); summary != "" {
		_, _ = fmt.Fprintf(&b, "%s\n\n", summary)
	}
	writeHelpLines(&b, "Usage", topic.Usage)
	writeHelpCommands(&b, "Subcommands", topic.Subcommands)
	writeHelpLines(&b, "Examples", topic.Examples)
	writeHelpLines(&b, "Notes", topic.Notes)
	return b.String()
}

func writeHelpLines(
	b *strings.Builder,
	title string,
	lines []string,
) {
	if len(lines) == 0 {
		return
	}
	_, _ = fmt.Fprintf(b, "%s:\n", title)
	for _, line := range lines {
		_, _ = fmt.Fprintf(b, "  %s\n", strings.TrimSpace(line))
	}
	_, _ = fmt.Fprintln(b)
}

func writeHelpCommands(
	b *strings.Builder,
	title string,
	commands []helpCommand,
) {
	if len(commands) == 0 {
		return
	}
	_, _ = fmt.Fprintf(b, "%s:\n", title)
	for _, cmd := range commands {
		_, _ = fmt.Fprintf(
			b,
			"  %-18s %s\n",
			strings.TrimSpace(cmd.Command),
			strings.TrimSpace(cmd.Description),
		)
	}
	_, _ = fmt.Fprintln(b)
}

func captureCommandHelpText(
	args []string,
) (string, int, error) {
	if len(args) == 0 {
		return "", 0, nil
	}
	if strings.TrimSpace(args[0]) == subcmdUpgrade {
		var b strings.Builder
		printUpgradeUsage(&b)
		return b.String(), 0, nil
	}
	return captureAppCommandHelpText(args)
}

func indentHelpText(text string, prefix string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n") + "\n"
}

func formatInspectablePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "~/.trpc-agent-go/openclaw"
	}
	return path
}

func resolveTopLevelHelpInfo() topLevelHelpInfo {
	_, paths, err := normalizeOpenClawArgsWithPaths(nil)
	if err != nil {
		paths = startupPaths{StateDir: defaultStateDir()}
	}
	trpcPath, err := resolveTRPCConfigPath("")
	if err == nil {
		paths.TRPCConfigPath = trpcPath
	}
	return topLevelHelpInfo{
		BinaryPath:                currentExecutablePath(),
		OpenClawConfigPath:        paths.OpenClawConfigPath,
		OpenClawConfigDefaultPath: defaultConfigPath(),
		TRPCConfigPath:            paths.TRPCConfigPath,
		TRPCConfigDefaultPath:     defaultTRPCConfigPath(),
		StateDir:                  paths.StateDir,
	}
}

func formatHelpPath(path string, fallback string) string {
	path = strings.TrimSpace(path)
	fallback = strings.TrimSpace(fallback)
	switch {
	case path != "":
		return pathStatus(path)
	case fallback != "":
		return pathStatus(fallback) + " (default)"
	default:
		return "<none>"
	}
}

func pathStatus(path string) string {
	if strings.TrimSpace(path) == "" {
		return "<none>"
	}
	info, err := os.Stat(path)
	switch {
	case err == nil && info != nil:
		return path + " (exists)"
	case os.IsNotExist(err):
		return path + " (missing)"
	default:
		return path
	}
}

func captureAppHelpText() (string, int, error) {
	return captureAppCommandHelpText([]string{helpFlagLong})
}

func captureAppCommandHelpText(
	args []string,
) (string, int, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return "", 0, err
	}

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	oldFlagOutput := flag.CommandLine.Output()
	os.Stdout = writer
	os.Stderr = writer
	flag.CommandLine.SetOutput(writer)

	code := app.Main(args)

	flag.CommandLine.SetOutput(oldFlagOutput)
	os.Stdout = oldStdout
	os.Stderr = oldStderr
	_ = writer.Close()

	data, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if readErr != nil {
		return "", code, readErr
	}
	return rewriteHelpBinaryName(string(data)), code, nil
}

func rewriteHelpBinaryName(text string) string {
	replacer := strings.NewReplacer(
		"Usage of openclaw:",
		"Usage of trpc-claw:",
		"Usage of inspect deps:",
		"Usage of trpc-claw inspect deps:",
		"Usage of inspect config-keys:",
		"Usage of trpc-claw inspect config-keys:",
		"Usage of bootstrap deps:",
		"Usage of trpc-claw bootstrap deps:",
		"Usage of pairing:",
		"Usage of trpc-claw pairing:",
		"Usage of doctor:",
		"Usage of trpc-claw doctor:",
		"Usage of weixin:",
		"Usage of trpc-claw weixin:",
		"openclaw inspect",
		"trpc-claw inspect",
		"openclaw bootstrap",
		"trpc-claw bootstrap",
		"openclaw pairing",
		"trpc-claw pairing",
		"openclaw doctor",
		"trpc-claw doctor",
		"openclaw weixin",
		"trpc-claw weixin",
		"openclaw flags",
		"trpc-claw flags",
	)
	return replacer.Replace(text)
}

func isTopLevelWeixinCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	return strings.TrimSpace(args[0]) == subcmdWeixin
}

func runWeixinCommand(args []string, globalStateDir string) int {
	return runWeixinCommandWithIO(
		args,
		globalStateDir,
		os.Stdout,
		os.Stderr,
	)
}

func runWeixinCommandWithIO(
	args []string,
	globalStateDir string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	if len(args) == 0 {
		return printWeixinHelp(stdout)
	}

	stateDir := weixinchannel.ResolveStateDir(globalStateDir, "")
	switch strings.TrimSpace(args[0]) {
	case subcmdHelp, helpFlagShort, helpFlagLong:
		return printWeixinHelp(stdout)
	case weixinCmdLogin:
		flags := flag.NewFlagSet(weixinCmdLogin, flag.ContinueOnError)
		flags.SetOutput(stderr)
		baseURL := flags.String("base-url", "", "Weixin API base URL")
		botType := flags.String(
			"bot-type",
			weixinDefaultLoginBotType,
			"Weixin login bot type",
		)
		timeout := flags.Duration(
			"timeout",
			weixinDefaultLoginTimeout,
			"QR login timeout",
		)
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}

		ctx, cancel := context.WithTimeout(
			context.Background(),
			*timeout,
		)
		defer cancel()

		account, err := weixinchannel.LoginWithQR(
			ctx,
			stateDir,
			*baseURL,
			*botType,
			weixinchannel.LoginCallbacks{
				OnQRCode: func(qrURL string) {
					_, _ = fmt.Fprintln(
						stdout,
						"Weixin QR URL:",
					)
					_, _ = fmt.Fprintln(stdout, qrURL)
				},
				OnStatus: func(status string) {
					_, _ = fmt.Fprintf(
						stdout,
						"Login status: %s\n",
						status,
					)
				},
			},
		)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}

		_, _ = fmt.Fprintf(
			stdout,
			"Saved Weixin account %s\n",
			account.AccountID,
		)
		return 0
	case weixinCmdList:
		accounts, err := weixinchannel.ListAccounts(stateDir)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
		if len(accounts) == 0 {
			_, _ = fmt.Fprintf(
				stdout,
				"No Weixin accounts under %s\n",
				stateDir,
			)
			return 0
		}
		for _, account := range accounts {
			status, err := weixinchannel.LoadRuntimeStatus(
				stateDir,
				account.AccountID,
			)
			if err != nil {
				_, _ = fmt.Fprintf(
					stdout,
					"%s token=present paused=? error=%v\n",
					account.AccountID,
					err,
				)
				continue
			}
			paused := "no"
			if status.PausedUntil != nil {
				paused = status.PausedUntil.Format(time.RFC3339)
			}
			_, _ = fmt.Fprintf(
				stdout,
				"%s user=%s paused=%s last_error=%s\n",
				account.AccountID,
				strings.TrimSpace(account.UserID),
				paused,
				strings.TrimSpace(status.LastError),
			)
		}
		return 0
	case weixinCmdRemove:
		if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
			_, _ = fmt.Fprintln(
				stderr,
				"usage: trpc-claw weixin remove <ACCOUNT_ID>",
			)
			return 2
		}
		if err := weixinchannel.RemoveAccount(stateDir, args[1]); err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
		_, _ = fmt.Fprintf(
			stdout,
			"Removed Weixin account %s\n",
			strings.TrimSpace(args[1]),
		)
		return 0
	default:
		return printWeixinHelp(stdout)
	}
}

func printWeixinHelp(w io.Writer) int {
	_, _ = fmt.Fprintln(w, "trpc-claw weixin")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Commands:")
	_, _ = fmt.Fprintln(
		w,
		"  trpc-claw weixin login [--base-url URL] [--timeout 8m]",
	)
	_, _ = fmt.Fprintln(w, "  trpc-claw weixin list")
	_, _ = fmt.Fprintln(w, "  trpc-claw weixin remove <ACCOUNT_ID>")
	return 0
}

func buildServiceMuxes(rt *app.Runtime) (map[string]*http.ServeMux, error) {
	if rt == nil {
		return nil, fmt.Errorf("nil runtime")
	}
	if rt.Gateway.Handler == nil {
		return nil, fmt.Errorf("nil gateway handler")
	}

	gatewayMux := http.NewServeMux()
	gatewayMux.Handle("/", rt.Gateway.Handler)
	a2aSurface, hasA2A, err := extractRuntimeA2ASurface(rt)
	if err != nil {
		return nil, fmt.Errorf("extract a2a surface: %w", err)
	}
	if hasA2A {
		if err := mountA2ASurface(a2aSurface, gatewayMux); err != nil {
			return nil, fmt.Errorf("mount a2a surface: %w", err)
		}
	}

	muxes := map[string]*http.ServeMux{
		trpcServiceName: gatewayMux,
	}

	for _, ch := range rt.Channels {
		mounter, ok := ch.(ingress.HTTPIngress)
		if !ok {
			continue
		}

		service := strings.TrimSpace(mounter.HTTPServiceName())
		if service == "" {
			service = trpcServiceName
		}

		mux := muxes[service]
		if mux == nil {
			mux = http.NewServeMux()
			muxes[service] = mux
		}

		if err := mountHTTPIngress(mounter, mux); err != nil {
			return nil, fmt.Errorf(
				"mount HTTP ingress for channel %q: %w",
				ch.ID(),
				err,
			)
		}
		for _, pattern := range mounter.HTTPPatterns() {
			tlog.Infof(
				"Mounted channel %q HTTP ingress: %s on %q",
				ch.ID(),
				strings.TrimSpace(pattern),
				service,
			)
		}
	}

	return muxes, nil
}

func mountHTTPIngress(
	mounter ingress.HTTPIngress,
	mux *http.ServeMux,
) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic while mounting HTTP ingress: %v", r)
		}
	}()
	return mounter.MountHTTP(mux)
}

func mountA2ASurface(
	surface runtimeA2ASurface,
	mux *http.ServeMux,
) error {
	if mux == nil {
		return fmt.Errorf("nil a2a mux")
	}
	if surface.Handler == nil {
		return fmt.Errorf("nil a2a handler")
	}

	basePath := strings.TrimSpace(surface.BasePath)
	basePath = strings.TrimRight(basePath, "/")
	if basePath == "" || basePath == "/" {
		return fmt.Errorf("invalid a2a base path %q", surface.BasePath)
	}
	if !strings.HasPrefix(basePath, "/") {
		basePath = "/" + basePath
	}

	mux.Handle(basePath, surface.Handler)
	mux.Handle(basePath+"/", surface.Handler)
	return nil
}

func extractRuntimeA2ASurface(
	runtime any,
) (runtimeA2ASurface, bool, error) {
	if runtime == nil {
		return runtimeA2ASurface{}, false, nil
	}

	value := reflect.ValueOf(runtime)
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return runtimeA2ASurface{}, false, nil
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return runtimeA2ASurface{}, false, fmt.Errorf(
			"runtime must be a struct or pointer to struct",
		)
	}

	field := value.FieldByName("A2A")
	if !field.IsValid() {
		return runtimeA2ASurface{}, false, nil
	}
	if field.Kind() == reflect.Pointer {
		if field.IsNil() {
			return runtimeA2ASurface{}, false, nil
		}
		field = field.Elem()
	}
	if field.Kind() != reflect.Struct {
		return runtimeA2ASurface{}, false, fmt.Errorf(
			"runtime A2A field must be a struct",
		)
	}

	handlerField := field.FieldByName("Handler")
	if !handlerField.IsValid() || handlerField.IsZero() {
		return runtimeA2ASurface{}, false, nil
	}
	handler, ok := handlerField.Interface().(http.Handler)
	if !ok {
		return runtimeA2ASurface{}, false, fmt.Errorf(
			"runtime A2A handler has unexpected type",
		)
	}

	basePath, err := extractRuntimeA2AString(field, "BasePath")
	if err != nil {
		return runtimeA2ASurface{}, false, err
	}
	cardPath, err := extractRuntimeA2AString(field, "AgentCardPath")
	if err != nil {
		return runtimeA2ASurface{}, false, err
	}

	return runtimeA2ASurface{
		Handler:       handler,
		BasePath:      basePath,
		AgentCardPath: cardPath,
	}, true, nil
}

func extractRuntimeA2AString(
	field reflect.Value,
	name string,
) (string, error) {
	part := field.FieldByName(name)
	if !part.IsValid() {
		return "", fmt.Errorf("runtime A2A %s is missing", name)
	}
	if part.Kind() != reflect.String {
		return "", fmt.Errorf(
			"runtime A2A %s must be a string",
			name,
		)
	}
	return part.String(), nil
}

func stripTRPCConf(args []string) (string, []string, error) {
	var (
		confPath string
		cleaned  []string
	)

	cleaned = make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		raw := args[i]
		value, ok := matchFlagValue(raw, flagConf)
		if !ok {
			cleaned = append(cleaned, raw)
			continue
		}

		if value == "" {
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf(
					"flag %q requires a value",
					flagConf,
				)
			}
			value = args[i+1]
			i++
		}

		confPath = value
	}

	return strings.TrimSpace(confPath), cleaned, nil
}

func sortedMapKeys[V any](m map[string]V) []string {
	if len(m) == 0 {
		return nil
	}

	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

type stateDirConfig struct {
	StateDir string `yaml:"state_dir,omitempty"`
}

type startupPaths struct {
	TRPCConfigPath     string
	OpenClawConfigPath string
	StateDir           string
}

func normalizeOpenClawArgs(args []string) ([]string, error) {
	normalized, _, err := normalizeOpenClawArgsWithPaths(args)
	if err != nil {
		return nil, err
	}
	return normalized, nil
}

func normalizeOpenClawArgsWithPaths(
	args []string,
) ([]string, startupPaths, error) {
	normalized := append([]string(nil), args...)
	paths := startupPaths{
		StateDir: defaultStateDir(),
	}

	var err error
	var stateDir string
	normalized, stateDir, err = normalizeUserPathFlag(
		normalized,
		flagStateDir,
	)
	if err != nil {
		return nil, startupPaths{}, err
	}
	if stateDir != "" {
		paths.StateDir = stateDir
	}

	var cfgPath string
	normalized, cfgPath, err = normalizeConfigPathFlag(normalized)
	if err != nil {
		return nil, startupPaths{}, err
	}
	if cfgPath == "" {
		cfgPath = defaultConfigPathIfExists()
	}
	paths.OpenClawConfigPath = cfgPath
	if hasFlag(normalized, flagStateDir) {
		normalized, err = expandBundledDepsArgs(
			normalized,
			paths.StateDir,
		)
		if err != nil {
			return nil, startupPaths{}, err
		}
		return normalized, paths, nil
	}
	if strings.TrimSpace(cfgPath) == "" {
		normalized, err = expandBundledDepsArgs(
			normalized,
			paths.StateDir,
		)
		if err != nil {
			return nil, startupPaths{}, err
		}
		return normalized, paths, nil
	}

	stateDir, err = loadStateDirFromConfig(cfgPath)
	if err != nil {
		return nil, startupPaths{}, err
	}
	if stateDir == "" {
		return normalized, paths, nil
	}
	expanded, changed, err := expandUserPath(stateDir)
	if err != nil {
		return nil, startupPaths{}, err
	}
	if len(normalized) > 0 &&
		strings.TrimSpace(normalized[0]) == subcmdInspect {
		if changed {
			paths.StateDir = expanded
		} else {
			paths.StateDir = stateDir
		}
		normalized, err = expandBundledDepsArgs(
			normalized,
			paths.StateDir,
		)
		if err != nil {
			return nil, startupPaths{}, err
		}
		return normalized, paths, nil
	}
	if len(normalized) > 0 &&
		strings.TrimSpace(normalized[0]) == subcmdWeixin {
		if changed {
			paths.StateDir = expanded
		} else {
			paths.StateDir = stateDir
		}
		normalized, err = expandBundledDepsArgs(
			normalized,
			paths.StateDir,
		)
		if err != nil {
			return nil, startupPaths{}, err
		}
		return normalized, paths, nil
	}
	if changed {
		normalized = setFlagValue(normalized, flagStateDir, expanded)
		stateDir = expanded
	}
	paths.StateDir = stateDir

	normalized, err = expandBundledDepsArgs(normalized, paths.StateDir)
	if err != nil {
		return nil, startupPaths{}, err
	}
	return normalized, paths, nil
}

func expandBundledDepsArgs(
	args []string,
	stateDir string,
) ([]string, error) {
	if !isDepsSubcommand(args) {
		return args, nil
	}

	normalized, bundled, err := consumeBoolFlag(args, flagBundled)
	if err != nil || !bundled {
		return normalized, err
	}

	if err := rejectBundledDepsOverrides(normalized); err != nil {
		return nil, err
	}

	root, err := resolveBundledSkillsRoot(stateDir)
	if err != nil {
		return nil, err
	}
	names, err := bundledDepsSkillNames(root)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, fmt.Errorf(
			"no bundled skills with metadata.%s found under %q",
			openClawMetadataKey,
			root,
		)
	}

	normalized = setFlagValue(normalized, flagSkillsRoot, root)
	normalized = setFlagValue(
		normalized,
		flagSkill,
		strings.Join(names, ","),
	)
	if !hasFlag(normalized, flagProfile) {
		normalized = setFlagValue(
			normalized,
			flagProfile,
			defaultDepsProfile,
		)
	}
	return normalized, nil
}

func isDepsSubcommand(args []string) bool {
	if len(args) < 2 {
		return false
	}

	root := strings.TrimSpace(args[0])
	cmd := strings.TrimSpace(args[1])
	switch root {
	case subcmdBootstrap, subcmdInspect:
		return cmd == bootstrapCmdDeps || cmd == inspectCmdDeps
	default:
		return false
	}
}

func consumeBoolFlag(
	args []string,
	name string,
) ([]string, bool, error) {
	out := make([]string, 0, len(args))
	seen := false
	enabled := false

	for _, raw := range args {
		value, ok := matchFlagValue(raw, name)
		if !ok {
			out = append(out, raw)
			continue
		}

		seen = true
		switch raw {
		case "-" + name, "--" + name:
			enabled = true
		default:
			parsed, err := parseBoolFlagValue(name, value)
			if err != nil {
				return nil, false, err
			}
			enabled = parsed
		}
	}

	if !seen {
		return args, false, nil
	}
	return out, enabled, nil
}

func parseBoolFlagValue(
	name string,
	value string,
) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "on", "true", "yes":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf(
			"flag %q expects a boolean value",
			name,
		)
	}
}

func rejectBundledDepsOverrides(args []string) error {
	var conflicts []string
	for _, name := range []string{
		flagSkill,
		flagSkillsRoot,
		flagSkillsExtraDirs,
		flagSkillsAllowBundled,
	} {
		if hasFlag(args, name) {
			conflicts = append(conflicts, "--"+name)
		}
	}
	if len(conflicts) == 0 {
		return nil
	}
	return fmt.Errorf(
		"`--%s` cannot be combined with %s",
		flagBundled,
		strings.Join(conflicts, ", "),
	)
}

func resolveBundledSkillsRoot(stateDir string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}
	candidates := bundledSkillsRootCandidates(
		stateDir,
		os.Getenv(runtimeStateDirEnvName),
		cwd,
		sudoUserDefaultStateDir(),
	)

	for _, candidate := range candidates {
		if isExistingDir(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf(
		"cannot find bundled skills root; checked %s",
		strings.Join(candidates, ", "),
	)
}

func bundledSkillsRootCandidates(
	stateDir string,
	runtimeStateDir string,
	cwd string,
	sudoStateDir string,
) []string {
	candidates := make([]string, 0, 5)
	candidates = appendBundledSkillsRootCandidate(
		candidates,
		stateDir,
	)
	candidates = appendBundledSkillsRootCandidate(
		candidates,
		runtimeStateDir,
	)
	candidates = appendBundledSkillsRootCandidate(
		candidates,
		sudoStateDir,
	)
	if dir := strings.TrimSpace(cwd); dir != "" {
		candidates = appendUniquePath(
			candidates,
			filepath.Join(dir, skillsDirName),
		)
		candidates = appendUniquePath(
			candidates,
			filepath.Join(
				dir,
				defaultConfigAppDir,
				skillsDirName,
			),
		)
	}
	return candidates
}

func appendBundledSkillsRootCandidate(
	candidates []string,
	stateDir string,
) []string {
	dir := strings.TrimSpace(stateDir)
	if dir == "" {
		return candidates
	}
	return appendUniquePath(
		candidates,
		filepath.Join(
			dir,
			skillsDirName,
			bundledSkillsDirName,
		),
	)
}

func appendUniquePath(paths []string, path string) []string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return paths
	}
	for _, existing := range paths {
		if existing == trimmed {
			return paths
		}
	}
	return append(paths, trimmed)
}

func sudoUserDefaultStateDir() string {
	name := strings.TrimSpace(os.Getenv(sudoUserEnvName))
	if name == "" {
		return ""
	}
	info, err := lookupUserFunc(name)
	if err != nil || info == nil {
		return ""
	}
	home := strings.TrimSpace(info.HomeDir)
	if home == "" {
		return ""
	}
	return filepath.Join(
		home,
		defaultConfigRootDir,
		defaultConfigAppDir,
	)
}

func isExistingDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info != nil && info.IsDir()
}

func bundledDepsSkillNames(root string) ([]string, error) {
	repo, err := skill.NewFSRepository(root)
	if err != nil {
		return nil, err
	}

	summaries := repo.Summaries()
	names := make([]string, 0, len(summaries))
	for _, summary := range summaries {
		name := strings.TrimSpace(summary.Name)
		if name == "" {
			continue
		}

		skillDir, err := repo.Path(name)
		if err != nil {
			return nil, err
		}
		ok, err := skillEligibleForBundledDeps(skillDir)
		if err != nil {
			return nil, err
		}
		if ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

func skillEligibleForBundledDeps(skillDir string) (bool, error) {
	data, err := os.ReadFile(
		filepath.Join(skillDir, skillDocFileName),
	)
	if err != nil {
		return false, err
	}

	meta, ok, err := parseSkillMetadata(string(data))
	if err != nil || !ok {
		return false, err
	}
	rawOpenClawMeta, found := meta[openClawMetadataKey]
	if !found {
		return false, nil
	}
	openClawMeta := normalizeSkillMetadata(rawOpenClawMeta)
	if len(openClawMeta) == 0 {
		return false, nil
	}
	if !bundledDepsAllowedOnCurrentOS(openClawMeta) {
		return false, nil
	}
	return true, nil
}

func parseSkillMetadata(
	content string,
) (map[string]any, bool, error) {
	raw, ok := parseFrontMatterBlock(content)
	if !ok {
		return nil, false, nil
	}

	values := map[string]any{}
	if err := yaml.Unmarshal([]byte(raw), &values); err != nil {
		return nil, false, err
	}
	meta := normalizeSkillMetadata(values[skillMetadataKey])
	if len(meta) == 0 {
		return nil, false, nil
	}
	return meta, true, nil
}

func parseFrontMatterBlock(content string) (string, bool) {
	text := strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return "", false
	}

	idx := strings.Index(text[4:], "\n---\n")
	if idx < 0 {
		return "", false
	}
	return text[4 : 4+idx], true
}

func normalizeSkillMetadata(v any) map[string]any {
	out := normalizeStringAnyMap(v)
	if len(out) > 0 {
		return out
	}

	text, ok := v.(string)
	if !ok {
		return nil
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	m := map[string]any{}
	if err := yaml.Unmarshal([]byte(text), &m); err != nil {
		return nil
	}
	return normalizeStringAnyMap(m)
}

func normalizeStringAnyMap(v any) map[string]any {
	switch typed := v.(type) {
	case map[string]any:
		return typed
	case map[any]any:
		out := make(map[string]any, len(typed))
		for key, value := range typed {
			name, ok := key.(string)
			if !ok {
				continue
			}
			out[name] = value
		}
		return out
	default:
		return nil
	}
}

func bundledDepsAllowedOnCurrentOS(meta map[string]any) bool {
	if len(meta) == 0 {
		return true
	}

	allowed := normalizeSkillMetadataStrings(meta[skillMetadataOSKey])
	if len(allowed) == 0 {
		return true
	}

	current := normalizeSkillMetadataOS(runtime.GOOS)
	for _, raw := range allowed {
		if normalizeSkillMetadataOS(raw) == current {
			return true
		}
	}
	return false
}

func normalizeSkillMetadataStrings(v any) []string {
	switch typed := v.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, raw := range typed {
			value := strings.TrimSpace(raw)
			if value == "" {
				continue
			}
			out = append(out, value)
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, raw := range typed {
			value, ok := raw.(string)
			if !ok {
				continue
			}
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			out = append(out, value)
		}
		return out
	case string:
		value := strings.TrimSpace(typed)
		if value == "" {
			return nil
		}
		return []string{value}
	default:
		return nil
	}
}

func normalizeSkillMetadataOS(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "win32" {
		return "windows"
	}
	return value
}

func normalizeConfigPathFlag(args []string) ([]string, string, error) {
	normalized, cfgPath, err := normalizeUserPathFlag(args, flagConfig)
	if err != nil {
		return nil, "", err
	}
	if strings.TrimSpace(cfgPath) != "" {
		return normalized, cfgPath, nil
	}

	raw := strings.TrimSpace(os.Getenv(openClawConfigEnvName))
	if raw == "" {
		return normalized, "", nil
	}
	expanded, changed, err := expandUserPath(raw)
	if err != nil {
		return nil, "", err
	}
	if !changed {
		return normalized, raw, nil
	}
	return setFlagValue(normalized, flagConfig, expanded), expanded, nil
}

func resolveSourceOpenClawConfigPath(
	rawArgs []string,
	fallback string,
) (string, error) {
	envPath, ok, err := resolveSourceConfigPathFromEnv()
	if err != nil {
		return "", err
	}
	if ok {
		return envPath, nil
	}
	_, cfgPath, err := normalizeConfigPathFlag(rawArgs)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(cfgPath) != "" {
		return strings.TrimSpace(cfgPath), nil
	}
	return strings.TrimSpace(fallback), nil
}

func resolveSourceConfigPathFromEnv() (string, bool, error) {
	raw := strings.TrimSpace(os.Getenv(adminSourceConfigPathEnv))
	if raw == "" {
		return "", false, nil
	}

	expanded, changed, err := expandUserPath(raw)
	if err != nil {
		return "", false, err
	}
	if !changed {
		return raw, true, nil
	}
	return expanded, true, nil
}

func normalizeUserPathFlag(
	args []string,
	name string,
) ([]string, string, error) {
	value, ok, err := flagValueFromArgs(args, name)
	if err != nil || !ok {
		return args, "", err
	}
	expanded, changed, err := expandUserPath(value)
	if err != nil {
		return nil, "", err
	}
	if !changed {
		return args, value, nil
	}
	return setFlagValue(args, name, expanded), expanded, nil
}

func flagValueFromArgs(
	args []string,
	name string,
) (string, bool, error) {
	for i := 0; i < len(args); i++ {
		raw := args[i]
		value, ok := matchFlagValue(raw, name)
		if !ok {
			continue
		}
		if value != "" {
			return strings.TrimSpace(value), true, nil
		}
		if i+1 >= len(args) {
			return "", false, fmt.Errorf(
				"flag %q requires a value",
				name,
			)
		}
		return strings.TrimSpace(args[i+1]), true, nil
	}
	return "", false, nil
}

func hasFlag(args []string, name string) bool {
	_, ok, err := flagValueFromArgs(args, name)
	return err == nil && ok
}

func setFlagValue(args []string, name string, value string) []string {
	out := make([]string, 0, len(args)+2)
	matched := false
	short := "-" + name

	for i := 0; i < len(args); i++ {
		raw := args[i]
		current, ok := matchFlagValue(raw, name)
		if !ok {
			out = append(out, raw)
			continue
		}
		matched = true
		if current == "" && (raw == short || raw == "--"+name) {
			out = append(out, short, value)
			if i+1 < len(args) {
				i++
			}
			continue
		}
		out = append(out, short+"="+value)
	}
	if matched {
		return out
	}
	return append(out, short, value)
}

func expandUserPath(raw string) (string, bool, error) {
	path := strings.TrimSpace(raw)
	if path == "" {
		return "", false, nil
	}
	switch {
	case path == "~":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false, err
		}
		return home, true, nil
	case strings.HasPrefix(path, "~/"):
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false, err
		}
		return filepath.Join(
			home,
			strings.TrimPrefix(path, "~/"),
		), true, nil
	default:
		return path, false, nil
	}
}

func loadStateDirFromConfig(cfgPath string) (string, error) {
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return "", fmt.Errorf("read config %q: %w", cfgPath, err)
	}
	var cfg stateDirConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return "", fmt.Errorf("parse config %q: %w", cfgPath, err)
	}
	return strings.TrimSpace(cfg.StateDir), nil
}

func defaultConfigPathIfExists() string {
	cfgPath := defaultConfigPath()
	if strings.TrimSpace(cfgPath) == "" {
		return ""
	}
	info, err := os.Stat(cfgPath)
	if err != nil || info == nil || info.IsDir() {
		return ""
	}
	return cfgPath
}

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	if strings.TrimSpace(home) == "" {
		return ""
	}
	cfgPath := filepath.Join(
		home,
		defaultConfigRootDir,
		defaultConfigAppDir,
		defaultConfigFile,
	)
	return cfgPath
}

func defaultTRPCConfigPathIfExists() string {
	cfgPath := defaultTRPCConfigPath()
	if strings.TrimSpace(cfgPath) == "" {
		return ""
	}
	info, err := os.Stat(cfgPath)
	if err != nil || info == nil || info.IsDir() {
		return ""
	}
	return cfgPath
}

func defaultTRPCConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	if strings.TrimSpace(home) == "" {
		return ""
	}
	cfgPath := filepath.Join(
		home,
		defaultConfigRootDir,
		defaultConfigAppDir,
		defaultTRPCConfigFile,
	)
	return cfgPath
}

func resolveTRPCConfigPath(raw string) (string, error) {
	path := strings.TrimSpace(raw)
	if path != "" {
		expanded, _, err := expandUserPath(path)
		if err != nil {
			return "", err
		}
		return expanded, nil
	}

	cwd, err := os.Getwd()
	if err == nil && strings.TrimSpace(cwd) != "" {
		cfgPath := filepath.Join(cwd, defaultTRPCConfigFile)
		info, statErr := os.Stat(cfgPath)
		if statErr == nil && info != nil && !info.IsDir() {
			return cfgPath, nil
		}
	}

	return defaultTRPCConfigPathIfExists(), nil
}

func defaultStateDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	if strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(
		home,
		defaultConfigRootDir,
		defaultConfigAppDir,
	)
}

func applyRuntimeEnvDefaults(paths startupPaths) error {
	applyConfigEnvDefaults()
	setDefaultEnv(runtimeStateDirEnvName, paths.StateDir)
	stateDir := effectiveStartupStateDir(paths)
	loadRuntimeEnvDefaults(stateDir)
	assets := ensureRuntimeSupportAssets(stateDir)
	setRuntimeToolchainEnvDefaults(stateDir)
	setRuntimeBrowserEnvDefaults(stateDir)
	if strings.TrimSpace(assets.DocHelperPath) != "" {
		setDefaultEnv(runtimeDocHelperEnvName, assets.DocHelperPath)
	}
	if strings.TrimSpace(assets.BrowserRuntimePath) != "" {
		setDefaultEnv(
			runtimeBrowserRuntimeEnvName,
			assets.BrowserRuntimePath,
		)
	}
	if err := applyRuntimeExecEnvDefaults(stateDir); err != nil {
		return err
	}
	if err := applyRuntimeShellEnvDefaults(stateDir, assets); err != nil {
		return err
	}
	return nil
}

func loadRuntimeEnvDefaults(stateDir string) {
	values, err := readRuntimeEnvAssignmentsForStateDir(stateDir)
	if err != nil {
		return
	}
	for name, value := range values {
		setDefaultEnv(name, value)
	}
}

func readRuntimeEnvAssignmentsForStateDir(
	stateDir string,
) (map[string]string, error) {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return nil, os.ErrNotExist
	}
	return readRuntimeEnvAssignments(
		filepath.Join(stateDir, runtimeEnvFileName),
	)
}

func readRuntimeEnvAssignments(path string) (map[string]string, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	values := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		values[key] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return values, nil
}

func runCleanup(cleanup func()) {
	if cleanup != nil {
		cleanup()
	}
}

func applyConfigEnvDefaults() {
	setDefaultEnv(
		wecomAICallbackPathEnvName,
		defaultWeComAICallbackPath,
	)
	setDefaultEnv(
		wecomNotificationCallbackPathEnvName,
		defaultWeComNotificationCallbackPath,
	)
}

func setRuntimeToolchainEnvDefaults(stateDir string) {
	toolchainDir := effectiveRuntimeToolchainDir(stateDir)
	if strings.TrimSpace(toolchainDir) == "" {
		return
	}
	setDefaultEnv(runtimeToolchainDirEnvName, toolchainDir)
	setDefaultEnv(runtimeToolchainRootEnvName, toolchainDir)
	setDefaultEnv(
		runtimeFontsDirEnvName,
		runtimeFontsDirFromRoot(toolchainDir),
	)
	setDefaultEnv(
		runtimeTessdataDirEnvName,
		runtimeTessdataDirFromRoot(toolchainDir),
	)
	setDefaultEnv(
		runtimeManagedPythonEnvName,
		runtimeManagedPythonPathFromRoot(toolchainDir),
	)
	setDefaultEnv(
		virtualEnvEnvName,
		runtimeManagedPythonRootFromRoot(toolchainDir),
	)
	setDefaultEnv(
		runtimePIPDisableEnvName,
		runtimePIPDisableEnvValue,
	)
}

func setRuntimeBrowserEnvDefaults(stateDir string) {
	toolchainDir := effectiveRuntimeToolchainDir(stateDir)
	if strings.TrimSpace(toolchainDir) != "" {
		setDefaultEnv(
			runtimeBrowserMCPBinEnvName,
			runtimeManagedBrowserMCPPathFromRoot(toolchainDir),
		)
	}
	setDefaultEnv(
		runtimePlaywrightBrowsersEnvName,
		runtimePlaywrightDir(stateDir),
	)
	setDefaultEnv(
		runtimeBrowserNameEnvName,
		defaultRuntimeBrowserName(),
	)
	mode := resolveRuntimeBrowserMode(
		os.Getenv(runtimeBrowserModeEnvName),
		os.Getenv(runtimeBrowserHeadlessEnvName),
		os.Getenv(runtimeOpenClawBrowserHeadlessEnvName),
	)
	if mode == "" {
		mode = defaultRuntimeBrowserMode()
	}
	setDefaultEnv(runtimeBrowserModeEnvName, mode)
	executablePath := resolveRuntimeBrowserPath(
		os.Getenv(runtimeBrowserPathEnvName),
		os.Getenv(runtimeBrowserExecPathEnvName),
		os.Getenv(runtimeOpenClawBrowserExecPathEnvName),
	)
	if executablePath == "" {
		executablePath = detectRuntimeBrowserExecutablePathForStateDir(
			stateDir,
		)
	}
	setDefaultEnv(runtimeBrowserPathEnvName, executablePath)
	executablePath = resolveRuntimeBrowserPath(
		os.Getenv(runtimeBrowserPathEnvName),
		os.Getenv(runtimeBrowserExecPathEnvName),
		os.Getenv(runtimeOpenClawBrowserExecPathEnvName),
	)
	setDefaultEnv(runtimeBrowserExecPathEnvName, executablePath)
	setDefaultEnv(
		runtimeOpenClawBrowserExecPathEnvName,
		executablePath,
	)
	headlessValue := resolveRuntimeBrowserHeadlessValue(
		os.Getenv(runtimeBrowserModeEnvName),
		os.Getenv(runtimeBrowserHeadlessEnvName),
		os.Getenv(runtimeOpenClawBrowserHeadlessEnvName),
		detectRuntimeBrowserHeadlessDefault(),
	)
	setDefaultEnv(
		runtimeBrowserHeadlessEnvName,
		headlessValue,
	)
	setDefaultEnv(
		runtimeOpenClawBrowserHeadlessEnvName,
		headlessValue,
	)
}

func detectRuntimeBrowserMCPPath(stateDir string) string {
	return detectRuntimeBrowserMCPPathWith(
		stateDir,
		func(name string) (string, error) {
			return exec.LookPath(name)
		},
		fileExecutable,
	)
}

func detectRuntimeBrowserMCPPathWith(
	stateDir string,
	lookPath func(string) (string, error),
	isExecutable func(string) bool,
) string {
	for _, candidate := range []string{
		strings.TrimSpace(
			os.Getenv(runtimeBrowserMCPBinEnvName),
		),
		runtimeManagedBrowserMCPPath(stateDir),
		runtimeLegacyManagedBrowserMCPPath(stateDir),
	} {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		if isExecutable(candidate) {
			return candidate
		}
	}
	path, err := lookPath(runtimePlaywrightMCPBinName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(path)
}

func shouldAutoManageBrowserRuntime(args []string) bool {
	return !isOpenClawSubcommand(args)
}

func ensureManagedBrowserRuntime(
	stateDir string,
) (bool, string) {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return false, ""
	}
	if strings.TrimSpace(
		detectRuntimeBrowserMCPPath(stateDir),
	) != "" {
		return true, ""
	}
	if _, err := exec.LookPath(runtimeNodeExecName); err != nil {
		return false, runtimeBrowserWarning(browserRuntimeMissingNode)
	}
	if _, err := exec.LookPath(runtimeNPMExecName); err != nil {
		return false, runtimeBrowserWarning(browserRuntimeMissingNPM)
	}
	if strings.TrimSpace(
		detectRuntimeBrowserExecutablePathForStateDir(stateDir),
	) == "" {
		return false, runtimeBrowserWarning(browserRuntimeMissingBrowser)
	}
	if err := installManagedBrowserRuntime(stateDir); err != nil {
		return false, runtimeBrowserWarning(
			browserRuntimeInstallFailed + ": " + err.Error(),
		)
	}
	if strings.TrimSpace(
		detectRuntimeBrowserMCPPath(stateDir),
	) == "" {
		return false, runtimeBrowserWarning(browserRuntimeInstallFailed)
	}
	return true, ""
}

func installManagedBrowserRuntime(stateDir string) error {
	managedRoot := runtimeManagedPythonRoot(stateDir)
	if strings.TrimSpace(managedRoot) == "" {
		return errors.New("managed toolchain root is empty")
	}
	if err := os.MkdirAll(managedRoot, runtimeSupportDirPerm); err != nil {
		return err
	}
	cmd := exec.Command(
		runtimeNPMExecName,
		"install",
		"-g",
		"--prefix",
		managedRoot,
		runtimePlaywrightMCPPackage,
	)
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	return fmt.Errorf(
		"%w: %s",
		err,
		strings.TrimSpace(condenseRuntimeCommandOutput(string(output))),
	)
}

func condenseRuntimeCommandOutput(output string) string {
	lines := strings.Fields(strings.TrimSpace(output))
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, " ")
}

func runtimeBrowserWarning(detail string) string {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return ""
	}
	return "browser runtime unavailable: " + detail
}

func defaultRuntimeBrowserName() string {
	return runtimeBrowserNameChromium
}

func defaultRuntimeBrowserMode() string {
	return runtimeBrowserModeAuto
}

func runtimePlaywrightDir(stateDir string) string {
	return runtimeStateSubdir(stateDir, runtimePlaywrightDirName)
}

func detectRuntimeBrowserExecutablePathForStateDir(stateDir string) string {
	roots := []string{
		strings.TrimSpace(
			os.Getenv(runtimePlaywrightBrowsersEnvName),
		),
		runtimePlaywrightDir(stateDir),
	}
	return detectRuntimeBrowserExecutablePathWith(
		exec.LookPath,
		fileExecutable,
		roots...,
	)
}

func detectRuntimeBrowserExecutablePathWith(
	lookPath func(string) (string, error),
	isExecutable func(string) bool,
	roots ...string,
) string {
	for _, name := range runtimeBrowserExecutableCandidates {
		path, err := lookPath(name)
		if err != nil {
			continue
		}
		path = strings.TrimSpace(path)
		if path != "" {
			return path
		}
	}
	for _, path := range runtimeBrowserAbsoluteExecutableCandidates {
		if isExecutable(path) {
			return path
		}
	}
	for _, root := range roots {
		path := detectRuntimeBrowserExecutablePathFromRoot(
			root,
			isExecutable,
		)
		if path != "" {
			return path
		}
	}
	return ""
}

func detectRuntimeBrowserExecutablePathFromRoot(
	root string,
	isExecutable func(string) bool,
) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	patterns := []string{
		filepath.Join(
			root,
			"chromium-*",
			"chrome-linux64",
			"chrome",
		),
		filepath.Join(
			root,
			"chromium-*",
			"chrome-linux",
			"chrome",
		),
		filepath.Join(
			root,
			"chromium-*",
			"chrome-mac",
			"Chromium.app",
			"Contents",
			"MacOS",
			"Chromium",
		),
	}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, match := range matches {
			if isExecutable(match) {
				return match
			}
		}
	}
	return ""
}

func fileExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}

func resolveRuntimeBrowserMode(
	modeValue string,
	headlessValue string,
	compatHeadlessValue string,
) string {
	if mode := normalizeRuntimeBrowserMode(modeValue); mode != "" {
		return mode
	}
	if mode := runtimeBrowserModeFromHeadlessValue(headlessValue); mode != "" {
		return mode
	}
	return runtimeBrowserModeFromHeadlessValue(compatHeadlessValue)
}

func normalizeRuntimeBrowserMode(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case runtimeBrowserModeAuto:
		return runtimeBrowserModeAuto
	case runtimeBrowserModeHeadless:
		return runtimeBrowserModeHeadless
	case runtimeBrowserModeInteractive:
		return runtimeBrowserModeInteractive
	default:
		return ""
	}
}

func runtimeBrowserModeFromHeadlessValue(value string) string {
	switch normalizeRuntimeBrowserHeadlessValue(value) {
	case runtimeBrowserHeadlessEnabledValue:
		return runtimeBrowserModeHeadless
	case runtimeBrowserHeadlessDisabledValue:
		return runtimeBrowserModeInteractive
	default:
		return ""
	}
}

func normalizeRuntimeBrowserHeadlessValue(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case runtimeBrowserHeadlessEnabledValue, "true", "yes":
		return runtimeBrowserHeadlessEnabledValue
	case runtimeBrowserHeadlessDisabledValue, "false", "no":
		return runtimeBrowserHeadlessDisabledValue
	default:
		return ""
	}
}

func resolveRuntimeBrowserHeadlessValue(
	modeValue string,
	headlessValue string,
	compatHeadlessValue string,
	detectedDefault string,
) string {
	if headless := normalizeRuntimeBrowserHeadlessValue(
		headlessValue,
	); headless != "" {
		return headless
	}
	if mode := normalizeRuntimeBrowserMode(modeValue); mode != "" {
		switch mode {
		case runtimeBrowserModeHeadless:
			return runtimeBrowserHeadlessEnabledValue
		case runtimeBrowserModeInteractive:
			return runtimeBrowserHeadlessDisabledValue
		}
	}
	if headless := normalizeRuntimeBrowserHeadlessValue(
		compatHeadlessValue,
	); headless != "" {
		return headless
	}
	return detectedDefault
}

func resolveRuntimeBrowserPath(paths ...string) string {
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path != "" {
			return path
		}
	}
	return ""
}

func applyRuntimeExecEnvDefaults(stateDir string) error {
	tempRoot, err := ensureRuntimeTempRoot(stateDir)
	if err != nil {
		return err
	}
	setDefaultEnv(runtimeTmpDirEnvName, tempRoot)
	setDefaultEnv(runtimeTmpEnvName, tempRoot)
	setDefaultEnv(runtimeTempEnvName, tempRoot)

	execPath := currentExecutablePath()
	execDir := ""
	if execPath != "" {
		execDir = filepath.Dir(execPath)
		setDefaultEnv(runtimeBinEnvName, execPath)
		setDefaultEnv(runtimeOpenClawBinEnvName, execPath)
	}
	if execDir != "" {
		setDefaultEnv(runtimeBinDirEnvName, execDir)
		setDefaultEnv(runtimeOpenClawBinDirEnvName, execDir)
	}
	candidates := runtimePathDefaults(execDir, stateDir)
	if len(candidates) == 0 {
		return nil
	}
	updated := prependPathEntries(
		os.Getenv(runtimePathEnvName),
		candidates,
	)
	if strings.TrimSpace(updated) == "" {
		return nil
	}
	_ = os.Setenv(runtimePathEnvName, updated)
	return nil
}

func applyRuntimeShellEnvDefaults(
	stateDir string,
	assets runtimeSupportAssets,
) error {
	shellEnvPath := strings.TrimSpace(assets.ShellEnvPath)
	if shellEnvPath == "" {
		shellEnvPath = runtimeShellEnvPath(stateDir)
	}
	if shellEnvPath == "" {
		return nil
	}
	_ = os.Setenv(runtimeShellEnvFileEnvName, shellEnvPath)
	_ = os.Setenv(runtimeBashEnvName, shellEnvPath)
	_ = os.Setenv(runtimePosixShellEnvName, shellEnvPath)
	return writeRuntimeSupportFileMode(
		shellEnvPath,
		runtimeShellEnvContent(os.Environ()),
		runtimeSupportPrivateFilePerm,
	)
}

func currentExecutablePath() string {
	path, err := os.Executable()
	if err != nil {
		return ""
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil && strings.TrimSpace(resolved) != "" {
		return resolved
	}
	return path
}

func effectiveRuntimeToolchainDir(stateDir string) string {
	toolchainDir := strings.TrimSpace(
		os.Getenv(runtimeToolchainDirEnvName),
	)
	if toolchainDir != "" {
		return toolchainDir
	}
	return runtimeToolchainDir(stateDir)
}

func effectiveRuntimeManagedPythonPath(stateDir string) string {
	pythonPath := strings.TrimSpace(
		os.Getenv(runtimeManagedPythonEnvName),
	)
	if pythonPath != "" {
		return pythonPath
	}
	return runtimeManagedPythonPathFromRoot(
		effectiveRuntimeToolchainDir(stateDir),
	)
}

func runtimePathDefaults(execDir string, stateDir string) []string {
	candidates := make([]string, 0, 16)
	candidates = appendNonEmptyDir(candidates, execDir)
	candidates = appendEnvPathDir(
		candidates,
		runtimeBinEnvName,
	)
	candidates = appendEnvDir(
		candidates,
		runtimeBinDirEnvName,
	)
	candidates = appendEnvPathDir(
		candidates,
		runtimeOpenClawBinEnvName,
	)
	candidates = appendEnvDir(
		candidates,
		runtimeOpenClawBinDirEnvName,
	)
	candidates = appendNonEmptyDir(
		candidates,
		runtimeToolsDir(stateDir),
	)
	candidates = appendEnvPathDir(
		candidates,
		runtimeDocHelperEnvName,
	)
	candidates = appendEnvPathDir(
		candidates,
		runtimeBrowserRuntimeEnvName,
	)
	toolchainDir := effectiveRuntimeToolchainDir(stateDir)
	candidates = appendNonEmptyDir(
		candidates,
		runtimeToolchainBinDirFromRoot(toolchainDir),
	)
	candidates = appendEnvPathDir(
		candidates,
		runtimeBrowserMCPBinEnvName,
	)
	candidates = appendNonEmptyDir(
		candidates,
		pathValueDir(
			effectiveRuntimeManagedPythonPath(stateDir),
		),
	)
	candidates = appendEnvListDirs(
		candidates,
		runtimeExtraPathEnvName,
	)
	candidates = appendCommonHomeBinDirs(candidates)
	candidates = appendCommonEnvBinDirs(candidates)
	return candidates
}

func detectRuntimeBrowserHeadlessDefault() string {
	inContainer := fileExists("/.dockerenv") ||
		fileExists("/run/.containerenv")
	return defaultRuntimeBrowserHeadless(
		runtime.GOOS,
		os.Getenv("DISPLAY"),
		os.Getenv("WAYLAND_DISPLAY"),
		inContainer,
	)
}

func defaultRuntimeBrowserHeadless(
	goos string,
	display string,
	waylandDisplay string,
	inContainer bool,
) string {
	switch strings.TrimSpace(goos) {
	case "darwin", "windows":
		return runtimeBrowserHeadlessDisabledValue
	}
	if strings.TrimSpace(display) != "" ||
		strings.TrimSpace(waylandDisplay) != "" {
		return runtimeBrowserHeadlessDisabledValue
	}
	if inContainer {
		return runtimeBrowserHeadlessEnabledValue
	}
	return runtimeBrowserHeadlessEnabledValue
}

func appendNonEmptyDir(out []string, dir string) []string {
	dir = normalizeRuntimePathDir(dir)
	if dir == "" {
		return out
	}
	return append(out, dir)
}

func normalizeRuntimePathDir(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	expanded, _, err := expandUserPath(raw)
	if err == nil && strings.TrimSpace(expanded) != "" {
		raw = expanded
	}
	if !filepath.IsAbs(raw) {
		absolute, err := filepath.Abs(raw)
		if err == nil && strings.TrimSpace(absolute) != "" {
			raw = absolute
		}
	}
	return filepath.Clean(raw)
}

func appendEnvDir(out []string, name string) []string {
	return appendNonEmptyDir(
		out,
		os.Getenv(name),
	)
}

func appendEnvPathDir(out []string, name string) []string {
	return appendNonEmptyDir(
		out,
		pathValueDir(os.Getenv(name)),
	)
}

func appendEnvSubdir(
	out []string,
	name string,
	subdir string,
) []string {
	root := strings.TrimSpace(os.Getenv(name))
	if root == "" {
		return out
	}
	return appendNonEmptyDir(
		out,
		filepath.Join(root, subdir),
	)
}

func appendEnvListDirs(out []string, name string) []string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return out
	}
	for _, dir := range filepath.SplitList(value) {
		out = appendNonEmptyDir(out, dir)
	}
	return out
}

func appendEnvListSubdirs(
	out []string,
	name string,
	subdir string,
) []string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return out
	}
	for _, root := range filepath.SplitList(value) {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		out = appendNonEmptyDir(
			out,
			filepath.Join(root, subdir),
		)
	}
	return out
}

func appendCommonHomeBinDirs(out []string) []string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return out
	}
	out = appendNonEmptyDir(
		out,
		filepath.Join(home, defaultUserLocalBinDir),
	)
	out = appendNonEmptyDir(
		out,
		filepath.Join(home, defaultUserBinDir),
	)
	out = appendNonEmptyDir(
		out,
		filepath.Join(
			home,
			defaultUserGoDir,
			defaultUserBinDir,
		),
	)
	out = appendNonEmptyDir(
		out,
		filepath.Join(
			home,
			defaultCargoDir,
			defaultUserBinDir,
		),
	)
	return out
}

func appendCommonEnvBinDirs(out []string) []string {
	out = appendEnvDir(out, goBinEnvName)
	out = appendEnvListSubdirs(
		out,
		goPathEnvName,
		defaultUserBinDir,
	)
	out = appendEnvSubdir(
		out,
		goRootEnvName,
		defaultUserBinDir,
	)
	out = appendEnvSubdir(
		out,
		cargoHomeEnvName,
		defaultUserBinDir,
	)
	out = appendEnvDir(out, pnpmHomeEnvName)
	out = appendEnvSubdir(
		out,
		nodeHomeEnvName,
		defaultUserBinDir,
	)
	out = appendEnvSubdir(
		out,
		nodePrefixEnvName,
		defaultUserBinDir,
	)
	out = appendEnvSubdir(
		out,
		virtualEnvEnvName,
		defaultUserBinDir,
	)
	return out
}

func pathValueDir(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	info, err := os.Stat(value)
	if err == nil && info != nil && info.IsDir() {
		return value
	}
	if !strings.Contains(value, string(os.PathSeparator)) {
		return ""
	}
	return filepath.Dir(value)
}

func prependPathEntries(current string, candidates []string) string {
	out := make([]string, 0, len(candidates)+4)
	seen := make(map[string]struct{}, len(candidates)+4)

	appendPath := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		cleaned := filepath.Clean(raw)
		if _, ok := seen[cleaned]; ok {
			return
		}
		seen[cleaned] = struct{}{}
		out = append(out, raw)
	}

	for _, dir := range candidates {
		appendPath(dir)
	}
	for _, dir := range filepath.SplitList(current) {
		appendPath(dir)
	}
	return strings.Join(out, string(os.PathListSeparator))
}

func setDefaultEnv(name string, value string) {
	if strings.TrimSpace(name) == "" {
		return
	}
	if strings.TrimSpace(value) == "" {
		return
	}
	if strings.TrimSpace(os.Getenv(name)) != "" {
		return
	}
	_ = os.Setenv(name, value)
}

func ensureRuntimeTempRoot(stateDir string) (string, error) {
	tempRoot, err := workspacecfg.EnsureTempRoot(
		workspacecfg.DefaultTempRoot(stateDir),
	)
	if err != nil {
		return "", fmt.Errorf(
			"prepare runtime temp root: %w",
			err,
		)
	}
	return tempRoot, nil
}

func ensureScratchOutputRoot(scratchRoot string) (string, error) {
	scratchRoot = strings.TrimSpace(scratchRoot)
	if scratchRoot == "" {
		return "", nil
	}
	outputRoot := filepath.Join(scratchRoot, scratchOutputDirName)
	if err := os.MkdirAll(outputRoot, 0o755); err != nil {
		return "", fmt.Errorf(
			"create scratch output root %q: %w",
			outputRoot,
			err,
		)
	}
	return outputRoot, nil
}

func prepareOpenClawConfig(
	args []string,
	paths startupPaths,
) ([]string, func(), error) {
	cleanup := func() {}
	cfgPath := strings.TrimSpace(paths.OpenClawConfigPath)
	if cfgPath == "" {
		return args, cleanup, nil
	}
	if !shouldPrepareOpenClawConfig(args) {
		return args, cleanup, nil
	}

	preparedPath, preparedArgs, cleanup, err := preprocessOpenClawConfig(
		cfgPath,
		args,
		paths.StateDir,
	)
	if err != nil {
		return nil, func() {}, err
	}
	args = preparedArgs
	if strings.TrimSpace(preparedPath) == "" ||
		preparedPath == cfgPath {
		return args, cleanup, nil
	}
	return setFlagValue(args, flagConfig, preparedPath), cleanup, nil
}

func shouldPrepareOpenClawConfig(args []string) bool {
	if len(args) == 0 {
		return true
	}

	switch strings.TrimSpace(args[0]) {
	case subcmdBootstrap:
		return false
	case subcmdInspect:
		if len(args) < 2 {
			return false
		}
		return strings.TrimSpace(args[1]) == inspectCmdConfigKeys
	default:
		return true
	}
}

func preprocessOpenClawConfig(
	cfgPath string,
	args []string,
	stateDir string,
) (string, []string, func(), error) {
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return "", nil, nil, fmt.Errorf(
			"read config %q: %w",
			cfgPath,
			err,
		)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return "", nil, nil, fmt.Errorf(
			"parse config %q: %w",
			cfgPath,
			err,
		)
	}
	changed, warnings, preparedArgs, err := prepareRuntimeConfigRoot(
		&root,
		args,
		stateDir,
		cfgPath,
		os.LookupEnv,
	)
	if err != nil {
		return "", nil, nil, err
	}
	emitConfigWarnings(warnings)
	if !changed {
		return cfgPath, preparedArgs, func() {}, nil
	}

	tmpFile, err := os.CreateTemp("", "trpc-claw-config-*.yaml")
	if err != nil {
		return "", nil, nil, err
	}
	tmpPath := tmpFile.Name()
	cleanup := func() {
		_ = os.Remove(tmpPath)
	}

	enc := yaml.NewEncoder(tmpFile)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		_ = enc.Close()
		_ = tmpFile.Close()
		cleanup()
		return "", nil, nil, err
	}
	if err := enc.Close(); err != nil {
		_ = tmpFile.Close()
		cleanup()
		return "", nil, nil, err
	}
	if err := tmpFile.Close(); err != nil {
		cleanup()
		return "", nil, nil, err
	}
	return tmpPath, preparedArgs, cleanup, nil
}

func prepareRuntimeConfigRoot(
	root *yaml.Node,
	args []string,
	stateDir string,
	cfgPath string,
	lookup envLookupFunc,
) (bool, []string, []string, error) {
	implicitChanged, err := applyImplicitWeixinChannelDefault(root)
	if err != nil {
		return false, nil, nil, err
	}
	changed, err := applyChannelEnabledDefaultsWithLookup(root, lookup)
	if err != nil {
		return false, nil, nil, err
	}
	envChanged, err := expandConfigEnvScalarsWithLookup(
		root,
		stateDir,
		lookup,
	)
	if err != nil {
		return false, nil, nil, err
	}
	defaultsChanged, warnings, preparedArgs, err := applyConfigDefaults(
		root,
		args,
		stateDir,
		cfgPath,
	)
	if err != nil {
		return false, nil, nil, err
	}
	return implicitChanged || changed || envChanged || defaultsChanged,
		warnings, preparedArgs, nil
}

func applyConfigDefaults(
	root *yaml.Node,
	args []string,
	stateDir string,
	cfgPath string,
) (bool, []string, []string, error) {
	changed := false
	warnings := make([]string, 0, 1)
	preparedArgs := append([]string(nil), args...)
	configStateDir := effectiveConfigStateDir(root, stateDir)

	instructionChanged, err := applyAgentInstructionDefaults(
		root,
		preparedArgs,
		configStateDir,
		cfgPath,
	)
	if err != nil {
		return false, nil, nil, err
	}
	changed = changed || instructionChanged

	preparedArgs, fallbackChanged, fallbackWarning, err :=
		applyMemoryFallbackDefaults(
			root,
			preparedArgs,
		)
	if err != nil {
		return false, nil, nil, err
	}
	if fallbackChanged {
		changed = true
	}
	if strings.TrimSpace(fallbackWarning) != "" {
		warnings = append(warnings, fallbackWarning)
	}

	sourceSkillsChanged, err := applySourceTreeSkillsDefaults(
		root,
		cfgPath,
		stateDir,
	)
	if err != nil {
		return false, nil, nil, err
	}
	if sourceSkillsChanged {
		changed = true
	}

	skillsDefaultsChanged, err := applySkillsDefaults(root)
	if err != nil {
		return false, nil, nil, err
	}
	if skillsDefaultsChanged {
		changed = true
	}

	extraDirsChanged, err := applySkillsExtraDirDefaults(root)
	if err != nil {
		return false, nil, nil, err
	}
	if extraDirsChanged {
		changed = true
	}

	envProbeChanged, err := applyEnvProbeToolProviderDefaults(root)
	if err != nil {
		return false, nil, nil, err
	}
	if envProbeChanged {
		changed = true
	}

	assistantNameChanged, err := applyAssistantNameToolProviderDefaults(
		root,
	)
	if err != nil {
		return false, nil, nil, err
	}
	if assistantNameChanged {
		changed = true
	}

	if shouldAutoManageBrowserRuntime(preparedArgs) {
		browserReady, browserWarning := ensureManagedBrowserRuntime(
			stateDir,
		)
		if strings.TrimSpace(browserWarning) != "" {
			warnings = append(warnings, browserWarning)
		}
		if browserReady {
			browserChanged, err := applyBrowserToolProviderDefaults(root)
			if err != nil {
				return false, nil, nil, err
			}
			if browserChanged {
				changed = true
			}
		}
	}

	codingDefaults, hasCodingConfig, err := resolveCodingAgentDefaults(
		root,
		stateDir,
	)
	if err != nil {
		return false, nil, nil, err
	}

	codingChanged, err := applySkillsCodingAgentDefaults(
		root,
		codingDefaults,
		hasCodingConfig,
	)
	if err != nil {
		return false, nil, nil, err
	}
	if codingChanged {
		changed = true
	}

	identity, err := resolveRuntimeModelIdentity(root, preparedArgs)
	if err != nil {
		return false, nil, nil, err
	}

	replyDeliveryRoots, err := resolveWeComReplyDeliveryRoots(
		root,
	)
	if err != nil {
		return false, nil, nil, err
	}

	wecomChanged, err := applyWeComRuntimeDefaults(
		root,
		codingDefaults,
		identity,
		replyDeliveryRoots,
		configStateDir,
		cfgPath,
	)
	if err != nil {
		return false, nil, nil, err
	}
	if wecomChanged {
		changed = true
	}

	systemPromptChanged, err := applyAgentSystemPromptDefaults(
		root,
		preparedArgs,
		configStateDir,
		cfgPath,
		identity,
		codingDefaults,
		hasCodingConfig,
	)
	if err != nil {
		return false, nil, nil, err
	}
	if systemPromptChanged {
		changed = true
	}
	return changed, warnings, preparedArgs, nil
}

func expandConfigEnvScalars(
	root *yaml.Node,
	stateDir string,
) (bool, error) {
	return expandConfigEnvScalarsWithLookup(
		root,
		stateDir,
		os.LookupEnv,
	)
}

func expandConfigEnvScalarsWithLookup(
	root *yaml.Node,
	stateDir string,
	lookup envLookupFunc,
) (bool, error) {
	if root == nil {
		return false, nil
	}
	return expandConfigEnvNode(root, stateDir, lookup)
}

func expandConfigEnvNode(
	node *yaml.Node,
	stateDir string,
	lookup envLookupFunc,
) (bool, error) {
	if node == nil {
		return false, nil
	}
	if lookup == nil {
		lookup = os.LookupEnv
	}

	changed := false
	if node.Kind == yaml.ScalarNode && node.Tag == "!!str" {
		expanded, scalarChanged, err := expandConfigEnvStringWithLookup(
			node.Value,
			stateDir,
			lookup,
		)
		if err != nil {
			return false, err
		}
		if scalarChanged {
			node.Value = expanded
			node.Tag = "" // let YAML re-resolve the type from value content
			changed = true
		}
	}

	for _, child := range node.Content {
		childChanged, err := expandConfigEnvNode(
			child,
			stateDir,
			lookup,
		)
		if err != nil {
			return false, err
		}
		if childChanged {
			changed = true
		}
	}
	return changed, nil
}

func expandConfigEnvString(
	raw string,
	stateDir string,
) (string, bool, error) {
	return expandConfigEnvStringWithLookup(
		raw,
		stateDir,
		os.LookupEnv,
	)
}

func expandConfigEnvStringWithLookup(
	raw string,
	stateDir string,
	lookup envLookupFunc,
) (string, bool, error) {
	if !strings.Contains(raw, configEnvRefPrefix) {
		return raw, false, nil
	}

	var builder strings.Builder
	remaining := raw
	for remaining != "" {
		start := strings.Index(remaining, configEnvRefPrefix)
		if start < 0 {
			builder.WriteString(remaining)
			break
		}
		builder.WriteString(remaining[:start])
		remaining = remaining[start+len(configEnvRefPrefix):]

		end := strings.Index(remaining, configEnvRefSuffix)
		if end < 0 {
			return "", false, fmt.Errorf(
				"config: unterminated env ref in %q",
				raw,
			)
		}

		value, err := resolveConfigEnvRefWithLookup(
			remaining[:end],
			stateDir,
			lookup,
		)
		if err != nil {
			return "", false, err
		}
		builder.WriteString(value)
		remaining = remaining[end+len(configEnvRefSuffix):]
	}
	expanded := builder.String()
	return expanded, expanded != raw, nil
}

func resolveConfigEnvRef(
	token string,
	stateDir string,
) (string, error) {
	return resolveConfigEnvRefWithLookup(
		token,
		stateDir,
		os.LookupEnv,
	)
}

func resolveConfigEnvRefWithLookup(
	token string,
	stateDir string,
	lookup envLookupFunc,
) (string, error) {
	name, defaultValue, hasDefault := strings.Cut(
		token,
		configEnvDefaultSep,
	)
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("config: empty env var name in %q", token)
	}
	if name == runtimeStateDirEnvName {
		value := strings.TrimSpace(stateDir)
		if value != "" {
			return value, nil
		}
		return configEnvRefPrefix + token + configEnvRefSuffix, nil
	}
	if lookup == nil {
		lookup = os.LookupEnv
	}
	if value, ok := lookup(name); ok {
		if value != "" || !hasDefault {
			return value, nil
		}
	}
	if hasDefault {
		return defaultValue, nil
	}
	if allowsMissingConfigEnvRef(name) {
		return "", nil
	}
	return "", fmt.Errorf("config: env var %s is not set", name)
}

func allowsMissingConfigEnvRef(name string) bool {
	switch name {
	case wecomGroupSessionModeEnvName:
		return true
	default:
		return false
	}
}

type promptBundleSpec struct {
	embeddedDir  string
	defaultDir   func(promptasset.Paths) string
	defaultFiles []string
}

func configBaseDir(cfgPath string) string {
	cfgPath = strings.TrimSpace(cfgPath)
	if cfgPath == "" {
		return ""
	}
	return filepath.Dir(cfgPath)
}

func configuredStateDirValue(root *yaml.Node) string {
	doc := documentNode(root)
	if doc == nil {
		return ""
	}
	return strings.TrimSpace(mappingStringValue(doc, stateDirKey))
}

func effectiveConfigStateDir(
	root *yaml.Node,
	stateDir string,
) string {
	if value := configuredStateDirValue(root); value != "" {
		expanded, _, err := expandUserPath(value)
		if err == nil && strings.TrimSpace(expanded) != "" {
			return expanded
		}
		return value
	}
	return strings.TrimSpace(stateDir)
}

func loadPromptBundleFromConfig(
	baseDir string,
	rawFiles []string,
	rawDir string,
	stateDir string,
	loadDefault bool,
	spec promptBundleSpec,
	vars map[string]string,
) (string, error) {
	files, dir, err := promptasset.ResolvePaths(
		baseDir,
		rawFiles,
		rawDir,
	)
	if err != nil {
		return "", err
	}
	switch {
	case len(files) > 0 || dir != "":
		raw, err := promptasset.ReadDiskBundle(files, dir)
		if err != nil {
			return "", err
		}
		return promptasset.Render(raw, vars)
	case !loadDefault:
		return "", nil
	}

	if strings.TrimSpace(stateDir) != "" {
		paths, err := promptasset.EnsureDefaultFiles(stateDir)
		if err != nil {
			return "", err
		}
		defaultDir := spec.defaultDir(paths)
		raw, err := readPromptBundleFromDir(
			defaultDir,
			spec.defaultFiles,
		)
		if err != nil {
			return "", err
		}
		return promptasset.Render(raw, vars)
	}

	raw, err := readEmbeddedPromptBundle(
		spec.embeddedDir,
		spec.defaultFiles,
	)
	if err != nil {
		return "", err
	}
	return promptasset.Render(raw, vars)
}

func readPromptBundleFromDir(
	dir string,
	files []string,
) (string, error) {
	if len(files) == 0 {
		return promptasset.ReadDiskBundle(nil, dir)
	}
	paths := make([]string, 0, len(files))
	for _, name := range files {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		paths = append(paths, filepath.Join(dir, name))
	}
	return promptasset.ReadDiskBundle(paths, "")
}

func readEmbeddedPromptBundle(
	dir string,
	files []string,
) (string, error) {
	if len(files) == 0 {
		return promptasset.ReadEmbeddedBundle(dir)
	}
	parts := make([]string, 0, len(files))
	embeddedFiles, err := promptasset.ReadEmbeddedFiles(dir)
	if err != nil {
		return "", err
	}
	for _, name := range files {
		text, ok := embeddedFiles[name]
		if !ok {
			return "", fmt.Errorf(
				"prompt asset %s/%s not found",
				dir,
				name,
			)
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "\n\n"), nil
}

func mustEmbeddedPromptBundle(
	dir string,
	files []string,
) string {
	raw, err := readEmbeddedPromptBundle(dir, files)
	if err != nil {
		panic(err)
	}
	text, err := promptasset.Render(raw, nil)
	if err != nil {
		panic(err)
	}
	return text
}

func resolveRuntimeModelIdentity(
	root *yaml.Node,
	args []string,
) (runtimeModelIdentity, error) {
	identity := runtimeModelIdentity{
		ModelMode:     defaultRuntimeModelMode,
		OpenAIVariant: defaultRuntimeOpenAIVariant,
	}

	doc := documentNode(root)
	if doc != nil {
		modelNode := mappingValue(doc, modelSectionKey)
		if modelNode != nil && modelNode.Kind != yaml.MappingNode {
			return runtimeModelIdentity{}, fmt.Errorf(
				"config: %s must be a mapping",
				modelSectionKey,
			)
		}
		identity.ModelMode = firstNonEmptyString(
			mappingStringValue(modelNode, modelModeKey),
			identity.ModelMode,
		)
		identity.ModelName = mappingStringValue(modelNode, modelNameKey)
		identity.OpenAIVariant = firstNonEmptyString(
			mappingStringValue(modelNode, modelOpenAIVariantKey),
			identity.OpenAIVariant,
		)
		identity.OpenAIBaseURL = mappingStringValue(
			modelNode,
			modelBaseURLKey,
		)
	}

	if value, ok, err := flagValueFromArgs(args, flagMode); err != nil {
		return runtimeModelIdentity{}, err
	} else if ok {
		identity.ModelMode = strings.TrimSpace(value)
	}
	if value, ok, err := flagValueFromArgs(args, flagModel); err != nil {
		return runtimeModelIdentity{}, err
	} else if ok {
		identity.ModelName = strings.TrimSpace(value)
	}
	if value, ok, err := flagValueFromArgs(args, flagOpenAIVariant); err != nil {
		return runtimeModelIdentity{}, err
	} else if ok {
		identity.OpenAIVariant = strings.TrimSpace(value)
	}
	if value, ok, err := flagValueFromArgs(
		args,
		flagOpenAIBaseURL,
	); err != nil {
		return runtimeModelIdentity{}, err
	} else if ok {
		identity.OpenAIBaseURL = strings.TrimSpace(value)
	}

	if strings.TrimSpace(identity.OpenAIBaseURL) == "" {
		identity.OpenAIBaseURL = strings.TrimSpace(
			os.Getenv(openAIBaseURLEnvName),
		)
	}
	identity.ModelMode = normalizeNonEmptyString(
		identity.ModelMode,
		defaultRuntimeModelMode,
	)
	identity.OpenAIVariant = normalizeNonEmptyString(
		identity.OpenAIVariant,
		defaultRuntimeOpenAIVariant,
	)
	return identity, nil
}

func buildRuntimeIdentitySystemPrompt(
	identity runtimeModelIdentity,
) string {
	raw, err := readEmbeddedPromptBundle(
		promptasset.DefaultSystemEmbeddedDir,
		[]string{promptasset.DefaultRuntimeIdentityFileName},
	)
	if err != nil {
		return ""
	}
	text, err := promptasset.Render(
		raw,
		buildSystemPromptTemplateVars(
			identity,
			codingAgentDefaults{},
			runtimeProductName,
		),
	)
	if err != nil {
		return ""
	}
	return text
}

func stripLegacyManagedSystemPrompt(raw string) string {
	if isLegacyManagedSystemPrompt(raw) {
		return ""
	}
	return strings.TrimSpace(raw)
}

func isLegacyManagedSystemPrompt(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}

	lowered := strings.ToLower(raw)
	if !strings.HasPrefix(lowered, legacySystemPromptPrefix) {
		return false
	}

	candidate := strings.TrimSpace(raw[len(legacySystemPromptPrefix):])
	candidate = strings.TrimRight(candidate, ".!?,;:…")
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return false
	}
	return strings.EqualFold(candidate, runtimeProductName)
}

func applyMemoryFallbackDefaults(
	root *yaml.Node,
	args []string,
) ([]string, bool, string, error) {
	doc := documentNode(root)
	if doc == nil {
		return args, false, "", nil
	}
	memoryNode := mappingValue(doc, memoryKey)
	if memoryNode == nil {
		return args, false, "", nil
	}
	if memoryNode.Kind != yaml.MappingNode {
		return args, false, "", fmt.Errorf(
			"config: %s must be a mapping",
			memoryKey,
		)
	}

	fallbackEnabled, fallbackChanged, err := extractMemoryFallbackEnabled(
		memoryNode,
	)
	if err != nil {
		return args, false, "", err
	}
	if !fallbackEnabled {
		return args, fallbackChanged, "", nil
	}

	if effectiveMemoryBackend(memoryNode, args) !=
		memoryBackendSQLiteVecName {
		return args, fallbackChanged, "", nil
	}

	identity, err := resolveRuntimeModelIdentity(root, args)
	if err != nil {
		return args, false, "", err
	}
	decision, err := decideSQLiteMemoryFallback(root, memoryNode, identity)
	if err != nil {
		return args, false, "", err
	}
	if strings.TrimSpace(decision.Reason) == "" {
		return args, fallbackChanged, "", nil
	}

	if err := rewriteMemoryBackendToSQLite(root, memoryNode); err != nil {
		return args, false, "", err
	}
	args = syncFallbackMemoryBackendArgs(args)
	warning := "memory backend fallback triggered: switched " +
		"sqlitevec to sqlite because " + decision.Reason
	return args, true, warning, nil
}

func effectiveMemoryBackend(
	memoryNode *yaml.Node,
	args []string,
) string {
	if value, ok, err := flagValueFromArgs(
		args,
		flagMemoryBackend,
	); err == nil && ok {
		return normalizeMemoryBackend(value)
	}
	return normalizeMemoryBackend(
		mappingStringValue(memoryNode, memoryBackendKey),
	)
}

func normalizeMemoryBackend(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case memoryBackendFileName:
		return memoryBackendFileName
	default:
		return value
	}
}

func syncFallbackMemoryBackendArgs(args []string) []string {
	value, ok, err := flagValueFromArgs(args, flagMemoryBackend)
	if err != nil || !ok {
		return args
	}
	if strings.ToLower(strings.TrimSpace(value)) !=
		memoryBackendSQLiteVecName {
		return args
	}
	return setFlagValue(
		args,
		flagMemoryBackend,
		memoryBackendSQLiteName,
	)
}

func extractMemoryFallbackEnabled(
	memoryNode *yaml.Node,
) (bool, bool, error) {
	value, found, err := firstMappingBoolValue(
		memoryNode,
		memoryFallbackKey,
		memoryFallbackCamelKey,
	)
	if err != nil {
		return false, false, err
	}
	if !found {
		return true, false, nil
	}
	deleteMappingKeys(memoryNode, memoryFallbackKey, memoryFallbackCamelKey)
	return value, true, nil
}

func decideSQLiteMemoryFallback(
	root *yaml.Node,
	memoryNode *yaml.Node,
	identity runtimeModelIdentity,
) (sqliteFallbackDecision, error) {
	target, err := resolveEmbedderTarget(memoryNode)
	if err != nil {
		return sqliteFallbackDecision{}, err
	}

	if looksLikeDeepSeekBaseURL(target.BaseURL) {
		return sqliteFallbackDecision{
			Reason: "the resolved embeddings base URL " +
				fmt.Sprintf("%q", target.BaseURL) +
				" points to DeepSeek, which does not " +
				"expose /embeddings for this setup",
		}, nil
	}
	if looksLikeDeepSeekModel(target.Model) {
		return sqliteFallbackDecision{
			Reason: "the configured embeddings model " +
				fmt.Sprintf("%q", target.Model) +
				" looks like a DeepSeek chat model",
		}, nil
	}

	if runtimeModelLooksDeepSeek(identity) &&
		strings.TrimSpace(target.ExplicitBaseURL) == "" &&
		strings.TrimSpace(target.ExplicitAPIKey) == "" &&
		!target.EnvAPIKeySet {
		return sqliteFallbackDecision{
			Reason: "the chat model is configured for DeepSeek " +
				"but memory.embedder has no separate embeddings " +
				"endpoint or API key configured",
		}, nil
	}
	return sqliteFallbackDecision{}, nil
}

func resolveEmbedderTarget(
	memoryNode *yaml.Node,
) (embedderTarget, error) {
	target := embedderTarget{
		EnvAPIKeySet: strings.TrimSpace(
			os.Getenv(openAIAPIKeyEnvName),
		) != "",
	}
	configNode := mappingValue(memoryNode, memoryConfigKey)
	if configNode == nil {
		target.BaseURL = strings.TrimSpace(
			os.Getenv(openAIBaseURLEnvName),
		)
		return target, nil
	}
	if configNode.Kind != yaml.MappingNode {
		return embedderTarget{}, fmt.Errorf(
			"config: %s.%s must be a mapping",
			memoryKey,
			memoryConfigKey,
		)
	}

	embedderNode := mappingValue(configNode, embedderKey)
	if embedderNode == nil {
		target.BaseURL = strings.TrimSpace(
			os.Getenv(openAIBaseURLEnvName),
		)
		return target, nil
	}
	if embedderNode.Kind != yaml.MappingNode {
		return embedderTarget{}, fmt.Errorf(
			"config: %s.%s.%s must be a mapping",
			memoryKey,
			memoryConfigKey,
			embedderKey,
		)
	}

	target.Model = mappingStringValue(embedderNode, embedderModelKey)
	target.ExplicitBaseURL = mappingStringValue(
		embedderNode,
		embedderBaseURLKey,
	)
	target.ExplicitAPIKey = mappingStringValue(
		embedderNode,
		embedderAPIKeyKey,
	)
	target.BaseURL = firstNonEmptyString(
		target.ExplicitBaseURL,
		strings.TrimSpace(os.Getenv(openAIBaseURLEnvName)),
	)
	return target, nil
}

func rewriteMemoryBackendToSQLite(
	root *yaml.Node,
	memoryNode *yaml.Node,
) error {
	setMappingString(memoryNode, memoryBackendKey, memoryBackendSQLiteName)

	configNode := mappingValue(memoryNode, memoryConfigKey)
	if configNode != nil && configNode.Kind != yaml.MappingNode {
		return fmt.Errorf(
			"config: %s.%s must be a mapping",
			memoryKey,
			memoryConfigKey,
		)
	}

	fallbackPath := resolveFallbackSQLitePath(root, memoryNode)
	sqliteConfig := buildSQLiteFallbackConfigNode(
		configNode,
		fallbackPath,
	)
	if sqliteConfig == nil {
		deleteMappingKey(memoryNode, memoryConfigKey)
		return nil
	}
	setMappingNode(memoryNode, memoryConfigKey, sqliteConfig)
	return nil
}

func resolveFallbackSQLitePath(
	root *yaml.Node,
	memoryNode *yaml.Node,
) string {
	configNode := mappingValue(memoryNode, memoryConfigKey)
	if configNode != nil {
		if raw := strings.TrimSpace(
			mappingStringValue(configNode, sqliteConfigPathKey),
		); raw != "" {
			return deriveFallbackSQLitePath(raw)
		}
		if raw := strings.TrimSpace(
			mappingStringValue(configNode, sqliteConfigDSNKey),
		); raw != "" {
			if path := fallbackSQLitePathFromDSN(raw); path != "" {
				return path
			}
		}
	}

	stateDir := resolveConfiguredStateDir(root)
	if stateDir == "" {
		stateDir = runtimeStateDirEnvRef
	}
	return filepath.Join(stateDir, defaultSQLiteMemoryDBFileName)
}

func resolveConfiguredStateDir(root *yaml.Node) string {
	doc := documentNode(root)
	if doc == nil {
		return defaultStateDir()
	}
	raw := strings.TrimSpace(mappingStringValue(doc, stateDirKey))
	if raw == "" {
		return defaultStateDir()
	}
	return raw
}

func buildSQLiteFallbackConfigNode(
	configNode *yaml.Node,
	fallbackPath string,
) *yaml.Node {
	sqliteConfig := &yaml.Node{
		Kind: yaml.MappingNode,
		Tag:  "!!map",
	}
	if configNode != nil && configNode.Kind == yaml.MappingNode {
		copyMappingValue(sqliteConfig, configNode, sqliteConfigDSNKey)
		copyMappingValue(sqliteConfig, configNode, sqliteTableNameKey)
		copyMappingValue(sqliteConfig, configNode, sqliteSkipDBInitKey)
		copyMappingValue(sqliteConfig, configNode, sqliteSoftDeleteKey)
	}
	if strings.TrimSpace(fallbackPath) != "" {
		setMappingString(sqliteConfig, sqliteConfigPathKey, fallbackPath)
		deleteMappingKey(sqliteConfig, sqliteConfigDSNKey)
	}
	if len(sqliteConfig.Content) == 0 {
		return nil
	}
	return sqliteConfig
}

func deriveFallbackSQLitePath(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" || value == ":memory:" {
		return ""
	}
	if strings.HasSuffix(value, defaultSQLiteVecDBFileName) {
		return strings.TrimSuffix(
			value,
			defaultSQLiteVecDBFileName,
		) + defaultSQLiteMemoryDBFileName
	}
	if strings.HasSuffix(value, sqliteFileExtension) {
		return strings.TrimSuffix(
			value,
			sqliteFileExtension,
		) + sqliteFallbackFileSuffix
	}
	return value + sqliteFallbackFileSuffix
}

func fallbackSQLitePathFromDSN(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "file:") {
		value = strings.TrimPrefix(value, "file:")
		if idx := strings.Index(value, "?"); idx >= 0 {
			value = value[:idx]
		}
	}
	return deriveFallbackSQLitePath(value)
}

func looksLikeDeepSeekBaseURL(raw string) bool {
	value := strings.ToLower(strings.TrimSpace(raw))
	return strings.Contains(value, deepSeekAPIHost)
}

func looksLikeDeepSeekModel(raw string) bool {
	value := strings.ToLower(strings.TrimSpace(raw))
	return strings.HasPrefix(value, deepSeekModelPrefix)
}

func runtimeModelLooksDeepSeek(identity runtimeModelIdentity) bool {
	if looksLikeDeepSeekModel(identity.ModelName) {
		return true
	}
	if looksLikeDeepSeekBaseURL(identity.OpenAIBaseURL) {
		return true
	}
	return strings.EqualFold(
		strings.TrimSpace(identity.OpenAIVariant),
		"deepseek",
	)
}

func emitConfigWarnings(warnings []string) {
	if len(warnings) == 0 {
		return
	}
	for _, warning := range warnings {
		message := strings.TrimSpace(warning)
		if message == "" {
			continue
		}
		configWarningEmitter(runtimeIdentityWarningPrefix + message)
	}
}

func normalizeNonEmptyString(value string, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(fallback)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func firstMappingBoolValue(
	root *yaml.Node,
	keys ...string,
) (bool, bool, error) {
	node := firstMappingValue(root, keys...)
	if node == nil {
		return false, false, nil
	}
	value, err := scalarBoolValue(node)
	if err != nil {
		return false, false, err
	}
	return value, true, nil
}

func scalarBoolValue(node *yaml.Node) (bool, error) {
	if node == nil {
		return false, nil
	}
	var value bool
	if err := node.Decode(&value); err != nil {
		return false, fmt.Errorf(
			"invalid boolean value %q",
			strings.TrimSpace(node.Value),
		)
	}
	return value, nil
}

func normalizeConfiguredPersonaID(raw string) (string, bool) {
	key := strings.ToLower(strings.TrimSpace(raw))
	switch key {
	case "":
		return personaapi.DefaultID, true
	case "off", "none", "reset":
		return "", false
	default:
		return personaapi.NormalizeID(key), true
	}
}

func applyAgentInstructionDefaults(
	root *yaml.Node,
	args []string,
	stateDir string,
	cfgPath string,
) (bool, error) {
	doc := documentNode(root)
	if doc == nil {
		return false, nil
	}
	agentNode := ensureMappingValue(doc, agentKey)
	if agentNode == nil {
		return false, nil
	}
	memoryNode := mappingValue(doc, memoryKey)
	text, err := loadPromptBundleFromConfig(
		configBaseDir(cfgPath),
		yamlSequenceValues(mappingValue(agentNode, instructionFilesKey)),
		mappingStringValue(agentNode, instructionDirKey),
		stateDir,
		shouldInjectDefaultMemoryInstruction(memoryNode, args),
		promptBundleSpec{
			embeddedDir: promptasset.DefaultInstructionEmbeddedDir,
			defaultDir: func(paths promptasset.Paths) string {
				return paths.InstructionDir
			},
			defaultFiles: []string{promptasset.DefaultMemoryFileName},
		},
		nil,
	)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(text) == "" {
		return false, nil
	}

	existing := mappingStringValue(agentNode, instructionKey)
	setMappingString(
		agentNode,
		instructionKey,
		appendPromptText(existing, text),
	)
	return true, nil
}

func applyAgentSystemPromptDefaults(
	root *yaml.Node,
	args []string,
	stateDir string,
	cfgPath string,
	identity runtimeModelIdentity,
	defaults codingAgentDefaults,
	codingEnabled bool,
) (bool, error) {
	_ = args
	doc := documentNode(root)
	if doc == nil {
		return false, nil
	}
	agentNode := ensureMappingValue(doc, agentKey)
	if agentNode == nil {
		return false, nil
	}

	explicitSystemPromptConfig := hasPromptBundleConfig(
		yamlSequenceValues(mappingValue(agentNode, systemPromptFilesKey)),
		mappingStringValue(agentNode, systemPromptDirKey),
	)
	nameState, err := loadRuntimeAssistantNameState(root, stateDir)
	if err != nil {
		return false, err
	}
	vars := buildSystemPromptTemplateVars(
		identity,
		defaults,
		nameState.EffectiveName,
	)
	text, err := loadPromptBundleFromConfig(
		configBaseDir(cfgPath),
		yamlSequenceValues(mappingValue(agentNode, systemPromptFilesKey)),
		mappingStringValue(agentNode, systemPromptDirKey),
		stateDir,
		true,
		promptBundleSpec{
			embeddedDir: promptasset.DefaultSystemEmbeddedDir,
			defaultDir: func(paths promptasset.Paths) string {
				return paths.SystemDir
			},
			defaultFiles: defaultSystemPromptFiles(),
		},
		vars,
	)
	if err != nil {
		return false, err
	}
	if codingEnabled && !explicitSystemPromptConfig {
		text = appendPromptText(
			text,
			buildCodingAgentSystemPrompt(defaults),
		)
	}

	personaPrompt, personaChanged, err := configuredPersonaPrompt(
		agentNode,
		stateDir,
		cfgPath,
	)
	if err != nil {
		return false, err
	}

	existingRaw := mappingStringValue(agentNode, systemPromptKey)
	existing := stripLegacyManagedSystemPrompt(existingRaw)
	legacyInlineRemoved := strings.TrimSpace(existingRaw) != existing
	merged := appendPromptText(existing, text)
	merged = appendPromptText(merged, personaPrompt)
	if strings.TrimSpace(merged) == strings.TrimSpace(existing) &&
		!personaChanged &&
		!legacyInlineRemoved {
		return false, nil
	}
	if strings.TrimSpace(merged) == "" {
		if mappingValue(agentNode, systemPromptKey) != nil {
			deleteMappingKey(agentNode, systemPromptKey)
			return true, nil
		}
		return personaChanged || legacyInlineRemoved, nil
	}
	setMappingString(agentNode, systemPromptKey, merged)
	return true, nil
}

func hasPromptBundleConfig(files []string, dir string) bool {
	return len(files) > 0 || strings.TrimSpace(dir) != ""
}

func defaultSystemPromptFiles() []string {
	return []string{promptasset.DefaultRuntimeIdentityFileName}
}

func buildSystemPromptTemplateVars(
	identity runtimeModelIdentity,
	defaults codingAgentDefaults,
	assistantName string,
) map[string]string {
	vars := map[string]string{
		"TRPC_CLAW_ASSISTANT_NAME":       assistantName,
		"TRPC_CLAW_RUNTIME_PRODUCT_NAME": runtimeProductName,
		"TRPC_CLAW_RUNTIME_MODEL_MODE": firstNonEmptyString(
			identity.ModelMode,
			defaultRuntimeModelMode,
		),
		"TRPC_CLAW_RUNTIME_MODEL_NAME_LINE":        "",
		"TRPC_CLAW_RUNTIME_OPENAI_VARIANT_LINE":    "",
		"TRPC_CLAW_RUNTIME_PROVIDER_BASE_URL_LINE": "",
		"TRPC_CLAW_RUNTIME_AUTONOMY_RULE":          runtimeAutonomyRule,
		"TRPC_CLAW_RUNTIME_GOAL_COMPLETION_RULE":   runtimeGoalCompletionRule,
		"TRPC_CLAW_RUNTIME_MINIMAL_QUESTION_RULE":  runtimeMinimalQuestionRule,
		"TRPC_CLAW_RUNTIME_NO_CHOICE_TAIL_RULE":    runtimeNoChoiceTailRule,
		"TRPC_CLAW_RUNTIME_SKILL_FIRST_CAPABILITY_RULE": "" +
			runtimeSkillFirstCapabilityRule,
		"TRPC_CLAW_RUNTIME_SKILL_PLATFORM_BOUNDARY_RULE": "" +
			runtimeSkillPlatformBoundaryRule,
		"TRPC_CLAW_RUNTIME_PRIVATE_CONFIG_RULE": "" +
			runtimePrivateConfigRule,
		"TRPC_CLAW_RUNTIME_SKILL_FOLLOW_THROUGH_RULE": "" +
			runtimeSkillFollowThroughRule,
		"TRPC_CLAW_CODING_EXECUTION_MODE_GUIDANCE": "",
		"TRPC_CLAW_CODING_ARTIFACT_GUIDANCE":       "",
		"TRPC_CLAW_CODING_WORKDIR_LINE":            "",
		"TRPC_CLAW_CODING_OUTPUT_ROOT_LINE":        "",
		"TRPC_CLAW_CODING_TEMP_ROOT_LINE":          "",
	}
	if value := strings.TrimSpace(identity.ModelName); value != "" {
		vars["TRPC_CLAW_RUNTIME_MODEL_NAME_LINE"] =
			"Runtime model name: " + value
	}
	if value := strings.TrimSpace(identity.OpenAIVariant); value != "" {
		vars["TRPC_CLAW_RUNTIME_OPENAI_VARIANT_LINE"] =
			"Runtime OpenAI variant: " + value
	}
	if value := strings.TrimSpace(identity.OpenAIBaseURL); value != "" {
		vars["TRPC_CLAW_RUNTIME_PROVIDER_BASE_URL_LINE"] =
			"Runtime provider base URL: " + value
	}
	if vars["TRPC_CLAW_ASSISTANT_NAME"] == "" {
		vars["TRPC_CLAW_ASSISTANT_NAME"] = runtimeProductName
	}

	mode := strings.TrimSpace(defaults.ExecutionMode)
	if mode != "" {
		vars["TRPC_CLAW_CODING_EXECUTION_MODE_GUIDANCE"] =
			buildCodingAgentExecutionModeGuidance(mode)
	}
	if value := strings.TrimSpace(
		buildCodingAgentArtifactGuidance(defaults),
	); value != "" {
		vars["TRPC_CLAW_CODING_ARTIFACT_GUIDANCE"] = value
	}
	facts := collectCodingWorkspaceFacts(defaults.DefaultWorkdir)
	if facts.Workdir != "" {
		vars["TRPC_CLAW_CODING_WORKDIR_LINE"] =
			buildCodingWorkdirPromptLine(facts)
	}
	if value := strings.TrimSpace(
		buildOutputRootPromptLine(defaults),
	); value != "" {
		vars["TRPC_CLAW_CODING_OUTPUT_ROOT_LINE"] = value
	}
	if value := strings.TrimSpace(
		buildTempRootPromptLine(defaults),
	); value != "" {
		vars["TRPC_CLAW_CODING_TEMP_ROOT_LINE"] = value
	}
	return vars
}

func configuredPersonaPrompt(
	agentNode *yaml.Node,
	stateDir string,
	cfgPath string,
) (string, bool, error) {
	if agentNode == nil {
		return "", false, nil
	}
	personaNode := mappingValue(agentNode, personaKey)
	rawPersonaID := ""
	if personaNode != nil {
		rawPersonaID = personaNode.Value
		deleteMappingKey(agentNode, personaKey)
	}

	personaID, enabled := normalizeConfiguredPersonaID(
		rawPersonaID,
	)
	if !enabled {
		return "", personaNode != nil, nil
	}

	registryDir, err := resolveAgentPersonaDir(
		agentNode,
		stateDir,
		cfgPath,
	)
	if err != nil {
		return "", false, err
	}
	registry := personaapi.NewRegistry(registryDir)
	def, ok, err := registry.Get(personaID)
	if err != nil {
		return "", false, err
	}
	if !ok {
		return "", false, fmt.Errorf(
			"config: unknown agent.persona %q",
			strings.TrimSpace(rawPersonaID),
		)
	}
	return strings.TrimSpace(def.Prompt), true, nil
}

func resolveAgentPersonaDir(
	agentNode *yaml.Node,
	stateDir string,
	cfgPath string,
) (string, error) {
	override := mappingStringValue(agentNode, agentPersonaDirKey)
	_, dir, err := promptasset.ResolvePaths(
		configBaseDir(cfgPath),
		nil,
		override,
	)
	if err != nil {
		return "", err
	}
	if dir != "" {
		return dir, nil
	}
	if strings.TrimSpace(stateDir) == "" {
		return "", nil
	}
	paths := promptasset.DefaultPaths(stateDir)
	return paths.PersonaDir, nil
}

func shouldInjectDefaultMemoryInstruction(
	memoryNode *yaml.Node,
	args []string,
) bool {
	switch effectiveMemoryBackend(memoryNode, args) {
	case memoryBackendFileName:
		return true
	default:
		return false
	}
}

func appendPromptText(text string, suffix string) string {
	text = strings.TrimSpace(text)
	suffix = strings.TrimSpace(suffix)
	switch {
	case text == "":
		return suffix
	case suffix == "":
		return text
	default:
		return text + "\n\n" + suffix
	}
}

func resolveCodingAgentDefaults(
	root *yaml.Node,
	stateDir string,
) (codingAgentDefaults, bool, error) {
	doc := documentNode(root)
	if doc == nil {
		defaults, err := extractCodingAgentDefaults(nil, stateDir)
		return defaults, false, err
	}
	skillsNode := mappingValue(doc, skillsKey)
	if skillsNode == nil {
		defaults, err := extractCodingAgentDefaults(nil, stateDir)
		return defaults, false, err
	}
	codingNode := firstMappingValue(
		skillsNode,
		codingAgentKey,
		codingAgentCamelKey,
	)
	defaults, err := extractCodingAgentDefaults(codingNode, stateDir)
	return defaults, codingNode != nil, err
}

func applySkillsCodingAgentDefaults(
	root *yaml.Node,
	defaults codingAgentDefaults,
	enabled bool,
) (bool, error) {
	if !enabled {
		return false, nil
	}
	doc := documentNode(root)
	if doc == nil {
		return false, nil
	}
	skillsNode := mappingValue(doc, skillsKey)
	if skillsNode == nil {
		return false, nil
	}
	deleteMappingKeys(
		skillsNode,
		codingAgentKey,
		codingAgentCamelKey,
	)

	changed := true

	if firstMappingValue(
		skillsNode,
		toolingGuidanceKey,
		toolingGuidanceCamelKey,
	) != nil {
		return changed, nil
	}

	setMappingString(
		skillsNode,
		toolingGuidanceKey,
		buildSkillsToolingGuidanceWithRoots(defaults, nil),
	)
	deleteMappingKey(skillsNode, toolingGuidanceCamelKey)
	return true, nil
}

func applyWeComRuntimeDefaults(
	root *yaml.Node,
	defaults codingAgentDefaults,
	identity runtimeModelIdentity,
	replyDeliveryRoots []string,
	stateDir string,
	cfgPath string,
) (bool, error) {
	doc := documentNode(root)
	if doc == nil {
		return false, nil
	}
	channelsNode := mappingValue(doc, channelsKey)
	if channelsNode == nil {
		return false, nil
	}
	if channelsNode.Kind != yaml.SequenceNode {
		return false, fmt.Errorf("config: channels must be a sequence")
	}

	changed := false
	agentNode := mappingValue(doc, agentKey)
	agentPersonaDir, err := resolveAgentPersonaDir(
		agentNode,
		stateDir,
		cfgPath,
	)
	if err != nil {
		return false, err
	}
	for _, channelNode := range channelsNode.Content {
		if channelNode == nil || channelNode.Kind != yaml.MappingNode {
			continue
		}
		if mappingStringValue(channelNode, channelTypeKey) !=
			wecomchannel.PluginType {
			continue
		}
		configNode := ensureMappingValue(channelNode, channelConfigKey)
		if configNode == nil {
			continue
		}
		if syncWeComRuntimeConfigValue(
			configNode,
			wecomchannel.RuntimeDefaultWorkdirConfigKey,
			defaults.DefaultWorkdir,
		) {
			changed = true
		}
		if syncWeComRuntimeConfigValue(
			configNode,
			wecomchannel.RuntimeScratchRootConfigKey,
			defaults.ScratchRoot,
		) {
			changed = true
		}
		if syncWeComRuntimeConfigValue(
			configNode,
			wecomchannel.RuntimeModelNameConfigKey,
			identity.ModelName,
		) {
			changed = true
		}
		if syncWeComRuntimeConfigSequence(
			configNode,
			wecomchannel.RuntimeReplyDeliveryRootsConfigKey,
			replyDeliveryRoots,
		) {
			changed = true
		}
		pathChanged, err := normalizeWeComPromptConfigPaths(
			configNode,
			cfgPath,
			agentPersonaDir,
		)
		if err != nil {
			return false, err
		}
		if pathChanged {
			changed = true
		}
	}
	return changed, nil
}

func normalizeWeComPromptConfigPaths(
	configNode *yaml.Node,
	cfgPath string,
	agentPersonaDir string,
) (bool, error) {
	if configNode == nil {
		return false, nil
	}

	changed := false
	if agentPersonaDir != "" &&
		mappingStringValue(configNode, agentPersonaDirKey) == "" {
		setMappingString(
			configNode,
			agentPersonaDirKey,
			agentPersonaDir,
		)
		changed = true
	}

	for _, key := range []string{
		agentPersonaDirKey,
		wecomchannel.RequestSystemPromptDirConfigKey,
	} {
		value := mappingStringValue(configNode, key)
		if value == "" {
			continue
		}
		_, dir, err := promptasset.ResolvePaths(
			configBaseDir(cfgPath),
			nil,
			value,
		)
		if err != nil {
			return false, err
		}
		if dir == "" || dir == value {
			continue
		}
		setMappingString(configNode, key, dir)
		changed = true
	}

	filesNode := mappingValue(
		configNode,
		wecomchannel.RequestSystemPromptFilesConfigKey,
	)
	files := yamlSequenceValues(filesNode)
	if len(files) > 0 {
		resolved, _, err := promptasset.ResolvePaths(
			configBaseDir(cfgPath),
			files,
			"",
		)
		if err != nil {
			return false, err
		}
		if !reflect.DeepEqual(resolved, files) {
			setMappingSequence(
				configNode,
				wecomchannel.RequestSystemPromptFilesConfigKey,
				resolved,
			)
			changed = true
		}
	}
	return changed, nil
}

func resolveWeComReplyDeliveryRoots(
	root *yaml.Node,
) ([]string, error) {
	doc := documentNode(root)
	if doc == nil {
		return nil, nil
	}
	toolsNode := mappingValue(doc, toolsKey)
	if toolsNode == nil {
		return nil, nil
	}
	toolsetsNode := mappingValue(toolsNode, toolsetsKey)
	if toolsetsNode == nil {
		return nil, nil
	}
	if toolsetsNode.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf(
			"config: %s.%s must be a sequence",
			toolsKey,
			toolsetsKey,
		)
	}

	seen := make(map[string]struct{}, len(toolsetsNode.Content))
	roots := make([]string, 0, len(toolsetsNode.Content))
	for index, toolsetNode := range toolsetsNode.Content {
		if toolsetNode == nil {
			continue
		}
		if toolsetNode.Kind != yaml.MappingNode {
			return nil, fmt.Errorf(
				"config: %s.%s[%d] must be a mapping",
				toolsKey,
				toolsetsKey,
				index,
			)
		}
		if normalizeToolsetType(
			mappingStringValue(toolsetNode, toolTypeKey),
		) != fileToolTypeName {
			continue
		}

		configNode := mappingValue(toolsetNode, toolConfigKey)
		if configNode != nil && configNode.Kind != yaml.MappingNode {
			return nil, fmt.Errorf(
				"config: %s.%s[%d].%s must be a mapping",
				toolsKey,
				toolsetsKey,
				index,
				toolConfigKey,
			)
		}

		readOnly, err := resolveFileToolReadOnly(configNode)
		if err != nil {
			return nil, fmt.Errorf(
				"config: %s.%s[%d].%s: %w",
				toolsKey,
				toolsetsKey,
				index,
				toolConfigKey,
				err,
			)
		}
		if readOnly {
			continue
		}

		rootPath, err := resolveExplicitFileToolBaseDir(configNode)
		if err != nil {
			return nil, fmt.Errorf(
				"config: %s.%s[%d].%s: %w",
				toolsKey,
				toolsetsKey,
				index,
				toolConfigKey,
				err,
			)
		}
		if rootPath == "" {
			continue
		}
		if _, exists := seen[rootPath]; exists {
			continue
		}
		seen[rootPath] = struct{}{}
		roots = append(roots, rootPath)
	}
	return roots, nil
}

func resolveExplicitFileToolBaseDir(
	configNode *yaml.Node,
) (string, error) {
	raw := strings.TrimSpace(firstNonEmptyString(
		mappingStringValue(configNode, fileToolBaseDirKey),
		mappingStringValue(configNode, fileToolBaseDirCamelKey),
	))
	if raw == "" {
		return "", nil
	}

	path, err := workspacecfg.NormalizeDir(raw, false)
	if err != nil {
		return "", fmt.Errorf(
			"invalid %s %q: %w",
			fileToolBaseDirKey,
			raw,
			err,
		)
	}
	return path, nil
}

func normalizeToolsetType(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func resolveFileToolReadOnly(
	configNode *yaml.Node,
) (bool, error) {
	if configNode == nil {
		return false, nil
	}
	value, ok, err := firstMappingBoolValue(
		configNode,
		fileToolReadOnlyKey,
		fileToolReadOnlyCamelKey,
	)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	return value, nil
}

func syncWeComRuntimeConfigValue(
	configNode *yaml.Node,
	key string,
	value string,
) bool {
	value = strings.TrimSpace(value)
	current := mappingStringValue(configNode, key)
	if value == "" {
		if current == "" {
			return false
		}
		deleteMappingKey(configNode, key)
		return true
	}
	if current == value {
		return false
	}
	setMappingString(configNode, key, value)
	return true
}

func syncWeComRuntimeConfigSequence(
	configNode *yaml.Node,
	key string,
	values []string,
) bool {
	values = normalizeStringSequence(values)
	current := mappingValue(configNode, key)
	if len(values) == 0 {
		if current == nil {
			return false
		}
		deleteMappingKey(configNode, key)
		return true
	}
	if reflect.DeepEqual(
		yamlSequenceValues(current),
		values,
	) {
		return false
	}
	setMappingSequence(configNode, key, values)
	return true
}

func normalizeStringSequence(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func yamlSequenceValues(node *yaml.Node) []string {
	if node == nil || node.Kind != yaml.SequenceNode {
		return nil
	}

	values := make([]string, 0, len(node.Content))
	for _, child := range node.Content {
		if child == nil {
			continue
		}
		value := strings.TrimSpace(child.Value)
		if value == "" {
			continue
		}
		values = append(values, value)
	}
	return values
}

func applySkillsExtraDirDefaults(
	root *yaml.Node,
) (bool, error) {
	doc := documentNode(root)
	if doc == nil {
		return false, nil
	}
	skillsNode := mappingValue(doc, skillsKey)
	if skillsNode == nil {
		return false, nil
	}

	codexSkillsDir := defaultCodexSkillsDir()
	if codexSkillsDir == "" || !isExistingDir(codexSkillsDir) {
		return false, nil
	}

	extraDirsNode := mappingValue(skillsNode, extraDirsKey)
	if extraDirsNode == nil {
		setMappingSequence(
			skillsNode,
			extraDirsKey,
			[]string{codexSkillsDir},
		)
		return true, nil
	}
	if extraDirsNode.Kind != yaml.SequenceNode {
		return false, fmt.Errorf(
			"config: skills.%s must be a sequence",
			extraDirsKey,
		)
	}
	if sequenceContainsPath(extraDirsNode, codexSkillsDir) {
		return false, nil
	}
	extraDirsNode.Content = append(
		extraDirsNode.Content,
		&yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!str",
			Value: codexSkillsDir,
		},
	)
	return true, nil
}

func applySkillsDefaults(
	root *yaml.Node,
) (bool, error) {
	doc := documentNode(root)
	if doc == nil {
		return false, nil
	}
	skillsNode := mappingValue(doc, skillsKey)
	if skillsNode == nil {
		return false, nil
	}

	changed := false
	if applyMappingStringDefault(
		skillsNode,
		skillsLoadModeKey,
		skillsLoadModeCamelKey,
		skillsLoadModeSession,
	) {
		changed = true
	}
	if applyMappingIntDefault(
		skillsNode,
		skillsMaxLoadedKey,
		skillsMaxLoadedCamelKey,
		defaultSkillsMaxLoaded,
	) {
		changed = true
	}
	if applyMappingBoolDefault(
		skillsNode,
		skillsSkipFallbackKey,
		skillsSkipFallbackCamelKey,
		defaultSkillsSkipFallback,
	) {
		changed = true
	}
	disabledByDefault, err := applyDefaultSkillEntryEnabled(
		skillsNode,
		codingAgentSkillName,
		false,
	)
	if err != nil {
		return false, err
	}
	if disabledByDefault {
		changed = true
	}
	return changed, nil
}

func applyDefaultSkillEntryEnabled(
	skillsNode *yaml.Node,
	skillName string,
	enabled bool,
) (bool, error) {
	entriesNode := ensureMappingValue(skillsNode, skillsEntriesKey)
	if entriesNode == nil {
		return false, nil
	}
	entryNode := mappingValue(entriesNode, skillName)
	if entryNode != nil && entryNode.Kind != yaml.MappingNode {
		return false, fmt.Errorf(
			"config: skills.%s.%s must be a mapping",
			skillsEntriesKey,
			skillName,
		)
	}
	entryNode = ensureMappingValue(entriesNode, skillName)
	if entryNode == nil {
		return false, nil
	}
	if mappingValue(entryNode, skillEnabledKey) != nil {
		return false, nil
	}
	setMappingBool(entryNode, skillEnabledKey, enabled)
	return true, nil
}

func applyMappingStringDefault(
	root *yaml.Node,
	snakeKey string,
	camelKey string,
	value string,
) bool {
	node := firstMappingValue(root, snakeKey, camelKey)
	if node != nil && strings.TrimSpace(node.Value) != "" {
		return false
	}
	setMappingString(root, defaultMappingKey(root, snakeKey, camelKey), value)
	return true
}

func applyMappingIntDefault(
	root *yaml.Node,
	snakeKey string,
	camelKey string,
	value int,
) bool {
	node := firstMappingValue(root, snakeKey, camelKey)
	if node != nil && strings.TrimSpace(node.Value) != "" {
		return false
	}
	setMappingInt(root, defaultMappingKey(root, snakeKey, camelKey), value)
	return true
}

func applyMappingBoolDefault(
	root *yaml.Node,
	snakeKey string,
	camelKey string,
	value bool,
) bool {
	node := firstMappingValue(root, snakeKey, camelKey)
	if node != nil && strings.TrimSpace(node.Value) != "" {
		return false
	}
	setMappingBool(root, defaultMappingKey(root, snakeKey, camelKey), value)
	return true
}

func defaultMappingKey(
	root *yaml.Node,
	snakeKey string,
	camelKey string,
) string {
	if mappingValue(root, snakeKey) == nil &&
		mappingValue(root, camelKey) != nil {
		return camelKey
	}
	return snakeKey
}

func applyEnvProbeToolProviderDefaults(
	root *yaml.Node,
) (bool, error) {
	doc := documentNode(root)
	if doc == nil {
		return false, nil
	}
	toolsNode := mappingValue(doc, toolsKey)
	if toolsNode == nil {
		return false, nil
	}

	providersNode := mappingValue(toolsNode, toolProvidersKey)
	if providersNode == nil {
		sequence := &yaml.Node{
			Kind: yaml.SequenceNode,
			Tag:  "!!seq",
		}
		sequence.Content = append(
			sequence.Content,
			newToolProviderNode(envprobeplugin.PluginType),
		)
		setMappingNode(toolsNode, toolProvidersKey, sequence)
		return true, nil
	}
	if providersNode.Kind != yaml.SequenceNode {
		return false, fmt.Errorf(
			"config: %s.%s must be a sequence",
			toolsKey,
			toolProvidersKey,
		)
	}
	if hasToolProviderType(
		providersNode,
		envprobeplugin.PluginType,
	) {
		return false, nil
	}
	providersNode.Content = append(
		providersNode.Content,
		newToolProviderNode(envprobeplugin.PluginType),
	)
	return true, nil
}

func applyBrowserToolProviderDefaults(
	root *yaml.Node,
) (bool, error) {
	doc := documentNode(root)
	if doc == nil {
		return false, nil
	}
	toolsNode := mappingValue(doc, toolsKey)
	if toolsNode == nil {
		return false, nil
	}

	providersNode := mappingValue(toolsNode, toolProvidersKey)
	if providersNode == nil {
		sequence := &yaml.Node{
			Kind: yaml.SequenceNode,
			Tag:  "!!seq",
		}
		sequence.Content = append(
			sequence.Content,
			newBrowserToolProviderNode(),
		)
		setMappingNode(toolsNode, toolProvidersKey, sequence)
		return true, nil
	}
	if providersNode.Kind != yaml.SequenceNode {
		return false, fmt.Errorf(
			"config: %s.%s must be a sequence",
			toolsKey,
			toolProvidersKey,
		)
	}
	if hasToolProviderType(
		providersNode,
		browserToolProviderTypeName,
	) {
		return false, nil
	}
	providersNode.Content = append(
		providersNode.Content,
		newBrowserToolProviderNode(),
	)
	return true, nil
}

func applyAssistantNameToolProviderDefaults(
	root *yaml.Node,
) (bool, error) {
	if len(collectWeComConfigNodes(root)) == 0 {
		return false, nil
	}

	doc := documentNode(root)
	if doc == nil {
		return false, nil
	}
	toolsNode := mappingValue(doc, toolsKey)
	if toolsNode == nil {
		return false, nil
	}

	providersNode := mappingValue(toolsNode, toolProvidersKey)
	if providersNode == nil {
		sequence := &yaml.Node{
			Kind: yaml.SequenceNode,
			Tag:  "!!seq",
		}
		sequence.Content = append(
			sequence.Content,
			newToolProviderNode(
				wecomchannel.AssistantNameToolProviderType,
			),
		)
		setMappingNode(toolsNode, toolProvidersKey, sequence)
		return true, nil
	}
	if providersNode.Kind != yaml.SequenceNode {
		return false, fmt.Errorf(
			"config: %s.%s must be a sequence",
			toolsKey,
			toolProvidersKey,
		)
	}
	if hasToolProviderType(
		providersNode,
		wecomchannel.AssistantNameToolProviderType,
	) {
		return false, nil
	}
	providersNode.Content = append(
		providersNode.Content,
		newToolProviderNode(wecomchannel.AssistantNameToolProviderType),
	)
	return true, nil
}

func applySourceTreeSkillsDefaults(
	root *yaml.Node,
	cfgPath string,
	stateDir string,
) (bool, error) {
	doc := documentNode(root)
	if doc == nil {
		return false, nil
	}
	skillsNode := mappingValue(doc, skillsKey)
	if skillsNode == nil {
		return false, nil
	}

	sourceSkillsDir := configAdjacentSkillsDir(cfgPath)
	if sourceSkillsDir == "" {
		return false, nil
	}

	bundledSkillsDir := defaultBundledSkillsDir(stateDir)
	if bundledSkillsDir == "" {
		return false, nil
	}

	currentRoot := mappingStringValue(skillsNode, skillsRootKey)
	if !sameNormalizedPath(currentRoot, bundledSkillsDir) {
		return false, nil
	}
	if sameNormalizedPath(currentRoot, sourceSkillsDir) {
		return false, nil
	}

	setMappingString(skillsNode, skillsRootKey, sourceSkillsDir)
	changed := true
	if isExistingDir(bundledSkillsDir) {
		extraChanged, err := ensureSkillsExtraDir(
			skillsNode,
			bundledSkillsDir,
		)
		if err != nil {
			return false, err
		}
		changed = changed || extraChanged
	}
	return changed, nil
}

func configAdjacentSkillsDir(cfgPath string) string {
	dir := strings.TrimSpace(cfgPath)
	if dir == "" {
		return ""
	}
	baseDir := filepath.Dir(dir)
	if strings.TrimSpace(baseDir) == "" {
		return ""
	}
	candidate, err := filepath.Abs(
		filepath.Join(baseDir, skillsDirName),
	)
	if err != nil {
		candidate = filepath.Clean(
			filepath.Join(baseDir, skillsDirName),
		)
	}
	if !isDirectSkillRoot(candidate) {
		return ""
	}
	return candidate
}

func isDirectSkillRoot(path string) bool {
	if !isExistingDir(path) {
		return false
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry == nil || !entry.IsDir() {
			continue
		}
		if fileExists(
			filepath.Join(
				path,
				entry.Name(),
				skillDocFileName,
			),
		) {
			return true
		}
	}
	return false
}

func defaultBundledSkillsDir(stateDir string) string {
	dir := strings.TrimSpace(stateDir)
	if dir == "" {
		return ""
	}
	return filepath.Join(
		dir,
		skillsDirName,
		bundledSkillsDirName,
	)
}

func ensureSkillsExtraDir(
	skillsNode *yaml.Node,
	dir string,
) (bool, error) {
	if skillsNode == nil {
		return false, nil
	}

	extraDirsNode := mappingValue(skillsNode, extraDirsKey)
	if extraDirsNode == nil {
		setMappingSequence(skillsNode, extraDirsKey, []string{dir})
		return true, nil
	}
	if extraDirsNode.Kind != yaml.SequenceNode {
		return false, fmt.Errorf(
			"config: skills.%s must be a sequence",
			extraDirsKey,
		)
	}
	if sequenceContainsPath(extraDirsNode, dir) {
		return false, nil
	}
	extraDirsNode.Content = append(
		extraDirsNode.Content,
		&yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!str",
			Value: dir,
		},
	)
	return true, nil
}

func sameNormalizedPath(left string, right string) bool {
	normalizedLeft, leftOK := normalizedPathValue(left)
	if !leftOK {
		return false
	}
	normalizedRight, rightOK := normalizedPathValue(right)
	if !rightOK {
		return false
	}
	return normalizedLeft == normalizedRight
}

func extractCodingAgentExecutionMode(
	codingNode *yaml.Node,
) (string, error) {
	if codingNode == nil {
		return defaultCodingAgentExecutionMode, nil
	}
	if codingNode.Kind != yaml.MappingNode {
		return "", fmt.Errorf(
			"config: skills.%s must be a mapping",
			codingAgentKey,
		)
	}
	modeNode := firstMappingValue(
		codingNode,
		executionModeKey,
		executionModeCamelKey,
	)
	if modeNode == nil {
		return defaultCodingAgentExecutionMode, nil
	}
	mode := normalizeCodingAgentExecutionMode(modeNode.Value)
	switch mode {
	case codingAgentModeAuto,
		codingAgentModeSandbox,
		codingAgentModeHost:
		return mode, nil
	default:
		return "", fmt.Errorf(
			"config: unknown skills.%s.%s %q "+
				"(supported: %s)",
			codingAgentKey,
			executionModeKey,
			strings.TrimSpace(modeNode.Value),
			supportedCodingAgentModes,
		)
	}
}

func normalizeCodingAgentExecutionMode(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func extractCodingAgentDefaults(
	codingNode *yaml.Node,
	stateDir string,
) (codingAgentDefaults, error) {
	mode, err := extractCodingAgentExecutionMode(codingNode)
	if err != nil {
		return codingAgentDefaults{}, err
	}

	defaultWorkdir, err := extractCodingAgentPath(
		codingNode,
		true,
		defaultWorkdirKey,
		defaultWorkdirCamelKey,
	)
	if err != nil {
		return codingAgentDefaults{}, err
	}
	if defaultWorkdir == "" {
		defaultWorkdir = workspacecfg.ImplicitDefaultWorkdir()
	}

	scratchRoot, err := extractCodingAgentPath(
		codingNode,
		false,
		scratchRootKey,
		scratchRootCamelKey,
	)
	if err != nil {
		return codingAgentDefaults{}, err
	}
	if scratchRoot == "" {
		scratchRoot = workspacecfg.DefaultScratchRoot(stateDir)
	}
	scratchRoot, err = workspacecfg.EnsureScratchRoot(scratchRoot)
	if err != nil {
		return codingAgentDefaults{}, fmt.Errorf(
			"config: prepare skills.%s.%s: %w",
			codingAgentKey,
			scratchRootKey,
			err,
		)
	}

	outputRoot, err := ensureScratchOutputRoot(scratchRoot)
	if err != nil {
		return codingAgentDefaults{}, fmt.Errorf(
			"config: prepare scratch output root: %w",
			err,
		)
	}

	tempRoot, err := ensureRuntimeTempRoot(stateDir)
	if err != nil {
		return codingAgentDefaults{}, fmt.Errorf(
			"config: prepare runtime temp root: %w",
			err,
		)
	}

	return codingAgentDefaults{
		ExecutionMode:  mode,
		DefaultWorkdir: defaultWorkdir,
		ScratchRoot:    scratchRoot,
		OutputRoot:     outputRoot,
		TempRoot:       tempRoot,
	}, nil
}

func extractCodingAgentPath(
	codingNode *yaml.Node,
	requireExisting bool,
	keys ...string,
) (string, error) {
	node := firstMappingValue(codingNode, keys...)
	if node == nil {
		return "", nil
	}

	value := strings.TrimSpace(node.Value)
	return normalizeCodingAgentPathValue(
		value,
		requireExisting,
		keys[0],
	)
}

func normalizeCodingAgentPathValue(
	value string,
	requireExisting bool,
	fieldName string,
) (string, error) {
	if value == "" {
		return "", nil
	}

	expanded, err := workspacecfg.NormalizeDir(
		value,
		requireExisting,
	)
	if err != nil {
		if requireExisting {
			return "", fmt.Errorf(
				"config: skills.%s.%s %q must be an "+
					"existing directory",
				codingAgentKey,
				fieldName,
				value,
			)
		}
		return "", err
	}
	if expanded == "" {
		return "", nil
	}
	return expanded, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info == nil {
		return false
	}
	return !info.IsDir()
}

func applyCodingAgentSystemPrompt(
	doc *yaml.Node,
	defaults codingAgentDefaults,
) (bool, error) {
	if doc == nil {
		return false, nil
	}
	agentNode := ensureMappingValue(doc, agentKey)
	if agentNode == nil {
		return false, nil
	}

	prompt := buildCodingAgentSystemPrompt(defaults)
	if strings.TrimSpace(prompt) == "" {
		return false, nil
	}

	existing := strings.TrimSpace(
		mappingStringValue(agentNode, systemPromptKey),
	)
	if strings.Contains(existing, runtimeCodingPromptHeader) {
		return false, nil
	}
	if existing == "" {
		setMappingString(agentNode, systemPromptKey, prompt)
		return true, nil
	}
	setMappingString(
		agentNode,
		systemPromptKey,
		existing+"\n\n"+prompt,
	)
	return true, nil
}

func buildCodingAgentSystemPrompt(
	defaults codingAgentDefaults,
) string {
	lines := []string{
		runtimeCodingPromptHeader,
		runtimeLanguageProtocolHeader,
		buildPromptLine(runtimeLanguageFollowUserRule),
		buildPromptLine(runtimeLanguagePreserveTermsRule),
		buildPromptLine(runtimeLanguageNoSentenceMixRule),
		runtimeProgressProtocolHeader,
		buildPromptLine(runtimePreambleVisibilityRule),
		buildPromptLine(runtimePreambleBeforeToolRule),
		buildPromptLine(runtimePreambleImmediateRule),
		buildPromptLine(runtimePreambleNoConfirmRule),
		buildPromptLine(runtimePreambleRequiresActionRule),
		buildPromptLine(runtimePreambleGroupingRule),
		buildPromptLine(runtimePreambleTrivialRule),
		buildPromptLine(runtimeProgressMilestoneRule),
		buildPromptLine(runtimeProgressLongRunningRule),
		buildPromptLine(runtimeProgressQuietPollRule),
		buildPromptLine(runtimeProgressWaitingRule),
		buildPromptLine(runtimeProgressContentRule),
		buildPromptLine(runtimeProgressExamplesRule),
		buildPromptLine(runtimeProgressPersonaRule),
		runtimeWorkflowProtocolHeader,
		"For code, repository, build, test, refactor, or " +
			"review tasks, operate like a local coding " +
			"agent instead of a generic chatbot.",
		"Inspect the target workspace before editing or " +
			"answering code-grounded questions: check the " +
			"directory, git status, relevant files, and " +
			"any AGENTS.md instructions that apply.",
		runtimeFreshInspectionRule,
		runtimeSearchPriorityRule,
		runtimeSearchScopeRule,
		runtimeReadNarrowRule,
		runtimeAutonomyRule,
		runtimeGoalCompletionRule,
		runtimeSkillFirstCapabilityRule,
		runtimeSkillPlatformBoundaryRule,
		runtimePrivateConfigRule,
		runtimeSkillFollowThroughRule,
		runtimeSelfRecoveryRule,
		runtimeExternalLookupRule,
		runtimeMinimalQuestionRule,
		runtimeNoChoiceTailRule,
		runtimeWorkspaceSeparationRule,
		runtimeCrossRootInspectionRule,
		"Use direct tools for quick reads or tiny edits. " +
			"For multi-file, build, review, or long-running " +
			"repo work, keep using repo-aware runtime " +
			"execution tools directly.",
		"The built-in fs_* tools are scoped to their " +
			"configured base_dir and are not a general " +
			"repo browser. For arbitrary repos or coding " +
			"workspaces, prefer exec_command with an " +
			"explicit workdir or another repo-aware " +
			"runtime tool.",
		"For generated documents or other large literal " +
			"artifacts, prefer a file-writing tool or " +
			"redirected stdin over giant shell arguments. " +
			"Use shell commands mainly for conversion, " +
			"inspection, or verification.",
		runtimeArtifactVerificationRule,
		runtimeTruthProtocolHeader,
		"Never claim code changes, tests, builds, or " +
			"commands succeeded unless tool output proved it.",
		"Before using document, media, or build tooling, " +
			"probe local capabilities first: check whether " +
			"the command, Python module, or codec is " +
			"actually available instead of assuming it is.",
		runtimeEnvProbeRule,
		"If a trusted host-mode task is blocked only by a " +
			"missing user-space dependency, install the " +
			"minimum dependency needed, verify it, and then " +
			"continue. Prefer user-space installs over " +
			"system-wide package manager changes.",
		runtimeDepsBootstrapRule,
		runtimeManagedInstallRule,
		runtimeManagedCJKAssetsRule,
		runtimeCJKVerificationRule,
		runtimeSelfContainedDocRule,
		"For ffmpeg or codec work, inspect available " +
			"encoders and formats before choosing flags.",
	}

	facts := collectCodingWorkspaceFacts(defaults.DefaultWorkdir)
	if facts.Workdir != "" {
		lines = append(
			lines,
			buildCodingWorkdirPromptLine(facts),
		)
	}
	if facts.GitRoot != "" && facts.GitRoot != facts.Workdir {
		lines = append(
			lines,
			"Git repository root for the default coding "+
				"workspace: "+facts.GitRoot,
		)
	}
	if facts.AgentsPath != "" {
		lines = append(
			lines,
			"Effective AGENTS.md for the default coding "+
				"workspace: "+facts.AgentsPath,
		)
	}
	if strings.TrimSpace(defaults.ScratchRoot) != "" {
		lines = append(
			lines,
			"Scratch repo root for standalone toy projects "+
				"or no-repo tasks: "+defaults.ScratchRoot,
		)
	}
	if artifactLine := buildOutputRootPromptLine(defaults); artifactLine != "" {
		lines = append(lines, artifactLine)
	}
	if tempLine := buildTempRootPromptLine(defaults); tempLine != "" {
		lines = append(lines, tempLine)
	}
	if helperLine := runtimeDocHelperPromptLine(); helperLine != "" {
		lines = append(lines, helperLine)
	}
	if helperLine := runtimeBrowserRuntimePromptLine(); helperLine != "" {
		lines = append(lines, helperLine)
	}
	return strings.Join(lines, "\n")
}

func buildPromptLine(text string) string {
	return "- " + text
}

func buildCodingWorkdirPromptLine(
	facts codingWorkspaceFacts,
) string {
	return "Default coding workdir: " + facts.Workdir +
		". Treat current repo, current workspace, or this " +
		"repo as this directory unless the user explicitly " +
		"names another repo. If a relative path is missing " +
		"there, inspect the repo root before assuming a " +
		"different codebase."
}

func collectCodingWorkspaceFacts(
	workdir string,
) codingWorkspaceFacts {
	workdir = strings.TrimSpace(workdir)
	if workdir == "" {
		return codingWorkspaceFacts{}
	}
	facts := codingWorkspaceFacts{Workdir: workdir}
	facts.GitRoot = findGitRoot(workdir)
	stopDir := facts.GitRoot
	if stopDir == "" {
		stopDir = workdir
	}
	facts.AgentsPath = findNearestAncestorFile(
		workdir,
		stopDir,
		agentsDocFileName,
	)
	if facts.AgentsPath == "" {
		facts.AgentsPath = workspacecfg.ExistingUserAgentsPath()
	}
	return facts
}

func findGitRoot(path string) string {
	current := filepath.Clean(strings.TrimSpace(path))
	for current != "" {
		if _, err := os.Stat(filepath.Join(current, gitDirName)); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return ""
}

func findNearestAncestorFile(
	start string,
	stop string,
	name string,
) string {
	current := filepath.Clean(strings.TrimSpace(start))
	stop = filepath.Clean(strings.TrimSpace(stop))
	for current != "" {
		candidate := filepath.Join(current, name)
		if fileExists(candidate) {
			return candidate
		}
		if stop != "" && current == stop {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return ""
}

func buildSkillsToolingGuidance(
	_ codingAgentDefaults,
) string {
	return buildSkillsToolingGuidanceWithRoots(codingAgentDefaults{}, nil)
}

func buildSkillsToolingGuidanceWithRoots(
	_ codingAgentDefaults,
	_ []string,
) string {
	parts := []string{
		"Treat the skill overview below as the skills " +
			"available " +
			"in this session. Each entry includes a path to " +
			"that skill's `SKILL.md` on disk. This is a " +
			"blocking requirement for matching skills.",
		runtimeLanguageFollowUserRule,
		runtimeLanguagePreserveTermsRule,
		runtimeLanguageNoSentenceMixRule,
		"If the user names a skill, names a slash command, " +
			"or the task clearly matches a listed skill " +
			"description, you must use that skill in the " +
			"same turn. Start with one brief user-visible " +
			"preamble about the immediate next step, then " +
			"call `skill_load` for that skill right away in " +
			"the same turn. That preamble is part of acting " +
			"immediately, not a pause to ask what to do next.",
		"A preamble-only skill response is invalid. If " +
			"you say you will use, read, load, write, " +
			"create, send, or publish through a skill, the " +
			"same assistant message must include the " +
			"`skill_load` or other required tool call. Do " +
			"not stop after announcing the skill-backed " +
			"next step.",
		"That preamble may announce the immediate task, " +
			"but do not use it for substantive guidance, " +
			"capability disclaimers, or explanations about " +
			"which subsystem loads versus runs the skill. " +
			"Do not turn that preamble into a request for " +
			"confirmation or an options menu.",
		"Never mention reading, loading, or using a " +
			"matching skill unless you already called " +
			"`skill_load` for it in this turn. Never say " +
			"that you could read or load a matching skill " +
			"later without actually doing it first. Do not " +
			"answer a matching skill task from the short " +
			"summary, prior knowledge, or partial memory. " +
			"Even if you think you already know the answer, " +
			"load `SKILL.md` first. Load `SKILL.md` before " +
			"giving substantive guidance or acting on the " +
			"workflow.",
		"When `SKILL.md` references relative paths, " +
			"resolve them from the skill directory first. " +
			"Read only the supporting docs, scripts, " +
			"assets, examples, or templates you still need.",
		"Do not respond with capability disclaimers such " +
			"as `I can read the skill` when you can load it " +
			"now. Announce the next step briefly and do it.",
		runtimeSkillFirstCapabilityRule,
		runtimeSkillPlatformBoundaryRule,
		runtimePrivateConfigRule,
		runtimeSkillFollowThroughRule,
		"Reuse bundled scripts, templates, and assets " +
			"when they already fit. If multiple skills " +
			"match, use the smallest set that covers the " +
			"task. Keep context small and avoid bulk-" +
			"loading docs.",
		"Do not invent commands, flags, auth steps, file " +
			"layouts, or workflows from a short summary or " +
			"partial memory. Keep exploring nearby runtime " +
			"facts, retries, and recovery paths yourself " +
			"before asking for more input.",
		"If local exploration reveals an obvious next " +
			"recovery step such as a canonical identifier, " +
			"corrected parameter, alternate lookup, nearby " +
			"supporting doc, or retry path, take it in this " +
			"turn instead of stopping to explain the " +
			"recovery plan.",
		"When tool output or nearby exploration already " +
			"gives you one reasonable canonical identifier, " +
			"corrected parameter, or target resource, treat " +
			"it as the working value and continue in this " +
			"turn without asking the user to confirm it first.",
		"If a matching skill is missing, unreadable, or " +
			"still lacks a required external input after " +
			"reasonable local exploration and no feasible " +
			"next recovery step remains, state the issue " +
			"briefly as a factual status line and take the " +
			"best direct fallback still in scope. Do not " +
			"stop to ask the user to confirm the fallback " +
			"unless applying it would be risky or " +
			"irreversible.",
	}
	return strings.Join(parts, " ")
}

func buildCodingAgentWorkdirGuidance(
	defaults codingAgentDefaults,
) string {
	parts := make([]string, 0, 3)
	if strings.TrimSpace(defaults.DefaultWorkdir) != "" {
		parts = append(
			parts,
			"Default coding workdir: "+defaults.DefaultWorkdir+
				". Resolve phrases like current repo, "+
				"current workspace, or this repo to this "+
				"path unless the user explicitly picks "+
				"another repo or directory.",
		)
	}
	if strings.TrimSpace(defaults.ScratchRoot) != "" {
		parts = append(
			parts,
			"If the user wants a scratch project, create a "+
				"fresh git repo under "+defaults.ScratchRoot+".",
		)
	}
	return strings.Join(parts, " ")
}

func buildOutputRootPromptLine(
	defaults codingAgentDefaults,
) string {
	if strings.TrimSpace(defaults.OutputRoot) == "" {
		return ""
	}
	return "Runtime artifact output root: " + defaults.OutputRoot +
		". This is the default home for direct user-facing " +
		"generated files, exported docs, converted media, OCR " +
		"text, screenshots, and other non-source deliverables " +
		"when the user did not specify a repo path."
}

func buildTempRootPromptLine(
	defaults codingAgentDefaults,
) string {
	if strings.TrimSpace(defaults.TempRoot) == "" {
		return ""
	}
	return "Runtime temp root: " + defaults.TempRoot +
		". Keep disposable intermediates, downloads, working " +
		"copies of uploads, unpacked archives, and caches here. " +
		"TMPDIR, TMP, and TEMP already point to this directory."
}

func buildCodingAgentArtifactGuidance(
	defaults codingAgentDefaults,
) string {
	parts := make([]string, 0, 2)
	if strings.TrimSpace(defaults.OutputRoot) != "" {
		parts = append(
			parts,
			"Keep source edits in the repo or selected "+
				"workspace. Unless the user explicitly "+
				"wants repo writes, treat uploads and "+
				"user-facing generated artifacts as "+
				"non-repo by default and put direct "+
				"uploads, "+
				"generated exports, converted docs, "+
				"screenshots, OCR text, and other "+
				"non-source deliverables under "+
				defaults.OutputRoot+".",
		)
	}
	if strings.TrimSpace(defaults.TempRoot) != "" {
		parts = append(
			parts,
			"Use $"+runtimeTmpDirEnvName+
				" ("+defaults.TempRoot+") for disposable "+
				"intermediates, downloads, working copies "+
				"of uploads, unpacked archives, and caches "+
				"instead of cluttering the repo.",
		)
	}
	return strings.Join(parts, " ")
}

func buildCodingAgentExecutionModeGuidance(mode string) string {
	switch mode {
	case codingAgentModeSandbox:
		return "Current coding_agent.execution_mode is sandbox: " +
			"stay sandboxed, keep elevated:false, prefer " +
			"`codex exec --full-auto`, and report sandbox " +
			"limits clearly instead of pretending the task " +
			"finished."
	case codingAgentModeAuto:
		return "Current coding_agent.execution_mode is auto: " +
			"start sandboxed and prefer `codex exec " +
			"--full-auto`. If output shows sandbox write " +
			"limits such as `Sandbox(LandlockRestrict)` or " +
			"`permission denied`, explain it briefly and retry " +
			"once with elevated:true plus `codex exec " +
			"--dangerously-bypass-approvals-and-sandbox` " +
			"when the task requires real host writes or " +
			"self-checks."
	default:
		return "Current coding_agent.execution_mode is host: " +
			"for trusted coding tasks, prefer elevated:true " +
			"and `codex exec " +
			"--dangerously-bypass-approvals-and-sandbox` so " +
			"the agent can really edit files and run " +
			"self-checks on the host. Keep workdir narrow and " +
			"do not touch unrelated paths."
	}
}

func documentNode(root *yaml.Node) *yaml.Node {
	if root == nil {
		return nil
	}
	if root.Kind == yaml.DocumentNode {
		if len(root.Content) == 0 {
			return nil
		}
		return root.Content[0]
	}
	return root
}

func ensureMappingValue(
	root *yaml.Node,
	key string,
) *yaml.Node {
	value := mappingValue(root, key)
	if value != nil {
		return value
	}
	if root.Kind == 0 {
		root.Kind = yaml.MappingNode
	}
	if root.Kind != yaml.MappingNode {
		return nil
	}
	keyNode := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!str",
		Value: key,
	}
	valueNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	root.Content = append(root.Content, keyNode, valueNode)
	return valueNode
}

func mappingValue(root *yaml.Node, key string) *yaml.Node {
	if root == nil || root.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			return root.Content[i+1]
		}
	}
	return nil
}

func firstMappingValue(
	root *yaml.Node,
	keys ...string,
) *yaml.Node {
	for _, key := range keys {
		if value := mappingValue(root, key); value != nil {
			return value
		}
	}
	return nil
}

func mappingStringValue(root *yaml.Node, key string) string {
	node := mappingValue(root, key)
	if node == nil {
		return ""
	}
	return strings.TrimSpace(node.Value)
}

func setMappingString(
	root *yaml.Node,
	key string,
	value string,
) {
	setMappingScalar(root, key, "!!str", value)
}

func setMappingInt(
	root *yaml.Node,
	key string,
	value int,
) {
	setMappingScalar(root, key, "!!int", strconv.Itoa(value))
}

func setMappingBool(
	root *yaml.Node,
	key string,
	value bool,
) {
	setMappingScalar(root, key, "!!bool", strconv.FormatBool(value))
}

func setMappingScalar(
	root *yaml.Node,
	key string,
	tag string,
	value string,
) {
	if root == nil || root.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != key {
			continue
		}
		root.Content[i+1] = &yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   tag,
			Value: value,
		}
		return
	}
	root.Content = append(
		root.Content,
		&yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!str",
			Value: key,
		},
		&yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   tag,
			Value: value,
		},
	)
}

func setMappingNode(
	root *yaml.Node,
	key string,
	value *yaml.Node,
) {
	if root == nil || root.Kind != yaml.MappingNode || value == nil {
		return
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != key {
			continue
		}
		root.Content[i+1] = value
		return
	}
	root.Content = append(
		root.Content,
		&yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!str",
			Value: key,
		},
		value,
	)
}

func copyMappingValue(
	dst *yaml.Node,
	src *yaml.Node,
	key string,
) {
	value := mappingValue(src, key)
	if value == nil {
		return
	}
	setMappingNode(dst, key, value)
}

func setMappingSequence(
	root *yaml.Node,
	key string,
	values []string,
) {
	if root == nil || root.Kind != yaml.MappingNode {
		return
	}

	sequence := &yaml.Node{
		Kind: yaml.SequenceNode,
		Tag:  "!!seq",
	}
	for _, value := range values {
		sequence.Content = append(
			sequence.Content,
			&yaml.Node{
				Kind:  yaml.ScalarNode,
				Tag:   "!!str",
				Value: value,
			},
		)
	}

	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != key {
			continue
		}
		root.Content[i+1] = sequence
		return
	}
	root.Content = append(
		root.Content,
		&yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!str",
			Value: key,
		},
		sequence,
	)
}

func newToolProviderNode(typeName string) *yaml.Node {
	node := &yaml.Node{
		Kind: yaml.MappingNode,
		Tag:  "!!map",
	}
	setMappingString(node, toolTypeKey, typeName)
	return node
}

func newBrowserToolProviderNode() *yaml.Node {
	node := newToolProviderNode(browserToolProviderTypeName)
	setMappingString(node, toolNameKey, browserToolProviderName)
	setMappingNode(node, toolConfigKey, newBrowserToolConfigNode())
	return node
}

func newBrowserToolConfigNode() *yaml.Node {
	node := &yaml.Node{
		Kind: yaml.MappingNode,
		Tag:  "!!map",
	}
	setMappingString(
		node,
		toolDefaultProfileKey,
		browserToolDefaultProfileName,
	)
	profilesNode := &yaml.Node{
		Kind: yaml.SequenceNode,
		Tag:  "!!seq",
	}
	profilesNode.Content = append(
		profilesNode.Content,
		newBrowserToolProfileNode(),
	)
	setMappingNode(node, toolProfilesKey, profilesNode)
	return node
}

func newBrowserToolProfileNode() *yaml.Node {
	node := &yaml.Node{
		Kind: yaml.MappingNode,
		Tag:  "!!map",
	}
	setMappingString(node, toolNameKey, browserToolDefaultProfileName)
	setMappingString(
		node,
		toolTransportKey,
		browserToolTransportSTDIO,
	)
	setMappingString(
		node,
		toolCommandKey,
		runtimeBrowserRuntimeName,
	)
	setMappingString(
		node,
		toolTimeoutKey,
		browserToolDefaultTimeout,
	)
	argsNode := &yaml.Node{
		Kind: yaml.SequenceNode,
		Tag:  "!!seq",
	}
	argsNode.Content = append(
		argsNode.Content,
		newStringNode(runtimeMCPStdIOArg()),
	)
	setMappingNode(node, toolArgsKey, argsNode)
	return node
}

func runtimeMCPStdIOArg() string {
	return "mcp-stdio"
}

func newStringNode(value string) *yaml.Node {
	return &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!str",
		Value: value,
	}
}

func hasToolProviderType(
	root *yaml.Node,
	typeName string,
) bool {
	want := strings.TrimSpace(typeName)
	if root == nil || root.Kind != yaml.SequenceNode || want == "" {
		return false
	}
	for _, child := range root.Content {
		if child == nil || child.Kind != yaml.MappingNode {
			continue
		}
		if strings.TrimSpace(
			mappingStringValue(child, toolTypeKey),
		) == want {
			return true
		}
	}
	return false
}

func sequenceContainsPath(
	root *yaml.Node,
	want string,
) bool {
	normalizedWant, ok := normalizedPathValue(want)
	if !ok {
		return false
	}
	for _, child := range root.Content {
		if child == nil || child.Kind != yaml.ScalarNode {
			continue
		}
		normalizedChild, childOK := normalizedPathValue(
			child.Value,
		)
		if childOK && normalizedChild == normalizedWant {
			return true
		}
	}
	return false
}

func normalizedPathValue(raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", false
	}
	value = os.ExpandEnv(value)
	expanded, _, err := expandUserPath(value)
	if err != nil {
		return value, false
	}
	return expanded, true
}

func defaultCodexSkillsDir() string {
	base := strings.TrimSpace(os.Getenv(codexHomeEnvName))
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, codexDirName)
	}
	if strings.TrimSpace(base) == "" {
		return ""
	}
	return filepath.Join(base, skillsDirName)
}

func deleteMappingKey(root *yaml.Node, key string) {
	if root == nil || root.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != key {
			continue
		}
		root.Content = append(
			root.Content[:i],
			root.Content[i+2:]...,
		)
		return
	}
}

func deleteMappingKeys(root *yaml.Node, keys ...string) {
	for _, key := range keys {
		deleteMappingKey(root, key)
	}
}

func logStartupPaths(paths startupPaths) {
	tlog.Infof(
		"Config:   tRPC = %s",
		displayStartupPath(paths.TRPCConfigPath),
	)
	tlog.Infof(
		"Config:   OpenClaw = %s",
		displayStartupPath(paths.OpenClawConfigPath),
	)
	tlog.Infof(
		"Config:   state_dir = %s",
		displayStartupPath(effectiveStartupStateDir(paths)),
	)
}

func effectiveStartupStateDir(paths startupPaths) string {
	if value := strings.TrimSpace(
		os.Getenv(runtimeStateDirEnvName),
	); value != "" {
		return value
	}
	return strings.TrimSpace(paths.StateDir)
}

func displayStartupPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "<none>"
	}
	return path
}

func matchFlagValue(arg string, name string) (string, bool) {
	short := "-" + name
	long := "--" + name

	if arg == short || arg == long {
		return "", true
	}

	shortEq := short + "="
	longEq := long + "="
	if strings.HasPrefix(arg, shortEq) {
		return strings.TrimPrefix(arg, shortEq), true
	}
	if strings.HasPrefix(arg, longEq) {
		return strings.TrimPrefix(arg, longEq), true
	}
	return "", false
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}

	type exitCoder interface {
		ExitCode() int
	}

	coder, ok := err.(exitCoder)
	if !ok {
		return 1
	}
	code := coder.ExitCode()
	if code == 0 {
		return 1
	}
	return code
}
