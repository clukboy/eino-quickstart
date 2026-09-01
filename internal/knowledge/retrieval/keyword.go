package retrieval

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

func (s *PostgresKeywordSearcher) Search(ctx context.Context, actorSubject string, query string, limit int) ([]Candidate, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("keyword search database client is required")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("keyword search limit must be greater than zero")
	}

	rows, err := s.client.QueryContext(
		ctx,
		`
		SELECT
			dc.id,
			(
				-- 正文及 metadata 全文检索
				ts_rank(
					to_tsvector(
						'simple',
						concat_ws(
							' ',
							dc.content,
							COALESCE(dc.metadata ->> 'model', ''),
							COALESCE(dc.metadata ->> 'exact_model', ''),
							COALESCE(dc.metadata ->> 'category', ''),
							COALESCE(dc.metadata ->> 'subcategory', ''),
							COALESCE(dc.metadata ->> 'series', ''),
							COALESCE(dc.metadata ->> 'name', ''),
							COALESCE(dc.metadata ->> 'installation', ''),
							COALESCE(dc.metadata ->> 'door_material', ''),
							COALESCE(dc.metadata ->> 'force_type', ''),
							COALESCE(dc.metadata ->> 'base_material', ''),
							COALESCE(dc.metadata ->> 'family_prefix', ''),
							COALESCE(dc.metadata ->> 'variant_models', '')
						)
					),
					plainto_tsquery('simple', $1)
				)
				+
				-- 完整查询字符串命中正文
				CASE
					WHEN position(
						lower($1)
						in lower(dc.content)
					) > 0
					THEN 1.0
					ELSE 0.0
				END
				+
				-- 型号字段命中
				CASE
					WHEN lower(COALESCE(dc.metadata ->> 'model', '')) = lower($1)
					THEN 2.0
		
					WHEN lower(COALESCE(dc.metadata ->> 'exact_model', '')) = lower($1)
					THEN 3.0
		
					ELSE 0.0
				END
				+
				-- 属性字段直接命中
				CASE
					WHEN position(
						lower($1)
						in lower(
							concat_ws(
								' ',
								COALESCE(dc.metadata ->> 'category', ''),
								COALESCE(dc.metadata ->> 'subcategory', ''),
								COALESCE(dc.metadata ->> 'series', ''),
								COALESCE(dc.metadata ->> 'name', ''),
								COALESCE(dc.metadata ->> 'installation', ''),
								COALESCE(dc.metadata ->> 'door_material', ''),
								COALESCE(dc.metadata ->> 'force_type', ''),
								COALESCE(dc.metadata ->> 'base_material', '')
							)
						)
					) > 0
					THEN 1.5
					ELSE 0.0
				END
			) AS score
		
		FROM document_chunks AS dc
		
		JOIN documents AS d
			ON d.id = dc.document_chunks
		
		WHERE d.status = 'ready'
		
		  AND (
			d.visibility = 'system'
			OR (
				d.visibility = 'private'
				AND d.owner_subject = $2
			)
		  )
		
		  AND (
			to_tsvector(
				'simple',
				concat_ws(
					' ',
					dc.content,
					COALESCE(dc.metadata ->> 'model', ''),
					COALESCE(dc.metadata ->> 'exact_model', ''),
					COALESCE(dc.metadata ->> 'category', ''),
					COALESCE(dc.metadata ->> 'subcategory', ''),
					COALESCE(dc.metadata ->> 'series', ''),
					COALESCE(dc.metadata ->> 'name', ''),
					COALESCE(dc.metadata ->> 'installation', ''),
					COALESCE(dc.metadata ->> 'door_material', ''),
					COALESCE(dc.metadata ->> 'force_type', ''),
					COALESCE(dc.metadata ->> 'base_material', ''),
					COALESCE(dc.metadata ->> 'family_prefix', ''),
					COALESCE(dc.metadata ->> 'variant_models', '')
				)
			)
			@@ plainto_tsquery('simple', $1)
		
			OR
		
			position(
				lower($1)
				in lower(dc.content)
			) > 0
		
			OR
		
			position(
				lower($1)
				in lower(
					concat_ws(
						' ',
						COALESCE(dc.metadata ->> 'model', ''),
						COALESCE(dc.metadata ->> 'exact_model', ''),
						COALESCE(dc.metadata ->> 'category', ''),
						COALESCE(dc.metadata ->> 'subcategory', ''),
						COALESCE(dc.metadata ->> 'series', ''),
						COALESCE(dc.metadata ->> 'name', ''),
						COALESCE(dc.metadata ->> 'installation', ''),
						COALESCE(dc.metadata ->> 'door_material', ''),
						COALESCE(dc.metadata ->> 'force_type', ''),
						COALESCE(dc.metadata ->> 'base_material', ''),
						COALESCE(dc.metadata ->> 'family_prefix', ''),
						COALESCE(dc.metadata ->> 'variant_models', '')
					)
				)
			) > 0
		  )
		ORDER BY score DESC, dc.id
		LIMIT $3;
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
