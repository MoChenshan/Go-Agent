package wecom

import "strings"

const (
	msgTypeTemplateCard = "template_card"

	templateCardTypeButtonInteraction   = "button_interaction"
	templateCardTypeMultipleInteraction = "multiple_interaction"
	templateCardTypeNewsNotice          = "news_notice"
	templateCardTypeTextNotice          = "text_notice"

	templateCardButtonStyleDefault = 1

	templateCardButtonLimit = 6
	templateCardActionLimit = 3
	templateCardSelectLimit = 3

	templateCardButtonOptionLimit = 10
	templateCardMultiOptionLimit  = 20

	templateCardButtonTextMaxRunes     = 10
	templateCardOptionTextMaxRunes     = 11
	templateCardSelectionTitleMaxRunes = 8

	defaultAssistantDisplayName    = "trpc-claw"
	enterChatWelcomeCardActionURL  = "https://work.weixin.qq.com/"
	enterChatWelcomeTitlePrefix    = "👋 嗨，我是 "
	enterChatWelcomeTextInputHint  = "💬 问题、图片、文件都可以直接发给我。"
	enterChatWelcomeTextShortcuts  = "⚡ 你可以先试试："
	enterChatWelcomeTextStatusHint = "📍 想看看我现在在做什么，" +
		"可发送 " + statusKeyword + "。"
	enterChatWelcomeTextSessionHint = "🧵 想找回最近上下文，可发送 " +
		sessionsKeyword + "。"
	enterChatWelcomeTextPersonaHint = "🎭 想换种语气或风格，可发送 " +
		personaKeyword + "。"
	enterChatWelcomeTextWelcomeHint = "🌟 想重新打开这张卡片，可发送 " +
		welcomeKeyword + "。"
	enterChatWelcomeTextRepoHint = "💻 做代码任务前，如果要指定仓库，" +
		"可先发 /workspace /path/to/repo。"
	welcomeShortcutHelpTitle       = "📚 完整帮助"
	welcomeShortcutHelpQuestion    = helpKeyword
	welcomeShortcutHelpLine        = helpKeyword + " 看完整用法"
	welcomeShortcutPersonaTitle    = "🎭 人格卡片"
	welcomeShortcutPersonaQuestion = personaKeyword
	welcomeShortcutPersonaLine     = personaKeyword +
		" 打开人格卡片"
	welcomeShortcutStatusTitle    = "📍 查看状态"
	welcomeShortcutStatusQuestion = statusKeyword
	welcomeShortcutStatusLine     = statusKeyword +
		" 看当前进度和最近输出"
)

type callbackReplyBody struct {
	MsgType      string             `json:"msgtype,omitempty"`
	Text         *callbackReplyText `json:"text,omitempty"`
	TemplateCard *templateCard      `json:"template_card,omitempty"`
}

type callbackReplyText struct {
	Content string `json:"content,omitempty"`
}

type templateCard struct {
	CardType        string                   `json:"card_type,omitempty"`
	ActionMenu      *templateCardActionMenu  `json:"action_menu,omitempty"`
	MainTitle       *templateCardMainTitle   `json:"main_title,omitempty"`
	SubTitleText    string                   `json:"sub_title_text,omitempty"`
	JumpList        []templateCardJumpAction `json:"jump_list,omitempty"`
	ButtonSelection *templateCardSelection   `json:"button_selection,omitempty"`
	ButtonList      []templateCardButton     `json:"button_list,omitempty"`
	SelectList      []templateCardSelection  `json:"select_list,omitempty"`
	SubmitButton    *templateCardSubmit      `json:"submit_button,omitempty"`
	CardAction      *templateCardCardAction  `json:"card_action,omitempty"`
	TaskID          string                   `json:"task_id,omitempty"`
	Feedback        *templateCardFeedback    `json:"feedback,omitempty"`
}

type templateCardActionMenu struct {
	Desc       string                       `json:"desc,omitempty"`
	ActionList []templateCardActionMenuItem `json:"action_list,omitempty"`
}

type templateCardActionMenuItem struct {
	Text string `json:"text,omitempty"`
	Key  string `json:"key,omitempty"`
}

type templateCardMainTitle struct {
	Title string `json:"title,omitempty"`
	Desc  string `json:"desc,omitempty"`
}

type templateCardJumpAction struct {
	Type     int    `json:"type,omitempty"`
	Title    string `json:"title,omitempty"`
	Question string `json:"question,omitempty"`
}

type templateCardCardAction struct {
	Type int    `json:"type,omitempty"`
	URL  string `json:"url,omitempty"`
}

type templateCardSelection struct {
	QuestionKey string               `json:"question_key,omitempty"`
	Title       string               `json:"title,omitempty"`
	Disable     bool                 `json:"disable,omitempty"`
	SelectedID  string               `json:"selected_id,omitempty"`
	OptionList  []templateCardOption `json:"option_list,omitempty"`
}

type templateCardOption struct {
	ID        string `json:"id,omitempty"`
	Text      string `json:"text,omitempty"`
	IsChecked bool   `json:"is_checked,omitempty"`
}

type templateCardButton struct {
	Text  string `json:"text,omitempty"`
	Style int    `json:"style,omitempty"`
	Key   string `json:"key,omitempty"`
}

type templateCardSubmit struct {
	Text string `json:"text,omitempty"`
	Key  string `json:"key,omitempty"`
}

type templateCardFeedback struct {
	ID string `json:"id,omitempty"`
}

type welcomeShortcut struct {
	Title    string
	Question string
	TextLine string
}

func normalizeTemplateCard(card *templateCard) *templateCard {
	if card == nil {
		return nil
	}
	if card.ActionMenu != nil &&
		len(card.ActionMenu.ActionList) > templateCardActionLimit {
		limit := templateCardActionLimit
		card.ActionMenu.ActionList = card.ActionMenu.ActionList[:limit]
	}
	if len(card.ButtonList) > templateCardButtonLimit {
		card.ButtonList = card.ButtonList[:templateCardButtonLimit]
	}
	for i := range card.ButtonList {
		card.ButtonList[i].Text = trimTemplateCardLabel(
			card.ButtonList[i].Text,
			templateCardButtonTextMaxRunes,
		)
	}
	card.ButtonSelection = normalizeTemplateCardSelection(
		card.ButtonSelection,
		card.CardType,
	)
	if len(card.SelectList) > templateCardSelectLimit {
		card.SelectList = card.SelectList[:templateCardSelectLimit]
	}
	for i := range card.SelectList {
		card.SelectList[i] = normalizeTemplateCardSelectListItem(
			card.SelectList[i],
		)
	}
	return card
}

func normalizeTemplateCardSelection(
	selection *templateCardSelection,
	cardType string,
) *templateCardSelection {
	if selection == nil {
		return nil
	}
	selection.OptionList, selection.SelectedID =
		clampTemplateCardOptions(
			selection.OptionList,
			selection.SelectedID,
			templateCardOptionLimit(cardType),
		)
	selection.Title = trimTemplateCardLabel(
		selection.Title,
		templateCardSelectionTitleMaxRunes,
	)
	return selection
}

func normalizeTemplateCardSelectListItem(
	selection templateCardSelection,
) templateCardSelection {
	selection.OptionList, selection.SelectedID =
		clampTemplateCardOptions(
			selection.OptionList,
			selection.SelectedID,
			templateCardOptionLimit(
				templateCardTypeMultipleInteraction,
			),
		)
	return selection
}

func templateCardOptionLimit(cardType string) int {
	switch strings.TrimSpace(cardType) {
	case templateCardTypeMultipleInteraction:
		return templateCardMultiOptionLimit
	default:
		return templateCardButtonOptionLimit
	}
}

func clampTemplateCardOptions(
	options []templateCardOption,
	selectedID string,
	limit int,
) ([]templateCardOption, string) {
	for i := range options {
		options[i].Text = trimTemplateCardLabel(
			options[i].Text,
			templateCardOptionTextMaxRunes,
		)
	}
	selectedID = strings.TrimSpace(selectedID)
	if limit <= 0 || len(options) <= limit {
		if !hasTemplateCardOption(options, selectedID) {
			selectedID = ""
		}
		return options, selectedID
	}
	clamped := append(
		make([]templateCardOption, 0, limit),
		options[:limit]...,
	)
	if selectedID == "" {
		return clamped, ""
	}
	if hasTemplateCardOption(clamped, selectedID) {
		return clamped, selectedID
	}
	for _, option := range options[limit:] {
		if strings.TrimSpace(option.ID) != selectedID {
			continue
		}
		clamped[len(clamped)-1] = option
		return clamped, selectedID
	}
	return clamped, ""
}

func hasTemplateCardOption(
	options []templateCardOption,
	selectedID string,
) bool {
	selectedID = strings.TrimSpace(selectedID)
	if selectedID == "" {
		return false
	}
	for _, option := range options {
		if strings.TrimSpace(option.ID) == selectedID {
			return true
		}
	}
	return false
}

func trimTemplateCardLabel(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	if limit == 1 {
		return string(runes[:1])
	}
	return string(runes[:limit-1]) + "…"
}

var defaultEnterChatWelcomeShortcuts = []welcomeShortcut{
	{
		Title:    welcomeShortcutHelpTitle,
		Question: welcomeShortcutHelpQuestion,
		TextLine: welcomeShortcutHelpLine,
	},
	{
		Title:    welcomeShortcutPersonaTitle,
		Question: welcomeShortcutPersonaQuestion,
		TextLine: welcomeShortcutPersonaLine,
	},
	{
		Title:    welcomeShortcutStatusTitle,
		Question: welcomeShortcutStatusQuestion,
		TextLine: welcomeShortcutStatusLine,
	},
}

func buildEnterChatWelcomeReplyWithTaskID(
	assistantName string,
	modelName string,
	taskID string,
) callbackReplyBody {
	return callbackReplyBody{
		MsgType: msgTypeTemplateCard,
		TemplateCard: buildEnterChatWelcomeCard(
			assistantName,
			modelName,
			taskID,
		),
	}
}

func buildEnterChatWelcomeCard(
	assistantName string,
	modelName string,
	taskID string,
) *templateCard {
	return buildControlHomeCard(
		assistantName,
		defaultChatPersonaDisplay(),
		workspaceDisplayUnset,
		modelName,
		taskID,
	)
}

func buildEnterChatWelcomeMessage(
	assistantName string,
	modelName string,
) string {
	assistantName = resolveAssistantDisplayName(assistantName)
	lines := []string{
		enterChatWelcomeTitlePrefix + assistantName +
			"，很高兴一起开工。",
		enterChatWelcomeTextInputHint,
	}
	if modelLine := formatModelDisplayLine(modelName); modelLine != "" {
		lines = append(lines, modelLine)
	}
	lines = append(lines, "", enterChatWelcomeTextShortcuts)
	for _, shortcut := range defaultEnterChatWelcomeShortcuts {
		lines = append(lines, shortcut.TextLine)
	}
	lines = append(
		lines,
		"",
		enterChatWelcomeTextStatusHint,
		enterChatWelcomeTextSessionHint,
		enterChatWelcomeTextPersonaHint,
		enterChatWelcomeTextWelcomeHint,
		enterChatWelcomeTextRepoHint,
	)
	return strings.Join(lines, "\n")
}

func formatModelDisplayLine(modelName string) string {
	value := strings.TrimSpace(modelName)
	if value == "" {
		return ""
	}
	return displayLabelModel + value
}
