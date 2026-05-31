package agent

import (
	"context"
	"os"
	"strings"
	"testing"
)

const testFilePerm = 0644

func TestHandleFileOperationReadsFileWithinWorkingDirectory(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	if err := os.WriteFile(
		"note.txt",
		[]byte("hello"),
		testFilePerm,
	); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	resp, err := handleFileOperation(
		context.Background(),
		fileRequest{Path: "note.txt", Operation: "read"},
	)
	if err != nil {
		t.Fatalf("handle file operation: %v", err)
	}
	if !resp.Success {
		t.Fatalf("unexpected failure: %s", resp.Message)
	}
	if !strings.Contains(resp.Result, "hello") {
		t.Fatalf("unexpected result: %q", resp.Result)
	}
}

func TestHandleFileOperationRejectsParentTraversal(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	resp, err := handleFileOperation(
		context.Background(),
		fileRequest{Path: "../secret.txt", Operation: "read"},
	)
	if err != nil {
		t.Fatalf("handle file operation: %v", err)
	}
	if resp.Success {
		t.Fatalf("expected traversal to be rejected")
	}
	if !strings.Contains(resp.Message, "Security error:") {
		t.Fatalf("unexpected message: %q", resp.Message)
	}
}
