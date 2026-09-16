package dataset

import (
	"context"
	"errors"

	"eino-quickstart/ent"
	"eino-quickstart/internal/application/knowledge"
	"eino-quickstart/internal/platform/auth"
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

// actorSubject 取当前调用方的 subject，用作文档属主。
func actorSubject(ctx context.Context) (string, error) {
	identity, ok := auth.IdentityFromContext(ctx)
	if !ok {
		return "", httpx.Unauthorized("unauthorized")
	}
	return identity.Subject, nil
}

// documentFail maps a knowledge domain error onto the HTTP envelope.
//
// The domain signals validation failures with *knowledge.ValidationError, whose
// message is written for the client; everything else is matched by sentinel, so
// internal detail (SQL text, file paths) never reaches the response body.
func documentFail(err error) error {
	var invalid *knowledge.ValidationError
	switch {
	case errors.As(err, &invalid):
		return httpx.BadRequest(invalid.Message)
	case errors.Is(err, knowledge.ErrDatasetNotFound):
		return httpx.NotFound("dataset not found")
	case errors.Is(err, knowledge.ErrDocumentNotFound):
		return httpx.NotFound("document not found")
	case errors.Is(err, knowledge.ErrSourceConflict):
		return httpx.Conflict("document source already exists")
	case errors.Is(err, knowledge.ErrContentTooLarge):
		return httpx.TooLarge("document content is too large")
	case errors.Is(err, knowledge.ErrContentNotManaged):
		// 注册进来的外部文件不允许由接口覆盖：调用方用错了接口，不是 404。
		return httpx.BadRequest("document content is not managed by the api")
	case errors.Is(err, knowledge.ErrContentUnavailable):
		// 正文文件被移走或删掉了：这是调用方能修的状态，不是服务端故障。
		return httpx.BadRequest("document content is unavailable")
	case errors.Is(err, knowledge.ErrQueueUnavailable):
		// 索引任务投不出去。这里**不能**降级成 2xx：文档的索引永远不会发生，
		// 静默成功只会让它卡在 indexing 而没人知道。
		return httpx.Unavailable("index queue is unavailable")
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

func documentDTO(base *ent.Document, stat knowledge.ChunkStat) *types.DocumentResp {
	return &types.DocumentResp{
		ID:                base.ID,
		DatasetID:         base.DatasetID,
		Source:            base.Source,
		Title:             base.Title,
		Status:            string(base.Status),
		Visibility:        string(base.Visibility),
		OwnerSubject:      base.OwnerSubject,
		ChunkCount:        stat.Total,
		IndexedChunkCount: stat.Indexed,
		CreatedAt:         base.CreatedAt.UnixMilli(),
		UpdatedAt:         base.UpdatedAt.UnixMilli(),
	}
}

// documentDTOs flattens a page of documents, taking every chunk count in one
// pair of grouped queries so the number of round trips does not grow with the
// page size.
//
// 计数是实时聚合出来的，不是 documents 上的列：切块在 worker 内完成，请求
// 返回时根本没有一个可以落库的准确值，维护冗余列只会让它慢慢漂。
func documentDTOs(
	ctx context.Context,
	service *knowledge.Service,
	docs []*ent.Document,
) ([]*types.DocumentResp, error) {
	if len(docs) == 0 {
		return []*types.DocumentResp{}, nil
	}

	ids := make([]uint64, 0, len(docs))
	for _, doc := range docs {
		ids = append(ids, doc.ID)
	}
	stats, err := service.ChunkStats(ctx, ids)
	if err != nil {
		return nil, documentFail(err)
	}

	result := make([]*types.DocumentResp, 0, len(docs))
	for _, doc := range docs {
		result = append(result, documentDTO(doc, stats[doc.ID]))
	}
	return result, nil
}

func documentDTOOne(
	ctx context.Context,
	service *knowledge.Service,
	doc *ent.Document,
) (*types.DocumentResp, error) {
	list, err := documentDTOs(ctx, service, []*ent.Document{doc})
	if err != nil {
		return nil, err
	}
	return list[0], nil
}
