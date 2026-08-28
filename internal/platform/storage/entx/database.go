package entx

import (
	"context"
	"fmt"
	"net/url"

	"eino-quickstart/ent"
	"eino-quickstart/internal/platform/config"

	_ "github.com/lib/pq"
)

func Open(ctx context.Context, c config.Storage) (*ent.Client, error) {
	connectionURL := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.Username, c.Password),
		Host:   fmt.Sprintf("%s:%d", c.Host, c.Port),
		Path:   c.DBName,
		RawQuery: url.Values{
			"sslmode": []string{c.SSLMode},
		}.Encode(),
	}
	client, err := ent.Open("postgres", connectionURL.String())
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	// 学习阶段自动建表；生产阶段改用 Ent/Atlas 版本化迁移。
	if err := client.Schema.Create(ctx); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("migrate postgres schema: %w", err)
	}

	return client, nil
}
