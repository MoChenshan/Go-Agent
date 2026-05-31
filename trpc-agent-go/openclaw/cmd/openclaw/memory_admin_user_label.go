package main

import (
	"strings"
	"sync"

	wecomchannel "git.woa.com/trpc-go/trpc-agent-go/openclaw/channel/wecom"
	ocadmin "trpc.group/trpc-go/trpc-agent-go/openclaw/admin"
	occhannel "trpc.group/trpc-go/trpc-agent-go/openclaw/channel"
)

const (
	wecomDirectMessageSessionPrefix = "wecom:dm:"
	wecomScopedUserSeparator        = ":user:"

	memoryUserRTXPrefix     = "RTX "
	memoryUserLabelOpenWrap = " ("
	memoryUserLabelClose    = ")"
)

type runtimeMemoryUserLabelResolver struct {
	targets []wecomchannel.AdminTarget

	mu    sync.Mutex
	cache map[string]string
}

func newRuntimeMemoryUserLabelResolver(
	channels []occhannel.Channel,
) ocadmin.MemoryUserLabelResolver {
	targets := collectRuntimeWeComAdminTargets(channels)
	if len(targets) == 0 {
		return nil
	}
	return &runtimeMemoryUserLabelResolver{
		targets: targets,
		cache:   map[string]string{},
	}
}

func (r *runtimeMemoryUserLabelResolver) ResolveMemoryUserLabel(
	_ string,
	userID string,
) string {
	if r == nil {
		return ""
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ""
	}
	if label, ok := r.cachedLabel(userID); ok {
		return label
	}
	candidateID := memoryAdminKnownWeComUserID(userID)
	if candidateID == "" {
		r.storeLabel(userID, "")
		return ""
	}
	label := r.resolveKnownUserLabel(candidateID)
	r.storeLabel(userID, label)
	return label
}

func (r *runtimeMemoryUserLabelResolver) cachedLabel(
	userID string,
) (string, bool) {
	if r == nil {
		return "", false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	label, ok := r.cache[userID]
	return label, ok
}

func (r *runtimeMemoryUserLabelResolver) storeLabel(
	userID string,
	label string,
) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache[userID] = strings.TrimSpace(label)
}

func (r *runtimeMemoryUserLabelResolver) resolveKnownUserLabel(
	userID string,
) string {
	if r == nil {
		return ""
	}
	for _, target := range r.targets {
		identities := wecomchannel.ResolveKnownUserIdentities(
			target.StateDir,
			"",
			[]string{userID},
		)
		if len(identities) == 0 {
			continue
		}
		label := formatMemoryKnownUserLabel(identities[userID])
		if label != "" {
			return label
		}
	}
	return ""
}

func memoryAdminKnownWeComUserID(userID string) string {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ""
	}
	if looksLikeWeComUserID(userID) {
		return userID
	}
	if strings.HasPrefix(userID, wecomDirectMessageSessionPrefix) {
		return strings.TrimSpace(
			strings.TrimPrefix(userID, wecomDirectMessageSessionPrefix),
		)
	}
	index := strings.LastIndex(userID, wecomScopedUserSeparator)
	if index < 0 {
		return ""
	}
	return strings.TrimSpace(
		userID[index+len(wecomScopedUserSeparator):],
	)
}

func looksLikeWeComUserID(userID string) bool {
	userID = strings.TrimSpace(userID)
	if len(userID) < 2 {
		return false
	}
	return strings.HasPrefix(strings.ToUpper(userID), "T")
}

func formatMemoryKnownUserLabel(
	identity wecomchannel.KnownUserIdentity,
) string {
	account := strings.TrimSpace(identity.AccountName)
	display := strings.TrimSpace(identity.DisplayName)
	switch {
	case account != "" && display != "" &&
		!strings.EqualFold(account, display):
		return memoryUserRTXPrefix +
			account +
			memoryUserLabelOpenWrap +
			display +
			memoryUserLabelClose
	case account != "":
		return memoryUserRTXPrefix + account
	case display != "":
		return display
	default:
		return ""
	}
}
