// Package envprobe registers a narrow env visibility probe tool.
package envprobe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/tool"

	"trpc.group/trpc-go/trpc-agent-go/openclaw/registry"
)

const (
	// PluginType is the registered provider type for env probing.
	PluginType = "envprobe"

	toolName = "env_probe"

	argName = "name"

	schemaTypeObject = "object"
	schemaTypeString = "string"

	envPlatformFile = "TRPC_CLAW_ENV_FILE"

	runtimeDirName     = "runtime"
	runtimeEnvFileName = "env.sh"

	shellFileBashRC  = ".bashrc"
	shellFileZshRC   = ".zshrc"
	shellFileProfile = ".profile"

	sourceNotFoundFormat = "Environment variable %s was not found in the " +
		"current trpc-claw process environment or trusted env files. " +
		"The value is hidden."
	sourcePresentFormat = "Environment variable %s is present in the " +
		"current trpc-claw process environment. The value is hidden."
	sourcePresentEmptyFormat = "Environment variable %s exists in the " +
		"current trpc-claw process environment, but its current value " +
		"looks empty. The value is hidden."
	sourceActivatedFormat = "Environment variable %s was detected in %s " +
		"and loaded into the current trpc-claw process for future tool " +
		"calls. The value is hidden."
	sourceDeclaredFormat = "Environment variable %s is declared in %s. " +
		"The value is hidden."
	sourceDeclaredEmptyFormat = "Environment variable %s is declared in " +
		"%s, but the detected value looks empty. The value is hidden."
	sourceDynamicFormat = " The declaration uses shell syntax that " +
		"env_probe does not auto-activate."

	exportPrefix = "export "
)

var sensitiveNameHints = []string{
	"TOKEN",
	"SECRET",
	"KEY",
	"PASSWORD",
	"COOKIE",
	"CREDENTIAL",
	"AUTH",
}

func init() {
	if err := registry.RegisterToolProvider(PluginType, newTools); err !=
		nil {
		panic(err)
	}
}

type probeInput struct {
	Name string `json:"name"`
}

type probeResult struct {
	Name string `json:"name"`

	PresentNow  bool `json:"present_now"`
	NonEmptyNow bool `json:"non_empty_now"`

	ActivatedNow    bool   `json:"activated_now,omitempty"`
	ActivatedSource string `json:"activated_source,omitempty"`

	DeclaredSources         []string `json:"declared_sources,omitempty"`
	NonEmptyDeclaredSources []string `json:"non_empty_declared_sources,omitempty"`

	RequiresRestartForMainProcess bool `json:"requires_restart_for_main_process"`

	Sensitive   bool   `json:"sensitive"`
	SafeMessage string `json:"safe_message"`
}

type sourceHit struct {
	Label       string
	NonEmpty    bool
	Value       string
	CanActivate bool
}

type sourceSpec struct {
	Label string
	Path  string
}

type resolver struct {
	stateDir string
	homeDir  string

	lookupEnv func(string) (string, bool)
	setEnv    func(string, string) error
	readFile  func(string) ([]byte, error)
}

type probeTool struct {
	resolver resolver
}

func newTools(
	deps registry.ToolProviderDeps,
	spec registry.PluginSpec,
) ([]tool.Tool, error) {
	if spec.Config != nil {
		var cfg struct{}
		if err := registry.DecodeStrict(spec.Config, &cfg); err != nil {
			return nil, err
		}
	}
	return []tool.Tool{
		probeTool{resolver: newResolver(deps.StateDir)},
	}, nil
}

func newResolver(stateDir string) resolver {
	homeDir, _ := os.UserHomeDir()
	return resolver{
		stateDir:  strings.TrimSpace(stateDir),
		homeDir:   strings.TrimSpace(homeDir),
		lookupEnv: os.LookupEnv,
		setEnv:    os.Setenv,
		readFile:  os.ReadFile,
	}
}

func (t probeTool) Declaration() *tool.Declaration {
	return &tool.Declaration{
		Name: toolName,
		Description: "Safely check whether one environment variable is " +
			"available to the current trpc-claw process or declared " +
			"in trusted local env files. Use this when the user asks " +
			"whether a token, secret, API key, or shell-configured " +
			"environment variable is visible. Never exposes the " +
			"value. When a trusted file contains a simple static " +
			"declaration and the current process lacks it, " +
			"env_probe activates it for future tool calls. " +
			"Trusted sources include the current process, " +
			"TRPC_CLAW_ENV_FILE, runtime/env.sh, ~/.bashrc, " +
			"~/.zshrc, and ~/.profile.",
		InputSchema: &tool.Schema{
			Type:     schemaTypeObject,
			Required: []string{argName},
			Properties: map[string]*tool.Schema{
				argName: {
					Type: schemaTypeString,
					Description: "Environment variable name to " +
						"check, for example TAIHU_PAT_TOKEN.",
				},
			},
		},
	}
}

func (t probeTool) Call(
	_ context.Context,
	args []byte,
) (any, error) {
	var in probeInput
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, fmt.Errorf("invalid args: %w", err)
	}
	return t.resolver.Probe(in.Name)
}

func (r resolver) Probe(name string) (probeResult, error) {
	key := strings.TrimSpace(name)
	if !isValidEnvName(key) {
		return probeResult{}, fmt.Errorf(
			"env_probe: invalid env name %q",
			name,
		)
	}

	result := probeResult{
		Name:      key,
		Sensitive: isSensitiveEnvName(key),
	}
	if value, ok := r.lookupEnv(key); ok {
		result.PresentNow = true
		result.NonEmptyNow = strings.TrimSpace(value) != ""
	}

	hits, err := r.findSourceHits(key)
	if err != nil {
		return probeResult{}, err
	}
	result.DeclaredSources = sourceLabels(hits, false)
	result.NonEmptyDeclaredSources = sourceLabels(hits, true)
	if !result.NonEmptyNow {
		activated, source, err := r.activateDeclaredValue(key, hits)
		if err != nil {
			return probeResult{}, err
		}
		if activated {
			result.PresentNow = true
			result.NonEmptyNow = true
			result.ActivatedNow = true
			result.ActivatedSource = source
		}
	}
	result.RequiresRestartForMainProcess = needsMainRestart(result)
	result.SafeMessage = buildSafeMessage(result)
	return result, nil
}

func (r resolver) activateDeclaredValue(
	name string,
	hits []sourceHit,
) (bool, string, error) {
	for _, hit := range hits {
		if !hit.NonEmpty || !hit.CanActivate {
			continue
		}
		if err := r.setEnv(name, hit.Value); err != nil {
			return false, "", fmt.Errorf(
				"env_probe: activate %s from %s: %w",
				name,
				hit.Label,
				err,
			)
		}
		return true, hit.Label, nil
	}
	return false, "", nil
}

func (r resolver) findSourceHits(name string) ([]sourceHit, error) {
	sources := r.trustedSources()
	hits := make([]sourceHit, 0, len(sources))
	seen := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		if strings.TrimSpace(source.Path) == "" {
			continue
		}
		found, hit, err := r.findInFile(source.Path, name)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		if _, ok := seen[source.Label]; ok {
			continue
		}
		seen[source.Label] = struct{}{}
		hit.Label = source.Label
		hits = append(hits, hit)
	}
	return hits, nil
}

func (r resolver) trustedSources() []sourceSpec {
	sources := make([]sourceSpec, 0, 5)
	if path, ok := r.lookupEnv(envPlatformFile); ok {
		sources = append(sources, sourceSpec{
			Label: displayPath(path, r.homeDir),
			Path:  strings.TrimSpace(path),
		})
	}
	if strings.TrimSpace(r.stateDir) != "" {
		path := filepath.Join(
			r.stateDir,
			runtimeDirName,
			runtimeEnvFileName,
		)
		sources = append(sources, sourceSpec{
			Label: displayPath(path, r.homeDir),
			Path:  path,
		})
	}
	for _, name := range []string{
		shellFileBashRC,
		shellFileZshRC,
		shellFileProfile,
	} {
		if strings.TrimSpace(r.homeDir) == "" {
			break
		}
		path := filepath.Join(r.homeDir, name)
		sources = append(sources, sourceSpec{
			Label: displayPath(path, r.homeDir),
			Path:  path,
		})
	}
	return sources
}

func (r resolver) findInFile(
	path string,
	name string,
) (bool, sourceHit, error) {
	data, err := r.readFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, sourceHit{}, nil
		}
		return false, sourceHit{}, fmt.Errorf(
			"env_probe: read %s: %w",
			path,
			err,
		)
	}
	found, hit := parseEnvDeclaration(data, name)
	return found, hit, nil
}

func parseEnvDeclaration(
	data []byte,
	name string,
) (bool, sourceHit) {
	lines := strings.Split(string(data), "\n")
	hit := sourceHit{}
	found := false
	for _, line := range lines {
		key, value, ok := parseEnvAssignment(line)
		if !ok || key != name {
			continue
		}
		found = true
		hit.Value, hit.NonEmpty, hit.CanActivate =
			parseDeclaredValue(value)
	}
	return found, hit
}

func parseEnvAssignment(line string) (string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}
	if strings.HasPrefix(trimmed, exportPrefix) {
		trimmed = strings.TrimSpace(
			strings.TrimPrefix(trimmed, exportPrefix),
		)
	}
	index := strings.Index(trimmed, "=")
	if index <= 0 {
		return "", "", false
	}
	key := strings.TrimSpace(trimmed[:index])
	if !isValidEnvName(key) {
		return "", "", false
	}
	return key, strings.TrimSpace(trimmed[index+1:]), true
}

func parseDeclaredValue(raw string) (string, bool, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", false, true
	}
	if isSingleQuoted(value) {
		inner := value[1 : len(value)-1]
		return inner, strings.TrimSpace(inner) != "", true
	}
	if isDoubleQuoted(value) {
		unquoted, err := strconv.Unquote(value)
		if err != nil {
			return "", false, false
		}
		return unquoted, strings.TrimSpace(unquoted) != "",
			!containsDynamicDoubleQuotedValue(value)
	}
	value = trimInlineComment(value)
	token := firstField(value)
	if token == "" {
		return "", false, false
	}
	return token, strings.TrimSpace(token) != "",
		isStaticUnquotedValue(token)
}

func isSingleQuoted(value string) bool {
	return len(value) >= 2 &&
		strings.HasPrefix(value, "'") &&
		strings.HasSuffix(value, "'")
}

func isDoubleQuoted(value string) bool {
	return len(value) >= 2 &&
		strings.HasPrefix(value, "\"") &&
		strings.HasSuffix(value, "\"")
}

func containsDynamicDoubleQuotedValue(value string) bool {
	if len(value) < 2 {
		return false
	}
	inner := value[1 : len(value)-1]
	return strings.Contains(inner, "${") ||
		strings.Contains(inner, "$(") ||
		strings.Contains(inner, "`") ||
		strings.Contains(inner, "$")
}

func trimInlineComment(value string) string {
	comment := strings.Index(value, " #")
	if comment >= 0 {
		return strings.TrimSpace(value[:comment])
	}
	return value
}

func firstField(value string) string {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func isStaticUnquotedValue(value string) bool {
	if value == "" {
		return true
	}
	return !strings.Contains(value, "${") &&
		!strings.Contains(value, "$(") &&
		!strings.Contains(value, "`") &&
		!strings.Contains(value, "$") &&
		!strings.ContainsAny(value, ";|&<>")
}

func isValidEnvName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if i == 0 {
			if !isEnvFirstRune(r) {
				return false
			}
			continue
		}
		if !isEnvRune(r) {
			return false
		}
	}
	return true
}

func isEnvFirstRune(r rune) bool {
	return r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z'
}

func isEnvRune(r rune) bool {
	return isEnvFirstRune(r) || r >= '0' && r <= '9'
}

func isSensitiveEnvName(name string) bool {
	upper := strings.ToUpper(strings.TrimSpace(name))
	for _, hint := range sensitiveNameHints {
		if strings.Contains(upper, hint) {
			return true
		}
	}
	return false
}

func sourceLabels(hits []sourceHit, nonEmptyOnly bool) []string {
	if len(hits) == 0 {
		return nil
	}
	out := make([]string, 0, len(hits))
	for _, hit := range hits {
		if nonEmptyOnly && !hit.NonEmpty {
			continue
		}
		out = append(out, hit.Label)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func needsMainRestart(result probeResult) bool {
	if result.NonEmptyNow {
		return false
	}
	return len(result.NonEmptyDeclaredSources) > 0
}

func buildSafeMessage(result probeResult) string {
	switch {
	case result.ActivatedNow:
		return fmt.Sprintf(
			sourceActivatedFormat,
			result.Name,
			result.ActivatedSource,
		)
	case result.PresentNow && result.NonEmptyNow:
		return fmt.Sprintf(sourcePresentFormat, result.Name)
	case result.PresentNow && !result.NonEmptyNow &&
		len(result.NonEmptyDeclaredSources) > 0:
		return fmt.Sprintf(
			sourcePresentEmptyFormat,
			result.Name,
		)
	case result.PresentNow:
		return fmt.Sprintf(sourcePresentEmptyFormat, result.Name)
	case len(result.NonEmptyDeclaredSources) > 0:
		return fmt.Sprintf(
			sourceDeclaredFormat,
			result.Name,
			joinLabels(result.NonEmptyDeclaredSources),
		) + sourceDynamicFormat
	case len(result.DeclaredSources) > 0:
		return fmt.Sprintf(
			sourceDeclaredEmptyFormat,
			result.Name,
			joinLabels(result.DeclaredSources),
		)
	default:
		return fmt.Sprintf(sourceNotFoundFormat, result.Name)
	}
}

func joinLabels(labels []string) string {
	if len(labels) == 0 {
		return ""
	}
	if len(labels) == 1 {
		return labels[0]
	}
	return strings.Join(labels[:len(labels)-1], ", ") + " and " +
		labels[len(labels)-1]
}

func displayPath(path string, homeDir string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	home := strings.TrimSpace(homeDir)
	if home == "" {
		return trimmed
	}
	if trimmed == home {
		return "~"
	}
	prefix := home + string(os.PathSeparator)
	if strings.HasPrefix(trimmed, prefix) {
		return "~" + string(os.PathSeparator) +
			strings.TrimPrefix(trimmed, prefix)
	}
	return trimmed
}
