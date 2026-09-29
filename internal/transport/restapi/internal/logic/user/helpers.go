package user

import (
	"regexp"
	"strings"

	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/platform/persistence/account"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
)

// usernamePattern 限制登录名的形状。
//
// 收紧到 ASCII 是为了避免「看起来一样、其实是两个账号」的坑：允许中文或全角
// 字符时，同形字（l / I / 1、全角字母、西里尔 о 与拉丁 o）会让人永远登不进去
// 而且想不明白为什么。真实姓名不是登录名 —— 那是「显示名」的需求，与身份标识
// 混在一起只会让两者都做不好。
//
// 长度 3~32 的下界是为了挡住 "a" 这种一眼就会被穷举的名字（虽然真正兜底的是
// 密码与 bcrypt 成本）；上界是为了让它在日志与界面里可读。
var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{3,32}$`)

// usernameRuleMessage 与 usernamePattern 必须逐字对应 —— 前端的表单提示也用
// 这一句，规则改了要一起改，否则管理员会收到一条「按提示做却还是不通过」的
// 报错。前端目前只在输入框上写了这个提示，真正的判定只在这里。
const usernameRuleMessage = "用户名需为 3-32 位的字母、数字、下划线、点或连字符"

// accountStore 取账号库，未开启时回 503。
//
// 与 logic/auth 的 accountDeps 分开写：这一组不需要签发器（不签任何令牌），
// 要 503 的情况也不同（这里是「账号功能没开」，那里是「登录功能没开」）。共用
// 一个函数会让两组互相牵制 —— 例如将来管理台想在账号功能关闭时仍能看列表。
func accountStore(svcCtx *svc.ServiceContext) (*account.Store, error) {
	if svcCtx.AccountStore == nil {
		return nil, httpx.Unavailable("账号功能未开启")
	}

	return svcCtx.AccountStore, nil
}

// normalizeUsername 去掉首尾空白并校验格式。返回的错误可以直接展示。
func normalizeUsername(raw string) (string, error) {
	username := strings.TrimSpace(raw)
	if !usernamePattern.MatchString(username) {
		return "", httpx.BadRequest(usernameRuleMessage)
	}

	return username, nil
}

// userDTO 把持久层记录转成对外形状。
//
// subject 由 id 现算而不是从库里读：users 表没有 subject 列，归属字符串是
// 「类型 + 主键」的派生值（见 auth.NewAccountSubject）。存一份冗余的副本只会
// 引入「两处不一致时以谁为准」的问题。
func userDTO(record account.Record) types.UserResp {
	return types.UserResp{
		ID:                 record.ID,
		Subject:            auth.NewAccountSubject(record.ID),
		Username:           record.Username,
		Role:               record.Role,
		Status:             record.Status,
		MustChangePassword: record.MustChangePassword,
		CreatedAt:          record.CreatedAt.UnixMilli(),
	}
}
