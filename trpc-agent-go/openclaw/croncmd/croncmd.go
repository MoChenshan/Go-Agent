package croncmd

import (
	"errors"
	"strconv"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/openclaw/gwclient"
)

const (
	ActionHelp   = "help"
	ActionList   = "list"
	ActionStatus = "status"
	ActionStop   = "stop"
	ActionResume = "resume"
	ActionRemove = "remove"
	ActionClear  = "clear"

	shortIDRunes = 8
)

type Parsed struct {
	Action   string
	Selector string
}

func Parse(raw string) (Parsed, error) {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return Parsed{Action: ActionHelp}, nil
	}
	action := strings.ToLower(strings.TrimSpace(fields[0]))
	switch action {
	case ActionHelp, ActionList, ActionClear:
		if len(fields) != 1 {
			return Parsed{}, errors.New("croncmd: invalid arguments")
		}
		return Parsed{Action: action}, nil
	case ActionStatus, ActionStop, ActionResume, ActionRemove:
		if len(fields) != 2 {
			return Parsed{}, errors.New("croncmd: selector required")
		}
		return Parsed{
			Action:   action,
			Selector: strings.TrimSpace(fields[1]),
		}, nil
	default:
		return Parsed{}, errors.New("croncmd: unknown action")
	}
}

func ResolveSelector(
	jobs []gwclient.ScheduledJobSummary,
	selector string,
) (gwclient.ScheduledJobSummary, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return gwclient.ScheduledJobSummary{},
			errors.New("croncmd: selector required")
	}
	if index, err := strconv.Atoi(selector); err == nil {
		if index >= 1 && index <= len(jobs) {
			return jobs[index-1], nil
		}
		return gwclient.ScheduledJobSummary{},
			errors.New("croncmd: selector out of range")
	}
	for _, job := range jobs {
		if sameJobSelector(job.ID, selector) {
			return job, nil
		}
	}
	for _, job := range jobs {
		if sameJobSelector(job.Name, selector) {
			return job, nil
		}
	}
	for _, job := range jobs {
		if sameJobSelector(ShortID(job.ID), selector) {
			return job, nil
		}
	}
	return gwclient.ScheduledJobSummary{},
		errors.New("croncmd: job not found")
}

func ShortID(id string) string {
	runes := []rune(strings.TrimSpace(id))
	if len(runes) <= shortIDRunes {
		return string(runes)
	}
	return string(runes[:shortIDRunes])
}

func sameJobSelector(value string, selector string) bool {
	return strings.EqualFold(
		strings.TrimSpace(value),
		strings.TrimSpace(selector),
	)
}
