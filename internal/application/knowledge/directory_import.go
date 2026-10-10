package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"eino-quickstart/ent"
	"eino-quickstart/internal/rag"
)

const (
	DirectoryFileCreated   = "created"
	DirectoryFileUpdated   = "updated"
	DirectoryFileUnchanged = "unchanged"
	DirectoryFileFailed    = "failed"
)

// DirectoryImportConfig defines a bounded recursive directory import.
// Root is the only filesystem boundary; symlinks resolving outside it are rejected.
type DirectoryImportConfig struct {
	Root            string
	DatasetID       uint64
	DryRun          bool
	MaxBytes        int64
	AllowExtensions []string
	Exclude         []string
	RetryPaths      []string
}

// DirectoryImportFile is the machine-readable result for one input path.
type DirectoryImportFile struct {
	Path          string `json:"path"`
	Source        string `json:"source"`
	ContentSHA256 string `json:"content_sha256,omitempty"`
	Status        string `json:"status"`
	DocumentID    uint64 `json:"document_id,omitempty"`
	Error         string `json:"error,omitempty"`
}

// DirectoryImportReport is safe to persist as an acceptance artifact.
type DirectoryImportReport struct {
	Root              string                `json:"root"`
	DatasetID         uint64                `json:"dataset_id"`
	DryRun            bool                  `json:"dry_run"`
	NoDelete          bool                  `json:"no_delete"`
	Files             []DirectoryImportFile `json:"files"`
	Created           int                   `json:"created"`
	Updated           int                   `json:"updated"`
	Unchanged         int                   `json:"unchanged"`
	Failed            int                   `json:"failed"`
	MissingNotDeleted int                   `json:"missing_not_deleted"`
}

// DirectoryImportStore is the small application port needed by the importer.
// Keeping it separate makes dry-run and boundary behavior testable without a live queue.
type DirectoryImportStore interface {
	List(context.Context, uint64) ([]*ent.Document, error)
	Create(context.Context, CreateInput) ([]CreateResult, error)
	Update(context.Context, UpdateInput) (*ent.Document, error)
}

// ImportDirectory recursively plans and optionally applies a directory import.
// Existing documents whose source is absent from the directory are deliberately not deleted.
func ImportDirectory(ctx context.Context, store DirectoryImportStore, cfg DirectoryImportConfig) (*DirectoryImportReport, error) {
	if store == nil {
		return nil, errors.New("knowledge: directory import store is required")
	}
	root, err := resolveImportRoot(cfg.Root)
	if err != nil {
		return nil, err
	}
	if cfg.DatasetID == 0 {
		return nil, errors.New("knowledge: directory import dataset id must be positive")
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = defaultDocumentMaxBytes
	}
	allow := normalizeExtensions(cfg.AllowExtensions)
	if len(allow) == 0 {
		allow = map[string]struct{}{".md": {}, ".markdown": {}, ".txt": {}, ".text": {}}
	}

	files, err := scanImportFiles(root, cfg, allow)
	if err != nil {
		return nil, err
	}
	existing, err := store.List(ctx, cfg.DatasetID)
	if err != nil {
		return nil, fmt.Errorf("knowledge: list existing documents for directory import: %w", err)
	}
	bySource := make(map[string]*ent.Document, len(existing))
	for _, doc := range existing {
		if doc != nil && doc.Source != "" {
			bySource[doc.Source] = doc
		}
	}

	retry := make(map[string]struct{}, len(cfg.RetryPaths))
	for _, p := range cfg.RetryPaths {
		rel, err := safeRelativePath(root, p)
		if err != nil {
			return nil, fmt.Errorf("knowledge: retry path %q: %w", p, err)
		}
		retry[rel] = struct{}{}
	}

	report := &DirectoryImportReport{
		Root: root, DatasetID: cfg.DatasetID, DryRun: cfg.DryRun, NoDelete: true,
		Files: make([]DirectoryImportFile, 0, len(files)),
	}
	seenSources := make(map[string]struct{}, len(files))
	for _, file := range files {
		if len(retry) > 0 {
			if _, ok := retry[file.rel]; !ok {
				continue
			}
		}
		result := DirectoryImportFile{Path: file.rel, Source: file.source}
		seenSources[file.source] = struct{}{}
		if file.scanError != "" {
			result.Status = DirectoryFileFailed
			result.Error = file.scanError
			report.Failed++
			report.Files = append(report.Files, result)
			continue
		}
		if file.size > cfg.MaxBytes {
			result.Status = DirectoryFileFailed
			result.Error = "payload_too_large"
			report.Failed++
			report.Files = append(report.Files, result)
			continue
		}
		f, err := os.Open(file.abs)
		if err != nil {
			result.Status = DirectoryFileFailed
			result.Error = err.Error()
			report.Failed++
			report.Files = append(report.Files, result)
			continue
		}
		content, readErr := io.ReadAll(io.LimitReader(f, cfg.MaxBytes+1))
		closeErr := f.Close()
		if readErr != nil {
			result.Status = DirectoryFileFailed
			result.Error = readErr.Error()
			report.Failed++
			report.Files = append(report.Files, result)
			continue
		}
		if closeErr != nil {
			result.Status = DirectoryFileFailed
			result.Error = closeErr.Error()
			report.Failed++
			report.Files = append(report.Files, result)
			continue
		}
		if int64(len(content)) > cfg.MaxBytes {
			result.Status = DirectoryFileFailed
			result.Error = "payload_too_large"
			report.Failed++
			report.Files = append(report.Files, result)
			continue
		}
		if len(content) == 0 {
			result.Status = DirectoryFileFailed
			result.Error = "empty_file"
			report.Failed++
			report.Files = append(report.Files, result)
			continue
		}
		hash := sha256.Sum256(content)
		result.ContentSHA256 = hex.EncodeToString(hash[:])
		doc := bySource[file.source]
		switch {
		case doc == nil:
			result.Status = DirectoryFileCreated
			if !cfg.DryRun {
				created, err := store.Create(ctx, CreateInput{
					DatasetID: cfg.DatasetID, Title: file.title, Content: string(content),
					Source: file.source, Metadata: map[string]string{"import_content_sha256": result.ContentSHA256},
				})
				if err != nil {
					result.Status, result.Error = DirectoryFileFailed, err.Error()
					report.Failed++
					report.Files = append(report.Files, result)
					continue
				}
				if len(created) > 0 && created[0].Document != nil {
					result.DocumentID = created[0].Document.ID
				}
			}
			report.Created++
		case sha256String(doc.Content) == result.ContentSHA256:
			result.Status = DirectoryFileUnchanged
			result.DocumentID = doc.ID
			report.Unchanged++
		default:
			result.Status = DirectoryFileUpdated
			result.DocumentID = doc.ID
			if !cfg.DryRun {
				updated, err := store.Update(ctx, UpdateInput{
					DatasetID: cfg.DatasetID, DocumentID: doc.ID, Title: file.title,
					Content: string(content), Metadata: map[string]string{"import_content_sha256": result.ContentSHA256},
				})
				if err != nil {
					result.Status, result.Error = DirectoryFileFailed, err.Error()
					report.Failed++
					report.Files = append(report.Files, result)
					continue
				}
				if updated != nil {
					result.DocumentID = updated.ID
				}
			}
			report.Updated++
		}
		report.Files = append(report.Files, result)
	}
	// Missing source rows are intentionally not counted as failures and are never deleted.
	// A retry report is scoped to failed files, so it must not pretend all other source
	// files disappeared from the input tree.
	if len(retry) == 0 {
		for source := range bySource {
			if _, ok := seenSources[source]; !ok {
				report.MissingNotDeleted++
			}
		}
	}
	return report, nil
}

// ImportDirectory is also available as a method for the normal application service.
func (s *Service) ImportDirectory(ctx context.Context, cfg DirectoryImportConfig) (*DirectoryImportReport, error) {
	return ImportDirectory(ctx, s, cfg)
}

type importFile struct {
	abs, rel, source, title string
	size                    int64
	scanError               string
}

func resolveImportRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", errors.New("knowledge: directory import root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("knowledge: resolve import root: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("knowledge: stat import root: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("knowledge: import root is not a directory: %s", abs)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("knowledge: resolve import root symlink: %w", err)
	}
	return filepath.Clean(resolved), nil
}

func scanImportFiles(root string, cfg DirectoryImportConfig, allow map[string]struct{}) ([]importFile, error) {
	excludes := make([]string, 0, len(cfg.Exclude))
	for _, e := range cfg.Exclude {
		e = filepath.Clean(strings.TrimSpace(e))
		if e != "." && e != "" {
			excludes = append(excludes, e)
		}
	}
	var files []importFile
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if excludedImportPath(rel, excludes) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			// Evaluate the target and apply the same root check as regular files.
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				files = append(files, importFile{rel: rel, source: sourceForImport(cfg.DatasetID, rel), title: titleForImport(rel), scanError: "symlink_error: " + err.Error()})
				return nil
			}
			if !withinRoot(root, resolved) {
				files = append(files, importFile{rel: rel, source: sourceForImport(cfg.DatasetID, rel), title: titleForImport(rel), scanError: "path_outside_root"})
				return nil
			}
		}
		ext := strings.ToLower(filepath.Ext(rel))
		if _, ok := allow[ext]; !ok {
			return nil
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if info.Size() > cfg.MaxBytes {
			files = append(files, importFile{abs: path, rel: rel, source: sourceForImport(cfg.DatasetID, rel), title: titleForImport(rel), size: info.Size()})
			return nil
		}
		files = append(files, importFile{abs: path, rel: rel, source: sourceForImport(cfg.DatasetID, rel), title: titleForImport(rel), size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("knowledge: scan import root: %w", err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })
	return files, nil
}

func withinRoot(root, path string) bool {
	root, path = filepath.Clean(root), filepath.Clean(path)
	return path == root || strings.HasPrefix(path, root+string(os.PathSeparator))
}

func safeRelativePath(root, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("empty path")
	}
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, abs)
	}
	abs, err := filepath.Abs(abs)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || rel == ".." {
		return "", errors.New("path is outside import root")
	}
	return rel, nil
}

func excludedImportPath(rel string, excludes []string) bool {
	for _, e := range excludes {
		if rel == e || strings.HasPrefix(rel, e+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

func normalizeExtensions(exts []string) map[string]struct{} {
	out := make(map[string]struct{}, len(exts))
	for _, ext := range exts {
		ext = strings.ToLower(strings.TrimSpace(ext))
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		out[ext] = struct{}{}
	}
	return out
}

func sourceForImport(datasetID uint64, rel string) string {
	ext := filepath.Ext(rel)
	stem := strings.TrimSuffix(filepath.ToSlash(rel), ext)
	pathHash := sha256.Sum256([]byte(filepath.ToSlash(rel)))
	suffix := hex.EncodeToString(pathHash[:])[:10]
	return rag.DocumentSource(datasetID, rag.Slugify(stem)+"-"+suffix)
}

func titleForImport(rel string) string {
	base := filepath.Base(rel)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func sha256String(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// WriteJSON persists a report without changing its schema or adding secrets.
func (r *DirectoryImportReport) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}
