package chunker

import (
	"context"
	"eino-quickstart/internal/rag/domain"
	"strings"
)

type MarkdownChunkerConfig struct {
	MaxChars int
	Overlap  int
}

type MarkdownChunker struct {
	config MarkdownChunkerConfig
}

func NewMarkdownChunker(config MarkdownChunkerConfig) *MarkdownChunker {
	if config.MaxChars <= 0 {
		config.MaxChars = 1200
	}

	if config.Overlap < 0 {
		config.Overlap = 0
	}

	if config.Overlap >= config.MaxChars {
		config.Overlap = config.MaxChars / 6
	}

	return &MarkdownChunker{
		config: config,
	}
}

func (c *MarkdownChunker) Chunk(ctx context.Context, document *domain.Document) ([]*domain.Chunk, error) {
	if document == nil {
		return nil, domain.ErrInvalidDocument
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	sections := splitMarkdown(document.Content)

	var chunks []*domain.Chunk

	for _, section := range sections {
		if strings.TrimSpace(section.Content) == "" {
			continue
		}

		content := strings.TrimSpace(section.Content)

		chunks = append(
			chunks,
			&domain.Chunk{
				DocumentID: document.ID,
				Content:    content,
				Type:       domain.ChunkTypeGeneral,
				Position: domain.ChunkPosition{
					Index:       len(chunks),
					HeadingPath: section.Headings,
				},
				Metadata: document.Metadata.Clone(),
			},
		)
	}
	return chunks, nil
}

type markdownSection struct {
	Content  string
	Headings []string
}

func splitMarkdown(content string) []markdownSection {
	lines := strings.Split(content, "\n")

	var sections []markdownSection

	var current strings.Builder

	var headings []string

	flush := func() {
		if strings.TrimSpace(current.String()) == "" {
			return
		}

		sections = append(
			sections,
			markdownSection{
				Content:  current.String(),
				Headings: append([]string(nil), headings...),
			},
		)

		current.Reset()
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "#") {
			level := headingLevel(trimmed)

			if level > 0 {
				flush()

				title := strings.TrimSpace(
					strings.TrimLeft(trimmed, "#"),
				)

				if level <= len(headings) {
					headings = headings[:level-1]
				}

				headings = append(headings, title)

				current.WriteString(line)
				current.WriteByte('\n')

				continue
			}
		}

		current.WriteString(line)
		current.WriteByte('\n')
	}

	flush()

	return sections
}

func headingLevel(line string) int {
	count := 0

	for _, r := range line {
		if r == '#' {
			count++
			continue
		}

		break
	}

	if count > 6 {
		return 0
	}

	if count == 0 {
		return 0
	}

	return count
}
