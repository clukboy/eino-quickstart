package vectorstore

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/milvus-io/milvus/client/v2/entity"
)

type recordingMilvus struct {
	hasCollection bool
	collection    *entity.Collection

	createdSchema *entity.Schema
	createdMetric entity.MetricType
	loaded        bool
	upsertIDs     []int64
	upsertVectors [][]float32
	deletedIDs    []int64
	searchResults []SearchResult
	readyErr      error
}

func (m *recordingMilvus) HasCollection(
	context.Context,
	string,
) (bool, error) {
	return m.hasCollection, nil
}

func (m *recordingMilvus) DescribeCollection(
	context.Context,
	string,
) (*entity.Collection, error) {
	if m.collection != nil {
		return m.collection, nil
	}
	return &entity.Collection{Schema: m.createdSchema}, nil
}

func (m *recordingMilvus) CreateCollection(
	_ context.Context,
	_ string,
	schema *entity.Schema,
	metric entity.MetricType,
) error {
	m.createdSchema = schema
	m.createdMetric = metric
	m.hasCollection = true
	return nil
}

func (m *recordingMilvus) LoadCollection(context.Context, string) error {
	m.loaded = true
	return nil
}

func (m *recordingMilvus) Upsert(
	_ context.Context,
	_ string,
	ids []int64,
	vectors [][]float32,
	_ int,
) error {
	m.upsertIDs = append([]int64(nil), ids...)
	m.upsertVectors = make([][]float32, len(vectors))
	for index, vector := range vectors {
		m.upsertVectors[index] = append([]float32(nil), vector...)
	}
	return nil
}

func (m *recordingMilvus) Delete(
	_ context.Context,
	_ string,
	ids []int64,
) error {
	m.deletedIDs = append([]int64(nil), ids...)
	return nil
}

func (m *recordingMilvus) Search(
	context.Context,
	string,
	[]float32,
	int,
) ([]SearchResult, error) {
	return append([]SearchResult(nil), m.searchResults...), nil
}

func (m *recordingMilvus) Ready(context.Context, string) error {
	return m.readyErr
}

func (m *recordingMilvus) Close(context.Context) error {
	return nil
}

func TestMilvusStoreEnsureCollectionCreatesExpectedSchema(t *testing.T) {
	client := &recordingMilvus{}
	store := &MilvusStore{
		client:     client,
		collection: "knowledge_chunks",
		dimensions: 3,
		metricType: entity.COSINE,
	}

	if err := store.EnsureCollection(context.Background()); err != nil {
		t.Fatalf("EnsureCollection() error = %v", err)
	}
	if client.createdSchema == nil || !client.loaded {
		t.Fatalf("collection was not created and loaded: %#v", client)
	}
	if client.createdMetric != entity.COSINE {
		t.Errorf("metric = %q, want COSINE", client.createdMetric)
	}
	if err := validateCollectionSchema(
		&entity.Collection{Schema: client.createdSchema},
		3,
	); err != nil {
		t.Errorf("created schema validation error = %v", err)
	}
}

func TestMilvusStoreValidatesAndCopiesVectors(t *testing.T) {
	client := &recordingMilvus{
		searchResults: []SearchResult{
			{ChunkID: 1, Score: 0.9},
			{ChunkID: 2, Score: 0.8},
			{ChunkID: 3, Score: 0.7},
		},
	}
	store := &MilvusStore{
		client:     client,
		collection: "knowledge_chunks",
		dimensions: 2,
		metricType: entity.IP,
	}
	vectors := []Vector{{
		ChunkID:        1,
		Embedding:      []float32{0.25, 0.75},
		EmbeddingModel: "embedding-model",
	}}

	if err := store.Upsert(context.Background(), vectors); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	vectors[0].Embedding[0] = 99
	if !reflect.DeepEqual(client.upsertIDs, []int64{1}) ||
		!reflect.DeepEqual(client.upsertVectors, [][]float32{{0.25, 0.75}}) {
		t.Errorf("upsert arguments = %v, %v", client.upsertIDs, client.upsertVectors)
	}

	if err := store.Delete(context.Background(), []int64{1, 1, 2}); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if !reflect.DeepEqual(client.deletedIDs, []int64{1, 2}) {
		t.Errorf("delete IDs = %v, want [1 2]", client.deletedIDs)
	}

	results, err := store.Search(context.Background(), []float32{0.5, 0.5}, 2)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if !reflect.DeepEqual(results, client.searchResults[:2]) {
		t.Errorf("Search() = %v, want first two results", results)
	}

	if err := store.Upsert(context.Background(), []Vector{{
		ChunkID:        2,
		Embedding:      []float32{1, 2},
		EmbeddingModel: "",
	}}); err == nil {
		t.Error("Upsert() accepted an empty embedding model")
	}
}

func TestValidateMilvusConfig(t *testing.T) {
	config := MilvusConfig{
		Address:    "localhost:19530",
		Collection: "knowledge_chunks",
		Dimensions: 3,
		MetricType: "cosine",
	}

	collection, metric, err := validateMilvusConfig(config)
	if err != nil || collection != "knowledge_chunks" || metric != entity.COSINE {
		t.Errorf(
			"validateMilvusConfig() = (%q, %q, %v)",
			collection,
			metric,
			err,
		)
	}

	for name, mutate := range map[string]func(*MilvusConfig){
		"empty address": func(config *MilvusConfig) {
			config.Address = ""
		},
		"address without port": func(config *MilvusConfig) {
			config.Address = "localhost"
		},
		"invalid collection": func(config *MilvusConfig) {
			config.Collection = "knowledge-chunks"
		},
		"zero dimensions": func(config *MilvusConfig) {
			config.Dimensions = 0
		},
		"unsupported metric": func(config *MilvusConfig) {
			config.MetricType = "HAMMING"
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := config
			mutate(&candidate)
			if _, _, err := validateMilvusConfig(candidate); err == nil {
				t.Error("validateMilvusConfig() error = nil, want validation error")
			}
		})
	}

	client := &recordingMilvus{readyErr: errors.New("not loaded")}
	store := &MilvusStore{
		client:     client,
		collection: "knowledge_chunks",
		dimensions: 3,
		metricType: entity.L2,
	}
	if err := store.Ready(context.Background()); err == nil {
		t.Error("Ready() error = nil, want readiness error")
	}
}
