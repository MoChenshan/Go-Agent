package pcg123

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/artifact"
	"trpc.group/trpc-go/trpc-agent-go/artifact/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/codeexecutor"
)

const (
	testWorkspaceID     = "ws-test"
	testKeepFile        = "keep.txt"
	testKeepContent     = "keep"
	testNestedTempFile  = "work/.metadata.tmp"
	testNestedContent   = "nested"
	testUserTempFile    = ".metadata.report.tmp"
	testUserTempContent = "business"
	testMetadataTmpPID  = "12345"
	testMetadataTmpTime = "1710000000000000000"
	testMetadataTmpID   = "7"
	testMetadataTmpRand = "0123456789abcdef"
)

func TestIsRootMetadataTempPath(t *testing.T) {
	tests := []struct {
		name string
		rel  string
		want bool
	}{
		{
			name: "root fixed metadata temp",
			rel:  metadataTmpFileName,
			want: true,
		},
		{
			name: "root generated metadata temp",
			rel:  generatedMetadataTempName(),
			want: true,
		},
		{
			name: "root generated norand metadata temp",
			rel:  generatedNoRandomMetadataTempName(),
			want: true,
		},
		{
			name: "root metadata temp with spaces",
			rel:  " " + metadataTmpFileName + " ",
			want: true,
		},
		{
			name: "nested work file",
			rel:  "work/" + metadataTmpFileName,
		},
		{
			name: "nested output file",
			rel:  "out/" + generatedMetadataTempName(),
		},
		{
			name: "metadata file",
			rel:  "metadata.json",
		},
		{
			name: "root user file with metadata words",
			rel:  testUserTempFile,
		},
		{
			name: "non uuid generated-like file",
			rel:  ".metadata.123.tmp",
		},
		{
			name: "bad random suffix",
			rel:  ".metadata.1.2.3.nothex.tmp",
		},
		{
			name: "empty numeric segment",
			rel:  ".metadata..2.3.0123456789abcdef.tmp",
		},
		{
			name: "bad numeric segment",
			rel:  ".metadata.pid.2.3.0123456789abcdef.tmp",
		},
		{
			name: "zero numeric segment",
			rel:  ".metadata.0.2.3.0123456789abcdef.tmp",
		},
		{
			name: "bad hex random segment",
			rel:  ".metadata.1.2.3.0123456789abcdeg.tmp",
		},
		{
			name: "missing suffix",
			rel:  ".metadata.123",
		},
		{
			name: "missing prefix",
			rel:  "metadata.123.tmp",
		},
		{
			name: "absolute path is not root relative",
			rel:  "/workspace/.metadata.123.tmp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRootMetadataTempPath(tt.rel)
			if got != tt.want {
				t.Fatalf(
					"isRootMetadataTempPath(%q) = %v, want %v",
					tt.rel,
					got,
					tt.want,
				)
			}
		})
	}
}

func TestCollectSkipsOnlyRootMetadataTemp(t *testing.T) {
	ctx := context.Background()
	rt, ws, nfs := newFakeRuntime(t)
	writeFakeFile(t, nfs, ws, metadataTmpFileName, "fixed")
	writeFakeFile(t, nfs, ws, generatedMetadataTempName(), "generated")
	writeFakeFile(t, nfs, ws, testKeepFile, testKeepContent)
	writeFakeFile(t, nfs, ws, testNestedTempFile, testNestedContent)
	writeFakeFile(t, nfs, ws, testUserTempFile, testUserTempContent)

	files, err := rt.collect(ctx, ws, []string{"*", "work/*"})
	if err != nil {
		t.Fatalf("collect failed: %v", err)
	}

	want := []string{
		testUserTempFile,
		testKeepFile,
		testNestedTempFile,
	}
	if got := collectFileNames(files); !reflect.DeepEqual(got, want) {
		t.Fatalf("collect names = %v, want %v", got, want)
	}
	requireFileContent(t, files, testNestedTempFile, testNestedContent)
	requireFileContent(t, files, testUserTempFile, testUserTempContent)
}

func TestCollectOutputsSkipsRootMetadataTempForSaveAndLimits(t *testing.T) {
	ctx, svc, session := newArtifactContext(context.Background())
	rt, ws, nfs := newFakeRuntime(t)
	writeFakeFile(t, nfs, ws, metadataTmpFileName, "fixed")
	writeFakeFile(t, nfs, ws, generatedMetadataTempName(), "generated")
	writeFakeFile(t, nfs, ws, testKeepFile, testKeepContent)

	manifest, err := rt.collectOutputs(ctx, ws, codeexecutor.OutputSpec{
		Globs:    []string{"*"},
		MaxFiles: 1,
		Save:     true,
		Inline:   true,
	})
	if err != nil {
		t.Fatalf("collect outputs failed: %v", err)
	}

	want := []string{testKeepFile}
	if got := outputFileNames(manifest.Files); !reflect.DeepEqual(got, want) {
		t.Fatalf("output names = %v, want %v", got, want)
	}
	if manifest.Files[0].Content != testKeepContent {
		t.Fatalf("output content = %q, want %q",
			manifest.Files[0].Content, testKeepContent)
	}
	if manifest.LimitsHit {
		t.Fatalf("limits hit = true, want false")
	}

	keys, err := svc.ListArtifactKeys(ctx, session)
	if err != nil {
		t.Fatalf("list artifact keys failed: %v", err)
	}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("saved artifact keys = %v, want %v", keys, want)
	}
}

func TestCollectOutputsRetainsNestedMetadataTemp(t *testing.T) {
	ctx, svc, session := newArtifactContext(context.Background())
	rt, ws, nfs := newFakeRuntime(t)
	writeFakeFile(t, nfs, ws, metadataTmpFileName, "fixed")
	writeFakeFile(t, nfs, ws, testNestedTempFile, testNestedContent)

	manifest, err := rt.collectOutputs(ctx, ws, codeexecutor.OutputSpec{
		Globs:  []string{"work/*"},
		Save:   true,
		Inline: true,
	})
	if err != nil {
		t.Fatalf("collect outputs failed: %v", err)
	}

	want := []string{testNestedTempFile}
	if got := outputFileNames(manifest.Files); !reflect.DeepEqual(got, want) {
		t.Fatalf("output names = %v, want %v", got, want)
	}
	if manifest.Files[0].Content != testNestedContent {
		t.Fatalf("output content = %q, want %q",
			manifest.Files[0].Content, testNestedContent)
	}

	keys, err := svc.ListArtifactKeys(ctx, session)
	if err != nil {
		t.Fatalf("list artifact keys failed: %v", err)
	}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("saved artifact keys = %v, want %v", keys, want)
	}
}

type fakeNFSClient struct {
	exportPath string
}

func newFakeRuntime(
	t *testing.T,
) (*NFSRuntime, codeexecutor.Workspace, *fakeNFSClient) {
	t.Helper()

	nfs := &fakeNFSClient{exportPath: filepath.ToSlash(t.TempDir())}
	rt := &NFSRuntime{
		nfsClient: func() nfsClient { return nfs },
		ensureReady: func(context.Context) error {
			return nil
		},
	}
	ws := codeexecutor.Workspace{
		ID:   testWorkspaceID,
		Path: path.Join(nfs.ExportPath(), testWorkspaceID),
	}
	if err := nfs.MkdirAll(ws.Path); err != nil {
		t.Fatalf("create fake workspace failed: %v", err)
	}
	return rt, ws, nfs
}

func newArtifactContext(
	ctx context.Context,
) (context.Context, *inmemory.Service, artifact.SessionInfo) {
	svc := inmemory.NewService()
	session := artifact.SessionInfo{
		AppName:   "app",
		UserID:    "user",
		SessionID: "session",
	}
	ctx = codeexecutor.WithArtifactService(ctx, svc)
	ctx = codeexecutor.WithArtifactSession(ctx, session)
	return ctx, svc, session
}

func writeFakeFile(
	t *testing.T,
	nfs *fakeNFSClient,
	ws codeexecutor.Workspace,
	rel string,
	content string,
) {
	t.Helper()

	filePath := path.Join(ws.Path, rel)
	err := nfs.WriteFile(filePath, []byte(content), defaultFileMode)
	if err != nil {
		t.Fatalf("write fake file %s failed: %v", rel, err)
	}
}

func collectFileNames(files []codeexecutor.File) []string {
	names := make([]string, 0, len(files))
	for _, file := range files {
		names = append(names, file.Name)
	}
	sort.Strings(names)
	return names
}

func outputFileNames(files []codeexecutor.FileRef) []string {
	names := make([]string, 0, len(files))
	for _, file := range files {
		names = append(names, file.Name)
	}
	sort.Strings(names)
	return names
}

func requireFileContent(
	t *testing.T,
	files []codeexecutor.File,
	name string,
	want string,
) {
	t.Helper()

	for _, file := range files {
		if file.Name == name {
			if file.Content != want {
				t.Fatalf("file %s content = %q, want %q",
					name, file.Content, want)
			}
			return
		}
	}
	t.Fatalf("file %s not found in %v", name, collectFileNames(files))
}

func generatedMetadataTempName() string {
	return metadataTmpPrefix +
		testMetadataTmpPID + "." +
		testMetadataTmpTime + "." +
		testMetadataTmpID + "." +
		testMetadataTmpRand +
		metadataTmpSuffix
}

func generatedNoRandomMetadataTempName() string {
	return metadataTmpPrefix +
		testMetadataTmpPID + "." +
		testMetadataTmpTime + "." +
		testMetadataTmpID + "." +
		metadataNoRandomSuffix +
		metadataTmpSuffix
}

func (c *fakeNFSClient) ExportPath() string {
	return c.exportPath
}

func (c *fakeNFSClient) MkdirAll(dirPath string) error {
	return os.MkdirAll(filepath.FromSlash(dirPath), 0o755)
}

func (c *fakeNFSClient) WriteFile(
	filePath string,
	data []byte,
	perm os.FileMode,
) error {
	localPath := filepath.FromSlash(filePath)
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(localPath, data, perm)
}

func (c *fakeNFSClient) ReadFile(filePath string) ([]byte, error) {
	return os.ReadFile(filepath.FromSlash(filePath))
}

func (c *fakeNFSClient) ReadFileLimited(
	filePath string,
	maxBytes int,
) ([]byte, error) {
	if maxBytes <= 0 {
		return []byte{}, nil
	}

	data, err := c.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	if len(data) > maxBytes {
		return data[:maxBytes], nil
	}
	return data, nil
}

func (c *fakeNFSClient) RemoveAll(dirPath string) error {
	return os.RemoveAll(filepath.FromSlash(dirPath))
}

func (c *fakeNFSClient) Stat(filePath string) (os.FileInfo, error) {
	return os.Stat(filepath.FromSlash(filePath))
}

func (c *fakeNFSClient) ReadDir(dirPath string) ([]os.FileInfo, error) {
	entries, err := os.ReadDir(filepath.FromSlash(dirPath))
	if err != nil {
		return nil, err
	}

	infos := make([]os.FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		infos = append(infos, info)
	}
	return infos, nil
}

func (c *fakeNFSClient) Glob(pattern string) ([]string, error) {
	matches, err := filepath.Glob(filepath.FromSlash(pattern))
	if err != nil {
		return nil, err
	}

	files := make([]string, 0, len(matches))
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			continue
		}
		files = append(files, filepath.ToSlash(match))
	}
	sort.Strings(files)
	return files, nil
}
