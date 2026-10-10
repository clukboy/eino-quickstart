package knowledge

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"eino-quickstart/ent"
)

type directoryImportFake struct {
	docs      []*ent.Document
	created   []CreateInput
	updated   []UpdateInput
	createErr error
	updateErr error
}

func (f *directoryImportFake) List(context.Context, uint64) ([]*ent.Document, error) {
	return f.docs, nil
}
func (f *directoryImportFake) Create(_ context.Context, in CreateInput) ([]CreateResult, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.created = append(f.created, in)
	doc := &ent.Document{ID: uint64(100 + len(f.created)), Source: in.Source, Content: in.Content, Title: in.Title}
	f.docs = append(f.docs, doc)
	return []CreateResult{{Document: doc, Operation: OperationCreated}}, nil
}
func (f *directoryImportFake) Update(_ context.Context, in UpdateInput) (*ent.Document, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	f.updated = append(f.updated, in)
	for _, doc := range f.docs {
		if doc.ID == in.DocumentID {
			doc.Content, doc.Title = in.Content, in.Title
			return doc, nil
		}
	}
	return nil, nil
}

func TestImportDirectoryDryRunAndIncrementalReplay(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.md", "alpha")
	write("nested/b.markdown", "bravo")
	write("ignored.pdf", "no")

	fake := &directoryImportFake{}
	report, err := ImportDirectory(context.Background(), fake, DirectoryImportConfig{Root: root, DatasetID: 7, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.Created != 2 || report.Failed != 0 || len(fake.created) != 0 {
		t.Fatalf("dry-run report=%+v writes=%d", report, len(fake.created))
	}
	if report.Files[0].Source == report.Files[1].Source {
		t.Fatalf("nested paths must have distinct stable sources: %+v", report.Files)
	}

	report, err = ImportDirectory(context.Background(), fake, DirectoryImportConfig{Root: root, DatasetID: 7})
	if err != nil {
		t.Fatal(err)
	}
	if report.Created != 2 || len(fake.created) != 2 {
		t.Fatalf("first import report=%+v writes=%d", report, len(fake.created))
	}
	firstSource := fake.created[0].Source

	report, err = ImportDirectory(context.Background(), fake, DirectoryImportConfig{Root: root, DatasetID: 7})
	if err != nil {
		t.Fatal(err)
	}
	if report.Unchanged != 2 || report.Created != 0 || report.Updated != 0 {
		t.Fatalf("replay report=%+v", report)
	}
	if fake.created[0].Source != firstSource {
		t.Fatalf("source changed across replay: %q vs %q", firstSource, fake.created[0].Source)
	}

	write("a.md", "alpha v2")
	report, err = ImportDirectory(context.Background(), fake, DirectoryImportConfig{Root: root, DatasetID: 7})
	if err != nil {
		t.Fatal(err)
	}
	if report.Updated != 1 || report.Unchanged != 1 || len(fake.updated) != 1 {
		t.Fatalf("incremental report=%+v updates=%d", report, len(fake.updated))
	}
}

func TestImportDirectoryFailureRetryAndNoDelete(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "good.md"), []byte("good"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "empty.md"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape.md")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	fake := &directoryImportFake{docs: []*ent.Document{{ID: 99, Source: "documents/4/removed.md", Content: "old"}}}
	report, err := ImportDirectory(context.Background(), fake, DirectoryImportConfig{Root: root, DatasetID: 4})
	if err != nil {
		t.Fatal(err)
	}
	if report.Created != 1 || report.Failed != 2 || report.MissingNotDeleted != 1 {
		t.Fatalf("failure report=%+v", report)
	}
	for _, item := range report.Files {
		if item.Path == "escape.md" && item.Error != "path_outside_root" {
			t.Fatalf("escape result=%+v", item)
		}
	}

	if err := os.WriteFile(filepath.Join(root, "empty.md"), []byte("fixed"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err = ImportDirectory(context.Background(), fake, DirectoryImportConfig{Root: root, DatasetID: 4, RetryPaths: []string{"empty.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Created != 1 || report.Failed != 0 || len(report.Files) != 1 {
		t.Fatalf("retry report=%+v", report)
	}
}

func TestImportDirectoryRejectsRetryOutsideRoot(t *testing.T) {
	fake := &directoryImportFake{}
	_, err := ImportDirectory(context.Background(), fake, DirectoryImportConfig{Root: t.TempDir(), DatasetID: 1, RetryPaths: []string{"../secret.md"}})
	if err == nil {
		t.Fatal("expected retry boundary error")
	}
}
