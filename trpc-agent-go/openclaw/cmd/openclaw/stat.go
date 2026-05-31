package main

import (
	"fmt"
	"runtime/debug"
	"strings"

	runtime "git.code.oa.com/trpc-go/trpc-metrics-runtime"
	"trpc.group/trpc-go/trpc-agent-go/telemetry/appid"
)

const (
	buildInfoVCSRevisionKey = "vcs.revision"
	defaultDevSuffix        = "-dev"
	defaultDevVersion       = "dev"
	devVersionPrefix        = "dev-"
	productName             = "trpc-agent-go-openclaw"
	shortCommitLength       = 7
)

var (
	buildBaseVersion string
	buildCommit      string
	readBuildInfo    = debug.ReadBuildInfo
	releaseVersion   string
)

func currentVersion() string {
	version := strings.TrimSpace(releaseVersion)
	if version != "" {
		return version
	}
	baseVersion := strings.TrimSpace(buildBaseVersion)
	commit := currentCommit()
	if baseVersion != "" {
		if commit == "" {
			return baseVersion + defaultDevSuffix
		}
		return fmt.Sprintf(
			"%s%s-%s",
			baseVersion,
			defaultDevSuffix,
			commit,
		)
	}
	if commit == "" {
		return defaultDevVersion
	}
	return devVersionPrefix + commit
}

func currentCommit() string {
	commit := shortenCommit(buildCommit)
	if commit != "" {
		return commit
	}
	info, ok := readBuildInfo()
	if !ok || info == nil {
		return ""
	}
	return shortenCommit(buildSettingValue(
		info.Settings,
		buildInfoVCSRevisionKey,
	))
}

func buildSettingValue(
	settings []debug.BuildSetting,
	key string,
) string {
	for _, setting := range settings {
		if strings.TrimSpace(setting.Key) == key {
			return setting.Value
		}
	}
	return ""
}

func shortenCommit(raw string) string {
	commit := strings.TrimSpace(raw)
	if commit == "" {
		return ""
	}
	if len(commit) <= shortCommitLength {
		return commit
	}
	return commit[:shortCommitLength]
}

func init() {
	// Inject resolvers so runtime.Stat can fallback to Runner names.
	runtime.ResolveApp = appid.DefaultApp
	runtime.ResolveServer = appid.DefaultAgent
	go runtime.StatReport(currentVersion(), productName)
}
