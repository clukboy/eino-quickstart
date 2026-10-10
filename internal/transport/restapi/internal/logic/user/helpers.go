package user

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/platform/persistence/account"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{3,32}$`)

const usernameRuleMessage = "用户名需为 3-32 位的字母、数字、下划线、点或连字符"

const nicknameLimit = 24

const nicknameRuleMessage = "昵称不能为空，且不超过 24 个字符"

func accountStore(svcCtx *svc.ServiceContext) (*account.Store, error) {
	if svcCtx.AccountStore == nil {
		return nil, httpx.Unavailable("账号功能未开启")
	}

	return svcCtx.AccountStore, nil
}

func normalizeUsername(raw string) (string, error) {
	username := strings.TrimSpace(raw)
	if !usernamePattern.MatchString(username) {
		return "", httpx.BadRequest(usernameRuleMessage)
	}

	return username, nil
}

func normalizeNickname(raw string) (string, error) {
	nickname := strings.TrimSpace(raw)
	if nickname == "" || utf8.RuneCountInString(nickname) > nicknameLimit {
		return "", httpx.BadRequest(nicknameRuleMessage)
	}

	return nickname, nil
}

func normalizeKeyword(raw string) string {
	return strings.TrimSpace(raw)
}

func userDTO(record *account.Record) types.UserResp {
	return types.UserResp{
		ID:                 record.ID,
		Subject:            auth.NewAccountSubject(record.ID),
		Username:           record.Username,
		Nickname:           record.Nickname,
		Role:               record.Role,
		Status:             record.Status,
		MustChangePassword: record.MustChangePassword,
		CreatedAt:          record.CreatedAt.UnixMilli(),
	}
}
