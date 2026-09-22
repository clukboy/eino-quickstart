package rag

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LegacyContentReader 读 knowledge root 下的旧正文文件。
//
// 它的存在只有一个理由：正文已经从磁盘搬进 documents.content，而存量文档的
// 正文还留在原来的文件里。想继续用这批文档，就得把它们读回来一次并写进库
// （见 application/knowledge 的 Indexer.importLegacyContent）。所以这个类型
// **只读、不写、不建目录**：任何「顺手落一份文件」的路径都被刻意去掉了 ——
// 留着它，正文的真相就会重新分裂成两处。
//
// 它是一次性的迁移工具，不是长期设施：新语料不该再产生任何文件，等存量文档
// 都导入过一次之后，这个类型连同 knowledge.root 那段配置就可以整块删掉。
//
// 进出的每一条路径都收敛到 root 之内，用的是和 FileLoader 同一套边界规则：
// 先解析符号链接，再比较前缀，`..` 与软链都逃不出去。这条检查在新语境下更
// 重要：root 是配置项，而 source 是**库里的数据**，没人保证它不是
// "../../etc/passwd"。
type LegacyContentReader struct {
	root     string
	maxBytes int64
}

var (
	// ErrContentNotFound 表示 source 指向的文件不存在或不可读。
	ErrContentNotFound = errors.New("knowledge content not found")
	// ErrContentOutsideRoot 表示 source 逃出了 knowledge root。
	ErrContentOutsideRoot = errors.New("knowledge content escapes root")
	// ErrContentTooLarge 表示正文超过 knowledge.maxDocumentBytes。
	ErrContentTooLarge = errors.New("knowledge content is too large")
)

// NewLegacyContentReader 打开旧正文目录。root 不存在不报错：那说明这批部署
// 从来没有文件时代的语料，是完全正常的形态。
func NewLegacyContentReader(root string, maxBytes int64) (*LegacyContentReader, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("knowledge content root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve knowledge root %q: %w", root, err)
	}
	// root 必须先归一化，否则后面每一条读取都会被误判成越界。
	// 两种情形都要处理，而且都不能靠 MkdirAll 绕过（这个类型不写磁盘）：
	//   - root 是个软链（macOS 的 /tmp 就是）；
	//   - root 还不存在（文件时代的语料可能压根没有）。
	//
	// 前缀比较是拿字符串比的：root 若是未解析的 /var/... 而实际路径是
	// /private/var/...，一条完全合法的读取会得到「逃出了 knowledge root」——
	// 一个把装配问题说成安全问题、且完全查不动的错误。
	if resolved, err := resolveExisting(abs); err == nil {
		abs = resolved
	}
	if maxBytes <= 0 {
		maxBytes = defaultContentMaxBytes
	}
	return &LegacyContentReader{root: abs, maxBytes: maxBytes}, nil
}

// defaultContentMaxBytes 与 knowledge.maxDocumentBytes 的默认值一致。
const defaultContentMaxBytes int64 = 5 << 20

// Root 返回解析后的 knowledge root 绝对路径。可能并不存在。
func (s *LegacyContentReader) Root() string {
	if s == nil {
		return ""
	}
	return s.root
}

// Read 读取正文。路径越界、文件缺失、超限都返回可判别的错误。
func (s *LegacyContentReader) Read(source string) (string, error) {
	abs, err := s.resolve(source)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("%w: %q", ErrContentNotFound, source)
		}
		return "", fmt.Errorf("stat knowledge content %q: %w", source, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%w: %q is a directory", ErrContentNotFound, source)
	}
	if s.maxBytes > 0 && info.Size() > s.maxBytes {
		return "", fmt.Errorf("%w: %d bytes exceeds %d", ErrContentTooLarge, info.Size(), s.maxBytes)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("%w: %q", ErrContentNotFound, source)
		}
		return "", fmt.Errorf("read knowledge content %q: %w", source, err)
	}
	return string(data), nil
}

// Exists 报告 source 是否指向一个已存在的文件。
func (s *LegacyContentReader) Exists(source string) bool {
	abs, err := s.resolve(source)
	if err != nil {
		return false
	}
	info, err := os.Stat(abs)
	return err == nil && !info.IsDir()
}

// resolve 把 source 归一到 root 内的绝对路径。
//
// 目标（以及它的若干级父目录）可能都不存在，所以先向上找到第一个存在的祖先
// 做符号链接解析，再把剩下的部分拼回去。若直接对不存在的路径调用 EvalSymlinks，
// 越界检查会因为拿不到真实路径而形同虚设。
func (s *LegacyContentReader) resolve(source string) (string, error) {
	if s == nil || s.root == "" {
		return "", errors.New("knowledge legacy content reader is not configured")
	}
	trimmed := strings.TrimSpace(source)
	if trimmed == "" {
		return "", fmt.Errorf("%w: empty source", ErrContentNotFound)
	}
	target := filepath.FromSlash(trimmed)
	if !filepath.IsAbs(target) {
		target = filepath.Join(s.root, target)
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("resolve knowledge content %q: %w", source, err)
	}

	resolved, err := resolveExisting(abs)
	if err != nil {
		return "", fmt.Errorf("%w: %q", ErrContentNotFound, source)
	}
	if resolved != s.root && !strings.HasPrefix(resolved, s.root+string(os.PathSeparator)) {
		return "", fmt.Errorf("%w: %q", ErrContentOutsideRoot, source)
	}
	return resolved, nil
}

// resolveExisting 解析 abs 中已存在部分里的符号链接，拼回尚不存在的尾部。
func resolveExisting(abs string) (string, error) {
	current := abs
	var pending []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			if len(pending) == 0 {
				return resolved, nil
			}
			parts := append([]string{resolved}, reverseStrings(pending)...)
			return filepath.Join(parts...), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		pending = append(pending, filepath.Base(current))
		current = parent
	}
}

func reverseStrings(values []string) []string {
	out := make([]string, len(values))
	for i, value := range values {
		out[len(values)-1-i] = value
	}
	return out
}
