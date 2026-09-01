package retrieval

import (
	"context"
	"eino-quickstart/ent"
	"fmt"
	"strings"
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
// 1. product_model
// 2. product_exact_model
// 3. product_family_prefix
// 4. product_variant_models
func (s *ProductSearcher) SearchModel(ctx context.Context, actorSubject string, model string, limit int) ([]Candidate, error) {
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

	model = strings.TrimSpace(model)

	if model == "" {
		return []Candidate{}, nil
	}

	rows, err := s.client.QueryContext(
		ctx,
		`
		SELECT dc.id,
			   CASE
				   -- 1. 精确匹配 exact_model
				   WHEN lower(COALESCE(dc.metadata ->> 'exact_model', '')) = lower($1)
					   THEN 4.0
		
				   -- 2. 匹配 model
				   WHEN lower(COALESCE(dc.metadata ->> 'model', '')) = lower($1)
					   THEN 3.0
		
				   -- 3. 匹配 family_prefix
				   WHEN lower(COALESCE(dc.metadata ->> 'family_prefix', '')) = lower($1)
					   THEN 2.0
		
				   -- 4. 匹配 variant_models (JSON 数组)
				   WHEN (
					   dc.metadata -> 'variant_models' IS NOT NULL
					   AND (
						   dc.metadata -> 'variant_models' @> to_jsonb($1::text)
						   OR position(lower($1) in lower(dc.metadata ->> 'variant_models')) > 0
					   )
				   )
					   THEN 1.0
		
				   ELSE 0.0
				   END AS score
		
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
			lower(COALESCE(dc.metadata ->> 'exact_model', '')) = lower($1)
				OR lower(COALESCE(dc.metadata ->> 'model', '')) = lower($1)
				OR lower(COALESCE(dc.metadata ->> 'family_prefix', '')) = lower($1)
				OR (
					dc.metadata -> 'variant_models' IS NOT NULL
					AND (
						dc.metadata -> 'variant_models' @> to_jsonb($1::text)
						OR position(lower($1) in lower(dc.metadata ->> 'variant_models')) > 0
					)
				)
			)
		
		ORDER BY score DESC, dc.id
		LIMIT $3
		`,
		model,
		actorSubject,
		limit,
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

		if err := rows.Scan(&item.ChunkID, &item.Score); err != nil {
			return nil, fmt.Errorf("scan product model result: %w", err)
		}

		results = append(results, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}
