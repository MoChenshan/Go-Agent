package weixin

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/delivery"
)

const (
	targetKeyAccountID = "account_id"
	targetKeyPeerID    = "peer_id"

	maxReplyRunes = 4000
)

type textTarget struct {
	AccountID string
	PeerID    string
}

func buildTextTarget(accountID string, peerID string) string {
	values := url.Values{}
	values.Set(targetKeyAccountID, strings.TrimSpace(accountID))
	values.Set(targetKeyPeerID, strings.TrimSpace(peerID))
	return values.Encode()
}

func parseTextTarget(raw string) (textTarget, error) {
	values, err := url.ParseQuery(strings.TrimSpace(raw))
	if err != nil {
		return textTarget{}, fmt.Errorf(
			"weixin target: parse target: %w",
			err,
		)
	}
	target := textTarget{
		AccountID: strings.TrimSpace(values.Get(targetKeyAccountID)),
		PeerID:    strings.TrimSpace(values.Get(targetKeyPeerID)),
	}
	if target.AccountID == "" || target.PeerID == "" {
		return textTarget{}, fmt.Errorf(
			"weixin target: missing %s or %s",
			targetKeyAccountID,
			targetKeyPeerID,
		)
	}
	return target, nil
}

func buildDeliveryTarget(
	accountID string,
	peerID string,
) delivery.Target {
	return delivery.Target{
		Channel: pluginType,
		Target:  buildTextTarget(accountID, peerID),
	}
}

func splitReplyText(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	return splitRunes(text, maxReplyRunes)
}

func splitRunes(text string, limit int) []string {
	if limit <= 0 {
		return []string{text}
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return []string{text}
	}

	parts := make([]string, 0, (len(runes)+limit-1)/limit)
	for start := 0; start < len(runes); start += limit {
		end := start + limit
		if end > len(runes) {
			end = len(runes)
		}
		parts = append(parts, string(runes[start:end]))
	}
	return parts
}

func nextClientID() string {
	return "weixin-" + uuid.NewString()
}

func (c *Channel) SendText(
	ctx context.Context,
	target string,
	text string,
) error {
	if c == nil {
		return fmt.Errorf("weixin channel: nil channel")
	}
	parsed, err := parseTextTarget(target)
	if err != nil {
		return err
	}
	account, ok := c.state.account(parsed.AccountID)
	if !ok {
		return fmt.Errorf(
			"weixin channel: unknown account %q",
			parsed.AccountID,
		)
	}
	return c.sendTextReply(
		ctx,
		account,
		parsed.PeerID,
		c.state.contextToken(parsed.AccountID, parsed.PeerID),
		text,
	)
}

func (c *Channel) sendTextReply(
	ctx context.Context,
	account Account,
	peerID string,
	contextToken string,
	text string,
) error {
	for _, part := range splitReplyText(text) {
		sendCtx, cancel := context.WithTimeout(
			ctx,
			defaultAPIRequestTimeout,
		)
		err := c.api.sendText(
			sendCtx,
			account,
			peerID,
			contextToken,
			nextClientID(),
			part,
		)
		cancel()
		if err != nil {
			if isSessionExpiredError(err) {
				_ = c.pauseAccount(account.AccountID, err)
			}
			return err
		}
	}
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return c.state.markOutbound(account.AccountID)
}
