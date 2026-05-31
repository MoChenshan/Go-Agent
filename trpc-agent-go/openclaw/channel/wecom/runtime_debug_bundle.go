package wecom

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"git.woa.com/trpc-go/trpc-agent-go/openclaw/assistantname"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/promptasset"
)

const (
	runtimeBundleDirName     = "debug-bundles"
	runtimeBundleModeCompact = "compact"
	runtimeBundleModeFull    = "full"

	runtimeBundleArchivePrefix  = "trpc-claw-debug-"
	runtimeBundleArchiveSuffix  = ".zip"
	runtimeBundleArchivePartTag = "-part"
	runtimeBundleManifestName   = "MANIFEST.txt"

	runtimeBundleConfigFileName = "openclaw.yaml"
	runtimeBundleTRPCConfigName = "trpc_go.yaml"
	runtimeBundleSessionDBName  = "sessions.sqlite"
	runtimeBundleDebugDirName   = "debug"
	runtimeBundleCronDirName    = "cron"
	runtimeBundleCronJobsName   = "jobs.json"
	runtimeBundleRuntimeDirName = "runtime"
	runtimeBundleLogsDirName    = "logs"
	runtimeBundleTRPCLogDirName = "log"

	runtimeBundleSourceIdentity    = "IDENTITY.md"
	runtimeBundleSourceInstruction = "prompts/instruction/"
	runtimeBundleSourcePersonas    = "personas/"
	runtimeBundleSourceTracker     = "wecom/session_tracker.json"
	runtimeBundleSourceSessions    = "sessions.sqlite"
	runtimeBundleSourceCronJobs    = "cron/jobs.json"
	runtimeBundleSourceDebug       = "debug/"
	runtimeBundleSourceSystem      = "prompts/system/"
	runtimeBundleSourceWeCom       = "prompts/wecom/request_system/"
	runtimeBundleSourceRuntimeCfg  = "runtime/openclaw.yaml"
	runtimeBundleSourceTRPCCfg     = "runtime/trpc_go.yaml"
	runtimeBundleSourceLogs        = "logs/"

	runtimeBundleSourceConfigPathEnv   = "TRPC_CLAW_ADMIN_SOURCE_CONFIG_PATH"
	runtimeBundleOpenClawConfigEnv     = "OPENCLAW_CONFIG"
	runtimeBundleConfigPathEnv         = "TRPC_CLAW_CONFIG_PATH"
	runtimeBundleStateDirEnv           = "TRPC_CLAW_STATE_DIR"
	runtimeBundleBinEnv                = "TRPC_CLAW_BIN"
	runtimeBundleBinDirEnv             = "TRPC_CLAW_BIN_DIR"
	runtimeBundleOpenClawBinEnv        = "OPENCLAW_BIN"
	runtimeBundleOpenClawBinDirEnv     = "OPENCLAW_BIN_DIR"
	runtimeBundleLogDirEnv             = "TRPC_CLAW_LOG_DIR"
	runtimeBundleOpenClawLogDirEnv     = "OPENCLAW_LOG_DIR"
	runtimeBundleTRPCLogDirEnv         = "TRPC_LOG_DIR"
	runtimeBundleGenericLogDirEnv      = "LOG_DIR"
	runtimeBundleLogPathEnv            = "TRPC_CLAW_LOG_PATH"
	runtimeBundleOpenClawLogPathEnv    = "OPENCLAW_LOG_PATH"
	runtimeBundleTRPCLogPathEnv        = "TRPC_LOG_PATH"
	runtimeBundleTRPCLogFileEnv        = "TRPC_LOG_FILE"
	runtimeBundleGenericLogFileEnv     = "LOG_FILE"
	runtimeBundleLogPatternTRPC        = "trpc.log*"
	runtimeBundleLogPatternSlim        = "trpc.slim.log*"
	runtimeBundleLogPatternOpenClaw    = "openclaw.log*"
	runtimeBundleLogPatternStart       = "start.log*"
	runtimeBundleArchiveNameFormat     = "%s-%d%s"
	runtimeBundleArchiveNameStartIndex = 2

	runtimeBundleSenderUnavailable = "当前回复通道不支持文件回传，" +
		"请改用支持附件回传的 AI 会话。"
	runtimeBundleTooLargeMessage = "当前实例的核心调试资料仍然超过" +
		"企微 20 MB 单附件上限，无法直接回传。请先清理较大的" +
		"调试目录或会话数据后再试。"
	runtimeBundleSingleFileTooLargeMessage = "当前实例里存在单个" +
		"调试文件本身就超过企微 20 MB 单附件上限，无法直接" +
		"回传。请先清理或压缩较大的调试文件后再试。"

	runtimeBundleArchiveSafetyMarginBytes = 512 * 1024
	runtimeBundleArchiveTargetBytes       = replyFileMaxBytes -
		runtimeBundleArchiveSafetyMarginBytes
	runtimeBundleManifestOmittedListLimit = 200

	runtimeBundleFullDefaultTotalBytes = 80 * 1024 * 1024
	runtimeBundleFullMaxTotalBytes     = 256 * 1024 * 1024
	runtimeBundlePartNumberWidth       = 2
	runtimeBundleSizeUnitBase          = 1024
)

type runtimeBundleEntry struct {
	ArchivePath string
	SourcePath  string
	Label       string
	Required    bool
	RecentFirst bool
}

type runtimeBundleFile struct {
	ArchivePath string
	SourcePath  string
	Label       string
	Size        int64
	ModTime     time.Time
	Required    bool
}

type runtimeBundleBuildResult struct {
	Included     []string
	Missing      []string
	Omitted      []string
	Archived     []string
	OmittedFiles []string
}

type runtimeDebugBundleRequest struct {
	Full            bool
	TotalLimitBytes int64
}

type runtimeBundleWriteOptions struct {
	TargetBytes        int64
	MaxBytes           int64
	OmittedListMaxSize int
}

type runtimeBundleMultipartOptions struct {
	TotalLimitBytes    int64
	PartTargetBytes    int64
	MaxBytes           int64
	OmittedListMaxSize int
}

type runtimeBundleMultipartResult struct {
	BuildResult  runtimeBundleBuildResult
	ArchivePaths []string
}

type runtimeBundleManifestMeta struct {
	Mode            string
	PartIndex       int
	ApproxTotalSize int64
	TargetBytes     int64
	MaxBytes        int64
}

func (c *Channel) sendRuntimeDebugBundle(
	ctx context.Context,
	chatID string,
	sender messageSender,
	req runtimeDebugBundleRequest,
) error {
	fileSender, ok := sender.(localFileSender)
	if !ok {
		_ = sendTextReply(
			ctx,
			sender,
			chatID,
			runtimeBundleSenderUnavailable,
		)
		return nil
	}

	if req.Full {
		return c.sendRuntimeDebugBundleFull(
			ctx,
			chatID,
			sender,
			fileSender,
			req,
		)
	}

	archivePath, included, missing, omitted, err := c.buildRuntimeDebugBundle()
	if err != nil {
		return err
	}
	if err := fileSender.SendLocalFile(ctx, chatID, archivePath); err != nil {
		var limitErr *replyMediaLimitError
		if errors.As(err, &limitErr) {
			return errors.New(runtimeBundleTooLargeMessage)
		}
		return err
	}

	_ = sendTextReply(
		ctx,
		sender,
		chatID,
		formatRuntimeDebugBundleResult(
			included,
			missing,
			omitted,
		),
	)
	return nil
}

func (c *Channel) sendRuntimeDebugBundleFull(
	ctx context.Context,
	chatID string,
	sender messageSender,
	fileSender localFileSender,
	req runtimeDebugBundleRequest,
) error {
	totalLimitBytes := normalizeRuntimeBundleTotalLimit(
		req.TotalLimitBytes,
	)
	result, err := c.buildRuntimeDebugBundleFull(
		totalLimitBytes,
	)
	if err != nil {
		return err
	}
	for _, archivePath := range result.ArchivePaths {
		if err := fileSender.SendLocalFile(
			ctx,
			chatID,
			archivePath,
		); err != nil {
			var limitErr *replyMediaLimitError
			if errors.As(err, &limitErr) {
				return errors.New(runtimeBundleTooLargeMessage)
			}
			return err
		}
	}

	_ = sendTextReply(
		ctx,
		sender,
		chatID,
		formatRuntimeDebugBundleFullResult(
			len(result.ArchivePaths),
			totalLimitBytes,
			result.BuildResult.Included,
			result.BuildResult.Missing,
			result.BuildResult.Omitted,
		),
	)
	return nil
}

func (c *Channel) buildRuntimeDebugBundle() (
	string,
	[]string,
	[]string,
	[]string,
	error,
) {
	bundleDir, err := c.ensureRuntimeBundleDir()
	if err != nil {
		return "", nil, nil, nil, err
	}

	archivePath := filepath.Join(
		bundleDir,
		runtimeBundleArchivePrefix+
			time.Now().Format("20060102-150405")+
			runtimeBundleArchiveSuffix,
	)
	result, err := writeRuntimeDebugBundle(
		archivePath,
		c.runtimeDebugBundleEntries(),
	)
	if err != nil {
		return "", nil, nil, nil, err
	}
	return archivePath,
		result.Included,
		result.Missing,
		result.Omitted,
		nil
}

func (c *Channel) buildRuntimeDebugBundleFull(
	totalLimitBytes int64,
) (runtimeBundleMultipartResult, error) {
	bundleDir, err := c.ensureRuntimeBundleDir()
	if err != nil {
		return runtimeBundleMultipartResult{}, err
	}

	basePath := filepath.Join(
		bundleDir,
		runtimeBundleArchivePrefix+
			time.Now().Format("20060102-150405"),
	)
	return writeRuntimeDebugBundleMultipartWithOptions(
		basePath,
		c.runtimeDebugBundleEntries(),
		defaultRuntimeBundleMultipartOptions(totalLimitBytes),
	)
}

func (c *Channel) ensureRuntimeBundleDir() (string, error) {
	root := strings.TrimSpace(c.runtimeTempRoot)
	if root == "" {
		root = strings.TrimSpace(c.stateDir)
	}
	if root == "" {
		root = os.TempDir()
	}
	path := filepath.Join(root, runtimeBundleDirName)
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", fmt.Errorf(
			"wecom: create runtime bundle dir: %w",
			err,
		)
	}
	return path, nil
}

func (c *Channel) runtimeDebugBundleEntries() []runtimeBundleEntry {
	paths := promptasset.DefaultPaths(c.stateDir)
	entries := []runtimeBundleEntry{
		{
			ArchivePath: runtimeBundleConfigFileName,
			SourcePath: runtimeBundleStatePath(
				c.stateDir,
				runtimeBundleConfigFileName,
			),
			Label:    runtimeBundleConfigFileName,
			Required: true,
		},
		{
			ArchivePath: assistantname.FileName,
			SourcePath:  c.assistantIdentityFile,
			Label:       runtimeBundleSourceIdentity,
			Required:    true,
		},
		{
			ArchivePath: filepath.Join("prompts", "instruction"),
			SourcePath:  paths.InstructionDir,
			Label:       runtimeBundleSourceInstruction,
			Required:    true,
		},
		{
			ArchivePath: filepath.Join("prompts", "system"),
			SourcePath:  paths.SystemDir,
			Label:       runtimeBundleSourceSystem,
			Required:    true,
		},
		{
			ArchivePath: filepath.Join(
				"prompts",
				"wecom",
				"request_system",
			),
			SourcePath: paths.WeComRequestDir,
			Label:      runtimeBundleSourceWeCom,
			Required:   true,
		},
		{
			ArchivePath: "personas",
			SourcePath:  paths.PersonaDir,
			Label:       runtimeBundleSourcePersonas,
			Required:    true,
		},
		{
			ArchivePath: filepath.Join(
				sessionTrackerStoreDirName,
				sessionTrackerStoreFileName,
			),
			SourcePath: sessionTrackerStorePath(c.stateDir),
			Label:      runtimeBundleSourceTracker,
			Required:   true,
		},
		{
			ArchivePath: runtimeBundleSessionDBName,
			SourcePath: runtimeBundleStatePath(
				c.stateDir,
				runtimeBundleSessionDBName,
			),
			Label: runtimeBundleSourceSessions,
		},
		{
			ArchivePath: filepath.Join(
				runtimeBundleCronDirName,
				runtimeBundleCronJobsName,
			),
			SourcePath: runtimeBundleStatePath(
				c.stateDir,
				filepath.Join(
					runtimeBundleCronDirName,
					runtimeBundleCronJobsName,
				),
			),
			Label:    runtimeBundleSourceCronJobs,
			Required: true,
		},
	}
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}
	entries = append(
		entries,
		runtimeAutoDiscoveredBundleEntries(c.stateDir, cwd)...,
	)
	entries = append(entries, runtimeBundleEntry{
		ArchivePath: runtimeBundleDebugDirName,
		SourcePath: runtimeBundleStatePath(
			c.stateDir,
			runtimeBundleDebugDirName,
		),
		Label:       runtimeBundleSourceDebug,
		RecentFirst: true,
	})
	return entries
}

func runtimeBundleStatePath(root string, name string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	return filepath.Join(root, name)
}

func runtimeAutoDiscoveredBundleEntries(
	stateDir string,
	cwd string,
) []runtimeBundleEntry {
	seenSources := make(map[string]struct{})
	seenArchives := make(map[string]struct{})
	entries := make([]runtimeBundleEntry, 0, 8)

	addFile := func(archivePath string, sourcePath string, label string) {
		appendExistingRuntimeBundleFileEntry(
			&entries,
			seenSources,
			seenArchives,
			archivePath,
			sourcePath,
			label,
		)
	}

	for _, path := range runtimeBundleConfigPathCandidates(stateDir, cwd) {
		addFile(
			filepath.Join(
				runtimeBundleRuntimeDirName,
				runtimeBundleConfigFileName,
			),
			path,
			runtimeBundleSourceRuntimeCfg,
		)
	}
	for _, path := range runtimeBundleTRPCConfigPathCandidates(stateDir, cwd) {
		addFile(
			filepath.Join(
				runtimeBundleRuntimeDirName,
				runtimeBundleTRPCConfigName,
			),
			path,
			runtimeBundleSourceTRPCCfg,
		)
	}

	entries = append(
		entries,
		runtimeBundleLogFileEntries(
			runtimeBundleEnvPaths(runtimeBundleLogFileEnvNames(), cwd),
			seenSources,
			seenArchives,
		)...,
	)
	entries = append(
		entries,
		runtimeBundleLogEntries(
			runtimeBundleLogDirCandidates(stateDir, cwd),
			seenSources,
			seenArchives,
		)...,
	)
	return entries
}

func runtimeBundleConfigPathCandidates(
	stateDir string,
	cwd string,
) []string {
	paths := runtimeBundleEnvPaths(runtimeBundleConfigPathEnvNames(), cwd)
	paths = append(
		paths,
		runtimeBundleRootFileCandidates(
			runtimeBundleRootCandidates(stateDir, cwd),
			runtimeBundleConfigFileName,
		)...,
	)
	return runtimeBundleUniquePaths(paths)
}

func runtimeBundleTRPCConfigPathCandidates(
	stateDir string,
	cwd string,
) []string {
	return runtimeBundleRootFileCandidates(
		runtimeBundleRootCandidates(stateDir, cwd),
		runtimeBundleTRPCConfigName,
	)
}

func runtimeBundleLogDirCandidates(
	stateDir string,
	cwd string,
) []string {
	roots := runtimeBundleRootCandidates(stateDir, cwd)
	dirs := runtimeBundleEnvPaths(runtimeBundleLogDirEnvNames(), cwd)
	for _, root := range roots {
		dirs = append(
			dirs,
			runtimeBundleJoin(root, "..", runtimeBundleTRPCLogDirName),
			runtimeBundleJoin(root, "..", runtimeBundleLogsDirName),
			runtimeBundleJoin(root, runtimeBundleTRPCLogDirName),
			runtimeBundleJoin(root, runtimeBundleLogsDirName),
		)
	}
	dirs = append(dirs, runtimeBundleEnvPaths(
		[]string{runtimeBundleStateDirEnv},
		cwd,
	)...)
	dirs = append(dirs, strings.TrimSpace(stateDir))
	return runtimeBundleUniquePaths(dirs)
}

func runtimeBundleConfigPathEnvNames() []string {
	return []string{
		runtimeBundleSourceConfigPathEnv,
		runtimeBundleOpenClawConfigEnv,
		runtimeBundleConfigPathEnv,
	}
}

func runtimeBundleRuntimeDirEnvNames() []string {
	return []string{
		runtimeBundleBinDirEnv,
		runtimeBundleOpenClawBinDirEnv,
	}
}

func runtimeBundleRuntimeFileEnvNames() []string {
	return []string{
		runtimeBundleBinEnv,
		runtimeBundleOpenClawBinEnv,
	}
}

func runtimeBundleLogDirEnvNames() []string {
	return []string{
		runtimeBundleLogDirEnv,
		runtimeBundleOpenClawLogDirEnv,
		runtimeBundleTRPCLogDirEnv,
		runtimeBundleGenericLogDirEnv,
	}
}

func runtimeBundleLogFileEnvNames() []string {
	return []string{
		runtimeBundleLogPathEnv,
		runtimeBundleOpenClawLogPathEnv,
		runtimeBundleTRPCLogPathEnv,
		runtimeBundleTRPCLogFileEnv,
		runtimeBundleGenericLogFileEnv,
	}
}

func runtimeBundleRootCandidates(
	stateDir string,
	cwd string,
) []string {
	roots := make([]string, 0, 10)
	for _, path := range runtimeBundleEnvPaths(runtimeBundleConfigPathEnvNames(), cwd) {
		roots = append(roots, filepath.Dir(path))
	}
	for _, path := range runtimeBundleEnvPaths(runtimeBundleRuntimeFileEnvNames(), cwd) {
		roots = append(roots, filepath.Dir(path))
	}
	roots = append(roots, runtimeBundleEnvPaths(
		runtimeBundleRuntimeDirEnvNames(),
		cwd,
	)...)
	roots = append(roots, cwd)
	roots = append(roots, runtimeBundleExecutableDir())
	roots = append(roots, runtimeBundleEnvPaths(
		[]string{runtimeBundleStateDirEnv},
		cwd,
	)...)
	roots = append(roots, strings.TrimSpace(stateDir))
	return runtimeBundleUniquePaths(roots)
}

func runtimeBundleRootFileCandidates(
	roots []string,
	name string,
) []string {
	paths := make([]string, 0, len(roots))
	for _, root := range roots {
		paths = append(paths, runtimeBundleJoin(root, name))
	}
	return runtimeBundleUniquePaths(paths)
}

func runtimeBundleEnvPaths(
	envNames []string,
	cwd string,
) []string {
	paths := make([]string, 0, len(envNames))
	for _, envName := range envNames {
		paths = append(
			paths,
			runtimeBundleResolveCandidatePath(os.Getenv(envName), cwd),
		)
	}
	return runtimeBundleUniquePaths(paths)
}

func runtimeBundleExecutableDir() string {
	path, err := os.Executable()
	if err != nil {
		return ""
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.Dir(path)
}

func runtimeBundleUniquePaths(paths []string) []string {
	seen := make(map[string]struct{})
	unique := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		cleanPath, err := filepath.Abs(path)
		if err != nil {
			cleanPath = filepath.Clean(path)
		}
		if _, ok := seen[cleanPath]; ok {
			continue
		}
		seen[cleanPath] = struct{}{}
		unique = append(unique, path)
	}
	return unique
}

func runtimeBundleJoin(root string, elems ...string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	parts := append([]string{root}, elems...)
	return filepath.Join(parts...)
}

func runtimeBundleResolveCandidatePath(path string, cwd string) string {
	path = strings.TrimSpace(os.ExpandEnv(path))
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil && strings.TrimSpace(home) != "" {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	if filepath.IsAbs(path) {
		return path
	}
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return path
	}
	return filepath.Join(cwd, path)
}

func appendExistingRuntimeBundleFileEntry(
	entries *[]runtimeBundleEntry,
	seenSources map[string]struct{},
	seenArchives map[string]struct{},
	archivePath string,
	sourcePath string,
	label string,
) {
	sourcePath = strings.TrimSpace(sourcePath)
	if sourcePath == "" {
		return
	}
	info, err := os.Stat(sourcePath)
	if err != nil || !info.Mode().IsRegular() {
		return
	}
	appendRuntimeBundleEntry(
		entries,
		seenSources,
		seenArchives,
		runtimeBundleEntry{
			ArchivePath: filepath.ToSlash(archivePath),
			SourcePath:  sourcePath,
			Label:       label,
		},
	)
}

func appendRuntimeBundleEntry(
	entries *[]runtimeBundleEntry,
	seenSources map[string]struct{},
	seenArchives map[string]struct{},
	entry runtimeBundleEntry,
) {
	sourcePath := strings.TrimSpace(entry.SourcePath)
	archivePath := filepath.ToSlash(strings.TrimSpace(entry.ArchivePath))
	if sourcePath == "" || archivePath == "" {
		return
	}
	cleanSource, err := filepath.Abs(sourcePath)
	if err != nil {
		cleanSource = filepath.Clean(sourcePath)
	}
	if _, ok := seenSources[cleanSource]; ok {
		return
	}
	if _, ok := seenArchives[archivePath]; ok {
		return
	}
	entry.SourcePath = sourcePath
	entry.ArchivePath = archivePath
	seenSources[cleanSource] = struct{}{}
	seenArchives[archivePath] = struct{}{}
	*entries = append(*entries, entry)
}

type runtimeBundleDiscoveredLog struct {
	path    string
	modTime time.Time
}

func runtimeBundleLogFileEntries(
	paths []string,
	seenSources map[string]struct{},
	seenArchives map[string]struct{},
) []runtimeBundleEntry {
	entries := make([]runtimeBundleEntry, 0, len(paths))
	for _, path := range paths {
		appendExistingRuntimeBundleFileEntry(
			&entries,
			seenSources,
			seenArchives,
			runtimeBundleLogArchivePath(path, seenArchives),
			path,
			runtimeBundleSourceLogs,
		)
	}
	return entries
}

func runtimeBundleLogEntries(
	dirs []string,
	seenSources map[string]struct{},
	seenArchives map[string]struct{},
) []runtimeBundleEntry {
	logs := runtimeBundleDiscoverLogFiles(dirs)
	entries := make([]runtimeBundleEntry, 0, len(logs))
	for _, logFile := range logs {
		appendRuntimeBundleEntry(
			&entries,
			seenSources,
			seenArchives,
			runtimeBundleEntry{
				ArchivePath: runtimeBundleLogArchivePath(
					logFile.path,
					seenArchives,
				),
				SourcePath: logFile.path,
				Label:      runtimeBundleSourceLogs,
			},
		)
	}
	return entries
}

func runtimeBundleLogArchivePath(
	path string,
	seenArchives map[string]struct{},
) string {
	baseName := filepath.Base(strings.TrimSpace(path))
	archivePath := filepath.ToSlash(filepath.Join(
		runtimeBundleLogsDirName,
		baseName,
	))
	if !runtimeBundleArchivePathSeen(archivePath, seenArchives) {
		return archivePath
	}
	stem, ext := runtimeBundleSplitArchiveName(baseName)
	for index := runtimeBundleArchiveNameStartIndex; ; index++ {
		candidate := filepath.ToSlash(filepath.Join(
			runtimeBundleLogsDirName,
			fmt.Sprintf(
				runtimeBundleArchiveNameFormat,
				stem,
				index,
				ext,
			),
		))
		if !runtimeBundleArchivePathSeen(candidate, seenArchives) {
			return candidate
		}
	}
}

func runtimeBundleSplitArchiveName(name string) (string, string) {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	if stem == "" {
		return name, ""
	}
	return stem, ext
}

func runtimeBundleArchivePathSeen(
	archivePath string,
	seenArchives map[string]struct{},
) bool {
	_, ok := seenArchives[filepath.ToSlash(strings.TrimSpace(archivePath))]
	return ok
}

func runtimeBundleDiscoverLogFiles(
	dirs []string,
) []runtimeBundleDiscoveredLog {
	seen := make(map[string]struct{})
	logs := make([]runtimeBundleDiscoveredLog, 0)
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !runtimeBundleLogNameMatches(entry.Name()) {
				continue
			}
			appendRuntimeBundleDiscoveredLog(
				&logs,
				seen,
				filepath.Join(dir, entry.Name()),
			)
		}
	}
	sort.SliceStable(logs, func(i int, j int) bool {
		if logs[i].modTime.Equal(logs[j].modTime) {
			return logs[i].path < logs[j].path
		}
		return logs[i].modTime.After(logs[j].modTime)
	})
	return logs
}

func runtimeBundleLogNameMatches(name string) bool {
	for _, pattern := range runtimeBundleLogPatterns() {
		matched, err := filepath.Match(pattern, name)
		if err != nil {
			continue
		}
		if matched {
			return true
		}
	}
	return false
}

func runtimeBundleLogPatterns() []string {
	return []string{
		runtimeBundleLogPatternTRPC,
		runtimeBundleLogPatternSlim,
		runtimeBundleLogPatternOpenClaw,
		runtimeBundleLogPatternStart,
	}
}

func appendRuntimeBundleDiscoveredLog(
	logs *[]runtimeBundleDiscoveredLog,
	seen map[string]struct{},
	path string,
) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return
	}
	cleanPath, err := filepath.Abs(path)
	if err != nil {
		cleanPath = filepath.Clean(path)
	}
	if _, ok := seen[cleanPath]; ok {
		return
	}
	seen[cleanPath] = struct{}{}
	*logs = append(*logs, runtimeBundleDiscoveredLog{
		path:    path,
		modTime: info.ModTime(),
	})
}

func writeRuntimeDebugBundle(
	archivePath string,
	entries []runtimeBundleEntry,
) (runtimeBundleBuildResult, error) {
	return writeRuntimeDebugBundleWithOptions(
		archivePath,
		entries,
		defaultRuntimeBundleWriteOptions(),
	)
}

func defaultRuntimeBundleWriteOptions() runtimeBundleWriteOptions {
	return runtimeBundleWriteOptions{
		TargetBytes:        runtimeBundleArchiveTargetBytes,
		MaxBytes:           replyFileMaxBytes,
		OmittedListMaxSize: runtimeBundleManifestOmittedListLimit,
	}
}

func defaultRuntimeBundleMultipartOptions(
	totalLimitBytes int64,
) runtimeBundleMultipartOptions {
	return runtimeBundleMultipartOptions{
		TotalLimitBytes: normalizeRuntimeBundleTotalLimit(
			totalLimitBytes,
		),
		PartTargetBytes:    runtimeBundleArchiveTargetBytes,
		MaxBytes:           replyFileMaxBytes,
		OmittedListMaxSize: runtimeBundleManifestOmittedListLimit,
	}
}

func normalizeRuntimeBundleTotalLimit(totalLimitBytes int64) int64 {
	if totalLimitBytes <= 0 {
		return runtimeBundleFullDefaultTotalBytes
	}
	if totalLimitBytes > runtimeBundleFullMaxTotalBytes {
		return runtimeBundleFullMaxTotalBytes
	}
	return totalLimitBytes
}

func writeRuntimeDebugBundleMultipartWithOptions(
	basePath string,
	entries []runtimeBundleEntry,
	opts runtimeBundleMultipartOptions,
) (runtimeBundleMultipartResult, error) {
	files, missing, err := collectRuntimeBundleFiles(entries)
	if err != nil {
		return runtimeBundleMultipartResult{}, err
	}
	selected, omitted := selectRuntimeBundleFiles(
		files,
		opts.TotalLimitBytes,
	)
	baseResult := buildRuntimeBundleResult(
		selected,
		missing,
		omitted,
		nil,
		opts.OmittedListMaxSize,
	)
	partPaths, archived, err := writeRuntimeBundleMultipartArchives(
		basePath,
		selected,
		baseResult,
		opts,
	)
	if err != nil {
		return runtimeBundleMultipartResult{}, err
	}
	baseResult.Archived = archived
	return runtimeBundleMultipartResult{
		BuildResult:  baseResult,
		ArchivePaths: partPaths,
	}, nil
}

func writeRuntimeDebugBundleWithOptions(
	archivePath string,
	entries []runtimeBundleEntry,
	opts runtimeBundleWriteOptions,
) (runtimeBundleBuildResult, error) {
	files, missing, err := collectRuntimeBundleFiles(entries)
	if err != nil {
		return runtimeBundleBuildResult{}, err
	}
	selected, omitted := selectRuntimeBundleFiles(
		files,
		opts.TargetBytes,
	)
	for {
		baseResult := buildRuntimeBundleResult(
			selected,
			missing,
			omitted,
			nil,
			opts.OmittedListMaxSize,
		)
		result, err := writeRuntimeBundleArchive(
			archivePath,
			selected,
			baseResult,
			runtimeBundleManifestMeta{
				Mode:        runtimeBundleModeCompact,
				TargetBytes: opts.TargetBytes,
				MaxBytes:    opts.MaxBytes,
			},
		)
		if err != nil {
			return runtimeBundleBuildResult{}, err
		}

		info, err := os.Stat(archivePath)
		if err != nil {
			return runtimeBundleBuildResult{}, fmt.Errorf(
				"wecom: stat runtime debug bundle: %w",
				err,
			)
		}
		if info.Size() <= opts.MaxBytes {
			return result, nil
		}

		dropIndex := lastOptionalRuntimeBundleFile(selected)
		if dropIndex < 0 {
			return runtimeBundleBuildResult{}, errors.New(
				runtimeBundleTooLargeMessage,
			)
		}
		omitted = append(omitted, selected[dropIndex])
		selected = append(
			selected[:dropIndex],
			selected[dropIndex+1:]...,
		)
	}
}

func collectRuntimeBundleFiles(
	entries []runtimeBundleEntry,
) ([]runtimeBundleFile, []string, error) {
	files := make([]runtimeBundleFile, 0, len(entries))
	missing := make([]string, 0, len(entries))
	for _, entry := range entries {
		collected, found, err := collectRuntimeBundleFilesForEntry(
			entry,
		)
		if err != nil {
			return nil, nil, err
		}
		if !found {
			missing = append(missing, strings.TrimSpace(entry.Label))
			continue
		}
		files = append(files, collected...)
	}
	sort.Strings(missing)
	return files, missing, nil
}

func collectRuntimeBundleFilesForEntry(
	entry runtimeBundleEntry,
) ([]runtimeBundleFile, bool, error) {
	sourcePath := strings.TrimSpace(entry.SourcePath)
	if sourcePath == "" {
		return nil, false, nil
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf(
			"wecom: stat runtime debug bundle path %q: %w",
			sourcePath,
			err,
		)
	}
	if !info.IsDir() {
		return []runtimeBundleFile{{
			ArchivePath: filepath.ToSlash(
				strings.TrimSpace(entry.ArchivePath),
			),
			SourcePath: sourcePath,
			Label:      strings.TrimSpace(entry.Label),
			Size:       info.Size(),
			ModTime:    info.ModTime(),
			Required:   entry.Required,
		}}, true, nil
	}

	files := make([]runtimeBundleFile, 0, 16)
	if err := filepath.WalkDir(
		sourcePath,
		func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return fmt.Errorf(
					"wecom: walk runtime debug bundle path %q: %w",
					path,
					walkErr,
				)
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(sourcePath, path)
			if err != nil {
				return fmt.Errorf(
					"wecom: build runtime debug bundle "+
						"relative path: %w",
					err,
				)
			}
			info, err := d.Info()
			if err != nil {
				return fmt.Errorf(
					"wecom: stat runtime debug bundle file %q: %w",
					path,
					err,
				)
			}
			files = append(files, runtimeBundleFile{
				ArchivePath: filepath.ToSlash(
					filepath.Join(
						strings.TrimSpace(entry.ArchivePath),
						rel,
					),
				),
				SourcePath: path,
				Label:      strings.TrimSpace(entry.Label),
				Size:       info.Size(),
				ModTime:    info.ModTime(),
				Required:   entry.Required,
			})
			return nil
		},
	); err != nil {
		return nil, false, err
	}
	sortRuntimeBundleFiles(files, entry.RecentFirst)
	return files, true, nil
}

func sortRuntimeBundleFiles(
	files []runtimeBundleFile,
	recentFirst bool,
) {
	sort.SliceStable(files, func(i int, j int) bool {
		if recentFirst {
			if files[i].ModTime.Equal(files[j].ModTime) {
				return files[i].ArchivePath < files[j].ArchivePath
			}
			return files[i].ModTime.After(files[j].ModTime)
		}
		return files[i].ArchivePath < files[j].ArchivePath
	})
}

func selectRuntimeBundleFiles(
	files []runtimeBundleFile,
	targetBytes int64,
) ([]runtimeBundleFile, []runtimeBundleFile) {
	selected := make([]runtimeBundleFile, 0, len(files))
	omitted := make([]runtimeBundleFile, 0)
	var totalBytes int64
	for _, file := range files {
		if file.Required {
			selected = append(selected, file)
			totalBytes += file.Size
			continue
		}
		if targetBytes > 0 && totalBytes+file.Size > targetBytes {
			omitted = append(omitted, file)
			continue
		}
		selected = append(selected, file)
		totalBytes += file.Size
	}
	return selected, omitted
}

func lastOptionalRuntimeBundleFile(
	files []runtimeBundleFile,
) int {
	for i := len(files) - 1; i >= 0; i-- {
		if files[i].Required {
			continue
		}
		return i
	}
	return -1
}

func writeRuntimeBundleMultipartArchives(
	basePath string,
	selected []runtimeBundleFile,
	baseResult runtimeBundleBuildResult,
	opts runtimeBundleMultipartOptions,
) ([]string, []string, error) {
	if len(selected) == 0 {
		archivePath := runtimeBundlePartPath(basePath, 1)
		result, err := writeRuntimeBundleArchive(
			archivePath,
			nil,
			baseResult,
			runtimeBundleManifestMeta{
				Mode:            runtimeBundleModeFull,
				PartIndex:       1,
				ApproxTotalSize: opts.TotalLimitBytes,
				TargetBytes:     opts.PartTargetBytes,
				MaxBytes:        opts.MaxBytes,
			},
		)
		if err != nil {
			return nil, nil, err
		}
		return []string{archivePath}, result.Archived, nil
	}

	remaining := append([]runtimeBundleFile(nil), selected...)
	partPaths := make([]string, 0, 4)
	allArchived := make([]string, 0, len(selected))
	partIndex := 1
	for len(remaining) > 0 {
		partFiles, rest := takeRuntimeBundlePart(
			remaining,
			opts.PartTargetBytes,
		)
		if len(partFiles) == 0 {
			return nil, nil, errors.New(
				runtimeBundleTooLargeMessage,
			)
		}

		archivePath := runtimeBundlePartPath(basePath, partIndex)
		for {
			result, err := writeRuntimeBundleArchive(
				archivePath,
				partFiles,
				baseResult,
				runtimeBundleManifestMeta{
					Mode:            runtimeBundleModeFull,
					PartIndex:       partIndex,
					ApproxTotalSize: opts.TotalLimitBytes,
					TargetBytes:     opts.PartTargetBytes,
					MaxBytes:        opts.MaxBytes,
				},
			)
			if err != nil {
				return nil, nil, err
			}

			info, err := os.Stat(archivePath)
			if err != nil {
				return nil, nil, fmt.Errorf(
					"wecom: stat runtime debug bundle: %w",
					err,
				)
			}
			if info.Size() <= opts.MaxBytes {
				partPaths = append(partPaths, archivePath)
				allArchived = append(
					allArchived,
					result.Archived...,
				)
				remaining = rest
				partIndex++
				break
			}

			if len(partFiles) == 1 {
				return nil, nil, errors.New(
					runtimeBundleSingleFileTooLargeMessage,
				)
			}

			moved := partFiles[len(partFiles)-1]
			partFiles = partFiles[:len(partFiles)-1]
			rest = append([]runtimeBundleFile{moved}, rest...)
		}
	}
	sort.Strings(allArchived)
	return partPaths, allArchived, nil
}

func takeRuntimeBundlePart(
	files []runtimeBundleFile,
	targetBytes int64,
) ([]runtimeBundleFile, []runtimeBundleFile) {
	if len(files) == 0 {
		return nil, nil
	}
	var totalBytes int64
	for i, file := range files {
		if i > 0 &&
			targetBytes > 0 &&
			totalBytes+file.Size > targetBytes {
			return files[:i], files[i:]
		}
		totalBytes += file.Size
	}
	return files, nil
}

func runtimeBundlePartPath(basePath string, partIndex int) string {
	return fmt.Sprintf(
		"%s%s%0*d%s",
		basePath,
		runtimeBundleArchivePartTag,
		runtimeBundlePartNumberWidth,
		partIndex,
		runtimeBundleArchiveSuffix,
	)
}

func writeRuntimeBundleArchive(
	archivePath string,
	selected []runtimeBundleFile,
	baseResult runtimeBundleBuildResult,
	meta runtimeBundleManifestMeta,
) (runtimeBundleBuildResult, error) {
	file, err := os.Create(archivePath)
	if err != nil {
		return runtimeBundleBuildResult{}, fmt.Errorf(
			"wecom: create runtime debug bundle: %w",
			err,
		)
	}
	defer file.Close()

	zw := zip.NewWriter(file)
	archived := make([]string, 0, len(selected))
	for _, bundleFile := range selected {
		if err := appendRuntimeBundleFile(
			zw,
			bundleFile.SourcePath,
			bundleFile.ArchivePath,
			&archived,
		); err != nil {
			_ = zw.Close()
			return runtimeBundleBuildResult{}, err
		}
	}

	result := cloneRuntimeBundleResult(baseResult)
	result.Archived = append([]string(nil), archived...)
	sort.Strings(result.Archived)
	if err := writeRuntimeBundleManifest(
		zw,
		result,
		meta,
	); err != nil {
		_ = zw.Close()
		return runtimeBundleBuildResult{}, err
	}
	if err := zw.Close(); err != nil {
		return runtimeBundleBuildResult{}, fmt.Errorf(
			"wecom: close runtime debug bundle: %w",
			err,
		)
	}
	return result, nil
}

func cloneRuntimeBundleResult(
	src runtimeBundleBuildResult,
) runtimeBundleBuildResult {
	return runtimeBundleBuildResult{
		Included: append([]string(nil), src.Included...),
		Missing:  append([]string(nil), src.Missing...),
		Omitted:  append([]string(nil), src.Omitted...),
		Archived: append([]string(nil), src.Archived...),
		OmittedFiles: append(
			[]string(nil),
			src.OmittedFiles...,
		),
	}
}

func appendRuntimeBundleFile(
	zw *zip.Writer,
	sourcePath string,
	archivePath string,
	archived *[]string,
) error {
	info, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf(
			"wecom: stat runtime debug bundle file %q: %w",
			sourcePath,
			err,
		)
	}

	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return fmt.Errorf(
			"wecom: build runtime debug bundle header: %w",
			err,
		)
	}
	header.Name = filepath.ToSlash(strings.TrimSpace(archivePath))
	header.Method = zip.Deflate

	writer, err := zw.CreateHeader(header)
	if err != nil {
		return fmt.Errorf(
			"wecom: create runtime debug bundle entry: %w",
			err,
		)
	}

	file, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf(
			"wecom: open runtime debug bundle file %q: %w",
			sourcePath,
			err,
		)
	}
	defer file.Close()

	if _, err := io.Copy(writer, file); err != nil {
		return fmt.Errorf(
			"wecom: write runtime debug bundle file %q: %w",
			sourcePath,
			err,
		)
	}
	*archived = append(*archived, header.Name)
	return nil
}

func writeRuntimeBundleManifest(
	zw *zip.Writer,
	result runtimeBundleBuildResult,
	meta runtimeBundleManifestMeta,
) error {
	writer, err := zw.Create(runtimeBundleManifestName)
	if err != nil {
		return fmt.Errorf(
			"wecom: create runtime debug bundle manifest: %w",
			err,
		)
	}

	lines := []string{
		"Runtime debug bundle",
		"Generated at: " + time.Now().Format(time.RFC3339),
	}
	if strings.TrimSpace(meta.Mode) != "" {
		lines = append(lines, "Mode: "+meta.Mode)
	}
	if meta.PartIndex > 0 {
		lines = append(
			lines,
			fmt.Sprintf("Part: %d", meta.PartIndex),
		)
	}
	if meta.ApproxTotalSize > 0 {
		lines = append(
			lines,
			"Approx total limit: "+
				formatRuntimeBundleBytes(meta.ApproxTotalSize),
		)
	}
	if meta.TargetBytes > 0 || meta.MaxBytes > 0 {
		lines = append(
			lines,
			fmt.Sprintf(
				"Target size: <= %d bytes (WeCom file limit %d bytes)",
				meta.TargetBytes,
				meta.MaxBytes,
			),
		)
	}
	lines = append(lines, "", "Included sources:")
	if len(result.Included) == 0 {
		lines = append(lines, "- (none)")
	} else {
		for _, label := range result.Included {
			lines = append(lines, "- "+label)
		}
	}

	lines = append(lines, "", "Missing default paths:")
	if len(result.Missing) == 0 {
		lines = append(lines, "- (none)")
	} else {
		for _, label := range result.Missing {
			lines = append(lines, "- "+label)
		}
	}

	lines = append(lines, "", "Omitted by size budget:")
	if len(result.Omitted) == 0 {
		lines = append(lines, "- (none)")
	} else {
		for _, label := range result.Omitted {
			lines = append(lines, "- "+label)
		}
	}

	lines = append(lines, "", "Archived files:")
	if len(result.Archived) == 0 {
		lines = append(lines, "- (none)")
	} else {
		for _, path := range result.Archived {
			lines = append(lines, "- "+path)
		}
	}

	lines = append(lines, "", "Files omitted from archive:")
	if len(result.OmittedFiles) == 0 {
		lines = append(lines, "- (none)")
	} else {
		for _, path := range result.OmittedFiles {
			lines = append(lines, "- "+path)
		}
	}

	if _, err := io.WriteString(
		writer,
		strings.Join(lines, "\n")+"\n",
	); err != nil {
		return fmt.Errorf(
			"wecom: write runtime debug bundle manifest: %w",
			err,
		)
	}
	return nil
}

func buildRuntimeBundleResult(
	selected []runtimeBundleFile,
	missing []string,
	omitted []runtimeBundleFile,
	archived []string,
	omittedListMaxSize int,
) runtimeBundleBuildResult {
	sort.Strings(missing)
	sort.Strings(archived)
	return runtimeBundleBuildResult{
		Included: runtimeBundleLabels(selected),
		Missing:  dedupeSortedStrings(missing),
		Omitted:  runtimeBundleLabels(omitted),
		Archived: archived,
		OmittedFiles: limitedRuntimeBundlePaths(
			omitted,
			omittedListMaxSize,
		),
	}
}

func runtimeBundleLabels(files []runtimeBundleFile) []string {
	labels := make([]string, 0, len(files))
	seen := make(map[string]struct{})
	for _, file := range files {
		label := strings.TrimSpace(file.Label)
		if label == "" {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		labels = append(labels, label)
	}
	sort.Strings(labels)
	return labels
}

func dedupeSortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := values[:0]
	var last string
	for i, value := range values {
		if i == 0 || value != last {
			out = append(out, value)
			last = value
		}
	}
	return out
}

func limitedRuntimeBundlePaths(
	files []runtimeBundleFile,
	limit int,
) []string {
	if len(files) == 0 {
		return nil
	}
	paths := make([]string, 0, len(files))
	for _, file := range files {
		path := strings.TrimSpace(file.ArchivePath)
		if path == "" {
			continue
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	if limit <= 0 || len(paths) <= limit {
		return paths
	}
	remaining := len(paths) - limit
	paths = append(paths[:limit],
		fmt.Sprintf("... (%d more omitted files)", remaining))
	return paths
}

func formatRuntimeDebugBundleResult(
	included []string,
	missing []string,
	omitted []string,
) string {
	lines := []string{
		"✅ 已打包并回传调试资料。",
	}
	if len(included) > 0 {
		lines = append(
			lines,
			"已包含："+strings.Join(included, "、"),
		)
	}
	if len(missing) > 0 {
		lines = append(
			lines,
			"默认路径不存在："+strings.Join(missing, "、"),
		)
	}
	if len(omitted) > 0 {
		lines = append(
			lines,
			"因企微 20 MB 限制，已省略部分资料："+strings.Join(
				omitted,
				"、",
			)+"。详情见压缩包里的 MANIFEST.txt。",
		)
	}
	return strings.Join(lines, "\n")
}

func formatRuntimeDebugBundleFullResult(
	partCount int,
	totalLimitBytes int64,
	included []string,
	missing []string,
	omitted []string,
) string {
	lines := []string{
		"✅ 已按 full 模式回传调试资料，共 " +
			fmt.Sprintf("%d 个分包。", partCount),
		"总上限约为：" +
			formatRuntimeBundleBytes(totalLimitBytes) + "。",
	}
	if len(included) > 0 {
		lines = append(
			lines,
			"已包含："+strings.Join(included, "、"),
		)
	}
	if len(missing) > 0 {
		lines = append(
			lines,
			"默认路径不存在："+strings.Join(missing, "、"),
		)
	}
	if len(omitted) > 0 {
		lines = append(
			lines,
			"因总上限限制，已省略部分资料："+strings.Join(
				omitted,
				"、",
			)+"。详情见各分包里的 MANIFEST.txt。",
		)
	}
	return strings.Join(lines, "\n")
}

func formatRuntimeBundleBytes(size int64) string {
	if size < runtimeBundleSizeUnitBase {
		return fmt.Sprintf("%d B", size)
	}
	kb := int64(runtimeBundleSizeUnitBase)
	mb := kb * runtimeBundleSizeUnitBase
	gb := mb * runtimeBundleSizeUnitBase
	if size < mb {
		return fmt.Sprintf("%.1f KB", float64(size)/float64(kb))
	}
	if size < gb {
		return fmt.Sprintf("%.1f MB", float64(size)/float64(mb))
	}
	return fmt.Sprintf("%.1f GB", float64(size)/float64(gb))
}
