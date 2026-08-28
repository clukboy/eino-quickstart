package vectorstore

import "github.com/milvus-io/milvus/client/v2/milvusclient"

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
	client     milvusclient.Client
	collection string
	dimensions int
	metricType string
}

func (s *MilvusStore) Collection() string {
	return s.collection
}
