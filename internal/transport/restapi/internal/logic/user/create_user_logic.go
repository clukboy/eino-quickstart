// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package user

import (
	"context"
	"errors"

	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/platform/persistence/account"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type CreateUserLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 创建账号（角色固定 agent，初始密码由管理员设定并强制首登改密）
func NewCreateUserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateUserLogic {
	return &CreateUserLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// CreateUser 建一个账号。这是系统里**唯一**产生账号的入口（没有注册接口）。
//
// ── 角色不能传 ────────────────────────────────────────────────────
// 本方法从头到尾没有出现 role：store.Create 的签名里也没有。新账号一律 agent，
// 所以管理后台造不出第二个管理员 —— 要给某人提权只能改库
// （UPDATE users SET role='admin'）。这是「不允许注册」这条需求推到极致的必然
// 结果，也是刻意的：把提权能力从接口里拿掉，就不存在「一次参数校验疏漏导致
// 全员变管理员」的可能。要改的话请给 CreateUserReq 加 role 字段并在本方法里
// 限制取值，不要去动 store.Create 的签名 —— 那层是故意做成拿不到提权能力的。
//
// ── 初始密码是共享凭证 ────────────────────────────────────────────
// 管理员设的这个密码他本人知道，所以它只能用一次：新账号的
// must_change_password 为真（store.Create 硬编码），在改密之前除
// /api/v1/auth/* 之外什么都访问不了（middleware/password_guard.go）。响应里
// 把 must_change_password 回给前端，就是为了让管理员看到「待改密」这一列，
// 从而明白这枚密码不是长期凭证。
func (l *CreateUserLogic) CreateUser(req *types.CreateUserReq) (resp *types.UserResp, err error) {
	store, err := accountStore(l.svcCtx)
	if err != nil {
		return nil, err
	}

	username, err := normalizeUsername(req.Username)
	if err != nil {
		return nil, err
	}

	// ValidatePassword 的消息是给用户看的，原样透出（见 account.ValidatePassword
	// 的注释：文案只有一份，避免建号说 8 位、改密说 6 位）。
	if err := account.ValidatePassword(req.Password); err != nil {
		return nil, httpx.BadRequest(err.Error())
	}

	passwordHash, err := account.HashPassword(req.Password, l.svcCtx.PasswordHashCost)
	if err != nil {
		l.Errorf("hash initial password failed username=%s: %v", username, err)
		return nil, httpx.Internal("创建账号失败")
	}

	record, err := store.Create(l.ctx, username, passwordHash)
	if errors.Is(err, account.ErrUsernameTaken) {
		// 409 而不是 400：用户名重复是**资源冲突**，前端的处置也不同（把输入框
		// 标红并提示换一个），与「格式不对」不是一类。
		return nil, httpx.Conflict("用户名已存在")
	}
	if err != nil {
		l.Errorf("create account failed username=%s: %v", username, err)
		return nil, httpx.Internal("创建账号失败")
	}

	// 记 subject（= user:<id>）而不是密码或哈希。管理员的操作值得留痕：这条
	// 日志是「这个账号是谁建的、什么时候建的」唯一能回答的地方（users 表里
	// 没有 creator 列）。
	l.Infof(
		"account created subject=%s username=%s role=%s must_change_password=%t",
		auth.NewAccountSubject(record.ID),
		record.Username,
		record.Role,
		record.MustChangePassword,
	)

	dto := userDTO(*record)

	return &dto, nil
}
