package audit

import (
	"context"
	"eino-quickstart/ent/auditevent"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
	"time"
)

type ListAuditLogsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListAuditLogsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListAuditLogsLogic {
	return &ListAuditLogsLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *ListAuditLogsLogic) ListAuditLogs(req *types.AuditLogReq) (*types.AuditLogListResp, error) {
	p, s := req.Page, req.PageSize
	if p <= 0 {
		p = 1
	}
	if s <= 0 || s > 200 {
		s = 20
	}
	q := l.svcCtx.EntClient.AuditEvent.Query()
	if req.Actor != "" {
		q.Where(auditevent.ActorSubjectEQ(req.Actor))
	}
	if req.Action != "" {
		q.Where(auditevent.ActionEQ(req.Action))
	}
	if req.ResourceType != "" {
		q.Where(auditevent.ResourceTypeEQ(req.ResourceType))
	}
	if req.ResourceID != "" {
		q.Where(auditevent.ResourceIDEQ(req.ResourceID))
	}
	if req.FromMS > 0 {
		q.Where(auditevent.CreatedAtGTE(time.UnixMilli(req.FromMS)))
	}
	if req.ToMS > 0 {
		q.Where(auditevent.CreatedAtLTE(time.UnixMilli(req.ToMS)))
	}
	total, e := q.Count(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	rows, e := q.Order(auditevent.ByCreatedAt()).Offset((p - 1) * s).Limit(s).All(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	out := make([]*types.AuditLogResp, 0, len(rows))
	for _, x := range rows {
		out = append(out, dto(x))
	}
	return &types.AuditLogListResp{Data: out, Total: total, Page: p, PageSize: s}, nil
}
