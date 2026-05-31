package pcg123

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"trpc.group/trpc-go/trpc-agent-go/codeexecutor"
	"trpc.group/trpc-go/trpc-agent-go/log"
	atrace "trpc.group/trpc-go/trpc-agent-go/telemetry/trace"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/codeexecutor/pcg123/internal/sessionuid"
)

const (
	maxFilesNum      = 100
	maxReadSizeBytes = 4 * 1024 * 1024
	maxTotalBytes    = 64 * 1024 * 1024
)

const (
	attrArgs = "args"
	attrEnv  = "env"
	attrCost = "cost"
)

const (
	defaultFileMode = 0o644
)

const (
	inputSchemeArtifact  = "artifact://"
	inputSchemeHost      = "host://"
	inputSchemeWorkspace = "workspace://"
	inputSchemeSkill     = "skill://"
	inputsDirName        = "inputs"
)

const (
	metadataTmpFileName = ".metadata.tmp"
	metadataTmpPrefix   = ".metadata."
	metadataTmpSuffix   = ".tmp"
)

const (
	metadataTmpPIDIndex = iota
	metadataTmpTimeIndex
	metadataTmpCounterIndex
	metadataTmpRandomIndex
	metadataTmpPartCount
)

const (
	metadataNoRandomSuffix = "norand"
	metadataRandomHexLen   = 16
)

type nfsClient interface {
	ExportPath() string
	MkdirAll(dirPath string) error
	WriteFile(filePath string, data []byte, perm os.FileMode) error
	ReadFile(filePath string) ([]byte, error)
	ReadFileLimited(filePath string, maxBytes int) ([]byte, error)
	RemoveAll(dirPath string) error
	Stat(filePath string) (os.FileInfo, error)
	ReadDir(dirPath string) ([]os.FileInfo, error)
	Glob(pattern string) ([]string, error)
}

// NFSRuntime implements workspace-based executor using NFS and remote 123 execution.
// It implements WorkspaceManager, WorkspaceFS, and ProgramRunner interfaces.
type NFSRuntime struct {
	nfsClient   func() nfsClient
	executor    *CodeExecutor
	ensureReady func(ctx context.Context) error
}

// NewNFSRuntime creates a new NFS-based runtime.
func NewNFSRuntime(executor *CodeExecutor) *NFSRuntime {
	return &NFSRuntime{
		nfsClient:   func() nfsClient { return executor.NFSClient() },
		executor:    executor,
		ensureReady: executor.ensureReady,
	}
}

// CreateWorkspace creates a new workspace on NFS server.
func (r *NFSRuntime) CreateWorkspace(
	ctx context.Context,
	execID string,
	policy codeexecutor.WorkspacePolicy,
) (codeexecutor.Workspace, error) {
	if err := r.ensureReady(ctx); err != nil {
		return codeexecutor.Workspace{}, err
	}

	return r.createWorkspace(ctx, execID, policy)
}

func (r *NFSRuntime) createWorkspace(
	ctx context.Context,
	execID string,
	_ codeexecutor.WorkspacePolicy,
) (codeexecutor.Workspace, error) {
	_, span := atrace.Tracer.Start(ctx, codeexecutor.SpanWorkspaceCreate)
	span.SetAttributes(attribute.String(codeexecutor.AttrExecID, execID))
	defer span.End()

	wsID := r.generateWorkspaceID(execID)
	wsRelPath := wsID
	wsFullPath := path.Join(r.nfsClient().ExportPath(), wsID)

	if err := r.nfsClient().MkdirAll(wsRelPath); err != nil {
		span.SetStatus(codes.Error, err.Error())
		return codeexecutor.Workspace{}, errors.WithMessagef(err, "create workspace dir %s", wsRelPath)
	}

	for _, dir := range []string{codeexecutor.DirWork, codeexecutor.DirOut, codeexecutor.DirSkills, codeexecutor.DirRuns} {
		dirPath := path.Join(wsRelPath, dir)
		if err := r.nfsClient().MkdirAll(dirPath); err != nil {
			span.SetStatus(codes.Error, err.Error())
			return codeexecutor.Workspace{}, errors.WithMessagef(err, "create %s dir", dir)
		}
	}

	ws := codeexecutor.Workspace{
		ID:   wsID,
		Path: wsFullPath,
	}

	// Provision a per-session Linux user/group, bind the workspace
	// to that group, and tighten the mode so cross-session processes
	// (running with a different per-session gid, "other" relative to
	// this workspace) cannot even traverse in. The workspace's owner
	// stays the sandbox default user so the SDK side keeps full
	// read/write access for staging files, metadata writes, and output
	// collection. The setgid bit on the directories ensures every
	// file/dir created inside (whether by the SDK over NFS or by bash
	// inside the sandbox) inherits the group without further
	// bookkeeping. Creating the user in /etc/passwd is what makes
	// `whoami`, `id -un`, and `getpwuid()` succeed inside the bash
	// session — without an entry, anything that looks up the uid emits
	// "cannot find name for user ID NNN" noise.
	//
	// Skipped when r.executor is nil (test-only fake runtime) or when the
	// caller has explicitly opted out via WithSessionIsolation(false).
	if r.executor != nil && r.executor.sessionIsolation {
		uid := sessionuid.Allocate(wsID)
		name := sessionuid.Username(wsID)
		if err := r.applySessionIdentity(ctx, wsFullPath, uid, name); err != nil {
			// Best-effort cleanup of the partial workspace on NFS so we don't
			// leak a dangling directory. Log and propagate the original error
			// regardless of cleanup outcome.
			if rmErr := r.nfsClient().RemoveAll(wsRelPath); rmErr != nil {
				log.WarnfContext(ctx, "cleanup partial workspace %s after identity setup failure: %v", wsRelPath, rmErr)
			}
			span.SetStatus(codes.Error, err.Error())
			return codeexecutor.Workspace{}, errors.WithMessagef(err, "apply session identity name=%s uid=%d", name, uid)
		}
		log.DebugfContext(ctx, "Bind workspace %s to session %s (uid=gid=%d, mode 2770)", ws.Path, name, uid)
	}

	log.DebugfContext(ctx, "Create workspace %s with path %s", ws.ID, ws.Path)
	return ws, nil
}

// applySessionIdentity provisions the per-session Linux user/group and
// binds the workspace to it: ensure user/group exist, chgrp -R the
// workspace, and set mode 2770 + setgid bit on every directory. The
// Python that actually runs in the sandbox lives in
// session_identity_wrapper.go; this method just dispatches it via
// runCodeBlock and converts the result into a Go error.
func (r *NFSRuntime) applySessionIdentity(
	ctx context.Context,
	wsFullPath string,
	uid uint32,
	name string,
) error {
	pythonCode := generateSessionIdentityScript(sessionIdentityParams{
		WorkspacePath: wsFullPath,
		UID:           uid,
		Username:      name,
	})

	result, _, err := r.executor.runCodeBlock(ctx, codeexecutor.CodeBlock{Code: pythonCode, Language: "python"})
	if err != nil {
		return errors.WithMessage(err, "run session identity setup")
	}
	if result.ExitCode != 0 {
		return errors.Errorf("session identity setup exit code %d, stderr: %s", result.ExitCode, result.Stderr)
	}
	return nil
}

// Cleanup removes the workspace from NFS unless it's marked as persistent.
func (r *NFSRuntime) Cleanup(ctx context.Context, ws codeexecutor.Workspace) error {
	if err := r.ensureReady(ctx); err != nil {
		return err
	}

	return r.cleanup(ctx, ws)
}

func (r *NFSRuntime) cleanup(ctx context.Context, ws codeexecutor.Workspace) error {
	_, span := atrace.Tracer.Start(ctx, codeexecutor.SpanWorkspaceCleanup)
	span.SetAttributes(attribute.String(codeexecutor.AttrPath, ws.Path))
	defer span.End()

	if err := r.nfsClient().RemoveAll(ws.Path); err != nil {
		span.SetStatus(codes.Error, err.Error())
		return errors.WithMessagef(err, "remove workspace %s", ws.Path)
	}

	log.DebugfContext(ctx, "Cleanup workspace %s with path %s", ws.ID, ws.Path)
	return nil
}

// PutFiles writes files to the workspace on NFS.
func (r *NFSRuntime) PutFiles(
	ctx context.Context,
	ws codeexecutor.Workspace,
	files []codeexecutor.PutFile,
) error {
	if err := r.ensureReady(ctx); err != nil {
		return err
	}
	return r.putFiles(ctx, ws, files)
}

func (r *NFSRuntime) putFiles(
	ctx context.Context,
	ws codeexecutor.Workspace,
	files []codeexecutor.PutFile,
) error {
	_, span := atrace.Tracer.Start(ctx, codeexecutor.SpanWorkspaceStageFiles)
	span.SetAttributes(attribute.Int(codeexecutor.AttrCount, len(files)))
	defer span.End()

	for _, f := range files {
		if err := r.writeFileSafe(ws.Path, f); err != nil {
			span.SetStatus(codes.Error, err.Error())
			return err
		}
	}

	log.DebugfContext(ctx, "Put %v files to workspace %s", len(files), ws.Path)
	return nil
}

func (r *NFSRuntime) writeFileSafe(root string, f codeexecutor.PutFile) error {
	if f.Path == "" {
		return errors.New("empty file path")
	}

	dst, err := ensurePathInsideRoot(root, f.Path)
	if err != nil {
		return err
	}

	if err := r.nfsClient().WriteFile(dst, f.Content, defaultFileMode); err != nil {
		return errors.WithMessagef(err, "nfs client write file %s", f.Path)
	}

	return nil
}

// StageDirectory stages a directory from local filesystem into the workspace.
// Behavior depends on options, e.g., marking the tree as read-only in metadata.
func (r *NFSRuntime) StageDirectory(
	ctx context.Context,
	ws codeexecutor.Workspace,
	src string,
	to string,
	options codeexecutor.StageOptions,
) error {
	if err := r.ensureReady(ctx); err != nil {
		return err
	}

	return r.stageDirectory(ctx, ws, src, to, options)
}

func (r *NFSRuntime) stageDirectory(
	ctx context.Context,
	ws codeexecutor.Workspace,
	src string,
	to string,
	_ codeexecutor.StageOptions,
) error {
	_, span := atrace.Tracer.Start(ctx, codeexecutor.SpanWorkspaceStageDir)
	span.SetAttributes(
		attribute.String(codeexecutor.AttrHostPath, src),
		attribute.String(codeexecutor.AttrTo, to),
	)
	defer span.End()

	if src == "" {
		err := errors.New("hostPath is empty")
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	src, err := filepath.Abs(src)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	dst, err := ensurePathInsideRoot(ws.Path, to)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	if err := r.copyDirToNFS(src, dst); err != nil {
		span.SetStatus(codes.Error, err.Error())
		return errors.WithMessagef(err, "copy dir to nfs %s", dst)
	}

	log.DebugfContext(ctx, "Stage directory from host %s to nfs  %s", src, dst)
	return nil
}

// recursively copies a directory from local filesystem to NFS.
func (r *NFSRuntime) copyDirToNFS(src, dst string) error {
	if err := r.nfsClient().MkdirAll(dst); err != nil {
		return err
	}

	return filepath.WalkDir(src, func(localPath string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, localPath)
		if err != nil {
			return err
		}

		nfsPath := path.Join(dst, filepath.ToSlash(rel))

		if d.IsDir() {
			return r.nfsClient().MkdirAll(nfsPath)
		}

		data, err := os.ReadFile(localPath)
		if err != nil {
			return err
		}

		return r.nfsClient().WriteFile(nfsPath, data, defaultFileMode)
	})
}

// Collect gathers files from workspace matching glob patterns.
func (r *NFSRuntime) Collect(
	ctx context.Context,
	ws codeexecutor.Workspace,
	patterns []string,
) ([]codeexecutor.File, error) {
	if err := r.ensureReady(ctx); err != nil {
		return nil, err
	}

	return r.collect(ctx, ws, patterns)
}

func (r *NFSRuntime) collect(
	ctx context.Context,
	ws codeexecutor.Workspace,
	patterns []string,
) ([]codeexecutor.File, error) {
	_, span := atrace.Tracer.Start(ctx, codeexecutor.SpanWorkspaceCollect)
	span.SetAttributes(attribute.Int(codeexecutor.AttrPatterns, len(patterns)))
	defer span.End()

	if len(patterns) == 0 {
		return nil, nil
	}

	normalizedPatterns := codeexecutor.NormalizeGlobs(patterns)
	seen := map[string]bool{}
	var files []codeexecutor.File

	for _, pattern := range normalizedPatterns {
		fullPattern := path.Join(ws.Path, pattern)

		matches, err := r.nfsClient().Glob(fullPattern)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			return nil, errors.WithMessagef(err, "glob pattern %s", pattern)
		}

		for _, match := range matches {
			if !pathInsideRoot(match, ws.Path) {
				log.WarnfContext(ctx, "Skip file %s outside workspace %s", match, ws.Path)
				continue
			}

			relPath := strings.TrimPrefix(match, ws.Path+"/")
			if isRootMetadataTempPath(relPath) {
				continue
			}
			if seen[relPath] {
				continue
			}
			seen[relPath] = true

			info, err := r.nfsClient().Stat(match)
			if err != nil {
				span.SetStatus(codes.Error, err.Error())
				return nil, err
			}

			data, mime, err := r.readLimited(match, maxReadSizeBytes)
			if err != nil {
				span.SetStatus(codes.Error, err.Error())
				return nil, err
			}

			sizeBytes := info.Size()
			truncated := sizeBytes > int64(len(data))

			files = append(files, codeexecutor.File{
				Name:      relPath,
				Content:   string(data),
				MIMEType:  mime,
				SizeBytes: sizeBytes,
				Truncated: truncated,
			})
		}
	}

	span.SetAttributes(attribute.Int(codeexecutor.AttrCount, len(files)))
	log.DebugfContext(ctx, "Collect %v files by pattern %v", len(files), patterns)
	return files, nil
}

// StageInputs maps external inputs into the workspace.
func (r *NFSRuntime) StageInputs(
	ctx context.Context,
	ws codeexecutor.Workspace,
	specs []codeexecutor.InputSpec,
) error {
	if err := r.ensureReady(ctx); err != nil {
		return err
	}

	return r.stageInputs(ctx, ws, specs)
}

func (r *NFSRuntime) stageInputs(
	ctx context.Context,
	ws codeexecutor.Workspace,
	specs []codeexecutor.InputSpec,
) error {
	for _, sp := range specs {
		mode := strings.ToLower(strings.TrimSpace(sp.Mode))
		if mode == "" {
			mode = "copy"
		}
		to := normalizeInputTo(sp.To)
		if strings.TrimSpace(to) == "" {
			base := inputDefaultName(sp.From)
			to = path.Join(codeexecutor.DirWork, inputsDirName, base)
		}

		switch {
		case strings.HasPrefix(sp.From, inputSchemeArtifact):
			name := strings.TrimPrefix(sp.From, inputSchemeArtifact)
			aname, aver, perr := codeexecutor.ParseArtifactRef(name)
			if perr != nil {
				return perr
			}
			data, _, _, lerr := codeexecutor.LoadArtifactHelper(ctx, aname, aver)
			if lerr != nil {
				return lerr
			}

			if err := r.writeFileSafe(ws.Path, codeexecutor.PutFile{
				Path:    to,
				Content: data,
				Mode:    defaultFileMode,
			}); err != nil {
				return errors.WithMessagef(err, "write artifact to %s", to)
			}

		case strings.HasPrefix(sp.From, inputSchemeWorkspace):
			rel := strings.TrimPrefix(sp.From, inputSchemeWorkspace)
			srcPath := path.Join(ws.Path, rel)

			if err := r.copyPath(ws.Path, srcPath, to); err != nil {
				return errors.WithMessagef(err, "copy workspace path %s to %s", rel, to)
			}

		case strings.HasPrefix(sp.From, inputSchemeSkill):
			rest := strings.TrimPrefix(sp.From, inputSchemeSkill)
			srcPath := path.Join(ws.Path, codeexecutor.DirSkills, rest)

			if err := r.copyPath(ws.Path, srcPath, to); err != nil {
				return errors.WithMessagef(err, "copy skill path %s to %s", rest, to)
			}

		case strings.HasPrefix(sp.From, inputSchemeHost):
			if mode != "copy" {
				return fmt.Errorf("host:// inputs only support copy mode in pcg123 NFS executor, got %q", mode)
			}
			if err := r.stageHostInput(ws, sp.From, to); err != nil {
				return err
			}

		default:
			return fmt.Errorf("unsupported input: %s", sp.From)
		}
	}

	log.DebugfContext(ctx, "Stage inputs %v to workspace %s", specs, ws.Path)
	return nil
}

// copies a host-local file or directory into the NFS workspace.
func (r *NFSRuntime) stageHostInput(ws codeexecutor.Workspace, from, to string) error {
	hostPath := strings.TrimPrefix(from, inputSchemeHost)
	if hostPath == "" {
		return errors.New("host:// input path is empty")
	}

	src, err := filepath.Abs(hostPath)
	if err != nil {
		return errors.WithMessagef(err, "resolve host path %s", hostPath)
	}

	info, err := os.Stat(src)
	if err != nil {
		return errors.WithMessagef(err, "stat host path %s", src)
	}

	if info.IsDir() {
		dst, err := ensurePathInsideRoot(ws.Path, to)
		if err != nil {
			return err
		}
		return errors.WithMessagef(r.copyDirToNFS(src, dst), "copy host dir %s to workspace %s", src, dst)
	}

	data, err := os.ReadFile(src)
	if err != nil {
		return errors.WithMessagef(err, "read host file %s", src)
	}

	return errors.WithMessagef(r.writeFileSafe(ws.Path, codeexecutor.PutFile{
		Path:    to,
		Content: data,
		Mode:    defaultFileMode,
	}), "write host file to %s", to)
}

func normalizeInputTo(to string) string {
	s := strings.TrimSpace(to)
	s = strings.ReplaceAll(s, "\\", "/")
	if s == "" {
		return ""
	}

	cleaned := path.Clean(s)
	if cleaned == "." {
		return ""
	}
	if cleaned == inputsDirName {
		return ""
	}

	prefix := inputsDirName + "/"
	if strings.HasPrefix(cleaned, prefix) {
		rest := strings.TrimPrefix(cleaned, prefix)
		return path.Join(codeexecutor.DirWork, inputsDirName, rest)
	}
	return cleaned
}

// copies a file or directory within NFS.
func (r *NFSRuntime) copyPath(root, srcPath, dstPath string) error {
	info, err := r.nfsClient().Stat(srcPath)
	if err != nil {
		return err
	}

	if info.IsDir() {
		return r.copyDirectory(root, srcPath, dstPath, "")
	}

	data, err := r.nfsClient().ReadFile(srcPath)
	if err != nil {
		return err
	}

	return r.writeFileSafe(root, codeexecutor.PutFile{
		Path:    dstPath,
		Content: data,
		Mode:    defaultFileMode,
	})
}

// recursively copies a directory within NFS.
func (r *NFSRuntime) copyDirectory(root, srcDir, dstDir, relPath string) error {
	entries, err := r.nfsClient().ReadDir(srcDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		name := entry.Name()
		srcPath := path.Join(srcDir, name)
		dstRelPath := path.Join(relPath, name)
		dstPath := path.Join(dstDir, dstRelPath)

		if entry.IsDir() {
			if err := r.copyDirectory(root, srcPath, dstDir, dstRelPath); err != nil {
				return err
			}
		} else {
			data, err := r.nfsClient().ReadFile(srcPath)
			if err != nil {
				return err
			}

			if err := r.writeFileSafe(root, codeexecutor.PutFile{
				Path:    dstPath,
				Content: data,
				Mode:    defaultFileMode,
			}); err != nil {
				return err
			}
		}
	}

	return nil
}

// CollectOutputs implements declarative collector with limits.
func (r *NFSRuntime) CollectOutputs(
	ctx context.Context,
	ws codeexecutor.Workspace,
	spec codeexecutor.OutputSpec,
) (codeexecutor.OutputManifest, error) {
	if err := r.ensureReady(ctx); err != nil {
		return codeexecutor.OutputManifest{}, err
	}

	return r.collectOutputs(ctx, ws, spec)
}

func (r *NFSRuntime) collectOutputs(
	ctx context.Context,
	ws codeexecutor.Workspace,
	spec codeexecutor.OutputSpec,
) (codeexecutor.OutputManifest, error) {
	maxFiles := spec.MaxFiles
	if maxFiles <= 0 {
		maxFiles = maxFilesNum
	}
	maxFileBytes := spec.MaxFileBytes
	if maxFileBytes <= 0 {
		maxFileBytes = maxReadSizeBytes
	}
	maxTotal := spec.MaxTotalBytes
	if maxTotal <= 0 {
		maxTotal = maxTotalBytes
	}

	leftTotal := maxTotal
	globs := codeexecutor.NormalizeGlobs(spec.Globs)
	out := codeexecutor.OutputManifest{}
	var savedNames []string
	var savedVers []int
	count := 0

	for _, g := range globs {
		fullPattern := path.Join(ws.Path, g)
		matches, err := r.nfsClient().Glob(fullPattern)
		if err != nil {
			return codeexecutor.OutputManifest{}, err
		}

		for _, m := range matches {
			if count >= maxFiles {
				out.LimitsHit = true
				break
			}

			if !pathInsideRoot(m, ws.Path) {
				log.WarnfContext(ctx, "Skip file %s outside workspace %s", m, ws.Path)
				continue
			}

			relPath := strings.TrimPrefix(m, ws.Path+"/")
			if isRootMetadataTempPath(relPath) {
				continue
			}

			info, err := r.nfsClient().Stat(m)
			if err != nil {
				return codeexecutor.OutputManifest{}, err
			}

			if info.IsDir() {
				continue
			}

			limit := int(maxFileBytes)
			if int64(limit) > leftTotal {
				limit = int(leftTotal)
			}

			data, mime, err := r.readLimited(m, limit)
			if err != nil {
				return codeexecutor.OutputManifest{}, err
			}

			if int64(len(data)) >= maxFileBytes {
				out.LimitsHit = true
			}

			leftTotal -= int64(len(data))
			count++

			ref := codeexecutor.FileRef{
				Name:      relPath,
				MIMEType:  mime,
				SizeBytes: info.Size(),
				Truncated: info.Size() > int64(len(data)),
			}

			if spec.Inline {
				ref.Content = string(data)
			}
			if spec.Save {
				saveName := relPath
				if spec.NameTemplate != "" {
					saveName = spec.NameTemplate + relPath
				}
				ver, err := codeexecutor.SaveArtifactHelper(ctx, saveName, data, mime)
				if err != nil {
					return codeexecutor.OutputManifest{}, err
				}
				ref.SavedAs = saveName
				ref.Version = ver
				savedNames = append(savedNames, saveName)
				savedVers = append(savedVers, ver)
			}

			out.Files = append(out.Files, ref)

			if leftTotal <= 0 {
				out.LimitsHit = true
				break
			}
		}
	}

	log.DebugfContext(ctx, "Collect outputs %v from workspace %s", out, ws.Path)
	return out, nil
}

func isRootMetadataTempPath(rel string) bool {
	rel = strings.TrimSpace(rel)
	if strings.Contains(rel, "/") {
		return false
	}
	if rel == metadataTmpFileName {
		return true
	}
	if !strings.HasPrefix(rel, metadataTmpPrefix) {
		return false
	}
	if !strings.HasSuffix(rel, metadataTmpSuffix) {
		return false
	}

	token := strings.TrimPrefix(rel, metadataTmpPrefix)
	token = strings.TrimSuffix(token, metadataTmpSuffix)
	return isGeneratedMetadataTempToken(token)
}

func isGeneratedMetadataTempToken(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != metadataTmpPartCount {
		return false
	}
	return isUnsignedDecimal(parts[metadataTmpPIDIndex]) &&
		isUnsignedDecimal(parts[metadataTmpTimeIndex]) &&
		isUnsignedDecimal(parts[metadataTmpCounterIndex]) &&
		isMetadataRandomSuffix(parts[metadataTmpRandomIndex])
}

func isUnsignedDecimal(s string) bool {
	if s == "" {
		return false
	}
	n, err := strconv.ParseUint(s, 10, 64)
	return err == nil && n != 0
}

func isMetadataRandomSuffix(s string) bool {
	if s == metadataNoRandomSuffix {
		return true
	}
	if len(s) != metadataRandomHexLen {
		return false
	}
	for _, ch := range s {
		if !isLowerHex(ch) {
			return false
		}
	}
	return true
}

func isLowerHex(ch rune) bool {
	return (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')
}

// RunProgram executes a program on remote 123 platform.
func (r *NFSRuntime) RunProgram(
	ctx context.Context,
	ws codeexecutor.Workspace,
	spec codeexecutor.RunProgramSpec,
) (codeexecutor.RunResult, error) {
	if err := r.ensureReady(ctx); err != nil {
		return codeexecutor.RunResult{}, err
	}

	return r.runProgram(ctx, ws, spec)
}

func (r *NFSRuntime) runProgram(
	ctx context.Context,
	ws codeexecutor.Workspace,
	spec codeexecutor.RunProgramSpec,
) (codeexecutor.RunResult, error) {
	_, span := atrace.Tracer.Start(ctx, codeexecutor.SpanWorkspaceRun)
	span.SetAttributes(
		attribute.String(codeexecutor.AttrCmd, spec.Cmd),
		attribute.String(codeexecutor.AttrCwd, spec.Cwd),
		attribute.StringSlice(attrArgs, spec.Args),
		attribute.StringSlice(attrEnv, envSlice(spec.Env)),
	)
	defer span.End()

	var empty codeexecutor.RunResult

	command := buildCommand(spec)
	workDir := spec.Cwd
	if workDir != "" {
		workDir = filepath.Clean(workDir)
	}

	// Derive per-session uid + gid if isolation is on. Skipped when
	// r.executor is nil (test-only fake runtime). uid and gid share the
	// same value — both are deterministic functions of the workspace ID,
	// and the per-session range we draw from is well above any real
	// system uid/gid so collisions with existing users/groups are
	// impossible.
	var runAsUID, runAsGID *uint32
	if r.executor != nil && r.executor.sessionIsolation {
		id := sessionuid.Allocate(ws.ID)
		runAsUID = &id
		runAsGID = &id
	}

	pythonCode := generateBashExecScript(bashExecParams{
		WorkspacePath: ws.Path,
		Command:       command,
		WorkDir:       workDir,
		Stdin:         spec.Stdin,
		TimeoutSec:    int(spec.Timeout.Seconds()),
		Env:           spec.Env,
		RunAsUID:      runAsUID,
		RunAsGID:      runAsGID,
	})

	log.DebugfContext(ctx, "Run program %s in workspace %s", command, ws.Path)
	execResult, _, err := r.executor.runCodeBlock(ctx, codeexecutor.CodeBlock{Code: pythonCode, Language: "python"})
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return empty, errors.WithMessage(err, "execute python wrapper")
	}

	span.SetAttributes(
		attribute.Int64(attrCost, execResult.Duration.Milliseconds()),
		attribute.Int(codeexecutor.AttrExitCode, execResult.ExitCode),
		attribute.Bool(codeexecutor.AttrTimedOut, execResult.TimedOut),
	)

	return execResult, nil
}

func (r *NFSRuntime) generateWorkspaceID(execID string) string {
	if execID != "" {
		sanitized := sanitizeExecID(execID)
		return fmt.Sprintf("ws_%s", sanitized)
	}
	return fmt.Sprintf("ws_%s", uuid.New().String()[:8])
}

func sanitizeExecID(execID string) string {
	return strings.ReplaceAll(execID, "/", "_")
}

func envSlice(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	envs := make([]string, 0, len(env))
	for k, v := range env {
		envs = append(envs, fmt.Sprintf("%s=%s", k, v))
	}
	return envs
}

func buildCommand(spec codeexecutor.RunProgramSpec) string {
	if spec.Cmd != "" {
		if len(spec.Args) == 0 {
			return spec.Cmd
		}
		var sb strings.Builder
		sb.WriteString(spec.Cmd)
		for _, arg := range spec.Args {
			sb.WriteString(" ")
			if strings.ContainsAny(arg, " \t\n'\"\\$`") {
				sb.WriteString("'")
				sb.WriteString(strings.ReplaceAll(arg, "'", "'\"'\"'"))
				sb.WriteString("'")
			} else {
				sb.WriteString(arg)
			}
		}
		return sb.String()
	}

	if len(spec.Args) == 0 {
		return ""
	}

	var sb strings.Builder
	for i, part := range spec.Args {
		if i > 0 {
			sb.WriteString(" ")
		}
		if strings.ContainsAny(part, " \t\n'\"\\$`") {
			sb.WriteString("'")
			sb.WriteString(strings.ReplaceAll(part, "'", "'\"'\"'"))
			sb.WriteString("'")
		} else {
			sb.WriteString(part)
		}
	}
	return sb.String()
}

func inputDefaultName(from string) string {
	s := strings.TrimSpace(from)
	if strings.HasPrefix(s, inputSchemeArtifact) {
		rest := strings.TrimPrefix(s, inputSchemeArtifact)
		name, _, err := codeexecutor.ParseArtifactRef(rest)
		if err == nil {
			base := path.Base(strings.TrimSpace(name))
			if base != "." && base != "/" && base != ".." && base != "" {
				return base
			}
		}
	}

	i := strings.LastIndex(s, "/")
	if i >= 0 && i+1 < len(s) {
		return s[i+1:]
	}
	return s
}

func ensurePathInsideRoot(root, relPath string) (string, error) {
	dst := path.Join(root, relPath)
	if !pathInsideRoot(dst, root) {
		return "", fmt.Errorf("path escapes nfs workspace: %s", relPath)
	}
	return path.Clean(dst), nil
}

func pathInsideRoot(filePath, root string) bool {
	cleanPath := path.Clean(filePath)
	cleanRoot := path.Clean(root)

	if cleanPath == cleanRoot {
		return true
	}

	return strings.HasPrefix(cleanPath, cleanRoot+"/")
}

func (r *NFSRuntime) readLimited(filePath string, limitBytes int) ([]byte, string, error) {
	if limitBytes <= 0 {
		return []byte{}, "", nil
	}

	if limitBytes > maxReadSizeBytes {
		limitBytes = maxReadSizeBytes
	}

	data, err := r.nfsClient().ReadFileLimited(filePath, limitBytes)
	if err != nil {
		return nil, "", err
	}

	mime := http.DetectContentType(data)
	return data, mime, nil
}
