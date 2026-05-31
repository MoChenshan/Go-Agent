package internal

import (
	"strings"
)

// RepairBasePath fixes basePath. If basePath[0] is '/', remove it;
// if basePath[len(basePath)-1] is not '/', add '/'
func RepairBasePath(basePath string) string {
	if len(basePath) == 0 {
		return ""
	}

	// Use strings.Builder for string concatenation to avoid multiple memory allocations
	var builder strings.Builder
	start := 0
	end := len(basePath)

	// Remove leading '/'
	if basePath[0] == '/' {
		start = 1
	}

	// Ensure the ending has '/'
	if len(basePath) > 0 && basePath[len(basePath)-1] != '/' {
		builder.Grow(end - start + 1) // Pre-allocate memory
		builder.WriteString(basePath[start:end])
		builder.WriteByte('/')
		return builder.String()
	}

	if start > 0 {
		return basePath[start:end]
	}
	return basePath
}
