package knowledge

import (
	"context"
	"crypto/sha256"
	"eino-quickstart/ent"
	"eino-quickstart/ent/document"
	"eino-quickstart/ent/documentchunk"
	"eino-quickstart/ent/vectoroutbox"
	"eino-quickstart/internal/knowledge/types/product"
	"eino-quickstart/internal/platform/storage/entx"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Service struct {
	client          *ent.Client
	chunker         Chunker
	maxChunksPerDoc int
	embeddingModel  string
}

// ServiceConfig configures document ingestion and vector-outbox creation.
type ServiceConfig struct {
	Client          *ent.Client
	Chunker         Chunker
	MaxChunksPerDoc int
	EmbeddingModel  string
}

// IngestRootResult reports the documents discovered and committed from a root.
type IngestRootResult struct {
	Loaded   int
	Ingested int
}

// IngestTarget identifies the destination and access boundary for documents.
// Sources only load content; callers supply this target when ingesting it.
type IngestTarget struct {
	KnowledgeBaseID uint64
	FolderID        *uint64
	Metadata        Metadata
	OwnerSubject    string
	Visibility      string
}

type IngestRootRequest struct {
	Loader *Loader
	Target IngestTarget
}

type IngestRequest struct {
	Source  string
	Title   string
	Content string

	Target IngestTarget
}

var errSourceCreateConflict = errors.New("document source creation conflict")

// NewService creates a knowledge ingestion service.
func NewService(config ServiceConfig) (*Service, error) {
	service := &Service{
		client:          config.Client,
		chunker:         config.Chunker,
		maxChunksPerDoc: config.MaxChunksPerDoc,
		embeddingModel:  strings.TrimSpace(config.EmbeddingModel),
	}
	if service.client == nil {
		return nil, errors.New("knowledge ent client is required")
	}
	if service.chunker.Size <= 0 {
		return nil, errors.New("chunk size must be greater than zero")
	}
	if service.chunker.Overlap < 0 ||
		service.chunker.Overlap >= service.chunker.Size {
		return nil, errors.New(
			"chunk overlap must be non-negative and smaller than chunk size",
		)
	}
	if service.maxChunksPerDoc <= 0 {
		return nil, errors.New(
			"maximum chunks per document must be greater than zero",
		)
	}
	if service.embeddingModel == "" {
		return nil, errors.New("embedding model is required")
	}
	return service, nil
}

// IngestRoot loads documents from a loader and ingests them in source order.
func (s *Service) IngestRoot(ctx context.Context, request IngestRootRequest) (IngestRootResult, error) {
	if request.Loader == nil {
		return IngestRootResult{}, errors.New("knowledge loader is required")
	}
	documents, err := request.Loader.Load(ctx)
	if err != nil {
		return IngestRootResult{}, err
	}

	result := IngestRootResult{Loaded: len(documents)}
	for _, item := range documents {
		if err := s.Ingest(ctx, IngestRequest{
			Source:  item.Source,
			Title:   item.Title,
			Content: item.Content,
			Target:  request.Target,
		}); err != nil {
			return result, fmt.Errorf(
				"ingest loaded document %q: %w",
				item.Source,
				err,
			)
		}
		result.Ingested++
	}
	return result, nil
}

func (s *Service) Ingest(ctx context.Context, req IngestRequest) error {
	if s == nil {
		return errors.New("knowledge service is nil")
	}
	if s.client == nil {
		return errors.New("knowledge ent client is required")
	}
	if ctx == nil {
		return errors.New("ingestion context is required")
	}
	if s.maxChunksPerDoc <= 0 {
		return errors.New("maximum chunks per document must be greater than zero")
	}
	if strings.TrimSpace(s.embeddingModel) == "" {
		return errors.New("embedding model is required")
	}

	input, err := validateIngestRequest(req)
	if err != nil {
		return err
	}
	productInfo, err := product.TryParse(input.Content)
	if err != nil {
		return fmt.Errorf("parse product metadata: %w", err)
	}

	chunks, err := s.chunker.Split(input.Content)
	if err != nil {
		return fmt.Errorf("split document into chunks: %w", err)
	}
	if len(chunks) == 0 {
		return errors.New("document content produced no chunks")
	}
	if len(chunks) > s.maxChunksPerDoc {
		return fmt.Errorf(
			"document has %d chunks, exceeding the maximum of %d",
			len(chunks),
			s.maxChunksPerDoc,
		)
	}

	documentMetadata := input.Metadata.withProduct(productInfo)

	checksum := fmt.Sprintf("%x", sha256.Sum256([]byte(input.Content)))
	for attempt := 0; attempt < 2; attempt++ {
		err = s.ingestOnce(ctx, input, documentMetadata, chunks, checksum)
		if !errors.Is(err, errSourceCreateConflict) {
			break
		}
	}
	if err != nil {
		return fmt.Errorf("ingest document: %w", err)
	}

	return nil
}

type ingestInput struct {
	KnowledgeBaseID uint64
	FolderID        *uint64

	Source  string
	Title   string
	Content string

	Metadata     Metadata
	OwnerSubject string
	Visibility   document.Visibility
}

func validateIngestRequest(req IngestRequest) (ingestInput, error) {
	input := ingestInput{
		Source:          strings.TrimSpace(req.Source),
		Title:           strings.TrimSpace(req.Title),
		Content:         req.Content,
		OwnerSubject:    strings.TrimSpace(req.Target.OwnerSubject),
		Visibility:      document.Visibility(strings.ToLower(strings.TrimSpace(req.Target.Visibility))),
		Metadata:        NewMetadata(req.Target.Metadata.values),
		KnowledgeBaseID: req.Target.KnowledgeBaseID,
		FolderID:        cloneUint64Pointer(req.Target.FolderID),
	}

	if input.Source == "" {
		return ingestInput{}, errors.New("document source is required")
	}
	if input.KnowledgeBaseID == 0 {
		return ingestInput{}, errors.New("knowledge base id is required")
	}
	if input.FolderID != nil && *input.FolderID == 0 {
		return ingestInput{}, errors.New("folder id must be greater than zero")
	}
	if input.Title == "" {
		return ingestInput{}, errors.New("document title is required")
	}
	if strings.TrimSpace(input.Content) == "" {
		return ingestInput{}, errors.New("document content is required")
	}
	if containsControlCharacter(input.Source) {
		return ingestInput{}, errors.New("document source contains a control character")
	}
	if containsControlCharacter(input.Title) {
		return ingestInput{}, errors.New("document title contains a control character")
	}
	if containsControlCharacter(input.OwnerSubject) {
		return ingestInput{}, errors.New("document owner subject contains a control character")
	}

	switch input.Visibility {
	case document.VisibilitySystem:
		if input.OwnerSubject == "" {
			input.OwnerSubject = "system"
		}
	case document.VisibilityPrivate:
		if input.OwnerSubject == "" {
			return ingestInput{}, errors.New("private document owner subject is required")
		}
	default:
		return ingestInput{}, fmt.Errorf(
			"document visibility %q must be system or private",
			req.Target.Visibility,
		)
	}

	return input, nil
}

func (s *Service) ingestOnce(ctx context.Context, input ingestInput, metadata Metadata, chunks []Chunk, checksum string) (err error) {
	return entx.WithTx(ctx, s.client, func(tx *ent.Tx) error {
		client := tx.Client()
		doc, created, err := upsertDocument(
			ctx,
			client,
			input,
			metadata.storageValue(),
			checksum,
		)
		if err != nil {
			if created && ent.IsConstraintError(err) {
				return errSourceCreateConflict
			}
			return err
		}

		previousChunks, err := client.DocumentChunk.Query().
			Where(documentchunk.HasDocumentWith(document.IDEQ(doc.ID))).
			All(ctx)
		if err != nil {
			return fmt.Errorf("load existing document chunks: %w", err)
		}
		for _, chunk := range previousChunks {
			if err := queueVectorOperation(
				ctx,
				client,
				chunk.ID,
				vectoroutbox.OperationDelete,
			); err != nil {
				return err
			}
		}

		if len(previousChunks) > 0 {
			if _, err := client.DocumentChunk.Delete().
				Where(documentchunk.HasDocumentWith(document.IDEQ(doc.ID))).
				Exec(ctx); err != nil {
				return fmt.Errorf("delete existing document chunks: %w", err)
			}
		}

		for index, chunk := range chunks {
			chunkMetadata := metadata.forChunk(
				chunk.HeadingPath,
				chunk.Content,
			).storageValue()
			record, err := client.DocumentChunk.Create().
				SetDocumentID(doc.ID).
				SetChunkIndex(index).
				SetCitationID(citationID(input.Source, index)).
				SetContent(chunk.Content).
				SetNillableHeadingPath(optionalString(chunk.HeadingPath)).
				SetStartLine(chunk.StartLine).
				SetEndLine(chunk.EndLine).
				SetMetadata(chunkMetadata).
				SetCharacterCount(len([]rune(chunk.Content))).
				SetEmbeddingModel(strings.TrimSpace(s.embeddingModel)).
				SetVectorStatus(documentchunk.VectorStatusPending).
				Save(ctx)
			if err != nil {
				return fmt.Errorf("create document chunk %d: %w", index, err)
			}

			if err := queueVectorOperation(ctx, client, record.ID, vectoroutbox.OperationUpsert); err != nil {
				return err
			}
		}

		return nil
	})
}

func upsertDocument(ctx context.Context, client *ent.Client, input ingestInput, metadata map[string]any, checksum string) (*ent.Document, bool, error) {
	existing, err := client.Document.Query().
		Where(document.SourceEQ(input.Source), document.KnowledgeBaseID(input.KnowledgeBaseID)).
		Only(ctx)
	if err == nil {
		updated, updateErr := existing.Update().
			SetTitle(input.Title).
			SetMetadata(metadata).
			SetChecksum(checksum).
			SetOwnerSubject(input.OwnerSubject).
			SetVisibility(input.Visibility).
			SetNillableFolderID(input.FolderID).
			SetStatus(document.StatusIndexing).
			Save(ctx)
		if updateErr != nil {
			return nil, false, fmt.Errorf("update document: %w", updateErr)
		}

		return updated, false, nil
	}
	if !ent.IsNotFound(err) {
		return nil, false, fmt.Errorf("look up document by source: %w", err)
	}

	created, err := client.Document.Create().
		SetKnowledgeBaseID(input.KnowledgeBaseID).
		SetSource(input.Source).
		SetTitle(input.Title).
		SetMetadata(metadata).
		SetChecksum(checksum).
		SetOwnerSubject(input.OwnerSubject).
		SetVisibility(input.Visibility).
		SetNillableFolderID(input.FolderID).
		SetStatus(document.StatusIndexing).
		Save(ctx)
	if err != nil {
		return nil, true, fmt.Errorf("create document: %w", err)
	}

	return created, true, nil
}

func queueVectorOperation(ctx context.Context, client *ent.Client, chunkID uint64, operation vectoroutbox.Operation) error {
	existing, err := client.VectorOutbox.Query().
		Where(vectoroutbox.ChunkIDEQ(chunkID)).
		Only(ctx)
	if err == nil {
		if err := existing.Update().
			SetOperation(operation).
			SetStatus(vectoroutbox.StatusPending).
			SetAttempts(0).
			SetAvailableAt(time.Now()).
			ClearLockedUntil().
			ClearLastError().
			Exec(ctx); err != nil {
			return fmt.Errorf("reset vector outbox for chunk %d: %w", chunkID, err)
		}

		return nil
	}
	if !ent.IsNotFound(err) {
		return fmt.Errorf("look up vector outbox for chunk %d: %w", chunkID, err)
	}

	if _, err := client.VectorOutbox.Create().
		SetChunkID(chunkID).
		SetOperation(operation).
		SetStatus(vectoroutbox.StatusPending).
		Save(ctx); err != nil {
		return fmt.Errorf("create vector outbox for chunk %d: %w", chunkID, err)
	}

	return nil
}

func citationID(source string, index int) string {
	return fmt.Sprintf("%s#chunk-%d", source, index+1)
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}

	return &value
}

func cloneUint64Pointer(value *uint64) *uint64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func containsControlCharacter(value string) bool {
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return true
		}
	}

	return false
}
