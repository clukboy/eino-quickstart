package knowledge

import (
	"strings"
	"testing"

	"eino-quickstart/ent/document"
)

func TestValidateIngestInput(t *testing.T) {
	input, err := validateIngestInput(
		" docs/guide.md ",
		" Guide ",
		"\n# Guide\ncontent\n",
		"",
		"SYSTEM",
	)
	if err != nil {
		t.Fatalf("validateIngestInput() error = %v", err)
	}
	if input.source != "docs/guide.md" || input.title != "Guide" ||
		input.visibility != document.VisibilitySystem ||
		input.ownerSubject != "system" {
		t.Errorf("validated input = %#v", input)
	}
	if input.content != "\n# Guide\ncontent\n" {
		t.Errorf("content = %q, want original content for source line citations", input.content)
	}

	for name, args := range map[string][]string{
		"empty source":       {"", "title", "content", "system", "system"},
		"empty title":        {"source", "", "content", "system", "system"},
		"empty content":      {"source", "title", " \n ", "system", "system"},
		"private no owner":   {"source", "title", "content", "", "private"},
		"invalid visibility": {"source", "title", "content", "owner", "public"},
		"control in source":  {"source\nnext", "title", "content", "system", "system"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := validateIngestInput(
				args[0],
				args[1],
				args[2],
				args[3],
				args[4],
			)
			if err == nil {
				t.Error("validateIngestInput() error = nil, want validation error")
			}
		})
	}
}

func TestCitationIDAndControlCharacters(t *testing.T) {
	if got, want := citationID("docs/guide.md", 0), "docs/guide.md#chunk-1"; got != want {
		t.Errorf("citationID() = %q, want %q", got, want)
	}
	if !containsControlCharacter("line\nbreak") ||
		containsControlCharacter("normal text") {
		t.Error("containsControlCharacter() returned unexpected result")
	}
	if optionalString("") != nil {
		t.Error("optionalString(\"\") != nil")
	}
	if value := optionalString("heading"); value == nil || *value != "heading" {
		t.Errorf("optionalString() = %v", value)
	}
	if !strings.Contains(citationID("source", 2), "chunk-3") {
		t.Error("citation ID does not include one-based chunk number")
	}
}
