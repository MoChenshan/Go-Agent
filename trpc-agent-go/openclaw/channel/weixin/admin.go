package weixin

import (
	"strings"
	"time"
)

type AdminTarget struct {
	Name                  string
	StateDir              string
	DefaultBaseURL        string
	PollTimeout           time.Duration
	ErrorBackoff          time.Duration
	EnableTyping          bool
	EnableRuntimeCommands bool
}

type AdminAccountState struct {
	Account          Account       `json:"account"`
	Status           RuntimeStatus `json:"status"`
	ContextPeerCount int           `json:"context_peer_count"`
}

func (c *Channel) WeixinAdminTarget() AdminTarget {
	if c == nil {
		return AdminTarget{}
	}
	return AdminTarget{
		Name:     strings.TrimSpace(c.name),
		StateDir: strings.TrimSpace(c.stateDir),
		DefaultBaseURL: defaultString(
			c.baseURL,
			defaultBaseURL,
		),
		PollTimeout:           c.pollTimeout,
		ErrorBackoff:          c.errorBackoff,
		EnableTyping:          c.enableTyping,
		EnableRuntimeCommands: c.enableRuntimeCommands,
	}
}

func ListAdminAccountStates(
	stateRoot string,
) ([]AdminAccountState, error) {
	state, err := loadChannelState(stateRoot)
	if err != nil {
		return nil, err
	}

	accounts := state.accountsSlice()
	out := make([]AdminAccountState, 0, len(accounts))
	for _, account := range accounts {
		accountID := strings.TrimSpace(account.AccountID)
		out = append(out, AdminAccountState{
			Account:          account,
			Status:           state.statusSnapshot(accountID),
			ContextPeerCount: state.contextPeerCount(accountID),
		})
	}
	return out, nil
}

func ResumeAccount(
	stateRoot string,
	accountID string,
) error {
	state, err := loadChannelState(stateRoot)
	if err != nil {
		return err
	}
	return state.resumeAccount(accountID)
}
