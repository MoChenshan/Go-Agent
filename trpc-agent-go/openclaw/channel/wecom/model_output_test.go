package wecom

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanitizeReplyModelOutputLeavesOtherModelsUntouched(
	t *testing.T,
) {
	t.Parallel()

	raw := "hello" + deepSeekEOSMarkerWide
	require.Equal(
		t,
		raw,
		sanitizeReplyModelOutput("gpt-5.2", raw),
	)
}

func TestSanitizeReplyModelOutputTrimsDeepSeekEOSMarker(
	t *testing.T,
) {
	t.Parallel()

	raw := "你好 " + deepSeekEOSMarkerWide
	require.Equal(
		t,
		"你好",
		sanitizeReplyModelOutput("deepseek-v3.2", raw),
	)
}

func TestSanitizeReplyModelOutputTrimsDeepSeekMarkerFragment(
	t *testing.T,
) {
	t.Parallel()

	fragment := "<｜end▁of▁sent"
	raw := "你好" + fragment
	require.Equal(
		t,
		"你好",
		sanitizeReplyModelOutput("deepseek-chat", raw),
	)
}

func TestSanitizeReplyModelOutputKeepsShortSuffixes(
	t *testing.T,
) {
	t.Parallel()

	raw := "value<|e"
	require.Equal(
		t,
		raw,
		sanitizeReplyModelOutput("deepseek-chat", raw),
	)
}

func TestSanitizeReplyModelOutputTrimsChineseOptionalOfferTail(
	t *testing.T,
) {
	t.Parallel()

	raw := "附件已回传。\n\n如需我可以继续补充说明。"
	require.Equal(
		t,
		"附件已回传。",
		sanitizeReplyModelOutput("gpt-5.2", raw),
	)
}

func TestSanitizeReplyModelOutputTrimsEnglishOptionalOfferTail(
	t *testing.T,
) {
	t.Parallel()

	raw := "Done.\n\nIf you'd like, I can also add tests."
	require.Equal(
		t,
		"Done.",
		sanitizeReplyModelOutput("gpt-5.2", raw),
	)
}

func TestSanitizeReplyModelOutputKeepsStandaloneQuestion(
	t *testing.T,
) {
	t.Parallel()

	raw := "Would you like me to continue?"
	require.Equal(
		t,
		raw,
		sanitizeReplyModelOutput("gpt-5.2", raw),
	)
}

func TestExternalLookupPromptNoteIsContextIndependent(t *testing.T) {
	t.Parallel()

	require.Equal(t, runtimeExternalLookupPromptRule, externalLookupPromptNote())
}
