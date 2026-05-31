package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
)

const testFilePerm = 0644

func TestReadFileContentReadsDataDirRelativeFile(t *testing.T) {
	tempDir := t.TempDir()
	dataFile := filepath.Join(tempDir, "note.txt")
	if err := os.WriteFile(dataFile, []byte("hello"), testFilePerm); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	previousDataDir := *dataDir
	*dataDir = tempDir
	t.Cleanup(func() {
		*dataDir = previousDataDir
	})

	resp, err := readFileContent(
		context.Background(),
		ReadFileRequest{FilePath: "note.txt"},
	)
	if err != nil {
		t.Fatalf("read file content: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("unexpected read error: %s", resp.Error)
	}
	if !strings.Contains(resp.Content, "hello") {
		t.Fatalf("unexpected content: %q", resp.Content)
	}
}

func TestReadFileContentRejectsPathTraversal(t *testing.T) {
	tempDir := t.TempDir()
	previousDataDir := *dataDir
	*dataDir = tempDir
	t.Cleanup(func() {
		*dataDir = previousDataDir
	})

	resp, err := readFileContent(
		context.Background(),
		ReadFileRequest{FilePath: "../secret.txt"},
	)
	if err != nil {
		t.Fatalf("read file content: %v", err)
	}
	if !strings.Contains(resp.Error, "path traversal is not allowed") {
		t.Fatalf("unexpected error: %q", resp.Error)
	}
}

func TestReadFileContentRejectsAbsolutePath(t *testing.T) {
	tempDir := t.TempDir()
	previousDataDir := *dataDir
	*dataDir = tempDir
	t.Cleanup(func() {
		*dataDir = previousDataDir
	})

	resp, err := readFileContent(
		context.Background(),
		ReadFileRequest{
			FilePath: filepath.Join(tempDir, "note.txt"),
		},
	)
	if err != nil {
		t.Fatalf("read file content: %v", err)
	}
	if !strings.Contains(resp.Error, "absolute paths are not allowed") {
		t.Fatalf("unexpected error: %q", resp.Error)
	}
}
