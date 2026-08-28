package entx

import (
	"context"
	"eino-quickstart/ent"
	"fmt"
	"log/slog"
)

// WithTx uses transaction in ent.
func WithTx(ctx context.Context, client *ent.Client, fn func(tx *ent.Tx) error) error {
	tx, err := client.Tx(ctx)
	if err != nil {
		slog.Error("failed to start transaction", slog.String("detail", err.Error()))
		return err
	}
	defer func() {
		if v := recover(); v != nil {
			_ = tx.Rollback()
			panic(v)
		}
	}()
	if err := fn(tx); err != nil {
		if rollBackErr := tx.Rollback(); rollBackErr != nil {
			err = fmt.Errorf("%w: rolling back transaction: %v", err, rollBackErr)
		}
		slog.Error("errors occur in transaction", slog.String("detail", err.Error()))
		return err
	}
	if err := tx.Commit(); err != nil {
		slog.Error("failed to commit transaction", slog.String("detail", err.Error()))
		return err
	}
	return nil
}
