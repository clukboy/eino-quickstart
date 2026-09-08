package knowledge

import "testing"

func TestMetadataCopiesCallerValuesAndBuildsChunkMetadata(t *testing.T) {
	input := map[string]any{"source": "manual"}
	metadata := NewMetadata(input)
	input["source"] = "changed"

	document := metadata.storageValue()
	if document["source"] != "manual" {
		t.Fatalf("document metadata = %#v, want copied caller value", document)
	}

	chunk := metadata.forChunk("Installation", "安装步骤").storageValue()
	if chunk["source"] != "manual" {
		t.Errorf("chunk metadata lost document value: %#v", chunk)
	}
	if _, found := chunk["topics"]; !found {
		t.Errorf("chunk metadata missing detected topics: %#v", chunk)
	}
	if _, found := document["topics"]; found {
		t.Errorf("document metadata was mutated by chunk enrichment: %#v", document)
	}
}
