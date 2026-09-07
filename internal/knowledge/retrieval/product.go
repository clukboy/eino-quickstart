package retrieval

import (
	"context"
	"fmt"
	"strings"

	"eino-quickstart/ent"
)

type ProductSearcher struct {
	client *ent.Client
}

func NewProductSearcher(client *ent.Client) *ProductSearcher {
	return &ProductSearcher{
		client: client,
	}
}

// SearchModel 根据产品型号进行精确检索。
//
// 优先级：
// 1. exact_model
// 2. model
// 3. family_prefix
// 4. variant_models
//
// 所有型号字段均采用完整值匹配。
// 不允许 substring 匹配。
func (s *ProductSearcher) SearchModel(
	ctx context.Context,
	scope SearchScope,
	model string,
	limit int,
) ([]Candidate, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf(
			"product searcher database client is required",
		)
	}

	if limit <= 0 {
		return nil, fmt.Errorf(
			"product search limit must be greater than zero",
		)
	}

	scope = scope.Normalized()

	model = strings.TrimSpace(model)

	if model == "" {
		return []Candidate{}, nil
	}

	args := []any{
		model,
		scope.ActorSubject,
	}

	kbCondition := ""

	if len(scope.KnowledgeBaseIDs) > 0 {
		kbCondition = `
		  AND d.knowledge_base_id = ANY($3)
		`
		args = append(
			args,
			scope.KnowledgeBaseIDs,
		)
	}

	limitPlaceholder := "$3"

	if len(scope.KnowledgeBaseIDs) > 0 {
		limitPlaceholder = "$4"
		args = append(args, limit)
	} else {
		args = append(args, limit)
	}

	rows, err := s.client.QueryContext(
		ctx,
		fmt.Sprintf(`
		SELECT
			dc.id,
			CASE
				WHEN lower(
					COALESCE(
						dc.metadata ->> 'exact_model',
						''
					)
				) = lower($1)
					THEN 4.0

				WHEN lower(
					COALESCE(
						dc.metadata ->> 'model',
						''
					)
				) = lower($1)
					THEN 3.0

				WHEN lower(
					COALESCE(
						dc.metadata ->> 'family_prefix',
						''
					)
				) = lower($1)
					THEN 2.0

				WHEN EXISTS (
					SELECT 1
					FROM jsonb_array_elements_text(
						COALESCE(
							dc.metadata -> 'variant_models',
							'[]'::jsonb
						)
					) AS variant(model)
					WHERE lower(variant.model) = lower($1)
				)
					THEN 1.0

				ELSE 0.0
			END AS score

		FROM document_chunks AS dc

		JOIN documents AS d
			ON d.id = dc.document_chunks

		JOIN knowledge_bases AS kb
			ON kb.id = d.knowledge_base_id

		WHERE d.status = 'ready'

		  AND kb.status = 'ACTIVE'

		  AND (
			kb.visibility = 'system'
			OR (
				kb.visibility = 'private'
				AND kb.owner_subject = $2
			)
		  )

		  AND (
			d.visibility = 'system'
			OR (
				d.visibility = 'private'
				AND d.owner_subject = $2
			)
		  )

		  %s

		  AND (
			lower(
				COALESCE(
					dc.metadata ->> 'exact_model',
					''
				)
			) = lower($1)

			OR lower(
				COALESCE(
					dc.metadata ->> 'model',
					''
				)
			) = lower($1)

			OR lower(
				COALESCE(
					dc.metadata ->> 'family_prefix',
					''
				)
			) = lower($1)

			OR EXISTS (
				SELECT 1
				FROM jsonb_array_elements_text(
					COALESCE(
						dc.metadata -> 'variant_models',
						'[]'::jsonb
					)
				) AS variant(model)
				WHERE lower(variant.model) = lower($1)
			)
		  )

		ORDER BY
			score DESC,
			dc.id

		LIMIT %s
		`, kbCondition, limitPlaceholder),
		args...,
	)

	if err != nil {
		return nil, fmt.Errorf(
			"product model search: %w",
			err,
		)
	}

	defer rows.Close()

	results := make([]Candidate, 0)

	for rows.Next() {
		var item Candidate

		if err := rows.Scan(
			&item.ChunkID,
			&item.Score,
		); err != nil {
			return nil, fmt.Errorf(
				"scan product model result: %w",
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
