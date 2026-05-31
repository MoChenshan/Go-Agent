package runtimepolicy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPromptNotesReuseCanonicalRules(t *testing.T) {
	t.Parallel()

	require.Contains(
		t,
		ExecutionPromptNote(),
		"[Execution policy: default to taking the next "+
			"concrete step yourself",
	)
	require.Contains(
		t,
		ExecutionPromptNote(),
		"brief visible preamble about the immediate "+
			"next step",
	)
	require.Contains(
		t,
		ExecutionPromptNote(),
		"announce the immediate next step, not to ask "+
			"whether you should take it",
	)
	require.Contains(
		t,
		ExecutionPromptNote(),
		"preamble-only response such as `I will ...`",
	)
	require.Contains(
		t,
		ExecutionPromptNote(),
		"Produce the requested content in the same turn",
	)
	require.Contains(
		t,
		ExecutionPromptNote(),
		"meaningful new stage in the output",
	)
	require.Contains(
		t,
		ExecutionPromptNote(),
		"user's actual request is resolved, not merely "+
			"diagnosed",
	)
	require.Contains(
		t,
		ExecutionPromptNote(),
		"resolve the canonical identifier yourself",
	)
	require.Contains(
		t,
		ExecutionPromptNote(),
		"continue without asking the user to confirm "+
			"it first",
	)
	require.Contains(
		t,
		ExecutionPromptNote(),
		"Do not narrate every empty poll",
	)
	require.Contains(
		t,
		ExecutionPromptNote(),
		"send one brief waiting update",
	)
	require.Contains(
		t,
		GoalCompletionPromptNote(),
		"spoken and strongly implied requirements",
	)
	require.Contains(
		t,
		GoalCompletionPromptNote(),
		"the turn is not complete until you have performed "+
			"the action",
	)
	require.Contains(
		t,
		GoalCompletionPromptNote(),
		"durable policy or memory update",
	)
	require.Contains(
		t,
		QuestionPromptNote(),
		"[Question policy: default to zero follow-up "+
			"questions.",
	)
	require.Contains(
		t,
		NoChoiceTailRule(),
		"optional-offer tails",
	)
	require.Contains(t, NoChoiceTailRule(), "`如果你要`")
	require.Contains(t, NoChoiceTailRule(), "`下一条我可以`")
}

func TestWeComRulesStayAtLeastAsStrongAsMaster(t *testing.T) {
	t.Parallel()

	require.Contains(
		t,
		WeComExecutionRule(),
		"Default to moving the user's request forward "+
			"immediately",
	)
	require.Contains(
		t,
		WeComExecutionRule(),
		"short visible preamble that says the immediate "+
			"next step",
	)
	require.Contains(
		t,
		WeComExecutionRule(),
		"announce the immediate next step, not to ask "+
			"whether you should take it",
	)
	require.Contains(
		t,
		WeComExecutionRule(),
		"preamble-only response such as `I will ...`",
	)
	require.Contains(
		t,
		WeComExecutionRule(),
		"Produce the requested content in the same turn",
	)
	require.Contains(
		t,
		WeComExecutionRule(),
		"formatting decisions",
	)
	require.Contains(
		t,
		WeComExecutionRule(),
		"runtime facts",
	)
	require.Contains(
		t,
		WeComExecutionRule(),
		"dependency bootstrap",
	)
	require.Contains(
		t,
		WeComExecutionRule(),
		"same turn",
	)
	require.Contains(
		t,
		WeComExecutionRule(),
		"meaningful new stage in the output",
	)
	require.Contains(
		t,
		WeComExecutionRule(),
		"user's actual request is resolved, not merely "+
			"diagnosed",
	)
	require.Contains(
		t,
		WeComExecutionRule(),
		"continue without asking the user to confirm "+
			"it first",
	)
	require.Contains(
		t,
		WeComExecutionRule(),
		"resolve the canonical identifier yourself",
	)
	require.Contains(
		t,
		WeComExecutionRule(),
		"Do not narrate every empty poll",
	)
	require.Contains(
		t,
		WeComExecutionRule(),
		"send one brief waiting update",
	)
	require.Contains(
		t,
		WeComExecutionRule(),
		"highest-likelihood reasonable next step",
	)
	require.Contains(
		t,
		WeComGoalCompletionRule(),
		"cheap reversible cleanup",
	)
	require.Contains(
		t,
		WeComGoalCompletionRule(),
		"the turn is not complete until you have performed "+
			"the action",
	)
	require.Contains(
		t,
		WeComGoalCompletionRule(),
		"durable policy or memory update",
	)
	require.Contains(
		t,
		WeComGoalCompletionRule(),
		"concrete schedule",
	)
	require.Contains(
		t,
		WeComQuestionRule(),
		"prefer no user questions at all",
	)
}

func TestFriendlyPersonaPromptIncludesAutonomyGuidance(t *testing.T) {
	t.Parallel()

	prompt := FriendlyPersonaPrompt()
	require.Contains(
		t,
		prompt,
		"keep clarification questions disabled by "+
			"default",
	)
	require.Contains(
		t,
		prompt,
		"what you'll do later",
	)
	require.Contains(
		t,
		prompt,
		"preamble-only response such as `I will ...`",
	)
	require.Contains(
		t,
		prompt,
		"Produce the requested content in the same turn",
	)
	require.Contains(
		t,
		prompt,
		"infer the likely complete goal",
	)
	require.Contains(
		t,
		prompt,
		"exact missing piece briefly as fact",
	)
}
