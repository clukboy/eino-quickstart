package tool

import (
	"context"
	"errors"
	"fmt"
	"strings"

	retrieval "eino-quickstart/internal/knowledge/retrieval"
	"eino-quickstart/internal/platform/auth"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

type KnowledgeSearch struct {
	Retriever    retrieval.Retriever
	ActorSubject string
}

type knowledgeSearchInput struct {
	Query string `json:"query" jsonschema_description:"Question or keywords to search in authorized knowledge documents"`
	TopK  int    `json:"topK,omitempty" jsonschema_description:"Optional number of results to return; use zero for the configured default"`
}

// NewKnowledgeSearch creates the search_knowledge Eino tool.
func NewKnowledgeSearch(retriever retrieval.Retriever, actorSubject string) (einotool.InvokableTool, error) {
	if retriever == nil {
		return nil, errors.New("knowledge retriever is required")
	}

	search := &KnowledgeSearch{
		Retriever:    retriever,
		ActorSubject: strings.TrimSpace(actorSubject),
	}
	return utils.InferTool("search_knowledge", "Search authorized knowledge documents and return cited source excerpts.", search.run)
}

// NewKnowledgeSearchTool is an alias for NewKnowledgeSearch.
func NewKnowledgeSearchTool(retriever retrieval.Retriever, actorSubject string) (einotool.InvokableTool, error) {
	return NewKnowledgeSearch(retriever, actorSubject)
}

func (s *KnowledgeSearch) run(ctx context.Context, input knowledgeSearchInput) (string, error) {
	if s == nil || s.Retriever == nil {
		return "", errors.New("knowledge retriever is required")
	}

	query := strings.TrimSpace(input.Query)
	if query == "" {
		return "", errors.New("knowledge query is required")
	}
	if input.TopK < 0 {
		return "", errors.New("knowledge topK must not be negative")
	}

	actorSubject := s.ActorSubject
	if identity, ok := auth.IdentityFromContext(ctx); ok {
		actorSubject = identity.Subject
	}
	actorSubject = strings.TrimSpace(actorSubject)
	if actorSubject == "" {
		return "", errors.New("authenticated actor subject is required")
	}

	results, err := s.Retriever.Search(ctx, actorSubject, query, input.TopK)
	if err != nil {
		return "", fmt.Errorf("search knowledge: %w", err)
	}
	if len(results) == 0 {
		return "No authorized knowledge-base results found.", nil
	}

	var output strings.Builder
	output.WriteString("Authorized knowledge search results:\n")
	for _, result := range results {
		citation := strings.TrimSpace(result.CitationID)
		if citation == "" {
			citation = fmt.Sprintf("%s#chunk-%d", result.Source, result.ChunkID)
		}

		fmt.Fprintf(&output, "\n[%s]\n", citation)
		fmt.Fprintf(&output, "Source: %s\n", result.Source)
		if result.Title != "" {
			fmt.Fprintf(&output, "Title: %s\n", result.Title)
		}
		if result.HeadingPath != "" {
			fmt.Fprintf(&output, "Section: %s\n", result.HeadingPath)
		}
		if result.StartLine > 0 || result.EndLine > 0 {
			fmt.Fprintf(
				&output,
				"Lines: %d-%d\n",
				result.StartLine,
				result.EndLine,
			)
		}
		fmt.Fprintf(&output, "Excerpt: %s\n", result.Content)
	}

	return output.String(), nil
}
