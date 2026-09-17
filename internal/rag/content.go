package rag

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// ContentStore 管理 knowledge root 下的文档正文文件。
//
// 正文以文件为唯一真相：documents.source 保存相对 root 的路径（或 root 内的
// 绝对路径），「重建索引」只要把文件重新读出来即可，数据库里不必再存一份。
//
// 进出的每一条路径都收敛到 root 之内，用的是和 FileLoader 同一套边界规则：
// 先解析符号链接，再比较前缀，`..` 与软链都逃不出去。
//
// 文件分两类：
//
//	托管文件  写在 <root>/documents/ 下，由 API 创建与改写，删除文档时一并清理
//	注册文件  调用方自己放进 root 的其他文件，只读不写，删除文档时保留
//
// 这么分的理由是删除的破坏性：注册文件是调用方的资产，摘掉索引就够了，
// 不该由知识库接口替它决定文件去留。
type ContentStore struct {
	root     string
	maxBytes int64
}

// ManagedDir 是 ContentStore 在 knowledge root 下自管的子目录。
const ManagedDir = "documents"

// defaultContentMaxBytes 与 knowledge.maxDocumentBytes 的默认值一致。
const defaultContentMaxBytes int64 = 5 << 20

var (
	// ErrContentNotFound 表示 source 指向的文件不存在或不可读。
	ErrContentNotFound = errors.New("knowledge content not found")
	// ErrContentOutsideRoot 表示 source 逃出了 knowledge root。
	ErrContentOutsideRoot = errors.New("knowledge content escapes root")
	// ErrContentTooLarge 表示正文超过 knowledge.maxDocumentBytes。
	ErrContentTooLarge = errors.New("knowledge content is too large")
	// ErrContentNotManaged 表示试图改写一个不属于托管目录的文件。
	ErrContentNotManaged = errors.New("knowledge content is not managed by the api")
)

// NewContentStore 打开（必要时创建）knowledge root。
func NewContentStore(root string, maxBytes int64) (*ContentStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("knowledge content root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve knowledge root %q: %w", root, err)
	}
	// root 本身可能是软链（macOS 的 /tmp 就是），解析后前缀比较才准确。
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("create knowledge root %q: %w", abs, err)
	}
	if maxBytes <= 0 {
		maxBytes = defaultContentMaxBytes
	}
	return &ContentStore{root: abs, maxBytes: maxBytes}, nil
}

// Root 返回解析后的 knowledge root 绝对路径。
func (s *ContentStore) Root() string {
	if s == nil {
		return ""
	}
	return s.root
}

// Exists 报告 source 是否指向一个已存在的文件。
func (s *ContentStore) Exists(source string) bool {
	abs, err := s.resolve(source)
	if err != nil {
		return false
	}
	info, err := os.Stat(abs)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func (s *ContentStore) Create(datasetID uint64, title, content string) (string, error) {
	if err := s.checkSize(content); err != nil {
		return "", err
	}
	name := slugify(title) + ".md"
	// 存库统一用正斜杠，避免换操作系统后读不回来。
	source := ManagedDir + "/" + strconv.FormatUint(datasetID, 10) + "/" + name

	abs, err := s.resolve(source)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", fmt.Errorf("create knowledge content dir for %q: %w", source, err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write knowledge content %q: %w", source, err)
	}
	return source, nil
}

// Write 覆盖写一个正文文件。只有托管目录内的路径允许改写：调用方自己放进
// root 的文件是它的资产，接口不该无声覆盖。
func (s *ContentStore) Write(source, content string) error {
	if err := s.checkSize(content); err != nil {
		return err
	}
	abs, err := s.resolve(source)
	if err != nil {
		return err
	}
	if !s.managedPath(abs) {
		return fmt.Errorf("%w: %q", ErrContentNotManaged, source)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Errorf("create knowledge content dir for %q: %w", source, err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write knowledge content %q: %w", source, err)
	}
	return nil
}

// Read 读取正文。路径越界、文件缺失、超限都返回可判别的错误。
func (s *ContentStore) Read(source string) (string, error) {
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
func (s *ContentStore) Exists(source string) bool {
	abs, err := s.resolve(source)
	if err != nil {
		return false
	}
	info, err := os.Stat(abs)
	return err == nil && !info.IsDir()
}

// Remove 删除托管目录内的正文文件。注册文件原样保留，文件不存在也返回 nil。
func (s *ContentStore) Remove(source string) error {
	abs, err := s.resolve(source)
	if err != nil {
		// 越界与空 source 都不该由删除路径报错：这里只做清理。
		return nil
	}
	if !s.managedPath(abs) {
		return nil
	}
	if err := os.Remove(abs); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove knowledge content %q: %w", source, err)
	}
	return nil
}

// Managed 报告 source 是否落在托管目录内。
func (s *ContentStore) Managed(source string) bool {
	abs, err := s.resolve(source)
	if err != nil {
		return false
	}
	return s.managedPath(abs)
}

func (s *ContentStore) managedPath(abs string) bool {
	root := filepath.Join(s.root, ManagedDir)
	return abs == root || strings.HasPrefix(abs, root+string(os.PathSeparator))
}

// resolve 把 source 归一到 root 内的绝对路径。
//
// 目标（以及它的若干级父目录）可能都还不存在 —— 新建文档就是这种情况 —— 所以
// 先向上找到第一个存在的祖先做符号链接解析，再把剩下的部分拼回去。若直接对
// 不存在的路径调用 EvalSymlinks，越界检查会因为拿不到真实路径而形同虚设。
func (s *ContentStore) resolve(source string) (string, error) {
	if s == nil || s.root == "" {
		return "", errors.New("knowledge content store is not configured")
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

func (s *ContentStore) checkSize(content string) error {
	if s.maxBytes > 0 && int64(len(content)) > s.maxBytes {
		return fmt.Errorf("%w: %d bytes exceeds %d", ErrContentTooLarge, len(content), s.maxBytes)
	}
	return nil
}

// slugify 把标题压成文件名片段：保留字母与数字（含中文），其余折叠成连字符。
func slugify(title string) string {
	var b strings.Builder
	lastDash := true // 防止开头出现连字符
	for _, r := range strings.TrimSpace(title) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
		if b.Len() >= 64 {
			break
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "document"
	}
	return out
}
