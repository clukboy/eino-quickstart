package dataset

import (
	"context"

	"eino-quickstart/ent"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/types"
)

// fail maps an Ent error onto the dataset error envelope.
func fail(err error) error {
	switch {
	case ent.IsNotFound(err):
		return httpx.NotFound("dataset resource not found")
	case ent.IsConstraintError(err):
		return httpx.Conflict("dataset resource already exists")
	default:
		return httpx.Internal("dataset operation failed")
	}
}

// documentFail maps a knowledge domain error onto the HTTP envelope.
//
// The domain signals validation failures with *knowledge.ValidationError, whose
// message is written for the client; everything else is matched by sentinel, so
// internal detail (SQL text, file paths) never reaches the response body.
func documentFail(err error) error {
	// var invalid *knowledge.ValidationError
	switch {
	// case errors.As(err, &invalid):
	// 	return httpx.BadRequest(invalid.Message)
	// case errors.Is(err, knowledge.ErrDatasetNotFound):
	// 	return httpx.NotFound("dataset not found")
	// case errors.Is(err, knowledge.ErrDocumentNotFound):
	// 	return httpx.NotFound("document not found")
	// case errors.Is(err, knowledge.ErrSourceConflict):
	// 	return httpx.Conflict("document source already exists")
	// case errors.Is(err, knowledge.ErrContentTooLarge):
	// 	return httpx.TooLarge("document content is too large")
	// case errors.Is(err, knowledge.ErrContentUnavailable):
	// 	// 正文文件被移走或删掉了：这是调用方能修的状态，不是服务端故障。
	// 	return httpx.BadRequest("document content is unavailable")
	case ent.IsNotFound(err):
		return httpx.NotFound("document not found")
	case ent.IsConstraintError(err):
		return httpx.Conflict("document resource already exists")
	default:
		return httpx.Internal("document operation failed")
	}
}

func DatasetDTO(base *ent.Dataset) *types.DatasetResp {
	return &types.DatasetResp{
		ID:           base.ID,
		Name:         base.Name,
		Description:  base.Description,
		OwnerSubject: base.OwnerSubject,
		Visibility:   string(base.Visibility),
		Status:       string(base.Status),
		CreatedAt:    base.CreatedAt.UnixMilli(),
		Type:         base.Type,
	}
}

func documentDTO(base *ent.Document) *types.DocumentResp {
	return &types.DocumentResp{
		ID:           base.ID,
		DatasetID:    base.DatasetID,
		Source:       base.Source,
		Title:        base.Title,
		Status:       string(base.Status),
		Visibility:   string(base.Visibility),
		OwnerSubject: base.OwnerSubject,
		CreatedAt:    base.CreatedAt.UnixMilli(),
		UpdatedAt:    base.UpdatedAt.UnixMilli(),
	}
}

// documentDTOs flattens a page of documents, taking every chunk count in one
// grouped query so the number of round trips does not grow with the page size.
func documentDTOs(ctx context.Context, docs []*ent.Document) ([]*types.DocumentResp, error) {
	ids := make([]uint64, 0, len(docs))
	for _, doc := range docs {
		ids = append(ids, doc.ID)
	}
	// stats, err := ChunkStats(ctx, ids)
	// if err != nil {
	// 	return nil, err
	// }

	result := make([]*types.DocumentResp, 0, len(docs))
	for _, doc := range docs {
		result = append(result, documentDTO(doc))
	}
	return result, nil
}

func documentDTOOne(ctx context.Context, doc *ent.Document) (*types.DocumentResp, error) {
	list, err := documentDTOs(ctx, []*ent.Document{doc})
	if err != nil {
		return nil, err
	}
	return list[0], nil
}
