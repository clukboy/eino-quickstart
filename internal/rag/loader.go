package rag

import (
	"context"
	"eino-quickstart/internal/rag/parser"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino-ext/components/document/loader/file"
	"github.com/cloudwego/eino/components/document"
	einoparser "github.com/cloudwego/eino/components/document/parser"
	"github.com/cloudwego/eino/schema"
)

// FileLoaderConfig controls the boundary checks applied by FileLoader.
type FileLoaderConfig struct {
	Root      string   // 允许的文档根目录
	MaxBytes  int64    // 单文件大小上限，0 表示默认 5MB
	AllowExts []string // 扩展名白名单，nil 表示默认 .md/.txt
}

// FileLoader 从受限根目录加载纯文本/Markdown 文件。
type FileLoader struct {
	root      string
	maxBytes  int64
	allowExts map[string]struct{}
	loader    document.Loader
}

// NewFileLoader 初始化 FileLoader
func NewFileLoader(ctx context.Context, cfg FileLoaderConfig) (*FileLoader, error) {
	if cfg.Root == "" {
		return nil, fmt.Errorf("rag: loader root is required")
	}
	abs, err := filepath.Abs(cfg.Root)
	if err != nil {
		return nil, fmt.Errorf("rag: resolve root %q: %w", cfg.Root, err)
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = 5 << 20 // 5MB
	}
	exts := cfg.AllowExts
	if len(exts) == 0 {
		exts = []string{".md", ".markdown", ".txt"}
	}
	m := make(map[string]struct{}, len(exts))
	for _, e := range exts {
		m[strings.ToLower(e)] = struct{}{}
	}
	docParser, err := parser.NewParser(ctx, &parser.ParserConfig{
		Parsers: map[string]einoparser.Parser{
			parser.TypeProduct: parser.ProductParser{},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("rag: create doc parser: %w", err)
	}
	loader, err := file.NewFileLoader(ctx, &file.FileLoaderConfig{
		UseNameAsID: false,
		Parser:      docParser,
	})

	if err != nil {
		return nil, fmt.Errorf("rag: create file loader: %w", err)
	}
	return &FileLoader{root: abs, maxBytes: cfg.MaxBytes, allowExts: m, loader: loader}, nil
}

// Load 实现 eino document.Loader 接口。

func (l *FileLoader) Load(ctx context.Context, src document.Source, opts ...document.LoaderOption) ([]*schema.Document, error) {

	// [优化] 路径穿越防护：先解析软链再校验前缀
	abs, err := filepath.Abs(src.URI)
	if err != nil {
		return nil, fmt.Errorf("rag: abs path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("rag: resolve symlink %q: %w", src.URI, err)
	}
	if !strings.HasPrefix(resolved, l.root+string(os.PathSeparator)) && resolved != l.root {
		return nil, fmt.Errorf("rag: path %q escapes doc root", src.URI)
	}

	ext := strings.ToLower(filepath.Ext(resolved))
	if _, ok := l.allowExts[ext]; !ok {
		return nil, fmt.Errorf("rag: extension %q not allowed", ext)
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("rag: stat %q: %w", src.URI, err)
	}
	if info.Size() > l.maxBytes {
		return nil, fmt.Errorf("rag: file too large: %d > %d", info.Size(), l.maxBytes)
	}

	return l.loader.Load(ctx, src, opts...)
}
