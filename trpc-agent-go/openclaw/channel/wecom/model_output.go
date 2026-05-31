package wecom

import "strings"

const (
	deepSeekModelPrefix = "deepseek"

	deepSeekEOSMarkerWide  = "<｜end▁of▁sentence｜>"
	deepSeekEOSMarkerASCII = "<|end_of_sentence|>"

	minTrimmedMarkerPrefixRunes = 4

	replyOptionalOfferLeadCNNeed      = "如需我可以"
	replyOptionalOfferLeadCNIfNeeded  = "如果需要，我可以"
	replyOptionalOfferLeadCNIfYouNeed = "如果你需要，我可以"
	replyOptionalOfferLeadCNNecessary = "需要的话，我可以"
	replyOptionalOfferLeadCNIfWant    = "要的话，我可以"
	replyOptionalOfferLeadENIfLike    = "if you'd like, i can"
	replyOptionalOfferLeadENIfWould   = "if you would like, i can"
	replyOptionalOfferLeadENIfWant    = "if you want, i can"
	replyOptionalOfferLeadENIfNeeded  = "if needed, i can"
	replyOptionalOfferLeadENLetMeKnow = "let me know if you want"
	replyOptionalOfferLeadENWouldLike = "would you like me to"

	replyOptionalOfferTrimCutset = " \t\r\n;；,，:："
)

type replyOutputPolicy struct {
	trimMarkers []string
}

var replyOptionalOfferLeadIns = []string{
	replyOptionalOfferLeadCNNeed,
	replyOptionalOfferLeadCNIfNeeded,
	replyOptionalOfferLeadCNIfYouNeed,
	replyOptionalOfferLeadCNNecessary,
	replyOptionalOfferLeadCNIfWant,
	replyOptionalOfferLeadENIfLike,
	replyOptionalOfferLeadENIfWould,
	replyOptionalOfferLeadENIfWant,
	replyOptionalOfferLeadENIfNeeded,
	replyOptionalOfferLeadENLetMeKnow,
	replyOptionalOfferLeadENWouldLike,
}

func sanitizeReplyModelOutput(
	modelName string,
	content string,
) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	policy := replyOutputPolicyForModel(modelName)
	if len(policy.trimMarkers) == 0 {
		return trimReplyOptionalOfferTails(content)
	}
	return trimReplyOptionalOfferTails(
		trimReplyOutputMarkers(content, policy.trimMarkers),
	)
}

func replyOutputPolicyForModel(modelName string) replyOutputPolicy {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	if strings.HasPrefix(modelName, deepSeekModelPrefix) {
		return replyOutputPolicy{
			trimMarkers: []string{
				deepSeekEOSMarkerWide,
				deepSeekEOSMarkerASCII,
			},
		}
	}
	return replyOutputPolicy{}
}

func trimReplyOutputMarkers(
	content string,
	markers []string,
) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ""
	}
	for {
		next := trimReplyOutputMarkerSuffix(trimmed, markers)
		if next == trimmed {
			break
		}
		trimmed = next
	}
	fragment := trailingMarkerFragment(trimmed, markers)
	if fragment != "" {
		trimmed = strings.TrimSpace(
			strings.TrimSuffix(trimmed, fragment),
		)
	}
	return trimmed
}

func trimReplyOutputMarkerSuffix(
	content string,
	markers []string,
) string {
	trimmed := strings.TrimSpace(content)
	for _, marker := range markers {
		if marker == "" {
			continue
		}
		if strings.HasSuffix(trimmed, marker) {
			return strings.TrimSpace(
				strings.TrimSuffix(trimmed, marker),
			)
		}
	}
	return trimmed
}

func trailingMarkerFragment(
	content string,
	markers []string,
) string {
	if content == "" {
		return ""
	}
	contentRunes := []rune(content)
	best := ""
	bestLen := 0
	for _, marker := range markers {
		markerRunes := []rune(marker)
		limit := len(markerRunes) - 1
		if limit > len(contentRunes) {
			limit = len(contentRunes)
		}
		for size := limit; size >= minTrimmedMarkerPrefixRunes; size-- {
			suffix := string(
				contentRunes[len(contentRunes)-size:],
			)
			if strings.HasPrefix(marker, suffix) {
				if size > bestLen {
					best = suffix
					bestLen = size
				}
				break
			}
		}
	}
	return best
}

func trimReplyOptionalOfferTails(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ""
	}
	for {
		next := trimReplyOptionalOfferTail(trimmed)
		if next == trimmed {
			return trimmed
		}
		trimmed = next
	}
}

func trimReplyOptionalOfferTail(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ""
	}

	lower := strings.ToLower(trimmed)
	best := -1
	for _, lead := range replyOptionalOfferLeadIns {
		index := strings.LastIndex(lower, lead)
		if index > best {
			best = index
		}
	}
	if best < 0 {
		return trimmed
	}

	prefix := strings.TrimSpace(trimmed[:best])
	prefix = strings.TrimRight(prefix, replyOptionalOfferTrimCutset)
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return trimmed
	}
	return prefix
}
