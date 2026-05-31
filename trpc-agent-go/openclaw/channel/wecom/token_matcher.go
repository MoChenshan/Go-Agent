package wecom

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func normalizeReplyRecoveryText(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"\n", " ",
		"\r", " ",
		"\t", " ",
		"“", "\"",
		"”", "\"",
		"`", "",
	)
	return strings.Join(strings.Fields(replacer.Replace(text)), " ")
}

func containsAnyToken(text string, tokens []string) bool {
	for _, token := range tokens {
		token = strings.TrimSpace(strings.ToLower(token))
		if token == "" {
			continue
		}
		if strings.Contains(text, token) {
			return true
		}
	}
	return false
}

func containsAnyTokenAtSegmentStartOrAfterPrefixes(
	text string,
	prefixes []string,
	tokens []string,
) bool {
	return containsAnyTokenAtSegmentStart(text, tokens) ||
		containsAnyTokenAtSegmentStartAfterPrefixes(
			text,
			prefixes,
			tokens,
		)
}

func containsAnyTokenAtSegmentStartAfterPrefixes(
	text string,
	prefixes []string,
	tokens []string,
) bool {
	for _, prefix := range prefixes {
		if tokenAppearsAfterSegmentPrefix(
			text,
			prefix,
			tokens,
		) {
			return true
		}
	}
	return false
}

func containsAnyTokenAtSegmentStart(
	text string,
	tokens []string,
) bool {
	for _, token := range tokens {
		if tokenAppearsAtSegmentStart(text, token) {
			return true
		}
	}
	return false
}

func tokenAppearsAfterSegmentPrefix(
	text string,
	prefix string,
	tokens []string,
) bool {
	prefix = normalizeReplyRecoveryText(prefix)
	if prefix == "" {
		return false
	}
	for start := 0; start < len(text); {
		offset := strings.Index(text[start:], prefix)
		if offset < 0 {
			return false
		}
		index := start + offset
		if !isSegmentStart(text, index) {
			start = index + len(prefix)
			continue
		}
		suffix := strings.TrimLeftFunc(
			text[index+len(prefix):],
			unicode.IsSpace,
		)
		if hasAnyTokenAtTextStart(suffix, tokens) {
			return true
		}
		start = index + len(prefix)
	}
	return false
}

func tokenAppearsAtSegmentStart(
	text string,
	token string,
) bool {
	token = normalizeReplyRecoveryText(token)
	if token == "" {
		return false
	}
	for start := 0; start < len(text); {
		offset := strings.Index(text[start:], token)
		if offset < 0 {
			return false
		}
		index := start + offset
		if isSegmentStart(text, index) {
			return true
		}
		start = index + len(token)
	}
	return false
}

func hasAnyTokenAtTextStart(text string, tokens []string) bool {
	for _, token := range tokens {
		token = normalizeReplyRecoveryText(token)
		if token == "" {
			continue
		}
		if strings.HasPrefix(text, token) {
			return true
		}
	}
	return false
}

func isSegmentStart(text string, index int) bool {
	if index <= 0 {
		return true
	}
	for cursor := index; cursor > 0; {
		r, size := utf8.DecodeLastRuneInString(text[:cursor])
		cursor -= size
		if unicode.IsSpace(r) {
			continue
		}
		return unicode.IsPunct(r)
	}
	return true
}
