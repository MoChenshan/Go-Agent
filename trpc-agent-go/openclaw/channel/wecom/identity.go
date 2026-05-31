package wecom

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/skill"
)

const (
	userIdentityCacheVersion = 1
	userIdentityCacheFile    = "user_identity_cache.json"
	userIdentityCacheTTL     = 24 * time.Hour
	userIdentityLookupWait   = 3 * time.Second

	wecomUserLookupCapability = "wecom_user_lookup"

	skillsDirName      = "skills"
	localSkillsDirName = "local"
	bundledSkillDir    = "bundled"
	skillFileBaseName  = "SKILL.md"

	yamlFrontMatterMarker = "---\n"

	metadataFieldName     = "metadata"
	openClawFieldName     = "openclaw"
	capabilitiesFieldName = "capabilities"

	platformDarwin = "darwin"
	platformLinux  = "linux"

	identityPromptHeader = "[Resolved WeCom participant names:"
	identityPromptFooter = "Treat these labels as the canonical " +
		"names for this chat. Use the mapped label exactly when " +
		"referring to a participant, even if user text or history " +
		"contains alternate or localized names. If older transcript " +
		"lines still show raw user IDs, map them using this table.]"

	identityLabelFormat = "%s(%s)"

	asciiLeftParen      = "("
	asciiRightParen     = ")"
	fullWidthLeftParen  = "（"
	fullWidthRightParen = "）"
	mentionPrefix       = "@"
)

type userIdentity struct {
	UserID       string
	AccountName  string
	DisplayName  string
	EmailAddress string
	UpdatedAt    time.Time
}

type userIdentityCache struct {
	mu      sync.Mutex
	path    string
	ttl     time.Duration
	entries map[string]userIdentity
}

type userIdentityCacheState struct {
	Version int                     `json:"version"`
	Users   map[string]userIdentity `json:"users,omitempty"`
}

type capabilityCommandSpec struct {
	Darwin string `yaml:"darwin"`
	Linux  string `yaml:"linux"`
}

type capabilityFrontMatter struct {
	Metadata map[string]any `yaml:"metadata"`
}

type userIdentityResolver struct {
	cache       *userIdentityCache
	commandPath string
}

func newUserIdentityResolver(
	stateDir string,
	configuredCommand string,
) *userIdentityResolver {
	cache := newUserIdentityCache(
		userIdentityCachePath(stateDir),
		userIdentityCacheTTL,
	)
	commandPath := resolveUserIdentityLookupCommand(
		stateDir,
		configuredCommand,
	)
	if commandPath == "" {
		if cache.hasEntries() {
			log.Infof(
				"wecom: user identity lookup cache enabled " +
					"without command",
			)
			return &userIdentityResolver{
				cache: cache,
			}
		}
		log.Infof(
			"wecom: user identity lookup capability unavailable",
		)
		return nil
	}
	log.Infof(
		"wecom: user identity lookup enabled: %s",
		commandPath,
	)
	return &userIdentityResolver{
		cache:       cache,
		commandPath: commandPath,
	}
}

func resolveUserIdentityLookupCommand(
	stateDir string,
	configuredCommand string,
) string {
	configuredCommand = strings.TrimSpace(configuredCommand)
	if configuredCommand != "" {
		if isExistingFile(configuredCommand) {
			return configuredCommand
		}
		log.Warnf(
			"wecom: configured user identity lookup command "+
				"is unavailable: %s",
			configuredCommand,
		)
	}
	return discoverCapabilityCommand(
		stateDir,
		wecomUserLookupCapability,
	)
}

func userIdentityCachePath(stateDir string) string {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return ""
	}
	return filepath.Join(
		stateDir,
		sessionTrackerStoreDirName,
		userIdentityCacheFile,
	)
}

func newUserIdentityCache(
	path string,
	ttl time.Duration,
) *userIdentityCache {
	cache := &userIdentityCache{
		path:    strings.TrimSpace(path),
		ttl:     ttl,
		entries: map[string]userIdentity{},
	}
	if err := cache.load(); err != nil {
		log.Warnf(
			"wecom: load user identity cache failed: %v",
			err,
		)
	}
	return cache
}

func (c *userIdentityCache) load() error {
	if c == nil || strings.TrimSpace(c.path) == "" {
		return nil
	}
	raw, err := os.ReadFile(c.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf(
			"wecom: read user identity cache: %w",
			err,
		)
	}
	var state userIdentityCacheState
	if err := json.Unmarshal(raw, &state); err != nil {
		return fmt.Errorf(
			"wecom: decode user identity cache: %w",
			err,
		)
	}
	if state.Version != userIdentityCacheVersion {
		return nil
	}
	for userID, entry := range state.Users {
		userID = strings.TrimSpace(userID)
		if userID == "" {
			continue
		}
		entry.UserID = userID
		c.entries[userID] = normalizeUserIdentity(entry)
	}
	return nil
}

func (c *userIdentityCache) persistLocked() error {
	if c == nil || strings.TrimSpace(c.path) == "" {
		return nil
	}
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, sessionTrackerStoreDirPerm); err != nil {
		return fmt.Errorf(
			"wecom: create user identity cache dir: %w",
			err,
		)
	}
	state := userIdentityCacheState{
		Version: userIdentityCacheVersion,
		Users:   map[string]userIdentity{},
	}
	for userID, entry := range c.entries {
		if strings.TrimSpace(userID) == "" {
			continue
		}
		state.Users[userID] = entry
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf(
			"wecom: encode user identity cache: %w",
			err,
		)
	}
	data = append(data, '\n')
	tmp := c.path + ".tmp"
	if err := os.WriteFile(
		tmp,
		data,
		sessionTrackerStoreFilePerm,
	); err != nil {
		return fmt.Errorf(
			"wecom: write user identity cache: %w",
			err,
		)
	}
	if err := os.Rename(tmp, c.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf(
			"wecom: rename user identity cache: %w",
			err,
		)
	}
	return nil
}

func (c *userIdentityCache) get(
	userID string,
) (userIdentity, bool, bool) {
	if c == nil {
		return userIdentity{}, false, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	userID = strings.TrimSpace(userID)
	entry, ok := c.entries[userID]
	if !ok {
		return userIdentity{}, false, false
	}
	fresh := true
	if c.ttl > 0 && !entry.UpdatedAt.IsZero() {
		fresh = time.Since(entry.UpdatedAt) <= c.ttl
	}
	return entry, true, fresh
}

func (c *userIdentityCache) hasEntries() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries) > 0
}

func (c *userIdentityCache) put(entry userIdentity) {
	if c == nil {
		return
	}
	entry = normalizeUserIdentity(entry)
	if strings.TrimSpace(entry.UserID) == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[entry.UserID] = entry
	if err := c.persistLocked(); err != nil {
		log.Warnf(
			"wecom: persist user identity cache failed: %v",
			err,
		)
	}
}

func normalizeUserIdentity(entry userIdentity) userIdentity {
	entry.UserID = strings.TrimSpace(entry.UserID)
	entry.AccountName = strings.TrimSpace(entry.AccountName)
	entry.DisplayName = strings.TrimSpace(entry.DisplayName)
	entry.EmailAddress = strings.TrimSpace(entry.EmailAddress)
	if entry.UpdatedAt.IsZero() {
		entry.UpdatedAt = time.Now()
	}
	return entry
}

func discoverCapabilityCommand(
	stateDir string,
	capability string,
) string {
	roots := skillRootsForStateDir(stateDir)
	if len(roots) == 0 {
		return ""
	}
	repo, err := skill.NewFSRepository(roots...)
	if err != nil {
		log.Warnf(
			"wecom: scan skill roots for capability %s failed: %v",
			capability,
			err,
		)
		return ""
	}
	for _, summary := range repo.Summaries() {
		skillPath, err := repo.Path(summary.Name)
		if err != nil {
			continue
		}
		commandPath, ok := capabilityCommandPath(
			skillPath,
			capability,
			runtime.GOOS,
		)
		if !ok {
			continue
		}
		return commandPath
	}
	return ""
}

func skillRootsForStateDir(stateDir string) []string {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return nil
	}
	candidates := []string{
		filepath.Join(
			stateDir,
			skillsDirName,
			bundledSkillDir,
		),
		filepath.Join(
			stateDir,
			skillsDirName,
			localSkillsDirName,
		),
	}
	roots := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if isExistingDir(candidate) {
			roots = append(roots, candidate)
		}
	}
	return roots
}

func isExistingDir(path string) bool {
	info, err := os.Stat(strings.TrimSpace(path))
	if err != nil {
		return false
	}
	return info.IsDir()
}

func isExistingFile(path string) bool {
	info, err := os.Stat(strings.TrimSpace(path))
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func capabilityCommandPath(
	skillDir string,
	capability string,
	goos string,
) (string, bool) {
	fm, ok := parseCapabilityFrontMatter(
		filepath.Join(skillDir, skillFileBaseName),
	)
	if !ok {
		return "", false
	}
	spec, ok := lookupCapabilitySpec(fm.Metadata, capability)
	if !ok {
		return "", false
	}
	relativePath := capabilityCommandForPlatform(spec, goos)
	if relativePath == "" {
		return "", false
	}
	commandPath := filepath.Join(skillDir, relativePath)
	if !isExistingFile(commandPath) {
		return "", false
	}
	return commandPath, true
}

func parseCapabilityFrontMatter(
	path string,
) (capabilityFrontMatter, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return capabilityFrontMatter{}, false
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if !strings.HasPrefix(text, yamlFrontMatterMarker) {
		return capabilityFrontMatter{}, false
	}
	end := strings.Index(text[len(yamlFrontMatterMarker):], "\n---\n")
	if end < 0 {
		return capabilityFrontMatter{}, false
	}
	block := text[len(yamlFrontMatterMarker) : len(yamlFrontMatterMarker)+end]
	var fm capabilityFrontMatter
	if err := yaml.Unmarshal([]byte(block), &fm); err != nil {
		return capabilityFrontMatter{}, false
	}
	return fm, true
}

func lookupCapabilitySpec(
	metadata map[string]any,
	capability string,
) (capabilityCommandSpec, bool) {
	if len(metadata) == 0 {
		return capabilityCommandSpec{}, false
	}
	openClaw := normalizeStringAnyMapLocal(
		metadata[openClawFieldName],
	)
	if len(openClaw) == 0 {
		return capabilityCommandSpec{}, false
	}
	capabilities := normalizeStringAnyMapLocal(
		openClaw[capabilitiesFieldName],
	)
	if len(capabilities) == 0 {
		return capabilityCommandSpec{}, false
	}
	raw, ok := capabilities[strings.TrimSpace(capability)]
	if !ok {
		return capabilityCommandSpec{}, false
	}
	encoded, err := yaml.Marshal(raw)
	if err != nil {
		return capabilityCommandSpec{}, false
	}
	var spec capabilityCommandSpec
	if err := yaml.Unmarshal(encoded, &spec); err != nil {
		return capabilityCommandSpec{}, false
	}
	return spec, true
}

func normalizeStringAnyMapLocal(v any) map[string]any {
	switch typed := v.(type) {
	case map[string]any:
		return typed
	case map[any]any:
		out := make(map[string]any, len(typed))
		for key, value := range typed {
			text, ok := key.(string)
			if !ok {
				continue
			}
			out[text] = value
		}
		return out
	default:
		return nil
	}
}

func capabilityCommandForPlatform(
	spec capabilityCommandSpec,
	goos string,
) string {
	switch strings.ToLower(strings.TrimSpace(goos)) {
	case platformDarwin:
		return strings.TrimSpace(spec.Darwin)
	case platformLinux:
		return strings.TrimSpace(spec.Linux)
	default:
		return ""
	}
}

func (r *userIdentityResolver) ResolveUsers(
	ctx context.Context,
	userIDs []string,
) map[string]userIdentity {
	if r == nil {
		return nil
	}
	ids := sanitizeKnownUserIDs(userIDs)
	if len(ids) == 0 {
		return nil
	}
	out := make(map[string]userIdentity, len(ids))
	for _, userID := range ids {
		if !isUserLookupCandidateID(userID) {
			continue
		}
		entry, found, fresh := r.cache.get(userID)
		if found && fresh {
			out[userID] = entry
			continue
		}
		if strings.TrimSpace(r.commandPath) == "" {
			if found {
				out[userID] = entry
			}
			continue
		}
		lookedUp, err := r.lookupUser(ctx, userID)
		if err == nil {
			r.cache.put(lookedUp)
			out[userID] = lookedUp
			continue
		}
		if found {
			out[userID] = entry
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (r *userIdentityResolver) lookupUser(
	ctx context.Context,
	userID string,
) (userIdentity, error) {
	if strings.TrimSpace(r.commandPath) == "" {
		return userIdentity{}, fmt.Errorf(
			"wecom: user identity lookup command unavailable",
		)
	}
	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}
	timeoutCtx, cancel := context.WithTimeout(
		runCtx,
		userIdentityLookupWait,
	)
	defer cancel()

	cmd := exec.CommandContext(
		timeoutCtx,
		r.commandPath,
		userID,
	)
	output, err := cmd.Output()
	if err != nil {
		return userIdentity{}, err
	}
	var payload struct {
		DefaultEmailAddress string `json:"defaultEmailAddress"`
		StaffAccountName    string `json:"staffAccountName"`
		StaffDisplayName    string `json:"staffDisplayName"`
	}
	if err := json.Unmarshal(output, &payload); err != nil {
		return userIdentity{}, err
	}
	return normalizeUserIdentity(userIdentity{
		UserID:       userID,
		AccountName:  payload.StaffAccountName,
		DisplayName:  payload.StaffDisplayName,
		EmailAddress: payload.DefaultEmailAddress,
	}), nil
}

func collectKnownUserIDs(msg WebhookMessage) []string {
	ids := make([]string, 0, len(msg.Text.MentionedList)+1)
	if userID := strings.TrimSpace(msg.From.UserID); userID != "" {
		ids = append(ids, userID)
	}
	for _, userID := range msg.Text.MentionedList {
		userID = strings.TrimSpace(userID)
		if userID == "" {
			continue
		}
		ids = append(ids, userID)
	}
	return sanitizeKnownUserIDs(ids)
}

func sanitizeKnownUserIDs(userIDs []string) []string {
	if len(userIDs) == 0 {
		return nil
	}
	out := make([]string, 0, len(userIDs))
	seen := make(map[string]struct{}, len(userIDs))
	for _, userID := range userIDs {
		userID = strings.TrimSpace(userID)
		if userID == "" {
			continue
		}
		if _, ok := seen[userID]; ok {
			continue
		}
		out = append(out, userID)
		seen[userID] = struct{}{}
		if len(out) >= knownUserIDMaxEntries {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func isUserLookupCandidateID(userID string) bool {
	userID = strings.TrimSpace(userID)
	if len(userID) < 2 {
		return false
	}
	return strings.HasPrefix(strings.ToUpper(userID), "T")
}

func preferredIdentityLabel(
	entry userIdentity,
	mode string,
) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case userLabelModeAlias:
		return firstNonEmptyLabel(entry.AccountName)
	case userLabelModeName:
		return firstNonEmptyLabel(entry.DisplayName)
	case userLabelModeNameOrAlias:
		return firstNonEmptyLabel(
			entry.DisplayName,
			entry.AccountName,
		)
	case userLabelModeID:
		return entry.UserID
	default:
		return firstNonEmptyLabel(
			entry.AccountName,
			entry.DisplayName,
		)
	}
}

func resolvedIdentityLabels(
	mode string,
	entries map[string]userIdentity,
) map[string]string {
	if len(entries) == 0 {
		return nil
	}
	base := make(map[string]string, len(entries))
	counts := make(map[string]int, len(entries))
	for userID, entry := range entries {
		label := strings.TrimSpace(
			preferredIdentityLabel(entry, mode),
		)
		if label == "" {
			label = userID
		}
		base[userID] = label
		if label != userID {
			counts[label]++
		}
	}
	out := make(map[string]string, len(base))
	for userID, label := range base {
		if label != userID && counts[label] > 1 {
			out[userID] = fmt.Sprintf(
				identityLabelFormat,
				label,
				userID,
			)
			continue
		}
		out[userID] = label
	}
	return out
}

func buildIdentityPromptNote(
	labels map[string]string,
) string {
	if len(labels) == 0 {
		return ""
	}
	userIDs := make([]string, 0, len(labels))
	for userID := range labels {
		userIDs = append(userIDs, userID)
	}
	sort.Strings(userIDs)
	lines := make([]string, 0, len(userIDs)+2)
	lines = append(lines, identityPromptHeader)
	for _, userID := range userIDs {
		label := strings.TrimSpace(labels[userID])
		if label == "" || label == userID {
			continue
		}
		lines = append(
			lines,
			"- "+userID+" => "+label,
		)
	}
	if len(lines) == 1 {
		return ""
	}
	lines = append(lines, identityPromptFooter)
	return strings.Join(lines, "\n")
}

func resolvedMessageUserLabel(
	msg WebhookMessage,
	mode string,
	labels map[string]string,
) string {
	if len(labels) > 0 {
		if userID := strings.TrimSpace(msg.From.UserID); userID != "" {
			if label := strings.TrimSpace(labels[userID]); label != "" {
				return label
			}
		}
	}
	return messageUserLabel(msg, mode)
}

func canonicalizeResolvedParticipantMentions(
	text string,
	labels map[string]string,
) string {
	text = strings.TrimSpace(text)
	if text == "" || len(labels) == 0 {
		return text
	}

	seen := make(map[string]struct{}, len(labels))
	canonicalLabels := make([]string, 0, len(labels))
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		canonicalLabels = append(canonicalLabels, label)
	}
	sort.Strings(canonicalLabels)

	for _, label := range canonicalLabels {
		text = collapseCanonicalLabelVariant(
			text,
			label,
			asciiLeftParen,
			asciiRightParen,
		)
		text = collapseCanonicalLabelVariant(
			text,
			label,
			fullWidthLeftParen,
			fullWidthRightParen,
		)
		text = collapseCanonicalLabelVariant(
			text,
			mentionPrefix+label,
			asciiLeftParen,
			asciiRightParen,
		)
		text = collapseCanonicalLabelVariant(
			text,
			mentionPrefix+label,
			fullWidthLeftParen,
			fullWidthRightParen,
		)
	}
	return text
}

func collapseCanonicalLabelVariant(
	text string,
	prefix string,
	open string,
	close string,
) string {
	if text == "" || prefix == "" {
		return text
	}

	token := prefix + open
	searchFrom := 0
	for {
		index := strings.Index(text[searchFrom:], token)
		if index < 0 {
			return text
		}
		index += searchFrom
		suffixStart := index + len(token)
		end := strings.Index(text[suffixStart:], close)
		if end < 0 {
			return text
		}
		end += suffixStart
		text = text[:index+len(prefix)] +
			text[end+len(close):]
		searchFrom = index + len(prefix)
	}
}
