package knowledge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLoaderLoadsSupportedDocumentsWithinRoot(t *testing.T) {
	root := newKnowledgeTestDirectory(t, "loader-root")
	outside := newKnowledgeTestDirectory(t, "loader-outside")
	writeKnowledgeTestFile(t, filepath.Join(root, "guide.md"), "# Guide\nHello\n")
	writeKnowledgeTestFile(t, filepath.Join(root, "nested", "notes.TXT"), "Notes\n")
	writeKnowledgeTestFile(t, filepath.Join(root, "ignored.json"), `{"ignored":true}`)
	writeKnowledgeTestFile(t, filepath.Join(outside, "secret.md"), "outside\n")

	if err := os.Symlink(
		filepath.Join(outside, "secret.md"),
		filepath.Join(root, "linked-file.md"),
	); err != nil {
		t.Fatalf("create file symlink: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked-directory")); err != nil {
		t.Fatalf("create directory symlink: %v", err)
	}

	loader, err := NewLoader(LoaderConfig{
		Root:             root,
		MaxDocumentBytes: 64,
	})
	if err != nil {
		t.Fatalf("NewLoader() error = %v", err)
	}
	documents, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := []LoadedDocument{
		{Source: "guide.md", Title: "guide", Content: "# Guide\nHello\n"},
		{Source: "nested/notes.TXT", Title: "notes", Content: "Notes\n"},
	}
	if !reflect.DeepEqual(documents, want) {
		t.Errorf("Load() = %#v, want %#v", documents, want)
	}
}

func TestLoaderRejectsOversizedAndNonUTF8Documents(t *testing.T) {
	for name, content := range map[string][]byte{
		"oversized": {'1', '2', '3', '4', '5'},
		"non UTF-8": {0xff},
	} {
		t.Run(name, func(t *testing.T) {
			root := newKnowledgeTestDirectory(t, "loader-invalid")
			path := filepath.Join(root, "invalid.txt")
			if err := os.WriteFile(path, content, 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}

			loader, err := NewLoader(LoaderConfig{
				Root:             root,
				MaxDocumentBytes: 4,
			})
			if err != nil {
				t.Fatalf("NewLoader() error = %v", err)
			}
			_, err = loader.Load(context.Background())
			if err == nil {
				t.Fatal("Load() error = nil, want validation error")
			}
			if name == "oversized" && !strings.Contains(err.Error(), "exceeds") {
				t.Errorf("Load() error = %v, want size error", err)
			}
			if name == "non UTF-8" && !strings.Contains(err.Error(), "UTF-8") {
				t.Errorf("Load() error = %v, want UTF-8 error", err)
			}
		})
	}
}

func TestLoaderValidatesConfigurationAndContext(t *testing.T) {
	root := newKnowledgeTestDirectory(t, "loader-config")
	file := filepath.Join(root, "not-a-directory")
	writeKnowledgeTestFile(t, file, "content")

	for name, config := range map[string]LoaderConfig{
		"empty root": {Root: "", MaxDocumentBytes: 1},
		"zero limit": {Root: root, MaxDocumentBytes: 0},
		"file root":  {Root: file, MaxDocumentBytes: 1},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewLoader(config); err == nil {
				t.Error("NewLoader() error = nil, want validation error")
			}
		})
	}

	writeKnowledgeTestFile(t, filepath.Join(root, "guide.md"), "content")
	loader, err := NewLoader(LoaderConfig{Root: root, MaxDocumentBytes: 64})
	if err != nil {
		t.Fatalf("NewLoader() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = loader.Load(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Load() error = %v, want context.Canceled", err)
	}
}

func newKnowledgeTestDirectory(t *testing.T, prefix string) string {
	t.Helper()
	path := filepath.Join(
		".",
		"."+prefix+"-"+strconv.FormatInt(time.Now().UnixNano(), 36),
	)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("create test directory %q: %v", path, err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(path); err != nil {
			t.Errorf("remove test directory %q: %v", path, err)
		}
	})
	return path
}

func writeKnowledgeTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create fixture directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture %q: %v", path, err)
	}
}
