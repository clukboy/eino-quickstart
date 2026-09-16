package admin

import (
	"net/http"

	"eino-quickstart/ent"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
)

// noContent returns an empty 204, the contract the grant/revoke endpoints have.
// go-zero's generated handler cannot emit a bare status from its (resp, err)
// return, so the status travels on a typed error.
func noContent() error {
	return httpx.New(http.StatusNoContent, "", "").Empty()
}

// client returns the dataset store, or a typed 503 when the transport was
// composed without one — for example a deployment that only serves chat.
func client(svcCtx *svc.ServiceContext) (*ent.Client, error) {
	if svcCtx.EntClient == nil {
		return nil, httpx.Unavailable("dataset management is unavailable")
	}
	return svcCtx.EntClient, nil
}

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

// DatasetDTO flattens a Dataset row into the wire type. It must stay in step
// with internal/logic/dataset's DatasetDTO: both back routes that return
// types.DatasetResp.
func DatasetDTO(base *ent.Dataset) *types.DatasetResp {
	return &types.DatasetResp{
		ID:           base.ID,
		Name:         base.Name,
		Description:  base.Description,
		OwnerSubject: base.OwnerSubject,
		Visibility:   string(base.Visibility),
		Status:       string(base.Status),
		CreatedAt:    base.CreatedAt.UnixMilli(),
	}
}
