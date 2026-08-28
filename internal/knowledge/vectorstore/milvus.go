package vectorstore

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"regexp"
	"strconv"
	"strings"

	"github.com/milvus-io/milvus/client/v2/entity"
	"github.com/milvus-io/milvus/client/v2/index"
	"github.com/milvus-io/milvus/client/v2/milvusclient"
)

type CollectionStore interface {
	Store
	Collection() string
}

type MilvusConfig struct {
	Address    string
	Collection string
	Dimensions int
	MetricType string
}

type MilvusStore struct {
	client     milvusAPI
	collection string
	dimensions int
	metricType entity.MetricType
}

var _ CollectionStore = (*MilvusStore)(nil)

func (s *MilvusStore) Collection() string {
	if s == nil {
		return ""
	}

	return s.collection
}

const (
	milvusIDField        = "chunk_id"
	milvusEmbeddingField = "embedding"
)

var milvusCollectionName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,254}$`)

type milvusAPI interface {
	HasCollection(ctx context.Context, collection string) (bool, error)
	DescribeCollection(
		ctx context.Context,
		collection string,
	) (*entity.Collection, error)
	CreateCollection(
		ctx context.Context,
		collection string,
		schema *entity.Schema,
		metric entity.MetricType,
	) error
	LoadCollection(ctx context.Context, collection string) error
	Upsert(
		ctx context.Context,
		collection string,
		ids []int64,
		vectors [][]float32,
		dimensions int,
	) error
	Delete(ctx context.Context, collection string, ids []int64) error
	Search(
		ctx context.Context,
		collection string,
		embedding []float32,
		limit int,
	) ([]SearchResult, error)
	Ready(ctx context.Context, collection string) error
	Close(ctx context.Context) error
}

type milvusClientAdapter struct {
	client *milvusclient.Client
}

// NewMilvusStore creates a Milvus vector store. The caller must invoke
// EnsureCollection before the store is used.
func NewMilvusStore(
	ctx context.Context,
	config MilvusConfig,
) (*MilvusStore, error) {
	collection, metric, err := validateMilvusConfig(config)
	if err != nil {
		return nil, err
	}

	client, err := milvusclient.New(ctx, &milvusclient.ClientConfig{
		Address: strings.TrimSpace(config.Address),
	})
	if err != nil {
		return nil, fmt.Errorf("create Milvus client: %w", err)
	}

	return &MilvusStore{
		client:     &milvusClientAdapter{client: client},
		collection: collection,
		dimensions: config.Dimensions,
		metricType: metric,
	}, nil
}

// EnsureCollection creates the collection when needed, verifies its schema,
// and waits for it to be loaded.
func (s *MilvusStore) EnsureCollection(ctx context.Context) error {
	if err := s.validate(); err != nil {
		return err
	}

	exists, err := s.client.HasCollection(ctx, s.collection)
	if err != nil {
		return fmt.Errorf("check Milvus collection: %w", err)
	}

	if !exists {
		schema := entity.NewSchema().
			WithName(s.collection).
			WithDescription("Knowledge document chunk embeddings").
			WithDynamicFieldEnabled(false).
			WithField(
				entity.NewField().
					WithName(milvusIDField).
					WithDataType(entity.FieldTypeInt64).
					WithIsPrimaryKey(true).
					WithIsAutoID(false),
			).
			WithField(
				entity.NewField().
					WithName(milvusEmbeddingField).
					WithDataType(entity.FieldTypeFloatVector).
					WithDim(int64(s.dimensions)),
			)

		if err := s.client.CreateCollection(
			ctx,
			s.collection,
			schema,
			s.metricType,
		); err != nil {
			created, checkErr := s.client.HasCollection(ctx, s.collection)
			if checkErr != nil || !created {
				return fmt.Errorf("create Milvus collection: %w", err)
			}
		}
	}

	collection, err := s.client.DescribeCollection(ctx, s.collection)
	if err != nil {
		return fmt.Errorf("describe Milvus collection: %w", err)
	}
	if err := validateCollectionSchema(collection, s.dimensions); err != nil {
		return err
	}

	if err := s.client.LoadCollection(ctx, s.collection); err != nil {
		return fmt.Errorf("load Milvus collection: %w", err)
	}

	return nil
}

func (s *MilvusStore) Upsert(ctx context.Context, vectors []Vector) error {
	if err := s.validate(); err != nil {
		return err
	}
	if len(vectors) == 0 {
		return nil
	}

	ids := make([]int64, len(vectors))
	embeddings := make([][]float32, len(vectors))
	seen := make(map[int64]struct{}, len(vectors))

	for i, vector := range vectors {
		if vector.ChunkID <= 0 {
			return fmt.Errorf("vector %d has an invalid chunk ID", i)
		}
		if _, exists := seen[vector.ChunkID]; exists {
			return fmt.Errorf("vector %d has a duplicate chunk ID", i)
		}
		if strings.TrimSpace(vector.EmbeddingModel) == "" {
			return fmt.Errorf("vector %d has an empty embedding model", i)
		}
		if err := validateVector(vector.Embedding, s.dimensions); err != nil {
			return fmt.Errorf("vector %d: %w", i, err)
		}

		seen[vector.ChunkID] = struct{}{}
		ids[i] = vector.ChunkID
		embeddings[i] = append([]float32(nil), vector.Embedding...)
	}

	if err := s.client.Upsert(
		ctx,
		s.collection,
		ids,
		embeddings,
		s.dimensions,
	); err != nil {
		return fmt.Errorf("upsert Milvus vectors: %w", err)
	}

	return nil
}

func (s *MilvusStore) Delete(ctx context.Context, chunkIDs []int64) error {
	if err := s.validate(); err != nil {
		return err
	}
	if len(chunkIDs) == 0 {
		return nil
	}

	ids := make([]int64, 0, len(chunkIDs))
	seen := make(map[int64]struct{}, len(chunkIDs))
	for _, chunkID := range chunkIDs {
		if chunkID <= 0 {
			return fmt.Errorf("invalid chunk ID %d", chunkID)
		}
		if _, exists := seen[chunkID]; exists {
			continue
		}

		seen[chunkID] = struct{}{}
		ids = append(ids, chunkID)
	}

	if err := s.client.Delete(ctx, s.collection, ids); err != nil {
		return fmt.Errorf("delete Milvus vectors: %w", err)
	}

	return nil
}

func (s *MilvusStore) Search(
	ctx context.Context,
	embedding []float32,
	limit int,
) ([]SearchResult, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		return nil, errors.New("search limit must be greater than zero")
	}
	if err := validateVector(embedding, s.dimensions); err != nil {
		return nil, fmt.Errorf("search embedding: %w", err)
	}

	results, err := s.client.Search(
		ctx,
		s.collection,
		append([]float32(nil), embedding...),
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("search Milvus vectors: %w", err)
	}
	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

func (s *MilvusStore) Ready(ctx context.Context) error {
	if err := s.validate(); err != nil {
		return err
	}
	if err := s.client.Ready(ctx, s.collection); err != nil {
		return fmt.Errorf("Milvus vector store is not ready: %w", err)
	}

	return nil
}

func (s *MilvusStore) Close(ctx context.Context) error {
	if s == nil || s.client == nil {
		return nil
	}
	if err := s.client.Close(ctx); err != nil {
		return fmt.Errorf("close Milvus client: %w", err)
	}

	return nil
}

func (s *MilvusStore) validate() error {
	if s == nil {
		return errors.New("Milvus store is nil")
	}
	if s.client == nil {
		return errors.New("Milvus client is required")
	}
	if !milvusCollectionName.MatchString(s.collection) {
		return errors.New("Milvus collection name is invalid")
	}
	if s.dimensions <= 0 {
		return errors.New("Milvus dimensions must be greater than zero")
	}
	if !validMetricType(s.metricType) {
		return fmt.Errorf("Milvus metric type %q is invalid", s.metricType)
	}

	return nil
}

func validateMilvusConfig(config MilvusConfig) (string, entity.MetricType, error) {
	address := strings.TrimSpace(config.Address)
	if address == "" {
		return "", "", errors.New("Milvus address is required")
	}
	if strings.ContainsAny(address, " \t\r\n") {
		return "", "", errors.New("Milvus address must not contain whitespace")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" {
		return "", "", errors.New("Milvus address must be host:port")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return "", "", errors.New("Milvus address port must be between 1 and 65535")
	}

	collection := strings.TrimSpace(config.Collection)
	if !milvusCollectionName.MatchString(collection) {
		return "", "", errors.New("Milvus collection name is invalid")
	}
	if config.Dimensions <= 0 {
		return "", "", errors.New("Milvus dimensions must be greater than zero")
	}

	metric := entity.MetricType(strings.ToUpper(strings.TrimSpace(config.MetricType)))
	if metric == "" {
		metric = entity.COSINE
	}
	if !validMetricType(metric) {
		return "", "", fmt.Errorf("unsupported Milvus metric type %q", config.MetricType)
	}

	return collection, metric, nil
}

func validMetricType(metric entity.MetricType) bool {
	switch metric {
	case entity.COSINE, entity.IP, entity.L2:
		return true
	default:
		return false
	}
}

func validateCollectionSchema(
	collection *entity.Collection,
	dimensions int,
) error {
	if collection == nil || collection.Schema == nil {
		return errors.New("Milvus collection has no schema")
	}

	var idField, embeddingField *entity.Field
	for _, field := range collection.Schema.Fields {
		switch field.Name {
		case milvusIDField:
			idField = field
		case milvusEmbeddingField:
			embeddingField = field
		}
	}

	if idField == nil || idField.DataType != entity.FieldTypeInt64 ||
		!idField.PrimaryKey || idField.AutoID {
		return errors.New("Milvus collection has an incompatible chunk_id field")
	}
	if embeddingField == nil ||
		embeddingField.DataType != entity.FieldTypeFloatVector {
		return errors.New("Milvus collection has an incompatible embedding field")
	}
	dimension, err := embeddingField.GetDim()
	if err != nil {
		return fmt.Errorf("read Milvus embedding dimension: %w", err)
	}
	if dimension != int64(dimensions) {
		return fmt.Errorf(
			"Milvus collection embedding dimension %d does not match configured dimension %d",
			dimension,
			dimensions,
		)
	}

	return nil
}

func validateVector(values []float32, dimensions int) error {
	if len(values) != dimensions {
		return fmt.Errorf(
			"dimension %d does not match configured dimension %d",
			len(values),
			dimensions,
		)
	}
	for _, value := range values {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return errors.New("contains a non-finite value")
		}
	}

	return nil
}

func (c *milvusClientAdapter) HasCollection(
	ctx context.Context,
	collection string,
) (bool, error) {
	return c.client.HasCollection(
		ctx,
		milvusclient.NewHasCollectionOption(collection),
	)
}

func (c *milvusClientAdapter) DescribeCollection(
	ctx context.Context,
	collection string,
) (*entity.Collection, error) {
	return c.client.DescribeCollection(
		ctx,
		milvusclient.NewDescribeCollectionOption(collection),
	)
}

func (c *milvusClientAdapter) CreateCollection(
	ctx context.Context,
	collection string,
	schema *entity.Schema,
	metric entity.MetricType,
) error {
	if err := c.client.CreateCollection(
		ctx,
		milvusclient.NewCreateCollectionOption(collection, schema),
	); err != nil {
		return err
	}

	task, err := c.client.CreateIndex(
		ctx,
		milvusclient.NewCreateIndexOption(
			collection,
			milvusEmbeddingField,
			index.NewAutoIndex(metric),
		),
	)
	if err != nil {
		return fmt.Errorf("create vector index: %w", err)
	}
	if err := task.Await(ctx); err != nil {
		return fmt.Errorf("wait for vector index: %w", err)
	}

	return nil
}

func (c *milvusClientAdapter) LoadCollection(
	ctx context.Context,
	collection string,
) error {
	task, err := c.client.LoadCollection(
		ctx,
		milvusclient.NewLoadCollectionOption(collection),
	)
	if err != nil {
		return err
	}

	return task.Await(ctx)
}

func (c *milvusClientAdapter) Upsert(
	ctx context.Context,
	collection string,
	ids []int64,
	vectors [][]float32,
	dimensions int,
) error {
	_, err := c.client.Upsert(
		ctx,
		milvusclient.NewColumnBasedInsertOption(collection).
			WithInt64Column(milvusIDField, ids).
			WithFloatVectorColumn(milvusEmbeddingField, dimensions, vectors),
	)
	return err
}

func (c *milvusClientAdapter) Delete(
	ctx context.Context,
	collection string,
	ids []int64,
) error {
	_, err := c.client.Delete(
		ctx,
		milvusclient.NewDeleteOption(collection).
			WithInt64IDs(milvusIDField, ids),
	)
	return err
}

func (c *milvusClientAdapter) Search(
	ctx context.Context,
	collection string,
	embedding []float32,
	limit int,
) ([]SearchResult, error) {
	resultSets, err := c.client.Search(
		ctx,
		milvusclient.NewSearchOption(
			collection,
			limit,
			[]entity.Vector{entity.FloatVector(embedding)},
		).WithANNSField(milvusEmbeddingField),
	)
	if err != nil {
		return nil, err
	}
	if len(resultSets) != 1 {
		return nil, fmt.Errorf(
			"Milvus search returned %d result sets for one query",
			len(resultSets),
		)
	}

	resultSet := resultSets[0]
	if resultSet.Err != nil {
		return nil, resultSet.Err
	}
	if resultSet.IDs == nil {
		return nil, errors.New("Milvus search result IDs are missing")
	}
	if resultSet.Len() != len(resultSet.Scores) {
		return nil, errors.New("Milvus search result scores do not match IDs")
	}

	results := make([]SearchResult, 0, resultSet.Len())
	for i := 0; i < resultSet.Len(); i++ {
		id, err := resultSet.IDs.GetAsInt64(i)
		if err != nil {
			return nil, fmt.Errorf("read Milvus search result ID: %w", err)
		}
		if id <= 0 {
			return nil, fmt.Errorf("Milvus search returned invalid chunk ID %d", id)
		}

		results = append(results, SearchResult{
			ChunkID: id,
			Score:   float64(resultSet.Scores[i]),
		})
	}

	return results, nil
}

func (c *milvusClientAdapter) Ready(
	ctx context.Context,
	collection string,
) error {
	exists, err := c.HasCollection(ctx, collection)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("collection does not exist")
	}

	state, err := c.client.GetLoadState(
		ctx,
		milvusclient.NewGetLoadStateOption(collection),
	)
	if err != nil {
		return err
	}
	if state.State != entity.LoadStateLoaded {
		return fmt.Errorf("collection load state is %v", state.State)
	}

	return nil
}

func (c *milvusClientAdapter) Close(ctx context.Context) error {
	return c.client.Close(ctx)
}
