package wecom

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWriteRuntimeDebugBundleWithOptions(
	t *testing.T,
) {
	t.Parallel()

	root := t.TempDir()
	debugDir := filepath.Join(root, runtimeBundleDebugDirName)
	require.NoError(t, os.MkdirAll(debugDir, 0o755))

	configPath := filepath.Join(root, runtimeBundleConfigFileName)
	recentPath := filepath.Join(debugDir, "recent.log")
	oldPath := filepath.Join(debugDir, "old.log")

	writeRuntimeBundleTestFile(
		t,
		configPath,
		runtimeBundleTestBytes(64),
	)
	writeRuntimeBundleTestFile(
		t,
		recentPath,
		runtimeBundleTestBytes(256),
	)
	writeRuntimeBundleTestFile(
		t,
		oldPath,
		runtimeBundleTestBytes(256),
	)

	now := time.Now()
	require.NoError(t, os.Chtimes(oldPath, now, now))
	require.NoError(
		t,
		os.Chtimes(recentPath, now.Add(time.Minute), now.Add(time.Minute)),
	)

	archivePath := filepath.Join(root, "bundle.zip")
	result, err := writeRuntimeDebugBundleWithOptions(
		archivePath,
		[]runtimeBundleEntry{
			{
				ArchivePath: runtimeBundleConfigFileName,
				SourcePath:  configPath,
				Label:       runtimeBundleConfigFileName,
				Required:    true,
			},
			{
				ArchivePath: runtimeBundleDebugDirName,
				SourcePath:  debugDir,
				Label:       runtimeBundleSourceDebug,
				RecentFirst: true,
			},
		},
		runtimeBundleWriteOptions{
			TargetBytes:        320,
			MaxBytes:           1024,
			OmittedListMaxSize: 10,
		},
	)
	require.NoError(t, err)
	require.Equal(
		t,
		[]string{
			runtimeBundleSourceDebug,
			runtimeBundleConfigFileName,
		},
		result.Included,
	)
	require.Equal(t, []string{runtimeBundleSourceDebug}, result.Omitted)
	require.Contains(t, result.Archived, runtimeBundleConfigFileName)
	require.Contains(
		t,
		result.Archived,
		filepath.ToSlash(
			filepath.Join(runtimeBundleDebugDirName, "recent.log"),
		),
	)
	require.Contains(
		t,
		result.OmittedFiles,
		filepath.ToSlash(
			filepath.Join(runtimeBundleDebugDirName, "old.log"),
		),
	)

	names := runtimeBundleArchiveNames(t, archivePath)
	require.Contains(t, names, runtimeBundleConfigFileName)
	require.Contains(
		t,
		names,
		filepath.ToSlash(
			filepath.Join(runtimeBundleDebugDirName, "recent.log"),
		),
	)
	require.NotContains(
		t,
		names,
		filepath.ToSlash(
			filepath.Join(runtimeBundleDebugDirName, "old.log"),
		),
	)
	require.Contains(t, names, runtimeBundleManifestName)

	manifest := runtimeBundleArchiveEntry(
		t,
		archivePath,
		runtimeBundleManifestName,
	)
	require.Contains(t, manifest, "Omitted by size budget:")
	require.Contains(t, manifest, runtimeBundleSourceDebug)
	require.Contains(
		t,
		manifest,
		filepath.ToSlash(
			filepath.Join(runtimeBundleDebugDirName, "old.log"),
		),
	)
}

func TestWriteRuntimeDebugBundleMultipartWithOptions(
	t *testing.T,
) {
	t.Parallel()

	root := t.TempDir()
	debugDir := filepath.Join(root, runtimeBundleDebugDirName)
	require.NoError(t, os.MkdirAll(debugDir, 0o755))

	configPath := filepath.Join(root, runtimeBundleConfigFileName)
	recentPath := filepath.Join(debugDir, "recent.log")
	middlePath := filepath.Join(debugDir, "middle.log")
	oldPath := filepath.Join(debugDir, "old.log")

	writeRuntimeBundleTestFile(
		t,
		configPath,
		runtimeBundleTestBytes(64),
	)
	writeRuntimeBundleTestFile(
		t,
		recentPath,
		runtimeBundleTestBytes(200),
	)
	writeRuntimeBundleTestFile(
		t,
		middlePath,
		runtimeBundleTestBytes(200),
	)
	writeRuntimeBundleTestFile(
		t,
		oldPath,
		runtimeBundleTestBytes(200),
	)

	now := time.Now()
	require.NoError(t, os.Chtimes(oldPath, now, now))
	require.NoError(
		t,
		os.Chtimes(
			middlePath,
			now.Add(time.Minute),
			now.Add(time.Minute),
		),
	)
	require.NoError(
		t,
		os.Chtimes(
			recentPath,
			now.Add(2*time.Minute),
			now.Add(2*time.Minute),
		),
	)

	result, err := writeRuntimeDebugBundleMultipartWithOptions(
		filepath.Join(root, "bundle"),
		[]runtimeBundleEntry{
			{
				ArchivePath: runtimeBundleConfigFileName,
				SourcePath:  configPath,
				Label:       runtimeBundleConfigFileName,
				Required:    true,
			},
			{
				ArchivePath: runtimeBundleDebugDirName,
				SourcePath:  debugDir,
				Label:       runtimeBundleSourceDebug,
				RecentFirst: true,
			},
		},
		runtimeBundleMultipartOptions{
			TotalLimitBytes:    500,
			PartTargetBytes:    240,
			MaxBytes:           1024,
			OmittedListMaxSize: 10,
		},
	)
	require.NoError(t, err)
	require.Len(t, result.ArchivePaths, 3)
	require.Equal(
		t,
		[]string{
			runtimeBundleSourceDebug,
			runtimeBundleConfigFileName,
		},
		result.BuildResult.Included,
	)
	require.Equal(
		t,
		[]string{runtimeBundleSourceDebug},
		result.BuildResult.Omitted,
	)
	require.Contains(
		t,
		result.BuildResult.OmittedFiles,
		filepath.ToSlash(
			filepath.Join(runtimeBundleDebugDirName, "old.log"),
		),
	)

	require.Contains(
		t,
		result.ArchivePaths[0],
		runtimeBundleArchivePartTag+"01"+runtimeBundleArchiveSuffix,
	)
	manifest := runtimeBundleArchiveEntry(
		t,
		result.ArchivePaths[0],
		runtimeBundleManifestName,
	)
	require.Contains(t, manifest, "Mode: "+runtimeBundleModeFull)
	require.Contains(t, manifest, "Part: 1")
	require.Contains(t, manifest, "Approx total limit:")
}

func TestRuntimeDebugBundleIncludesCronJobs(
	t *testing.T,
) {
	t.Parallel()

	root := t.TempDir()
	cronJobsPath := filepath.Join(
		root,
		runtimeBundleCronDirName,
		runtimeBundleCronJobsName,
	)
	writeRuntimeBundleTestFile(t, cronJobsPath, []byte(`{"jobs":[]}`))

	archivePath := filepath.Join(t.TempDir(), "bundle.zip")
	channel := &Channel{stateDir: root}
	result, err := writeRuntimeDebugBundleWithOptions(
		archivePath,
		channel.runtimeDebugBundleEntries(),
		runtimeBundleWriteOptions{
			TargetBytes:        1,
			MaxBytes:           1024 * 1024,
			OmittedListMaxSize: 10,
		},
	)
	require.NoError(t, err)
	require.Contains(t, result.Included, runtimeBundleSourceCronJobs)

	cronJobsArchivePath := filepath.ToSlash(
		filepath.Join(
			runtimeBundleCronDirName,
			runtimeBundleCronJobsName,
		),
	)
	names := runtimeBundleArchiveNames(t, archivePath)
	require.Contains(t, names, cronJobsArchivePath)

	manifest := runtimeBundleArchiveEntry(
		t,
		archivePath,
		runtimeBundleManifestName,
	)
	require.Contains(t, manifest, runtimeBundleSourceCronJobs)
	require.Contains(t, manifest, cronJobsArchivePath)
}

func TestRuntimeDebugBundleAutoDiscoversRuntimeFiles(
	t *testing.T,
) {
	t.Parallel()

	root := t.TempDir()
	binDir := filepath.Join(root, "services", "trpc", "bin")
	logDir := filepath.Join(root, "services", "trpc", "log")
	stateDir := filepath.Join(root, "state")

	runtimeConfigPath := filepath.Join(binDir, runtimeBundleConfigFileName)
	trpcConfigPath := filepath.Join(binDir, runtimeBundleTRPCConfigName)
	trpcLogPath := filepath.Join(logDir, "trpc.log")
	slimLogPath := filepath.Join(logDir, "trpc.slim.log")
	openClawLogPath := filepath.Join(logDir, "openclaw.log")
	unrelatedLogPath := filepath.Join(logDir, "other.log")

	writeRuntimeBundleTestFile(
		t,
		runtimeConfigPath,
		[]byte("app_name: test\n"),
	)
	writeRuntimeBundleTestFile(
		t,
		trpcConfigPath,
		[]byte("server: {}\n"),
	)
	writeRuntimeBundleTestFile(t, trpcLogPath, []byte("trpc\n"))
	writeRuntimeBundleTestFile(t, slimLogPath, []byte("slim\n"))
	writeRuntimeBundleTestFile(t, openClawLogPath, []byte("stderr\n"))
	writeRuntimeBundleTestFile(t, unrelatedLogPath, []byte("other\n"))

	archivePath := filepath.Join(root, "bundle.zip")
	result, err := writeRuntimeDebugBundleWithOptions(
		archivePath,
		runtimeAutoDiscoveredBundleEntries(stateDir, binDir),
		runtimeBundleWriteOptions{
			TargetBytes:        1024 * 1024,
			MaxBytes:           1024 * 1024,
			OmittedListMaxSize: 10,
		},
	)
	require.NoError(t, err)
	require.Contains(t, result.Included, runtimeBundleSourceRuntimeCfg)
	require.Contains(t, result.Included, runtimeBundleSourceTRPCCfg)
	require.Contains(t, result.Included, runtimeBundleSourceLogs)

	names := runtimeBundleArchiveNames(t, archivePath)
	require.Contains(
		t,
		names,
		filepath.ToSlash(
			filepath.Join(
				runtimeBundleRuntimeDirName,
				runtimeBundleConfigFileName,
			),
		),
	)
	require.Contains(
		t,
		names,
		filepath.ToSlash(
			filepath.Join(
				runtimeBundleRuntimeDirName,
				runtimeBundleTRPCConfigName,
			),
		),
	)
	require.Contains(
		t,
		names,
		filepath.ToSlash(filepath.Join(runtimeBundleLogsDirName, "trpc.log")),
	)
	require.Contains(
		t,
		names,
		filepath.ToSlash(
			filepath.Join(runtimeBundleLogsDirName, "trpc.slim.log"),
		),
	)
	require.Contains(
		t,
		names,
		filepath.ToSlash(
			filepath.Join(runtimeBundleLogsDirName, "openclaw.log"),
		),
	)
	require.NotContains(
		t,
		names,
		filepath.ToSlash(
			filepath.Join(runtimeBundleLogsDirName, "other.log"),
		),
	)
}

func TestRuntimeDebugBundleDiscoversLogsInLiteralPatternDirs(
	t *testing.T,
) {
	t.Parallel()

	const (
		runtimeDirName = "runtime[prod]"
		logFileName    = "trpc.log"
		logBody        = "right\n"
		wrongLogBody   = "wrong\n"
	)

	root := t.TempDir()
	cwd := filepath.Join(root, runtimeDirName, "bin")
	logPath := filepath.Join(
		root,
		runtimeDirName,
		runtimeBundleTRPCLogDirName,
		logFileName,
	)
	wrongLogPath := filepath.Join(
		root,
		"runtimep",
		runtimeBundleTRPCLogDirName,
		logFileName,
	)
	stateDir := filepath.Join(root, "state")

	writeRuntimeBundleTestFile(t, logPath, []byte(logBody))
	writeRuntimeBundleTestFile(t, wrongLogPath, []byte(wrongLogBody))

	archivePath := filepath.Join(root, "bundle.zip")
	result, err := writeRuntimeDebugBundleWithOptions(
		archivePath,
		runtimeAutoDiscoveredBundleEntries(stateDir, cwd),
		runtimeBundleWriteOptions{
			TargetBytes:        1024 * 1024,
			MaxBytes:           1024 * 1024,
			OmittedListMaxSize: 10,
		},
	)
	require.NoError(t, err)
	require.Contains(t, result.Included, runtimeBundleSourceLogs)

	logArchivePath := filepath.ToSlash(
		filepath.Join(runtimeBundleLogsDirName, logFileName),
	)
	require.Equal(
		t,
		logBody,
		runtimeBundleArchiveEntry(t, archivePath, logArchivePath),
	)
}

func TestRuntimeDebugBundleAutoDiscoversEnvironmentHints(
	t *testing.T,
) {
	root := t.TempDir()
	cwd := filepath.Join(root, "unused-cwd")
	configDir := filepath.Join(root, "runtime", "conf")
	binDir := filepath.Join(root, "runtime", "bin")
	logDir := filepath.Join(root, "runtime", "logs")
	stateDir := filepath.Join(root, "state")

	runtimeConfigPath := filepath.Join(configDir, runtimeBundleConfigFileName)
	trpcConfigPath := filepath.Join(binDir, runtimeBundleTRPCConfigName)
	startLogPath := filepath.Join(logDir, "start.log.1")
	directLogPath := filepath.Join(root, "capture", "worker-output.txt")

	writeRuntimeBundleTestFile(
		t,
		runtimeConfigPath,
		[]byte("app_name: env-test\n"),
	)
	writeRuntimeBundleTestFile(
		t,
		trpcConfigPath,
		[]byte("server: {}\n"),
	)
	writeRuntimeBundleTestFile(t, startLogPath, []byte("start\n"))
	writeRuntimeBundleTestFile(t, directLogPath, []byte("stderr\n"))

	t.Setenv(runtimeBundleConfigPathEnv, runtimeConfigPath)
	t.Setenv(runtimeBundleBinDirEnv, binDir)
	t.Setenv(runtimeBundleLogDirEnv, logDir)
	t.Setenv(runtimeBundleOpenClawLogPathEnv, directLogPath)

	archivePath := filepath.Join(root, "bundle.zip")
	result, err := writeRuntimeDebugBundleWithOptions(
		archivePath,
		runtimeAutoDiscoveredBundleEntries(stateDir, cwd),
		runtimeBundleWriteOptions{
			TargetBytes:        1024 * 1024,
			MaxBytes:           1024 * 1024,
			OmittedListMaxSize: 10,
		},
	)
	require.NoError(t, err)
	require.Contains(t, result.Included, runtimeBundleSourceRuntimeCfg)
	require.Contains(t, result.Included, runtimeBundleSourceTRPCCfg)
	require.Contains(t, result.Included, runtimeBundleSourceLogs)

	names := runtimeBundleArchiveNames(t, archivePath)
	require.Contains(
		t,
		names,
		filepath.ToSlash(
			filepath.Join(
				runtimeBundleRuntimeDirName,
				runtimeBundleConfigFileName,
			),
		),
	)
	require.Contains(
		t,
		names,
		filepath.ToSlash(
			filepath.Join(
				runtimeBundleRuntimeDirName,
				runtimeBundleTRPCConfigName,
			),
		),
	)
	require.Contains(
		t,
		names,
		filepath.ToSlash(
			filepath.Join(runtimeBundleLogsDirName, "start.log.1"),
		),
	)
	require.Contains(
		t,
		names,
		filepath.ToSlash(
			filepath.Join(runtimeBundleLogsDirName, "worker-output.txt"),
		),
	)
}

func TestRuntimeDebugBundleIgnoresNonRegularLogHints(
	t *testing.T,
) {
	const (
		directLogFileName     = "worker-output.txt"
		discoveredLogFileName = "trpc.log"
	)

	root := t.TempDir()
	cwd := filepath.Join(root, "runtime", "bin")
	logDir := filepath.Join(root, "runtime", runtimeBundleTRPCLogDirName)
	directLogPath := filepath.Join(root, "capture", directLogFileName)
	discoveredLogPath := filepath.Join(logDir, discoveredLogFileName)
	stateDir := filepath.Join(root, "state")

	require.NoError(t, os.MkdirAll(directLogPath, 0o755))
	require.NoError(t, os.MkdirAll(discoveredLogPath, 0o755))
	t.Setenv(runtimeBundleLogPathEnv, directLogPath)

	archivePath := filepath.Join(root, "bundle.zip")
	result, err := writeRuntimeDebugBundleWithOptions(
		archivePath,
		runtimeAutoDiscoveredBundleEntries(stateDir, cwd),
		runtimeBundleWriteOptions{
			TargetBytes:        1024 * 1024,
			MaxBytes:           1024 * 1024,
			OmittedListMaxSize: 10,
		},
	)
	require.NoError(t, err)
	require.NotContains(t, result.Included, runtimeBundleSourceLogs)

	names := runtimeBundleArchiveNames(t, archivePath)
	require.NotContains(
		t,
		names,
		filepath.ToSlash(
			filepath.Join(runtimeBundleLogsDirName, directLogFileName),
		),
	)
	require.NotContains(
		t,
		names,
		filepath.ToSlash(
			filepath.Join(runtimeBundleLogsDirName, discoveredLogFileName),
		),
	)
}

func TestRuntimeDebugBundleKeepsDuplicateLogBasenames(
	t *testing.T,
) {
	const (
		logFileName   = "openclaw.log"
		directLogBody = "direct\n"
		firstLogBody  = "first\n"
		secondLogBody = "second\n"
	)

	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	firstBinDir := filepath.Join(root, "first", "bin")
	secondBinDir := filepath.Join(root, "second", "bin")
	firstLogDir := filepath.Join(root, "first", runtimeBundleTRPCLogDirName)
	secondLogDir := filepath.Join(root, "second", runtimeBundleTRPCLogDirName)
	directLogPath := filepath.Join(root, "capture", logFileName)
	firstLogPath := filepath.Join(firstLogDir, logFileName)
	secondLogPath := filepath.Join(secondLogDir, logFileName)

	writeRuntimeBundleTestFile(t, directLogPath, []byte(directLogBody))
	writeRuntimeBundleTestFile(t, firstLogPath, []byte(firstLogBody))
	writeRuntimeBundleTestFile(t, secondLogPath, []byte(secondLogBody))

	now := time.Now()
	require.NoError(t, os.Chtimes(firstLogPath, now, now))
	require.NoError(
		t,
		os.Chtimes(
			secondLogPath,
			now.Add(time.Minute),
			now.Add(time.Minute),
		),
	)

	t.Setenv(runtimeBundleLogPathEnv, directLogPath)
	t.Setenv(runtimeBundleBinDirEnv, secondBinDir)

	archivePath := filepath.Join(root, "bundle.zip")
	result, err := writeRuntimeDebugBundleWithOptions(
		archivePath,
		runtimeAutoDiscoveredBundleEntries(stateDir, firstBinDir),
		runtimeBundleWriteOptions{
			TargetBytes:        1024 * 1024,
			MaxBytes:           1024 * 1024,
			OmittedListMaxSize: 10,
		},
	)
	require.NoError(t, err)
	require.Contains(t, result.Included, runtimeBundleSourceLogs)

	firstArchivePath := filepath.ToSlash(
		filepath.Join(runtimeBundleLogsDirName, logFileName),
	)
	secondArchivePath := filepath.ToSlash(
		filepath.Join(runtimeBundleLogsDirName, "openclaw-2.log"),
	)
	thirdArchivePath := filepath.ToSlash(
		filepath.Join(runtimeBundleLogsDirName, "openclaw-3.log"),
	)

	names := runtimeBundleArchiveNames(t, archivePath)
	require.Contains(t, names, firstArchivePath)
	require.Contains(t, names, secondArchivePath)
	require.Contains(t, names, thirdArchivePath)
	require.Equal(
		t,
		directLogBody,
		runtimeBundleArchiveEntry(t, archivePath, firstArchivePath),
	)
	require.Equal(
		t,
		secondLogBody,
		runtimeBundleArchiveEntry(t, archivePath, secondArchivePath),
	)
	require.Equal(
		t,
		firstLogBody,
		runtimeBundleArchiveEntry(t, archivePath, thirdArchivePath),
	)
}

func TestFormatRuntimeDebugBundleResult(
	t *testing.T,
) {
	t.Parallel()

	text := formatRuntimeDebugBundleResult(
		[]string{runtimeBundleConfigFileName},
		[]string{runtimeBundleSourceSessions},
		[]string{runtimeBundleSourceDebug},
	)
	require.Contains(t, text, "✅ 已打包并回传调试资料。")
	require.Contains(t, text, "已包含："+runtimeBundleConfigFileName)
	require.Contains(
		t,
		text,
		"默认路径不存在："+runtimeBundleSourceSessions,
	)
	require.Contains(
		t,
		text,
		"因企微 20 MB 限制，已省略部分资料："+
			runtimeBundleSourceDebug,
	)
}

func TestFormatRuntimeDebugBundleFullResult(
	t *testing.T,
) {
	t.Parallel()

	text := formatRuntimeDebugBundleFullResult(
		3,
		80*1024*1024,
		[]string{runtimeBundleConfigFileName},
		[]string{runtimeBundleSourceSessions},
		[]string{runtimeBundleSourceDebug},
	)
	require.Contains(t, text, "共 3 个分包")
	require.Contains(t, text, "总上限约为：80.0 MB")
	require.Contains(
		t,
		text,
		"因总上限限制，已省略部分资料："+
			runtimeBundleSourceDebug,
	)
}

func runtimeBundleArchiveNames(
	t *testing.T,
	archivePath string,
) []string {
	t.Helper()

	reader, err := zip.OpenReader(archivePath)
	require.NoError(t, err)
	defer reader.Close()

	names := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		names = append(names, file.Name)
	}
	return names
}

func runtimeBundleArchiveEntry(
	t *testing.T,
	archivePath string,
	name string,
) string {
	t.Helper()

	reader, err := zip.OpenReader(archivePath)
	require.NoError(t, err)
	defer reader.Close()

	for _, file := range reader.File {
		if file.Name != name {
			continue
		}
		rc, err := file.Open()
		require.NoError(t, err)
		defer rc.Close()

		data, err := io.ReadAll(rc)
		require.NoError(t, err)
		return string(data)
	}
	t.Fatalf("archive entry %q not found", name)
	return ""
}

func writeRuntimeBundleTestFile(
	t *testing.T,
	path string,
	data []byte,
) {
	t.Helper()

	require.NoError(
		t,
		os.MkdirAll(filepath.Dir(path), 0o755),
	)
	require.NoError(t, os.WriteFile(path, data, 0o644))
}

func runtimeBundleTestBytes(size int) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = byte((i*31 + 7) % 251)
	}
	return data
}
