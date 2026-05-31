package weixin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"git.woa.com/trpc-go/trpc-agent-go/openclaw/releaseinfo"
	"trpc.group/trpc-go/trpc-agent-go/log"
	occhannel "trpc.group/trpc-go/trpc-agent-go/openclaw/channel"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/delivery"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/gwclient"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/registry"
)

const (
	pluginType = "weixin"

	defaultErrorBackoff   = 30 * time.Second
	defaultAccountRefresh = 5 * time.Second
	typingTicketTTL       = 5 * time.Minute

	notAllowedMessage = "This Weixin channel does not allow " +
		"your account."
	textOnlyMessage = "This Weixin channel currently supports " +
		"text input only."

	runtimeCommandRoot      = "/runtime"
	runtimeCommandStatus    = "status"
	runtimeCommandVersions  = "versions"
	runtimeCommandChangelog = "changelog"

	versionSummaryLimit   = 5
	changelogSummaryLimit = 5
)

var _ occhannel.TextSender = (*Channel)(nil)

type channelCfg struct {
	StateDir              string   `yaml:"state_dir,omitempty"`
	BaseURL               string   `yaml:"base_url,omitempty"`
	PollTimeout           string   `yaml:"poll_timeout,omitempty"`
	ErrorBackoff          string   `yaml:"error_backoff,omitempty"`
	EnableTyping          *bool    `yaml:"enable_typing,omitempty"`
	EnableRuntimeCommands *bool    `yaml:"enable_runtime_commands,omitempty"`
	AllowUsers            []string `yaml:"allow_users,omitempty"`
	ReleaseBaseURL        string   `yaml:"release_base_url,omitempty"`
}

type Channel struct {
	gateway registry.GatewayClient
	api     *apiClient
	state   *channelState

	name     string
	stateDir string
	baseURL  string

	pollTimeout           time.Duration
	errorBackoff          time.Duration
	enableTyping          bool
	enableRuntimeCommands bool

	allowUsers map[string]struct{}

	releaseClient releaseinfo.Client
	typingTickets *typingTicketCache

	accountRefreshInterval time.Duration
}

type typingTicketCache struct {
	mu      sync.RWMutex
	entries map[string]typingTicketEntry
}

type typingTicketEntry struct {
	Value     string
	ExpiresAt time.Time
}

func init() {
	if err := registry.RegisterChannel(pluginType, func(
		deps registry.ChannelDeps,
		spec registry.PluginSpec,
	) (occhannel.Channel, error) {
		return newChannel(deps, spec)
	}); err != nil {
		panic(err)
	}
}

func New(
	deps registry.ChannelDeps,
	spec registry.PluginSpec,
) (occhannel.Channel, error) {
	return newChannel(deps, spec)
}

func newChannel(
	deps registry.ChannelDeps,
	spec registry.PluginSpec,
) (occhannel.Channel, error) {
	if deps.Gateway == nil {
		return nil, fmt.Errorf("weixin channel: nil gateway")
	}

	var cfg channelCfg
	if err := registry.DecodeStrict(spec.Config, &cfg); err != nil {
		return nil, err
	}

	pollTimeout, err := parseDurationWithDefault(
		cfg.PollTimeout,
		defaultPollTimeout,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"weixin channel: invalid poll_timeout: %w",
			err,
		)
	}
	errorBackoff, err := parseDurationWithDefault(
		cfg.ErrorBackoff,
		defaultErrorBackoff,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"weixin channel: invalid error_backoff: %w",
			err,
		)
	}

	stateDir := resolveStateDir(deps.StateDir, cfg.StateDir)
	state, err := loadChannelState(stateDir)
	if err != nil {
		return nil, err
	}

	channel := &Channel{
		gateway: deps.Gateway,
		api: newAPIClient(&http.Client{
			Timeout: pollTimeout + defaultPollTimeoutGrace,
		}),
		state:        state,
		name:         strings.TrimSpace(spec.Name),
		stateDir:     stateDir,
		baseURL:      strings.TrimSpace(cfg.BaseURL),
		pollTimeout:  pollTimeout,
		errorBackoff: errorBackoff,
		enableTyping: resolveBool(cfg.EnableTyping, true),
		enableRuntimeCommands: resolveBool(
			cfg.EnableRuntimeCommands,
			true,
		),
		allowUsers: buildAllowSet(cfg.AllowUsers, deps.AllowUsers),
		releaseClient: releaseinfo.Client{
			BaseURL: defaultString(
				cfg.ReleaseBaseURL,
				releaseinfo.DefaultBaseURL,
			),
			HTTPClient: &http.Client{
				Timeout: defaultAPIRequestTimeout,
			},
		},
		typingTickets:          newTypingTicketCache(),
		accountRefreshInterval: defaultAccountRefresh,
	}

	for _, account := range state.accountsSlice() {
		if strings.TrimSpace(account.BaseURL) != "" {
			continue
		}
		account.BaseURL = channel.baseURL
		if err := channel.state.saveAccount(account); err != nil {
			return nil, err
		}
	}

	return channel, nil
}

func (c *Channel) ID() string {
	return pluginType
}

func (c *Channel) Run(ctx context.Context) error {
	type accountLoop struct {
		cancel context.CancelFunc
		done   chan struct{}
	}

	loops := make(map[string]accountLoop)
	ticker := time.NewTicker(c.accountRefreshInterval)
	defer ticker.Stop()

	reconcile := func(
		warnedNoAccounts bool,
	) bool {
		for accountID, loop := range loops {
			select {
			case <-loop.done:
				delete(loops, accountID)
			default:
			}
		}

		accounts, err := c.state.syncAccountsFromDisk()
		if err != nil {
			log.ErrorfContext(
				ctx,
				"weixin: sync accounts under %s: %v",
				c.stateDir,
				err,
			)
			return warnedNoAccounts
		}
		if len(accounts) == 0 {
			if !warnedNoAccounts {
				log.WarnfContext(
					ctx,
					"weixin: no accounts found under %s",
					c.stateDir,
				)
			}
			warnedNoAccounts = true
		} else {
			warnedNoAccounts = false
		}

		desired := make(map[string]struct{}, len(accounts))
		for _, account := range accounts {
			accountID := strings.TrimSpace(account.AccountID)
			if accountID == "" {
				continue
			}
			desired[accountID] = struct{}{}
			if _, ok := loops[accountID]; ok {
				continue
			}

			accountCtx, cancel := context.WithCancel(ctx)
			done := make(chan struct{})
			loops[accountID] = accountLoop{
				cancel: cancel,
				done:   done,
			}
			go func(accountID string, done chan struct{}) {
				defer close(done)
				c.runAccountLoop(accountCtx, accountID)
			}(accountID, done)
		}

		for accountID, loop := range loops {
			if _, ok := desired[accountID]; ok {
				continue
			}
			loop.cancel()
			delete(loops, accountID)
		}
		return warnedNoAccounts
	}

	warnedNoAccounts := reconcile(false)
	for {
		select {
		case <-ctx.Done():
			for _, loop := range loops {
				loop.cancel()
			}
			for _, loop := range loops {
				<-loop.done
			}
			return nil
		case <-ticker.C:
			warnedNoAccounts = reconcile(warnedNoAccounts)
		}
	}
}

func (c *Channel) runAccountLoop(
	ctx context.Context,
	accountID string,
) {
	logPrefix := logWithAccount(accountID)
	missingTokenLogged := false
	for {
		if ctx.Err() != nil {
			return
		}

		account, ok := c.state.account(accountID)
		if !ok {
			return
		}
		if strings.TrimSpace(account.Token) == "" {
			if !missingTokenLogged {
				log.Warnf(
					"%s skip account without token",
					logPrefix,
				)
			}
			missingTokenLogged = true
			if err := sleepWithContext(ctx, c.errorBackoff); err != nil {
				return
			}
			continue
		}
		missingTokenLogged = false

		if remaining, err := c.state.pauseRemaining(
			account.AccountID,
			time.Now(),
		); err != nil {
			log.Errorf("%s load pause state: %v", logPrefix, err)
		} else if remaining > 0 {
			log.Infof(
				"%s account paused for %s",
				logPrefix,
				remaining.Round(time.Second),
			)
			if err := sleepWithContext(ctx, remaining); err != nil {
				return
			}
			continue
		}

		cursor := c.state.cursor(account.AccountID)
		rsp, err := c.api.getUpdates(
			ctx,
			account,
			cursor,
			c.pollTimeout,
		)
		if err != nil {
			if isSessionExpiredError(err) {
				_ = c.pauseAccount(account.AccountID, err)
			} else {
				_ = c.state.recordError(account.AccountID, err.Error())
				log.Errorf("%s getupdates failed: %v", logPrefix, err)
			}
			if err := sleepWithContext(ctx, c.errorBackoff); err != nil {
				return
			}
			continue
		}

		if rsp.ErrCode == sessionExpiredErrCode {
			_ = c.pauseAccount(
				account.AccountID,
				&apiResponseError{
					Endpoint: endpointGetUpdates,
					Ret:      rsp.Ret,
					ErrCode:  rsp.ErrCode,
					ErrMsg:   rsp.ErrMsg,
				},
			)
			continue
		}
		if rsp.ErrCode != 0 || rsp.Ret != 0 {
			err := &apiResponseError{
				Endpoint: endpointGetUpdates,
				Ret:      rsp.Ret,
				ErrCode:  rsp.ErrCode,
				ErrMsg:   rsp.ErrMsg,
			}
			_ = c.state.recordError(account.AccountID, err.Error())
			log.Errorf(
				"%s getupdates returned error: %v",
				logPrefix,
				err,
			)
			if err := sleepWithContext(ctx, c.errorBackoff); err != nil {
				return
			}
			continue
		}

		if rsp.GetUpdatesBuf != "" && rsp.GetUpdatesBuf != cursor {
			if err := c.state.setCursor(
				account.AccountID,
				rsp.GetUpdatesBuf,
			); err != nil {
				log.Errorf("%s save cursor failed: %v", logPrefix, err)
			}
		}

		for _, msg := range rsp.Messages {
			if err := c.handleInboundMessage(
				ctx,
				account,
				msg,
			); err != nil {
				_ = c.state.recordError(account.AccountID, err.Error())
				log.Errorf(
					"%s handle inbound failed: %v",
					logPrefix,
					err,
				)
			}
		}
	}
}

func (c *Channel) handleInboundMessage(
	ctx context.Context,
	account Account,
	msg weixinMessage,
) error {
	if msg.MessageType == messageTypeBot {
		return nil
	}

	peerID := strings.TrimSpace(msg.FromUserID)
	if peerID == "" {
		return nil
	}
	if !c.userAllowed(peerID) {
		return c.sendTextReply(
			ctx,
			account,
			peerID,
			msg.ContextToken,
			notAllowedMessage,
		)
	}

	if msg.ContextToken != "" {
		if err := c.state.setContextToken(
			account.AccountID,
			peerID,
			msg.ContextToken,
		); err != nil {
			return err
		}
	}
	if err := c.state.markInbound(account.AccountID); err != nil {
		return err
	}

	text, hasNonText := extractMessageText(msg.ItemList)
	if strings.TrimSpace(text) == "" {
		if hasNonText {
			return c.sendTextReply(
				ctx,
				account,
				peerID,
				msg.ContextToken,
				textOnlyMessage,
			)
		}
		return nil
	}

	if c.enableRuntimeCommands &&
		strings.HasPrefix(strings.TrimSpace(text), runtimeCommandRoot) {
		return c.handleRuntimeCommand(
			ctx,
			account,
			peerID,
			msg.ContextToken,
			text,
		)
	}

	stopTyping := c.startTyping(
		ctx,
		account,
		peerID,
		msg.ContextToken,
	)
	defer stopTyping()

	req, err := c.buildGatewayRequest(account, msg, text)
	if err != nil {
		return err
	}
	rsp, err := c.gateway.SendMessage(ctx, req)
	if err != nil {
		return err
	}
	if rsp.Ignored || strings.TrimSpace(rsp.Reply) == "" {
		return nil
	}
	return c.sendTextReply(
		ctx,
		account,
		peerID,
		msg.ContextToken,
		rsp.Reply,
	)
}

func (c *Channel) buildGatewayRequest(
	account Account,
	msg weixinMessage,
	text string,
) (gwclient.MessageRequest, error) {
	sessionID := buildSessionID(account.AccountID, msg.FromUserID)
	messageID := buildMessageID(account.AccountID, msg)
	requestID := buildRequestID(account.AccountID, messageID)

	extensions, err := delivery.MergeRequestExtension(
		nil,
		buildDeliveryTarget(account.AccountID, msg.FromUserID),
	)
	if err != nil {
		return gwclient.MessageRequest{}, err
	}
	if msg.ContextToken != "" {
		extensions, err = mergeWeixinExtension(
			extensions,
			weixinRequestExtension{
				AccountID:    account.AccountID,
				PeerID:       msg.FromUserID,
				ContextToken: msg.ContextToken,
			},
		)
		if err != nil {
			return gwclient.MessageRequest{}, err
		}
	}

	return gwclient.MessageRequest{
		Channel:    pluginType,
		From:       strings.TrimSpace(msg.FromUserID),
		To:         strings.TrimSpace(msg.ToUserID),
		MessageID:  messageID,
		Text:       strings.TrimSpace(text),
		UserID:     strings.TrimSpace(msg.FromUserID),
		SessionID:  sessionID,
		RequestID:  requestID,
		Extensions: extensions,
	}, nil
}

func (c *Channel) startTyping(
	ctx context.Context,
	account Account,
	peerID string,
	contextToken string,
) func() {
	if !c.enableTyping {
		return func() {}
	}

	ticket, err := c.ensureTypingTicket(
		ctx,
		account,
		peerID,
		contextToken,
	)
	if err != nil || ticket == "" {
		return func() {}
	}

	sendCtx, cancel := context.WithTimeout(
		ctx,
		defaultConfigRequestTimer,
	)
	err = c.api.sendTypingStatus(
		sendCtx,
		account,
		peerID,
		ticket,
		typingStatusActive,
	)
	cancel()
	if err != nil {
		if isSessionExpiredError(err) {
			_ = c.pauseAccount(account.AccountID, err)
		}
		return func() {}
	}

	return func() {
		stopCtx, stopCancel := context.WithTimeout(
			context.Background(),
			defaultConfigRequestTimer,
		)
		defer stopCancel()
		err := c.api.sendTypingStatus(
			stopCtx,
			account,
			peerID,
			ticket,
			typingStatusCancel,
		)
		if err != nil && isSessionExpiredError(err) {
			_ = c.pauseAccount(account.AccountID, err)
		}
	}
}

func (c *Channel) ensureTypingTicket(
	ctx context.Context,
	account Account,
	peerID string,
	contextToken string,
) (string, error) {
	if ticket, ok := c.typingTickets.Get(
		account.AccountID,
		peerID,
		time.Now(),
	); ok {
		return ticket, nil
	}

	getCtx, cancel := context.WithTimeout(
		ctx,
		defaultConfigRequestTimer,
	)
	defer cancel()
	ticket, err := c.api.getTypingTicket(
		getCtx,
		account,
		peerID,
		contextToken,
	)
	if err != nil {
		return "", err
	}
	if ticket == "" {
		return "", nil
	}
	c.typingTickets.Put(account.AccountID, peerID, ticket, time.Now())
	return ticket, nil
}

func (c *Channel) handleRuntimeCommand(
	ctx context.Context,
	account Account,
	peerID string,
	contextToken string,
	text string,
) error {
	response, err := c.runtimeCommandResponse(ctx, account, text)
	if err != nil {
		response = "Runtime command failed: " + err.Error()
	}
	return c.sendTextReply(
		ctx,
		account,
		peerID,
		contextToken,
		response,
	)
}

func (c *Channel) runtimeCommandResponse(
	ctx context.Context,
	account Account,
	text string,
) (string, error) {
	action, arg := parseRuntimeCommand(text)
	switch action {
	case "":
		return buildRuntimeHelpText(), nil
	case runtimeCommandStatus:
		return c.runtimeStatusText(account), nil
	case runtimeCommandVersions:
		index, err := c.releaseClient.FetchIndex(ctx)
		if err != nil {
			return "", err
		}
		return formatVersionIndex(index), nil
	case runtimeCommandChangelog:
		summary, version, err := c.fetchChangelogSummary(ctx, arg)
		if err != nil {
			return "", err
		}
		return formatChangelogSummary(version, summary), nil
	default:
		return buildRuntimeHelpText(), nil
	}
}

func (c *Channel) runtimeStatusText(account Account) string {
	status := c.state.statusSnapshot(account.AccountID)
	lines := []string{
		"Runtime status",
		"Current version: " + currentRuntimeVersion(),
		"Account: " + account.AccountID,
	}
	if status.PausedUntil != nil {
		lines = append(
			lines,
			"Paused until: "+status.PausedUntil.Format(time.RFC3339),
		)
	} else {
		lines = append(lines, "Paused until: no")
	}
	lines = append(lines, "Last event: "+formatTimePtr(status.LastEventAt))
	lines = append(
		lines,
		"Last inbound: "+formatTimePtr(status.LastInboundAt),
	)
	lines = append(
		lines,
		"Last outbound: "+formatTimePtr(status.LastOutboundAt),
	)
	if status.LastError != "" {
		lines = append(lines, "Last error: "+status.LastError)
	}
	return strings.Join(lines, "\n")
}

func (c *Channel) fetchChangelogSummary(
	ctx context.Context,
	version string,
) ([]string, string, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		latest, err := c.releaseClient.FetchLatestVersion(ctx)
		if err != nil {
			return nil, "", err
		}
		version = latest
	}
	summary, err := c.releaseClient.FetchChangeSummary(
		ctx,
		version,
		changelogSummaryLimit,
	)
	if err != nil {
		return nil, "", err
	}
	return summary, version, nil
}

func (c *Channel) pauseAccount(
	accountID string,
	err error,
) error {
	until := time.Now().Add(defaultPauseDuration)
	message := err.Error()
	log.Warnf(
		"%s pausing account until %s: %v",
		logWithAccount(accountID),
		until.Format(time.RFC3339),
		err,
	)
	return c.state.markPaused(accountID, until, message)
}

func (c *Channel) userAllowed(userID string) bool {
	if len(c.allowUsers) == 0 {
		return true
	}
	_, ok := c.allowUsers[strings.TrimSpace(userID)]
	return ok
}

func buildAllowSet(
	configured []string,
	inherited []string,
) map[string]struct{} {
	values := configured
	if len(values) == 0 {
		values = inherited
	}
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out[value] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseDurationWithDefault(
	raw string,
	defaultValue time.Duration,
) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultValue, nil
	}
	return time.ParseDuration(raw)
}

func resolveBool(value *bool, defaultValue bool) bool {
	if value == nil {
		return defaultValue
	}
	return *value
}

func defaultString(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}

func extractMessageText(items []messageItem) (string, bool) {
	hasNonText := false
	for _, item := range items {
		switch item.Type {
		case messageItemTypeText:
			if item.TextItem != nil &&
				strings.TrimSpace(item.TextItem.Text) != "" {
				return strings.TrimSpace(item.TextItem.Text), hasNonText
			}
		case messageItemTypeVoice:
			if item.VoiceItem != nil &&
				strings.TrimSpace(item.VoiceItem.Text) != "" {
				return strings.TrimSpace(item.VoiceItem.Text), hasNonText
			}
			hasNonText = true
		case messageItemTypeImage, messageItemTypeFile, messageItemTypeVideo:
			hasNonText = true
		}
	}
	return "", hasNonText
}

func buildSessionID(accountID string, peerID string) string {
	return strings.Join(
		[]string{
			pluginType,
			"dm",
			strings.TrimSpace(accountID),
			strings.TrimSpace(peerID),
		},
		":",
	)
}

func buildMessageID(accountID string, msg weixinMessage) string {
	switch {
	case msg.MessageID != 0:
		return fmt.Sprintf(
			"%s:%d",
			strings.TrimSpace(accountID),
			msg.MessageID,
		)
	case strings.TrimSpace(msg.ClientID) != "":
		return strings.TrimSpace(accountID) + ":" +
			strings.TrimSpace(msg.ClientID)
	case msg.Seq != 0:
		return fmt.Sprintf(
			"%s:seq:%d",
			strings.TrimSpace(accountID),
			msg.Seq,
		)
	default:
		return nextClientID()
	}
}

func buildRequestID(accountID string, messageID string) string {
	return strings.Join(
		[]string{
			pluginType,
			strings.TrimSpace(accountID),
			strings.TrimSpace(messageID),
		},
		":",
	)
}

func buildRuntimeHelpText() string {
	return strings.Join([]string{
		"Runtime commands:",
		runtimeCommandRoot + " " + runtimeCommandStatus,
		runtimeCommandRoot + " " + runtimeCommandVersions,
		runtimeCommandRoot + " " + runtimeCommandChangelog +
			" [version]",
	}, "\n")
}

func parseRuntimeCommand(text string) (string, string) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 || fields[0] != runtimeCommandRoot {
		return "", ""
	}
	if len(fields) == 1 {
		return "", ""
	}
	action := strings.ToLower(strings.TrimSpace(fields[1]))
	arg := ""
	if len(fields) > 2 {
		arg = strings.TrimSpace(fields[2])
	}
	return action, arg
}

func formatVersionIndex(index releaseinfo.Index) string {
	lines := []string{
		"Available versions",
		"Latest: " + strings.TrimSpace(index.LatestVersion),
	}
	entries := append([]releaseinfo.Entry(nil), index.Versions...)
	sort.Slice(entries, func(i int, j int) bool {
		return entries[i].PublishedAt.After(entries[j].PublishedAt)
	})
	limit := versionSummaryLimit
	if len(entries) < limit {
		limit = len(entries)
	}
	for i := 0; i < limit; i++ {
		entry := entries[i]
		line := "- " + strings.TrimSpace(entry.Version)
		if !entry.PublishedAt.IsZero() {
			line += " (" + entry.PublishedAt.Format("2006-01-02") + ")"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func formatChangelogSummary(
	version string,
	summary []string,
) string {
	lines := []string{"Changelog: " + strings.TrimSpace(version)}
	if len(summary) == 0 {
		lines = append(lines, "- no summary available")
		return strings.Join(lines, "\n")
	}
	for _, item := range summary {
		lines = append(lines, "- "+strings.TrimSpace(item))
	}
	return strings.Join(lines, "\n")
}

func formatTimePtr(value *time.Time) string {
	if value == nil {
		return "never"
	}
	return value.Format(time.RFC3339)
}

func sleepWithContext(
	ctx context.Context,
	duration time.Duration,
) error {
	if duration <= 0 {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func logWithAccount(accountID string) string {
	return "weixin[" + strings.TrimSpace(accountID) + "]"
}

type weixinRequestExtension struct {
	AccountID    string `json:"account_id,omitempty"`
	PeerID       string `json:"peer_id,omitempty"`
	ContextToken string `json:"context_token,omitempty"`
}

const weixinExtensionKey = "openclaw:weixin:v1"

func mergeWeixinExtension(
	extensions map[string]json.RawMessage,
	annotation weixinRequestExtension,
) (map[string]json.RawMessage, error) {
	raw, err := json.Marshal(annotation)
	if err != nil {
		return nil, err
	}
	if extensions == nil {
		extensions = make(map[string]json.RawMessage)
	}
	extensions[weixinExtensionKey] = raw
	return extensions, nil
}

func newTypingTicketCache() *typingTicketCache {
	return &typingTicketCache{
		entries: make(map[string]typingTicketEntry),
	}
}

func (c *typingTicketCache) Get(
	accountID string,
	peerID string,
	now time.Time,
) (string, bool) {
	if c == nil {
		return "", false
	}
	key := typingTicketCacheKey(accountID, peerID)

	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		return "", false
	}
	if !now.Before(entry.ExpiresAt) {
		c.mu.Lock()
		delete(c.entries, key)
		c.mu.Unlock()
		return "", false
	}
	return entry.Value, entry.Value != ""
}

func (c *typingTicketCache) Put(
	accountID string,
	peerID string,
	value string,
	now time.Time,
) {
	if c == nil || strings.TrimSpace(value) == "" {
		return
	}
	key := typingTicketCacheKey(accountID, peerID)
	c.mu.Lock()
	c.entries[key] = typingTicketEntry{
		Value:     strings.TrimSpace(value),
		ExpiresAt: now.Add(typingTicketTTL),
	}
	c.mu.Unlock()
}

func typingTicketCacheKey(accountID string, peerID string) string {
	return strings.TrimSpace(accountID) + "\n" +
		strings.TrimSpace(peerID)
}

const (
	buildInfoVersionUnknown = "dev"
	buildInfoVersionDevel   = "(devel)"
	buildInfoRevisionKey    = "vcs.revision"
	shortCommitLength       = 7
)

func currentRuntimeVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info == nil {
		return buildInfoVersionUnknown
	}
	mainVersion := strings.TrimSpace(info.Main.Version)
	if mainVersion != "" && mainVersion != buildInfoVersionDevel {
		return mainVersion
	}
	for _, setting := range info.Settings {
		if strings.TrimSpace(setting.Key) != buildInfoRevisionKey {
			continue
		}
		commit := strings.TrimSpace(setting.Value)
		if len(commit) > shortCommitLength {
			commit = commit[:shortCommitLength]
		}
		if commit != "" {
			return "dev-" + commit
		}
	}
	return buildInfoVersionUnknown
}
