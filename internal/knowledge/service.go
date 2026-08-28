package knowledge

import (
	"context"
	"crypto/sha256"
	"eino-quickstart/ent"
	"eino-quickstart/ent/document"
	"eino-quickstart/ent/documentchunk"
	"eino-quickstart/ent/vectoroutbox"
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
		if err := s.Ingest(
			ctx,
			item.Source,
			item.Title,
			item.Content,
			ownerSubject,
			visibility,
		); err != nil {
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

func (s *Service) Ingest(
	ctx context.Context,
	source string,
	title string,
	content string,
	ownerSubject string,
	visibility string,
) error {
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

	input, err := validateIngestInput(
		source,
		title,
		content,
		ownerSubject,
		visibility,
	)
	if err != nil {
		return err
	}

	chunks, err := s.Chunker.Split(input.content)
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

	checksum := fmt.Sprintf("%x", sha256.Sum256([]byte(input.content)))
	for attempt := 0; attempt < 2; attempt++ {
		err = s.ingestOnce(ctx, input, chunks, checksum)
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
	source       string
	title        string
	content      string
	ownerSubject string
	visibility   document.Visibility
}

func validateIngestInput(
	source string,
	title string,
	content string,
	ownerSubject string,
	visibility string,
) (ingestInput, error) {
	input := ingestInput{
		source:       strings.TrimSpace(source),
		title:        strings.TrimSpace(title),
		content:      content,
		ownerSubject: strings.TrimSpace(ownerSubject),
		visibility:   document.Visibility(strings.ToLower(strings.TrimSpace(visibility))),
	}

	if input.source == "" {
		return ingestInput{}, errors.New("document source is required")
	}
	if input.title == "" {
		return ingestInput{}, errors.New("document title is required")
	}
	if strings.TrimSpace(input.content) == "" {
		return ingestInput{}, errors.New("document content is required")
	}
	if containsControlCharacter(input.source) {
		return ingestInput{}, errors.New("document source contains a control character")
	}
	if containsControlCharacter(input.title) {
		return ingestInput{}, errors.New("document title contains a control character")
	}
	if containsControlCharacter(input.ownerSubject) {
		return ingestInput{}, errors.New("document owner subject contains a control character")
	}

	switch input.visibility {
	case document.VisibilitySystem:
		if input.ownerSubject == "" {
			input.ownerSubject = "system"
		}
	case document.VisibilityPrivate:
		if input.ownerSubject == "" {
			return ingestInput{}, errors.New(
				"private document owner subject is required",
			)
		}
	default:
		return ingestInput{}, fmt.Errorf(
			"document visibility %q must be system or private",
			visibility,
		)
	}

	return input, nil
}

func (s *Service) ingestOnce(
	ctx context.Context,
	input ingestInput,
	chunks []Chunk,
	checksum string,
) (err error) {
	tx, err := s.Client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("start ingestion transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

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
		record, err := client.DocumentChunk.Create().
			SetDocumentID(doc.ID).
			SetChunkIndex(index).
			SetCitationID(citationID(input.source, index)).
			SetContent(chunk.Content).
			SetNillableHeadingPath(optionalString(chunk.HeadingPath)).
			SetStartLine(chunk.StartLine).
			SetEndLine(chunk.EndLine).
			SetCharacterCount(len([]rune(chunk.Content))).
			SetEmbeddingModel(strings.TrimSpace(s.EmbeddingModel)).
			SetVectorStatus(documentchunk.VectorStatusPending).
			Save(ctx)
		if err != nil {
			return fmt.Errorf("create document chunk %d: %w", index, err)
		}

		if err := queueVectorOperation(
			ctx,
			client,
			int64(record.ID),
			vectoroutbox.OperationUpsert,
		); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit ingestion transaction: %w", err)
	}

	return nil
}

func upsertDocument(
	ctx context.Context,
	client *ent.Client,
	input ingestInput,
	checksum string,
) (*ent.Document, bool, error) {
	existing, err := client.Document.Query().
		Where(document.SourceEQ(input.source)).
		Only(ctx)
	if err == nil {
		updated, updateErr := existing.Update().
			SetTitle(input.title).
			SetChecksum(checksum).
			SetOwnerSubject(input.ownerSubject).
			SetVisibility(input.visibility).
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
		SetSource(input.source).
		SetTitle(input.title).
		SetChecksum(checksum).
		SetOwnerSubject(input.ownerSubject).
		SetVisibility(input.visibility).
		SetStatus(document.StatusIndexing).
		Save(ctx)
	if err != nil {
		return nil, true, fmt.Errorf("create document: %w", err)
	}

	return created, true, nil
}

func queueVectorOperation(
	ctx context.Context,
	client *ent.Client,
	chunkID int64,
	operation vectoroutbox.Operation,
) error {
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
