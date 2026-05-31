package releaseinfo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "https://mirrors.tencent.com/" +
		"repository/generic/trpc-agent-go/trpc-claw"

	ChannelLatest  = "latest"
	ChannelPreview = "preview"

	LatestVersionRelPath    = "latest/VERSION"
	LatestChangelogRelPath  = "latest/CHANGELOG.md"
	LatestIndexRelPath      = "latest/releases.json"
	PreviewVersionRelPath   = "preview/VERSION"
	PreviewChangelogRelPath = "preview/CHANGELOG.md"
	PreviewIndexRelPath     = "preview/releases.json"

	releasesDirName       = "releases"
	changeLogDocName      = "CHANGELOG.md"
	versionFileName       = "VERSION"
	releaseIndexFileName  = "releases.json"
	changelogHeadingPref  = "## "
	changelogBulletPrefix = "- "
)

type HTTPStatusError struct {
	URL        string
	Status     string
	StatusCode int
}

func (e *HTTPStatusError) Error() string {
	if e == nil {
		return "release request failed"
	}
	return fmt.Sprintf(
		"request %q failed: status %s",
		e.URL,
		e.Status,
	)
}

type Entry struct {
	Version      string    `json:"version"`
	PublishedAt  time.Time `json:"published_at,omitempty"`
	InstallURL   string    `json:"install_url,omitempty"`
	ChangelogURL string    `json:"changelog_url,omitempty"`
	Notes        []string  `json:"notes,omitempty"`
}

type Index struct {
	LatestVersion      string  `json:"latest_version"`
	Channel            string  `json:"channel,omitempty"`
	MinSupportedTarget string  `json:"min_supported_target,omitempty"`
	Versions           []Entry `json:"versions,omitempty"`
}

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func (c Client) FetchLatestVersion(
	ctx context.Context,
) (string, error) {
	return c.FetchChannelVersion(ctx, ChannelLatest)
}

func (c Client) FetchChannelVersion(
	ctx context.Context,
	channel string,
) (string, error) {
	channel, err := NormalizeChannel(channel)
	if err != nil {
		return "", err
	}
	body, err := c.fetchAsset(ctx, c.channelVersionURL(channel))
	if err != nil {
		return "", err
	}
	version := strings.TrimSpace(string(body))
	if version == "" {
		return "", fmt.Errorf("%s version is empty", channel)
	}
	return version, nil
}

func (c Client) FetchIndex(
	ctx context.Context,
) (Index, error) {
	return c.FetchChannelIndex(ctx, ChannelLatest)
}

func (c Client) FetchChannelIndex(
	ctx context.Context,
	channel string,
) (Index, error) {
	channel, err := NormalizeChannel(channel)
	if err != nil {
		return Index{}, err
	}
	body, err := c.fetchAsset(ctx, c.channelIndexURL(channel))
	if err == nil {
		return c.parseIndex(body, channel)
	}

	statusErr := &HTTPStatusError{}
	if !isNotFoundStatus(err, statusErr) {
		return Index{}, err
	}

	latest, latestErr := c.FetchChannelVersion(ctx, channel)
	if latestErr != nil {
		return Index{}, latestErr
	}
	notes, notesErr := c.FetchChangeSummary(ctx, latest, 3)
	if notesErr != nil {
		notes = nil
	}
	entry := Entry{
		Version:      latest,
		InstallURL:   c.versionInstallURL(latest),
		ChangelogURL: c.versionChangelogURL(latest),
		Notes:        notes,
	}
	return Index{
		LatestVersion: latest,
		Channel:       channel,
		Versions:      []Entry{entry},
	}, nil
}

func (c Client) FetchChangelog(
	ctx context.Context,
	version string,
) (string, error) {
	return c.FetchChannelChangelog(ctx, ChannelLatest, version)
}

func (c Client) FetchChannelChangelog(
	ctx context.Context,
	channel string,
	version string,
) (string, error) {
	channel, err := NormalizeChannel(channel)
	if err != nil {
		return "", err
	}
	version = strings.TrimSpace(version)
	if version == "" {
		latest, err := c.FetchChannelVersion(ctx, channel)
		if err != nil {
			return "", err
		}
		version = latest
	}

	body, err := c.fetchAsset(
		ctx,
		c.versionChangelogURL(version),
	)
	if err == nil {
		return string(body), nil
	}

	statusErr := &HTTPStatusError{}
	if !isNotFoundStatus(err, statusErr) {
		return "", err
	}

	body, err = c.fetchAsset(ctx, c.channelChangelogURL(channel))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (c Client) FetchChangeSummary(
	ctx context.Context,
	version string,
	limit int,
) ([]string, error) {
	return c.FetchChannelChangeSummary(
		ctx,
		ChannelLatest,
		version,
		limit,
	)
}

func (c Client) FetchChannelChangeSummary(
	ctx context.Context,
	channel string,
	version string,
	limit int,
) ([]string, error) {
	channel, err := NormalizeChannel(channel)
	if err != nil {
		return nil, err
	}
	version = strings.TrimSpace(version)
	if version == "" {
		latest, err := c.FetchChannelVersion(ctx, channel)
		if err != nil {
			return nil, err
		}
		version = latest
	}
	changelog, err := c.FetchChannelChangelog(
		ctx,
		channel,
		version,
	)
	if err != nil {
		return nil, err
	}
	return ExtractReleaseChanges(
		changelog,
		version,
		limit,
	), nil
}

func (c Client) parseIndex(body []byte, channel string) (Index, error) {
	var index Index
	if err := json.Unmarshal(body, &index); err != nil {
		return Index{}, fmt.Errorf(
			"decode releases index: %w",
			err,
		)
	}
	index.Versions = normalizeEntries(index.Versions)
	if strings.TrimSpace(index.LatestVersion) == "" &&
		len(index.Versions) > 0 {
		index.LatestVersion = index.Versions[0].Version
	}
	if strings.TrimSpace(index.Channel) == "" {
		index.Channel = channel
	}
	return index, nil
}

func (c Client) fetchAsset(
	ctx context.Context,
	url string,
) ([]byte, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		url,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %q: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPStatusError{
			URL:        url,
			Status:     resp.Status,
			StatusCode: resp.StatusCode,
		}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	return body, nil
}

func (c Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func (c Client) baseURL() string {
	base := strings.TrimSpace(c.BaseURL)
	if base == "" {
		base = DefaultBaseURL
	}
	return strings.TrimRight(base, "/")
}

func (c Client) latestVersionURL() string {
	return c.channelVersionURL(ChannelLatest)
}

func (c Client) latestChangelogURL() string {
	return c.channelChangelogURL(ChannelLatest)
}

func (c Client) latestIndexURL() string {
	return c.channelIndexURL(ChannelLatest)
}

func (c Client) channelVersionURL(channel string) string {
	return JoinURL(
		c.baseURL(),
		strings.Join(
			[]string{
				channel,
				versionFileName,
			},
			"/",
		),
	)
}

func (c Client) channelChangelogURL(channel string) string {
	return JoinURL(
		c.baseURL(),
		strings.Join(
			[]string{
				channel,
				changeLogDocName,
			},
			"/",
		),
	)
}

func (c Client) channelIndexURL(channel string) string {
	return JoinURL(
		c.baseURL(),
		strings.Join(
			[]string{
				channel,
				releaseIndexFileName,
			},
			"/",
		),
	)
}

func (c Client) versionChangelogURL(version string) string {
	return JoinURL(
		c.baseURL(),
		strings.Join(
			[]string{
				releasesDirName,
				strings.TrimSpace(version),
				changeLogDocName,
			},
			"/",
		),
	)
}

func (c Client) versionInstallURL(version string) string {
	return JoinURL(
		c.baseURL(),
		strings.Join(
			[]string{
				releasesDirName,
				strings.TrimSpace(version),
				"install.sh",
			},
			"/",
		),
	)
}

func JoinURL(base string, rel string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	rel = strings.TrimLeft(strings.TrimSpace(rel), "/")
	return base + "/" + rel
}

func NormalizeChannel(channel string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case "", ChannelLatest:
		return ChannelLatest, nil
	case ChannelPreview:
		return ChannelPreview, nil
	default:
		return "", fmt.Errorf("unsupported release channel %q", channel)
	}
}

func ExtractReleaseChanges(
	markdown string,
	version string,
	limit int,
) []string {
	if limit <= 0 {
		return nil
	}

	targetVersion := strings.TrimSpace(version)
	lines := strings.Split(markdown, "\n")
	changes := make([]string, 0, limit)
	inSection := false
	current := ""
	flush := func() {
		if strings.TrimSpace(current) == "" {
			current = ""
			return
		}
		changes = append(changes, strings.TrimSpace(current))
		current = ""
	}

	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		headingVersion := changelogHeadingVersion(line)
		if headingVersion != "" {
			if inSection {
				flush()
				break
			}
			inSection = headingVersion == targetVersion
			continue
		}
		if !inSection {
			continue
		}

		switch {
		case strings.HasPrefix(line, changelogBulletPrefix):
			flush()
			current = strings.TrimSpace(
				strings.TrimPrefix(
					line,
					changelogBulletPrefix,
				),
			)
		case line == "":
			flush()
		case current != "":
			current += " " + line
		}

		if len(changes) >= limit {
			break
		}
	}
	flush()
	if len(changes) > limit {
		return changes[:limit]
	}
	return changes
}

func changelogHeadingVersion(line string) string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, changelogHeadingPref) {
		return ""
	}

	trimmed := strings.TrimSpace(
		strings.TrimPrefix(line, changelogHeadingPref),
	)
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return ""
	}

	version := fields[0]
	if _, ok := parseVersion(version); !ok {
		return ""
	}
	return version
}

func HasNewerRelease(
	latest string,
	current string,
) bool {
	return CompareVersions(latest, current) > 0
}

func CompareVersions(left string, right string) int {
	leftVersion, leftOK := parseComparableVersion(left)
	rightVersion, rightOK := parseComparableVersion(right)
	if !leftOK || !rightOK {
		left = strings.TrimSpace(left)
		right = strings.TrimSpace(right)
		switch {
		case left == right:
			return 0
		case left > right:
			return 1
		default:
			return -1
		}
	}

	maxLen := len(leftVersion.parts)
	if len(rightVersion.parts) > maxLen {
		maxLen = len(rightVersion.parts)
	}
	for i := 0; i < maxLen; i++ {
		leftValue := versionPartAt(leftVersion.parts, i)
		rightValue := versionPartAt(rightVersion.parts, i)
		switch {
		case leftValue > rightValue:
			return 1
		case leftValue < rightValue:
			return -1
		}
	}
	return comparePrereleaseVersions(leftVersion, rightVersion)
}

func versionPartAt(parts []int, index int) int {
	if index < 0 || index >= len(parts) {
		return 0
	}
	return parts[index]
}

func parseVersion(raw string) ([]int, bool) {
	parsed, ok := parseComparableVersion(raw)
	if !ok {
		return nil, false
	}
	return parsed.parts, true
}

type comparableVersion struct {
	parts      []int
	prerelease string
}

func parseComparableVersion(raw string) (comparableVersion, bool) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "v")
	raw = strings.TrimPrefix(raw, "V")
	if raw == "" {
		return comparableVersion{}, false
	}
	if buildIndex := strings.Index(raw, "+"); buildIndex >= 0 {
		raw = raw[:buildIndex]
	}

	prerelease := ""
	if preIndex := strings.Index(raw, "-"); preIndex >= 0 {
		prerelease = raw[preIndex+1:]
		raw = raw[:preIndex]
		if strings.TrimSpace(prerelease) == "" {
			return comparableVersion{}, false
		}
	}

	parts := strings.Split(raw, ".")
	values := make([]int, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return comparableVersion{}, false
		}
		value, err := strconv.Atoi(part)
		if err != nil {
			return comparableVersion{}, false
		}
		values = append(values, value)
	}
	return comparableVersion{
		parts:      values,
		prerelease: prerelease,
	}, true
}

func comparePrereleaseVersions(
	left comparableVersion,
	right comparableVersion,
) int {
	leftPre := strings.TrimSpace(left.prerelease)
	rightPre := strings.TrimSpace(right.prerelease)
	switch {
	case leftPre == "" && rightPre == "":
		return 0
	case leftPre == "":
		return 1
	case rightPre == "":
		return -1
	}
	return comparePrereleaseValues(leftPre, rightPre)
}

func comparePrereleaseValues(left string, right string) int {
	leftParts := strings.Split(left, ".")
	rightParts := strings.Split(right, ".")
	maxLen := len(leftParts)
	if len(rightParts) > maxLen {
		maxLen = len(rightParts)
	}
	for i := 0; i < maxLen; i++ {
		switch {
		case i >= len(leftParts):
			return -1
		case i >= len(rightParts):
			return 1
		}
		result := comparePrereleasePart(leftParts[i], rightParts[i])
		if result != 0 {
			return result
		}
	}
	return 0
}

func comparePrereleasePart(left string, right string) int {
	leftNum, leftNumeric := parsePrereleaseNumber(left)
	rightNum, rightNumeric := parsePrereleaseNumber(right)
	switch {
	case leftNumeric && rightNumeric:
		switch {
		case leftNum > rightNum:
			return 1
		case leftNum < rightNum:
			return -1
		default:
			return 0
		}
	case leftNumeric:
		return -1
	case rightNumeric:
		return 1
	}
	switch {
	case left > right:
		return 1
	case left < right:
		return -1
	default:
		return 0
	}
}

func parsePrereleaseNumber(raw string) (int, bool) {
	if strings.TrimSpace(raw) == "" {
		return 0, false
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return value, true
}

func normalizeEntries(entries []Entry) []Entry {
	if len(entries) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(entries))
	normalized := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		entry.Version = strings.TrimSpace(entry.Version)
		if entry.Version == "" {
			continue
		}
		if _, ok := seen[entry.Version]; ok {
			continue
		}
		seen[entry.Version] = struct{}{}
		entry.Notes = normalizeNotes(entry.Notes)
		normalized = append(normalized, entry)
	}

	sortEntries(normalized)
	return normalized
}

func normalizeNotes(notes []string) []string {
	if len(notes) == 0 {
		return nil
	}
	out := make([]string, 0, len(notes))
	for _, note := range notes {
		note = strings.TrimSpace(note)
		if note == "" {
			continue
		}
		out = append(out, note)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sortEntries(entries []Entry) {
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if CompareVersions(
				entries[j].Version,
				entries[i].Version,
			) > 0 {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}
}

func isNotFoundStatus(
	err error,
	statusErr *HTTPStatusError,
) bool {
	if statusErr == nil {
		return false
	}
	if !asHTTPStatusError(err, statusErr) {
		return false
	}
	return statusErr.StatusCode == http.StatusNotFound
}

func asHTTPStatusError(
	err error,
	target *HTTPStatusError,
) bool {
	if err == nil || target == nil {
		return false
	}
	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) || statusErr == nil {
		return false
	}
	*target = *statusErr
	return true
}
