package rag

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestStore(t *testing.T, maxBytes int64) *ContentStore {
	t.Helper()
	store, err := NewContentStore(t.TempDir(), maxBytes)
	if err != nil {
		t.Fatalf("NewContentStore: %v", err)
	}
	return store
}

func TestContentStoreCreateAndRead(t *testing.T) {
	store := newTestStore(t, 0)

	source, err := store.Create(7, "安装指南", "# 安装\n\n正文")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.HasPrefix(source, ManagedDir+"/7/") {
		t.Fatalf("source %q is not under the managed directory", source)
	}
	if !store.Managed(source) {
		t.Fatalf("Managed(%q) = false, want true", source)
	}

	content, err := store.Read(source)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if content != "# 安装\n\n正文" {
		t.Fatalf("Read returned %q", content)
	}
	if !store.Exists(source) {
		t.Fatal("Exists = false after Create")
	}
}

// 标题是调用方可控的输入，不能被拼成逃出 root 的路径。
func TestContentStoreSlugifiesTitle(t *testing.T) {
	store := newTestStore(t, 0)

	source, err := store.Create(1, "../../../etc/passwd", "x")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if strings.Contains(source, "..") {
		t.Fatalf("source %q contains a traversal segment", source)
	}
	if !store.Managed(source) {
		t.Fatalf("Managed(%q) = false, want true", source)
	}
}

func TestContentStoreRejectsEscape(t *testing.T) {
	store := newTestStore(t, 0)
	secret := filepath.Join(filepath.Dir(store.Root()), "outside.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	if _, err := store.Read("../outside.txt"); !errors.Is(err, ErrContentOutsideRoot) {
		t.Fatalf("Read outside root: err = %v, want ErrContentOutsideRoot", err)
	}
	if _, err := store.Read(secret); !errors.Is(err, ErrContentOutsideRoot) {
		t.Fatalf("Read absolute outside path: err = %v, want ErrContentOutsideRoot", err)
	}
}

// 软链是绕过前缀比较的经典手法：校验必须发生在解析符号链接之后。
func TestContentStoreRejectsSymlinkEscape(t *testing.T) {
	store := newTestStore(t, 0)
	outside := filepath.Join(filepath.Dir(store.Root()), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	link := filepath.Join(store.Root(), ManagedDir, "link.md")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatalf("mkdir managed dir: %v", err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	if _, err := store.Read(ManagedDir + "/link.md"); !errors.Is(err, ErrContentOutsideRoot) {
		t.Fatalf("Read symlink: err = %v, want ErrContentOutsideRoot", err)
	}
}

func TestContentStoreWriteOnlyManaged(t *testing.T) {
	store := newTestStore(t, 0)

	registered := filepath.Join(store.Root(), "handwritten.md")
	if err := os.WriteFile(registered, []byte("original"), 0o644); err != nil {
		t.Fatalf("write registered file: %v", err)
	}
	if store.Managed("handwritten.md") {
		t.Fatal("Managed reported an externally registered file as managed")
	}
	if err := store.Write("handwritten.md", "hijacked"); !errors.Is(err, ErrContentNotManaged) {
		t.Fatalf("Write registered file: err = %v, want ErrContentNotManaged", err)
	}

	content, err := os.ReadFile(registered)
	if err != nil {
		t.Fatalf("read registered file: %v", err)
	}
	if string(content) != "original" {
		t.Fatalf("registered file was modified: %q", content)
	}

	// 托管文件可以覆盖，这是 update 改正文的前提。
	source, err := store.Create(1, "doc", "v1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Write(source, "v2"); err != nil {
		t.Fatalf("Write managed file: %v", err)
	}
	if got, err := store.Read(source); err != nil || got != "v2" {
		t.Fatalf("Read after Write = %q, %v", got, err)
	}
}

func TestContentStoreRemoveKeepsRegisteredFiles(t *testing.T) {
	store := newTestStore(t, 0)

	registered := filepath.Join(store.Root(), "handwritten.md")
	if err := os.WriteFile(registered, []byte("keep me"), 0o644); err != nil {
		t.Fatalf("write registered file: %v", err)
	}
	if err := store.Remove("handwritten.md"); err != nil {
		t.Fatalf("Remove registered file: %v", err)
	}
	if !store.Exists("handwritten.md") {
		t.Fatal("Remove deleted an externally registered file")
	}

	source, err := store.Create(1, "doc", "v1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Remove(source); err != nil {
		t.Fatalf("Remove managed file: %v", err)
	}
	if store.Exists(source) {
		t.Fatal("Remove kept a managed file")
	}
	// 幂等：删两次不该报错，删除流程重试时会走到这里。
	if err := store.Remove(source); err != nil {
		t.Fatalf("Remove missing managed file: %v", err)
	}
}

func TestContentStoreEnforcesMaxBytes(t *testing.T) {
	store := newTestStore(t, 4)

	if _, err := store.Create(1, "doc", "12345"); !errors.Is(err, ErrContentTooLarge) {
		t.Fatalf("Create oversize: err = %v, want ErrContentTooLarge", err)
	}

	source, err := store.Create(1, "doc", "1234")
	if err != nil {
		t.Fatalf("Create at limit: %v", err)
	}
	if err := store.Write(source, "12345"); !errors.Is(err, ErrContentTooLarge) {
		t.Fatalf("Write oversize: err = %v, want ErrContentTooLarge", err)
	}
}

func TestContentStoreReadMissing(t *testing.T) {
	store := newTestStore(t, 0)

	if _, err := store.Read(ManagedDir + "/1/nope.md"); !errors.Is(err, ErrContentNotFound) {
		t.Fatalf("Read missing: err = %v, want ErrContentNotFound", err)
	}
	if _, err := store.Read("  "); !errors.Is(err, ErrContentNotFound) {
		t.Fatalf("Read empty source: err = %v, want ErrContentNotFound", err)
	}
}
