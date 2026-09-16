package parser

import (
	"context"
	"errors"
	"io"

	"github.com/cloudwego/eino/components/document/parser"
	"github.com/cloudwego/eino/schema"
)

// ExtParserConfig defines the configuration for the ExtParser.
type ParserConfig struct {
	// ext -> parser.
	// eg: map[string]Parser{
	// 	".pdf": &PDFParser{},
	// 	".md": &MarkdownParser{},
	// }
	Parsers map[string]parser.Parser

	// Fallback parser to use when no other parser is found.
	// Default is TextParser if not set.
	FallbackParser parser.Parser
}

type Parser struct {
	parsers map[string]parser.Parser

	fallbackParser parser.Parser
}

// NewParser creates a new Parser.
func NewParser(ctx context.Context, conf *ParserConfig) (*Parser, error) {
	if conf == nil {
		conf = &ParserConfig{}
	}

	p := &Parser{
		parsers:        conf.Parsers,
		fallbackParser: conf.FallbackParser,
	}

	if p.fallbackParser == nil {
		p.fallbackParser = parser.TextParser{}
	}

	if p.parsers == nil {
		p.parsers = make(map[string]parser.Parser)
	}

	return p, nil
}

// GetParsers returns a copy of the registered parsers.
// It is safe to modify the returned parsers.
func (p *Parser) GetParsers() map[string]parser.Parser {
	res := make(map[string]parser.Parser, len(p.parsers))
	for k, v := range p.parsers {
		res[k] = v
	}

	return res
}

// Parse parses the given reader and returns a list of documents.
func (p *Parser) Parse(ctx context.Context, reader io.Reader, opts ...parser.Option) ([]*schema.Document, error) {
	opt := parser.GetCommonOptions(&parser.Options{}, opts...)
	typ := opt.ExtraMeta["type"]

	parser, ok := p.parsers[typ.(string)]

	if !ok {
		parser = p.fallbackParser
	}

	if parser == nil {
		return nil, errors.New("no parser found for type " + typ.(string))
	}

	docs, err := parser.Parse(ctx, reader, opts...)
	if err != nil {
		return nil, err
	}

	for _, doc := range docs {
		if doc == nil {
			continue
		}

		if doc.MetaData == nil {
			doc.MetaData = make(map[string]any)
		}

		for k, v := range opt.ExtraMeta {
			doc.MetaData[k] = v
		}
	}

	return docs, nil
}
