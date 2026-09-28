// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package auth

import (
	"context"
	"time"

	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type IssueAnonymousTokenLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 签发匿名身份令牌（带旧令牌时为续期）
func NewIssueAnonymousTokenLogic(ctx context.Context, svcCtx *svc.ServiceContext) *IssueAnonymousTokenLogic {
	return &IssueAnonymousTokenLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// IssueAnonymousToken 签发匿名身份令牌。契约与取舍写在 docs/auth/auth.api。
//
// 三条判断，各自都有必须如此的道理：
//
//	issuer 为 nil        → 503。这个部署不提供匿名身份。**不能**退化成「签一个
//	                       空身份」或落回某个默认 subject —— 那会让所有访客
//	                       重新变成同一个人，正是本接口要解决的问题，而且退化
//	                       之后没有任何症状。
//	带了 token 且验签通过 → 复用它的 subject，这是续期（刷新页面不换人）。
//	其余                 → 新建一个 anon:<uuid>。
func (l *IssueAnonymousTokenLogic) IssueAnonymousToken(
	req *types.AnonymousTokenReq,
) (resp *types.AnonymousTokenResp, err error) {
	issuer := l.svcCtx.Anonymous
	if issuer == nil {
		return nil, httpx.Unavailable("匿名身份未开启")
	}

	// 旧令牌无效或已过期时不报错，直接按「还没有身份」处理：客户端能做的动作
	// 与首次申请完全一样（存下新令牌），为一个它无法补救的原因回错误，只会让
	// 每个调用方都多写一个必然走不到别处的分支。
	subject := ""
	if req.Token != "" {
		if identity, ok := issuer.Verify(req.Token); ok {
			subject = identity.Subject
		}
	}

	token, subject, expiresAt, err := issuer.Issue(subject)
	if err != nil {
		l.Errorf("issue anonymous token failed: %v", err)
		return nil, httpx.Internal("签发匿名身份失败")
	}

	// 记一条日志：匿名身份在库里只留下一个字符串归属，这是唯一能回答「它是
	// 什么时候、以什么方式出现的」的地方。这里不记 token 本身 —— 它等同于
	// 那个身份的凭证。
	origin := "new"
	if req.Token != "" {
		origin = "renewal_attempt"
	}
	l.Infof(
		"anonymous token issued subject=%s origin=%s expires_at=%s",
		subject,
		origin,
		expiresAt.Format(time.RFC3339),
	)

	return &types.AnonymousTokenResp{
		Token:     token,
		Subject:   subject,
		Role:      string(issuer.Role()),
		IssuedAt:  time.Now().UnixMilli(),
		ExpiresAt: expiresAt.UnixMilli(),
	}, nil
}
