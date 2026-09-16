package tool

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"eino-quickstart/ent"
	"eino-quickstart/ent/agentdataset"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

type KnowledgeSearch struct {
	ActorSubject string
	Bindings     DatasetBindings

	// AllowedDatasetIDs supports fixed bindings for callers that do not
	// have an authenticated subject-to-dataset resolver.
	//
	// LLM 不应该能够通过 tool 参数自行指定 dataset。
	AllowedDatasetIDs []uint64
}

type DatasetBindings interface {
	DatasetIDs(ctx context.Context, subject string) ([]uint64, error)
}

type EntDatasetBindings struct {
	client *ent.Client
}

func NewEntDatasetBindings(client *ent.Client) (*EntDatasetBindings, error) {
	if client == nil {
		return nil, errors.New("dataset binding database client is required")
	}
	return &EntDatasetBindings{client: client}, nil
}

func (b *EntDatasetBindings) DatasetIDs(
	ctx context.Context,
	subject string,
) ([]uint64, error) {
	if b == nil || b.client == nil {
		return nil, errors.New("dataset binding database client is required")
	}
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return nil, errors.New("authenticated actor subject is required")
	}
	bindings, err := b.client.AgentDataset.Query().
		Where(agentdataset.SubjectEQ(subject)).
		Order(agentdataset.ByDatasetID()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("query dataset bindings: %w", err)
	}
	ids := make([]uint64, 0, len(bindings))
	for _, binding := range bindings {
		ids = append(ids, binding.DatasetID)
	}
	return normalizeDatasetIDs(ids), nil
}

type knowledgeSearchInput struct {
	Query string `json:"query" jsonschema_description:"Question or keywords to search in authorized knowledge documents"`
	TopK  int    `json:"topK,omitempty" jsonschema_description:"Optional number of results to return; use zero for the configured default"`
}

// NewKnowledgeSearch creates the search_knowledge Eino tool.
func NewKnowledgeSearch(actorSubject string) (tool.InvokableTool, error) {
	return nil, errors.New(
		"knowledge search requires an explicit dataset allowlist",
	)
}

func NewKnowledgeSearchWithDatasets(actorSubject string, datasetIDs []uint64) (tool.InvokableTool, error) {
	// if retriever == nil {
	// 	return nil, errors.New("knowledge retriever is required")
	// }
	allowedDatasetIDs := normalizeDatasetIDs(datasetIDs)
	if len(allowedDatasetIDs) == 0 {
		return nil, errors.New(
			"knowledge search requires at least one allowed dataset",
		)
	}

	search := &KnowledgeSearch{
		// Retriever:        retriever,
		ActorSubject:      strings.TrimSpace(actorSubject),
		AllowedDatasetIDs: allowedDatasetIDs,
	}
	return utils.InferTool("search_knowledge", "Search authorized knowledge documents and return cited source excerpts.", search.run)
}

// NewKnowledgeSearchWithBindings creates a search tool whose dataset whitelist
// is resolved from the authenticated subject every time the tool is invoked.
func NewKnowledgeSearchWithBindings(actorSubject string, bindings DatasetBindings) (tool.InvokableTool, error) {
	// if retriever == nil {
	// 	return nil, errors.New("knowledge retriever is required")
	// }
	if bindings == nil {
		return nil, errors.New("dataset bindings are required")
	}
	return utils.InferTool(
		"search_knowledge",
		"Search authorized knowledge documents and return cited source excerpts.",
		(&KnowledgeSearch{
			ActorSubject: strings.TrimSpace(actorSubject),
			Bindings:     bindings,
		}).run,
	)
}

// NewKnowledgeSearchTool is an alias for NewKnowledgeSearch.
func NewKnowledgeSearchTool(actorSubject string) (tool.InvokableTool, error) {
	return NewKnowledgeSearch(actorSubject)
}

func (s *KnowledgeSearch) run(ctx context.Context, input knowledgeSearchInput) (string, error) {
	// if s == nil {
	// 	return "", errors.New("knowledge search is not initialized")
	// }

	// query := strings.TrimSpace(input.Query)
	// if query == "" {
	// 	return "", errors.New("knowledge query is required")
	// }
	// if input.TopK < 0 {
	// 	return "", errors.New("knowledge topK must not be negative")
	// }

	// actorSubject := s.ActorSubject
	// if identity, ok := auth.IdentityFromContext(ctx); ok {
	// 	actorSubject = identity.Subject
	// }
	// actorSubject = strings.TrimSpace(actorSubject)
	// if actorSubject == "" {
	// 	return "", errors.New("authenticated actor subject is required")
	// }
	// datasetIDs := s.AllowedDatasetIDs
	// if s.Bindings != nil {
	// 	resolvedIDs, err := s.Bindings.DatasetIDs(ctx, actorSubject)
	// 	if err != nil {
	// 		return "", fmt.Errorf("resolve dataset bindings: %w", err)
	// 	}
	// 	datasetIDs = resolvedIDs
	// }

	// results, err := s.Retriever.Search(ctx, retrieval.SearchRequest{
	// 	ActorSubject: actorSubject,
	// 	Query:        query,
	// 	TopK:         input.TopK,
	// 	DatasetIDs:   datasetIDs,
	// })
	// if err != nil {
	// 	return "", fmt.Errorf("search knowledge: %w", err)
	// }
	// if len(results) == 0 {
	// 	return "No authorized knowledge-base results found.", nil
	// }

	// var output strings.Builder
	// output.WriteString("Authorized knowledge search results:\n")
	// for _, result := range results {
	// 	citation := strings.TrimSpace(result.CitationID)
	// 	if citation == "" {
	// 		citation = fmt.Sprintf("%s#chunk-%d", result.Source, result.ChunkID)
	// 	}

	// 	fmt.Fprintf(&output, "\n[%s]\n", citation)
	// 	fmt.Fprintf(&output, "Source: %s\n", result.Source)
	// 	if result.Title != "" {
	// 		fmt.Fprintf(&output, "Title: %s\n", result.Title)
	// 	}
	// 	if result.HeadingPath != "" {
	// 		fmt.Fprintf(&output, "Section: %s\n", result.HeadingPath)
	// 	}
	// 	if result.StartLine > 0 || result.EndLine > 0 {
	// 		fmt.Fprintf(&output, "Lines: %d-%d\n", result.StartLine, result.EndLine)
	// 	}
	// 	fmt.Fprintf(&output, "Excerpt: %s\n", result.Content)
	// }

	// return output.String(), nil
	return "", nil
}

func normalizeDatasetIDs(datasetIDs []uint64) []uint64 {
	if len(datasetIDs) == 0 {
		return nil
	}
	result := make([]uint64, 0, len(datasetIDs))
	seen := make(map[uint64]struct{}, len(datasetIDs))
	for _, id := range datasetIDs {
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
