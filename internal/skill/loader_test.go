package skill

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLoaderListsAndLoadsSkills(t *testing.T) {
	root := t.TempDir()

	skillDir := filepath.Join(root, "go-testing")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}

	content := "# Go Testing\nUse go test ./...\n"
	if err := os.WriteFile(
		filepath.Join(skillDir, "SKILL.md"),
		[]byte(content),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	loader := &Loader{
		root:         root,
		maxReadBytes: 1024,
	}

	skills, err := loader.list(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if skills != "go-testing" {
		t.Fatalf("skills = %q", skills)
	}

	result, err := loader.load(
		context.Background(),
		loadSkillInput{Name: "go-testing"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result != content {
		t.Fatalf("result = %q", result)
	}
}

func TestLoaderRejectsInvalidSkillName(t *testing.T) {
	loader := &Loader{
		root:         t.TempDir(),
		maxReadBytes: 1024,
	}

	_, err := loader.load(
		context.Background(),
		loadSkillInput{Name: "../secret"},
	)
	if err == nil {
		t.Fatal("expected invalid skill name error")
	}
}
