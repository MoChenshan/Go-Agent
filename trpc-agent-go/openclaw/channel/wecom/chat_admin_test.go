package wecom

import (
	"os"
	"path/filepath"
	"testing"

	personaapi "git.woa.com/trpc-go/trpc-agent-go/openclaw/persona"
	"github.com/stretchr/testify/require"
)

func TestListTrackedChats(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	tracker := newSessionTrackerWithPath(
		sessionTrackerStorePath(stateDir),
	)
	tracker.setAssistantAlias("wecom:dm:user1", "林妹妹")
	tracker.recordKnownUsers("wecom:dm:user1", []string{"user1"})
	tracker.setPersona(
		"wecom:chat:group1:user:user2",
		personaapi.CoachID,
	)
	tracker.setWorkspace(
		"wecom:chat:group1:user:user2",
		"/tmp/workspace",
	)

	chats, err := ListTrackedChats(stateDir)
	require.NoError(t, err)
	require.Len(t, chats, 2)

	dm := findTrackedChat(chats, "wecom:dm:user1")
	require.Equal(t, trackedChatKindDM, dm.Kind)
	require.Equal(t, trackedChatKindDMLabel, dm.KindLabel)
	require.Equal(t, "DM · user1", dm.DisplayLabel)
	require.Equal(t, "林妹妹", dm.AssistantAlias)
	require.Equal(t, []string{"user1"}, dm.KnownUserIDs)

	groupUser := findTrackedChat(
		chats,
		"wecom:chat:group1:user:user2",
	)
	require.Equal(t, trackedChatKindGroupUser, groupUser.Kind)
	require.Equal(
		t,
		trackedChatKindGroupUserLabel,
		groupUser.KindLabel,
	)
	require.Equal(
		t,
		"Group user · group1 / user2",
		groupUser.DisplayLabel,
	)
	require.Equal(t, personaapi.CoachID, groupUser.PersonaID)
	require.True(t, groupUser.PersonaPinned)
	require.Equal(t, "/tmp/workspace", groupUser.WorkspacePath)
}

func TestResolveKnownUserLabels(t *testing.T) {
	stateDir := t.TempDir()
	commandPath := filepath.Join(stateDir, "lookup")
	require.NoError(
		t,
		os.WriteFile(
			commandPath,
			[]byte(
				"#!/bin/sh\n"+
					"cat <<'EOF'\n"+
					"{\"staffAccountName\":\"alice\","+
					"\"staffDisplayName\":\"Alice Chen\"}\n"+
					"EOF\n",
			),
			0o755,
		),
	)

	labels := ResolveKnownUserLabels(
		stateDir,
		commandPath,
		userLabelModeNameOrAlias,
		[]string{"T00010001"},
	)
	require.Equal(
		t,
		map[string]string{"T00010001": "Alice Chen"},
		labels,
	)

	labels = ResolveKnownUserLabels(
		stateDir,
		commandPath,
		userLabelModeID,
		[]string{"T00010001"},
	)
	require.Nil(t, labels)
}

func TestResolveKnownUserIdentities(t *testing.T) {
	stateDir := t.TempDir()
	commandPath := filepath.Join(stateDir, "lookup")
	require.NoError(
		t,
		os.WriteFile(
			commandPath,
			[]byte(
				"#!/bin/sh\n"+
					"cat <<'EOF'\n"+
					"{\"defaultEmailAddress\":\"alice@example.com\","+
					"\"staffAccountName\":\"alice\","+
					"\"staffDisplayName\":\"Alice Chen\"}\n"+
					"EOF\n",
			),
			0o755,
		),
	)

	identities := ResolveKnownUserIdentities(
		stateDir,
		commandPath,
		[]string{"T00010001"},
	)
	require.Equal(
		t,
		map[string]KnownUserIdentity{
			"T00010001": {
				UserID:       "T00010001",
				AccountName:  "alice",
				DisplayName:  "Alice Chen",
				EmailAddress: "alice@example.com",
			},
		},
		identities,
	)
}

func TestFormatTrackedChatDisplayLabel(t *testing.T) {
	t.Parallel()

	require.Equal(
		t,
		"DM · Alice Chen (T00010001)",
		FormatTrackedChatDisplayLabel(
			TrackedChatState{
				BaseSessionID: "wecom:dm:T00010001",
				DisplayLabel:  "DM · T00010001",
				Kind:          trackedChatKindDM,
			},
			map[string]string{"T00010001": "Alice Chen"},
		),
	)
	require.Equal(
		t,
		"Group user · group1 / Alice Chen (T00010001)",
		FormatTrackedChatDisplayLabel(
			TrackedChatState{
				BaseSessionID: "wecom:chat:group1:user:T00010001",
				DisplayLabel:  "Group user · group1 / T00010001",
				Kind:          trackedChatKindGroupUser,
			},
			map[string]string{"T00010001": "Alice Chen"},
		),
	)
	require.Equal(
		t,
		"Group · group1",
		FormatTrackedChatDisplayLabel(
			TrackedChatState{
				BaseSessionID: "wecom:chat:group1",
				DisplayLabel:  "Group · group1",
				Kind:          trackedChatKindGroup,
			},
			map[string]string{"T00010001": "Alice Chen"},
		),
	)
}

func TestTranscriptLookupSessionIDs(t *testing.T) {
	t.Parallel()

	require.Equal(
		t,
		[]string{
			"wecom:dm:T00010001:42",
			"wecom:thread:wecom:dm:T00010001:42",
		},
		TranscriptLookupSessionIDs(
			"wecom:dm:T00010001:42",
		),
	)
	require.Equal(
		t,
		[]string{
			"wecom:dm:T00010001:42",
			"wecom:thread:wecom:dm:T00010001:42",
		},
		TranscriptLookupSessionIDs(
			"wecom:thread:wecom:dm:T00010001:42",
		),
	)
	require.Equal(
		t,
		[]string{"other:session:42"},
		TranscriptLookupSessionIDs("other:session:42"),
	)
}

func findTrackedChat(
	chats []TrackedChatState,
	baseSessionID string,
) TrackedChatState {
	for _, chat := range chats {
		if chat.BaseSessionID == baseSessionID {
			return chat
		}
	}
	return TrackedChatState{}
}
