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

// usernamePattern 限制登录名的形状。
//
// 收紧到 ASCII 是为了避免「看起来一样、其实是两个账号」的坑：允许中文或全角
// 字符时，同形字（l / I / 1、全角字母、西里尔 о 与拉丁 o）会让人永远登不进去
// 而且想不明白为什么。真实姓名不是登录名 —— 那是「显示名」的需求（昵称就是
// 为它准备的），与身份标识混在一起只会让两者都做不好。
//
// 长度 3~32 的下界是为了挡住 "a" 这种一眼就会被穷举的名字（虽然真正兜底的是
// 密码与 bcrypt 成本）；上界是为了让它在日志与界面里可读。
var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{3,32}$`)

// usernameRuleMessage 与 usernamePattern 必须逐字对应 —— 前端的表单提示也用
// 这一句，规则改了要一起改，否则管理员会收到一条「按提示做却还是不通过」的
// 报错。前端目前只在输入框上写了这个提示，真正的判定只在这里。
const usernameRuleMessage = "用户名需为 3-32 位的字母、数字、下划线、点或连字符"

// nicknameLimit 是昵称的长度上界，单位是**字符**（rune）而不是字节。
//
// 按字节算会让「张三」这种正常的中文名占掉 6 个额度，于是同一句提示对中文
// 用户实际只有三分之一的效果 —— 而且报错时用户怎么数都数不出「超了」。
//
// 它比 username 的上界（32）小，因为昵称是给人看的：界面上要放得下、列表里
// 不该被截断成「……」，而登录名再长也只是个标识。
const nicknameLimit = 24

// nicknameRuleMessage 与 nicknameLimit 必须逐字对应，且要和前端的表单提示
// 一致（见 views/users/UserCreateModal.vue）。理由同 usernameRuleMessage。
const nicknameRuleMessage = "昵称不能为空，且不超过 24 个字符"

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

// normalizeNickname 去掉首尾空白并校验昵称。返回的错误可以直接展示。
//
// 与 username 的差别只有「不限制字符集」一条：昵称本来就该允许中文、空格、
// emoji（真实姓名与花名都可能带这些）。这里唯一挡的是空与超长 ——
// 空是因为接口把昵称当必填，超长是为了不让列表行被撑坏。
//
// ⚠️ 只 TrimSpace、不折叠中间的连续空白：把「张<两个空格>三」折成「张 三」
// 看起来更整洁，但那是在猜用户的意图，而且会让「管理员填的」和「库里存的」
// 不一致。首尾空白是另一回事 —— 那多半是粘贴带来的噪声，去掉它没有歧义。
func normalizeNickname(raw string) (string, error) {
	nickname := strings.TrimSpace(raw)
	if nickname == "" || utf8.RuneCountInString(nickname) > nicknameLimit {
		return "", httpx.BadRequest(nicknameRuleMessage)
	}

	return nickname, nil
}

// normalizeKeyword 归一化列表的搜索关键字。
//
// 全空白等价于「不搜索」而不是「搜一个空格」：后者在用户名与昵称里都几乎不可能
// 命中，返回空列表会让管理员以为「搜不到」而不是「没输入东西」。而归一化放在
// 这里而不是 store，是为了让持久层只判断「空串 = 不过滤」这一件事。
func normalizeKeyword(raw string) string {
	return strings.TrimSpace(raw)
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
		Nickname:           record.Nickname,
		Role:               record.Role,
		Status:             record.Status,
		MustChangePassword: record.MustChangePassword,
		CreatedAt:          record.CreatedAt.UnixMilli(),
	}
}
