package builtin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFileSystemRejectsPathsOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, "escape")); err != nil {
		t.Fatal(err)
	}

	fs := newTestFileSystem(t, workspace)
	for _, path := range []string{"../secret.txt", "escape/secret.txt"} {
		if _, err := fs.readFile(context.Background(), readFileInput{Path: path}); err == nil {
			t.Fatalf("read %q unexpectedly succeeded", path)
		}
	}
	if _, err := fs.writeFile(context.Background(), writeFileInput{Path: "escape/new.txt", Content: "unsafe"}); err == nil {
		t.Fatal("write through symlink unexpectedly succeeded")
	}
}

func TestFileSystemReadsAndWritesWithinWorkspace(t *testing.T) {
	workspace := t.TempDir()
	fs := newTestFileSystem(t, workspace)
	if _, err := fs.writeFile(context.Background(), writeFileInput{Path: "nested/file.txt", Content: "safe"}); err != nil {
		t.Fatal(err)
	}
	content, err := fs.readFile(context.Background(), readFileInput{Path: "nested/file.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if content != "safe" {
		t.Fatalf("content = %q, want safe", content)
	}
}

func newTestFileSystem(t *testing.T, root string) *FileSystem {
	t.Helper()
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return &FileSystem{Root: resolvedRoot, MaxReadBytes: 1024}
}
