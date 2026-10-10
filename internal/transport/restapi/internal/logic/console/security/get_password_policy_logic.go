package security

import (
	"context"
	"eino-quickstart/ent/passwordpolicy"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type GetPasswordPolicyLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetPasswordPolicyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetPasswordPolicyLogic {
	return &GetPasswordPolicyLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *GetPasswordPolicyLogic) GetPasswordPolicy() (*types.PasswordPolicyResp, error) {
	x, e := l.svcCtx.EntClient.PasswordPolicy.Query().Where(passwordpolicy.ConfigKeyEQ("default")).Only(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	return dto(x), nil
}
