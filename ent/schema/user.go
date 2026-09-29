package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// User holds the schema definition for the User entity.
//
// 账号**只能由管理员创建**（POST /api/v1/users，挂 RoleAdmin）；系统里没有
// 注册入口。这条约束不是靠界面藏掉一个按钮实现的，而是靠「根本没有注册接口」
// —— 见 internal/transport/restapi/docs/auth/auth.api 的文件头说明。
type User struct {
	ent.Schema
}

// Fields of the User.
func (User) Fields() []ent.Field {
	return []ent.Field{
		// username 是登录名，也是「这个账号是谁」的唯一人类可读标识。
		//
		// Unique() 本身就带唯一索引，不要再加一条 index.Unique()：那会在库里
		// 生成两个功能重复的索引（一个来自字段的 Unique，一个来自 Indexes）。
		field.String("username").Unique().NotEmpty(),

		// nickname 是**显示名**，与上面的 username（登录名）分工不同：
		// username 是身份标识（ASCII、唯一、可用来登录），nickname 是人看的名字
		// （可中文、可重名）。两者必须分开 —— usernamePattern 那段注释解释了
		// 为什么登录名要收紧到 ASCII，而「真实姓名」正是那里的反例：把它塞进
		// username 会让显示需求与标识需求互相牵制，最后两边都做不好。
		//
		// NotEmpty：本列是**接口层必填**的（见 logic/user/helpers.go 的
		// normalizeNickname），建号时不带昵称会被回 400。
		//
		// ⚠️ entsql.DefaultExpr("''") 是**给 ALTER TABLE 用的**，理由与下面
		// updated_at 那段完全相同，别删：这一列加在一张**已存在**的表上，只写
		// NotEmpty 的话 ent 生成的 DDL 是 `ADD COLUMN nickname ... NOT NULL`
		// （没有默认值），PostgreSQL 对任何非空表都会直接报
		// `column "nickname" contains null values` —— 后端起不来，而且失败发生在
		// 迁移阶段，看起来像「代码有问题」而不是「DDL 缺默认值」。
		// 有了这个注解，DDL 才是 `ADD COLUMN nickname ... NOT NULL DEFAULT ''`，
		// 存量行先落成空串，再由 entx.Open 的启动回填按 username 补上。
		//
		// 它不影响「接口必填」：默认值只作用于数据库层，ent 的 Create() 仍会按
		// NotEmpty 在校验阶段拒绝空串。
		field.String("nickname").
			NotEmpty().
			Annotations(entsql.DefaultExpr("''")),

		// password_hash 是 bcrypt 的产物（$2a$...），**永远不出后端**：
		// 所有 DTO 里都没有这个字段，日志也不记它。见 logic/user 与 logic/auth。
		field.String("password_hash").NotEmpty(),

		// role 决定这个账号能进哪些功能组（见 middleware/roles.go，admin 恒真）。
		//
		// 默认 agent：建号接口不接受 role 参数（决策 5），所以常规路径下这里
		// 永远是 agent。要造管理员只能改库 —— 那是刻意的，避免一次误配置把
		// 所有人都变成 admin。
		field.Enum("role").Values("agent", "approver", "admin").Default("agent"),

		// status 目前只有「停用」一个用途：登录时 status != ACTIVE 一律 401。
		// 注意它挡不住**已签发**的令牌 —— 令牌是无状态的（见 auth.AccountIssuer），
		// 停用要等令牌过期才生效。
		field.Enum("status").Values("ACTIVE", "DISABLED").Default("ACTIVE"),

		// must_change_password 默认 true：管理员设定的初始密码只能用一次。
		//
		// 它会被签进令牌载荷（tokenPayload.mcp），并由
		// middleware/password_guard_middleware.go 在服务端强制 —— 改密之前，
		// 除 /api/v1/auth/ 之外的所有接口一律 403。只在前端拦是拦不住的
		// （curl 就绕过了），而管理员知道的初始密码会一直有效。
		field.Bool("must_change_password").Default(true),

		// password_changed_at 为空表示「从未改过密码」。它不参与鉴权，
		// 只用于运维排查「这个账号的密码是什么时候换的」。
		field.Time("password_changed_at").Optional().Nillable(),

		field.Time("created_at").Default(time.Now).Immutable(),

		// updated_at 的 DefaultExpr 是**给数据库 DDL 用的**，理由与
		// session.go 里那段完全一样：只写 field.Default 的话 ent 不会把它落到
		// 建表语句里，日后给一张已存在的表补这一列会变成
		// ADD COLUMN ... NOT NULL（没有默认值），PostgreSQL 直接报
		// "contains null values"，启动即迁移失败。有了这个注解，DDL 才是
		// ... NOT NULL DEFAULT now()。
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now).
			Annotations(entsql.DefaultExpr("now()")),
	}
}

// Edges of the User.
func (User) Edges() []ent.Edge {
	return nil
}

func (User) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("users"),
		schema.Comment("User represents an administrator-created account."),
	}
}
