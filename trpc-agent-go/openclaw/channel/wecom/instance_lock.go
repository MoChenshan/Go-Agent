package wecom

import (
	"path/filepath"
	"strings"
	"unicode"
)

const (
	webSocketLockFilePrefix = "websocket_"
	webSocketLockFileSuffix = ".lock"
	webSocketLockFallbackID = "bot"
)

type processLock interface {
	Close() error
}

func websocketInstanceLockPath(
	sessionTrackerPath string,
	botID string,
) string {
	sessionTrackerPath = strings.TrimSpace(sessionTrackerPath)
	if sessionTrackerPath == "" {
		return ""
	}
	return filepath.Join(
		filepath.Dir(sessionTrackerPath),
		webSocketLockFilePrefix+
			sanitizeLockComponent(botID)+
			webSocketLockFileSuffix,
	)
}

func sanitizeLockComponent(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return webSocketLockFallbackID
	}

	var builder strings.Builder
	builder.Grow(len(raw))

	for _, r := range raw {
		if unicode.IsLetter(r) ||
			unicode.IsDigit(r) ||
			r == '-' ||
			r == '_' {
			builder.WriteRune(r)
			continue
		}
		builder.WriteRune('_')
	}

	sanitized := strings.Trim(builder.String(), "_")
	if sanitized == "" {
		return webSocketLockFallbackID
	}
	return sanitized
}
