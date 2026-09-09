package domain

type ChunkType string

const (
	ChunkTypeGeneral         ChunkType = "general"
	ChunkTypeFAQ             ChunkType = "faq"
	ChunkTypeQAPair          ChunkType = "qa_pair"
	ChunkTypeSpecification   ChunkType = "specification"
	ChunkTypeInstruction     ChunkType = "instruction"
	ChunkTypeTroubleshooting ChunkType = "troubleshooting"
	ChunkTypeSummary         ChunkType = "summary"
)

type ChunkPosition struct {
	Index       int
	StartOffset int
	EndOffset   int
	HeadingPath []string
}

// Chunk is the smallest retrievable unit.
type Chunk struct {
	ID         string
	DocumentID string

	Content string

	ParentID string
	Type     ChunkType

	Position ChunkPosition

	Metadata Metadata
}
