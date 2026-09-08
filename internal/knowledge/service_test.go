package knowledge

import (
	"strings"
	"testing"

	"eino-quickstart/ent/document"
)

func TestValidateIngestRequest(t *testing.T) {
	input, err := validateIngestRequest(IngestRequest{
		Source: " docs/guide.md ", Title: " Guide ", Content: "\n# Guide\ncontent\n",
		Target: IngestTarget{KnowledgeBaseID: 1, Visibility: "SYSTEM"},
	})
	if err != nil {
		t.Fatalf("validateIngestInput() error = %v", err)
	}
	if input.Source != "docs/guide.md" || input.Title != "Guide" ||
		input.Visibility != document.VisibilitySystem ||
		input.OwnerSubject != "system" {
		t.Errorf("validated input = %#v", input)
	}
	if input.Content != "\n# Guide\ncontent\n" {
		t.Errorf("content = %q, want original content for source line citations", input.Content)
	}

	for name, request := range map[string]IngestRequest{
		"empty source":       {Title: "title", Content: "content", Target: IngestTarget{KnowledgeBaseID: 1, OwnerSubject: "system", Visibility: "system"}},
		"empty title":        {Source: "source", Content: "content", Target: IngestTarget{KnowledgeBaseID: 1, OwnerSubject: "system", Visibility: "system"}},
		"empty content":      {Source: "source", Title: "title", Content: " \n ", Target: IngestTarget{KnowledgeBaseID: 1, OwnerSubject: "system", Visibility: "system"}},
		"missing base":       {Source: "source", Title: "title", Content: "content", Target: IngestTarget{OwnerSubject: "system", Visibility: "system"}},
		"private no owner":   {Source: "source", Title: "title", Content: "content", Target: IngestTarget{KnowledgeBaseID: 1, Visibility: "private"}},
		"invalid visibility": {Source: "source", Title: "title", Content: "content", Target: IngestTarget{KnowledgeBaseID: 1, OwnerSubject: "owner", Visibility: "public"}},
		"control in source":  {Source: "source\nnext", Title: "title", Content: "content", Target: IngestTarget{KnowledgeBaseID: 1, OwnerSubject: "system", Visibility: "system"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := validateIngestRequest(request)
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
