package es

import (
	"context"
	"eino-quickstart/internal/platform/config"
	"errors"
	"os"

	"github.com/elastic/go-elasticsearch/v9"
	"github.com/elastic/go-elasticsearch/v9/typedapi/core/index"
	"github.com/elastic/go-elasticsearch/v9/typedapi/core/search"
	"github.com/elastic/go-elasticsearch/v9/typedapi/indices/create"
	"github.com/elastic/go-elasticsearch/v9/typedapi/indices/delete"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types/enums/textquerytype"
)

type Store interface {
	Search(ctx context.Context, index string, query string, fields ...string) (*search.Response, error)
	ExistsIndex(ctx context.Context, index string) (bool, error)
	CreateIndex(ctx context.Context, index string, mapping types.TypeMappingVariant) (*create.Response, error)
	DeleteIndex(ctx context.Context, index string) (*delete.Response, error)

	CreateDocument(ctx context.Context, index string, document any) (*index.Response, error)
}

type ESClient struct {
	*elasticsearch.TypedClient
}

func NewESClient(cfg *config.ESConfig) (*ESClient, error) {
	cert, err := os.ReadFile(cfg.CaCertPath)
	if err != nil {
		return nil, err
	}
	client, err := elasticsearch.NewTyped(
		elasticsearch.WithAddresses(cfg.Address...),
		elasticsearch.WithBasicAuth(cfg.Username, cfg.Password),
		elasticsearch.WithCACert(cert),
	)
	if err != nil {
		return nil, err
	}
	return &ESClient{
		TypedClient: client,
	}, nil
}

func (c *ESClient) Search(ctx context.Context, index string, query string, fields ...string) (*search.Response, error) {
	if len(index) == 0 {
		return nil, errors.New("index cannot be empty")
	}

	if len(fields) == 0 {
		return nil, errors.New("fields cannot be empty")
	}

	if len(query) == 0 {
		return nil, errors.New("query cannot be empty")
	}

	return c.TypedClient.Search().Index(index).
		Request(&search.Request{
			Query: &types.Query{
				MultiMatch: &types.MultiMatchQuery{
					Query:  query,
					Fields: fields,
					Type:   &textquerytype.Mostfields,
				},
			},
		}).Do(ctx)
}

// ExistsIndex implements [Store].
func (c *ESClient) ExistsIndex(ctx context.Context, index string) (bool, error) {
	return c.TypedClient.Indices.Exists(index).Do(ctx)
}

// CreateIndex implements [Store].
func (c *ESClient) CreateIndex(ctx context.Context, index string, mapping types.TypeMappingVariant) (*create.Response, error) {
	return c.TypedClient.Indices.Create(index).Mappings(mapping).Do(ctx)
}

// DeleteIndex implements [Store].
func (c *ESClient) DeleteIndex(ctx context.Context, index string) (*delete.Response, error) {
	return c.TypedClient.Indices.Delete(index).Do(ctx)
}

// CreateDocument implements [Store].
func (c *ESClient) CreateDocument(ctx context.Context, index string, document any) (*index.Response, error) {
	return c.Index(index).Document(document).Do(ctx)
}

var _ Store = (*ESClient)(nil)
