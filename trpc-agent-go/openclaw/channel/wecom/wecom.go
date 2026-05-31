// Package wecom 提供企业微信（WeCom）Channel 插件，用于 OpenClaw 框架。
//
// 实现了 openclaw Channel 接口，通过 webhook 回调接收企业微信消息，
// 并通过 WeChat Webhook API 发送回复。
package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"git.woa.com/trpc-go/trpc-agent-go/openclaw/assistantname"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/ingress"
	personaapi "git.woa.com/trpc-go/trpc-agent-go/openclaw/persona"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/progress"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/promptasset"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/runtimectl"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/runtimehint"
	"trpc.group/trpc-go/trpc-agent-go/log"

	publicsubagent "git.woa.com/trpc-go/trpc-agent-go/openclaw/subagent"
	occhannel "trpc.group/trpc-go/trpc-agent-go/openclaw/channel"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/conversation"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/delivery"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/gwclient"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/gwproto"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/registry"
)

const (
	pluginType = "wecom"
	// PluginType is the registered plugin type name.
	PluginType = pluginType

	// RuntimeDefaultWorkdirConfigKey is an internal config key injected
	// by cmd/openclaw after resolving the runtime workspace defaults.
	RuntimeDefaultWorkdirConfigKey = "runtime_default_workdir"
	// RuntimeScratchRootConfigKey is an internal config key injected by
	// cmd/openclaw after resolving the runtime scratch root.
	RuntimeScratchRootConfigKey = "runtime_scratch_root"
	// RuntimeModelNameConfigKey is an internal config key injected by
	// cmd/openclaw after resolving the runtime model name.
	RuntimeModelNameConfigKey = "runtime_model_name"
	// RuntimeReplyDeliveryRootsConfigKey is an internal config key
	// injected by cmd/openclaw after resolving extra writable roots
	// that are safe for same-chat file return delivery.
	RuntimeReplyDeliveryRootsConfigKey = "runtime_reply_delivery_roots"
	// RequestSystemPromptFilesConfigKey configures extra request-level
	// system prompt files for WeCom runs.
	RequestSystemPromptFilesConfigKey = "request_system_prompt_files"
	// RequestSystemPromptDirConfigKey configures a directory of
	// request-level system prompt fragments for WeCom runs.
	RequestSystemPromptDirConfigKey = "request_system_prompt_dir"

	errNilGateway = "wecom channel: nil gateway client"

	requestIDPrefix = "wecom:"

	wecomThreadSessionPrefix = "wecom:thread:"

	// maxReplyRunes 是每条消息分片的最大字符数。
	// 企微 Webhook markdown 限制约 4096 字节，使用 2000 字符作为安全上限。
	maxReplyRunes = 2000

	continuedReplyPrefix = "(接上条消息)\n"

	defaultCallbackPath = "/wecom/callback"
	defaultCallbackPort = 8080

	connectionModeWebhook   = "webhook"
	connectionModeWebSocket = "websocket"
	defaultConnectionMode   = connectionModeWebhook

	// 机器人模式。
	botModeNotification = "notification" // 消息通知机器人
	botModeAI           = "ai"           // 智能机器人

	// 权限策略。
	chatPolicyDisabled  = "disabled"
	chatPolicyOpen      = "open"
	chatPolicyAllowlist = "allowlist"

	defaultChatPolicy = chatPolicyOpen

	// 文件/图片 URL 文本格式标记（embed_*_url 启用时使用）。
	fileURLPrefix  = "[file_url:"
	fileURLSuffix  = "]"
	imageURLPrefix = "[image_url:"
	imageURLSuffix = "]"

	// Default session timeout: disabled.
	defaultSessionTimeout = time.Duration(0)
	gatewayCancelTimeout  = 5 * time.Second

	defaultEnterChatWelcomeEnabled = true

	eventTypeEnterChat    = "enter_chat"
	eventTypeFeedback     = "feedback_event"
	eventTypeTemplateCard = "template_card_event"

	browserDoctorCommandName    = "doctor"
	browserDoctorPromptTimeout  = 3 * time.Second
	browserDoctorPromptCacheTTL = 5 * time.Second
	browserDoctorFailurePrefix  = "Current turn browser runtime " +
		"fact: doctor probe failed: "
	browserDoctorFailureRule = "Treat this fresh probe result " +
		"as newer than older browser or MCP failures in " +
		"session history."

	// Default messages (used when not overridden via Option).
	defaultProcessingMessage   = "正在思考中..."
	defaultCancelNoopMessage   = "当前没有正在执行的请求。"
	defaultCancelFailedMessage = "取消失败。"
	defaultCancelOKMessage     = "已取消。"
	defaultNotAllowedMessage   = "您没有权限使用此机器人。"
	defaultNewSessionMessage   = "✅ 已开始新会话，之前的上下文已清空。" +
		"如需切回 /new 前的上一会话，可发送 " +
		recallKeyword + "。"
	defaultRecallMessage               = "✅ 已切回上一会话。"
	defaultRecallNoopMessage           = "当前没有可切回的上一会话。"
	defaultImageAnalyzeText            = "请查看这张图片，并简要说明图片内容。"
	defaultFileAnalyzeText             = "请查看这个文件，并简要说明内容。"
	defaultMediaAnalyzeText            = "请查看附件，并简要说明内容。"
	defaultGatewayErrorMessage         = "处理消息失败，请稍后重试。"
	defaultAttachmentReadFailedMessage = "无法读取当前附件，请稍后重试。"
	runnerExecutionErrorMessageEN      = "An error occurred during " +
		"execution. Please contact the service provider."
	defaultQueuedMessage           = "上一条还在处理中，已加入队列..."
	defaultImageProcessingText     = "已收到图片，正在查看内容..."
	defaultAttachmentReadText      = "已收到附件，正在读取内容..."
	defaultCompletedStatusSummary  = "已生成最终回复。"
	defaultIgnoredStatusSummary    = "当前请求已忽略。"
	defaultEmptyReplyStatusSummary = "当前请求已完成，" +
		"没有额外回复内容。"
	requestPromptVarRuntimePersonaNote = "" +
		"TRPC_CLAW_WECOM_RUNTIME_PERSONA_NOTE"
	requestPromptVarIdentityNote = "" +
		"TRPC_CLAW_WECOM_IDENTITY_NOTE"
	requestPromptVarAssistantAliasNote = "" +
		"TRPC_CLAW_WECOM_ASSISTANT_ALIAS_NOTE"
	requestPromptVarWorkspaceNote = "" +
		"TRPC_CLAW_WECOM_WORKSPACE_NOTE"
	requestPromptVarCurrentTimeNote = "" +
		"TRPC_CLAW_WECOM_CURRENT_TIME_NOTE"
	requestPromptVarCronAuthoringNote = "" +
		"TRPC_CLAW_WECOM_CRON_AUTHORING_NOTE"
	requestPromptVarAIBotNotes   = "TRPC_CLAW_WECOM_AIBOT_NOTES"
	requestPromptVarReplyUXNotes = "" +
		"TRPC_CLAW_WECOM_REPLY_UX_NOTES"
	requestPromptVarBrowserEnvNote = "" +
		"TRPC_CLAW_WECOM_BROWSER_ENV_NOTE"
	requestPromptVarBrowserDoctorNote = "" +
		"TRPC_CLAW_WECOM_BROWSER_DOCTOR_NOTE"
	requestPromptVarExternalLookupNote = "" +
		"TRPC_CLAW_WECOM_EXTERNAL_LOOKUP_NOTE"
	requestPromptVarCronDeliveryNote = "" +
		"TRPC_CLAW_WECOM_CRON_DELIVERY_NOTE"
	requestPromptVarTurnContextNotes = "" +
		"TRPC_CLAW_WECOM_TURN_CONTEXT_NOTES"
	requestPromptVarRuntimeRules = "" +
		"TRPC_CLAW_WECOM_RUNTIME_RULES"
	requestPromptVarChannelNotes = "" +
		"TRPC_CLAW_WECOM_CHANNEL_NOTES"
	requestPromptVarBrowserNotes = "" +
		"TRPC_CLAW_WECOM_BROWSER_NOTES"
	requestPromptStructureTurnContext    = "[Turn context notes]"
	requestPromptStructureRuntimeRules   = "[Runtime rules]"
	requestPromptStructureChannelNotes   = "[Channel notes]"
	requestPromptStructureBrowserNotes   = "[Browser notes]"
	requestPromptStructureExternalLookup = "" +
		"[External lookup note]"
	requestPromptStructurePersona        = "[Persona override note]"
	requestPromptStructureIdentity       = "[Identity note]"
	requestPromptStructureAssistantAlias = "[Assistant alias note]"
	requestPromptStructureWorkspace      = "[Workspace note]"
	requestPromptStructureCurrentTime    = "[Current time note]"
	requestPromptStructureCronAuthoring  = "[Cron authoring rule]"
	requestPromptStructureAIBot          = "[AI bot notes]"
	requestPromptStructureReplyUX        = "[Reply UX notes]"
	requestPromptStructureBrowserEnv     = "[Browser env note]"
	requestPromptStructureBrowserDoctor  = "[Browser doctor note]"
	requestPromptStructureCronDelivery   = "[Cron delivery note]"
	runtimeAssistantAliasNoteTemplate    = "For this chat, your current " +
		"name is %s. This chat-specific name overrides " +
		"the global assistant name for this chat. When " +
		"the user asks who you are or what " +
		"your name is, use this chat-specific name."
	runtimeAssistantNameToolPromptRule = "[Name changes: if the user " +
		"explicitly asks you to set, remember, change, or " +
		"clear your name, or directly assigns you a new " +
		"name in plain language, use the set_assistant_name " +
		"tool. Use scope=session for the current chat. " +
		"Use scope=global only when the user clearly " +
		"wants the default name for future sessions.]"
	currentTimeOffsetLayout = "-07:00"
	currentTimeNotePrefix   = "[Current turn time: "
	currentTimeSourceRule   = " Use this timestamp as the source " +
		"of truth for now, today, tomorrow, and relative " +
		"times in this turn."
	currentTimeScheduleRule = " When creating schedules from " +
		"relative user time, use this current turn time as " +
		"the anchor. For RFC3339 at timestamps, keep this " +
		"numeric UTC offset unless the user specifies " +
		"another offset.]"
	runtimeCronAuthoringModeRule = "[Cron authoring: when " +
		"using the cron tool, distinguish between a " +
		"future agent task and a literal text delivery. " +
		"If the user wants a scheduled run to send a " +
		"fixed phrase, reminder text, status line, or " +
		"other exact wording, set the cron message to " +
		"an explicit instruction that replies with " +
		"exactly that text, rather than storing the " +
		"bare text token alone. Use a future agent task " +
		"only when the job should think, inspect, or " +
		"generate fresh content at run time."
	runtimeCronAuthoringFidelityRule = " For both cases, " +
		"keep the scheduled message or task instruction " +
		"faithful to the user's original request. " +
		"Preserve the stated scope, recipients, time " +
		"windows, and checklist items."
	runtimeCronAuthoringSummaryRule = " If the user gives " +
		"a long or multi-part reminder, keep those " +
		"requirements in the cron message instead of " +
		"collapsing them into a shorter todo summary " +
		"unless the user explicitly asks for a summary.]"
	runtimeCronAuthoringNote = runtimeCronAuthoringModeRule +
		runtimeCronAuthoringFidelityRule +
		runtimeCronAuthoringSummaryRule
	runtimeCronDeliveryNotePrefix = "[WeCom cron delivery: " +
		"current group chat target is %s."
	runtimeCronDeliveryIdentityRule = " Use the resolved " +
		"participant-name table to map named participants " +
		"to the right chat members, and keep using the " +
		"mapped canonical labels in confirmations and " +
		"scheduled message content."
	runtimeCronDeliveryProtocolRule = " WeCom AI websocket " +
		"proactive pushes do not support guaranteed real " +
		"@ mentions. When a scheduled reminder should " +
		"call out chat participants, use the mapped " +
		"canonical label exactly in the message body. " +
		"Do not copy alternate or localized names from " +
		"user text or history, and avoid <@userid> " +
		"tokens.]"
	runtimeSpeakerScopedMemoryNotePrefix = "[WeCom shared-chat " +
		"speaker memory: current speaker for this turn is %s."
	runtimeSpeakerScopedMemoryRule = " If a durable reply " +
		"style, tone, or formatting preference should apply " +
		"only when this same person @mentions you in this " +
		"group, treat it as speaker-scoped rather than a " +
		"group-wide rule."
	runtimeSpeakerScopedMemoryWriteRule = " If you write such " +
		"a preference into durable memory, keep it as a " +
		"short bullet tagged with this speaker's user ID or " +
		"canonical label. Prefer the explicit `speaker:` tag " +
		"when a user ID is available, for example `%s`."
	runtimeSpeakerScopedMemoryReadRule = " When answering in " +
		"this shared group, apply speaker-scoped bullets only " +
		"when they match the current speaker, and ignore " +
		"speaker-scoped bullets written for other " +
		"participants.]"
	runtimePersonaOverridePromptHeader = "Runtime chat " +
		"persona override: "
	runtimePersonaPrimaryStyleRule = "This persona is " +
		"the first and highest-priority style " +
		"instruction for this chat. Other runtime " +
		"notes only constrain channel protocol, tool " +
		"use, and factual accuracy."
	runtimePersonaHistoryOverrideRule = "It overrides " +
		"the tone, self-presentation, and mannerisms " +
		"shown in earlier assistant messages in this " +
		"session. Use session history only for facts, " +
		"user intent, and artifacts."
	runtimePersonaConsistencyRule = "Apply it " +
		"consistently to greetings, short replies, " +
		"progress updates, final answers, and any " +
		"questionnaire or interview turns the user " +
		"explicitly requests unless the user changes " +
		"persona."
	runtimeRecentUploadsJSONEnvName = "OPENCLAW_RECENT_UPLOADS_JSON"
	runtimeSessionUploadsDirEnvName = "OPENCLAW_SESSION_UPLOADS_DIR"
	runtimeLastUploadPathEnvName    = "OPENCLAW_LAST_UPLOAD_PATH"
	runtimeLastUploadPathEnvPattern = "OPENCLAW_LAST_*_PATH"
	runtimePromptNoteSeparator      = "\n\n"
	replyWritableRootsDescription   = "runtime artifact " +
		"output root or another approved writable " +
		"non-repo root by default; use the active " +
		"coding workspace only when the task " +
		"explicitly targets repo files"
	replyArtifactWriteStrategyNote = "For generated " +
		"documents with substantial literal content, " +
		"prefer a file-writing tool in one of those " +
		"approved roots and use shell commands mainly " +
		"for conversion, inspection, or post-processing."
	replyArtifactVerificationNote = "A reply-file marker " +
		"is a promise that the file already exists on " +
		"disk. Create the file first and verify the " +
		"exact path with `stat`, `ls`, or `test -f` " +
		"before adding the marker. Never invent " +
		"placeholder filenames or marker paths."
	replyDepsBootstrapNote = "If the task matches " +
		"existing dependency profiles or skill " +
		"metadata, prefer `trpc-claw inspect deps` " +
		"and `trpc-claw bootstrap deps --apply` " +
		"before scattered ad hoc installs."
	replyCJKVerificationNote = "For Chinese or other " +
		"CJK documents, use an explicit CJK-capable " +
		"font and verify the rendered or extracted " +
		"artifact is not garbled before sending it " +
		"back."
	replyDocHelperNote = "When the runtime document " +
		"helper is available, use `trpc-claw-doc-helper " +
		"probe` to inspect capabilities, " +
		"`trpc-claw-doc-helper ensure-fonts` to install " +
		"managed CJK fonts, `trpc-claw-doc-helper " +
		"ensure-tessdata chi_sim eng` to install OCR " +
		"language data, and " +
		"`trpc-claw-doc-helper verify-pdf --path <file> " +
		"--expect-cjk` before sending Chinese or other " +
		"CJK PDFs."
	currentTurnAttachmentNoteTemplate = "[Current turn attachments: " +
		"%d. Use only attachments from this current user turn or " +
		"quoted message as direct inputs unless the user " +
		"explicitly asks for an earlier or generated file. " +
		"Do not infer missing files from earlier session uploads. " +
		"Treat current-turn uploads as non-repo inputs by " +
		"default and inspect them in place first. " +
		"These uploads already live outside the coding workspace " +
		"in runtime-managed upload storage. When shell tools need " +
		"concrete upload paths, inspect `" +
		runtimeRecentUploadsJSONEnvName + "`, `" +
		runtimeSessionUploadsDirEnvName + "`, `" +
		runtimeLastUploadPathEnvName + "`, or type-specific `" +
		runtimeLastUploadPathEnvPattern +
		"` when available. Do not copy uploads into the repo " +
		"unless the task explicitly targets repo files.]"
	wecomAIBotWebhookTransportNote = "[Channel transport: " +
		"this Enterprise WeCom AI bot replies in the " +
		"current chat through a one-shot response_url " +
		"and can send markdown only. Do not use the " +
		"generic message tool or local file/media send " +
		"for same-chat replies.]"
	wecomAIBotWebSocketTransportNote = "[Channel transport: " +
		"this Enterprise WeCom AI bot replies in the " +
		"current chat through the current websocket " +
		"callback, not a generic outbound target. For " +
		"same-chat attachments, do not use the generic " +
		"message tool. Save the file under the " +
		replyWritableRootsDescription + ", then add one " +
		"standalone line exactly as " +
		replyFileMarkerPrefix + "/absolute/path/to/file" +
		replyFileMarkerSuffix + ". " +
		replyArtifactVerificationNote +
		" A standalone MEDIA:/absolute/path/to/file " +
		"line is also accepted.]"
	wecomAIBotWebhookSendBackNote = "[Channel constraint: this " +
		"Enterprise WeCom AI bot can only send markdown text " +
		"back in chat. Do not use message to send local " +
		"files or media back in this chat, and do not create " +
		"a document solely for return delivery. If the user " +
		"asks for a generated document or attachment, provide " +
		"the result inline and explain the channel limitation " +
		"briefly.]"
	wecomAIBotWebSocketSendBackNote = "[Channel capability: " +
		"this Enterprise WeCom AI bot can send eligible " +
		"local files or media back in chat when running in " +
		"websocket mode. If the user asks for a generated " +
		"document or attachment and you create one for " +
		"return delivery, save it under the " +
		replyWritableRootsDescription + ", then add one " +
		"standalone " +
		"line per file to the final reply exactly as " +
		replyFileMarkerPrefix + "/absolute/path/to/file" +
		replyFileMarkerSuffix + ". Keep the user-visible " +
		"reply natural, do not expose local paths outside " +
		"those marker lines, and only mark files that are " +
		"safe and intended to be sent back. A standalone " +
		"MEDIA:/absolute/path/to/file line is also accepted " +
		"for skill outputs, but prefer the explicit WeCom " +
		"marker in normal replies. If a skill first writes " +
		"media into the runtime temp root or another " +
		"transient directory, move the intended return file " +
		"into the runtime artifact output root or another " +
		"approved non-repo root before adding the final " +
		"marker. " + replyArtifactVerificationNote +
		" " +
		replyArtifactWriteStrategyNote + " " +
		"Supported " +
		"return limits are: image png/jpg/jpeg/gif <=2MB, " +
		"voice amr <=2MB, video mp4 <=10MB, ordinary file " +
		"<=20MB. If a generated artifact would exceed a " +
		"limit or uses an incompatible media format, shrink " +
		"or convert it before adding the marker. If the user " +
		"asks for voice output, prefer generating amr " +
		"directly or converting to amr first. If a missing " +
		"user-space dependency blocks a requested document " +
		"or media artifact, first probe whether it already " +
		"is installed; if not, install only the minimum " +
		"needed user-space dependency, verify it, and then " +
		"continue. " + replyDepsBootstrapNote + " " +
		replyCJKVerificationNote + " " +
		replyDocHelperNote + " Do not claim an " +
		"attachment was sent unless you actually add " +
		"marker lines for it.]"
)

var defaultHelpMessage = buildDefaultHelpMessage()

func init() {
	if err := registry.RegisterChannel(pluginType, func(
		deps registry.ChannelDeps,
		spec registry.PluginSpec,
	) (occhannel.Channel, error) {
		return newChannel(deps, spec)
	}); err != nil {
		panic(err)
	}
}

// channelCfg 保存企业微信 Channel 实例的 YAML 配置。
type channelCfg struct {
	// === 机器人模式选择 ===
	// 决定使用哪种机器人类型：
	//   "notification" - 消息通知机器人
	//   "ai"           - 智能机器人
	// 必填。根据企微管理后台中的机器人类型选择。
	BotMode string `yaml:"bot_mode"`

	// === 通用配置 ===
	CorpID         string `yaml:"corp_id,omitempty"`
	AgentID        string `yaml:"agent_id,omitempty"`
	Token          string `yaml:"token,omitempty"`
	EncodingAESKey string `yaml:"encoding_aes_key,omitempty"`
	CallbackPort   int    `yaml:"callback_port,omitempty"`
	CallbackPath   string `yaml:"callback_path,omitempty"`
	BotName        string `yaml:"bot_name,omitempty"`
	ConnectionMode string `yaml:"connection_mode,omitempty"`

	// === 消息通知机器人配置 ===
	// bot_mode 为 notification 时必填
	WebhookURL string `yaml:"webhook_url,omitempty"`

	// === 智能机器人配置 ===
	// bot_mode 为 ai 时必填
	AIBotID                   string         `yaml:"aibotid,omitempty"`
	EnableStream              bool           `yaml:"enable_stream,omitempty"`
	ReplyPrefix               replyPrefixCfg `yaml:"reply_prefix,omitempty"`
	StreamDisplayMode         string         `yaml:"stream_display_mode,omitempty"`
	StreamSnapshotMode        string         `yaml:"stream_snapshot_mode,omitempty"`
	Secret                    string         `yaml:"secret,omitempty"`
	WSURL                     string         `yaml:"ws_url,omitempty"`
	PersonaDir                string         `yaml:"persona_dir,omitempty"`
	RequestSystemPromptFiles  []string       `yaml:"request_system_prompt_files,omitempty"`
	RequestSystemPromptDir    string         `yaml:"request_system_prompt_dir,omitempty"`
	RuntimeDefaultWorkdir     string         `yaml:"runtime_default_workdir,omitempty"`
	RuntimeScratchRoot        string         `yaml:"runtime_scratch_root,omitempty"`
	RuntimeModelName          string         `yaml:"runtime_model_name,omitempty"`
	RuntimeReplyDeliveryRoots []string       `yaml:"runtime_reply_delivery_roots,omitempty"`

	// === 权限控制 ===
	// ChatPolicy 控制谁可以与机器人交互。
	// "open"（默认）- 所有人，"disabled" - 禁用，"allowlist" - 仅白名单用户。
	ChatPolicy         string   `yaml:"chat_policy,omitempty"`
	AllowUsers         []string `yaml:"allow_users,omitempty"`
	RuntimeAdminPolicy string   `yaml:"runtime_admin_policy,omitempty"`
	RuntimeAdminUsers  []string `yaml:"runtime_admin_users,omitempty"`
	UserLabelMode      string   `yaml:"user_label_mode,omitempty"`
	UserLookupCommand  string   `yaml:"user_identity_lookup_command,omitempty"`

	// === Multimodal Configuration ===
	// EmbedImageURL controls how image URLs are passed to the agent:
	//   false (default) - URLs are passed via ContentParts and
	//                     materialized before reaching the Gateway
	//   true            - URLs are embedded in text as [image_url:URL],
	//                     Agent downloads via tool (e.g., download_file)
	EmbedImageURL bool `yaml:"embed_image_url,omitempty"`

	// EmbedFileURL controls how file URLs are passed to the agent:
	//   false (default) - URLs are passed via ContentParts and
	//                     materialized before reaching the Gateway
	//   true            - URLs are embedded in text as [file_url:URL],
	//                     Agent downloads via tool (e.g., download_file)
	EmbedFileURL bool `yaml:"embed_file_url,omitempty"`

	// === 会话管理 ===
	// SessionTimeout 控制会话超时后自动分割的时间。
	// 如果距上一条消息超过此时间，则自动开始新会话。
	// 默认：0（关闭）。仅在显式配置时开启自动分割。
	// 示例："5m", "30m", "1h"
	SessionTimeout string `yaml:"session_timeout,omitempty"`

	// GroupSessionMode controls how group-chat sessions are scoped.
	// "shared" (default) shares one session across the whole group.
	// "isolated" gives each participant a separate group session.
	GroupSessionMode string `yaml:"group_session_mode,omitempty"`

	// === 消息聚合 ===
	// AggregateWindow 控制合并同一用户/会话中多条消息的时间窗口。
	// 用户在企微中同时发送文件+文字时，企微会拆分为两条独立回调。
	// 默认：2s。设为 0 禁用聚合。
	// 示例："1s", "2s", "3s"
	AggregateWindow string `yaml:"aggregate_window,omitempty"`

	HeartbeatInterval string `yaml:"heartbeat_interval,omitempty"`
	EnterChatWelcome  *bool  `yaml:"enter_chat_welcome,omitempty"`
}

// gatewayClient 是 Channel 所需的 Gateway 最小接口。
type gatewayClient interface {
	SendMessage(
		ctx context.Context,
		req gwclient.MessageRequest,
	) (gwclient.MessageResponse, error)

	Cancel(ctx context.Context, requestID string) (bool, error)
}

// messageSender 抽象出站消息 API，便于测试。
type messageSender interface {
	SendText(ctx context.Context, chatID, content string) error
	SendMarkdown(ctx context.Context, chatID, content string) error
}

// config 是收集所有功能选项的中间结构（与 Telegram 相同模式）。
type config struct {
	processingMessage   string
	cancelNoopMessage   string
	cancelFailedMessage string
	cancelOKMessage     string
	notAllowedMessage   string
	helpMessage         string
	newSessionMessage   string
}

// Option 配置企业微信 Channel。
type Option func(*config)

// WithProcessingMessage 设置等待回复时发送的"思考中"提示消息。
func WithProcessingMessage(msg string) Option {
	return func(c *config) { c.processingMessage = msg }
}

// WithCancelNoopMessage 设置没有正在执行的请求时的回复。
func WithCancelNoopMessage(msg string) Option {
	return func(c *config) { c.cancelNoopMessage = msg }
}

// WithCancelFailedMessage 设置取消失败时的回复。
func WithCancelFailedMessage(msg string) Option {
	return func(c *config) { c.cancelFailedMessage = msg }
}

// WithCancelOKMessage 设置取消成功时的回复。
func WithCancelOKMessage(msg string) Option {
	return func(c *config) { c.cancelOKMessage = msg }
}

// WithNotAllowedMessage 设置用户无权限时的回复。
func WithNotAllowedMessage(msg string) Option {
	return func(c *config) { c.notAllowedMessage = msg }
}

// WithHelpMessage 设置 /help 命令的回复。
func WithHelpMessage(msg string) Option {
	return func(c *config) { c.helpMessage = msg }
}

// Channel 实现基于企业微信 webhook 的聊天界面。
type Channel struct {
	gw       gatewayClient
	cfg      channelCfg
	crypt    *msgCrypt
	sender   messageSender
	lanes    *laneLocker
	inflight *inflightRequests

	name    string
	appName string

	// 机器人模式："notification" 或 "ai"
	botMode string

	chatPolicy string
	allowUsers map[string]struct{}

	runtimeAdminPolicy string
	runtimeAdminUsers  map[string]struct{}

	// embedImageURL controls how image URLs are handled:
	// false (default): URLs via ContentParts, materialized locally
	// true: URLs embedded in text as [image_url:...], Agent downloads via tool
	embedImageURL bool

	// embedFileURL controls how file URLs are handled:
	// false (default): URLs via ContentParts, materialized locally
	// true: URLs embedded in text as [file_url:...], Agent downloads via tool
	embedFileURL bool

	defaultCodingWorkspace    string
	codingScratchRoot         string
	codingArtifactOutputRoot  string
	runtimeTempRoot           string
	runtimeManagedUploadsRoot string
	runtimeModelName          string
	runtimeReplyDeliveryRoots []string
	stateDir                  string
	assistantIdentityFile     string

	// 会话管理：基于超时的自动分割
	sessionTimeout              time.Duration
	sessionTracker              *sessionTracker
	sessionCards                *sessionCardTracker
	personas                    *personaapi.Registry
	requestSystemPromptMu       sync.RWMutex
	requestSystemPromptTemplate string
	identityResolver            *userIdentityResolver
	groupSessionMode            string
	userLabelMode               string

	// 消息聚合：合并同时发送的文件+文字
	aggregator *messageAggregator
	runStatus  *runStatusTracker

	mediaClient       *http.Client
	mediaPrefetch     *mediaPrefetcher
	mediaURLValidator func(*url.URL) error

	// User-facing messages (set via Option, with defaults).
	processingMessage   string
	cancelNoopMessage   string
	cancelFailedMessage string
	cancelOKMessage     string
	notAllowedMessage   string
	helpMessage         string
	newSessionMessage   string
	enterChatWelcome    bool

	connectionMode            string
	wsURL                     string
	wsSecret                  string
	wsInstanceLockPath        string
	heartbeatInterval         time.Duration
	wsDialer                  websocketDialer
	reconnectDelay            time.Duration
	wsPushWriterMu            sync.RWMutex
	wsPushWriter              wsRequestWriter
	runtimeLifecycle          *runtimectl.Manager
	subagentService           publicsubagent.Service
	runtimeCompletionNotifier *runtimeCompletionNotifier
	browserDoctorNoteMu       sync.Mutex
	browserDoctorNote         string
	browserDoctorNoteAt       time.Time
}

var _ occhannel.TextSender = (*Channel)(nil)
var _ occhannel.MessageSender = (*Channel)(nil)

// New 通过 YAML 配置和功能选项创建企业微信 Channel。
func New(
	deps registry.ChannelDeps,
	spec registry.PluginSpec,
	opts ...Option,
) (occhannel.Channel, error) {
	return newChannel(deps, spec, opts...)
}

func newChannel(
	deps registry.ChannelDeps,
	spec registry.PluginSpec,
	opts ...Option,
) (occhannel.Channel, error) {
	if deps.Gateway == nil {
		return nil, errors.New(errNilGateway)
	}

	var yamlCfg channelCfg
	if err := registry.DecodeStrict(spec.Config, &yamlCfg); err != nil {
		return nil, err
	}
	if err := validate(yamlCfg); err != nil {
		return nil, err
	}

	connectionMode, err := parseConnectionMode(
		yamlCfg.ConnectionMode,
	)
	if err != nil {
		return nil, err
	}

	// CallbackPort == 0 表示共享 mux 模式（由外部挂载）。
	// 仅在明确需要独立端口模式时才填充默认端口。
	if strings.TrimSpace(yamlCfg.CallbackPath) == "" {
		yamlCfg.CallbackPath = defaultCallbackPath
	}

	chatPolicy, err := parseChatPolicy(yamlCfg.ChatPolicy)
	if err != nil {
		return nil, err
	}
	groupSessionMode, err := parseGroupSessionMode(
		yamlCfg.GroupSessionMode,
	)
	if err != nil {
		return nil, err
	}
	userLabelMode, err := parseUserLabelMode(yamlCfg.UserLabelMode)
	if err != nil {
		return nil, err
	}
	runtimeAdminPolicy, err := parseRuntimeAdminPolicy(
		yamlCfg.RuntimeAdminPolicy,
	)
	if err != nil {
		return nil, err
	}

	allowUsers := buildAllowSet(yamlCfg.AllowUsers)
	runtimeAdminUsers := buildAllowSet(yamlCfg.RuntimeAdminUsers)

	var crypt *msgCrypt
	if shouldInitWebhookCrypto(connectionMode, yamlCfg) {
		crypt, err = newMsgCrypt(
			yamlCfg.Token,
			yamlCfg.EncodingAESKey,
			yamlCfg.CorpID,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"wecom channel: init crypto: %w",
				err,
			)
		}
	}

	// 从配置中解析会话超时时间（默认关闭）。
	sessionTimeout := defaultSessionTimeout
	if yamlCfg.SessionTimeout != "" {
		parsed, err := time.ParseDuration(yamlCfg.SessionTimeout)
		if err != nil {
			return nil, fmt.Errorf("wecom channel: invalid session_timeout '%s': %w", yamlCfg.SessionTimeout, err)
		}
		sessionTimeout = parsed
	}

	heartbeatInterval := defaultHeartbeatInterval
	if strings.TrimSpace(yamlCfg.HeartbeatInterval) != "" {
		parsed, err := time.ParseDuration(
			yamlCfg.HeartbeatInterval,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"wecom channel: invalid heartbeat_interval "+
					"%q: %w",
				yamlCfg.HeartbeatInterval,
				err,
			)
		}
		heartbeatInterval = parsed
	}

	// 构建带默认值的中间配置。
	cfg := config{
		processingMessage:   defaultProcessingMessage,
		cancelNoopMessage:   defaultCancelNoopMessage,
		cancelFailedMessage: defaultCancelFailedMessage,
		cancelOKMessage:     defaultCancelOKMessage,
		notAllowedMessage:   defaultNotAllowedMessage,
		helpMessage:         defaultHelpMessage,
		newSessionMessage:   defaultNewSessionMessage,
	}
	// 应用功能选项。
	for _, opt := range opts {
		opt(&cfg)
	}

	// 根据机器人模式创建 sender
	var sender messageSender
	switch yamlCfg.BotMode {
	case botModeNotification:
		// 消息通知机器人：使用固定的 webhook_url
		sender = newWebhookSender(
			yamlCfg.WebhookURL,
			&http.Client{Timeout: 10 * time.Second},
		)
		log.InfofContext(
			context.Background(),
			"wecom: using notification bot mode with webhook_url",
		)
	case botModeAI:
		if connectionMode == connectionModeWebhook {
			sender = newWebhookSender(
				"",
				&http.Client{Timeout: 10 * time.Second},
			)
			log.InfofContext(
				context.Background(),
				"wecom: using ai bot webhook mode",
			)
		} else {
			log.InfofContext(
				context.Background(),
				"wecom: using ai bot websocket mode",
			)
		}
	}

	// 记录 embed_image_url 和 embed_file_url 设置
	if yamlCfg.EmbedImageURL {
		log.InfofContext(
			context.Background(),
			"wecom: embed_image_url enabled - image URLs "+
				"will be embedded in text",
		)
	}
	if yamlCfg.EmbedFileURL {
		log.InfofContext(
			context.Background(),
			"wecom: embed_file_url enabled - file URLs "+
				"will be embedded in text",
		)
	}

	// 记录会话超时设置
	if sessionTimeout > 0 {
		log.InfofContext(
			context.Background(),
			"wecom: session_timeout=%v - sessions will "+
				"auto-split after inactivity",
			sessionTimeout,
		)
	} else {
		log.InfofContext(
			context.Background(),
			"wecom: session_timeout disabled - sessions "+
				"will persist indefinitely",
		)
	}

	// 从配置中解析聚合窗口（默认 2s）。
	aggregateWindow := defaultAggregateWindow
	if yamlCfg.AggregateWindow != "" {
		parsed, err := time.ParseDuration(yamlCfg.AggregateWindow)
		if err != nil {
			return nil, fmt.Errorf(
				"wecom channel: invalid aggregate_window "+
					"'%s': %w",
				yamlCfg.AggregateWindow,
				err,
			)
		}
		aggregateWindow = parsed
	}
	if aggregateWindow > 0 {
		log.InfofContext(
			context.Background(),
			"wecom: aggregate_window=%v - messages within "+
				"window will be merged",
			aggregateWindow,
		)
	} else {
		log.InfofContext(
			context.Background(),
			"wecom: message aggregation disabled",
		)
	}

	enterChatWelcome := resolveEnterChatWelcome(
		yamlCfg.EnterChatWelcome,
	)
	textFastPathEnabled := true
	if yamlCfg.BotMode == botModeAI &&
		connectionMode == connectionModeWebSocket {
		textFastPathEnabled = false
		log.InfofContext(
			context.Background(),
			"wecom: text fast-path disabled for ai websocket "+
				"aggregation",
		)
	}
	stateDir := strings.TrimSpace(deps.StateDir)
	if stateDir == "" {
		stateDir = os.Getenv(sessionTrackerStateDirEnvName)
	}
	sessionTracker := sharedSessionTrackerWithPath(
		sessionTrackerStorePath(stateDir),
	)
	personaRegistry := personaapi.NewRegistry(
		resolvePersonaRegistryDir(stateDir, yamlCfg.PersonaDir),
	)
	requestSystemPromptTemplate, err := loadRequestSystemPromptTemplate(
		stateDir,
		yamlCfg.RequestSystemPromptFiles,
		yamlCfg.RequestSystemPromptDir,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"wecom channel: load request system prompt: %w",
			err,
		)
	}
	identityResolver := newUserIdentityResolver(
		stateDir,
		yamlCfg.UserLookupCommand,
	)
	mediaSnapshotDir := mediaPrefetchSnapshotDir(stateDir)
	codingDefaults, err := resolveChannelCodingDefaults(
		yamlCfg.RuntimeDefaultWorkdir,
		yamlCfg.RuntimeScratchRoot,
		stateDir,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"wecom channel: resolve coding defaults: %w",
			err,
		)
	}
	codingArtifactOutputRoot, err := ensureChannelArtifactOutputRoot(
		codingDefaults.ScratchRoot,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"wecom channel: prepare artifact output root: %w",
			err,
		)
	}
	runtimeTempRoot, err := ensureChannelTempRoot(stateDir)
	if err != nil {
		return nil, fmt.Errorf(
			"wecom channel: prepare runtime temp root: %w",
			err,
		)
	}
	runtimeManagedUploadsRoot := channelManagedUploadsRoot(stateDir)
	runtimeModelName := strings.TrimSpace(yamlCfg.RuntimeModelName)
	log.InfofContext(
		context.Background(),
		"wecom: runtime defaults model=%s workdir=%q "+
			"scratch_root=%q output_root=%q temp_root=%q",
		formatRuntimeModelLogValue(runtimeModelName),
		codingDefaults.DefaultWorkdir,
		codingDefaults.ScratchRoot,
		codingArtifactOutputRoot,
		runtimeTempRoot,
	)

	channel := &Channel{
		gw:       deps.Gateway,
		cfg:      yamlCfg,
		crypt:    crypt,
		sender:   sender,
		lanes:    newLaneLocker(),
		inflight: newInflightRequests(),
		name:     strings.TrimSpace(spec.Name),
		appName:  strings.TrimSpace(deps.AppName),
		botMode:  yamlCfg.BotMode,

		chatPolicy:                chatPolicy,
		allowUsers:                allowUsers,
		runtimeAdminPolicy:        runtimeAdminPolicy,
		runtimeAdminUsers:         runtimeAdminUsers,
		groupSessionMode:          groupSessionMode,
		userLabelMode:             userLabelMode,
		embedImageURL:             yamlCfg.EmbedImageURL,
		embedFileURL:              yamlCfg.EmbedFileURL,
		defaultCodingWorkspace:    codingDefaults.DefaultWorkdir,
		codingScratchRoot:         codingDefaults.ScratchRoot,
		codingArtifactOutputRoot:  codingArtifactOutputRoot,
		runtimeTempRoot:           runtimeTempRoot,
		runtimeManagedUploadsRoot: runtimeManagedUploadsRoot,
		runtimeModelName:          runtimeModelName,
		runtimeReplyDeliveryRoots: normalizeReplyDeliveryRoots(
			yamlCfg.RuntimeReplyDeliveryRoots,
		),
		stateDir: stateDir,
		assistantIdentityFile: promptasset.DefaultPaths(stateDir).
			IdentityFile,

		sessionTimeout:              sessionTimeout,
		sessionTracker:              sessionTracker,
		sessionCards:                newSessionCardTracker(),
		personas:                    personaRegistry,
		requestSystemPromptTemplate: requestSystemPromptTemplate,
		identityResolver:            identityResolver,
		runStatus:                   newRunStatusTracker(),

		aggregator: newMessageAggregatorWithTextFastPath(
			aggregateWindow,
			textFastPathEnabled,
		),
		mediaClient: &http.Client{
			Timeout: defaultMediaDownloadTimeout,
		},
		mediaPrefetch: newMediaPrefetcherWithSnapshotDir(
			defaultMediaPrefetchTTL,
			mediaSnapshotDir,
			defaultMediaSnapshotTTL,
		),

		processingMessage:   cfg.processingMessage,
		cancelNoopMessage:   cfg.cancelNoopMessage,
		cancelFailedMessage: cfg.cancelFailedMessage,
		cancelOKMessage:     cfg.cancelOKMessage,
		notAllowedMessage:   cfg.notAllowedMessage,
		helpMessage:         cfg.helpMessage,
		newSessionMessage:   cfg.newSessionMessage,
		enterChatWelcome:    enterChatWelcome,

		connectionMode: connectionMode,
		wsURL: defaultString(
			yamlCfg.WSURL,
			defaultWebSocketURL,
		),
		wsSecret: strings.TrimSpace(yamlCfg.Secret),
		wsInstanceLockPath: websocketInstanceLockPath(
			sessionTracker.path,
			yamlCfg.AIBotID,
		),
		heartbeatInterval: heartbeatInterval,
	}
	if yamlCfg.BotMode == botModeAI &&
		connectionMode == connectionModeWebSocket {
		channel.runtimeCompletionNotifier =
			newRuntimeCompletionNotifier(
				stateDir,
				channel.currentRuntimeVersion,
				channel.sendRuntimeCompletionResponse,
			)
	}
	return channel, nil
}

func validate(cfg channelCfg) error {
	// 验证 bot_mode
	botMode := strings.TrimSpace(cfg.BotMode)
	if botMode == "" {
		return errors.New("wecom channel: bot_mode is required (must be 'notification' or 'ai')")
	}
	if botMode != botModeNotification && botMode != botModeAI {
		return fmt.Errorf("wecom channel: invalid bot_mode '%s' (must be 'notification' or 'ai')", botMode)
	}

	connectionMode, err := parseConnectionMode(cfg.ConnectionMode)
	if err != nil {
		return err
	}

	// 按模式验证特定字段
	switch botMode {
	case botModeNotification:
		if connectionMode == connectionModeWebSocket {
			return errors.New(
				"wecom channel: websocket mode only supports " +
					"ai bot mode",
			)
		}
		if strings.TrimSpace(cfg.Token) == "" {
			return errors.New("wecom channel: token is required")
		}
		if strings.TrimSpace(cfg.EncodingAESKey) == "" {
			return errors.New(
				"wecom channel: encoding_aes_key is required",
			)
		}
		// 消息通知机器人必须提供 webhook_url
		if strings.TrimSpace(cfg.WebhookURL) == "" {
			return errors.New("wecom channel: webhook_url is required for notification bot mode")
		}

	case botModeAI:
		// 智能机器人模式：aibotid 为可选（可从回调消息中提取）
		// 如果配置了，可用于验证/过滤入站消息
		if cfg.AIBotID != "" {
			log.Debugf("wecom channel: AI bot mode with configured aibotid=%s (for validation)", cfg.AIBotID)
		}
		if connectionMode == connectionModeWebSocket {
			if strings.TrimSpace(cfg.AIBotID) == "" {
				return errors.New(
					"wecom channel: aibotid is required " +
						"for websocket mode",
				)
			}
			if strings.TrimSpace(cfg.Secret) == "" {
				return errors.New(
					"wecom channel: secret is required " +
						"for websocket mode",
				)
			}
			return nil
		}
		if strings.TrimSpace(cfg.Token) == "" {
			return errors.New("wecom channel: token is required")
		}
		if strings.TrimSpace(cfg.EncodingAESKey) == "" {
			return errors.New(
				"wecom channel: encoding_aes_key is required",
			)
		}
	}

	if err := validateReplyPrefix(cfg.ReplyPrefix); err != nil {
		return err
	}

	return nil
}

func resolvePersonaRegistryDir(
	stateDir string,
	override string,
) string {
	override = strings.TrimSpace(override)
	if override != "" {
		return override
	}
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return ""
	}
	defaultDir := promptasset.DefaultPaths(stateDir).PersonaDir
	legacyDir := filepath.Join(
		stateDir,
		sessionTrackerStoreDirName,
		personaStoreDirName,
	)
	if legacyDir != "" && !isDirPath(defaultDir) && isDirPath(legacyDir) {
		return legacyDir
	}
	return defaultDir
}

func loadRequestSystemPromptTemplate(
	stateDir string,
	files []string,
	dir string,
) (string, error) {
	dir = strings.TrimSpace(dir)
	files = normalizeStringSequence(files)
	switch {
	case len(files) > 0 || dir != "":
		return promptasset.ReadDiskBundle(files, dir)
	case strings.TrimSpace(stateDir) != "":
		paths, err := promptasset.EnsureDefaultFiles(stateDir)
		if err != nil {
			return "", err
		}
		return promptasset.ReadDiskBundle(nil, paths.WeComRequestDir)
	default:
		return promptasset.ReadEmbeddedBundle(
			promptasset.DefaultWeComRequestEmbeddedDir,
		)
	}
}

func isDirPath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil || info == nil {
		return false
	}
	return info.IsDir()
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
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func shouldInitWebhookCrypto(
	connectionMode string,
	cfg channelCfg,
) bool {
	if connectionMode != connectionModeWebSocket {
		return true
	}

	return strings.TrimSpace(cfg.Token) != "" &&
		strings.TrimSpace(cfg.EncodingAESKey) != ""
}

func parseConnectionMode(raw string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(raw))
	if mode == "" {
		return defaultConnectionMode, nil
	}
	switch mode {
	case connectionModeWebhook, connectionModeWebSocket:
		return mode, nil
	default:
		return "", fmt.Errorf(
			"wecom channel: unsupported connection_mode: %s",
			raw,
		)
	}
}

func resolveEnterChatWelcome(raw *bool) bool {
	if raw == nil {
		return defaultEnterChatWelcomeEnabled
	}
	return *raw
}

func formatRuntimeModelLogValue(modelName string) string {
	value := strings.TrimSpace(modelName)
	if value != "" {
		return value
	}
	return "unspecified"
}

func messageUserID(msg WebhookMessage) string {
	if value := strings.TrimSpace(msg.From.UserID); value != "" {
		return value
	}
	if value := strings.TrimSpace(msg.From.Alias); value != "" {
		return value
	}
	return strings.TrimSpace(msg.ChatID)
}

func (c *Channel) runtimeModelDisplayName() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.runtimeModelName)
}

func (c *Channel) runtimeModelLogValue() string {
	return formatRuntimeModelLogValue(c.runtimeModelDisplayName())
}

// formatFileURL 格式化文件 URL 以嵌入文本中。
// 格式：[file_url:https://example.com/file.pdf]
func formatFileURL(url string) string {
	return fileURLPrefix + url + fileURLSuffix
}

// formatImageURL 格式化图片 URL 以嵌入文本中。
// 格式：[image_url:https://example.com/image.jpg]
func formatImageURL(url string) string {
	return imageURLPrefix + url + imageURLSuffix
}

// ID 返回 Channel 标识符。
func (c *Channel) ID() string { return pluginType }

// PersonaDir returns the resolved persona registry directory.
func (c *Channel) PersonaDir() string {
	if c == nil || c.personas == nil {
		return ""
	}
	return c.personas.Dir()
}

// Handler 返回企微回调端点的 HTTP handler。
// 允许外部服务器（如 tRPC HTTP Service）将回调路由挂载到共享 mux 上，
// 而不需要独立端口。
func (c *Channel) Handler() http.Handler {
	return http.HandlerFunc(c.handleHTTP)
}

// Pattern 返回用于注册到共享 mux 的回调 URL 路径。
func (c *Channel) Pattern() string {
	if c == nil || c.connectionMode == connectionModeWebSocket {
		return ""
	}
	return c.cfg.CallbackPath
}

// HTTPServiceName returns the shared tRPC HTTP service used by this
// distribution for WeCom callbacks.
func (c *Channel) HTTPServiceName() string {
	return ingress.DefaultHTTPServiceName
}

// HTTPPatterns returns the callback path mounted onto the shared mux.
func (c *Channel) HTTPPatterns() []string {
	if c == nil {
		return nil
	}
	pattern := c.Pattern()
	if strings.TrimSpace(pattern) == "" {
		return nil
	}
	return []string{pattern}
}

// MountHTTP mounts the WeCom callback handler onto a shared HTTP mux.
func (c *Channel) MountHTTP(mux *http.ServeMux) error {
	if c == nil {
		return errors.New("wecom: nil channel")
	}
	if mux == nil {
		return errors.New("wecom: nil mux")
	}

	pattern := strings.TrimSpace(c.cfg.CallbackPath)
	if c.connectionMode == connectionModeWebSocket {
		return nil
	}
	if pattern == "" {
		return errors.New("wecom: empty callback path")
	}

	mux.Handle(pattern, c.Handler())
	return nil
}

// Run starts the WeCom channel and blocks until ctx is done.
//
// 如果 callback_port 为 0（或未设置），Run 假设回调路由已由外部通过
// Handler/Pattern 挂载到共享 HTTP 服务器上，仅阻塞等待 ctx 取消。
//
// 如果 callback_port > 0，Run 在该端口启动独立 HTTP 服务器（为向后兼容保留）。
func (c *Channel) Run(ctx context.Context) error {
	if c == nil {
		return errors.New("wecom: nil channel")
	}

	if c.connectionMode == connectionModeWebSocket {
		return c.runWebSocket(ctx)
	}

	// 共享 mux 模式：回调路由由外部挂载。
	if c.cfg.CallbackPort == 0 {
		log.InfofContext(ctx, "wecom: shared mux mode, callback path %s (no standalone listener)", c.cfg.CallbackPath)
		<-ctx.Done()
		return ctx.Err()
	}

	// 兼容模式：启动独立 HTTP 服务器。
	mux := http.NewServeMux()
	mux.HandleFunc(c.cfg.CallbackPath, c.handleHTTP)

	addr := fmt.Sprintf(":%d", c.cfg.CallbackPort)
	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	errCh := make(chan error, 1)
	go func() {
		log.InfofContext(ctx, "wecom: listening on %s%s", addr, c.cfg.CallbackPath)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

// handleHTTP 分发 GET（URL 验证）和 POST（消息回调）请求。
func (c *Channel) handleHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		c.handleVerify(w, r)
	case http.MethodPost:
		c.handleCallback(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleVerify 处理企业微信 URL 验证（GET 回调）。
func (c *Channel) handleVerify(w http.ResponseWriter, r *http.Request) {
	if c == nil || c.crypt == nil {
		http.Error(w, "webhook callback disabled", http.StatusNotFound)
		return
	}

	q := r.URL.Query()
	msgSignature := q.Get("msg_signature")
	timestamp := q.Get("timestamp")
	nonce := q.Get("nonce")
	echostr := q.Get("echostr")

	// 调试日志，用于排查问题
	log.InfofContext(r.Context(), "wecom: handleVerify called - msg_signature=%s, timestamp=%s, nonce=%s, echostr_len=%d",
		msgSignature, timestamp, nonce, len(echostr))
	log.InfofContext(r.Context(), "wecom: config - token_len=%d, aes_key_len=%d, callback_path=%s",
		len(c.cfg.Token), len(c.cfg.EncodingAESKey), c.cfg.CallbackPath)

	plainEcho, err := c.crypt.VerifyURL(msgSignature, timestamp, nonce, echostr)
	if err != nil {
		log.WarnfContext(r.Context(), "wecom: verify URL failed: %v", err)
		http.Error(w, "verify failed", http.StatusForbidden)
		return
	}

	log.InfofContext(r.Context(), "wecom: verify URL success, echo_len=%d", len(plainEcho))
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(plainEcho)
}

// handleCallback 处理企业微信消息回调（POST）。
func (c *Channel) handleCallback(w http.ResponseWriter, r *http.Request) {
	if c == nil || c.crypt == nil {
		http.Error(w, "webhook callback disabled", http.StatusNotFound)
		return
	}

	q := r.URL.Query()
	msgSignature := q.Get("msg_signature")
	timestamp := q.Get("timestamp")
	nonce := q.Get("nonce")

	log.InfofContext(r.Context(), "wecom: handleCallback called - msg_signature=%s, timestamp=%s, nonce=%s",
		msgSignature, timestamp, nonce)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.WarnfContext(r.Context(), "wecom: read body failed: %v", err)
		http.Error(w, "read body failed", http.StatusBadRequest)
		return
	}

	log.InfofContext(r.Context(), "wecom: handleCallback body_len=%d", len(body))

	msg, err := c.decryptWebhookMessage(
		r.Context(),
		msgSignature,
		timestamp,
		nonce,
		body,
	)
	if err == nil && isEnterChatEvent(msg) {
		if err := c.writeWebhookEnterChatResponse(
			r.Context(),
			w,
			timestamp,
			nonce,
			msg,
		); err != nil {
			log.WarnfContext(
				r.Context(),
				"wecom: write enter_chat response failed: %v",
				err,
			)
		}
		return
	}

	writeWebhookSuccessResponse(w)

	go func() {
		ctx := context.Background()
		if err == nil {
			err = c.processIncomingMessage(ctx, msg)
		} else {
			err = c.processMessage(
				ctx,
				msgSignature,
				timestamp,
				nonce,
				body,
			)
		}
		if err != nil {
			log.WarnfContext(ctx, "wecom: process message failed: %v", err)
		} else {
			log.InfofContext(ctx, "wecom: process message success")
		}
	}()
}

// processMessage 解密、解析并处理一条入站消息。
func (c *Channel) processMessage(
	ctx context.Context,
	msgSignature, timestamp, nonce string,
	body []byte,
) error {
	msg, err := c.decryptWebhookMessage(
		ctx,
		msgSignature,
		timestamp,
		nonce,
		body,
	)
	if err != nil {
		return err
	}
	return c.processIncomingMessage(ctx, msg)
}

func (c *Channel) decryptWebhookMessage(
	ctx context.Context,
	msgSignature string,
	timestamp string,
	nonce string,
	body []byte,
) (WebhookMessage, error) {
	if c == nil || c.crypt == nil {
		return WebhookMessage{}, errors.New(
			"wecom channel: webhook crypto unavailable",
		)
	}

	log.InfofContext(
		ctx,
		"wecom: processMessage start, body_len=%d",
		len(body),
	)

	plaintext, err := c.crypt.DecryptMsg(
		msgSignature,
		timestamp,
		nonce,
		body,
	)
	if err != nil {
		return WebhookMessage{}, fmt.Errorf("decrypt: %w", err)
	}

	log.InfofContext(
		ctx,
		"wecom: decrypt success, plaintext_len=%d, content=%s",
		len(plaintext),
		string(plaintext),
	)

	var msg WebhookMessage
	if err := json.Unmarshal(plaintext, &msg); err != nil {
		return WebhookMessage{}, fmt.Errorf(
			"unmarshal message: %w",
			err,
		)
	}
	msg.RawBody = append(json.RawMessage(nil), plaintext...)
	return msg, nil
}

func isEnterChatEvent(msg WebhookMessage) bool {
	return msg.MsgType == MsgTypeEvent &&
		strings.TrimSpace(msg.Event.EventType) ==
			eventTypeEnterChat
}

func (c *Channel) writeWebhookEnterChatResponse(
	ctx context.Context,
	w http.ResponseWriter,
	timestamp string,
	nonce string,
	msg WebhookMessage,
) error {
	reply, ok := c.enterChatWelcomeReply(ctx, msg)
	if !ok {
		writeWebhookEmptyResponse(w)
		return nil
	}

	plaintext, err := json.Marshal(reply)
	if err != nil {
		writeWebhookEmptyResponse(w)
		return fmt.Errorf(
			"wecom: marshal enter_chat reply: %w",
			err,
		)
	}

	encrypted, err := c.crypt.EncryptReply(
		plaintext,
		timestamp,
		nonce,
	)
	if err != nil {
		writeWebhookEmptyResponse(w)
		return fmt.Errorf(
			"wecom: encrypt enter_chat reply: %w",
			err,
		)
	}

	log.InfofContext(
		ctx,
		"wecom: send enter_chat welcome via webhook user=%s "+
			"model=%s",
		messageUserID(msg),
		c.runtimeModelLogValue(),
	)
	w.Header().Set(senderHeaderType, senderContentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(encrypted)
	return nil
}

func writeWebhookSuccessResponse(w http.ResponseWriter) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("success"))
}

func writeWebhookEmptyResponse(w http.ResponseWriter) {
	w.WriteHeader(http.StatusOK)
}

func (c *Channel) processIncomingMessage(
	ctx context.Context,
	msg WebhookMessage,
) error {
	log.InfofContext(
		ctx,
		"wecom: message parsed msgtype=%s msgid=%s chatid=%s "+
			"chattype=%s bot_mode=%s model=%s",
		msg.MsgType,
		msg.MsgID,
		msg.ChatID,
		msg.ChatType,
		c.botMode,
		c.runtimeModelLogValue(),
	)

	if c.botMode == botModeAI && msg.ResponseURL != "" {
		log.InfofContext(ctx, "wecom: AI bot mode - captured response_url=%s, aibotid=%s",
			msg.ResponseURL, msg.AIBotID)
	}

	// 跳过 event 和 stream 消息（不需要聚合）。
	if msg.MsgType == MsgTypeEvent || msg.MsgType == MsgTypeStream {
		return c.handleMessage(ctx, msg)
	}

	c.prefetchMessageMedia(msg)

	// Check if this is a command message — commands should not be aggregated.
	text, _, _ := c.extractContentParts(msg)
	if cmd := parseCommand(text); cmd != "" {
		return c.handleMessage(ctx, msg)
	}

	// 使用聚合器在短时间窗口内缓冲消息。
	// 窗口到期后，所有缓冲消息被合并并统一处理。
	log.InfofContext(ctx, "wecom: adding message to aggregator (type=%s, window=%v)",
		msg.MsgType, c.aggregator.window)
	c.aggregator.Add(msg, func(msgs []WebhookMessage) {
		merged := mergeMessages(msgs)
		if len(msgs) > 1 {
			log.InfofContext(ctx, "wecom: aggregated %d messages into one (types: %s)",
				len(msgs), describeMessageTypes(msgs))
		}
		if err := c.handleMessage(ctx, merged); err != nil {
			log.WarnfContext(ctx, "wecom: handle aggregated message failed: %v", err)
		}
	})
	return nil
}

// senderForMsg 根据机器人模式返回每个请求独立的 messageSender。
// 智能机器人模式下，每条消息携带一次性 response_url，
// 因此为每个请求创建独立的 aibotSender 以避免竞态。
// 消息通知机器人模式下，返回共享的 webhook sender。
func (c *Channel) senderForMsg(msg WebhookMessage) messageSender {
	if c.botMode == botModeAI &&
		c.connectionMode == connectionModeWebSocket &&
		msg.ReplyWriter != nil &&
		strings.TrimSpace(msg.CallbackReqID) != "" {
		return newAIBotWebSocketSender(
			msg.ReplyWriter,
			msg.CallbackReqID,
		)
	}
	if c.botMode == botModeAI && msg.ResponseURL != "" {
		return newAIBotSender(msg.ResponseURL, &http.Client{Timeout: 30 * time.Second})
	}
	return c.sender
}

// handleMessage 路由一条已解密的企微消息。
func (c *Channel) handleMessage(ctx context.Context, msg WebhookMessage) error {
	if msg.MsgType == MsgTypeEvent {
		return c.handleEventMessage(ctx, msg)
	}

	text, contentParts, decryptHints := c.extractContentParts(msg)
	if strings.TrimSpace(text) == "" && len(contentParts) == 0 {
		return nil
	}

	chatID := msg.ChatID
	fromID := messageTransportUserID(msg)

	// 为每个请求创建独立的 sender 以避免竞态。
	// 智能机器人模式下，每条消息有独立的一次性 response_url。
	sender := c.senderForMsg(msg)
	if c.botMode == botModeAI &&
		c.connectionMode == connectionModeWebSocket {
		if msg.ReplyWriter != nil &&
			strings.TrimSpace(msg.CallbackReqID) != "" {
			log.InfofContext(
				ctx,
				"wecom: reply via websocket req_id=%s",
				msg.CallbackReqID,
			)
		} else if strings.TrimSpace(msg.ResponseURL) != "" {
			log.WarnfContext(
				ctx,
				"wecom: websocket callback missing req_id, "+
					"fallback to response_url",
			)
		}
	}

	// 检查用户权限。
	if !c.isUserAllowed(fromID) {
		_ = sender.SendText(ctx, chatID, c.notAllowedMessage)
		return nil
	}

	// 构建基础会话 ID（chat/user 标识符，不含 epoch）。
	baseSessionID := buildScopedSessionID(
		chatID,
		fromID,
		c.groupSessionMode,
	)
	c.sessionTracker.recordKnownUsers(
		baseSessionID,
		collectKnownUserIDs(msg),
	)

	// 检查命令。
	cmd := parseCommandInput(text)
	if cmd.keyword != "" {
		if topic, ok := parseCommandHelpAlias(cmd); ok {
			_ = sender.SendText(
				ctx,
				chatID,
				formatCommandHelpTopic(topic),
			)
			return nil
		}
		switch cmd.keyword {
		case helpKeyword:
			return c.handleHelpCommand(
				ctx,
				chatID,
				baseSessionID,
				cmd.args,
				sender,
			)
		case nameKeyword:
			return c.handleNameCommand(
				ctx,
				chatID,
				baseSessionID,
				cmd.rawArgs,
				sender,
			)
		case welcomeKeyword:
			return c.handleWelcomeCommand(
				ctx,
				chatID,
				baseSessionID,
				sender,
			)
		case cronKeyword:
			return c.handleCronCommand(
				ctx,
				chatID,
				baseSessionID,
				fromID,
				cmd,
				sender,
			)
		case statusKeyword:
			sessionInfo := c.sessionTracker.getOrCreateSession(
				baseSessionID,
				0,
			)
			return c.handleStatusCommand(
				ctx,
				chatID,
				sessionInfo,
				sender,
			)
		case cancelKeyword:
			// 取消命令使用当前会话（含 epoch）。
			sessionInfo := c.sessionTracker.getOrCreateSession(
				baseSessionID,
				0,
			)
			return c.handleCancelCommand(
				ctx,
				chatID,
				sessionInfo.sessionID,
				sender,
			)
		case newKeyword:
			return c.handleNewSessionCommand(
				ctx,
				chatID,
				baseSessionID,
				sender,
			)
		case recallKeyword:
			return c.handleRecallCommand(
				ctx,
				chatID,
				baseSessionID,
				sender,
			)
		case runtimeKeyword:
			return c.handleRuntimeCommand(
				ctx,
				chatID,
				baseSessionID,
				fromID,
				msg.ResponseURL,
				cmd,
				sender,
			)
		case subagentsKeyword:
			return c.handleSubagentsCommand(
				ctx,
				chatID,
				fromID,
				baseSessionID,
				cmd,
				sender,
			)
		case sessionKeyword:
			return c.handleSessionCommand(
				ctx,
				chatID,
				baseSessionID,
				sender,
			)
		case sessionsKeyword:
			return c.handleSessionsCommand(
				ctx,
				chatID,
				baseSessionID,
				cmd.args,
				sender,
			)
		case switchKeyword:
			return c.handleSwitchCommand(
				ctx,
				chatID,
				baseSessionID,
				cmd.args,
				sender,
			)
		case personaKeyword:
			return c.handlePersonaCommand(
				ctx,
				chatID,
				baseSessionID,
				cmd,
				sender,
			)
		case workspaceKeyword:
			return c.handleWorkspaceCommand(
				ctx,
				chatID,
				baseSessionID,
				cmd.args,
				sender,
			)
		default:
			return nil
		}
	}

	requestLease, err := c.admitRuntimeRequest(
		ctx,
		baseSessionID,
	)
	if err != nil {
		_ = sender.SendText(
			ctx,
			chatID,
			c.runtimeAdmissionMessage(),
		)
		return nil
	}
	if requestLease != nil {
		defer requestLease.Done()
	}

	// 获取或创建会话。默认根会话 ID 稳定，可跨重启续上历史；
	// 显式分会话时再派生带后缀的新 session ID。
	sessionInfo := c.sessionTracker.getOrCreateSession(baseSessionID, c.sessionTimeout)
	sessionID := sessionInfo.sessionID

	log.InfofContext(ctx, "wecom: using session %s (base: %s)", sessionID, baseSessionID)

	requestID := buildRequestID(chatID, msg.MsgID)

	replyState := newReplyStreamState(
		requestID,
		msg.MsgID,
		sender,
		c.cfg.EnableStream,
	)
	if replyState != nil {
		replyState.nativeThinking = c.usesNativeThinkingStream()
	}
	if requestLease != nil {
		requestLease.SetAbort(
			requestID,
			func(cancelCtx context.Context) {
				_, _ = c.gw.Cancel(cancelCtx, requestID)
			},
		)
	}

	return c.lanes.withLockErrNotify(
		sessionID,
		func() {
			c.runStatus.queue(
				sessionID,
				requestID,
				defaultQueuedMessage,
			)
			c.sendQueuedReplyHint(
				ctx,
				msg,
				sender,
				replyState,
			)
		},
		func() error {
			if requestLease != nil {
				if err := requestLease.Context().Err(); err != nil {
					return c.handleRuntimeLeaseCanceled(
						ctx,
						msg,
						sender,
						replyState,
						sessionID,
						requestID,
						requestLease.Context(),
					)
				}
				requestLease.MarkRunning()
			}
			c.inflight.Set(sessionID, requestID)
			defer c.inflight.Clear(sessionID, requestID)

			requestCtx := ctx
			if requestLease != nil {
				requestCtx = requestLease.Context()
			}
			return c.callGatewayAndReplyWithState(
				requestCtx,
				msg,
				text,
				contentParts,
				decryptHints,
				fromID,
				requestID,
				sessionID,
				sessionInfo,
				sender,
				replyState,
			)
		})
}

func (c *Channel) handleEventMessage(
	ctx context.Context,
	msg WebhookMessage,
) error {
	eventType := strings.TrimSpace(msg.Event.EventType)
	if eventType == "" {
		return nil
	}
	switch eventType {
	case eventTypeEnterChat:
		return c.handleEnterChatEvent(ctx, msg)
	case eventTypeTemplateCard:
		return c.handleTemplateCardEvent(ctx, msg)
	case eventTypeFeedback:
		return c.handleFeedbackEvent(ctx, msg)
	default:
		log.InfofContext(
			ctx,
			"wecom: skip event message: %s",
			eventType,
		)
		return nil
	}
}

func (c *Channel) handleEnterChatEvent(
	ctx context.Context,
	msg WebhookMessage,
) error {
	reply, ok := c.enterChatWelcomeReply(ctx, msg)
	if !ok {
		return nil
	}

	if c.connectionMode != connectionModeWebSocket {
		log.InfofContext(
			ctx,
			"wecom: webhook enter_chat handled in callback "+
				"response user=%s model=%s",
			messageUserID(msg),
			c.runtimeModelLogValue(),
		)
		return nil
	}
	if msg.ReplyWriter == nil ||
		strings.TrimSpace(msg.CallbackReqID) == "" {
		log.WarnfContext(
			ctx,
			"wecom: skip enter_chat welcome: sender "+
				"unavailable user=%s req_id=%q "+
				"writer_nil=%t model=%s",
			messageUserID(msg),
			strings.TrimSpace(msg.CallbackReqID),
			msg.ReplyWriter == nil,
			c.runtimeModelLogValue(),
		)
		return nil
	}

	log.InfofContext(
		ctx,
		"wecom: send enter_chat welcome via websocket "+
			"user=%s model=%s",
		messageUserID(msg),
		c.runtimeModelLogValue(),
	)
	if err := sendWebSocketWelcome(
		ctx,
		msg.ReplyWriter,
		msg.CallbackReqID,
		reply,
	); err != nil {
		return fmt.Errorf(
			"wecom: send enter_chat welcome: %w",
			err,
		)
	}
	return nil
}

func (c *Channel) handleTemplateCardEvent(
	ctx context.Context,
	msg WebhookMessage,
) error {
	event := msg.Event.TemplateCardEvent
	if event == nil {
		return nil
	}

	log.InfofContext(
		ctx,
		"wecom: template card event: %s",
		templateCardEventSummary(event),
	)

	if isControlCardEventKey(event.EventKey) {
		return c.handleControlTemplateCardEvent(
			ctx,
			msg,
			event,
		)
	}

	if isPersonaCardEventKey(event.EventKey) {
		return c.handlePersonaTemplateCardEvent(
			ctx,
			msg,
			event,
		)
	}

	log.InfofContext(
		ctx,
		"wecom: skip template card event key: %s",
		event.EventKey,
	)
	return nil
}

func (c *Channel) handlePersonaTemplateCardEvent(
	ctx context.Context,
	msg WebhookMessage,
	event *TemplateCardEvent,
) error {
	fromID := messageTransportUserID(msg)
	if !c.isUserAllowed(fromID) {
		return nil
	}

	baseSessionID := buildScopedSessionID(
		msg.ChatID,
		fromID,
		c.groupSessionMode,
	)
	sessionInfo := c.sessionTracker.getOrCreateSession(
		baseSessionID,
		0,
	)
	view := personaCardViewDefault
	cardNote := ""
	fallbackText := ""
	eventKey := strings.TrimSpace(event.EventKey)

	switch {
	case eventKey == personaCardEventHome:
		return c.handleControlTemplateCardEvent(
			ctx,
			msg,
			&TemplateCardEvent{
				EventKey: controlCardEventHome,
				TaskID:   event.TaskID,
			},
		)
	case eventKey == personaCardEventApply:
		selection := resolvePersonaSelection(sessionInfo, event)
		sessionInfo, cardNote, fallbackText =
			c.applyPersonaTemplateSelection(
				baseSessionID,
				sessionInfo,
				selection,
			)
	case eventKey == personaCardEventSaveHelp:
		view = personaCardViewSaveHelp
	case isPersonaCardEventKey(eventKey):
		selection, _ := personaCardQuickPersonaID(eventKey)
		sessionInfo, cardNote, fallbackText =
			c.applyPersonaTemplateSelection(
				baseSessionID,
				sessionInfo,
				selection,
			)
	default:
		return nil
	}

	sender := c.senderForMsg(msg)
	updater, ok := sender.(templateCardUpdater)
	if ok && updater != nil {
		taskID := strings.TrimSpace(event.TaskID)
		if taskID == "" {
			taskID = newInteractiveCardTaskID(
				personaCardTaskPrefix,
				baseSessionID,
			)
		}
		defs, err := c.listPersonas()
		if err != nil {
			return err
		}
		card := buildPersonaSettingsCard(
			c.assistantDisplayNameForSession(
				baseSessionID,
			),
			c.activePersonaDisplay(sessionInfo),
			sessionInfo,
			defs,
			taskID,
			view,
			cardNote,
			c.personaStorageEnabled(),
		)
		if err := updater.UpdateTemplateCard(ctx, card); err != nil {
			return err
		}
		c.rememberSessionCardWithVariant(
			baseSessionID,
			sessionCardViewPersona,
			view,
			card.TaskID,
			sessionInfo,
		)
		return nil
	}

	if sender != nil && fallbackText != "" {
		_ = sender.SendText(ctx, msg.ChatID, fallbackText)
	}
	return nil
}

func (c *Channel) applyPersonaTemplateSelection(
	baseSessionID string,
	sessionInfo *sessionInfo,
	selection string,
) (*sessionInfo, string, string) {
	def, ok, err := c.lookupPersona(selection)
	if err != nil {
		message := "读取人格失败：" + err.Error()
		return sessionInfo, message, message
	}
	if !ok {
		message := "未知人格：" + strings.TrimSpace(selection) +
			"\n" + personaListHelpLine
		return sessionInfo, message, message
	}
	sessionInfo = c.sessionTracker.setPersona(
		baseSessionID,
		def.ID,
	)
	return sessionInfo,
		personaCardChangedNote,
		c.formatPersonaChanged(sessionInfo, def)
}

func (c *Channel) handleFeedbackEvent(
	ctx context.Context,
	msg WebhookMessage,
) error {
	if msg.Event.FeedbackEvent == nil {
		return nil
	}
	event := msg.Event.FeedbackEvent
	log.InfofContext(
		ctx,
		"wecom: feedback event id=%s type=%d reasons=%v",
		event.ID,
		event.Type,
		event.InaccurateReasonList,
	)
	return nil
}

func (c *Channel) enterChatWelcomeReply(
	ctx context.Context,
	msg WebhookMessage,
) (callbackReplyBody, bool) {
	if c.botMode != botModeAI {
		return callbackReplyBody{}, false
	}
	if !c.enterChatWelcome {
		log.InfofContext(
			ctx,
			"wecom: skip enter_chat welcome: disabled "+
				"model=%s",
			c.runtimeModelLogValue(),
		)
		return callbackReplyBody{}, false
	}

	fromID := messageTransportUserID(msg)
	if !c.isUserAllowed(fromID) {
		log.InfofContext(
			ctx,
			"wecom: skip enter_chat welcome: user=%s not "+
				"allowed model=%s",
			strings.TrimSpace(fromID),
			c.runtimeModelLogValue(),
		)
		return callbackReplyBody{}, false
	}

	baseSessionID := buildScopedSessionID(
		msg.ChatID,
		fromID,
		c.groupSessionMode,
	)

	return buildEnterChatWelcomeReplyWithTaskID(
		c.assistantDisplayNameForSession(baseSessionID),
		c.runtimeModelDisplayName(),
		newInteractiveCardTaskID(
			controlCardTaskPrefix,
			baseSessionID,
		),
	), true
}

// handleNewSessionCommand 处理 /new 命令，开始一个新会话。
func (c *Channel) handleNewSessionCommand(
	ctx context.Context,
	chatID string,
	baseSessionID string,
	sender messageSender,
) error {
	sessionInfo := c.sessionTracker.startNewSession(baseSessionID)
	log.InfofContext(
		ctx,
		"wecom: new session started: %s (previous: %s)",
		sessionInfo.sessionID,
		sessionInfo.recallSessionID,
	)

	_ = sender.SendText(ctx, chatID, c.newSessionMessage)
	c.syncActiveSessionCard(
		ctx,
		baseSessionID,
		sessionInfo,
		sender,
	)
	return nil
}

// isUserAllowed 检查用户是否有权与机器人交互。
func (c *Channel) isUserAllowed(userID string) bool {
	switch c.chatPolicy {
	case chatPolicyDisabled:
		return false
	case chatPolicyOpen:
		return true
	case chatPolicyAllowlist:
		if c.allowUsers == nil {
			return false
		}
		_, ok := c.allowUsers[userID]
		return ok
	default:
		return true
	}
}

// extractText 从消息中提取用户文本，并去除 @BotName 提及。
// 支持的消息类型：
//   - text: 纯文本内容
//   - image: 转为 [image:URL]
//   - voice: 智能机器人返回转写文本；通知机器人返回 [voice:media_id]
//   - video: 转为 [video:media_id]
//   - file: 转为 [file:URL]
//   - location: 转为 [location:name,address,lat,lng]
//   - link: 转为 [link:title,url]
//   - mixed: 文本和图片的组合
//   - stream: 忽略（用于流式继续）
func (c *Channel) extractText(msg WebhookMessage) string {
	var parts []string
	switch msg.MsgType {
	case MsgTypeText:
		parts = append(parts, msg.Text.Content)

	case MsgTypeImage:
		if url := strings.TrimSpace(msg.Image.URL); url != "" {
			parts = append(parts, "[image:"+url+"]")
		}

	case MsgTypeVoice:
		// AI Bot: voice.content contains transcribed text
		// Notification Bot: voice.media_id contains file reference
		if content := strings.TrimSpace(msg.Voice.Content); content != "" {
			parts = append(parts, content) // Use transcribed text directly
		} else if mediaID := strings.TrimSpace(msg.Voice.MediaID); mediaID != "" {
			parts = append(parts, "[voice:"+mediaID+"]")
		}

	case MsgTypeVideo:
		if mediaID := strings.TrimSpace(msg.Video.MediaID); mediaID != "" {
			parts = append(parts, "[video:"+mediaID+"]")
		}

	case MsgTypeFile:
		// AI Bot only: file.url contains encrypted download URL
		if url := strings.TrimSpace(msg.File.URL); url != "" {
			parts = append(parts, "[file:"+url+"]")
		}

	case MsgTypeLocation:
		// Format: [location:名称,地址,纬度,经度]
		loc := msg.Location
		locStr := fmt.Sprintf("[location:%s,%s,%.6f,%.6f]",
			loc.Name, loc.Address, loc.Latitude, loc.Longitude)
		parts = append(parts, locStr)

	case MsgTypeLink:
		// Format: [link:标题,URL]
		link := msg.Link
		if url := strings.TrimSpace(link.URL); url != "" {
			title := link.Title
			if title == "" {
				title = "链接"
			}
			parts = append(parts, fmt.Sprintf("[link:%s,%s]", title, url))
		}

	case MsgTypeMixed:
		for _, item := range msg.MixedMessage.MsgItem {
			switch item.MsgType {
			case MsgTypeText:
				if s := strings.TrimSpace(item.Text.Content); s != "" {
					parts = append(parts, item.Text.Content)
				}
			case MsgTypeImage:
				if url := strings.TrimSpace(item.Image.URL); url != "" {
					parts = append(parts, "[image:"+url+"]")
				}
			case MsgTypeFile:
				if url := strings.TrimSpace(item.File.URL); url != "" {
					parts = append(parts, "[file:"+url+"]")
				}
			}
		}

	case MsgTypeStream:
		// Stream messages are for continuing streaming replies, not user input
		return ""

	default:
		return ""
	}
	appendQuoteAttachmentText(&parts, msg.Quote)

	text := strings.Join(parts, " ")

	// Remove @BotName mention.
	if c.cfg.BotName != "" {
		text = strings.ReplaceAll(text, "@"+c.cfg.BotName, "")
	}
	return strings.TrimSpace(text)
}

func appendQuoteAttachmentText(
	parts *[]string,
	quote *QuoteContent,
) {
	for _, item := range quoteItems(quote) {
		switch item.MsgType {
		case MsgTypeImage:
			if url := strings.TrimSpace(item.Image.URL); url != "" {
				*parts = append(*parts, "[image:"+url+"]")
			}
		case MsgTypeFile:
			if url := strings.TrimSpace(item.File.URL); url != "" {
				*parts = append(*parts, "[file:"+url+"]")
			}
		}
	}
}

func quoteItems(quote *QuoteContent) []MixedMsgItem {
	if quote == nil {
		return nil
	}

	switch quote.MsgType {
	case MsgTypeText:
		return []MixedMsgItem{{
			MsgType: MsgTypeText,
			Text:    quote.Text,
		}}
	case MsgTypeImage:
		return []MixedMsgItem{{
			MsgType: MsgTypeImage,
			Image:   quote.Image,
		}}
	case MsgTypeFile:
		return []MixedMsgItem{{
			MsgType: MsgTypeFile,
			File:    quote.File,
		}}
	case MsgTypeVoice:
		if strings.TrimSpace(quote.Voice.Content) == "" {
			return nil
		}
		return []MixedMsgItem{{
			MsgType: MsgTypeText,
			Text: TextContent{
				Content: quote.Voice.Content,
			},
		}}
	case MsgTypeMixed:
		return quote.Mixed.MsgItem
	default:
		return nil
	}
}

// extractContentParts extracts multimodal content parts from the message.
// This enables Gateway multimodal support (PR #1304).
// When embedImageURL/embedFileURL is true, URLs are embedded in text
// instead of ContentParts, allowing the agent/model to control downloads.
// Returns text content and a slice of gwproto.ContentPart for images, audio, files, etc.
func (c *Channel) extractContentParts(
	msg WebhookMessage,
) (string, []gwproto.ContentPart, []contentPartDecryptHint) {
	var textParts []string
	var contentParts []gwproto.ContentPart
	var decryptHints []contentPartDecryptHint

	appendPart := func(
		part gwproto.ContentPart,
		decryptHint contentPartDecryptHint,
	) {
		contentParts = append(contentParts, part)
		decryptHints = append(decryptHints, decryptHint)
	}

	switch msg.MsgType {
	case MsgTypeText:
		textParts = append(textParts, msg.Text.Content)

	case MsgTypeImage:
		if url := strings.TrimSpace(msg.Image.URL); url != "" {
			if c.embedImageURL {
				// Embed URL in text, let agent decide when to download
				textParts = append(textParts, formatImageURL(url))
			} else {
				// Default: pass via ContentParts for local materialization
				appendPart(gwproto.ContentPart{
					Type: gwproto.PartTypeImage,
					Image: &gwproto.ImagePart{
						URL: url,
					},
				}, contentPartDecryptHint{
					AESKey: msg.Image.AESKey,
				})
			}
		}

	case MsgTypeVoice:
		// AI Bot: voice.content contains transcribed text
		// Notification Bot: voice.media_id contains file reference
		if content := strings.TrimSpace(msg.Voice.Content); content != "" {
			// Use transcribed text directly as text content
			textParts = append(textParts, content)
		}
		// Note: media_id requires WeChat API to download, not directly supported as URL

	case MsgTypeFile:
		// AI Bot only: file.url contains encrypted download URL
		if url := strings.TrimSpace(msg.File.URL); url != "" {
			if c.embedFileURL {
				// Embed URL in text, let agent decide when to download
				textParts = append(textParts, formatFileURL(url))
			} else {
				// Default: pass via ContentParts for local materialization
				appendPart(gwproto.ContentPart{
					Type: gwproto.PartTypeFile,
					File: &gwproto.FilePart{
						URL: url,
					},
				}, contentPartDecryptHint{
					AESKey: msg.File.AESKey,
				})
			}
		}

	case MsgTypeLocation:
		loc := msg.Location
		appendPart(gwproto.ContentPart{
			Type: gwproto.PartTypeLocation,
			Location: &gwproto.LocationPart{
				Latitude:  loc.Latitude,
				Longitude: loc.Longitude,
				Name:      loc.Name,
			},
		}, contentPartDecryptHint{})

	case MsgTypeLink:
		link := msg.Link
		if url := strings.TrimSpace(link.URL); url != "" {
			appendPart(gwproto.ContentPart{
				Type: gwproto.PartTypeLink,
				Link: &gwproto.LinkPart{
					URL:   url,
					Title: link.Title,
				},
			}, contentPartDecryptHint{})
		}

	case MsgTypeMixed:
		for _, item := range msg.MixedMessage.MsgItem {
			switch item.MsgType {
			case MsgTypeText:
				if s := strings.TrimSpace(item.Text.Content); s != "" {
					textParts = append(textParts, item.Text.Content)
				}
			case MsgTypeImage:
				if url := strings.TrimSpace(item.Image.URL); url != "" {
					if c.embedImageURL {
						// Embed URL in text, let agent decide when to download
						textParts = append(textParts, formatImageURL(url))
					} else {
						// Default: pass via ContentParts for local materialization
						appendPart(gwproto.ContentPart{
							Type: gwproto.PartTypeImage,
							Image: &gwproto.ImagePart{
								URL: url,
							},
						}, contentPartDecryptHint{
							AESKey: item.Image.AESKey,
						})
					}
				}
			case MsgTypeFile:
				if url := strings.TrimSpace(item.File.URL); url != "" {
					if c.embedFileURL {
						textParts = append(textParts,
							formatFileURL(url))
					} else {
						appendPart(gwproto.ContentPart{
							Type: gwproto.PartTypeFile,
							File: &gwproto.FilePart{
								URL: url,
							},
						}, contentPartDecryptHint{
							AESKey: item.File.AESKey,
						})
					}
				}
			}
		}
		for _, ref := range unknownMixedMediaRefs(msg) {
			if url := strings.TrimSpace(ref.URL); url != "" {
				appendPart(gwproto.ContentPart{
					Type: gwproto.PartTypeFile,
					File: &gwproto.FilePart{
						URL: url,
					},
				}, contentPartDecryptHint{
					AESKey: ref.AESKey,
				})
			}
		}

	case MsgTypeStream:
		// Stream messages are for continuing streaming replies, not user input
		return "", nil, nil

	default:
		return "", nil, nil
	}
	c.appendQuotedContentParts(
		msg.Quote,
		&textParts,
		appendPart,
	)

	text := strings.Join(textParts, " ")

	// Remove @BotName mention.
	if c.cfg.BotName != "" {
		text = strings.ReplaceAll(text, "@"+c.cfg.BotName, "")
	}
	return strings.TrimSpace(text), contentParts, decryptHints
}

func (c *Channel) appendQuotedContentParts(
	quote *QuoteContent,
	textParts *[]string,
	appendPart func(
		gwproto.ContentPart,
		contentPartDecryptHint,
	),
) {
	for _, item := range quoteItems(quote) {
		switch item.MsgType {
		case MsgTypeImage:
			if url := strings.TrimSpace(item.Image.URL); url != "" {
				if c.embedImageURL {
					*textParts = append(
						*textParts,
						formatImageURL(url),
					)
					continue
				}
				appendPart(gwproto.ContentPart{
					Type: gwproto.PartTypeImage,
					Image: &gwproto.ImagePart{
						URL: url,
					},
				}, contentPartDecryptHint{
					AESKey: item.Image.AESKey,
				})
			}
		case MsgTypeFile:
			if url := strings.TrimSpace(item.File.URL); url != "" {
				if c.embedFileURL {
					*textParts = append(
						*textParts,
						formatFileURL(url),
					)
					continue
				}
				appendPart(gwproto.ContentPart{
					Type: gwproto.PartTypeFile,
					File: &gwproto.FilePart{
						URL: url,
					},
				}, contentPartDecryptHint{
					AESKey: item.File.AESKey,
				})
			}
		}
	}
}

func defaultTextForContentParts(parts []gwproto.ContentPart) string {
	hasImage := false
	hasFile := false
	for _, part := range parts {
		switch part.Type {
		case gwproto.PartTypeImage:
			hasImage = true
		case gwproto.PartTypeFile:
			hasFile = true
		}
	}
	switch {
	case hasImage:
		return defaultImageAnalyzeText
	case hasFile:
		return defaultFileAnalyzeText
	case len(parts) > 0:
		return defaultMediaAnalyzeText
	default:
		return ""
	}
}

func (c *Channel) runtimePersonaNote(sessionID string) string {
	if c == nil || c.sessionTracker == nil {
		return ""
	}

	baseSessionID := baseSessionIDForSession(sessionID)
	info := c.sessionTracker.getSession(baseSessionID)
	if info == nil {
		return ""
	}
	return c.personaOverridePrompt(info.effectivePersonaID())
}

func (c *Channel) personaOverridePrompt(personaID string) string {
	personaID = strings.TrimSpace(personaID)
	if personaID == "" || personaapi.IsDefault(personaID) {
		return ""
	}
	if c == nil || c.personas == nil {
		return ""
	}
	def, ok, err := c.personas.Get(personaID)
	if err != nil || !ok {
		return ""
	}
	if strings.TrimSpace(def.Prompt) == "" {
		return ""
	}
	return strings.TrimSpace(
		runtimePersonaOverridePromptHeader + def.ID + ".\n" +
			def.Prompt + "\n" +
			runtimePersonaPrimaryStyleRule + "\n" +
			runtimePersonaHistoryOverrideRule + "\n" +
			runtimePersonaConsistencyRule,
	)
}

func (c *Channel) buildRuntimeRequestSystemPrompt(
	ctx context.Context,
	sessionID string,
	chatID string,
	text string,
	profile replyUXProfile,
	identityLabels map[string]string,
) string {
	if c == nil {
		return ""
	}
	template := strings.TrimSpace(
		c.requestSystemPromptTemplateValue(),
	)
	if template == "" {
		return c.buildLegacyRuntimeRequestSystemPrompt(
			ctx,
			sessionID,
			chatID,
			text,
			profile,
			identityLabels,
		)
	}
	vars := c.buildRuntimeRequestPromptVars(
		ctx,
		sessionID,
		chatID,
		text,
		profile,
		identityLabels,
	)
	rendered, err := promptasset.Render(
		template,
		vars,
	)
	if err != nil {
		log.Warnf(
			"wecom: render request system prompt: %v",
			err,
		)
		return c.buildLegacyRuntimeRequestSystemPrompt(
			ctx,
			sessionID,
			chatID,
			text,
			profile,
			identityLabels,
		)
	}
	return rendered
}

func (c *Channel) requestSystemPromptTemplateValue() string {
	if c == nil {
		return ""
	}
	c.requestSystemPromptMu.RLock()
	defer c.requestSystemPromptMu.RUnlock()
	return c.requestSystemPromptTemplate
}

func (c *Channel) SetRequestSystemPromptTemplate(
	template string,
) {
	if c == nil {
		return
	}
	c.requestSystemPromptMu.Lock()
	c.requestSystemPromptTemplate = strings.TrimSpace(template)
	c.requestSystemPromptMu.Unlock()
}

func (c *Channel) buildRuntimeRequestPromptVars(
	ctx context.Context,
	sessionID string,
	chatID string,
	_ string,
	profile replyUXProfile,
	identityLabels map[string]string,
) map[string]string {
	cronDeliveryNote := ""
	if c.botMode == botModeAI &&
		c.connectionMode == connectionModeWebSocket {
		cronDeliveryNote = buildWeComCronDeliveryPromptNote(chatID)
	}
	replyUXNotes := buildReplyUXPromptNotes(profile)
	browserEnvNote := runtimehint.BrowserPromptLineFromEnv()
	browserDoctorNote := c.browserDoctorPromptNote(ctx)
	externalLookupNote := externalLookupPromptNote()

	vars := map[string]string{
		requestPromptVarRuntimePersonaNote: "",
		requestPromptVarIdentityNote:       "",
		requestPromptVarAssistantAliasNote: "",
		requestPromptVarWorkspaceNote:      "",
		requestPromptVarCurrentTimeNote:    "",
		requestPromptVarCronAuthoringNote:  "",
		requestPromptVarAIBotNotes:         "",
		requestPromptVarReplyUXNotes:       "",
		requestPromptVarBrowserEnvNote:     "",
		requestPromptVarBrowserDoctorNote:  "",
		requestPromptVarExternalLookupNote: "",
		requestPromptVarCronDeliveryNote:   "",
		requestPromptVarTurnContextNotes:   "",
		requestPromptVarRuntimeRules:       "",
		requestPromptVarChannelNotes:       "",
		requestPromptVarBrowserNotes:       "",
	}
	vars[requestPromptVarRuntimePersonaNote] =
		c.runtimePersonaNote(sessionID)
	vars[requestPromptVarIdentityNote] =
		c.identityPromptNote(ctx, sessionID, identityLabels)
	vars[requestPromptVarAssistantAliasNote] =
		c.assistantAliasNote(sessionID)
	vars[requestPromptVarWorkspaceNote] =
		c.codingWorkspaceNote(sessionID)
	vars[requestPromptVarCurrentTimeNote] =
		buildCurrentTimePromptNote(time.Now())
	vars[requestPromptVarCronAuthoringNote] =
		runtimeCronAuthoringNote
	vars[requestPromptVarAIBotNotes] = c.aiBotPromptNotes()
	vars[requestPromptVarReplyUXNotes] = replyUXNotes
	vars[requestPromptVarBrowserEnvNote] = browserEnvNote
	vars[requestPromptVarBrowserDoctorNote] = browserDoctorNote
	vars[requestPromptVarExternalLookupNote] = externalLookupNote
	vars[requestPromptVarCronDeliveryNote] = cronDeliveryNote
	vars[requestPromptVarTurnContextNotes] = buildPromptNoteGroup(
		vars[requestPromptVarRuntimePersonaNote],
		vars[requestPromptVarIdentityNote],
		vars[requestPromptVarAssistantAliasNote],
		vars[requestPromptVarWorkspaceNote],
	)
	vars[requestPromptVarRuntimeRules] = buildPromptNoteGroup(
		runtimeAssistantNameToolPromptRule,
		vars[requestPromptVarCurrentTimeNote],
		vars[requestPromptVarCronAuthoringNote],
	)
	vars[requestPromptVarChannelNotes] = buildPromptNoteGroup(
		vars[requestPromptVarAIBotNotes],
		vars[requestPromptVarReplyUXNotes],
		vars[requestPromptVarCronDeliveryNote],
	)
	vars[requestPromptVarBrowserNotes] = buildPromptNoteGroup(
		vars[requestPromptVarBrowserEnvNote],
		vars[requestPromptVarBrowserDoctorNote],
	)
	return vars
}

func buildPromptNoteGroup(notes ...string) string {
	group := make([]string, 0, len(notes))
	for _, note := range notes {
		group = appendPromptNote(group, note)
	}
	return strings.Join(group, runtimePromptNoteSeparator)
}

func requestSystemPromptStructureVars() map[string]string {
	return map[string]string{
		requestPromptVarRuntimePersonaNote: requestPromptStructurePersona,
		requestPromptVarIdentityNote:       requestPromptStructureIdentity,
		requestPromptVarAssistantAliasNote: requestPromptStructureAssistantAlias,
		requestPromptVarWorkspaceNote:      requestPromptStructureWorkspace,
		requestPromptVarCurrentTimeNote:    requestPromptStructureCurrentTime,
		requestPromptVarCronAuthoringNote: "" +
			requestPromptStructureCronAuthoring,
		requestPromptVarAIBotNotes: requestPromptStructureAIBot,
		requestPromptVarReplyUXNotes: "" +
			requestPromptStructureReplyUX,
		requestPromptVarBrowserEnvNote:    requestPromptStructureBrowserEnv,
		requestPromptVarBrowserDoctorNote: requestPromptStructureBrowserDoctor,
		requestPromptVarExternalLookupNote: "" +
			requestPromptStructureExternalLookup,
		requestPromptVarCronDeliveryNote: requestPromptStructureCronDelivery,
		requestPromptVarTurnContextNotes: requestPromptStructureTurnContext,
		requestPromptVarRuntimeRules:     requestPromptStructureRuntimeRules,
		requestPromptVarChannelNotes:     requestPromptStructureChannelNotes,
		requestPromptVarBrowserNotes:     requestPromptStructureBrowserNotes,
	}
}

func RenderRequestSystemPromptStructure(template string) string {
	template = strings.TrimSpace(template)
	if template == "" {
		return ""
	}
	rendered, err := promptasset.Render(
		template,
		requestSystemPromptStructureVars(),
	)
	if err != nil {
		return template
	}
	return rendered
}

func (c *Channel) buildLegacyRuntimeRequestSystemPrompt(
	ctx context.Context,
	sessionID string,
	chatID string,
	_ string,
	profile replyUXProfile,
	identityLabels map[string]string,
) string {
	notes := make([]string, 0, 6)
	notes = appendPromptNote(notes, c.runtimePersonaNote(sessionID))
	notes = appendPromptNote(
		notes,
		c.identityPromptNote(ctx, sessionID, identityLabels),
	)
	notes = appendPromptNote(
		notes,
		c.assistantAliasNote(sessionID),
	)
	notes = appendPromptNote(
		notes,
		c.codingWorkspaceNote(sessionID),
	)
	notes = appendPromptNote(
		notes,
		runtimeAssistantNameToolPromptRule,
	)
	notes = appendPromptNote(
		notes,
		buildCurrentTimePromptNote(time.Now()),
	)
	notes = appendPromptNote(
		notes,
		runtimeCronAuthoringNote,
	)
	notes = appendPromptNote(notes, c.aiBotPromptNotes())
	notes = appendPromptNote(
		notes,
		buildReplyUXPromptNotes(profile),
	)
	notes = appendPromptNote(
		notes,
		runtimehint.BrowserPromptLineFromEnv(),
	)
	notes = appendPromptNote(
		notes,
		c.browserDoctorPromptNote(ctx),
	)
	notes = appendPromptNote(
		notes,
		externalLookupPromptNote(),
	)
	if c.botMode == botModeAI &&
		c.connectionMode == connectionModeWebSocket {
		notes = appendPromptNote(
			notes,
			buildWeComCronDeliveryPromptNote(chatID),
		)
	}
	return strings.Join(notes, runtimePromptNoteSeparator)
}

func buildCurrentTimePromptNote(now time.Time) string {
	zoneName, _ := now.Zone()
	return currentTimeNotePrefix +
		now.Format(time.RFC3339) +
		" (UTC offset: " + now.Format(currentTimeOffsetLayout) +
		"; zone label: " + zoneName + ")." +
		currentTimeSourceRule +
		currentTimeScheduleRule
}

func (c *Channel) browserDoctorPromptNote(ctx context.Context) string {
	helperPath := strings.TrimSpace(
		os.Getenv(runtimehint.BrowserRuntimeEnvName),
	)
	if helperPath == "" {
		return ""
	}
	if note := c.cachedBrowserDoctorPromptNote(); note != "" {
		return note
	}
	note := buildBrowserDoctorPromptNote(ctx, helperPath)
	c.storeBrowserDoctorPromptNote(note)
	return note
}

func (c *Channel) cachedBrowserDoctorPromptNote() string {
	if c == nil {
		return ""
	}
	c.browserDoctorNoteMu.Lock()
	defer c.browserDoctorNoteMu.Unlock()
	if time.Since(c.browserDoctorNoteAt) >
		browserDoctorPromptCacheTTL {
		return ""
	}
	return c.browserDoctorNote
}

func (c *Channel) storeBrowserDoctorPromptNote(note string) {
	if c == nil {
		return
	}
	c.browserDoctorNoteMu.Lock()
	defer c.browserDoctorNoteMu.Unlock()
	c.browserDoctorNote = strings.TrimSpace(note)
	c.browserDoctorNoteAt = time.Now()
}

func buildBrowserDoctorPromptNote(
	ctx context.Context,
	helperPath string,
) string {
	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}
	timeoutCtx, cancel := context.WithTimeout(
		runCtx,
		browserDoctorPromptTimeout,
	)
	defer cancel()

	cmd := exec.CommandContext(
		timeoutCtx,
		helperPath,
		browserDoctorCommandName,
	)
	output, err := cmd.CombinedOutput()
	note := runtimehint.BrowserDoctorPromptLineFromOutput(
		string(output),
	)
	if note != "" {
		return note
	}
	if err == nil {
		return ""
	}
	detail := strings.TrimSpace(
		condenseBrowserDoctorOutput(string(output)),
	)
	if detail == "" {
		detail = strings.TrimSpace(err.Error())
	}
	if detail == "" {
		return ""
	}
	return browserDoctorFailurePrefix + detail + ". " +
		browserDoctorFailureRule
}

func condenseBrowserDoctorOutput(output string) string {
	fields := strings.Fields(strings.TrimSpace(output))
	if len(fields) == 0 {
		return ""
	}
	return strings.Join(fields, " ")
}

func (c *Channel) resolveChatIdentityLabels(
	ctx context.Context,
	sessionInfo *sessionInfo,
	msg WebhookMessage,
) map[string]string {
	if c == nil || c.identityResolver == nil {
		return nil
	}
	if strings.TrimSpace(c.userLabelMode) == userLabelModeID {
		return nil
	}

	userIDs := collectKnownUserIDs(msg)
	if sessionInfo != nil {
		userIDs = append(userIDs, sessionInfo.knownUserIDs...)
	}
	if c.sessionTracker != nil {
		baseSessionID := buildSessionID(
			msg.ChatID,
			messageUserID(msg),
		)
		if sessionInfo != nil &&
			strings.TrimSpace(
				sessionInfo.baseSessionID,
			) != "" {
			baseSessionID = sessionInfo.baseSessionID
		}
		userIDs = append(
			userIDs,
			c.sessionTracker.knownUserIDsForSession(
				baseSessionID,
			)...,
		)
	}
	profiles := c.identityResolver.ResolveUsers(
		ctx,
		sanitizeKnownUserIDs(userIDs),
	)
	if len(profiles) == 0 {
		return nil
	}
	return resolvedIdentityLabels(c.userLabelMode, profiles)
}

func (c *Channel) identityPromptNote(
	_ context.Context,
	_ string,
	identityLabels map[string]string,
) string {
	if len(identityLabels) == 0 {
		return ""
	}
	return buildIdentityPromptNote(identityLabels)
}

func appendPromptNote(notes []string, note string) []string {
	note = strings.TrimSpace(note)
	if note == "" {
		return notes
	}
	for _, existing := range notes {
		if existing == note {
			return notes
		}
	}
	return append(notes, note)
}

func appendPromptNoteText(base string, note string) string {
	notes := make([]string, 0, 2)
	notes = appendPromptNote(notes, base)
	notes = appendPromptNote(notes, note)
	return strings.Join(notes, runtimePromptNoteSeparator)
}

func buildWeComCronDeliveryPromptNote(
	chatID string,
) string {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return ""
	}

	currentTarget := buildPushTarget(
		pushTargetKindGroup,
		chatID,
	)
	parts := []string{
		fmt.Sprintf(
			runtimeCronDeliveryNotePrefix,
			currentTarget,
		),
		runtimeCronDeliveryIdentityRule,
		runtimeCronDeliveryProtocolRule,
	}
	return strings.Join(parts, "")
}

func buildSpeakerScopedMemoryPromptNote(
	actorID string,
	actorLabel string,
) string {
	actorID = strings.TrimSpace(actorID)
	actorLabel = strings.TrimSpace(actorLabel)
	if actorID == "" && actorLabel == "" {
		return ""
	}

	currentSpeaker := actorID
	if actorLabel != "" {
		currentSpeaker = actorLabel
		if actorID != "" && actorID != actorLabel {
			currentSpeaker += " (user_id=" + actorID + ")"
		}
	}

	exampleTarget := actorID
	if actorLabel != "" {
		exampleTarget = actorLabel
	}
	if exampleTarget == "" {
		return ""
	}

	parts := []string{
		fmt.Sprintf(
			runtimeSpeakerScopedMemoryNotePrefix,
			currentSpeaker,
		),
		runtimeSpeakerScopedMemoryRule,
		fmt.Sprintf(
			runtimeSpeakerScopedMemoryWriteRule,
			buildSpeakerScopedMemoryExample(actorID, exampleTarget),
		),
		runtimeSpeakerScopedMemoryReadRule,
	}
	return strings.Join(parts, "")
}

func buildSpeakerScopedMemoryExample(
	actorID string,
	actorLabel string,
) string {
	actorID = strings.TrimSpace(actorID)
	actorLabel = strings.TrimSpace(actorLabel)
	if actorID != "" {
		return "- [speaker:" + actorID + "] reply in classical Chinese."
	}
	if actorLabel != "" {
		return "- [speaker:" + actorLabel + "] reply in classical Chinese."
	}
	return "- [speaker:current-user] reply in classical Chinese."
}

func (c *Channel) codingWorkspaceNote(
	sessionID string,
) string {
	if c == nil || c.sessionTracker == nil {
		return ""
	}

	baseSessionID := baseSessionIDForSession(sessionID)
	info := c.sessionTracker.getSession(baseSessionID)
	if info == nil {
		return ""
	}

	if strings.TrimSpace(info.workspacePath) == "" {
		return ""
	}

	return buildCodingWorkspaceNote(
		info.workspacePath,
		c.defaultCodingWorkspace,
		c.codingScratchRoot,
	)
}

const (
	assistantNameSourceChat     = "当前聊天名字"
	assistantNameSourceChatSame = "当前聊天名字" +
		"（与默认名字一致）"
	assistantNameSourceGlobal  = "默认名字"
	assistantNameSourceBotName = "默认名字（来自 wecom bot_name 回退）"
	assistantNameSourceRuntime = "默认名字（回退到 trpc-claw）"
)

type assistantNameState struct {
	EffectiveName string
	ChatOverride  string
	GlobalName    string
	SourceLabel   string
}

func (c *Channel) globalAssistantNameState() assistantNameState {
	if c == nil {
		return assistantNameState{
			EffectiveName: defaultAssistantDisplayName,
			GlobalName:    defaultAssistantDisplayName,
			SourceLabel:   assistantNameSourceRuntime,
		}
	}

	if name := c.globalAssistantName(); name != "" {
		return assistantNameState{
			EffectiveName: name,
			GlobalName:    name,
			SourceLabel:   assistantNameSourceGlobal,
		}
	}

	if strings.TrimSpace(c.cfg.BotName) != "" {
		name := resolveAssistantDisplayName(c.cfg.BotName)
		return assistantNameState{
			EffectiveName: name,
			GlobalName:    name,
			SourceLabel:   assistantNameSourceBotName,
		}
	}

	return assistantNameState{
		EffectiveName: defaultAssistantDisplayName,
		GlobalName:    defaultAssistantDisplayName,
		SourceLabel:   assistantNameSourceRuntime,
	}
}

func (c *Channel) assistantNameStateForInfo(
	info *sessionInfo,
) assistantNameState {
	state := c.globalAssistantNameState()
	if info == nil {
		return state
	}

	override := normalizeAssistantAlias(info.assistantAlias)
	if override == "" {
		return state
	}

	state.ChatOverride = override
	state.EffectiveName = override
	state.SourceLabel = assistantNameSourceChat
	if override == state.GlobalName {
		state.SourceLabel = assistantNameSourceChatSame
	}
	return state
}

func (c *Channel) assistantDisplayName() string {
	return c.globalAssistantNameState().EffectiveName
}

func (c *Channel) assistantDisplayNameForInfo(
	info *sessionInfo,
) string {
	return c.assistantNameStateForInfo(info).EffectiveName
}

func (c *Channel) assistantDisplayNameForSession(
	sessionID string,
) string {
	if c == nil || c.sessionTracker == nil {
		return c.assistantDisplayName()
	}

	baseSessionID := baseSessionIDForSession(sessionID)
	info := c.sessionTracker.getSession(baseSessionID)
	return c.assistantDisplayNameForInfo(info)
}

func (c *Channel) assistantAliasNote(sessionID string) string {
	if c == nil || c.sessionTracker == nil {
		return ""
	}

	baseSessionID := baseSessionIDForSession(sessionID)
	info := c.sessionTracker.getSession(baseSessionID)
	alias := ""
	if info != nil {
		alias = normalizeAssistantAlias(info.assistantAlias)
	}
	if alias == "" {
		return ""
	}
	if alias == c.globalAssistantNameState().GlobalName {
		return ""
	}
	return fmt.Sprintf(
		runtimeAssistantAliasNoteTemplate,
		alias,
	)
}

func (c *Channel) globalAssistantName() string {
	if c == nil {
		return ""
	}
	name, err := assistantname.ReadFile(c.assistantIdentityFile)
	if err != nil {
		log.Warnf("wecom: read assistant identity failed: %v", err)
		return ""
	}
	return name
}

func (c *Channel) saveGlobalAssistantName(name string) error {
	if c == nil {
		return nil
	}
	path := strings.TrimSpace(c.assistantIdentityFile)
	if path == "" {
		return fmt.Errorf(
			"wecom: global assistant name requires state_dir",
		)
	}
	return assistantname.WriteFile(path, name)
}

func (c *Channel) personaStorageEnabled() bool {
	if c == nil || c.personas == nil {
		return false
	}
	return strings.TrimSpace(c.personas.Dir()) != ""
}

func (c *Channel) showReplyContextPrefixEnabled() bool {
	if c == nil {
		return false
	}
	return resolveReplyPrefixEnabled(c.cfg.ReplyPrefix)
}

func (c *Channel) replyContextPrefix(
	sessionID string,
) string {
	if c == nil || !c.showReplyContextPrefixEnabled() {
		return ""
	}
	lines := c.replyPrefixLines(sessionID)
	return strings.Join(lines, "\n")
}

func baseSessionIDForSession(sessionID string) string {
	sessionID = canonicalWeComSessionID(sessionID)
	if sessionID == "" {
		return ""
	}

	lastColon := strings.LastIndex(sessionID, ":")
	if lastColon <= 0 || lastColon >= len(sessionID)-1 {
		return sessionID
	}
	if _, err := strconv.ParseInt(
		sessionID[lastColon+1:],
		10,
		64,
	); err != nil {
		return sessionID
	}
	return sessionID[:lastColon]
}

func canonicalWeComSessionID(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	for strings.HasPrefix(
		sessionID,
		wecomThreadSessionPrefix,
	) {
		sessionID = strings.TrimPrefix(
			sessionID,
			wecomThreadSessionPrefix,
		)
	}
	return sessionID
}

func (c *Channel) aiBotPromptNotes() string {
	if c == nil || c.botMode != botModeAI {
		return ""
	}
	notes := make([]string, 0, 2)
	notes = appendPromptNote(notes, c.aibotTransportNote())
	notes = appendPromptNote(notes, c.aibotDeliveryNote())
	return strings.Join(notes, runtimePromptNoteSeparator)
}

func (c *Channel) aibotTransportNote() string {
	if c != nil &&
		c.botMode == botModeAI &&
		c.connectionMode == connectionModeWebSocket {
		return wecomAIBotWebSocketTransportNote
	}
	return wecomAIBotWebhookTransportNote
}

func (c *Channel) aibotDeliveryNote() string {
	if c != nil &&
		c.botMode == botModeAI &&
		c.connectionMode == connectionModeWebSocket {
		return wecomAIBotWebSocketSendBackNote
	}
	return wecomAIBotWebhookSendBackNote
}

func currentTurnAttachmentNote(attachmentCount int) string {
	if attachmentCount <= 0 {
		return ""
	}
	return fmt.Sprintf(
		currentTurnAttachmentNoteTemplate,
		attachmentCount,
	)
}

// callGatewayAndReply calls the gateway and sends the reply back to WeChat.
// Supports Gateway multimodal protocol (PR #1304) for images, files,
// locations, and links.
// sessionID is the active session identifier for the current chat or
// DM, and may be either the stable base session ID or a derived split
// session ID.
func (c *Channel) callGatewayAndReply(
	ctx context.Context,
	msg WebhookMessage,
	text string,
	contentParts []gwproto.ContentPart,
	decryptHints []contentPartDecryptHint,
	fromID string,
	requestID string,
	sessionID string,
	sender messageSender,
) error {
	return c.callGatewayAndReplyWithState(
		ctx,
		msg,
		text,
		contentParts,
		decryptHints,
		fromID,
		requestID,
		sessionID,
		c.sessionTracker.getSession(
			baseSessionIDForSession(sessionID),
		),
		sender,
		nil,
	)
}

func (c *Channel) callGatewayAndReplyWithState(
	ctx context.Context,
	msg WebhookMessage,
	text string,
	contentParts []gwproto.ContentPart,
	decryptHints []contentPartDecryptHint,
	fromID string,
	requestID string,
	sessionID string,
	sessionInfo *sessionInfo,
	sender messageSender,
	replyState *replyStreamState,
) (retErr error) {
	if replyState == nil {
		replyState = newReplyStreamState(
			requestID,
			msg.MsgID,
			sender,
			c.cfg.EnableStream,
		)
	}
	if replyState != nil {
		replyState.nativeThinking = c.usesNativeThinkingStream()
	}
	baseSessionID := baseSessionIDForSession(sessionID)
	defer c.syncActiveSessionCard(
		ctx,
		baseSessionID,
		nil,
		sender,
	)
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	if handled := c.handleRuntimeAbortDuringRun(
		runCtx,
		msg,
		sender,
		replyState,
		sessionID,
		requestID,
	); handled {
		return nil
	}

	// In AI bot mode, response_url can only be used ONCE.
	// Skip the "thinking" hint to preserve it for the actual reply.
	if c.botMode != botModeAI {
		_ = sender.SendText(ctx, msg.ChatID, c.processingMessage)
	} else {
		log.InfofContext(
			ctx,
			"wecom: AI bot mode - skipping thinking hint "+
				"(response_url is one-shot)",
		)
	}

	c.refreshReplyDisplayPrefix(sessionID, replyState)

	initialHint := c.sendInitialReplyHint(
		ctx,
		msg,
		sender,
		contentParts,
		replyState,
	)
	c.runStatus.start(sessionID, requestID, initialHint)

	if len(contentParts) > 0 {
		resolvedParts, err := c.materializeContentParts(
			ctx,
			contentParts,
			decryptHints,
		)
		if err != nil {
			log.WarnfContext(ctx,
				"wecom: materialize content parts failed: %v",
				err)
			c.runStatus.fail(
				sessionID,
				requestID,
				defaultAttachmentReadFailedMessage,
				"",
			)
			if c.finishReplyHintOnError(
				ctx,
				msg,
				sender,
				replyState,
				defaultAttachmentReadFailedMessage,
			) {
				return nil
			}
			_ = sender.SendMarkdown(
				ctx,
				msg.ChatID,
				defaultAttachmentReadFailedMessage,
			)
			return nil
		}
		contentParts = resolvedParts
	}
	if strings.TrimSpace(text) == "" {
		text = defaultTextForContentParts(contentParts)
	}
	uxProfile := buildReplyUXProfile(contentParts)
	if replyState != nil {
		replyState.rewrite = func(content string) string {
			return sanitizeReplyModelOutput(
				c.runtimeModelName,
				stripReplyFileMarkers(
					rewriteReplyContentWithProfile(
						uxProfile,
						content,
					),
				),
			)
		}
	}
	if followupHint := preGatewayReplyHintContent(uxProfile); followupHint != "" &&
		followupHint != strings.TrimSpace(initialHint) {
		c.runStatus.progress(
			sessionID,
			requestID,
			streamStagePreparing,
			followupHint,
			0,
		)
		c.sendReplyHint(
			ctx,
			msg,
			sender,
			replyState,
			followupHint,
			false,
		)
	}
	identityLabels := c.resolveChatIdentityLabels(
		runCtx,
		sessionInfo,
		msg,
	)
	text = canonicalizeResolvedParticipantMentions(
		text,
		identityLabels,
	)
	actorLabel := resolvedMessageUserLabel(
		msg,
		c.userLabelMode,
		identityLabels,
	)
	requestSystemPrompt := c.buildRuntimeRequestSystemPrompt(
		runCtx,
		sessionID,
		msg.ChatID,
		text,
		uxProfile,
		identityLabels,
	)
	if msg.ChatID != "" &&
		c.groupSessionMode == groupSessionModeShared {
		requestSystemPrompt = appendPromptNoteText(
			requestSystemPrompt,
			buildSpeakerScopedMemoryPromptNote(
				fromID,
				actorLabel,
			),
		)
	}

	// Build gateway request with multimodal support.
	// SessionID uses the active session ID so explicit /new or timeout-based
	// splits still isolate history correctly. StorageUserID keeps persisted
	// state on the canonical transport scope even when the runtime-facing
	// user ID can use a resolved account label for observability.
	runUserID := buildGatewayUserID(
		fromID,
		msg.From.Alias,
		identityLabels,
	)
	storageUserID := buildScopedSessionID(
		msg.ChatID,
		fromID,
		c.groupSessionMode,
	)
	reqExtensions, err := conversation.MergeRequestExtension(
		nil,
		conversation.Annotation{
			HistoryMode: func() string {
				if msg.ChatID != "" &&
					c.groupSessionMode ==
						groupSessionModeShared {
					return conversation.HistoryModeShared
				}
				return ""
			}(),
			StorageUserID: storageUserID,
			ActorID:       fromID,
			ActorLabel:    actorLabel,
			ActorLabels:   identityLabels,
			QuoteText:     quoteTextPreview(msg.Quote),
		},
	)
	if err != nil {
		return fmt.Errorf(
			"wecom: encode conversation metadata: %w",
			err,
		)
	}
	if c.botMode == botModeAI &&
		c.connectionMode == connectionModeWebSocket {
		target, ok := buildDefaultDeliveryTarget(
			msg.ChatID,
			fromID,
		)
		if ok {
			reqExtensions, err = delivery.MergeRequestExtension(
				reqExtensions,
				target,
			)
			if err != nil {
				return fmt.Errorf(
					"wecom: encode delivery target: %w",
					err,
				)
			}
		}
	}
	gwReq := gwclient.MessageRequest{
		Channel:             pluginType,
		From:                fromID,
		Thread:              sessionID,
		MessageID:           msg.MsgID,
		Text:                text,
		RequestSystemPrompt: requestSystemPrompt,
		UserID:              runUserID,
		SessionID:           sessionID,
		RequestID:           requestID,
		Extensions:          reqExtensions,
	}

	// Add content parts if any (multimodal support)
	if len(contentParts) > 0 {
		gwReq.ContentParts = contentParts
		log.InfofContext(ctx, "wecom: sending multimodal request with %d content parts", len(contentParts))
	}

	traceCtx, traceSpan := startGatewayTraceSpan(
		runCtx,
		gwReq,
		resolveGatewayTraceIdentity(
			actorLabel,
			msg.ChatID,
			msg.ChatType,
			c.appName,
			c.cfg.BotName,
			c.name,
		),
	)
	runCtx = traceCtx
	traceOutput := ""
	defer func() {
		finishGatewayTraceSpan(traceSpan, traceOutput, retErr)
	}()

	if c.cfg.EnableStream {
		streamed, err := c.streamGatewayReply(
			runCtx,
			msg,
			gwReq,
			sender,
			replyState,
		)
		if streamed {
			traceOutput = streamPreviewText(replyState)
			if err != nil {
				if handled := c.handleRuntimeAbortDuringRun(
					runCtx,
					msg,
					sender,
					replyState,
					sessionID,
					requestID,
				); handled {
					return nil
				}
				if shouldAbortGatewayRunAfterReplyFailure(err) {
					c.abortGatewayRun(
						ctx,
						requestID,
						cancelRun,
					)
				}
			}
			return err
		}
	}

	rsp, err := c.gw.SendMessage(runCtx, gwReq)
	if err != nil {
		if handled := c.handleRuntimeAbortDuringRun(
			runCtx,
			msg,
			sender,
			replyState,
			sessionID,
			requestID,
		); handled {
			return nil
		}
		rawErrMsg := ""
		if rsp.Error != nil {
			rawErrMsg = strings.TrimSpace(rsp.Error.Message)
		}
		if rawErrMsg == "" {
			rawErrMsg = err.Error()
		}
		errMsg := sanitizeGatewayErrorMessage(
			rawErrMsg,
			requestID,
		)
		logGatewayFailure(
			ctx,
			"gateway send",
			requestID,
			rawErrMsg,
			err,
		)
		if c.finishReplyHintOnError(
			ctx,
			msg,
			sender,
			replyState,
			errMsg,
		) {
			c.runStatus.fail(
				sessionID,
				requestID,
				errMsg,
				"",
			)
			return err
		}
		c.runStatus.fail(
			sessionID,
			requestID,
			errMsg,
			"",
		)
		_ = sender.SendMarkdown(ctx, msg.ChatID, errMsg)

		if rsp.StatusCode >= http.StatusBadRequest &&
			rsp.StatusCode < http.StatusInternalServerError {
			return nil
		}

		// Propagate 5xx errors upward (same as Telegram).
		return err
	}

	if rsp.Ignored {
		traceOutput = defaultIgnoredStatusSummary
		c.runStatus.finish(
			sessionID,
			requestID,
			defaultIgnoredStatusSummary,
			"",
		)
		if c.finishReplyHint(
			ctx,
			msg,
			sender,
			replyState,
			defaultIgnoredStatusSummary,
		) {
			return nil
		}
		c.closeReplyHintIfNeeded(
			ctx,
			msg,
			sender,
			replyState,
		)
		return nil
	}
	c.recordContextUsage(sessionID, requestID, rsp.Usage)
	replyDisplayPrefix := c.refreshReplyDisplayPrefix(
		sessionID,
		replyState,
	)

	deliveryPlan := c.buildReplyDeliveryPlan(
		sessionID,
		rsp.Reply,
	)
	deliveryOutcome := c.sendReplyDeliveryFiles(
		ctx,
		sender,
		msg.ChatID,
		deliveryPlan,
		c.replyDeliveryProgressCallback(
			ctx,
			msg,
			sender,
			replyState,
			sessionID,
			requestID,
		),
	)
	reply := finalizeReplyDeliveryText(
		deliveryPlan.cleanReply,
		deliveryOutcome,
	)
	if reply == "" {
		traceOutput = defaultEmptyReplyStatusSummary
		c.runStatus.finish(
			sessionID,
			requestID,
			defaultEmptyReplyStatusSummary,
			"",
		)
		c.closeReplyHintIfNeeded(
			ctx,
			msg,
			sender,
			replyState,
		)
		return nil
	}
	if c.finishReplyHint(
		ctx,
		msg,
		sender,
		replyState,
		reply,
	) {
		c.runStatus.finish(
			sessionID,
			requestID,
			defaultCompletedStatusSummary,
			reply,
		)
		traceOutput = reply
		return nil
	}
	if replyState != nil {
		reply = rewriteReplyContent(replyState, reply)
	} else {
		reply = applyReplyDisplayPrefix(
			replyDisplayPrefix,
			sanitizeReplyModelOutput(
				c.runtimeModelName,
				rewriteReplyContentWithProfile(
					uxProfile,
					stripReplyFileMarkers(reply),
				),
			),
		)
	}

	c.runStatus.finish(
		sessionID,
		requestID,
		defaultCompletedStatusSummary,
		reply,
	)
	traceOutput = reply
	return sendMarkdownReply(
		ctx,
		sender,
		msg.ChatID,
		reply,
	)
}

func (c *Channel) abortGatewayRun(
	ctx context.Context,
	requestID string,
	cancel context.CancelFunc,
) {
	if cancel != nil {
		cancel()
	}
	requestID = strings.TrimSpace(requestID)
	if c == nil || c.gw == nil || requestID == "" {
		return
	}

	cancelCtx, stop := context.WithTimeout(
		context.Background(),
		gatewayCancelTimeout,
	)
	defer stop()

	canceled, err := c.gw.Cancel(cancelCtx, requestID)
	if err != nil {
		log.WarnfContext(
			ctx,
			"wecom: cancel gateway run request_id=%s: %v",
			requestID,
			err,
		)
		return
	}
	if canceled {
		log.InfofContext(
			ctx,
			"wecom: canceled gateway run request_id=%s "+
				"after reply delivery failure",
			requestID,
		)
	}
}

func shouldAbortGatewayRunAfterReplyFailure(
	err error,
) bool {
	return !isRecoverableReplyDeliveryError(err)
}

func newReplyStreamState(
	requestID string,
	msgID string,
	sender messageSender,
	enableStream bool,
) *replyStreamState {
	if !enableStream {
		return nil
	}
	if _, ok := sender.(streamingSender); !ok {
		return nil
	}
	return &replyStreamState{
		id:       buildReplyStreamID(),
		sender:   sender,
		progress: progress.NewState(),
		feedbackID: buildStreamFeedbackID(
			requestID,
			msgID,
		),
	}
}

func sendMarkdownReply(
	ctx context.Context,
	sender messageSender,
	chatID string,
	content string,
) error {
	for _, part := range splitReplyText(content) {
		if err := sender.SendMarkdown(ctx, chatID, part); err != nil {
			return err
		}
	}
	return nil
}

func countTurnAttachments(parts []gwproto.ContentPart) int {
	total := 0
	for _, part := range parts {
		switch part.Type {
		case gwproto.PartTypeImage, gwproto.PartTypeFile:
			total++
		}
	}
	return total
}

func (c *Channel) sendQueuedReplyHint(
	ctx context.Context,
	msg WebhookMessage,
	sender messageSender,
	state *replyStreamState,
) {
	if c.shouldSuppressQueuedReplyHint() {
		return
	}
	c.sendReplyHint(
		ctx,
		msg,
		sender,
		state,
		defaultQueuedMessage,
		false,
	)
}

func (c *Channel) shouldSuppressQueuedReplyHint() bool {
	return c != nil &&
		c.botMode == botModeAI &&
		c.connectionMode == connectionModeWebSocket
}

func (c *Channel) sendInitialReplyHint(
	ctx context.Context,
	msg WebhookMessage,
	sender messageSender,
	contentParts []gwproto.ContentPart,
	state *replyStreamState,
) string {
	content := initialReplyHintContent(
		c.processingMessage,
		contentParts,
	)
	c.sendReplyHint(
		ctx,
		msg,
		sender,
		state,
		content,
		false,
	)
	return content
}

func (c *Channel) sendReplyHint(
	ctx context.Context,
	msg WebhookMessage,
	sender messageSender,
	state *replyStreamState,
	content string,
	finish bool,
) {
	streamSender, ok := sender.(streamingSender)
	if !ok || state == nil {
		return
	}
	snapshotMode := normalizeStreamSnapshotMode(c.cfg.StreamSnapshotMode)
	if !finish {
		if snapshotMode == streamSnapshotModeFinalOnly {
			return
		}
		if !state.nativeThinking && snapshotMode != streamSnapshotModeFull {
			return
		}
		if state.nativeThinking && !streamModeSendsPlaceholder(snapshotMode) {
			return
		}
	}
	content = strings.TrimSpace(content)
	statusText := content
	if !finish {
		if state.nativeThinking {
			if state.started {
				return
			}
			content = streamNativeThinkingPlaceholder
		} else {
			if content == "" {
				return
			}
			statusText = recordProgressActivity(
				state,
				statusText,
			)
			content = renderProgressSnapshot(
				state,
				statusText,
				true,
			)
		}
	}
	if content == "" {
		return
	}
	sent, _ := sendReplySnapshot(
		ctx,
		streamSender,
		msg.ChatID,
		state,
		content,
		finish,
	)
	if sent && !finish {
		markStatusPulseSent(state, statusText)
	}
}

func (c *Channel) finishReplyHint(
	ctx context.Context,
	msg WebhookMessage,
	sender messageSender,
	state *replyStreamState,
	content string,
) bool {
	streamSender, ok := sender.(streamingSender)
	if !ok || state == nil {
		return false
	}
	if err := finishReplyStream(
		ctx,
		streamSender,
		msg.ChatID,
		state,
		content,
	); err != nil {
		log.WarnfContext(
			ctx,
			"wecom: finish reply stream failed: %v",
			err,
		)
		return state.started
	}
	return true
}

func (c *Channel) finishReplyHintOnError(
	ctx context.Context,
	msg WebhookMessage,
	sender messageSender,
	state *replyStreamState,
	content string,
) bool {
	return c.finishReplyHint(
		ctx,
		msg,
		sender,
		state,
		content,
	)
}

func (c *Channel) replyDeliveryProgressCallback(
	ctx context.Context,
	msg WebhookMessage,
	sender messageSender,
	state *replyStreamState,
	sessionID string,
	requestID string,
) replyDeliveryProgressFunc {
	if c == nil {
		return nil
	}
	return func(path string, index int, total int) {
		summary := replyDeliveryProgressText(path, index, total)
		if strings.TrimSpace(summary) == "" {
			return
		}
		c.runStatus.progress(
			sessionID,
			requestID,
			streamStageRunningTool,
			summary,
			0,
		)
		c.sendReplyHint(
			ctx,
			msg,
			sender,
			state,
			summary,
			false,
		)
	}
}

func (c *Channel) closeReplyHintIfNeeded(
	ctx context.Context,
	msg WebhookMessage,
	sender messageSender,
	state *replyStreamState,
) {
	streamSender, ok := sender.(streamingSender)
	if !ok || state == nil {
		return
	}
	if err := closeReplyStream(
		ctx,
		streamSender,
		msg.ChatID,
		state,
	); err != nil {
		log.WarnfContext(
			ctx,
			"wecom: close reply stream failed: %v",
			err,
		)
	}
}

func hasImageContentPart(parts []gwproto.ContentPart) bool {
	for _, part := range parts {
		if part.Type == gwproto.PartTypeImage {
			return true
		}
	}
	return false
}

func hasAttachmentContentPart(parts []gwproto.ContentPart) bool {
	for _, part := range parts {
		switch part.Type {
		case gwproto.PartTypeFile,
			gwproto.PartTypeAudio:
			return true
		}
	}
	return false
}
