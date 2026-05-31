package persona

import (
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"git.woa.com/trpc-go/trpc-agent-go/openclaw/promptasset"
	"gopkg.in/yaml.v3"
)

const (
	PragmaticID     = "pragmatic"
	DefaultID       = PragmaticID
	LegacyDefaultID = "default"
	FriendlyID      = "friendly"
	ProfessionalID  = "professional"
	ConciseID       = "concise"
	CoachID         = "coach"
	CreativeID      = "creative"
	CandidID        = "candid"
	QuirkyID        = "quirky"
	NerdyID         = "nerdy"
	SnarkyID        = "snarky"
	GirlfriendID    = "girlfriend"
	BoyfriendID     = "boyfriend"

	fileExt           = ".md"
	fileHeaderFence   = "---"
	filePerm          = 0o600
	dirPerm           = 0o700
	summaryMaxRunes   = 48
	nameMaxRunes      = 24
	maxPersonaIDRunes = 48

	autoIDPrefix         = "persona"
	defaultGeneratedName = "自定义人格"
)

var builtinAliases = map[string]string{
	LegacyDefaultID: DefaultID,
}

type Definition struct {
	ID      string
	Name    string
	Summary string
	Prompt  string
	BuiltIn bool
	Path    string
}

type Registry struct {
	dir string
}

type fileHeader struct {
	Name    string `yaml:"name,omitempty"`
	Summary string `yaml:"summary,omitempty"`
}

var defaultPersonaFileOrder = []string{
	PragmaticID + fileExt,
	SnarkyID + fileExt,
	GirlfriendID + fileExt,
	BoyfriendID + fileExt,
	QuirkyID + fileExt,
	CreativeID + fileExt,
	NerdyID + fileExt,
	FriendlyID + fileExt,
	CoachID + fileExt,
	CandidID + fileExt,
	ConciseID + fileExt,
	ProfessionalID + fileExt,
}

var (
	defaultDefinitionsOnce sync.Once
	defaultDefinitionsMemo []Definition
	defaultDefinitionsByID map[string]Definition
	defaultDefinitionsErr  error
)

func NewRegistry(dir string) *Registry {
	return &Registry{dir: strings.TrimSpace(dir)}
}

func (r *Registry) Dir() string {
	if r == nil {
		return ""
	}
	return r.dir
}

func Builtins() []Definition {
	defs := mustDefaultDefinitions()
	return defs
}

func BuiltinIDList() string {
	defs := mustDefaultDefinitions()
	ids := make([]string, 0, len(defs))
	for _, def := range defs {
		ids = append(ids, def.ID)
	}
	return strings.Join(ids, ", ")
}

func LookupBuiltin(raw string) (Definition, bool) {
	loadDefaultDefinitions()
	if defaultDefinitionsErr != nil {
		return Definition{}, false
	}
	if def, ok := defaultDefinitionsByID[NormalizeID(raw)]; ok {
		return def, true
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Definition{}, false
	}
	for _, def := range defaultDefinitionsMemo {
		if sameLookupKey(def.Name, raw) {
			return def, true
		}
	}
	return Definition{}, false
}

func loadDefaultDefinitions() {
	defaultDefinitionsOnce.Do(func() {
		files, err := promptasset.ReadEmbeddedFiles(
			promptasset.DefaultPersonasEmbeddedDir,
		)
		if err != nil {
			defaultDefinitionsErr = err
			return
		}
		defs := make([]Definition, 0, len(defaultPersonaFileOrder))
		for _, name := range defaultPersonaFileOrder {
			data, ok := files[name]
			if !ok {
				defaultDefinitionsErr = fmt.Errorf(
					"persona: missing default asset %s",
					name,
				)
				return
			}
			id := strings.TrimSuffix(name, fileExt)
			def, err := parseDefinitionFile(
				[]byte(data),
				name,
				id,
			)
			if err != nil {
				defaultDefinitionsErr = err
				return
			}
			def.BuiltIn = true
			def.Path = ""
			defs = append(defs, def)
		}
		defaultDefinitionsMemo = defs
		defaultDefinitionsByID = make(map[string]Definition, len(defs))
		for _, def := range defs {
			defaultDefinitionsByID[def.ID] = def
		}
	})
}

func mustDefaultDefinitions() []Definition {
	loadDefaultDefinitions()
	if defaultDefinitionsErr != nil {
		panic(defaultDefinitionsErr)
	}
	defs := make([]Definition, len(defaultDefinitionsMemo))
	copy(defs, defaultDefinitionsMemo)
	return defs
}

func NormalizeID(raw string) string {
	id := strings.ToLower(strings.TrimSpace(raw))
	if alias, ok := builtinAliases[id]; ok {
		return alias
	}
	return id
}

func IsDefault(raw string) bool {
	return NormalizeID(raw) == DefaultID
}

func ValidateID(raw string) (string, error) {
	id := NormalizeID(raw)
	if id == "" {
		return "", errorsf("persona id is required")
	}
	if len([]rune(id)) > maxPersonaIDRunes {
		return "", errorsf(
			"persona id is too long (max %d runes)",
			maxPersonaIDRunes,
		)
	}
	if !isValidID(id) {
		return "", errorsf(
			"persona id %q is invalid "+
				"(use letters, digits, - or _)",
			id,
		)
	}
	return id, nil
}

func ValidateCustomID(raw string) (string, error) {
	id, err := ValidateID(raw)
	if err != nil {
		return "", err
	}
	if _, ok := LookupBuiltin(id); ok {
		return "", errorsf("persona %q is reserved", id)
	}
	return id, nil
}

func ValidateName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", errorsf("persona name is required")
	}
	if len([]rune(name)) > nameMaxRunes {
		return "", errorsf(
			"persona name is too long (max %d runes)",
			nameMaxRunes,
		)
	}
	return name, nil
}

func (r *Registry) List() ([]Definition, error) {
	defs, err := r.loadDefaultDefinitions()
	if err != nil {
		return nil, err
	}
	custom, err := r.loadCustom()
	if err != nil {
		return nil, err
	}
	return append(defs, custom...), nil
}

func (r *Registry) Get(raw string) (Definition, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Definition{}, false, nil
	}
	defaults, err := r.loadDefaultDefinitions()
	if err != nil {
		return Definition{}, false, err
	}
	for _, def := range defaults {
		if matchesDefinitionLookup(def, raw) {
			return def, true, nil
		}
	}
	if r == nil || strings.TrimSpace(r.dir) == "" {
		return Definition{}, false, nil
	}
	if id, err := ValidateID(raw); err == nil {
		path := r.pathForID(id)
		def, readErr := readDefinitionFile(path, id)
		if readErr == nil {
			return def, true, nil
		}
		if !errors.Is(readErr, os.ErrNotExist) {
			return Definition{}, false, readErr
		}
	}
	custom, err := r.loadCustom()
	if err != nil {
		return Definition{}, false, err
	}
	for _, def := range custom {
		if matchesDefinitionLookup(def, raw) {
			return def, true, nil
		}
	}
	return Definition{}, false, nil
}

func (r *Registry) Save(rawName string, prompt string) (Definition, error) {
	if r == nil || strings.TrimSpace(r.dir) == "" {
		return Definition{}, errorsf("persona_dir is not configured")
	}
	name, err := ValidateName(rawName)
	if err != nil {
		return Definition{}, err
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return Definition{}, errorsf("persona prompt is required")
	}
	existing, err := r.lookupCustom(rawName)
	if err != nil {
		return Definition{}, err
	}
	id, name, err := r.resolveSaveTarget(name, existing)
	if err != nil {
		return Definition{}, err
	}

	def := Definition{
		ID:      id,
		Name:    name,
		Summary: SummarizePrompt(prompt),
		Prompt:  prompt,
		Path:    r.pathForID(id),
	}
	data, err := marshalDefinitionFile(def)
	if err != nil {
		return Definition{}, err
	}
	if err := os.MkdirAll(r.dir, dirPerm); err != nil {
		return Definition{}, fmt.Errorf(
			"persona: create dir: %w",
			err,
		)
	}
	if err := os.WriteFile(def.Path, data, filePerm); err != nil {
		return Definition{}, fmt.Errorf(
			"persona: write %s: %w",
			def.Path,
			err,
		)
	}
	return def, nil
}

func (r *Registry) Create(prompt string) (Definition, error) {
	if r == nil || strings.TrimSpace(r.dir) == "" {
		return Definition{}, errorsf("persona_dir is not configured")
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return Definition{}, errorsf("persona prompt is required")
	}
	name, err := r.uniqueGeneratedName(prompt)
	if err != nil {
		return Definition{}, err
	}
	return r.Save(name, prompt)
}

func (r *Registry) Delete(raw string) error {
	if r == nil || strings.TrimSpace(r.dir) == "" {
		return errorsf("persona_dir is not configured")
	}
	if builtin, ok := LookupBuiltin(raw); ok {
		path := r.pathForID(builtin.ID)
		if err := os.Remove(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return errorsf(
					"persona %q does not exist",
					strings.TrimSpace(raw),
				)
			}
			return fmt.Errorf("persona: delete %s: %w", path, err)
		}
		return nil
	}
	def, err := r.lookupCustom(raw)
	if err != nil {
		return err
	}
	if !def.BuiltIn && strings.TrimSpace(def.ID) == "" {
		return errorsf(
			"persona %q does not exist",
			strings.TrimSpace(raw),
		)
	}
	if err := os.Remove(def.Path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errorsf(
				"persona %q does not exist",
				strings.TrimSpace(raw),
			)
		}
		return fmt.Errorf("persona: delete %s: %w", def.Path, err)
	}
	return nil
}

func (r *Registry) Upsert(
	rawID string,
	rawName string,
	prompt string,
) (Definition, error) {
	if r == nil || strings.TrimSpace(r.dir) == "" {
		return Definition{}, errorsf("persona_dir is not configured")
	}
	id, err := ValidateID(rawID)
	if err != nil {
		return Definition{}, err
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return Definition{}, errorsf("persona prompt is required")
	}

	name, err := r.upsertName(id, rawName)
	if err != nil {
		return Definition{}, err
	}
	def := Definition{
		ID:      id,
		Name:    name,
		Summary: SummarizePrompt(prompt),
		Prompt:  prompt,
		Path:    r.pathForID(id),
	}
	data, err := marshalDefinitionFile(def)
	if err != nil {
		return Definition{}, err
	}
	if err := os.MkdirAll(r.dir, dirPerm); err != nil {
		return Definition{}, fmt.Errorf(
			"persona: create dir: %w",
			err,
		)
	}
	if err := os.WriteFile(def.Path, data, filePerm); err != nil {
		return Definition{}, fmt.Errorf(
			"persona: write %s: %w",
			def.Path,
			err,
		)
	}
	if builtin, ok := LookupBuiltin(id); ok {
		def.BuiltIn = true
		if strings.TrimSpace(def.Name) == "" {
			def.Name = builtin.Name
		}
	}
	return def, nil
}

func SummarizePrompt(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ""
	}
	lines := strings.Split(prompt, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		return truncateRunes(line, summaryMaxRunes)
	}
	return truncateRunes(prompt, summaryMaxRunes)
}

func (r *Registry) loadCustom() ([]Definition, error) {
	if r == nil || strings.TrimSpace(r.dir) == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf(
			"persona: read dir %s: %w",
			r.dir,
			err,
		)
	}
	defs := make([]Definition, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.TrimSpace(entry.Name())
		if filepath.Ext(name) != fileExt {
			continue
		}
		id := strings.TrimSuffix(name, fileExt)
		id, err = ValidateCustomID(id)
		if err != nil {
			continue
		}
		def, err := readDefinitionFile(r.pathForID(id), id)
		if err != nil {
			return nil, err
		}
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i int, j int) bool {
		return defs[i].ID < defs[j].ID
	})
	return defs, nil
}

func (r *Registry) pathForID(id string) string {
	return filepath.Join(r.dir, id+fileExt)
}

func (r *Registry) lookupCustom(raw string) (Definition, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Definition{}, nil
	}
	if def, ok := LookupBuiltin(raw); ok {
		return Definition{}, errorsf(
			"persona %q is reserved",
			defaultString(def.Name, raw),
		)
	}
	custom, err := r.loadCustom()
	if err != nil {
		return Definition{}, err
	}
	for _, def := range custom {
		if matchesDefinitionLookup(def, raw) {
			return def, nil
		}
	}
	return Definition{}, nil
}

func (r *Registry) loadDefaultDefinitions() ([]Definition, error) {
	defs := mustDefaultDefinitions()
	if r == nil || strings.TrimSpace(r.dir) == "" {
		return defs, nil
	}

	for i, def := range defs {
		override, err := readDefinitionFile(
			r.pathForID(def.ID),
			def.ID,
		)
		if err == nil {
			override.BuiltIn = true
			defs[i] = override
			continue
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	return defs, nil
}

func (r *Registry) resolveSaveTarget(
	name string,
	existing Definition,
) (string, string, error) {
	if strings.TrimSpace(existing.ID) != "" {
		return existing.ID, existing.Name, nil
	}
	defs, err := r.List()
	if err != nil {
		return "", "", err
	}
	if reservedPersonaName(defs, name) {
		return "", "", errorsf(
			"persona name %q is reserved",
			name,
		)
	}
	id := nextAvailableID(defs, candidateID(name))
	return id, normalizeDisplayName(name, id), nil
}

func (r *Registry) upsertName(
	id string,
	rawName string,
) (string, error) {
	if builtin, ok := LookupBuiltin(id); ok {
		name := strings.TrimSpace(rawName)
		if name == "" {
			return builtin.Name, nil
		}
		return ValidateName(name)
	}

	name := strings.TrimSpace(rawName)
	if name == "" {
		existing, err := readDefinitionFile(
			r.pathForID(id),
			id,
		)
		if err == nil {
			name = existing.Name
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	if name == "" {
		name = humanizeID(id)
	}
	return ValidateName(name)
}

func (r *Registry) uniqueGeneratedName(prompt string) (string, error) {
	defs, err := r.List()
	if err != nil {
		return "", err
	}
	base := SuggestedName(prompt)
	if base == "" {
		base = defaultGeneratedName
	}
	base, err = ValidateName(base)
	if err != nil {
		base = defaultGeneratedName
	}
	for index := 1; ; index++ {
		name := base
		if index > 1 {
			name = base + " " + strconv.Itoa(index)
		}
		if !lookupNameTaken(defs, name) {
			return name, nil
		}
	}
}

func readDefinitionFile(path string, id string) (Definition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Definition{}, fmt.Errorf(
			"persona: read %s: %w",
			path,
			err,
		)
	}
	return parseDefinitionFile(data, path, id)
}

func parseDefinitionFile(
	data []byte,
	path string,
	id string,
) (Definition, error) {
	header, prompt, err := splitDefinitionFile(string(data))
	if err != nil {
		return Definition{}, fmt.Errorf(
			"persona: parse %s: %w",
			path,
			err,
		)
	}
	def := Definition{
		ID:      id,
		Name:    strings.TrimSpace(header.Name),
		Summary: strings.TrimSpace(header.Summary),
		Prompt:  strings.TrimSpace(prompt),
		Path:    path,
	}
	if def.Name == "" {
		def.Name = humanizeID(id)
	}
	if def.Summary == "" {
		def.Summary = SummarizePrompt(def.Prompt)
	}
	return def, nil
}

func marshalDefinitionFile(def Definition) ([]byte, error) {
	header := fileHeader{
		Name:    strings.TrimSpace(def.Name),
		Summary: strings.TrimSpace(def.Summary),
	}
	data, err := yaml.Marshal(&header)
	if err != nil {
		return nil, fmt.Errorf("persona: encode header: %w", err)
	}
	text := fileHeaderFence + "\n" +
		string(data) +
		fileHeaderFence + "\n\n" +
		strings.TrimSpace(def.Prompt) + "\n"
	return []byte(text), nil
}

func splitDefinitionFile(raw string) (fileHeader, string, error) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fileHeader{}, "", nil
	}
	lines := strings.Split(raw, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != fileHeaderFence {
		return fileHeader{}, raw, nil
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == fileHeaderFence {
			end = i
			break
		}
	}
	if end < 0 {
		return fileHeader{}, "", errorsf("missing closing front matter")
	}
	var header fileHeader
	meta := strings.Join(lines[1:end], "\n")
	if err := yaml.Unmarshal([]byte(meta), &header); err != nil {
		return fileHeader{}, "", fmt.Errorf(
			"decode front matter: %w",
			err,
		)
	}
	prompt := strings.Join(lines[end+1:], "\n")
	return header, strings.TrimSpace(prompt), nil
}

func isValidID(id string) bool {
	if id == "" {
		return false
	}
	runes := []rune(id)
	for i, r := range runes {
		switch {
		case isIDLetter(r):
		case isIDDigit(r):
		case r == '-' || r == '_':
		default:
			return false
		}
		if i == 0 && !isIDLetter(r) {
			return false
		}
	}
	return true
}

func isIDLetter(r rune) bool {
	return unicode.IsLetter(r)
}

func isIDDigit(r rune) bool {
	return unicode.IsDigit(r)
}

func humanizeID(id string) string {
	parts := strings.FieldsFunc(id, func(r rune) bool {
		return r == '-' || r == '_'
	})
	if len(parts) == 0 {
		return id
	}
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = upperFirstRune(part)
	}
	return strings.Join(parts, " ")
}

func upperFirstRune(text string) string {
	runes := []rune(text)
	if len(runes) == 0 {
		return text
	}
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func truncateRunes(text string, max int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= max {
		return string(runes)
	}
	return string(runes[:max]) + "..."
}

func errorsf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}

func sameLookupKey(left string, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return false
	}
	return strings.EqualFold(left, right)
}

func matchesDefinitionLookup(def Definition, raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	if sameLookupKey(def.ID, raw) {
		return true
	}
	return sameLookupKey(def.Name, raw)
}

func normalizeDisplayName(name string, id string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return humanizeID(id)
	}
	if normalized, err := ValidateID(name); err == nil {
		return humanizeID(normalized)
	}
	return name
}

func reservedPersonaName(defs []Definition, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, def := range defs {
		if def.BuiltIn && sameLookupKey(def.Name, name) {
			return true
		}
	}
	return false
}

func lookupNameTaken(defs []Definition, name string) bool {
	for _, def := range defs {
		if sameLookupKey(def.Name, name) {
			return true
		}
	}
	return false
}

func candidateID(name string) string {
	builder := strings.Builder{}
	lastDash := false
	for _, r := range strings.TrimSpace(name) {
		lowered := unicode.ToLower(r)
		switch {
		case isIDLetter(lowered):
			builder.WriteRune(lowered)
			lastDash = false
		case isIDDigit(lowered):
			if builder.Len() == 0 {
				continue
			}
			builder.WriteRune(lowered)
			lastDash = false
		case lowered == '-' || lowered == '_':
			if builder.Len() == 0 || lastDash {
				continue
			}
			builder.WriteByte('-')
			lastDash = true
		case unicode.IsSpace(lowered):
			if builder.Len() == 0 || lastDash {
				continue
			}
			builder.WriteByte('-')
			lastDash = true
		case unicode.IsPunct(lowered),
			unicode.IsSymbol(lowered):
			if builder.Len() == 0 || lastDash {
				continue
			}
			builder.WriteByte('-')
			lastDash = true
		}
	}
	id := strings.Trim(builder.String(), "-")
	if _, err := ValidateCustomID(id); err == nil {
		return id
	}
	return autoIDPrefix + "-" + shortHash(name)
}

func nextAvailableID(defs []Definition, base string) string {
	base = NormalizeID(strings.TrimSpace(base))
	if _, err := ValidateCustomID(base); err != nil {
		base = autoIDPrefix + "-" + shortHash(base)
	}
	id := base
	for index := 2; idTaken(defs, id); index++ {
		id = base + "-" + strconv.Itoa(index)
	}
	return id
}

func idTaken(defs []Definition, id string) bool {
	for _, def := range defs {
		if sameLookupKey(def.ID, id) {
			return true
		}
	}
	return false
}

func shortHash(text string) string {
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(text))
	return fmt.Sprintf("%08x", hasher.Sum32())
}

func SuggestedName(prompt string) string {
	line := SummarizePrompt(prompt)
	line = trimPersonaLead(line)
	line = splitPersonaName(line)
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultGeneratedName
	}
	return truncateRunes(line, nameMaxRunes)
}

func trimPersonaLead(text string) string {
	prefixes := []string{
		"你是一个",
		"你是位",
		"你是名",
		"你是",
		"请你做一个",
		"请你做",
		"请你成为",
		"请你扮演",
		"请以",
		"作为一个",
		"作为",
		"you are ",
		"act as ",
		"be a ",
		"be an ",
	}
	trimmed := strings.TrimSpace(text)
	for _, prefix := range prefixes {
		if rest, ok := trimPrefixFold(trimmed, prefix); ok {
			return strings.TrimSpace(rest)
		}
	}
	return trimmed
}

func trimPrefixFold(text string, prefix string) (string, bool) {
	textRunes := []rune(strings.TrimSpace(text))
	prefixRunes := []rune(prefix)
	if len(textRunes) < len(prefixRunes) {
		return text, false
	}
	head := string(textRunes[:len(prefixRunes)])
	if !strings.EqualFold(head, prefix) {
		return text, false
	}
	return string(textRunes[len(prefixRunes):]), true
}

func splitPersonaName(text string) string {
	for _, separator := range []string{
		"，",
		"。",
		"；",
		";",
		",",
		"!",
		"！",
		"?",
		"？",
		":",
		"：",
	} {
		if before, _, ok := strings.Cut(text, separator); ok {
			text = before
			break
		}
	}
	return strings.TrimSpace(text)
}

func defaultString(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
