package rag

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestReader(t *testing.T, maxBytes int64) *LegacyContentReader {
	t.Helper()
	reader, err := NewLegacyContentReader(t.TempDir(), maxBytes)
	if err != nil {
		t.Fatalf("NewLegacyContentReader: %v", err)
	}
	return reader
}

// writeLegacy 在 reader 的 root 下写一份旧正文，返回它的 source。
func writeLegacy(t *testing.T, reader *LegacyContentReader, source, content string) {
	t.Helper()
	abs := filepath.Join(reader.Root(), filepath.FromSlash(source))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir for %q: %v", source, err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write %q: %v", source, err)
	}
}

func TestLegacyReaderReadsExistingFile(t *testing.T) {
	reader := newTestReader(t, 0)
	source := ManagedDir + "/7/安装指南.md"
	writeLegacy(t, reader, source, "# 安装\n\n正文")

	content, err := reader.Read(source)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if content != "# 安装\n\n正文" {
		t.Fatalf("Read returned %q", content)
	}
	if !reader.Exists(source) {
		t.Fatal("Exists = false for a file that was just written")
	}
}

// 读取器必须是无副作用的：构造它不该在磁盘上留下任何东西。
//
// 这一条守的是「正文只有一处真相」—— 只要它还建目录，就迟早会有人顺手写回去，
// 然后正文又分裂成库里的和盘上的两份，而两边的分歧没有任何东西会发现。
func TestLegacyReaderCreatesNothing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "never-created")
	reader, err := NewLegacyContentReader(root, 0)
	if err != nil {
		t.Fatalf("NewLegacyContentReader: %v", err)
	}
	if _, err := reader.Read(ManagedDir + "/1/a.md"); !errors.Is(err, ErrContentNotFound) {
		t.Fatalf("Read on a missing root: err = %v, want ErrContentNotFound", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("root %q exists after construction/read: %v", root, err)
	}
}

func TestLegacyReaderRejectsEscape(t *testing.T) {
	reader := newTestReader(t, 0)
	secret := filepath.Join(filepath.Dir(reader.Root()), "outside.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	if _, err := reader.Read("../outside.txt"); !errors.Is(err, ErrContentOutsideRoot) {
		t.Fatalf("Read outside root: err = %v, want ErrContentOutsideRoot", err)
	}
	if _, err := reader.Read(secret); !errors.Is(err, ErrContentOutsideRoot) {
		t.Fatalf("Read absolute outside path: err = %v, want ErrContentOutsideRoot", err)
	}
}

// 软链是绕过前缀比较的经典手法：校验必须发生在解析符号链接之后。
//
// 现在的语境比过去更需要它：source 是**库里的数据**而不是请求参数，没人保证
// 一条历史行的 source 不是别人精心构造出来的。
func TestLegacyReaderRejectsSymlinkEscape(t *testing.T) {
	reader := newTestReader(t, 0)
	outside := filepath.Join(filepath.Dir(reader.Root()), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	link := filepath.Join(reader.Root(), ManagedDir, "link.md")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatalf("mkdir managed dir: %v", err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	if _, err := reader.Read(ManagedDir + "/link.md"); !errors.Is(err, ErrContentOutsideRoot) {
		t.Fatalf("Read symlink: err = %v, want ErrContentOutsideRoot", err)
	}
}

func TestLegacyReaderEnforcesMaxBytes(t *testing.T) {
	reader := newTestReader(t, 4)
	source := ManagedDir + "/1/big.md"
	writeLegacy(t, reader, source, "12345")

	if _, err := reader.Read(source); !errors.Is(err, ErrContentTooLarge) {
		t.Fatalf("Read oversize: err = %v, want ErrContentTooLarge", err)
	}
}

func TestLegacyReaderReadMissing(t *testing.T) {
	reader := newTestReader(t, 0)

	if _, err := reader.Read(ManagedDir + "/1/nope.md"); !errors.Is(err, ErrContentNotFound) {
		t.Fatalf("Read missing: err = %v, want ErrContentNotFound", err)
	}
	if _, err := reader.Read("  "); !errors.Is(err, ErrContentNotFound) {
		t.Fatalf("Read empty source: err = %v, want ErrContentNotFound", err)
	}
}

// 目录不是正文：把它当文件读会返回 ReadFile 的 EISDIR，而那个错误对调用方
// 毫无意义（它会当成「读取失败」去重试）。
func TestLegacyReaderRejectsDirectory(t *testing.T) {
	reader := newTestReader(t, 0)
	dir := filepath.Join(reader.Root(), ManagedDir, "1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if _, err := reader.Read(ManagedDir + "/1"); !errors.Is(err, ErrContentNotFound) {
		t.Fatalf("Read directory: err = %v, want ErrContentNotFound", err)
	}
}

func TestLegacyReaderRequiresRoot(t *testing.T) {
	if _, err := NewLegacyContentReader("  ", 0); err == nil {
		t.Fatal("empty root should be rejected")
	}
}

// source 的形状是对外契约：存量语料、ES 字段与评测用例集里都存着它。
// 这里把形状钉住，免得「换个更干净的编码」把存量数据全部对不上。
func TestDocumentSourceIsStable(t *testing.T) {
	got := DocumentSource(2, "h105p-1a2b3c4d")
	want := "documents/2/h105p-1a2b3c4d.md"
	if got != want {
		t.Fatalf("DocumentSource = %q, want %q", got, want)
	}
	if strings.Contains(got, `\`) {
		t.Fatalf("source %q uses a backslash; it must stay slash-separated across platforms", got)
	}
}
