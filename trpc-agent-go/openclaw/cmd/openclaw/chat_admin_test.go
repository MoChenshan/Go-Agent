package main

import (
	"context"
	"strconv"
	"testing"
	"time"

	wecomchannel "git.woa.com/trpc-go/trpc-agent-go/openclaw/channel/wecom"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/admin"
	"trpc.group/trpc-go/trpc-agent-go/session"
	sessioninmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
)

func TestBuildAdminChatHistorySessions(t *testing.T) {
	t.Parallel()

	const (
		appName     = "openclaw"
		baseChatID  = "wecom:dm:alice"
		currentSess = "wecom:dm:alice:171"
	)

	svc := sessioninmemory.NewSessionService()
	sess, err := svc.CreateSession(
		context.Background(),
		session.Key{
			AppName:   appName,
			UserID:    baseChatID,
			SessionID: currentSess,
		},
		nil,
	)
	require.NoError(t, err)

	now := time.Unix(1700000000, 0)
	require.NoError(t, svc.AppendEvent(
		context.Background(),
		sess,
		&event.Event{
			Author:    "user",
			Timestamp: now,
			Response: &model.Response{
				Choices: []model.Choice{{
					Message: model.NewUserMessage("Who is in charge?"),
				}},
			},
		},
	))
	require.NoError(t, svc.AppendEvent(
		context.Background(),
		sess,
		&event.Event{
			Author:    "assistant",
			Timestamp: now.Add(time.Second),
			Response: &model.Response{
				Choices: []model.Choice{{
					Message: model.NewAssistantMessage(
						"The current chat name wins here.",
					),
				}},
			},
		},
	))

	history, bounded := buildAdminChatHistorySessions(
		appName,
		svc,
		wecomchannel.TrackedChatState{
			BaseSessionID:    baseChatID,
			CurrentSessionID: currentSess,
			History: []wecomchannel.TrackedChatSessionLine{{
				SessionID:    currentSess,
				LastActivity: now.Add(time.Second),
			}},
		},
		nil,
	)
	require.False(t, bounded)
	require.Len(t, history, 1)
	require.Equal(t, currentSess, history[0].SessionID)
	require.True(t, history[0].Current)
	require.Equal(t, adminChatHistoryLabelCurrent, history[0].SessionLabel)
	require.Len(t, history[0].Turns, 2)
	require.Equal(
		t,
		"Who is in charge?",
		history[0].Turns[0].Text,
	)
	require.Equal(
		t,
		"The current chat name wins here.",
		history[0].Turns[1].Text,
	)
}

func TestBuildAdminChatHistorySessionsUsesLegacyThreadSessionID(
	t *testing.T,
) {
	t.Parallel()

	const (
		appName       = "openclaw"
		baseChatID    = "wecom:dm:alice"
		currentSess   = "wecom:dm:alice:171"
		legacyStorage = "wecom:thread:wecom:dm:alice:171"
	)

	svc := sessioninmemory.NewSessionService()
	sess, err := svc.CreateSession(
		context.Background(),
		session.Key{
			AppName:   appName,
			UserID:    baseChatID,
			SessionID: legacyStorage,
		},
		nil,
	)
	require.NoError(t, err)

	now := time.Unix(1700000100, 0)
	require.NoError(t, svc.AppendEvent(
		context.Background(),
		sess,
		&event.Event{
			Author:    "user",
			Timestamp: now,
			Response: &model.Response{
				Choices: []model.Choice{{
					Message: model.NewUserMessage("hello"),
				}},
			},
		},
	))
	require.NoError(t, svc.AppendEvent(
		context.Background(),
		sess,
		&event.Event{
			Author:    "assistant",
			Timestamp: now.Add(time.Second),
			Response: &model.Response{
				Choices: []model.Choice{{
					Message: model.NewAssistantMessage("hi"),
				}},
			},
		},
	))

	history, bounded := buildAdminChatHistorySessions(
		appName,
		svc,
		wecomchannel.TrackedChatState{
			BaseSessionID:    baseChatID,
			CurrentSessionID: currentSess,
			History: []wecomchannel.TrackedChatSessionLine{{
				SessionID:    currentSess,
				LastActivity: now.Add(time.Second),
			}},
		},
		nil,
	)
	require.False(t, bounded)
	require.Len(t, history, 1)
	require.Equal(t, currentSess, history[0].SessionID)
	require.True(t, history[0].Current)
	require.Len(t, history[0].Turns, 2)
	require.Equal(t, "hello", history[0].Turns[0].Text)
	require.Equal(t, "hi", history[0].Turns[1].Text)
}

func TestBuildAdminChatHistorySessionsMergesAdjacentSessionIDs(
	t *testing.T,
) {
	t.Parallel()

	const (
		appName     = "openclaw"
		baseChatID  = "wecom:dm:alice"
		currentSess = "wecom:dm:alice:171"
	)

	svc := sessioninmemory.NewSessionService()
	sess, err := svc.CreateSession(
		context.Background(),
		session.Key{
			AppName:   appName,
			UserID:    baseChatID,
			SessionID: currentSess,
		},
		nil,
	)
	require.NoError(t, err)

	now := time.Unix(1700000200, 0)
	require.NoError(t, svc.AppendEvent(
		context.Background(),
		sess,
		&event.Event{
			Author:    "user",
			Timestamp: now,
			Response: &model.Response{
				Choices: []model.Choice{{
					Message: model.NewUserMessage("hello again"),
				}},
			},
		},
	))
	require.NoError(t, svc.AppendEvent(
		context.Background(),
		sess,
		&event.Event{
			Author:    "assistant",
			Timestamp: now.Add(time.Second),
			Response: &model.Response{
				Choices: []model.Choice{{
					Message: model.NewAssistantMessage("hi again"),
				}},
			},
		},
	))

	history, bounded := buildAdminChatHistorySessions(
		appName,
		svc,
		wecomchannel.TrackedChatState{
			BaseSessionID:    baseChatID,
			CurrentSessionID: currentSess,
			History: []wecomchannel.TrackedChatSessionLine{{
				SessionID:    currentSess,
				LastActivity: now,
			}, {
				SessionID:    currentSess,
				LastActivity: now.Add(2 * time.Second),
			}},
		},
		nil,
	)
	require.False(t, bounded)
	require.Len(t, history, 1)
	require.Equal(t, currentSess, history[0].SessionID)
	require.True(t, history[0].Current)
	require.Equal(
		t,
		now.Add(2*time.Second),
		history[0].LastActivity,
	)
	require.Len(t, history[0].Turns, 2)
	require.Equal(t, "hello again", history[0].Turns[0].Text)
	require.Equal(t, "hi again", history[0].Turns[1].Text)
}

func TestBuildAdminChatHistoryPage(t *testing.T) {
	t.Parallel()

	history := []adminChatHistorySession{{
		SessionID:    "wecom:dm:alice:170",
		SessionLabel: adminChatHistoryLabelRecall,
		LastActivity: time.Unix(1700000000, 0),
		Recall:       true,
		Turns: []admin.ChatTurnView{{
			Role:      "assistant",
			Speaker:   "Claw",
			Text:      "oldest",
			Timestamp: time.Unix(1700000001, 0),
		}, {
			Role:      "assistant",
			Speaker:   "Claw",
			Text:      "older",
			Timestamp: time.Unix(1700000002, 0),
		}},
	}, {
		SessionID:    "wecom:dm:alice:171",
		SessionLabel: adminChatHistoryLabelCurrent,
		LastActivity: time.Unix(1700000010, 0),
		Current:      true,
		Turns: []admin.ChatTurnView{{
			Role:      "user",
			Speaker:   "Alice",
			Text:      "hello",
			Timestamp: time.Unix(1700000011, 0),
		}, {
			Role:      "assistant",
			Speaker:   "Claw",
			Text:      "hi",
			Timestamp: time.Unix(1700000012, 0),
		}},
	}}

	page, err := buildAdminChatHistoryPage(
		"wecom:dm:alice",
		history,
		true,
		"",
	)
	require.NoError(t, err)
	require.Equal(t, "wecom:dm:alice", page.BaseSessionID)
	require.Equal(t, 2, page.SessionLineCount)
	require.Equal(t, 4, page.TurnCount)
	require.Equal(t, 4, page.ReturnedTurnCount)
	require.Empty(t, page.NextCursor)
	require.True(t, page.Bounded)
	require.Len(t, page.Items, 6)
	require.Equal(
		t,
		adminChatHistoryItemSession,
		page.Items[0].Kind,
	)
	require.Equal(
		t,
		adminChatHistoryLabelRecall,
		page.Items[0].SessionLabel,
	)
	require.Equal(
		t,
		adminChatHistoryItemTurn,
		page.Items[1].Kind,
	)
	require.Equal(
		t,
		"wecom:dm:alice:170",
		page.Items[1].SessionID,
	)
	require.Equal(t, "oldest", page.Items[1].Text)

	largeTurns := make([]admin.ChatTurnView, 0, adminChatHistoryPageTurnCount+2)
	for i := 0; i < adminChatHistoryPageTurnCount+2; i++ {
		largeTurns = append(largeTurns, admin.ChatTurnView{
			Role:      "assistant",
			Speaker:   "Claw",
			Text:      strconv.Itoa(i),
			Timestamp: time.Unix(int64(1700000100+i), 0),
		})
	}
	largeHistory := []adminChatHistorySession{{
		SessionID:    "wecom:dm:alice:172",
		SessionLabel: adminChatHistoryLabelCurrent,
		LastActivity: time.Unix(1700000200, 0),
		Current:      true,
		Turns:        largeTurns,
	}}

	page, err = buildAdminChatHistoryPage(
		"wecom:dm:alice",
		largeHistory,
		false,
		"1",
	)
	require.NoError(t, err)
	require.Equal(t, adminChatHistoryPageTurnCount, page.ReturnedTurnCount)
	require.Equal(t, "13", page.NextCursor)
	require.Len(t, page.Items, adminChatHistoryPageTurnCount+1)
	require.Equal(
		t,
		adminChatHistoryLabelCurrent,
		page.Items[0].SessionLabel,
	)
	require.Equal(t, "1", page.Items[1].Text)
	require.Equal(
		t,
		strconv.Itoa(adminChatHistoryPageTurnCount),
		page.Items[len(page.Items)-1].Text,
	)
	require.Equal(
		t,
		"wecom:dm:alice:172",
		page.Items[len(page.Items)-1].SessionID,
	)

	_, err = buildAdminChatHistoryPage(
		"wecom:dm:alice",
		history,
		false,
		"bad",
	)
	require.Error(t, err)
}

func TestBuildAdminChatViewAndTranscriptVisibility(t *testing.T) {
	t.Parallel()

	history := make(
		[]wecomchannel.TrackedChatSessionLine,
		0,
		adminChatHistorySessionLimit+2,
	)
	for i := 0; i < adminChatHistorySessionLimit+2; i++ {
		history = append(history, wecomchannel.TrackedChatSessionLine{
			SessionID: "wecom:dm:alice:history",
			LastActivity: time.Unix(
				int64(1700000000+i),
				0,
			),
		})
	}
	chat := buildAdminChatView(
		wecomchannel.TrackedChatState{
			BaseSessionID:    "wecom:dm:alice",
			CurrentSessionID: "wecom:dm:alice:history",
			History:          history,
		},
		"Claw",
		chatNameSourceIdentity,
		nil,
		nil,
	)
	require.Equal(t, adminChatHistorySessionLimit+2, chat.HistoryTotalCount)
	require.True(t, chat.HistoryTruncated)
	require.Len(t, chat.History, adminChatHistorySessionLimit)
	visibleHistory := 0
	for _, item := range chat.History {
		if item.Visible {
			visibleHistory++
		}
	}
	require.Equal(t, adminChatHistoryVisibleCount, visibleHistory)
}

func TestTrimAdminChatTranscriptText(t *testing.T) {
	t.Parallel()

	require.Empty(t, trimAdminChatTranscriptText("   "))

	short := "short transcript"
	require.Equal(t, short, trimAdminChatTranscriptText(short))

	long := make([]rune, adminChatTranscriptTextLimit+20)
	for i := range long {
		long[i] = 'a'
	}
	got := trimAdminChatTranscriptText(string(long))
	require.True(t, len(got) > 0)
	require.Contains(t, got, "...")
}
