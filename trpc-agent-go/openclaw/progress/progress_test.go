package progress

import "testing"

import "github.com/stretchr/testify/require"

func TestStateApplyActivity(t *testing.T) {
	t.Parallel()

	state := NewState()

	changed := state.Apply(Event{
		Kind:    KindActivity,
		Message: "Preparing request",
	})
	require.True(t, changed)
	require.Equal(
		t,
		"Preparing request",
		state.Snapshot().Activity,
	)

	changed = state.Apply(Event{
		Kind:    KindActivity,
		Message: "Preparing request",
	})
	require.False(t, changed)
}

func TestStateApplyMilestoneDeduplicates(t *testing.T) {
	t.Parallel()

	state := NewState()

	require.True(t, state.Apply(Event{
		Kind:    KindMilestone,
		Message: "Confirmed workspace layout",
	}))
	require.False(t, state.Apply(Event{
		Kind:    KindMilestone,
		Message: "Confirmed workspace layout",
	}))

	snapshot := state.Snapshot()
	require.Equal(
		t,
		[]string{"Confirmed workspace layout"},
		snapshot.Milestones,
	)
}

func TestStateApplyDraftAppends(t *testing.T) {
	t.Parallel()

	state := NewState()

	require.True(t, state.Apply(Event{
		Kind:    KindDraft,
		Message: "hel",
	}))
	require.True(t, state.Apply(Event{
		Kind:    KindDraft,
		Message: "lo",
	}))

	require.Equal(t, "hello", state.Snapshot().Draft)
}

func TestRenderNarrativePrefersDraft(t *testing.T) {
	t.Parallel()

	rendered := RenderNarrative(
		Snapshot{
			Activity:   "Preparing request",
			Milestones: []string{"Confirmed repository layout"},
			Draft:      "Current answer draft",
		},
		"Preparing request.",
	)

	require.Equal(
		t,
		"Confirmed repository layout\n\nCurrent answer draft",
		rendered,
	)
}

func TestRenderNarrativeShowsActivityWithoutDraft(t *testing.T) {
	t.Parallel()

	rendered := RenderNarrative(
		Snapshot{
			Milestones: []string{"Checked current input"},
		},
		"Reading input.",
	)

	require.Equal(
		t,
		"Checked current input\n\nReading input.",
		rendered,
	)
}
