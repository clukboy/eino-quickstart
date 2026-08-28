package retriever

import (
	"context"
	"fmt"

	"eino-quickstart/ent"
)

type PostgresKeywordSearcher struct {
	client *ent.Client
}

func NewPostgresKeywordSearcher(client *ent.Client) *PostgresKeywordSearcher {
	return &PostgresKeywordSearcher{
		client: client,
	}
}

func (s *PostgresKeywordSearcher) Search(
	ctx context.Context,
	actorSubject string,
	query string,
	limit int,
) ([]Candidate, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("keyword search limit must be greater than zero")
	}

	rows, err := s.client.QueryContext(
		ctx,
		`
	SELECT
		dc.id,
		(
			ts_rank(
				to_tsvector('simple', dc.content),
				plainto_tsquery('simple', $1)
			)
			+
			CASE
				WHEN position(
					lower($1) in lower(dc.content)
				) > 0
				THEN 1.0
				ELSE 0.0
			END
		) AS score
	FROM document_chunks AS dc
	JOIN documents AS d
		ON d.id = dc.document_chunks_document
	WHERE d.status = 'ready'
	  AND (
		d.visibility = 'system'
		OR (
			d.visibility = 'private'
			AND d.owner_subject = $2
		)
	  )
	  AND (
		to_tsvector('simple', dc.content)
			@@ plainto_tsquery('simple', $1)
		OR
		position(
			lower($1) in lower(dc.content)
		) > 0
	  )
	ORDER BY score DESC, dc.id
	LIMIT $3
	`,
		query,
		actorSubject,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]Candidate, 0)

	for rows.Next() {
		var item Candidate

		if err := rows.Scan(
			&item.ChunkID,
			&item.Score,
		); err != nil {
			return nil, err
		}

		results = append(results, item)
	}

	return results, rows.Err()
}
