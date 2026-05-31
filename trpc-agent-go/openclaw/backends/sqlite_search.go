package backends

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/go-ego/gse"
	"trpc.group/trpc-go/trpc-agent-go/memory"
)

const (
	minSearchTokenLen = 2
	fallbackScore     = 0.5
)

var (
	segmenter     gse.Segmenter
	segmenterOnce sync.Once
	segmenterErr  error
)

var cjkStopwords = map[string]struct{}{
	"的": {}, "了": {}, "是": {}, "在": {}, "和": {},
	"有": {}, "我": {}, "他": {}, "她": {}, "它": {},
	"这": {}, "那": {}, "都": {}, "也": {}, "就": {},
	"不": {}, "会": {}, "到": {}, "说": {}, "对": {},
}

func getSegmenter() (*gse.Segmenter, error) {
	segmenterOnce.Do(func() {
		segmenter.SkipLog = true
		segmenterErr = segmenter.LoadDictEmbed()
	})
	if segmenterErr != nil {
		return nil, fmt.Errorf(
			"load segmenter dict failed: %w",
			segmenterErr,
		)
	}
	return &segmenter, nil
}

func generateMemoryID(
	mem *memory.Memory,
	appName string,
	userID string,
) string {
	var builder strings.Builder
	builder.WriteString("memory:")
	if mem != nil {
		builder.WriteString(mem.Memory)
	}
	builder.WriteString("|app:")
	builder.WriteString(appName)
	builder.WriteString("|user:")
	builder.WriteString(userID)
	if kind := metadataIdentityKind(mem); kind != "" {
		builder.WriteString("|kind:")
		builder.WriteString(string(kind))
	}
	if mem != nil && mem.EventTime != nil {
		builder.WriteString("|event_time:")
		builder.WriteString(
			mem.EventTime.UTC().Format("2006-01-02T15:04:05Z07:00"),
		)
	}
	if participants := metadataIdentityParticipants(mem); len(participants) > 0 {
		builder.WriteString("|participants:")
		builder.WriteString(strings.Join(participants, ","))
	}
	if location := metadataIdentityLocation(mem); location != "" {
		builder.WriteString("|location:")
		builder.WriteString(location)
	}

	hash := sha256.Sum256([]byte(builder.String()))
	return fmt.Sprintf("%x", hash)
}

func scoreMemoryEntry(entry *memory.Entry, query string) float64 {
	if entry == nil || entry.Memory == nil {
		return 0
	}

	query = strings.TrimSpace(query)
	if query == "" {
		return 0
	}

	tokens := buildSearchTokens(query)
	if len(tokens) == 0 {
		queryLower := strings.ToLower(query)
		contentLower := strings.ToLower(entry.Memory.Memory)
		if strings.Contains(contentLower, queryLower) {
			return fallbackScore
		}
		for _, topic := range entry.Memory.Topics {
			if strings.Contains(strings.ToLower(topic), queryLower) {
				return fallbackScore
			}
		}
		return 0
	}

	contentLower := strings.ToLower(entry.Memory.Memory)
	matched := 0
	for _, token := range tokens {
		if token == "" {
			continue
		}
		if strings.Contains(contentLower, token) {
			matched++
			continue
		}
		for _, topic := range entry.Memory.Topics {
			if strings.Contains(strings.ToLower(topic), token) {
				matched++
				break
			}
		}
	}
	return float64(matched) / float64(len(tokens))
}

func buildSearchTokens(query string) []string {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return nil
	}

	if hasCJK(query) {
		return buildCJKTokens(query)
	}
	return buildAlphaTokens(query)
}

func hasCJK(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func buildCJKTokens(query string) []string {
	seg, err := getSegmenter()
	if err != nil {
		return nil
	}

	words := seg.CutSearch(query, true)
	tokens := make([]string, 0, len(words))
	for _, word := range words {
		word = strings.TrimSpace(word)
		if word == "" || isCJKStopword(word) {
			continue
		}
		if isPunctToken(word) {
			continue
		}
		tokens = append(tokens, word)
	}
	return dedupStrings(tokens)
}

func buildAlphaTokens(query string) []string {
	buf := make([]rune, 0, len(query))
	for _, r := range query {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			buf = append(buf, r)
			continue
		}
		buf = append(buf, ' ')
	}

	parts := strings.Fields(string(buf))
	tokens := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) < minSearchTokenLen {
			continue
		}
		if isStopword(part) {
			continue
		}
		tokens = append(tokens, part)
	}
	return dedupStrings(tokens)
}

func isCJKStopword(word string) bool {
	_, ok := cjkStopwords[word]
	return ok
}

func isPunctToken(s string) bool {
	for _, r := range s {
		if !isPunct(r) {
			return false
		}
	}
	return true
}

func isPunct(r rune) bool {
	return unicode.IsPunct(r) || unicode.IsSymbol(r)
}

func isStopword(word string) bool {
	switch word {
	case "a", "an", "the", "and", "or", "of", "in", "on", "to",
		"for", "with", "is", "are", "am", "be":
		return true
	default:
		return false
	}
}

func dedupStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func metadataIdentityKind(mem *memory.Memory) memory.Kind {
	if mem == nil {
		return ""
	}
	hasEventMetadata := mem.EventTime != nil ||
		len(mem.Participants) > 0 ||
		strings.TrimSpace(mem.Location) != ""
	if mem.Kind != "" && mem.Kind != memory.KindFact {
		return mem.Kind
	}
	if hasEventMetadata {
		return memory.KindFact
	}
	return ""
}

func effectiveKind(mem *memory.Memory) memory.Kind {
	if mem == nil {
		return ""
	}
	if mem.Kind != "" {
		return mem.Kind
	}
	return memory.KindFact
}

func metadataIdentityParticipants(mem *memory.Memory) []string {
	if mem == nil {
		return nil
	}
	participants := make([]string, 0, len(mem.Participants))
	for _, participant := range mem.Participants {
		participant = strings.TrimSpace(participant)
		if participant == "" {
			continue
		}
		participants = append(participants, participant)
	}
	if len(participants) == 0 {
		return nil
	}
	sort.Slice(participants, func(i int, j int) bool {
		left := strings.ToLower(participants[i])
		right := strings.ToLower(participants[j])
		if left != right {
			return left < right
		}
		return participants[i] < participants[j]
	})

	out := make([]string, 0, len(participants))
	var prev string
	for _, participant := range participants {
		folded := strings.ToLower(participant)
		if len(out) > 0 && folded == prev {
			continue
		}
		out = append(out, participant)
		prev = folded
	}
	return out
}

func metadataIdentityLocation(mem *memory.Memory) string {
	if mem == nil {
		return ""
	}
	return strings.TrimSpace(mem.Location)
}

func normalizeAddMetadata(
	metadataValue *memory.Metadata,
) *memory.Metadata {
	if metadataValue == nil {
		return nil
	}
	normalized := &memory.Metadata{
		Kind:      metadataValue.Kind,
		EventTime: metadataValue.EventTime,
		Participants: metadataIdentityParticipants(&memory.Memory{
			Participants: metadataValue.Participants,
		}),
		Location: strings.TrimSpace(metadataValue.Location),
	}
	if normalized.Kind == "" &&
		(normalized.EventTime != nil ||
			len(normalized.Participants) > 0 ||
			normalized.Location != "") {
		normalized.Kind = memory.KindFact
	}
	return normalized
}

func normalizeUpdateMetadata(
	metadataValue *memory.Metadata,
) *memory.Metadata {
	if metadataValue == nil {
		return nil
	}
	return &memory.Metadata{
		Kind:      metadataValue.Kind,
		EventTime: metadataValue.EventTime,
		Participants: metadataIdentityParticipants(&memory.Memory{
			Participants: metadataValue.Participants,
		}),
		Location: strings.TrimSpace(metadataValue.Location),
	}
}

func normalizeMemory(mem *memory.Memory) {
	if mem == nil {
		return
	}
	mem.Kind = effectiveKind(mem)
	mem.Participants = metadataIdentityParticipants(mem)
	mem.Location = strings.TrimSpace(mem.Location)
}

func newMemoryRecord(
	memoryText string,
	topics []string,
	metadataValue *memory.Metadata,
	now time.Time,
) *memory.Memory {
	mem := &memory.Memory{
		Memory:      memoryText,
		Topics:      dedupStrings(topics),
		LastUpdated: &now,
	}
	applyMemoryMetadata(mem, metadataValue)
	return mem
}

func applyMemoryMetadata(
	mem *memory.Memory,
	metadataValue *memory.Metadata,
) {
	if mem == nil {
		return
	}
	if metadataValue != nil {
		metadataValue = normalizeAddMetadata(metadataValue)
		if metadataValue.Kind != "" {
			mem.Kind = metadataValue.Kind
		}
		mem.EventTime = metadataValue.EventTime
		mem.Participants = metadataValue.Participants
		mem.Location = metadataValue.Location
	}
	normalizeMemory(mem)
}

func applyMemoryMetadataPatch(
	mem *memory.Memory,
	metadataValue *memory.Metadata,
) {
	if mem == nil {
		return
	}
	if metadataValue != nil {
		metadataValue = normalizeUpdateMetadata(metadataValue)
		if metadataValue.Kind != "" {
			mem.Kind = metadataValue.Kind
		}
		if metadataValue.EventTime != nil {
			mem.EventTime = metadataValue.EventTime
		}
		if len(metadataValue.Participants) > 0 {
			mem.Participants = metadataValue.Participants
		}
		if metadataValue.Location != "" {
			mem.Location = metadataValue.Location
		}
	}
	normalizeMemory(mem)
}

func cloneMemory(mem *memory.Memory) *memory.Memory {
	if mem == nil {
		return &memory.Memory{}
	}
	cloned := *mem
	cloned.Topics = slicesClone(mem.Topics)
	cloned.Participants = slicesClone(mem.Participants)
	if mem.LastUpdated != nil {
		lastUpdated := *mem.LastUpdated
		cloned.LastUpdated = &lastUpdated
	}
	if mem.EventTime != nil {
		eventTime := *mem.EventTime
		cloned.EventTime = &eventTime
	}
	return &cloned
}

func slicesClone(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func searchMemoryEntries(
	entries []*memory.Entry,
	opts memory.SearchOptions,
) []*memory.Entry {
	query := strings.TrimSpace(opts.Query)
	if query == "" {
		return []*memory.Entry{}
	}

	threshold := defaultSearchMinScore
	if opts.SimilarityThreshold > 0 {
		threshold = opts.SimilarityThreshold
	}
	limit := defaultSearchMaxResults
	if opts.MaxResults > 0 {
		limit = opts.MaxResults
	}

	candidates := scoreEntries(entries, query, threshold)
	results := filterAndSortEntries(candidates, opts)
	output := cloneScoredEntries(results)
	if opts.Kind != "" && opts.KindFallback &&
		len(output) < minKindFallbackResults {
		fallbackOpts := opts
		fallbackOpts.Kind = ""
		fallbackOpts.KindFallback = false
		fallback := cloneScoredEntries(
			filterAndSortEntries(candidates, fallbackOpts),
		)
		output = mergeSearchResults(output, fallback, opts.Kind, limit)
	}
	if opts.Deduplicate && len(output) > 1 {
		output = deduplicateResults(output)
	}
	if limit > 0 && len(output) > limit {
		output = output[:limit]
	}
	return output
}

const (
	defaultSearchMinScore   = 0.3
	defaultSearchMaxResults = 10
	minKindFallbackResults  = 3
	jaccardThreshold        = 0.80
)

func scoreEntries(
	entries []*memory.Entry,
	query string,
	minScore float64,
) []scoredEntry {
	candidates := make([]scoredEntry, 0, len(entries))
	for _, entry := range entries {
		score := scoreMemoryEntry(entry, query)
		if score < minScore {
			continue
		}
		candidates = append(candidates, scoredEntry{
			entry: entry,
			score: score,
		})
	}
	return candidates
}

func filterAndSortEntries(
	candidates []scoredEntry,
	opts memory.SearchOptions,
) []scoredEntry {
	filtered := make([]scoredEntry, 0, len(candidates))
	for _, candidate := range candidates {
		if !matchesSearchFilters(candidate.entry, opts) {
			continue
		}
		filtered = append(filtered, candidate)
	}

	sort.Slice(filtered, func(i int, j int) bool {
		if opts.OrderByEventTime {
			left := entryEventTime(filtered[i].entry)
			right := entryEventTime(filtered[j].entry)
			switch {
			case left == nil && right != nil:
				return false
			case left != nil && right == nil:
				return true
			case left != nil && right != nil && !left.Equal(*right):
				return left.Before(*right)
			}
		}
		if filtered[i].score != filtered[j].score {
			return filtered[i].score > filtered[j].score
		}
		if !filtered[i].entry.UpdatedAt.Equal(filtered[j].entry.UpdatedAt) {
			return filtered[i].entry.UpdatedAt.After(
				filtered[j].entry.UpdatedAt,
			)
		}
		if !filtered[i].entry.CreatedAt.Equal(filtered[j].entry.CreatedAt) {
			return filtered[i].entry.CreatedAt.After(
				filtered[j].entry.CreatedAt,
			)
		}
		return filtered[i].entry.ID < filtered[j].entry.ID
	})
	return filtered
}

func matchesSearchFilters(
	entry *memory.Entry,
	opts memory.SearchOptions,
) bool {
	if entry == nil || entry.Memory == nil {
		return false
	}
	if opts.Kind != "" && effectiveKind(entry.Memory) != opts.Kind {
		return false
	}
	if opts.TimeAfter != nil && entry.Memory.EventTime != nil &&
		entry.Memory.EventTime.Before(*opts.TimeAfter) {
		return false
	}
	if opts.TimeBefore != nil && entry.Memory.EventTime != nil &&
		entry.Memory.EventTime.After(*opts.TimeBefore) {
		return false
	}
	return true
}

func entryEventTime(entry *memory.Entry) *time.Time {
	if entry == nil || entry.Memory == nil {
		return nil
	}
	return entry.Memory.EventTime
}

func cloneScoredEntries(candidates []scoredEntry) []*memory.Entry {
	results := make([]*memory.Entry, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.entry == nil {
			continue
		}
		cloned := *candidate.entry
		cloned.Score = candidate.score
		results = append(results, &cloned)
	}
	return results
}

func mergeSearchResults(
	primary []*memory.Entry,
	fallback []*memory.Entry,
	preferredKind memory.Kind,
	maxResults int,
) []*memory.Entry {
	seen := make(map[string]struct{}, len(primary))
	for _, entry := range primary {
		seen[entry.ID] = struct{}{}
	}

	var kindMatch []*memory.Entry
	var kindOther []*memory.Entry
	for _, entry := range fallback {
		if _, ok := seen[entry.ID]; ok {
			continue
		}
		if effectiveKind(entry.Memory) == preferredKind {
			kindMatch = append(kindMatch, entry)
			continue
		}
		kindOther = append(kindOther, entry)
	}

	merged := make([]*memory.Entry, 0, len(primary)+len(fallback))
	merged = append(merged, primary...)
	merged = append(merged, kindMatch...)
	merged = append(merged, kindOther...)
	if maxResults > 0 && len(merged) > maxResults {
		return merged[:maxResults]
	}
	return merged
}

func deduplicateResults(results []*memory.Entry) []*memory.Entry {
	if len(results) <= 1 {
		return results
	}
	out := make([]*memory.Entry, 0, len(results))
	wordSets := make([]map[string]struct{}, len(results))
	for i, entry := range results {
		wordSets[i] = buildWordSet(entry)
		duplicate := false
		for _, kept := range out {
			if jaccardSimilarity(wordSets[i], buildWordSet(kept)) >
				jaccardThreshold {
				duplicate = true
				break
			}
		}
		if !duplicate {
			out = append(out, entry)
		}
	}
	return out
}

func buildWordSet(entry *memory.Entry) map[string]struct{} {
	set := make(map[string]struct{})
	if entry == nil || entry.Memory == nil {
		return set
	}
	for _, word := range strings.Fields(strings.ToLower(entry.Memory.Memory)) {
		set[word] = struct{}{}
	}
	return set
}

func jaccardSimilarity(
	left map[string]struct{},
	right map[string]struct{},
) float64 {
	if len(left) == 0 && len(right) == 0 {
		return 1
	}
	intersection := 0
	union := len(left)
	seen := make(map[string]struct{}, len(left))
	for key := range left {
		seen[key] = struct{}{}
	}
	for key := range right {
		if _, ok := seen[key]; ok {
			intersection++
			continue
		}
		union++
	}
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}
