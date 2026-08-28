package checkpoint

import (
	"context"
	"fmt"
	"time"

	"eino-quickstart/ent"
	entcheckpoint "eino-quickstart/ent/checkpoint"
)

type Store struct {
	client *ent.Client
}

func NewStore(client *ent.Client) *Store {
	return &Store{
		client: client,
	}
}

func (s *Store) Get(
	ctx context.Context,
	checkpointID string,
) ([]byte, bool, error) {
	record, err := s.client.Checkpoint.
		Query().
		Where(
			entcheckpoint.CheckpointIDEQ(checkpointID),
		).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf(
			"get checkpoint: %w",
			err,
		)
	}

	return append([]byte(nil), record.Payload...), true, nil
}

func (s *Store) Set(
	ctx context.Context,
	checkpointID string,
	payload []byte,
) error {
	expiresAt := time.Now().UTC().Add(24 * time.Hour)
	exists, err := s.client.Checkpoint.
		Query().
		Where(
			entcheckpoint.CheckpointIDEQ(checkpointID),
		).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf(
			"check checkpoint existence: %w",
			err,
		)
	}

	if exists {
		return s.client.Checkpoint.
			Update().
			Where(
				entcheckpoint.CheckpointIDEQ(checkpointID),
			).
			SetPayload(payload).
			Exec(ctx)
	}

	return s.client.Checkpoint.
		Create().
		SetCheckpointID(checkpointID).
		SetPayload(payload).
		SetExpiresAt(expiresAt).
		Exec(ctx)
}

func (s *Store) Delete(
	ctx context.Context,
	checkpointID string,
) error {
	_, err := s.client.Checkpoint.
		Delete().
		Where(
			entcheckpoint.CheckpointIDEQ(checkpointID),
		).
		Exec(ctx)

	if err != nil {
		return fmt.Errorf(
			"delete checkpoint: %w",
			err,
		)
	}

	return nil
}

func (s *Store) DeleteExpired(ctx context.Context, before time.Time, limit int) ([]string, error) {
	records, err := s.client.Checkpoint.
		Query().Where(
		entcheckpoint.ExpiresAtLT(before),
	).Limit(limit).All(ctx)
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(records))

	for _, record := range records {
		affected, err := s.client.Checkpoint.
			Delete().Where(
			entcheckpoint.IDEQ(record.ID),
		).Exec(ctx)
		if err != nil {
			return nil, err
		}
		if affected == 1 {
			ids = append(ids, record.CheckpointID)
		}
	}

	return ids, nil
}
