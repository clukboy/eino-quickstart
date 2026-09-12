package rag

import (
	"context"
	"eino-quickstart/ent"
	"eino-quickstart/internal/platform/config"
	"eino-quickstart/internal/platform/storage/es"
	"eino-quickstart/internal/rag/constant"
	"eino-quickstart/internal/rag/store/milvus"
	"eino-quickstart/internal/rag/store/postgres"
	"eino-quickstart/pkg/convert"
	"strconv"

	"github.com/cloudwego/eino/schema"
)

// Hit 是一次检索命中。
type Hit struct {
	Doc   *schema.Document
	Score float64
}

// Filter 描述检索侧的元数据过滤条件（ACL 等）。
type Filter struct {
	Visibility string // 为空表示不过滤
	Owner      string // 为空表示不过滤
}

func (f Filter) allow(d *schema.Document) bool {
	if f.Visibility != "" && MetaString(d, constant.MetaVisibility) != f.Visibility {
		return false
	}
	if f.Owner != "" && MetaString(d, constant.MetaOwner) != f.Owner {
		return false
	}
	return true
}

type Store struct {
	PgStore     postgres.Store
	MilvusStore milvus.Store
	ESStore     es.Store
}

func NewStore(ctx context.Context, entClient *ent.Client, cfg *config.Config, esClient es.Store) (*Store, error) {
	pgStore := postgres.NewPostgres(entClient)
	milvusStore, err := milvus.NewMilvusStore(ctx, &milvus.MilvusConfig{
		Address:    cfg.Milvus.Address,
		Collection: cfg.Milvus.Collection,
		Dimensions: cfg.Embedding.Dimensions,
		MetricType: cfg.Milvus.MetricType,
	})
	if err != nil {
		return nil, err
	}
	return &Store{
		PgStore:     pgStore,
		MilvusStore: milvusStore,
		ESStore:     esClient,
	}, nil
}

func (s *Store) Add(ctx context.Context, docs []*schema.Document, vecs [][]float64) error {
	// [优化] 1) 先存向量再存 chunk，避免 chunk 存储失败导致向量丢失；2) 幂等：同一 source 重新入库前先清理旧向量
	_, err := s.PgStore.CreateChunk(ctx, docs)
	if err != nil {
		return err
	}
	var chunkIDs []int64
	for _, doc := range docs {
		chunkID, _ := strconv.ParseInt(doc.ID, 10, 64)
		chunkIDs = append(chunkIDs, chunkID)
	}

	// 将文档存储到 ES 中
	for _, doc := range docs {
		_, err := s.ESStore.CreateDocument(ctx, "your_index_name", doc.MetaData)
		if err != nil {
			return err
		}
	}

	// [优化] 2) 向量存储失败时，chunk 已经存储成功，可能导致向量与 chunk 不一致；可按需改成硬错误
	err = s.MilvusStore.Upsert(ctx, chunkIDs, convert.Float64ToFloat32(vecs))
	if err != nil {
		return err
	}
	return nil

}

func (s *Store) DeleteBySource(ctx context.Context, source string) error {
	return nil
}

func (s *Store) SearchByVector(ctx context.Context, vec []float64, topK int, filter Filter) ([]Hit, error) {
	return nil, nil
}

func (s *Store) SearchByText(ctx context.Context, query string, topK int, filter Filter) ([]Hit, error) {
	return nil, nil
}
