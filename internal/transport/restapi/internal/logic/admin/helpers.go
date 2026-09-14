package admin

import (
	"context"
	"net/http"

	"eino-quickstart/ent"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
)

// accepted returns an empty 202, the placeholder contract of the upload
// endpoint. go-zero's generated handler cannot emit a bare status from its
// (resp, err) return, so the status travels on a typed error.
func accepted() error {
	return httpx.New(http.StatusAccepted, httpx.CodeUnavailable, "upload accepted").Empty()
}

// noContent returns an empty 204, the contract the grant/revoke endpoints had
// before the move to go-zero.
func noContent() error {
	return httpx.New(http.StatusNoContent, "", "").Empty()
}

// client returns the knowledge store, or a typed 503 when the transport was
// composed without one — for example a deployment that only serves chat. The
// net/http and Hertz transports guard the same way.
func client(svcCtx *svc.ServiceContext) (*ent.Client, error) {
	if svcCtx.EntClient == nil {
		return nil, httpx.Unavailable("knowledge management is unavailable")
	}
	return svcCtx.EntClient, nil
}

// fail maps an Ent error onto the knowledge-management error envelope. It
// preserves the semantics of the original net/http helper.
func fail(err error) error {
	switch {
	case ent.IsNotFound(err):
		return httpx.NotFound("knowledge resource not found")
	case ent.IsConstraintError(err):
		return httpx.Conflict("knowledge resource already exists")
	default:
		return httpx.Internal("knowledge operation failed")
	}
}

func DatasetDTO(base *ent.KnowledgeBase) *types.DatasetResp {
	return &types.DatasetResp{
		ID:           base.ID,
		Name:         base.Name,
		Description:  base.Description,
		OwnerSubject: base.OwnerSubject,
		Visibility:   string(base.Visibility),
		Status:       string(base.Status),
	}
}

func documentDTO(ctx context.Context, doc *ent.Document) (*types.DocumentResp, error) {
	chunkCount, err := doc.QueryChunks().Count(ctx)
	if err != nil {
		return nil, err
	}

	return &types.DocumentResp{
		ID:              doc.ID,
		KnowledgeBaseID: doc.KnowledgeBaseID,
		Source:          doc.Source,
		Title:           doc.Title,
		Status:          string(doc.Status),
		ChunkCount:      chunkCount,
	}, nil
}
