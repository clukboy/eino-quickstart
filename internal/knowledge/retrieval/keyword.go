package retrieval

import (
	"context"
	"fmt"
	"strings"

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

func (s *PostgresKeywordSearcher) Search(ctx context.Context, scope SearchScope, query string, limit int) ([]Candidate, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf(
			"keyword search database client is required",
		)
	}

	if limit <= 0 {
		return nil, fmt.Errorf(
			"keyword search limit must be greater than zero",
		)
	}

	scope = scope.Normalized()
	if !scope.HasKnowledgeBases() {
		return []Candidate{}, nil
	}

	query = strings.TrimSpace(query)

	if query == "" {
		return []Candidate{}, nil
	}

	searchText := `
		concat_ws(
			' ',
			dc.content,
			COALESCE(dc.heading_path, ''),
			COALESCE(dc.metadata::text, '')
		)
	`

	// scope 参数：
	//
	// $1 query
	// $2 KB ids
	// $3 limit
	args := []any{
		query,
		scope.KnowledgeBaseIDs,
		limit,
	}

	rows, err := s.client.QueryContext(
		ctx,
		fmt.Sprintf(`
		WITH search_data AS (
			SELECT
				dc.id,
				%s AS searchable_text
			FROM document_chunks AS dc
			JOIN documents AS d
				ON d.id = dc.document_chunks
			JOIN knowledge_bases AS kb
				ON kb.id = d.knowledge_base_id

			WHERE d.status = 'ready'

			  AND kb.status = 'ACTIVE'

			  AND d.knowledge_base_id = ANY($2)
		)

		SELECT
			id,

			(
				-- PostgreSQL FTS
				ts_rank(
					to_tsvector(
						'simple',
						searchable_text
					),
					plainto_tsquery(
						'simple',
						$1
					)
				) * 4.0

				+

				-- 完整 query 命中
				CASE
					WHEN position(
						lower($1)
						IN lower(searchable_text)
					) > 0
					THEN 2.0
					ELSE 0.0
				END

				+

				-- 对关键词逐个进行 substring 命中。
				--
				-- 这对中文非常重要：
				--
				-- H105 安装后关门有异响怎么办
				--
				-- 可以命中：
				-- 安装
				-- 关门
				-- 异响
				--
				-- 而不是依赖 PostgreSQL simple 中文分词。

				(
					SELECT
						LEAST(
							COUNT(*)::float,
							6.0
						) * 0.6

					FROM regexp_split_to_table(
						lower($1),
						'\s+'
					) AS keyword

					WHERE length(keyword) >= 2

					  AND position(
						keyword
						IN lower(searchable_text)
					  ) > 0
				)

			) AS score

		FROM search_data

		WHERE
			to_tsvector(
				'simple',
				searchable_text
			)
			@@ plainto_tsquery(
				'simple',
				$1
			)

			OR position(
				lower($1)
				IN lower(searchable_text)
			) > 0

			OR EXISTS (
				SELECT 1
				FROM regexp_split_to_table(
					lower($1),
					'\s+'
				) AS keyword

				WHERE length(keyword) >= 2

				  AND position(
					keyword
					IN lower(searchable_text)
				  ) > 0
			)

		ORDER BY
			score DESC,
			id

		LIMIT 		$3
		`, searchText),
		args...,
	)

	if err != nil {
		return nil, fmt.Errorf(
			"keyword search: %w",
			err,
		)
	}

	defer rows.Close()

	results := make([]Candidate, 0, limit)

	for rows.Next() {
		var item Candidate

		if err := rows.Scan(
			&item.ChunkID,
			&item.Score,
		); err != nil {
			return nil, fmt.Errorf(
				"scan keyword result: %w",
				err,
			)
		}

		results = append(results, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}
