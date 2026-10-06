package tool

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"eino-quickstart/ent"
	"eino-quickstart/ent/agentdataset"
	"eino-quickstart/ent/dataset"
	"eino-quickstart/internal/application/knowledge"
	"eino-quickstart/internal/platform/auth"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

type KnowledgeSearch struct {
	ActorSubject string
	Bindings     DatasetBindings
	Service      KnowledgeSearchService

	// AllowedDatasetIDs supports fixed bindings for callers that do not
	// have an authenticated subject-to-dataset resolver.
	//
	// LLM 不应该能够通过 tool 参数自行指定 dataset。
	AllowedDatasetIDs []uint64
}

type DatasetBindings interface {
	DatasetIDs(ctx context.Context, subject string) ([]uint64, error)
}

type KnowledgeSearchService interface {
	Search(ctx context.Context, input knowledge.SearchInput) (knowledge.SearchOutcome, error)
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
		Where(
			agentdataset.SubjectEQ(subject),
			agentdataset.HasDatasetWith(dataset.StatusEQ(dataset.StatusACTIVE)),
		).
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

func NewKnowledgeSearchWithDatasets(actorSubject string, datasetIDs []uint64, service KnowledgeSearchService) (tool.InvokableTool, error) {
	if service == nil {
		return nil, errors.New("knowledge search service is required")
	}
	allowedDatasetIDs := normalizeDatasetIDs(datasetIDs)
	if len(allowedDatasetIDs) == 0 {
		return nil, errors.New(
			"knowledge search requires at least one allowed dataset",
		)
	}

	search := &KnowledgeSearch{
		Service:           service,
		ActorSubject:      strings.TrimSpace(actorSubject),
		AllowedDatasetIDs: allowedDatasetIDs,
	}
	return utils.InferTool("search_knowledge", "Search authorized knowledge documents and return cited source excerpts.", search.run)
}

// NewKnowledgeSearchWithBindings creates a search tool whose dataset whitelist
// is resolved from the authenticated subject every time the tool is invoked.
func NewKnowledgeSearchWithBindings(actorSubject string, bindings DatasetBindings, service KnowledgeSearchService) (tool.InvokableTool, error) {
	if service == nil {
		return nil, errors.New("knowledge search service is required")
	}
	if bindings == nil {
		return nil, errors.New("dataset bindings are required")
	}
	return utils.InferTool(
		"search_knowledge",
		"Search authorized knowledge documents and return cited source excerpts.",
		(&KnowledgeSearch{
			Service:      service,
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
	if s == nil || s.Service == nil {
		return "", errors.New("knowledge search is not initialized")
	}

	query := strings.TrimSpace(input.Query)
	if query == "" {
		return "", errors.New("knowledge query is required")
	}
	if input.TopK < 0 {
		return "", errors.New("knowledge topK must not be negative")
	}

	actorSubject := strings.TrimSpace(s.ActorSubject)
	if identity, ok := auth.IdentityFromContext(ctx); ok {
		actorSubject = strings.TrimSpace(identity.Subject)
	}

	datasetIDs := s.AllowedDatasetIDs
	if s.Bindings != nil {
		if actorSubject == "" {
			return "", errors.New("authenticated actor subject is required")
		}
		resolvedIDs, err := s.Bindings.DatasetIDs(ctx, actorSubject)
		if err != nil {
			return "", fmt.Errorf("resolve dataset bindings: %w", err)
		}
		datasetIDs = resolvedIDs
	}
	if len(datasetIDs) == 0 {
		return "No authorized knowledge-base results found.", nil
	}

	results := make([]knowledge.SearchHit, 0)
	limit := input.TopK
	var firstErr error
	for _, datasetID := range datasetIDs {
		outcome, err := s.Service.Search(ctx, knowledge.SearchInput{
			DatasetID: datasetID,
			Query:     query,
			TopK:      input.TopK,
		})
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if limit <= 0 && outcome.TopK > 0 {
			limit = outcome.TopK
		}
		results = append(results, outcome.Hits...)
	}
	if len(results) == 0 && firstErr != nil {
		return "", fmt.Errorf("search knowledge: %w", firstErr)
	}

	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	if limit <= 0 {
		limit = 5
	}
	if len(results) > limit {
		results = results[:limit]
	}
	if len(results) == 0 {
		return "No authorized knowledge-base results found.", nil
	}

	var output strings.Builder
	output.WriteString("Authorized knowledge search results:\n")
	for _, result := range results {
		source := strings.TrimSpace(result.Source)
		if source == "" {
			source = fmt.Sprintf("document-%d", result.DocumentID)
		}
		citation := fmt.Sprintf("%s#chunk-%d", source, result.ChunkID)

		fmt.Fprintf(&output, "\n[%s]\n", citation)
		fmt.Fprintf(&output, "Source: %s\n", source)
		if result.Title != "" {
			fmt.Fprintf(&output, "Title: %s\n", result.Title)
		}
		if result.HeadingPath != "" {
			fmt.Fprintf(&output, "Section: %s\n", result.HeadingPath)
		}
		fmt.Fprintf(&output, "Excerpt: %s\n", result.Content)
		if result.Truncated {
			output.WriteString("Note: excerpt truncated by the knowledge service.\n")
		}
	}

	return output.String(), nil
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
