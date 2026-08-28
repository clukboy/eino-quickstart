package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

type Loader struct {
	root         string
	maxReadBytes int
}
type loadSkillInput struct {
	Name string `json:"name" jsonschema_description:"Skill name, for example go-testing"`
}

func NewLoader(root string, maxReadBytes int) ([]tool.BaseTool, error) {
	if maxReadBytes <= 0 {
		return nil, fmt.Errorf("skill max read bytes must be greater than zero")
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve skills root: %w", err)
	}

	resolveRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve skills root symlinks: %w", err)
	}
	info, err := os.Stat(resolveRoot)
	if err != nil {
		return nil, fmt.Errorf("stat skills root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("skills root is not a directory: %s", root)
	}
	loader := &Loader{root: resolveRoot, maxReadBytes: maxReadBytes}

	listTool, err := utils.InferTool(
		"list_skills",
		"List the available skills. Call this before loading a skill when no suitable skill name is known.",
		loader.list,
	)
	if err != nil {
		return nil, err
	}
	loadTool, err := utils.InferTool(
		"load_skill",
		"Load the instructions for one named skill. Use list_skills first if needed.",
		loader.load,
	)
	if err != nil {
		return nil, err
	}
	return []tool.BaseTool{listTool, loadTool}, nil
}
func (l *Loader) list(_ context.Context, _ string) (string, error) {
	entries, err := os.ReadDir(l.root)
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			continue
		}
		skillPath := filepath.Join(l.root, entry.Name(), "SKILL.md")
		if _, err := os.Stat(skillPath); err == nil {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "No skills are available.", nil
	}

	return strings.Join(names, "\n"), nil
}

func (l *Loader) load(_ context.Context, in loadSkillInput) (string, error) {
	if !validName(in.Name) {
		return "", fmt.Errorf("invalid skill name: %q", in.Name)
	}
	skillPath := filepath.Join(l.root, in.Name, "SKILL.md")
	resolvedPath, err := filepath.EvalSymlinks(skillPath)
	if err != nil {
		return "", fmt.Errorf("resolve skill %q: %w", in.Name, err)
	}
	relative, err := filepath.Rel(l.root, resolvedPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("skill %q is outside the skills root", in.Name)
	}
	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		return "", err
	}
	if len(data) > l.maxReadBytes {
		data = append(
			data[:l.maxReadBytes],
			[]byte("\n...[skill content truncated by harness]")...,
		)
	}
	return string(data), nil
}

func validName(name string) bool {
	if name == "" || name == "." || name == ".." || name != filepath.Base(name) {
		return false
	}
	for _, char := range name {
		if !((char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '-' ||
			char == '_') {
			return false
		}
	}

	return true
}
