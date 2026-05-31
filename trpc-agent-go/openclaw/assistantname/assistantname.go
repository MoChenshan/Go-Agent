package assistantname

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	FileName = "IDENTITY.md"

	MaxRunes = 32

	defaultFilePerm = 0o600
	defaultDirPerm  = 0o700

	resetTokenOff     = "off"
	resetTokenClear   = "clear"
	resetTokenDefault = "default"
	resetTokenReset   = "reset"
)

func Normalize(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	trimmed = strings.Trim(
		trimmed,
		"\"'“”‘’<>《》「」『』【】()（）[]",
	)
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" {
		return ""
	}

	fields := strings.Fields(trimmed)
	if len(fields) > 0 {
		trimmed = strings.Join(fields, " ")
	}

	runes := []rune(trimmed)
	if len(runes) > MaxRunes {
		runes = runes[:MaxRunes]
	}
	return strings.TrimSpace(string(runes))
}

func IsResetToken(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", resetTokenOff, resetTokenClear:
		return true
	case resetTokenDefault, resetTokenReset:
		return true
	default:
		return false
	}
}

func ReadFile(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return Normalize(string(data)), nil
}

func WriteFile(path string, name string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), defaultDirPerm); err != nil {
		return err
	}

	name = Normalize(name)
	body := ""
	if name != "" {
		body = name + "\n"
	}
	return os.WriteFile(path, []byte(body), defaultFilePerm)
}
