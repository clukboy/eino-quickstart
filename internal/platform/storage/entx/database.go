package entx

import (
	"context"
	"fmt"
	"net/url"

	"eino-quickstart/ent"
	"eino-quickstart/ent/schema/hook"
	"eino-quickstart/internal/platform/config"

	_ "github.com/lib/pq"
)

// Open opens PostgreSQL without issuing DDL.
//
// Application processes must use this entry point. Schema changes are an
// explicit deployment step through cmd/db-migrate; this keeps two application
// instances from racing to alter the database during startup.
func Open(ctx context.Context, c config.Storage) (*ent.Client, error) {
	return OpenWithoutMigration(ctx, c)
}

// OpenWithoutMigration is the production-safe database connection path. It
// installs query tracing but does not create, alter, drop, or backfill schema.
func OpenWithoutMigration(_ context.Context, c config.Storage) (*ent.Client, error) {
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

	// entgo 添加 trace hook 实现链路追踪
	client.Use(hook.TraceAuditHook())
	return client, nil
}

// OpenDevelopment retains the convenient local-development behavior. It is
// intentionally named so production code cannot accidentally opt into schema
// changes by calling the ordinary Open function.
func OpenDevelopment(ctx context.Context, c config.Storage) (*ent.Client, error) {
	client, err := OpenWithoutMigration(ctx, c)
	if err != nil {
		return nil, err
	}
	if err := Migrate(ctx, client); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}
