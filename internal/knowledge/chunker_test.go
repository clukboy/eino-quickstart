package knowledge

import (
	"strings"
	"testing"
)

func TestChunkerPreservesMarkdownContextAndOverlap(t *testing.T) {
	chunker := Chunker{Size: 20, Overlap: 5}
	text := "# Guide\nIntroduction\n## Setup\n" + strings.Repeat("a", 30)

	chunks, err := chunker.Split(text)
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}
	if len(chunks) != 4 {
		t.Fatalf("Split() returned %d chunks, want 4", len(chunks))
	}

	if got, want := chunks[0], (Chunk{
		Content:     "# Guide\nIntroduction",
		HeadingPath: "Guide",
		StartLine:   1,
		EndLine:     2,
	}); got != want {
		t.Errorf("first chunk = %#v, want %#v", got, want)
	}
	for index := 1; index < len(chunks); index++ {
		if got, want := chunks[index].HeadingPath, "Guide > Setup"; got != want {
			t.Errorf("chunk %d heading path = %q, want %q", index, got, want)
		}
	}
	if got, want := chunks[1].Content[len(chunks[1].Content)-5:], chunks[2].Content[:5]; got != want {
		t.Errorf("overlap = %q and %q, want equal text", got, want)
	}
	if got, want := chunks[2].Content[len(chunks[2].Content)-5:], chunks[3].Content[:5]; got != want {
		t.Errorf("overlap = %q and %q, want equal text", got, want)
	}
	for _, chunk := range chunks[1:] {
		if chunk.StartLine != 3 || chunk.EndLine != 4 {
			t.Errorf("large block line range = %#v, want 3-4", chunk)
		}
	}
}

func TestChunkerRejectsInvalidConfiguration(t *testing.T) {
	for _, chunker := range []Chunker{
		{Size: 0, Overlap: 0},
		{Size: 10, Overlap: -1},
		{Size: 10, Overlap: 10},
	} {
		if _, err := chunker.Split("content"); err == nil {
			t.Errorf("Split() with %#v error = nil, want validation error", chunker)
		}
	}
}
