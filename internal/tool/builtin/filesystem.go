package builtin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

type FileSystem struct {
	Root         string
	MaxReadBytes int
}

type readFileInput struct {
	Path string `json:"path" jsonschema_description:"Path relative to the workspace root"`
}

type writeFileInput struct {
	Path    string `json:"path" jsonschema_description:"Path relative to the workspace root"`
	Content string `json:"content" jsonschema_description:"Complete file content to write"`
}

type listDirInput struct {
	Path string `json:"path" jsonschema_description:"Directory path relative to the workspace root"`
}

func NewFileSystem(root string, maxReadBytes int) ([]tool.BaseTool, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root: %w", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root symlinks: %w", err)
	}
	info, err := os.Stat(resolvedRoot)
	if err != nil {
		return nil, fmt.Errorf("stat workspace root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workspace root is not a directory: %s", root)
	}
	fs := &FileSystem{Root: resolvedRoot, MaxReadBytes: maxReadBytes}
	readTool, err := utils.InferTool("read_file", "Read a UTF-8 text file from the workspace.", fs.readFile)
	if err != nil {
		return nil, err
	}
	writeTool, err := utils.InferTool("write_file", "Write or overwrite a UTF-8 text file in the workspace. Creates parent directories.", fs.writeFile)
	if err != nil {
		return nil, err
	}
	listTool, err := utils.InferTool("list_dir", "List entries in a workspace directory.", fs.listDir)
	if err != nil {
		return nil, err
	}
	return []tool.BaseTool{readTool, writeTool, listTool}, nil
}

func (f *FileSystem) safePath(p string) (string, error) {
	if p == "" {
		return "", errors.New("path is required")
	}
	clean := filepath.Clean(p)
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("absolute paths are not allowed: %s", p)
	}
	full := filepath.Join(f.Root, clean)
	rel, err := filepath.Rel(f.Root, full)
	if err != nil {
		return "", fmt.Errorf("path is outside the workspace: %s", p)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path is outside the workspace: %s", p)
	}
	return f.resolveWithinRoot(full, p)
}

func (f *FileSystem) resolveWithinRoot(full, original string) (string, error) {
	existing := full
	for {
		_, err := os.Lstat(existing)
		if err == nil {
			resolved, err := filepath.EvalSymlinks(existing)
			if err != nil {
				return "", fmt.Errorf("resolve path symlinks: %w", err)
			}
			rel, err := filepath.Rel(f.Root, resolved)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return "", fmt.Errorf("path escapes workspace through a symlink: %s", original)
			}
			return full, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("inspect path: %w", err)
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", fmt.Errorf("path is outside the workspace: %s", original)
		}
		existing = parent
	}
}

func (f *FileSystem) readFile(ctx context.Context, in readFileInput) (string, error) {
	full, err := f.safePath(in.Path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	if f.MaxReadBytes > 0 && len(data) > f.MaxReadBytes {
		data = data[:f.MaxReadBytes]
	}
	return string(data), nil
}

func (f *FileSystem) writeFile(ctx context.Context, in writeFileInput) (string, error) {
	full, err := f.safePath(in.Path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		return "", err
	}
	if _, err := f.safePath(in.Path); err != nil {
		return "", err
	}
	if err := os.WriteFile(full, []byte(in.Content), 0644); err != nil {
		return "", err
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(in.Content), in.Path), nil
}

func (f *FileSystem) listDir(ctx context.Context, in listDirInput) (string, error) {
	full, err := f.safePath(in.Path)
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, e := range entries {
		kind := "file"
		if e.IsDir() {
			kind = "dir"
		}
		fmt.Fprintf(&b, "%s\t%s\n", kind, e.Name())
	}
	return b.String(), nil
}
