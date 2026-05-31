package progress

import "strings"

const (
	defaultMilestoneLimit = 4
)

type Kind string

const (
	KindActivity  Kind = "activity"
	KindMilestone Kind = "milestone"
	KindDraft     Kind = "draft"
	KindDone      Kind = "done"
	KindError     Kind = "error"
)

type Event struct {
	Kind    Kind
	Message string
}

type Snapshot struct {
	Activity   string
	Milestones []string
	Draft      string
	Done       bool
	Error      string
}

type State struct {
	activity       string
	milestones     []string
	draft          string
	done           bool
	err            string
	milestoneLimit int
}

func NewState() *State {
	return &State{
		milestoneLimit: defaultMilestoneLimit,
	}
}

func (s *State) Apply(event Event) bool {
	if s == nil {
		return false
	}

	message := strings.TrimSpace(event.Message)

	switch event.Kind {
	case KindActivity:
		if message == "" || s.activity == message {
			return false
		}
		s.activity = message
		return true
	case KindMilestone:
		if message == "" {
			return false
		}
		if s.hasMilestone(message) {
			return false
		}
		s.milestones = append(s.milestones, message)
		if s.milestoneLimit > 0 &&
			len(s.milestones) > s.milestoneLimit {
			s.milestones = s.milestones[len(s.milestones)-s.milestoneLimit:]
		}
		return true
	case KindDraft:
		if message == "" {
			return false
		}
		s.draft += message
		return true
	case KindDone:
		if s.done {
			return false
		}
		s.done = true
		return true
	case KindError:
		if message == "" || s.err == message {
			return false
		}
		s.err = message
		return true
	default:
		return false
	}
}

func (s *State) Snapshot() Snapshot {
	if s == nil {
		return Snapshot{}
	}

	out := Snapshot{
		Activity: s.activity,
		Draft:    s.draft,
		Done:     s.done,
		Error:    s.err,
	}
	if len(s.milestones) > 0 {
		out.Milestones = append(
			[]string(nil),
			s.milestones...,
		)
	}
	return out
}

func RenderNarrative(
	snapshot Snapshot,
	activity string,
) string {
	parts := make([]string, 0, len(snapshot.Milestones)+2)
	parts = append(parts, snapshot.Milestones...)

	if snapshot.Draft != "" {
		parts = append(parts, snapshot.Draft)
	} else if strings.TrimSpace(activity) != "" {
		parts = append(parts, strings.TrimSpace(activity))
	}

	if snapshot.Draft == "" && snapshot.Error != "" {
		parts = append(parts, snapshot.Error)
	}

	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func (s *State) hasMilestone(message string) bool {
	for _, milestone := range s.milestones {
		if milestone == message {
			return true
		}
	}
	return false
}
