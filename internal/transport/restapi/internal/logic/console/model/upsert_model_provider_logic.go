package model

import (
	"context"
	"eino-quickstart/ent"
	"eino-quickstart/ent/modelprovider"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
	"strings"
)

type UpsertModelProviderLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpsertModelProviderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpsertModelProviderLogic {
	return &UpsertModelProviderLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UpsertModelProviderLogic) UpsertModelProvider(req *types.UpsertModelProviderReq) (*types.ModelProviderResp, error) {
	if strings.TrimSpace(req.Key) == "" || strings.TrimSpace(req.Name) == "" {
		return nil, httpx.BadRequest("provider key and name are required")
	}

	// 密钥只以密文落库。
	//
	// 空字符串的语义是「这次不改密钥」，不是「把密钥清空」：console 只能回显
	// has_secret，拿不到明文，所以每次保存表单都带不上 secret。若把空值当清空，
	// 用户改一下 provider 名字就会把自己的 API Key 抹掉。
	//
	// 没有主密钥时明确拒绝，而不是把明文写进 secret_ciphertext —— 那等于给一个
	// 叫 ciphertext 的字段塞明文，比不存更危险。
	var ciphertext *string
	if req.Secret != "" {
		if l.svcCtx.SecretBox == nil {
			return nil, httpx.Internal("model provider secret encryption is not configured")
		}

		sealed, err := l.svcCtx.SecretBox.Seal(req.Secret)
		if err != nil {
			return nil, fail(err)
		}
		ciphertext = &sealed
	}

	row, e := l.svcCtx.EntClient.ModelProvider.Query().Where(modelprovider.KeyEQ(req.Key)).Only(l.ctx)
	if ent.IsNotFound(e) {
		create := l.svcCtx.EntClient.ModelProvider.Create().
			SetKey(req.Key).
			SetName(req.Name).
			SetBaseURL(req.BaseURL).
			SetAuthType(modelprovider.AuthType(req.AuthType)).
			SetEnabled(req.Enabled)
		if ciphertext != nil {
			create = create.SetSecretCiphertext(*ciphertext)
		}
		row, e = create.Save(l.ctx)
	} else if e == nil {
		update := l.svcCtx.EntClient.ModelProvider.UpdateOne(row).
			SetName(req.Name).
			SetBaseURL(req.BaseURL).
			SetAuthType(modelprovider.AuthType(req.AuthType)).
			SetEnabled(req.Enabled)
		if ciphertext != nil {
			update = update.SetSecretCiphertext(*ciphertext)
		}
		row, e = update.Save(l.ctx)
	}
	if e != nil {
		return nil, fail(e)
	}
	return providerDTO(row), nil
}
