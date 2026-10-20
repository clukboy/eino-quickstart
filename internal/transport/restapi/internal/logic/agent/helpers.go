package agent

import (
	"context"
	"errors"

	"eino-quickstart/ent"
	"eino-quickstart/internal/platform/agenticmsg"
	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/platform/persistence/session"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/types"
)

// fail maps a persistence error onto the session error envelope.
//
// ErrNotFoundOrForbidden 与 ent.IsNotFound 都翻成 404，**不能**翻成 403：
// session 包刻意把「不存在」与「不是你的」合并成同一个错误，就是为了不让调用方
// 通过 uuid 试探出「这条会话存在」。传输层再按 403/404 分开，等于把被抹掉的
// 那点信息又还回去了。
func fail(err error) error {
	switch {
	case errors.Is(err, session.ErrNotFoundOrForbidden), ent.IsNotFound(err):
		return httpx.NotFound("session not found")
	default:
		return httpx.Internal("session operation failed")
	}
}

// actorSubject 取当前调用方的 subject，用做会话属主。
//
// 这一组三条路由都挂了 RoleAgent，走不到这里说明中间件没生效 —— 那是配置事故，
// 不能被当成「匿名用户」静默放过（空 subject 会让查询退化成「什么都不匹配」，
// 症状是一个看起来很正常的空列表）。
func actorSubject(ctx context.Context) (string, error) {
	identity, ok := auth.IdentityFromContext(ctx)
	if !ok || identity.Subject == "" {
		return "", httpx.Unauthorized("unauthenticated")
	}

	return identity.Subject, nil
}

func sessionDTO(record session.SessionRecord) *types.SessionResp {
	return &types.SessionResp{
		SessionID: record.ID,
		Title:     record.Title,
		CreatedAt: record.CreatedAt.UnixMilli(),
		UpdatedAt: record.UpdatedAt.UnixMilli(),
	}
}

func messageDTO(record session.MessageRecord) *types.SessionMessageResp {
	return &types.SessionMessageResp{
		ID:        record.ID,
		Role:      record.Role,
		Content:   record.Content,
		CreatedAt: record.CreatedAt.UnixMilli(),
		Blocks:    blockDTOs(record.Blocks),
	}
}

// blockDTOs 把存储层的展示块翻成接口类型。
//
// 空切片返回 nil 而不是空数组：契约里 null 的含义是「这条消息没有结构化内容，
// 请用 content」，写成 [] 会让前端多一个「有 blocks 但全是空」的分支要处理。
func blockDTOs(blocks []agenticmsg.Block) []*types.SessionMessageBlockResp {
	if len(blocks) == 0 {
		return nil
	}

	result := make([]*types.SessionMessageBlockResp, 0, len(blocks))
	for _, block := range blocks {
		result = append(result, &types.SessionMessageBlockResp{
			Type:      block.Type,
			Text:      block.Text,
			Name:      block.Name,
			Arguments: block.Arguments,
			Summary:   block.Summary,
			URL:       block.URL,
			MIMEType:  block.MIMEType,
			Filename:  block.Filename,
		})
	}

	return result
}
