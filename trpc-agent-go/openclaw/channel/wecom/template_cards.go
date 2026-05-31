package wecom

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	personaapi "git.woa.com/trpc-go/trpc-agent-go/openclaw/persona"
)

const (
	personaCardTaskPrefix = "persona"

	personaCardViewDefault  = "default"
	personaCardViewSaveHelp = "save_help"

	personaCardQuestionKey = "persona_selection"

	personaCardEventApply    = "persona_apply"
	personaCardEventHome     = "persona_home"
	personaCardEventSaveHelp = "persona_save_help"
	personaCardQuickKeyPref  = "persona_quick_"

	personaCardTitle           = "🎭 人格设置"
	personaCardApplyText       = "✅ 应用所选"
	personaCardHomeText        = "🏠 返回面板"
	personaCardSaveHelpText    = "🪄 新建说明"
	personaCardSelectionTitle  = "🎛️ 更多人格"
	personaCardCurrentSuffix   = "·当前"
	personaCardQuickHint       = "🎯 常用人格：点一下直接切换。"
	personaCardMoreHint        = "📚 更多人格：下拉后点“应用所选”。"
	personaCardEffectHint      = "⏱️ 生效范围：下一条回复开始。"
	personaCardChangedNote     = "✅ 已切换成功，下一条回复开始生效。"
	personaCardStorageDisabled = "⚠️ 当前未配置 persona_dir 或 " +
		"state_dir，暂时不能保存自定义人格。"
)

var personaCardQuickPersonaIDs = []string{
	personaapi.SnarkyID,
	personaapi.GirlfriendID,
	personaapi.BoyfriendID,
}

var personaCardDropdownPersonaIDs = []string{
	personaapi.PragmaticID,
	personaapi.QuirkyID,
	personaapi.CreativeID,
	personaapi.NerdyID,
	personaapi.FriendlyID,
	personaapi.CoachID,
	personaapi.CandidID,
}

type templateCardSender interface {
	SendTemplateCard(
		ctx context.Context,
		chatID string,
		card *templateCard,
	) error
}

type templateCardUpdater interface {
	UpdateTemplateCard(
		ctx context.Context,
		card *templateCard,
	) error
}

type interactiveTemplateCardSender interface {
	templateCardSender
	templateCardUpdater
}

func buildPersonaSettingsCard(
	assistantName string,
	currentPersona string,
	info *sessionInfo,
	defs []personaapi.Definition,
	taskID string,
	view string,
	note string,
	storageEnabled bool,
) *templateCard {
	selectedID := personaCardSelectedID(info)
	optionDefs, optionSelectedID := personaCardOptionDefinitions(
		defs,
		selectedID,
	)
	options := buildPersonaCardOptions(
		optionDefs,
		optionSelectedID,
	)
	return &templateCard{
		CardType: templateCardTypeButtonInteraction,
		MainTitle: &templateCardMainTitle{
			Title: resolveAssistantDisplayName(assistantName) +
				" · " + personaCardTitle,
			Desc: "当前人格：" + currentPersona,
		},
		SubTitleText: buildPersonaCardSubtitle(
			view,
			note,
			storageEnabled,
		),
		ButtonSelection: &templateCardSelection{
			QuestionKey: personaCardQuestionKey,
			Title:       personaCardSelectionTitle,
			SelectedID:  optionSelectedID,
			OptionList:  options,
		},
		ButtonList: buildPersonaCardButtons(defs, selectedID),
		TaskID:     strings.TrimSpace(taskID),
	}
}

func buildPersonaCardSubtitle(
	view string,
	note string,
	storageEnabled bool,
) string {
	lines := make([]string, 0, 4)
	note = strings.TrimSpace(note)
	if note != "" {
		lines = append(lines, note)
	}
	switch strings.TrimSpace(view) {
	case personaCardViewSaveHelp:
		lines = append(lines,
			personaKeyword+" "+personaExamplePrompt,
			personaKeyword+" "+personaActionSave+
				" "+personaExampleName+" "+
				personaExamplePrompt,
			"直接写设定会自动新增人格；想自己命名"+
				"时再用 save。",
		)
		if !storageEnabled {
			lines = append(
				lines,
				personaCardStorageDisabled,
			)
		}
		return strings.Join(lines, "\n")
	default:
		lines = append(
			lines,
			personaCardQuickHint,
			personaCardMoreHint,
			personaCardEffectHint,
			personaListHelpLine,
		)
		return strings.Join(lines, "\n")
	}
}

func buildPersonaCardButtons(
	defs []personaapi.Definition,
	selectedID string,
) []templateCardButton {
	buttons := make(
		[]templateCardButton,
		0,
		len(personaCardQuickPersonaIDs)+2,
	)
	for _, id := range personaCardQuickPersonaIDs {
		def, ok := findPersonaCardDefinition(defs, id)
		if !ok {
			continue
		}
		buttons = append(buttons, templateCardButton{
			Text:  buildPersonaCardQuickText(def, selectedID),
			Style: templateCardButtonStyleDefault,
			Key:   personaCardQuickEventKey(def.ID),
		})
	}
	buttons = append(
		buttons,
		templateCardButton{
			Text:  personaCardApplyText,
			Style: templateCardButtonStyleDefault,
			Key:   personaCardEventApply,
		},
		templateCardButton{
			Text:  personaCardSaveHelpText,
			Style: templateCardButtonStyleDefault,
			Key:   personaCardEventSaveHelp,
		},
		templateCardButton{
			Text:  personaCardHomeText,
			Style: templateCardButtonStyleDefault,
			Key:   personaCardEventHome,
		},
	)
	return buttons
}

func findPersonaCardDefinition(
	defs []personaapi.Definition,
	id string,
) (personaapi.Definition, bool) {
	id = strings.TrimSpace(id)
	for _, def := range defs {
		if strings.TrimSpace(def.ID) == id {
			return def, true
		}
	}
	return personaapi.LookupBuiltin(id)
}

func buildPersonaCardQuickText(
	def personaapi.Definition,
	selectedID string,
) string {
	text := strings.TrimSpace(def.Name)
	if text == "" {
		text = strings.TrimSpace(def.ID)
	}
	if strings.TrimSpace(def.ID) == strings.TrimSpace(selectedID) {
		return text + personaCardCurrentSuffix
	}
	return text
}

func buildPersonaCardOptions(
	defs []personaapi.Definition,
	selectedID string,
) []templateCardOption {
	options := make([]templateCardOption, 0, len(defs))
	for _, def := range defs {
		text := strings.TrimSpace(def.Name)
		if text == "" {
			text = def.ID
		}
		options = append(options, templateCardOption{
			ID:        def.ID,
			Text:      text,
			IsChecked: selectedID == def.ID,
		})
	}
	return options
}

func personaCardOptionDefinitions(
	defs []personaapi.Definition,
	selectedID string,
) ([]personaapi.Definition, string) {
	filtered := make([]personaapi.Definition, 0, len(defs))
	for _, def := range defs {
		if isPersonaCardQuickID(def.ID) {
			continue
		}
		filtered = append(filtered, def)
	}
	filtered = orderedPersonaCardDefinitions(filtered)
	filtered = visiblePersonaDefinitions(
		filtered,
		selectedID,
		len(personaCardDropdownPersonaIDs),
	)
	if isPersonaCardQuickID(selectedID) {
		return filtered, ""
	}
	if !hasPersonaDefinition(filtered, selectedID) {
		return filtered, ""
	}
	return filtered, selectedID
}

func orderedPersonaCardDefinitions(
	defs []personaapi.Definition,
) []personaapi.Definition {
	if len(defs) <= 1 {
		return defs
	}
	ordered := make([]personaapi.Definition, 0, len(defs))
	used := make(map[string]struct{}, len(defs))
	for _, id := range personaCardDropdownPersonaIDs {
		for _, def := range defs {
			if strings.TrimSpace(def.ID) != strings.TrimSpace(id) {
				continue
			}
			ordered = append(ordered, def)
			used[def.ID] = struct{}{}
			break
		}
	}
	for _, def := range defs {
		if _, ok := used[def.ID]; ok {
			continue
		}
		ordered = append(ordered, def)
	}
	return ordered
}

func visiblePersonaDefinitions(
	defs []personaapi.Definition,
	selectedID string,
	limit int,
) []personaapi.Definition {
	if limit <= 0 || len(defs) <= limit {
		return defs
	}
	visible := append(
		make([]personaapi.Definition, 0, limit),
		defs[:limit]...,
	)
	for _, def := range visible {
		if def.ID == selectedID {
			return visible
		}
	}
	for _, def := range defs[limit:] {
		if def.ID == selectedID {
			visible[len(visible)-1] = def
			return visible
		}
	}
	return visible
}

func hasPersonaDefinition(
	defs []personaapi.Definition,
	id string,
) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	for _, def := range defs {
		if strings.TrimSpace(def.ID) == id {
			return true
		}
	}
	return false
}

func isPersonaCardQuickID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	for _, quickID := range personaCardQuickPersonaIDs {
		if strings.TrimSpace(quickID) == id {
			return true
		}
	}
	return false
}

func personaCardSelectedID(info *sessionInfo) string {
	if info == nil {
		return defaultChatPersonaID
	}
	return info.effectivePersonaID()
}

func personaCardQuickEventKey(personaID string) string {
	return personaCardQuickKeyPref + strings.TrimSpace(personaID)
}

func personaCardQuickPersonaID(eventKey string) (string, bool) {
	eventKey = strings.TrimSpace(eventKey)
	if !strings.HasPrefix(eventKey, personaCardQuickKeyPref) {
		return "", false
	}
	personaID := strings.TrimSpace(
		strings.TrimPrefix(eventKey, personaCardQuickKeyPref),
	)
	if personaID == "" {
		return "", false
	}
	return personaID, true
}

func isPersonaCardEventKey(eventKey string) bool {
	eventKey = strings.TrimSpace(eventKey)
	if eventKey == personaCardEventApply ||
		eventKey == personaCardEventHome ||
		eventKey == personaCardEventSaveHelp {
		return true
	}
	_, ok := personaCardQuickPersonaID(eventKey)
	return ok
}

func resolvePersonaSelection(
	info *sessionInfo,
	event *TemplateCardEvent,
) string {
	optionID := selectedTemplateCardOption(
		event,
		personaCardQuestionKey,
	)
	if optionID != "" {
		return strings.TrimSpace(optionID)
	}
	return personaCardSelectedID(info)
}

func selectedTemplateCardOption(
	event *TemplateCardEvent,
	questionKey string,
) string {
	if event == nil {
		return ""
	}
	for _, item := range event.SelectedItems.SelectedItem {
		if strings.TrimSpace(item.QuestionKey) !=
			strings.TrimSpace(questionKey) {
			continue
		}
		for _, optionID := range item.OptionIDs.OptionID {
			optionID = strings.TrimSpace(optionID)
			if optionID != "" {
				return optionID
			}
		}
	}
	return ""
}

func newInteractiveCardTaskID(
	prefix string,
	baseSessionID string,
) string {
	replacer := strings.NewReplacer(
		":",
		"-",
		"/",
		"-",
		" ",
		"-",
	)
	suffix := replacer.Replace(
		strings.TrimSpace(baseSessionID),
	)
	if suffix == "" {
		suffix = "session"
	}
	return strings.TrimSpace(prefix) + "-" + suffix + "-" +
		strconv.FormatInt(time.Now().UnixNano(), 10)
}

func resolveAssistantDisplayName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return defaultAssistantDisplayName
	}
	return name
}

func templateCardEventSummary(
	event *TemplateCardEvent,
) string {
	if event == nil {
		return ""
	}
	selected := selectedTemplateCardOption(
		event,
		personaCardQuestionKey,
	)
	if selected == "" {
		return strings.TrimSpace(event.EventKey)
	}
	return fmt.Sprintf(
		"%s(%s)",
		strings.TrimSpace(event.EventKey),
		selected,
	)
}
