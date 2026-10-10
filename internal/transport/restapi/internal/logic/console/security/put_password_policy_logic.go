package security

import (
	"context"
	"eino-quickstart/ent"
	"eino-quickstart/ent/passwordpolicy"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type PutPasswordPolicyLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewPutPasswordPolicyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PutPasswordPolicyLogic {
	return &PutPasswordPolicyLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *PutPasswordPolicyLogic) PutPasswordPolicy(req *types.PutPasswordPolicyReq) (*types.PasswordPolicyResp, error) {
	if req.MinLength < 1 {
		return nil, httpx.BadRequest("min_length must be positive")
	}
	x, e := l.svcCtx.EntClient.PasswordPolicy.Query().Where(passwordpolicy.ConfigKeyEQ("default")).Only(l.ctx)
	if e != nil && !ent.IsNotFound(e) {
		return nil, fail(e)
	}
	if ent.IsNotFound(e) {
		x, e = l.svcCtx.EntClient.PasswordPolicy.Create().SetConfigKey("default").SetMinLength(req.MinLength).SetRequireUpper(req.RequireUpper).SetRequireLower(req.RequireLower).SetRequireDigit(req.RequireDigit).SetRequireSymbol(req.RequireSymbol).SetMaxAgeDays(req.MaxAgeDays).SetHistoryCount(req.HistoryCount).SetMaxFailedAttempts(req.MaxFailedAttempts).SetLockoutSeconds(req.LockoutSeconds).SetSessionTTLHours(req.SessionTTLHours).Save(l.ctx)
	} else {
		x, e = l.svcCtx.EntClient.PasswordPolicy.UpdateOne(x).SetMinLength(req.MinLength).SetRequireUpper(req.RequireUpper).SetRequireLower(req.RequireLower).SetRequireDigit(req.RequireDigit).SetRequireSymbol(req.RequireSymbol).SetMaxAgeDays(req.MaxAgeDays).SetHistoryCount(req.HistoryCount).SetMaxFailedAttempts(req.MaxFailedAttempts).SetLockoutSeconds(req.LockoutSeconds).SetSessionTTLHours(req.SessionTTLHours).Save(l.ctx)
	}
	if e != nil {
		return nil, fail(e)
	}
	return dto(x), nil
}
