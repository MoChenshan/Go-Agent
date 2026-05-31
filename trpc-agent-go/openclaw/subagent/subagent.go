package subagent

import upstream "trpc.group/trpc-go/trpc-agent-go/openclaw/subagent"

const (
	RuntimeStateKeyRun             = upstream.RuntimeStateKeyRun
	RuntimeStateKeyRunID           = upstream.RuntimeStateKeyRunID
	RuntimeStateKeyParentSessionID = upstream.RuntimeStateKeyParentSessionID
)

var ErrRunNotFound = upstream.ErrRunNotFound

var ErrRunAlreadyExists = upstream.ErrRunAlreadyExists

var ErrNotStarted = upstream.ErrNotStarted

type Status = upstream.Status

const (
	StatusQueued    = upstream.StatusQueued
	StatusRunning   = upstream.StatusRunning
	StatusCompleted = upstream.StatusCompleted
	StatusFailed    = upstream.StatusFailed
	StatusCanceled  = upstream.StatusCanceled
)

type Run = upstream.Run

type ListFilter = upstream.ListFilter

type Service = upstream.Service
