// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package agent

import (
	"context"

	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
)

type CreateSessionLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreateSessionLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateSessionLogic {
	return &CreateSessionLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// CreateSession creates (or reuses) a session owned by the calling subject.
func (l *CreateSessionLogic) CreateSession() (resp *types.CreateSessionResp, err error) {
	identity, ok := auth.IdentityFromContext(l.ctx)
	if !ok {
		return nil, httpx.Unauthorized("unauthenticated")
	}

	id := uuid.NewString()
	if err = l.svcCtx.Sessions.GetOrCreate(l.ctx, id, identity.Subject); err != nil {
		return nil, httpx.Internal("create session failed")
	}

	return &types.CreateSessionResp{SessionID: id}, nil
}
