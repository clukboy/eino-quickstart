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
	Client          *ent.Client
	Chunker         Chunker
	MaxChunksPerDoc int
	EmbeddingModel  string
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

type IngestRequest struct {
	KnowledgeBaseID int
	FolderID        *int

	Source  string
	Title   string
	Content string

	Metadata map[string]any

	OwnerSubject string
	Visibility   string
}

var errSourceCreateConflict = errors.New("document source creation conflict")

// NewService creates a knowledge ingestion service.
func NewService(config ServiceConfig) (*Service, error) {
	service := &Service{
		Client:          config.Client,
		Chunker:         config.Chunker,
		MaxChunksPerDoc: config.MaxChunksPerDoc,
		EmbeddingModel:  strings.TrimSpace(config.EmbeddingModel),
	}
	if service.Client == nil {
		return nil, errors.New("knowledge ent client is required")
	}
	if service.Chunker.Size <= 0 {
		return nil, errors.New("chunk size must be greater than zero")
	}
	if service.Chunker.Overlap < 0 ||
		service.Chunker.Overlap >= service.Chunker.Size {
		return nil, errors.New(
			"chunk overlap must be non-negative and smaller than chunk size",
		)
	}
	if service.MaxChunksPerDoc <= 0 {
		return nil, errors.New(
			"maximum chunks per document must be greater than zero",
		)
	}
	if service.EmbeddingModel == "" {
		return nil, errors.New("embedding model is required")
	}
	return service, nil
}

// IngestRoot loads documents from a loader and ingests them in source order.
func (s *Service) IngestRoot(ctx context.Context, loader *Loader, ownerSubject string, visibility string) (IngestRootResult, error) {
	if loader == nil {
		return IngestRootResult{}, errors.New("knowledge loader is required")
	}
	documents, err := loader.Load(ctx)
	if err != nil {
		return IngestRootResult{}, err
	}

	result := IngestRootResult{Loaded: len(documents)}
	for _, item := range documents {
		if err := s.Ingest(ctx, IngestRequest{
			KnowledgeBaseID: loader.KnowledgeBaseID,
			FolderID:        loader.FolderID,
			Source:          item.Source,
			Title:           item.Title,
			Content:         item.Content,
			Metadata:        item.Metadata,
			OwnerSubject:    ownerSubject,
			Visibility:      visibility,
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
	if s.Client == nil {
		return errors.New("knowledge ent client is required")
	}
	if ctx == nil {
		return errors.New("ingestion context is required")
	}
	if s.MaxChunksPerDoc <= 0 {
		return errors.New("maximum chunks per document must be greater than zero")
	}
	if strings.TrimSpace(s.EmbeddingModel) == "" {
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

	chunks, err := s.Chunker.Split(input.Content)
	if err != nil {
		return fmt.Errorf("split document into chunks: %w", err)
	}
	if len(chunks) == 0 {
		return errors.New("document content produced no chunks")
	}
	if len(chunks) > s.MaxChunksPerDoc {
		return fmt.Errorf(
			"document has %d chunks, exceeding the maximum of %d",
			len(chunks),
			s.MaxChunksPerDoc,
		)
	}

	documentMetadata := cloneMap(input.Metadata)

	if productInfo != nil {
		for key, value := range productInfo.ToMap() {
			documentMetadata[key] = value
		}

		documentMetadata["document_type"] = "product"
	}

	checksum := fmt.Sprintf("%x", sha256.Sum256([]byte(input.Content)))
	for attempt := 0; attempt < 2; attempt++ {
		err = s.ingestOnce(ctx, input, documentMetadata, productInfo, chunks, checksum)
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
	KnowledgeBaseID int
	FolderID        *int

	Source  string
	Title   string
	Content string

	Metadata map[string]any

	OwnerSubject string
	Visibility   document.Visibility
}

func validateIngestRequest(req IngestRequest) (ingestInput, error) {
	input := ingestInput{
		Source:          strings.TrimSpace(req.Source),
		Title:           strings.TrimSpace(req.Title),
		Content:         req.Content,
		OwnerSubject:    strings.TrimSpace(req.OwnerSubject),
		Visibility:      document.Visibility(strings.ToLower(strings.TrimSpace(req.Visibility))),
		Metadata:        cloneMap(req.Metadata),
		KnowledgeBaseID: req.KnowledgeBaseID,
		FolderID:        req.FolderID,
	}

	if input.Source == "" {
		return ingestInput{}, errors.New("document source is required")
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
			req.Visibility,
		)
	}

	return input, nil
}

func (s *Service) ingestOnce(ctx context.Context, input ingestInput, metadata map[string]any, productInfo *product.Product, chunks []Chunk, checksum string) (err error) {
	return entx.WithTx(ctx, s.Client, func(tx *ent.Tx) error {
		client := tx.Client()
		doc, created, err := upsertDocument(ctx, client, input, checksum)
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
				int64(chunk.ID),
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
			chunkMetadata := BuildChunkMetadata(
				metadata,
				productInfo,
				chunk.HeadingPath,
				chunk.Content,
			)
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
				SetEmbeddingModel(strings.TrimSpace(s.EmbeddingModel)).
				SetVectorStatus(documentchunk.VectorStatusPending).
				Save(ctx)
			if err != nil {
				return fmt.Errorf("create document chunk %d: %w", index, err)
			}

			if err := queueVectorOperation(ctx, client, int64(record.ID), vectoroutbox.OperationUpsert); err != nil {
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

func queueVectorOperation(ctx context.Context, client *ent.Client, chunkID int64, operation vectoroutbox.Operation) error {
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

func containsControlCharacter(value string) bool {
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return true
		}
	}

	return false
}
