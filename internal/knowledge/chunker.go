package knowledge

import (
	"fmt"
	"strings"
)

type Chunk struct {
	Content     string
	HeadingPath string
	StartLine   int
	EndLine     int
}

type Chunker struct {
	Size    int
	Overlap int
}

func (c Chunker) Split(text string) ([]Chunk, error) {
	if c.Size <= 0 {
		return nil, fmt.Errorf("chunk size must be greater than zero")
	}

	if c.Overlap < 0 || c.Overlap >= c.Size {
		return nil, fmt.Errorf(
			"chunk overlap must be non-negative and smaller than chunk size",
		)
	}

	lines := strings.Split(text, "\n")
	headings := make([]string, 0)
	blocks := make([]Chunk, 0)

	var buffer strings.Builder
	startLine := 1
	currentLine := 1

	flush := func(endLine int) {
		content := strings.TrimSpace(buffer.String())
		if content == "" {
			return
		}

		blocks = append(blocks, Chunk{
			Content:     content,
			HeadingPath: strings.Join(headings, " > "),
			StartLine:   startLine,
			EndLine:     endLine,
		})

		buffer.Reset()
	}

	for _, line := range lines {
		if level, title, ok := markdownHeading(line); ok {
			flush(currentLine - 1)

			for len(headings) >= level {
				headings = headings[:len(headings)-1]
			}
			headings = append(headings, title)

			startLine = currentLine
			buffer.WriteString(line)
			buffer.WriteByte('\n')
			currentLine++
			continue
		}

		if buffer.Len() == 0 {
			startLine = currentLine
		}

		buffer.WriteString(line)
		buffer.WriteByte('\n')
		currentLine++
	}

	flush(currentLine - 1)

	return c.splitLargeBlocks(blocks), nil
}

func (c Chunker) splitLargeBlocks(blocks []Chunk) []Chunk {
	result := make([]Chunk, 0, len(blocks))

	for _, block := range blocks {
		runes := []rune(block.Content)

		if len(runes) <= c.Size {
			result = append(result, block)
			continue
		}

		for start := 0; start < len(runes); {
			end := start + c.Size
			if end > len(runes) {
				end = len(runes)
			}

			result = append(result, Chunk{
				Content:     strings.TrimSpace(string(runes[start:end])),
				HeadingPath: block.HeadingPath,
				StartLine:   block.StartLine,
				EndLine:     block.EndLine,
			})

			if end == len(runes) {
				break
			}

			start = end - c.Overlap
		}
	}

	return result
}

func markdownHeading(line string) (
	level int,
	title string,
	ok bool,
) {
	trimmed := strings.TrimSpace(line)

	for level < len(trimmed) &&
		trimmed[level] == '#' {
		level++
	}

	if level == 0 || level > 6 {
		return 0, "", false
	}

	if len(trimmed) == level ||
		trimmed[level] != ' ' {
		return 0, "", false
	}

	title = strings.TrimSpace(trimmed[level:])
	return level, title, title != ""
}
