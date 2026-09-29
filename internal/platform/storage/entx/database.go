package entx

import (
	"context"
	"fmt"
	"net/url"

	"eino-quickstart/ent"
	"eino-quickstart/ent/schema/hook"
	"eino-quickstart/internal/platform/config"

	"entgo.io/ent/dialect/sql/schema"
	_ "github.com/lib/pq"
)

func Open(ctx context.Context, c config.Storage) (*ent.Client, error) {
	connectionURL := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.Username, c.Password),
		Host:   fmt.Sprintf("%s:%d", c.Host, c.Port),
		Path:   c.DBName,
		RawQuery: url.Values{
			"sslmode": []string{c.SSLMode},
		}.Encode(),
	}
	client, err := ent.Open("postgres", connectionURL.String())
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	// entgo 添加 trace hook 实现链路追踪
	client.Use(hook.TraceAuditHook())

	// 学习阶段自动建表；生产阶段改用 Ent/Atlas 版本化迁移。
	if err := client.Schema.Create(ctx, schema.WithDropColumn(true), schema.WithDropIndex(true), schema.WithForeignKeys(false)); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("migrate postgres schema: %w", err)
	}

	if err := backfillUserNicknames(ctx, client); err != nil {
		_ = client.Close()
		return nil, err
	}

	return client, nil
}

// backfillUserNicknames 给存量账号补上昵称。
//
// users.nickname 是后加的列，而且接口层把它当必填（见 logic/user/helpers.go
// 的 normalizeNickname）。但 DDL 只能给一个空串默认值 —— 理由见
// ent/schema/user.go 里那段：非空表加 NOT NULL 列必须带默认值，否则 PostgreSQL
// 直接拒绝迁移。所以存量行落地时全是 ''，而空昵称会在用户列表里渲染成一行
// 空白，看起来像「这一列坏了」。
//
// 用 username 回填是唯一自洽的答案：它本来就是「这个账号是谁」的既有答案，
// 而且两者同名时最不容易让人误会（昵称与登录名不一致时，管理员看到列表会
// 以为自己写错了）。
//
// ⚠️ 这条 UPDATE 每次启动都会跑，这是刻意的：ent 的 Schema.Create 不报告
// 「哪一列是这次新加的」，靠一个自己维护的版本号去判断反而会引入新的失配
// 可能。语句是幂等的 —— WHERE nickname = '' 的命中行只会越来越少，而空昵称
// 在接口层已经被禁止，所以它实际只在第一次启动（以及有人绕过接口直接写库
// 之后）才会真正改到数据。恒 0 行的 UPDATE 代价可以忽略。
func backfillUserNicknames(ctx context.Context, client *ent.Client) error {
	if _, err := client.ExecContext(
		ctx,
		`UPDATE users SET nickname = username WHERE nickname = ''`,
	); err != nil {
		return fmt.Errorf("backfill user nicknames: %w", err)
	}

	return nil
}
