package wecom

import (
	"context"
	"testing"

	publicsubagent "git.woa.com/trpc-go/trpc-agent-go/openclaw/subagent"
	"github.com/stretchr/testify/require"
)

type fakeSubagentService struct {
	runs   map[string]publicsubagent.Run
	owners map[string]string
}

func (f *fakeSubagentService) ListForUser(
	userID string,
	filter publicsubagent.ListFilter,
) []publicsubagent.Run {
	out := make([]publicsubagent.Run, 0, len(f.runs))
	for id, run := range f.runs {
		if f.owners[id] != userID {
			continue
		}
		if filter.ParentSessionID != "" &&
			run.ParentSessionID != filter.ParentSessionID {
			continue
		}
		out = append(out, run)
	}
	return out
}

func (f *fakeSubagentService) GetForUser(
	userID string,
	runID string,
) (*publicsubagent.Run, error) {
	run, ok := f.runs[runID]
	if !ok || f.owners[runID] != userID {
		return nil, publicsubagent.ErrRunNotFound
	}
	copied := run
	return &copied, nil
}

func (f *fakeSubagentService) CancelForUser(
	userID string,
	runID string,
) (*publicsubagent.Run, bool, error) {
	run, ok := f.runs[runID]
	if !ok || f.owners[runID] != userID {
		return nil, false, publicsubagent.ErrRunNotFound
	}
	if run.Status.IsTerminal() {
		copied := run
		return &copied, false, nil
	}
	run.Status = publicsubagent.StatusCanceled
	run.Summary = "canceled"
	f.runs[runID] = run
	copied := run
	return &copied, true, nil
}

func TestHandleSubagentsCommandList(t *testing.T) {
	t.Parallel()

	baseSessionID := "wecom:dm:chat-1"
	channel := &Channel{
		sessionTracker: newSessionTracker(),
		subagentService: &fakeSubagentService{
			runs:   make(map[string]publicsubagent.Run),
			owners: make(map[string]string),
		},
	}
	sessionInfo := channel.sessionTracker.getOrCreateSession(
		baseSessionID,
		0,
	)

	service := channel.subagentService.(*fakeSubagentService)
	service.runs["run-a"] = publicsubagent.Run{
		ID:              "run-a",
		ParentSessionID: sessionInfo.sessionID,
		Status:          publicsubagent.StatusRunning,
		Task:            "inspect repository",
		Summary:         "still running",
	}
	service.owners["run-a"] = "user-a"
	service.runs["run-b"] = publicsubagent.Run{
		ID:              "run-b",
		ParentSessionID: "other-session",
		Status:          publicsubagent.StatusCompleted,
		Task:            "other scope",
	}
	service.owners["run-b"] = "user-a"

	sender := &mockSender{}
	err := channel.handleSubagentsCommand(
		context.Background(),
		"chat-1",
		"user-a",
		baseSessionID,
		parsedCommand{keyword: subagentsKeyword},
		sender,
	)
	require.NoError(t, err)
	require.Contains(t, sender.lastText, subagentListTitle)
	require.Contains(t, sender.lastText, "run-a")
	require.NotContains(t, sender.lastText, "run-b")
}

func TestHandleSubagentsCommandGetScopesCurrentSession(t *testing.T) {
	t.Parallel()

	baseSessionID := "wecom:dm:chat-1"
	channel := &Channel{
		sessionTracker: newSessionTracker(),
		subagentService: &fakeSubagentService{
			runs: map[string]publicsubagent.Run{
				"run-a": {
					ID:              "run-a",
					ParentSessionID: "other-session",
					Status:          publicsubagent.StatusCompleted,
					Task:            "other scope",
				},
			},
			owners: map[string]string{"run-a": "user-a"},
		},
	}

	sender := &mockSender{}
	err := channel.handleSubagentsCommand(
		context.Background(),
		"chat-1",
		"user-a",
		baseSessionID,
		parsedCommand{
			keyword: subagentsKeyword,
			args:    []string{subagentActionGet, "run-a"},
		},
		sender,
	)
	require.NoError(t, err)
	require.Contains(
		t,
		sender.lastText,
		subagentNotFoundMessage("run-a"),
	)
}

func TestHandleSubagentsCommandCancel(t *testing.T) {
	t.Parallel()

	baseSessionID := "wecom:dm:chat-1"
	channel := &Channel{
		sessionTracker: newSessionTracker(),
		subagentService: &fakeSubagentService{
			runs:   make(map[string]publicsubagent.Run),
			owners: map[string]string{"run-a": "user-a"},
		},
	}
	sessionInfo := channel.sessionTracker.getOrCreateSession(
		baseSessionID,
		0,
	)
	channel.subagentService.(*fakeSubagentService).runs["run-a"] =
		publicsubagent.Run{
			ID:              "run-a",
			ParentSessionID: sessionInfo.sessionID,
			Status:          publicsubagent.StatusRunning,
			Task:            "cancel me",
		}

	sender := &mockSender{}
	err := channel.handleSubagentsCommand(
		context.Background(),
		"chat-1",
		"user-a",
		baseSessionID,
		parsedCommand{
			keyword: subagentsKeyword,
			args:    []string{subagentActionCancel, "run-a"},
		},
		sender,
	)
	require.NoError(t, err)
	require.Contains(t, sender.lastText, subagentDetailTitle)
	require.Contains(t, sender.lastText, subagentStatusCanceled)
}

func TestHandleSubagentsCommandUnavailable(t *testing.T) {
	t.Parallel()

	channel := &Channel{
		sessionTracker: newSessionTracker(),
	}
	sender := &mockSender{}
	err := channel.handleSubagentsCommand(
		context.Background(),
		"chat-1",
		"user-a",
		"wecom:dm:chat-1",
		parsedCommand{keyword: subagentsKeyword},
		sender,
	)
	require.NoError(t, err)
	require.Contains(t, sender.lastText, subagentUnavailableMessage)
	require.Contains(t, sender.lastText, subagentsCommandUsage)
}
