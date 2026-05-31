package weixin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	stateDirName = "weixin"

	accountsDirName = "accounts"
	runtimeDirName  = "runtime"

	accountIndexFileName = "index.json"
	cursorFileSuffix     = ".sync.json"
	contextFileSuffix    = ".context.json"
	statusFileSuffix     = ".status.json"
	accountFileSuffix    = ".json"
	privateDirMode       = 0o700
	privateFileMode      = 0o600
	atomicFilePattern    = ".tmp-*"
)

type Account struct {
	AccountID string    `json:"account_id"`
	Token     string    `json:"token,omitempty"`
	BaseURL   string    `json:"base_url,omitempty"`
	UserID    string    `json:"user_id,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

type RuntimeStatus struct {
	AccountID      string     `json:"account_id,omitempty"`
	PausedUntil    *time.Time `json:"paused_until,omitempty"`
	LastEventAt    *time.Time `json:"last_event_at,omitempty"`
	LastInboundAt  *time.Time `json:"last_inbound_at,omitempty"`
	LastOutboundAt *time.Time `json:"last_outbound_at,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	UpdatedAt      *time.Time `json:"updated_at,omitempty"`
}

type channelState struct {
	root string

	mu sync.RWMutex

	accounts      map[string]Account
	cursors       map[string]string
	contextTokens map[string]map[string]string
	statuses      map[string]RuntimeStatus
}

func ResolveStateDir(globalStateDir string, override string) string {
	override = strings.TrimSpace(override)
	if override != "" {
		return override
	}
	globalStateDir = strings.TrimSpace(globalStateDir)
	if globalStateDir == "" {
		return stateDirName
	}
	return filepath.Join(globalStateDir, stateDirName)
}

func resolveStateDir(globalStateDir string, override string) string {
	return ResolveStateDir(globalStateDir, override)
}

func ListAccounts(stateRoot string) ([]Account, error) {
	state, err := loadChannelState(stateRoot)
	if err != nil {
		return nil, err
	}
	return state.accountsSlice(), nil
}

func SaveAccount(stateRoot string, account Account) error {
	state, err := loadChannelState(stateRoot)
	if err != nil {
		return err
	}
	return state.saveAccount(account)
}

func RemoveAccount(stateRoot string, accountID string) error {
	state, err := loadChannelState(stateRoot)
	if err != nil {
		return err
	}
	return state.removeAccount(accountID)
}

func LoadRuntimeStatus(
	stateRoot string,
	accountID string,
) (RuntimeStatus, error) {
	state, err := loadChannelState(stateRoot)
	if err != nil {
		return RuntimeStatus{}, err
	}
	return state.statusSnapshot(accountID), nil
}

func loadChannelState(stateRoot string) (*channelState, error) {
	root := strings.TrimSpace(stateRoot)
	if root == "" {
		root = stateDirName
	}
	state := &channelState{
		root:          root,
		accounts:      make(map[string]Account),
		cursors:       make(map[string]string),
		contextTokens: make(map[string]map[string]string),
		statuses:      make(map[string]RuntimeStatus),
	}

	accounts, err := loadAccounts(root)
	if err != nil {
		return nil, err
	}
	for _, account := range accounts {
		state.accounts[account.AccountID] = account

		cursor, err := readCursor(root, account.AccountID)
		if err != nil {
			return nil, err
		}
		if cursor != "" {
			state.cursors[account.AccountID] = cursor
		}

		tokens, err := readContextTokens(root, account.AccountID)
		if err != nil {
			return nil, err
		}
		if len(tokens) > 0 {
			state.contextTokens[account.AccountID] = tokens
		}

		status, err := readRuntimeStatus(root, account.AccountID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(status.AccountID) == "" {
			status.AccountID = account.AccountID
		}
		state.statuses[account.AccountID] = status
	}

	return state, nil
}

func loadAccounts(stateRoot string) ([]Account, error) {
	accountIDs, err := loadAccountIDs(stateRoot)
	if err != nil {
		return nil, err
	}
	accounts := make([]Account, 0, len(accountIDs))
	for _, accountID := range accountIDs {
		account, err := readAccount(stateRoot, accountID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(account.AccountID) == "" {
			account.AccountID = accountID
		}
		accounts = append(accounts, account)
	}
	sort.Slice(accounts, func(i int, j int) bool {
		return accounts[i].AccountID < accounts[j].AccountID
	})
	return accounts, nil
}

func (s *channelState) accountsSlice() []Account {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Account, 0, len(s.accounts))
	for _, account := range s.accounts {
		out = append(out, account)
	}
	sort.Slice(out, func(i int, j int) bool {
		return out[i].AccountID < out[j].AccountID
	})
	return out
}

func (s *channelState) accountIDs() []string {
	accounts := s.accountsSlice()
	out := make([]string, 0, len(accounts))
	for _, account := range accounts {
		out = append(out, account.AccountID)
	}
	return out
}

func (s *channelState) account(accountID string) (Account, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	account, ok := s.accounts[strings.TrimSpace(accountID)]
	return account, ok
}

func (s *channelState) syncAccountsFromDisk() ([]Account, error) {
	accounts, err := loadAccounts(s.root)
	if err != nil {
		return nil, err
	}

	nextAccounts := make(map[string]Account, len(accounts))
	for _, account := range accounts {
		nextAccounts[account.AccountID] = account
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for accountID := range s.accounts {
		if _, ok := nextAccounts[accountID]; ok {
			continue
		}
		delete(s.accounts, accountID)
		delete(s.cursors, accountID)
		delete(s.contextTokens, accountID)
		delete(s.statuses, accountID)
	}
	for accountID, account := range nextAccounts {
		s.accounts[accountID] = account
	}
	return append([]Account(nil), accounts...), nil
}

func (s *channelState) saveAccount(account Account) error {
	account.AccountID = strings.TrimSpace(account.AccountID)
	account.Token = strings.TrimSpace(account.Token)
	account.BaseURL = strings.TrimSpace(account.BaseURL)
	account.UserID = strings.TrimSpace(account.UserID)
	if account.AccountID == "" {
		return fmt.Errorf("weixin state: empty account id")
	}
	if account.Token == "" {
		return fmt.Errorf("weixin state: empty token for %s", account.AccountID)
	}
	account.UpdatedAt = time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.accounts[account.AccountID] = account
	if err := writeAccount(s.root, account); err != nil {
		return err
	}
	return writeAccountIndex(s.root, mapKeys(s.accounts))
}

func (s *channelState) removeAccount(accountID string) error {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return fmt.Errorf("weixin state: empty account id")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.accounts, accountID)
	delete(s.cursors, accountID)
	delete(s.contextTokens, accountID)
	delete(s.statuses, accountID)

	if err := os.Remove(accountPath(s.root, accountID)); err != nil &&
		!os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(cursorPath(s.root, accountID)); err != nil &&
		!os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(contextPath(s.root, accountID)); err != nil &&
		!os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(statusPath(s.root, accountID)); err != nil &&
		!os.IsNotExist(err) {
		return err
	}

	return writeAccountIndex(s.root, mapKeys(s.accounts))
}

func (s *channelState) cursor(accountID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return strings.TrimSpace(s.cursors[strings.TrimSpace(accountID)])
}

func (s *channelState) setCursor(
	accountID string,
	cursor string,
) error {
	accountID = strings.TrimSpace(accountID)
	cursor = strings.TrimSpace(cursor)
	if accountID == "" {
		return fmt.Errorf("weixin state: empty account id")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if cursor == "" {
		delete(s.cursors, accountID)
	} else {
		s.cursors[accountID] = cursor
	}
	return writeCursor(s.root, accountID, cursor)
}

func (s *channelState) contextToken(
	accountID string,
	peerID string,
) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	accountID = strings.TrimSpace(accountID)
	peerID = strings.TrimSpace(peerID)
	if peerID == "" {
		return ""
	}
	return strings.TrimSpace(s.contextTokens[accountID][peerID])
}

func (s *channelState) setContextToken(
	accountID string,
	peerID string,
	token string,
) error {
	accountID = strings.TrimSpace(accountID)
	peerID = strings.TrimSpace(peerID)
	token = strings.TrimSpace(token)
	if accountID == "" || peerID == "" {
		return fmt.Errorf("weixin state: missing context token key")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	peerTokens := s.contextTokens[accountID]
	if peerTokens == nil {
		peerTokens = make(map[string]string)
		s.contextTokens[accountID] = peerTokens
	}
	if token == "" {
		delete(peerTokens, peerID)
	} else {
		peerTokens[peerID] = token
	}
	return writeContextTokens(s.root, accountID, peerTokens)
}

func (s *channelState) statusSnapshot(accountID string) RuntimeStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneRuntimeStatus(s.statuses[strings.TrimSpace(accountID)])
}

func (s *channelState) contextPeerCount(accountID string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.contextTokens[strings.TrimSpace(accountID)])
}

func (s *channelState) markInbound(accountID string) error {
	now := time.Now()
	return s.updateStatus(accountID, func(status *RuntimeStatus) {
		status.LastEventAt = cloneTime(now)
		status.LastInboundAt = cloneTime(now)
	})
}

func (s *channelState) markOutbound(accountID string) error {
	now := time.Now()
	return s.updateStatus(accountID, func(status *RuntimeStatus) {
		status.LastEventAt = cloneTime(now)
		status.LastOutboundAt = cloneTime(now)
	})
}

func (s *channelState) recordError(
	accountID string,
	message string,
) error {
	now := time.Now()
	return s.updateStatus(accountID, func(status *RuntimeStatus) {
		status.LastError = strings.TrimSpace(message)
		status.UpdatedAt = cloneTime(now)
	})
}

func (s *channelState) markPaused(
	accountID string,
	until time.Time,
	message string,
) error {
	return s.updateStatus(accountID, func(status *RuntimeStatus) {
		status.PausedUntil = cloneTime(until)
		status.LastError = strings.TrimSpace(message)
	})
}

func (s *channelState) resumeAccount(accountID string) error {
	return s.updateStatus(accountID, func(status *RuntimeStatus) {
		status.PausedUntil = nil
	})
}

func (s *channelState) pauseRemaining(
	accountID string,
	now time.Time,
) (time.Duration, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return 0, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	status := cloneRuntimeStatus(s.statuses[accountID])
	if status.PausedUntil == nil {
		return 0, nil
	}
	if !now.Before(*status.PausedUntil) {
		status.PausedUntil = nil
		s.statuses[accountID] = status
		if err := writeRuntimeStatus(s.root, status); err != nil {
			return 0, err
		}
		return 0, nil
	}
	return status.PausedUntil.Sub(now), nil
}

func (s *channelState) updateStatus(
	accountID string,
	update func(*RuntimeStatus),
) error {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return fmt.Errorf("weixin state: empty account id")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	status := cloneRuntimeStatus(s.statuses[accountID])
	status.AccountID = accountID
	update(&status)
	now := time.Now()
	status.UpdatedAt = cloneTime(now)
	s.statuses[accountID] = status
	return writeRuntimeStatus(s.root, status)
}

func loadAccountIDs(stateRoot string) ([]string, error) {
	index := accountIndexPath(stateRoot)
	data, err := os.ReadFile(index)
	if err == nil {
		var ids []string
		if err := json.Unmarshal(data, &ids); err != nil {
			return nil, fmt.Errorf(
				"weixin state: decode account index: %w",
				err,
			)
		}
		return normalizeIDs(ids), nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	return scanAccountIDs(stateRoot)
}

func scanAccountIDs(stateRoot string) ([]string, error) {
	dir := accountsDir(stateRoot)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		switch {
		case name == accountIndexFileName:
			continue
		case !strings.HasSuffix(name, accountFileSuffix):
			continue
		case strings.HasSuffix(name, cursorFileSuffix):
			continue
		case strings.HasSuffix(name, contextFileSuffix):
			continue
		default:
			ids = append(
				ids,
				strings.TrimSuffix(name, accountFileSuffix),
			)
		}
	}
	return normalizeIDs(ids), nil
}

func readAccount(stateRoot string, accountID string) (Account, error) {
	var account Account
	data, err := os.ReadFile(accountPath(stateRoot, accountID))
	if err != nil {
		return Account{}, err
	}
	if err := json.Unmarshal(data, &account); err != nil {
		return Account{}, fmt.Errorf(
			"weixin state: decode account %s: %w",
			accountID,
			err,
		)
	}
	account.AccountID = strings.TrimSpace(account.AccountID)
	if account.AccountID == "" {
		account.AccountID = strings.TrimSpace(accountID)
	}
	return account, nil
}

func writeAccount(stateRoot string, account Account) error {
	if err := ensureDir(accountsDir(stateRoot)); err != nil {
		return err
	}
	return atomicWriteJSON(
		accountPath(stateRoot, account.AccountID),
		account,
	)
}

func readCursor(stateRoot string, accountID string) (string, error) {
	data, err := os.ReadFile(cursorPath(stateRoot, accountID))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	var payload struct {
		Cursor string `json:"cursor,omitempty"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", fmt.Errorf(
			"weixin state: decode cursor %s: %w",
			accountID,
			err,
		)
	}
	return strings.TrimSpace(payload.Cursor), nil
}

func writeCursor(
	stateRoot string,
	accountID string,
	cursor string,
) error {
	if err := ensureDir(accountsDir(stateRoot)); err != nil {
		return err
	}
	payload := struct {
		Cursor string `json:"cursor,omitempty"`
	}{
		Cursor: strings.TrimSpace(cursor),
	}
	return atomicWriteJSON(cursorPath(stateRoot, accountID), payload)
}

func readContextTokens(
	stateRoot string,
	accountID string,
) (map[string]string, error) {
	data, err := os.ReadFile(contextPath(stateRoot, accountID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var tokens map[string]string
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, fmt.Errorf(
			"weixin state: decode context tokens %s: %w",
			accountID,
			err,
		)
	}
	for peerID, token := range tokens {
		trimmedPeerID := strings.TrimSpace(peerID)
		trimmedToken := strings.TrimSpace(token)
		if trimmedPeerID == "" || trimmedToken == "" {
			delete(tokens, peerID)
			continue
		}
		if trimmedPeerID != peerID {
			delete(tokens, peerID)
			tokens[trimmedPeerID] = trimmedToken
			continue
		}
		tokens[peerID] = trimmedToken
	}
	return tokens, nil
}

func writeContextTokens(
	stateRoot string,
	accountID string,
	tokens map[string]string,
) error {
	if err := ensureDir(accountsDir(stateRoot)); err != nil {
		return err
	}
	if tokens == nil {
		tokens = make(map[string]string)
	}
	return atomicWriteJSON(contextPath(stateRoot, accountID), tokens)
}

func readRuntimeStatus(
	stateRoot string,
	accountID string,
) (RuntimeStatus, error) {
	data, err := os.ReadFile(statusPath(stateRoot, accountID))
	if err != nil {
		if os.IsNotExist(err) {
			return RuntimeStatus{AccountID: accountID}, nil
		}
		return RuntimeStatus{}, err
	}
	var status RuntimeStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return RuntimeStatus{}, fmt.Errorf(
			"weixin state: decode runtime status %s: %w",
			accountID,
			err,
		)
	}
	return status, nil
}

func writeRuntimeStatus(
	stateRoot string,
	status RuntimeStatus,
) error {
	if err := ensureDir(runtimeDir(stateRoot)); err != nil {
		return err
	}
	return atomicWriteJSON(statusPath(stateRoot, status.AccountID), status)
}

func writeAccountIndex(
	stateRoot string,
	accountIDs []string,
) error {
	if err := ensureDir(accountsDir(stateRoot)); err != nil {
		return err
	}
	return atomicWriteJSON(
		accountIndexPath(stateRoot),
		normalizeIDs(accountIDs),
	)
}

func atomicWriteJSON(path string, value any) error {
	dir := filepath.Dir(path)
	if err := ensureDir(dir); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, atomicFilePattern)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(tmp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Chmod(privateFileMode); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

func ensureDir(path string) error {
	return os.MkdirAll(path, privateDirMode)
}

func normalizeIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func mapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	return out
}

func cloneRuntimeStatus(status RuntimeStatus) RuntimeStatus {
	return RuntimeStatus{
		AccountID:      strings.TrimSpace(status.AccountID),
		PausedUntil:    cloneTimePtr(status.PausedUntil),
		LastEventAt:    cloneTimePtr(status.LastEventAt),
		LastInboundAt:  cloneTimePtr(status.LastInboundAt),
		LastOutboundAt: cloneTimePtr(status.LastOutboundAt),
		LastError:      strings.TrimSpace(status.LastError),
		UpdatedAt:      cloneTimePtr(status.UpdatedAt),
	}
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneTime(value time.Time) *time.Time {
	cloned := value
	return &cloned
}

func accountsDir(stateRoot string) string {
	return filepath.Join(stateRoot, accountsDirName)
}

func runtimeDir(stateRoot string) string {
	return filepath.Join(stateRoot, runtimeDirName)
}

func accountIndexPath(stateRoot string) string {
	return filepath.Join(accountsDir(stateRoot), accountIndexFileName)
}

func accountPath(stateRoot string, accountID string) string {
	return filepath.Join(
		accountsDir(stateRoot),
		strings.TrimSpace(accountID)+accountFileSuffix,
	)
}

func cursorPath(stateRoot string, accountID string) string {
	return filepath.Join(
		accountsDir(stateRoot),
		strings.TrimSpace(accountID)+cursorFileSuffix,
	)
}

func contextPath(stateRoot string, accountID string) string {
	return filepath.Join(
		accountsDir(stateRoot),
		strings.TrimSpace(accountID)+contextFileSuffix,
	)
}

func statusPath(stateRoot string, accountID string) string {
	return filepath.Join(
		runtimeDir(stateRoot),
		strings.TrimSpace(accountID)+statusFileSuffix,
	)
}

func (a Account) effectiveBaseURL(defaultValue string) string {
	baseURL := strings.TrimSpace(a.BaseURL)
	if baseURL != "" {
		return baseURL
	}
	return strings.TrimSpace(defaultValue)
}
