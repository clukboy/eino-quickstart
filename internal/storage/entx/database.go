package entx

import (
	"context"
	"eino-quickstart/internal/config"
	"fmt"

	"eino-quickstart/ent"

	_ "github.com/lib/pq"
)

func Open(ctx context.Context, c config.Storage) (*ent.Client, error) {
	client, err := ent.Open("postgres", fmt.Sprintf("postgresql://%s:%s@%s:%d/%s?sslmode=%s", c.Username, c.Password, c.Host, c.Port, c.DBName, c.SSLMode))
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
