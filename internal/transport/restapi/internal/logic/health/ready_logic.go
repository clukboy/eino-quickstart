// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package health

import (
	"context"
	"time"

	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ReadyLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewReadyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ReadyLogic {
	return &ReadyLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// Ready checks the primary database dependency.
//
// The failure path keeps the original probe contract: 503 with
// {"status":"not ready","error":"..."}. Kubernetes only reads the status code,
// but the net/http and Hertz transports both ship the body, so it stays.
func (l *ReadyLogic) Ready() (resp *types.ReadyResp, err error) {
	probeCtx, cancel := context.WithTimeout(l.ctx, 2*time.Second)
	defer cancel()

	if err = l.svcCtx.Sessions.Ready(probeCtx); err != nil {
		return nil, httpx.Unavailable("not ready").WithBody(&types.ReadyResp{
			Status: "not ready",
			Error:  err.Error(),
		})
	}

	return &types.ReadyResp{Status: "ready"}, nil
}
