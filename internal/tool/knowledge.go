package tool

import (
	"context"
	"eino-quickstart/ent"
	"eino-quickstart/ent/agentknowledgebase"
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
	Bindings     KnowledgeBaseBindings

	// AllowedKnowledgeBaseIDs supports fixed bindings for callers that do not
	// have an authenticated subject-to-knowledge-base resolver.
	//
	// LLM 不应该能够通过 tool 参数自行指定 KB。
	AllowedKnowledgeBaseIDs []uint64
}

type KnowledgeBaseBindings interface {
	KnowledgeBaseIDs(ctx context.Context, subject string) ([]uint64, error)
}

type EntKnowledgeBaseBindings struct {
	client *ent.Client
}

func NewEntKnowledgeBaseBindings(client *ent.Client) (*EntKnowledgeBaseBindings, error) {
	if client == nil {
		return nil, errors.New("knowledge binding database client is required")
	}
	return &EntKnowledgeBaseBindings{client: client}, nil
}

func (b *EntKnowledgeBaseBindings) KnowledgeBaseIDs(
	ctx context.Context,
	subject string,
) ([]uint64, error) {
	if b == nil || b.client == nil {
		return nil, errors.New("knowledge binding database client is required")
	}
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return nil, errors.New("authenticated actor subject is required")
	}
	bindings, err := b.client.AgentKnowledgeBase.Query().
		Where(agentknowledgebase.SubjectEQ(subject)).
		Order(agentknowledgebase.ByKnowledgeBaseID()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("query knowledge base bindings: %w", err)
	}
	ids := make([]uint64, 0, len(bindings))
	for _, binding := range bindings {
		ids = append(ids, binding.KnowledgeBaseID)
	}
	return normalizeKnowledgeBaseIDs(ids), nil
}

type knowledgeSearchInput struct {
	Query string `json:"query" jsonschema_description:"Question or keywords to search in authorized knowledge documents"`
	TopK  int    `json:"topK,omitempty" jsonschema_description:"Optional number of results to return; use zero for the configured default"`
}

// NewKnowledgeSearch creates the search_knowledge Eino tool.
func NewKnowledgeSearch(retriever retrieval.Retriever, actorSubject string) (einotool.InvokableTool, error) {
	return nil, errors.New(
		"knowledge search requires an explicit knowledge base allowlist",
	)
}

func NewKnowledgeSearchWithKnowledgeBases(retriever retrieval.Retriever, actorSubject string, knowledgeBaseIDs []uint64) (einotool.InvokableTool, error) {
	if retriever == nil {
		return nil, errors.New("knowledge retriever is required")
	}
	allowedKnowledgeBaseIDs := normalizeKnowledgeBaseIDs(knowledgeBaseIDs)
	if len(allowedKnowledgeBaseIDs) == 0 {
		return nil, errors.New(
			"knowledge search requires at least one allowed knowledge base",
		)
	}

	search := &KnowledgeSearch{
		Retriever:               retriever,
		ActorSubject:            strings.TrimSpace(actorSubject),
		AllowedKnowledgeBaseIDs: allowedKnowledgeBaseIDs,
	}
	return utils.InferTool("search_knowledge", "Search authorized knowledge documents and return cited source excerpts.", search.run)
}

// NewKnowledgeSearchWithBindings creates a search tool whose KB whitelist is
// resolved from the authenticated subject every time the tool is invoked.
func NewKnowledgeSearchWithBindings(
	retriever retrieval.Retriever,
	actorSubject string,
	bindings KnowledgeBaseBindings,
) (einotool.InvokableTool, error) {
	if retriever == nil {
		return nil, errors.New("knowledge retriever is required")
	}
	if bindings == nil {
		return nil, errors.New("knowledge base bindings are required")
	}
	return utils.InferTool(
		"search_knowledge",
		"Search authorized knowledge documents and return cited source excerpts.",
		(&KnowledgeSearch{
			Retriever:    retriever,
			ActorSubject: strings.TrimSpace(actorSubject),
			Bindings:     bindings,
		}).run,
	)
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
	knowledgeBaseIDs := s.AllowedKnowledgeBaseIDs
	if s.Bindings != nil {
		resolvedIDs, err := s.Bindings.KnowledgeBaseIDs(ctx, actorSubject)
		if err != nil {
			return "", fmt.Errorf("resolve knowledge base bindings: %w", err)
		}
		knowledgeBaseIDs = resolvedIDs
	}

	results, err := s.Retriever.Search(ctx, retrieval.SearchRequest{
		ActorSubject:     actorSubject,
		Query:            query,
		TopK:             input.TopK,
		KnowledgeBaseIDs: knowledgeBaseIDs,
	})
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
			fmt.Fprintf(&output, "Lines: %d-%d\n", result.StartLine, result.EndLine)
		}
		fmt.Fprintf(&output, "Excerpt: %s\n", result.Content)
	}

	return output.String(), nil
}

func normalizeKnowledgeBaseIDs(knowledgeBaseIDs []uint64) []uint64 {
	if len(knowledgeBaseIDs) == 0 {
		return nil
	}
	result := make([]uint64, 0, len(knowledgeBaseIDs))
	seen := make(map[uint64]struct{}, len(knowledgeBaseIDs))
	for _, id := range knowledgeBaseIDs {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}
