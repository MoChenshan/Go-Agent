package wecom

import (
	"strings"

	"git.woa.com/trpc-go/trpc-agent-go/openclaw/releaseinfo"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/runtimectl"
)

const (
	helpAliasArg = "help"

	helpTopicPrefix         = "📘 "
	helpTopicSectionUsage   = "用法："
	helpTopicSectionNotes   = "说明："
	helpTopicSectionMore    = "更多："
	helpTopicUnknownPrefix  = "未知帮助主题："
	helpTopicAvailableLabel = "可用主题："
	helpTopicExampleLabel   = "例如："
)

type commandHelpTopic struct {
	canonical string
	title     string
	summary   string
	usage     []string
	notes     []string
	more      []string
}

func defaultCommandHelpTopics() []commandHelpTopic {
	return []commandHelpTopic{
		{
			canonical: helpKeyword,
			title:     helpKeyword + " 帮助系统",
			summary: "默认打开帮助卡片；需要深入某个命令时，" +
				"再看对应主题帮助。",
			usage: []string{
				helpKeyword,
				helpKeyword + " " + helpArgAll,
				helpKeyword + " runtime",
				runtimeKeyword + " " + helpAliasArg,
			},
			notes: []string{
				helpKeyword + " 优先返回帮助卡片；" +
					"没有卡片能力时回退到文本摘要。",
				helpKeyword + " " + helpArgAll + " / " +
					helpKeyword + " " + helpArgText + " / " +
					helpKeyword + " " + helpArgCommands +
					" 会返回全文命令文本。",
				helpKeyword + " <主题> 会展开某个命令的完整说明；" +
					"主题既可以写 runtime，也可以写 /runtime。",
				"当前内置 slash 命令都支持 /<命令> " +
					helpAliasArg + " 作为等价入口。",
			},
			more: []string{
				helpKeyword + " runtime",
				helpKeyword + " persona",
				helpKeyword + " cron",
			},
		},
		{
			canonical: welcomeKeyword,
			title:     welcomeKeyword + " 欢迎卡片",
			summary:   "重新打开主页卡片和常用入口。",
			usage: []string{
				welcomeKeyword,
				welcomeKeyword + " " + helpAliasArg,
			},
			notes: []string{
				"适合把主页卡片重新拉出来，继续点主页 / 人格 / " +
					"状态 / 运行时这些入口。",
				"不会修改会话、人格或工作区，只是重新展示入口卡片。",
			},
			more: []string{
				helpKeyword + " runtime",
				helpKeyword + " persona",
			},
		},
		{
			canonical: nameKeyword,
			title:     nameKeyword + " 称呼设置",
			summary: "查看当前名字，或修改默认名字。" +
				"当前聊天名字优先，默认名字兜底。",
			usage: []string{
				nameKeyword,
				nameKeyword + " 小助手",
				nameKeyword + " off",
				nameKeyword + " global 林妹妹",
				nameKeyword + " global off",
				nameKeyword + " " + helpAliasArg,
			},
			notes: []string{
				"不带 global 时，会修改当前聊天里的名字；" +
					"这个覆盖会跨 /new 保留，直到发送 " +
					nameKeyword + " off。",
				"发送 " + nameKeyword + " global <称呼> " +
					"会修改默认名字。它会影响其他用户的" +
					"新私聊、其他群里的新聊天，以及任何还没有" +
					"单独改名的现有聊天。",
				"如果另一个私聊、另一个群已经设置了自己的" +
					"当前聊天名字，那边不会被 global 改掉。",
				"名字只影响自称和展示，不影响模型权限或" +
					"运行时产品信息。",
			},
			more: []string{
				statusKeyword,
				welcomeKeyword,
			},
		},
		{
			canonical: statusKeyword,
			title:     statusKeyword + " 运行状态",
			summary: "查看当前请求阶段、排队情况、最近输出、" +
				"模型和运行版本。",
			usage: []string{
				statusKeyword,
				statusKeyword + " " + helpAliasArg,
			},
			notes: []string{
				"适合在长任务处理中随时查看当前进度。",
				"状态文本会尽量带上最近输出、上下文占用、" +
					"当前模型和运行版本。",
				"drain 期间仍然可以使用 " + statusKeyword +
					" 查看切换进度。",
			},
			more: []string{
				runtimeKeyword,
				sessionsKeyword,
			},
		},
		{
			canonical: newKeyword,
			title:     newKeyword + " 新会话",
			summary:   "开始一个全新的会话，上下文从空开始。",
			usage: []string{
				newKeyword,
				newKeyword + " " + helpAliasArg,
			},
			notes: []string{
				"适合话题已经跑偏，或者你想切到全新上下文时使用。",
				"执行后仍然可以用 " + recallKeyword +
					" 切回 /new 前的上一会话。",
			},
			more: []string{
				recallKeyword,
				sessionsKeyword,
			},
		},
		{
			canonical: cancelKeyword,
			title:     cancelKeyword + " 中断当前请求",
			summary:   "取消当前正在执行的那一条请求。",
			usage: []string{
				cancelKeyword,
				cancelKeyword + " " + helpAliasArg,
			},
			notes: []string{
				"只影响当前正在运行的请求，不会清空会话历史。",
				"如果当前没有正在执行的请求，会明确提示没有可取消项。",
			},
			more: []string{
				statusKeyword,
				newKeyword,
			},
		},
		{
			canonical: subagentsKeyword,
			title:     subagentsKeyword + " 后台子任务",
			summary: "查看当前会话里由模型创建的后台 " +
				"subagent，并获取结果或取消运行中的任务。",
			usage: []string{
				subagentsKeyword,
				subagentsKeyword + " list",
				subagentsKeyword + " get <id>",
				subagentsKeyword + " cancel <id>",
				subagentsKeyword + " " + helpAliasArg,
			},
			notes: []string{
				"默认只看当前用户、当前会话范围内的 subagent。",
				"不带参数等价于 " + subagentsKeyword +
					" list。",
				subagentsKeyword + " get <id> " +
					"会返回任务、状态、摘要和错误信息。",
				subagentsKeyword + " cancel <id> " +
					"只会 best-effort 取消仍在运行的任务。",
			},
			more: []string{
				statusKeyword,
				sessionKeyword,
			},
		},
		{
			canonical: sessionKeyword,
			title:     sessionKeyword + " 当前会话",
			summary:   "查看当前会话、人格、工作区和分会话设置。",
			usage: []string{
				sessionKeyword,
				sessionKeyword + " " + helpAliasArg,
			},
			notes: []string{
				"适合先确认自己现在到底在哪个会话里。",
				"如果开启自动分会话，这里也会带上相关状态。",
			},
			more: []string{
				sessionsKeyword,
				switchKeyword,
			},
		},
		{
			canonical: sessionsKeyword,
			title:     sessionsKeyword + " 最近会话",
			summary:   "列出最近会话，并配合 /switch 回到旧上下文。",
			usage: []string{
				sessionsKeyword,
				sessionsKeyword + " 10",
				sessionsKeyword + " " + helpAliasArg,
			},
			notes: []string{
				"不带参数时返回默认数量；带数字时可看更多最近会话。",
				"输出里的序号可以直接配合 " + switchKeyword +
					" <序号> 使用。",
			},
			more: []string{
				switchKeyword + " 2",
				recallKeyword,
			},
		},
		{
			canonical: switchKeyword,
			title:     switchKeyword + " 切换会话",
			summary:   "按 /sessions 里的序号切换到某个历史会话。",
			usage: []string{
				switchKeyword + " <序号>",
				switchKeyword + " 2",
				switchKeyword + " " + helpAliasArg,
			},
			notes: []string{
				"先发 " + sessionsKeyword + " 看列表，再用序号切换。",
				"切换后，后续普通消息都会继续沿用目标会话的上下文。",
			},
			more: []string{
				sessionsKeyword,
				recallKeyword,
			},
		},
		{
			canonical: recallKeyword,
			title:     recallKeyword + " 切回上一会话",
			summary:   "切回 /new 前的上一会话。",
			usage: []string{
				recallKeyword,
				recallKeyword + " " + helpAliasArg,
			},
			notes: []string{
				"只针对最近一次 /new 造成的会话切换。",
				"如果当前没有可回退的上一会话，会明确提示。",
			},
			more: []string{
				newKeyword,
				sessionsKeyword,
			},
		},
		{
			canonical: personaKeyword,
			title:     personaKeyword + " 人格管理",
			summary: "查看当前人格，切换已有预设，或创建、" +
				"保存自定义人格。",
			usage: []string{
				personaKeyword,
				personaKeyword + " " + personaActionList,
				personaKeyword + " friendly",
				personaKeyword + " " + personaActionUse +
					" pragmatic",
				personaKeyword + " " + personaActionSave +
					" 爱心 热心一点，先给结论",
				personaKeyword + " " + personaActionShow +
					" 爱心",
				personaKeyword + " " + personaActionDelete +
					" 爱心",
				personaKeyword + " " + helpAliasArg,
			},
			notes: []string{
				"不带参数时会优先打开人格卡片；文本回退时返回当前人格状态。",
				personaKeyword + " <名称或设定> 会先按名称查找；" +
					"找不到时把整段内容当成新设定直接创建人格。",
				personaKeyword + " " + personaActionSave +
					" 会指定名称新增或更新人格，并立即启用。",
				personaKeyword + " " + personaActionDelete +
					" 只会删除自定义人格，不会删内置预设。",
			},
			more: []string{
				helpKeyword + " persona",
				statusKeyword,
			},
		},
		{
			canonical: workspaceKeyword,
			title:     workspaceKeyword + " 代码工作区",
			summary:   "查看、设置或清除当前聊天绑定的代码工作区。",
			usage: []string{
				workspaceKeyword,
				workspaceKeyword + " /path/to/repo",
				workspaceKeyword + " off",
				workspaceKeyword + " " + helpAliasArg,
			},
			notes: []string{
				"设置后，代码相关请求会优先在这个目录里执行。",
				"发送 " + workspaceKeyword + " off " +
					"会清除当前聊天的显式工作区绑定。",
				"如果不显式设置，运行时会回退到默认 coding workspace。",
			},
			more: []string{
				statusKeyword,
				helpKeyword + " runtime",
			},
		},
		{
			canonical: cronKeyword,
			title:     cronKeyword + " 定时任务管理",
			summary: "查看、暂停、恢复、删除当前聊天下的" +
				"定时任务。",
			usage: []string{
				cronKeyword + " " + helpAliasArg,
				cronKeyword + " list",
				cronKeyword + " status <序号或ID>",
				cronKeyword + " stop <序号或ID>",
				cronKeyword + " resume <序号或ID>",
				cronKeyword + " remove <序号或ID>",
				cronKeyword + " clear",
			},
			notes: []string{
				"这个 slash 负责查看和管理任务；创建任务一般还是通过" +
					"正常对话来触发。",
				"<序号> 对应 " + cronKeyword +
					" list 返回的列表序号；" +
					"<ID> 则是任务自己的稳定 ID。",
				cronKeyword + " clear 会清空当前聊天下的所有任务。",
			},
			more: []string{
				helpKeyword + " cron",
				statusKeyword,
			},
		},
		{
			canonical: runtimeKeyword,
			title:     runtimeKeyword + " 运行时控制",
			summary: "对当前在线实例发起无损 / 强制重启、" +
				"升级、版本查询、changelog 查询，或回传" +
				"调试 bundle。",
			usage: []string{
				runtimeKeyword,
				runtimeKeyword + " " + helpAliasArg,
				runtimeKeyword + " " + runtimeActionStatus,
				runtimeKeyword + " " + runtimeActionRestart,
				runtimeKeyword + " " + runtimeActionRestart +
					" " + runtimeActionForce,
				runtimeKeyword + " " + runtimeActionUpgrade,
				runtimeKeyword + " " + runtimeActionUpgrade +
					" " + runtimeActionForce,
				runtimeKeyword + " " + runtimeActionUpgrade +
					" " + runtimectl.DefaultMinTargetVersion,
				runtimeKeyword + " " + runtimeActionUpgrade +
					" " + runtimectl.DefaultMinTargetVersion +
					" " + runtimeActionForce,
				runtimeKeyword + " " + runtimeActionUpgrade +
					" " + releaseinfo.ChannelPreview,
				runtimeKeyword + " " + runtimeActionVersions,
				runtimeKeyword + " " + runtimeActionChangelog,
				runtimeKeyword + " " + runtimeActionChangelog +
					" " + runtimectl.DefaultMinTargetVersion,
				runtimeKeyword + " " + runtimeActionBundle,
				runtimeKeyword + " " + runtimeActionBundle +
					" " + runtimeActionFull,
				runtimeKeyword + " " + runtimeActionBundle +
					" " + runtimeActionFull + " 80mb",
			},
			notes: []string{
				"不带参数时会优先打开运行时控制卡片；" +
					"没有卡片能力时回退到文本状态。",
				"restart 只重启当前 binary；upgrade " +
					"会切到 latest 或你指定的目标版本。",
				"upgrade preview 会显式切到 " +
					"preview channel 当前指向的版本；默认 upgrade " +
					"不会进入 preview。",
				"无损动作会先 drain，停止接收新普通请求，" +
					"等当前已接收请求处理完再切换。",
				"强制动作会尽快取消当前任务，再重启或升级。",
				"指定版本升级当前只允许 >= " +
					runtimectl.DefaultMinTargetVersion + "。",
				"restart / upgrade / bundle 只允许 runtime " +
					"admin 执行；status / versions / changelog " +
					"所有人可看。",
				"bundle 会把当前机器上的调试目录、配置、" +
					"会话跟踪和默认 session DB 等资料打成 zip，" +
					"并直接回传到当前聊天。需要排查线上问题时，" +
					"优先先发这个。",
				"如果资料总量超过企微 20 MB 附件上限，" +
					"bundle 会优先保留配置、当前状态和最近的" +
					"调试文件；被省略的内容会写进压缩包里的 " +
					"MANIFEST.txt。",
				"bundle full 会把资料拆成多个 <=20 MB 的" +
					"分包回传；你也可以额外带一个总上限，" +
					"比如 /runtime bundle full 80mb。",
				"full 模式的总上限是近似值：系统会尽量把总" +
					"回传量控制在这个范围附近；默认单位是 MB。",
				"企业微信 AI WebSocket 模式下，实例切换完成后" +
					"会向原会话补一条完成消息。",
			},
			more: []string{
				helpKeyword + " all",
				statusKeyword,
			},
		},
	}
}

func isHelpAliasToken(raw string) bool {
	return strings.EqualFold(strings.TrimSpace(raw), helpAliasArg)
}

func isFullHelpToken(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case helpArgAll, helpArgText, helpArgCommands:
		return true
	default:
		return false
	}
}

func normalizeHelpTopic(raw string) (string, bool) {
	token := strings.ToLower(strings.TrimSpace(raw))
	if token == "" {
		return "", false
	}
	if !strings.HasPrefix(token, commandPrefix) {
		token = commandPrefix + token
	}
	switch token {
	case helpKeyword,
		welcomeKeyword,
		nameKeyword,
		statusKeyword,
		newKeyword,
		cancelKeyword,
		subagentsKeyword,
		sessionKeyword,
		sessionsKeyword,
		switchKeyword,
		recallKeyword,
		personaKeyword,
		workspaceKeyword,
		cronKeyword,
		runtimeKeyword:
		return token, true
	default:
		return "", false
	}
}

func findCommandHelpTopic(raw string) (commandHelpTopic, bool) {
	topicKey, ok := normalizeHelpTopic(raw)
	if !ok {
		return commandHelpTopic{}, false
	}
	for _, topic := range defaultCommandHelpTopics() {
		if topic.canonical == topicKey {
			return topic, true
		}
	}
	return commandHelpTopic{}, false
}

func parseExplicitHelpTopic(args []string) (commandHelpTopic, bool) {
	if len(args) == 0 {
		return commandHelpTopic{}, false
	}
	if isFullHelpToken(args[0]) {
		return commandHelpTopic{}, false
	}
	return findCommandHelpTopic(args[0])
}

func parseCommandHelpAlias(cmd parsedCommand) (commandHelpTopic, bool) {
	if cmd.keyword == "" || cmd.keyword == helpKeyword || len(cmd.args) != 1 {
		return commandHelpTopic{}, false
	}
	if !isHelpAliasToken(cmd.args[0]) {
		return commandHelpTopic{}, false
	}
	return findCommandHelpTopic(cmd.keyword)
}

func formatCommandHelpTopic(topic commandHelpTopic) string {
	lines := []string{helpTopicPrefix + topic.title}
	if summary := strings.TrimSpace(topic.summary); summary != "" {
		lines = append(lines, "", summary)
	}
	appendHelpTopicSection(&lines, helpTopicSectionUsage, topic.usage)
	appendHelpTopicSection(&lines, helpTopicSectionNotes, topic.notes)
	appendHelpTopicSection(&lines, helpTopicSectionMore, topic.more)
	return strings.Join(lines, "\n")
}

func appendHelpTopicSection(
	lines *[]string,
	title string,
	items []string,
) {
	cleaned := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		cleaned = append(cleaned, item)
	}
	if len(cleaned) == 0 {
		return
	}
	*lines = append(*lines, "", title)
	for _, item := range cleaned {
		*lines = append(*lines, "- "+item)
	}
}

func formatUnknownHelpTopic(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return helpCommandUsage
	}
	return helpTopicUnknownPrefix + trimmed + "\n" +
		helpTopicAvailableLabel + availableHelpTopics() + "\n" +
		helpTopicExampleLabel + helpKeyword + " runtime"
}

func availableHelpTopics() string {
	names := make([]string, 0, len(defaultCommandHelpTopics()))
	for _, topic := range defaultCommandHelpTopics() {
		names = append(
			names,
			strings.TrimPrefix(topic.canonical, commandPrefix),
		)
	}
	return strings.Join(names, "、")
}
