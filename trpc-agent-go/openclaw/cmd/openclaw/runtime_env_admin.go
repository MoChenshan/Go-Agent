package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/openclaw/admin"
)

const (
	runtimeConfigApplyHot = "hot"

	runtimeEnvSectionKey    = "runtime_environment"
	runtimeEnvInputReadOnly = "readonly"

	runtimeEnvFieldKeyPrefix = "runtime.env."

	runtimeEnvFieldExtraPathDirs = "extra_path_dirs"
	runtimeEnvFieldEffectivePath = "effective_path"
	runtimeEnvFieldSearchedDirs  = "searched_dirs"

	runtimeEnvSectionTitle   = "Runtime Environment"
	runtimeEnvSectionSummary = "Review the active PATH search order and " +
		"persist additional executable directories for the next runtime " +
		"start."
	runtimeEnvExtraPathTitle   = "Extra PATH Directories"
	runtimeEnvExtraPathSummary = "Additional directories appended through " +
		"TRPC_CLAW_EXTRA_PATH_DIRS. Separate entries with " +
		string(os.PathListSeparator) + ". Admin saves normalized " +
		"absolute paths."
	runtimeEnvExtraPathPlaceholder = "/usr/local/app/bin" +
		string(os.PathListSeparator) +
		"/opt/tools/bin"
	runtimeEnvExtraPathConfiguredExplicit = "Saved in the runtime env " +
		"file as normalized absolute paths and applied on the next " +
		"restart."
	runtimeEnvExtraPathConfiguredInherited = "Unset keeps the inherited " +
		"TRPC_CLAW_EXTRA_PATH_DIRS value."
	runtimeEnvExtraPathRuntimeSource = "Normalized current value for " +
		"TRPC_CLAW_EXTRA_PATH_DIRS."

	runtimeEnvEffectivePathTitle   = "Effective PATH"
	runtimeEnvEffectivePathSummary = "Read-only snapshot of the current " +
		"runtime PATH after startup defaults are applied. Bin checks and " +
		"exec.LookPath search these directories in order."
	runtimeEnvEffectivePathSource = "Current process PATH."

	runtimeEnvSearchedDirsTitle   = "Searched Directories"
	runtimeEnvSearchedDirsSummary = "Read-only ordered PATH entries used " +
		"for executable lookup."
	runtimeEnvSearchedDirsSource = "Derived by splitting the current " +
		"runtime PATH."

	runtimeEnvReadOnlySaveErr = "runtime env field is read-only"
)

func runtimeEnvFieldKey(field string) string {
	field = strings.TrimSpace(field)
	if field == "" {
		return runtimeEnvFieldKeyPrefix
	}
	return runtimeEnvFieldKeyPrefix + field
}

func parseRuntimeEnvFieldKey(key string) (string, bool) {
	key = strings.TrimSpace(key)
	if !strings.HasPrefix(key, runtimeEnvFieldKeyPrefix) {
		return "", false
	}
	field := strings.TrimSpace(strings.TrimPrefix(
		key,
		runtimeEnvFieldKeyPrefix,
	))
	if field == "" {
		return "", false
	}
	return field, true
}

func (p *runtimeAdminProvider) buildRuntimeEnvConfigSection() (
	admin.RuntimeConfigSection,
	error,
) {
	extraField, err := p.buildRuntimeExtraPathDirsField()
	if err != nil {
		return admin.RuntimeConfigSection{}, err
	}
	return admin.RuntimeConfigSection{
		Key:     runtimeEnvSectionKey,
		Title:   runtimeEnvSectionTitle,
		Summary: runtimeEnvSectionSummary,
		Fields: []admin.RuntimeConfigField{
			extraField,
			p.buildRuntimeEffectivePathField(),
			p.buildRuntimeSearchedDirsField(),
		},
	}, nil
}

func (p *runtimeAdminProvider) buildRuntimeExtraPathDirsField() (
	admin.RuntimeConfigField,
	error,
) {
	configured, explicit, err := p.runtimeEnvAssignment(
		runtimeExtraPathEnvName,
	)
	if err != nil {
		return admin.RuntimeConfigField{}, err
	}
	runtimeValue := normalizeRuntimePathList(
		os.Getenv(runtimeExtraPathEnvName),
	)
	return admin.RuntimeConfigField{
		Key:              runtimeEnvFieldKey(runtimeEnvFieldExtraPathDirs),
		Title:            runtimeEnvExtraPathTitle,
		Summary:          runtimeEnvExtraPathSummary,
		InputType:        channelConfigInputText,
		Placeholder:      runtimeEnvExtraPathPlaceholder,
		ApplyMode:        channelConfigApplyRestart,
		EditorValue:      configured,
		ConfiguredValue:  configured,
		ConfiguredSource: runtimeEnvConfiguredSource(explicit),
		ConfiguredSourceLabel: runtimeEnvConfiguredSourceLabel(
			explicit,
		),
		RuntimeValue:       runtimeValue,
		RuntimeSourceLabel: runtimeEnvExtraPathRuntimeSource,
		PendingRestart:     explicit && runtimeValue != configured,
		Resettable:         explicit,
	}, nil
}

func (p *runtimeAdminProvider) buildRuntimeEffectivePathField() admin.RuntimeConfigField {
	value := strings.TrimSpace(os.Getenv(runtimePathEnvName))
	return admin.RuntimeConfigField{
		Key:                runtimeEnvFieldKey(runtimeEnvFieldEffectivePath),
		Title:              runtimeEnvEffectivePathTitle,
		Summary:            runtimeEnvEffectivePathSummary,
		InputType:          runtimeEnvInputReadOnly,
		ApplyMode:          runtimeConfigApplyHot,
		EditorValue:        value,
		RuntimeValue:       value,
		RuntimeSourceLabel: runtimeEnvEffectivePathSource,
	}
}

func (p *runtimeAdminProvider) buildRuntimeSearchedDirsField() admin.RuntimeConfigField {
	return admin.RuntimeConfigField{
		Key:       runtimeEnvFieldKey(runtimeEnvFieldSearchedDirs),
		Title:     runtimeEnvSearchedDirsTitle,
		Summary:   runtimeEnvSearchedDirsSummary,
		InputType: runtimeEnvInputReadOnly,
		ApplyMode: runtimeConfigApplyHot,
		EditorValue: formatRuntimeSearchDirs(
			os.Getenv(runtimePathEnvName),
		),
		RuntimeValue: formatRuntimeSearchDirs(
			os.Getenv(runtimePathEnvName),
		),
		RuntimeSourceLabel: runtimeEnvSearchedDirsSource,
	}
}

func runtimeEnvConfiguredSource(explicit bool) string {
	if explicit {
		return channelConfigSourceExplicit
	}
	return channelConfigSourceInherited
}

func runtimeEnvConfiguredSourceLabel(explicit bool) string {
	if explicit {
		return runtimeEnvExtraPathConfiguredExplicit
	}
	return runtimeEnvExtraPathConfiguredInherited
}

func (p *runtimeAdminProvider) runtimeEnvAssignment(
	name string,
) (string, bool, error) {
	assignments, err := readRuntimeEnvAssignmentsForStateDir(
		p.stateDir,
	)
	if err != nil && !os.IsNotExist(err) {
		return "", false, err
	}
	if len(assignments) == 0 {
		return "", false, nil
	}
	value, ok := assignments[strings.TrimSpace(name)]
	if !ok {
		return "", false, nil
	}
	return normalizeRuntimePathList(value), true, nil
}

func normalizeRuntimePathList(raw string) string {
	parts := splitRuntimePathInput(raw)
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, string(os.PathListSeparator))
}

func splitRuntimePathInput(raw string) []string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(
		raw,
		"\n",
		string(os.PathListSeparator),
	)
	list := filepath.SplitList(raw)
	out := make([]string, 0, len(list))
	for _, entry := range list {
		entry = normalizeRuntimePathDir(entry)
		if entry == "" {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func formatRuntimeSearchDirs(pathValue string) string {
	parts := filepath.SplitList(pathValue)
	out := make([]string, 0, len(parts))
	for _, entry := range parts {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		out = append(out, entry)
	}
	return strings.Join(out, ", ")
}

func (p *runtimeAdminProvider) updateRuntimeEnvConfigValue(
	fieldKey string,
	value string,
	reset bool,
) (bool, error) {
	fieldKey, ok := parseRuntimeEnvFieldKey(fieldKey)
	if !ok {
		return false, nil
	}
	if p == nil {
		return true, fmt.Errorf(
			"runtime config provider is not available",
		)
	}

	switch fieldKey {
	case runtimeEnvFieldExtraPathDirs:
		if err := p.saveRuntimeExtraPathDirs(value, reset); err != nil {
			return true, err
		}
		return true, nil
	case runtimeEnvFieldEffectivePath, runtimeEnvFieldSearchedDirs:
		return true, fmt.Errorf(runtimeEnvReadOnlySaveErr)
	default:
		return true, fmt.Errorf(
			"unknown runtime config field %q",
			runtimeEnvFieldKey(fieldKey),
		)
	}
}

func (p *runtimeAdminProvider) saveRuntimeExtraPathDirs(
	value string,
	reset bool,
) error {
	normalized := normalizeRuntimePathList(value)
	if !reset && normalized == "" {
		reset = true
	}
	return updateRuntimeEnvAssignmentForStateDir(
		p.stateDir,
		runtimeExtraPathEnvName,
		normalized,
		reset,
	)
}

func updateRuntimeEnvAssignmentForStateDir(
	stateDir string,
	name string,
	value string,
	reset bool,
) error {
	stateDir = strings.TrimSpace(stateDir)
	name = strings.TrimSpace(name)
	value = strings.TrimSpace(value)
	if stateDir == "" {
		return fmt.Errorf("runtime state dir is empty")
	}
	if name == "" {
		return fmt.Errorf("runtime env name is empty")
	}

	path := filepath.Join(stateDir, runtimeEnvFileName)
	lines, err := readRuntimeEnvLines(path)
	if err != nil {
		return err
	}
	updated := updateRuntimeEnvLines(lines, name, value, reset)
	if !reset && !runtimeEnvNamePresent(updated, name) {
		updated = append(updated, name+"="+value)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir runtime env dir: %w", err)
	}
	data := strings.TrimRight(strings.Join(updated, "\n"), "\n")
	if data != "" {
		data += "\n"
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		return fmt.Errorf("write runtime env: %w", err)
	}
	return nil
}

func readRuntimeEnvLines(path string) ([]string, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read runtime env: %w", err)
	}
	return strings.Split(string(data), "\n"), nil
}

func updateRuntimeEnvLines(
	lines []string,
	name string,
	value string,
	reset bool,
) []string {
	out := make([]string, 0, len(lines))
	replaced := false
	for _, line := range lines {
		if !runtimeEnvLineHasName(line, name) {
			if line == "" && len(out) == 0 {
				continue
			}
			out = append(out, line)
			continue
		}
		if reset || replaced {
			continue
		}
		out = append(out, name+"="+value)
		replaced = true
	}
	return out
}

func runtimeEnvNamePresent(lines []string, name string) bool {
	for _, line := range lines {
		if runtimeEnvLineHasName(line, name) {
			return true
		}
	}
	return false
}

func runtimeEnvLineHasName(line string, name string) bool {
	key, _, ok := parseRuntimeEnvLine(line)
	return ok && key == name
}

func parseRuntimeEnvLine(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	if strings.HasPrefix(line, "export ") {
		line = strings.TrimSpace(
			strings.TrimPrefix(line, "export "),
		)
	}
	key, value, ok := strings.Cut(line, "=")
	if !ok {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", false
	}
	return key, strings.TrimSpace(value), true
}
