package knowledge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

var supportedDocumentExtensions = map[string]struct{}{
	".markdown": {},
	".md":       {},
	".text":     {},
	".txt":      {},
}

// LoaderConfig configures a filesystem-backed knowledge document loader.
type LoaderConfig struct {
	Root             string
	MaxDocumentBytes int
}

// LoadedDocument is a document read from a Loader's configured root.
// Source is always a slash-separated path relative to that root.
type LoadedDocument struct {
	Source  string
	Title   string
	Content string
}

// Loader recursively reads supported UTF-8 text documents from one root.
type Loader struct {
	root             string
	maxDocumentBytes int
}

// NewLoader creates a loader rooted at config.Root. The root is resolved once
// so files can subsequently be checked against its canonical boundary.
func NewLoader(config LoaderConfig) (*Loader, error) {
	if config.MaxDocumentBytes <= 0 {
		return nil, errors.New(
			"knowledge maximum document bytes must be greater than zero",
		)
	}
	if strings.TrimSpace(config.Root) == "" {
		return nil, errors.New("knowledge root is required")
	}

	absoluteRoot, err := filepath.Abs(config.Root)
	if err != nil {
		return nil, fmt.Errorf("resolve knowledge root: %w", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(absoluteRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve knowledge root symlinks: %w", err)
	}
	info, err := os.Stat(resolvedRoot)
	if err != nil {
		return nil, fmt.Errorf("stat knowledge root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("knowledge root is not a directory: %s", config.Root)
	}

	return &Loader{
		root:             filepath.Clean(resolvedRoot),
		maxDocumentBytes: config.MaxDocumentBytes,
	}, nil
}

// Load recursively loads supported regular files beneath the configured root.
// Symlinks and files that resolve outside the root are never followed.
func (l *Loader) Load(ctx context.Context) ([]LoadedDocument, error) {
	if l == nil {
		return nil, errors.New("knowledge loader is nil")
	}
	if ctx == nil {
		return nil, errors.New("knowledge load context is required")
	}
	if l.root == "" || l.maxDocumentBytes <= 0 {
		return nil, errors.New("knowledge loader is not configured")
	}
	if err := l.verifyRoot(); err != nil {
		return nil, err
	}

	documents := make([]LoadedDocument, 0)
	err := filepath.WalkDir(
		l.root,
		func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if path == l.root {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			info, err := entry.Info()
			if err != nil {
				return fmt.Errorf("stat knowledge path %q: %w", path, err)
			}
			if info.Mode()&os.ModeSymlink != 0 {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if info.IsDir() {
				return l.verifyPath(path)
			}
			if !info.Mode().IsRegular() || !isSupportedDocument(path) {
				return nil
			}

			document, err := l.loadFile(path)
			if err != nil {
				return err
			}
			documents = append(documents, document)
			return nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("load knowledge documents: %w", err)
	}

	sort.Slice(documents, func(i, j int) bool {
		return documents[i].Source < documents[j].Source
	})
	return documents, nil
}

func (l *Loader) verifyRoot() error {
	resolvedRoot, err := filepath.EvalSymlinks(l.root)
	if err != nil {
		return fmt.Errorf("resolve knowledge root: %w", err)
	}
	if filepath.Clean(resolvedRoot) != l.root {
		return errors.New("knowledge root changed after loader initialization")
	}
	info, err := os.Stat(l.root)
	if err != nil {
		return fmt.Errorf("stat knowledge root: %w", err)
	}
	if !info.IsDir() {
		return errors.New("knowledge root is not a directory")
	}
	return nil
}

func (l *Loader) loadFile(path string) (LoadedDocument, error) {
	source, err := l.sourceFor(path)
	if err != nil {
		return LoadedDocument{}, err
	}
	if err := l.verifyPath(path); err != nil {
		return LoadedDocument{}, err
	}

	file, err := os.Open(path)
	if err != nil {
		return LoadedDocument{}, fmt.Errorf("open knowledge document %q: %w", source, err)
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return LoadedDocument{}, fmt.Errorf("stat knowledge document %q: %w", source, err)
	}
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return LoadedDocument{}, fmt.Errorf("lstat knowledge document %q: %w", source, err)
	}
	if !pathInfo.Mode().IsRegular() || !fileInfo.Mode().IsRegular() ||
		!os.SameFile(pathInfo, fileInfo) {
		return LoadedDocument{}, fmt.Errorf(
			"knowledge document %q changed while opening",
			source,
		)
	}
	if err := l.verifyPath(path); err != nil {
		return LoadedDocument{}, err
	}

	data, err := io.ReadAll(io.LimitReader(
		file,
		int64(l.maxDocumentBytes)+1,
	))
	if err != nil {
		return LoadedDocument{}, fmt.Errorf("read knowledge document %q: %w", source, err)
	}
	if len(data) > l.maxDocumentBytes {
		return LoadedDocument{}, fmt.Errorf(
			"knowledge document %q exceeds the maximum of %d bytes",
			source,
			l.maxDocumentBytes,
		)
	}
	if !utf8.Valid(data) {
		return LoadedDocument{}, fmt.Errorf(
			"knowledge document %q is not valid UTF-8",
			source,
		)
	}

	title := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
	if title == "" {
		title = source
	}
	return LoadedDocument{
		Source:  source,
		Title:   title,
		Content: string(data),
	}, nil
}

func (l *Loader) sourceFor(path string) (string, error) {
	relative, err := filepath.Rel(l.root, path)
	if err != nil || relative == "." ||
		relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("knowledge path %q is outside the configured root", path)
	}
	if !utf8.ValidString(relative) || containsControlCharacter(relative) {
		return "", fmt.Errorf("knowledge path %q is not a valid document source", path)
	}
	return filepath.ToSlash(relative), nil
}

func (l *Loader) verifyPath(path string) error {
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("resolve knowledge path %q: %w", path, err)
	}
	relative, err := filepath.Rel(l.root, resolvedPath)
	if err != nil || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("knowledge path %q is outside the configured root", path)
	}
	return nil
}

func isSupportedDocument(path string) bool {
	_, supported := supportedDocumentExtensions[strings.ToLower(filepath.Ext(path))]
	return supported
}
