package main

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCurrentVersionUsesReleaseValue(t *testing.T) {
	oldBase := buildBaseVersion
	oldRelease := releaseVersion
	oldCommit := buildCommit
	oldReadBuildInfo := readBuildInfo
	buildBaseVersion = "v0.0.59"
	releaseVersion = " v0.0.5 "
	buildCommit = "abcdef0123456789"
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			Settings: []debug.BuildSetting{
				{
					Key:   buildInfoVCSRevisionKey,
					Value: "0123456789abcdef",
				},
			},
		}, true
	}
	t.Cleanup(func() {
		buildBaseVersion = oldBase
		buildCommit = oldCommit
		readBuildInfo = oldReadBuildInfo
		releaseVersion = oldRelease
	})

	require.Equal(t, "v0.0.5", currentVersion())
}

func TestCurrentVersionUsesBuildCommitFallback(t *testing.T) {
	oldBase := buildBaseVersion
	oldRelease := releaseVersion
	oldCommit := buildCommit
	oldReadBuildInfo := readBuildInfo
	buildBaseVersion = ""
	releaseVersion = "   "
	buildCommit = "abcdef0123456789"
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return nil, false
	}
	t.Cleanup(func() {
		buildBaseVersion = oldBase
		buildCommit = oldCommit
		readBuildInfo = oldReadBuildInfo
		releaseVersion = oldRelease
	})

	require.Equal(t, "dev-abcdef0", currentVersion())
}

func TestCurrentVersionUsesBuildInfoFallback(t *testing.T) {
	oldBase := buildBaseVersion
	oldRelease := releaseVersion
	oldCommit := buildCommit
	oldReadBuildInfo := readBuildInfo
	buildBaseVersion = ""
	releaseVersion = ""
	buildCommit = ""
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			Settings: []debug.BuildSetting{
				{
					Key:   buildInfoVCSRevisionKey,
					Value: "0123456789abcdef",
				},
			},
		}, true
	}
	t.Cleanup(func() {
		buildBaseVersion = oldBase
		buildCommit = oldCommit
		readBuildInfo = oldReadBuildInfo
		releaseVersion = oldRelease
	})

	require.Equal(t, "dev-0123456", currentVersion())
}

func TestCurrentVersionFallsBackToDev(t *testing.T) {
	oldBase := buildBaseVersion
	oldRelease := releaseVersion
	oldCommit := buildCommit
	oldReadBuildInfo := readBuildInfo
	buildBaseVersion = "   "
	releaseVersion = "   "
	buildCommit = "   "
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return nil, false
	}
	t.Cleanup(func() {
		buildBaseVersion = oldBase
		buildCommit = oldCommit
		readBuildInfo = oldReadBuildInfo
		releaseVersion = oldRelease
	})

	require.Equal(t, defaultDevVersion, currentVersion())
}

func TestCurrentVersionUsesBuildBaseVersion(t *testing.T) {
	oldBase := buildBaseVersion
	oldRelease := releaseVersion
	oldCommit := buildCommit
	oldReadBuildInfo := readBuildInfo
	buildBaseVersion = " v0.0.59 "
	releaseVersion = ""
	buildCommit = "abcdef0123456789"
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return nil, false
	}
	t.Cleanup(func() {
		buildBaseVersion = oldBase
		buildCommit = oldCommit
		readBuildInfo = oldReadBuildInfo
		releaseVersion = oldRelease
	})

	require.Equal(t, "v0.0.59-dev-abcdef0", currentVersion())
}

func TestCurrentVersionUsesBuildBaseVersionWithoutCommit(t *testing.T) {
	oldBase := buildBaseVersion
	oldRelease := releaseVersion
	oldCommit := buildCommit
	oldReadBuildInfo := readBuildInfo
	buildBaseVersion = "v0.0.59"
	releaseVersion = ""
	buildCommit = ""
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return nil, false
	}
	t.Cleanup(func() {
		buildBaseVersion = oldBase
		buildCommit = oldCommit
		readBuildInfo = oldReadBuildInfo
		releaseVersion = oldRelease
	})

	require.Equal(t, "v0.0.59-dev", currentVersion())
}
