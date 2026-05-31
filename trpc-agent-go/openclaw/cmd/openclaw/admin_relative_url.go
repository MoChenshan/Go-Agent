package main

import (
	"net/url"
	"path"
	"path/filepath"
	"strings"
)

const adminCurrentDirRef = "."

func adminRelativeReference(
	currentPath string,
	target string,
) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return adminCurrentDirRef
	}

	parsed, err := url.Parse(target)
	if err != nil {
		return target
	}
	if strings.TrimSpace(parsed.Path) != "" {
		parsed.Path = adminRelativePath(
			currentPath,
			parsed.Path,
		)
	}
	out := parsed.String()
	if out == "" {
		return adminCurrentDirRef
	}
	return out
}

func adminRelativePath(
	currentPath string,
	targetPath string,
) string {
	currentPath = adminCleanURLPath(currentPath)
	targetPath = adminCleanURLPath(targetPath)

	baseDir := strings.TrimPrefix(path.Dir(currentPath), "/")
	if baseDir == "" || baseDir == adminCurrentDirRef {
		baseDir = adminCurrentDirRef
	}

	targetPath = strings.TrimPrefix(targetPath, "/")
	if targetPath == "" {
		return adminCurrentDirRef
	}

	rel, err := filepath.Rel(
		filepath.FromSlash(baseDir),
		filepath.FromSlash(targetPath),
	)
	if err != nil {
		return targetPath
	}
	rel = strings.TrimSpace(filepath.ToSlash(rel))
	if rel == "" {
		return adminCurrentDirRef
	}
	return rel
}

func adminCleanURLPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/"
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	cleaned := path.Clean(value)
	if cleaned == "." {
		return "/"
	}
	return cleaned
}
