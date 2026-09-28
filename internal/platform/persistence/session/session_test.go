package session

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestDeriveTitle 钉住标题派生规则。
//
// 这条规则是**跨端契约**：前端的 sessionTitleFrom（src/api/model/chat.ts）与
// 库里那次历史回填的 SQL 都是它的镜像。规则漂了不会报错，只会让同一句话在
// 「刚发完消息」与「刷新之后」显示成两个不同的标题 —— 所以这里把边界钉死。
func TestDeriveTitle(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "空串", in: "", want: ""},
		// 全角空格（U+3000）也是空白：中文输入法下它是真实存在的输入。
		{name: "纯空白", in: " \t\n\u3000 ", want: ""},
		{name: "trim 并折叠内部空白", in: "  你好   世界  ", want: "你好 世界"},
		{name: "换行按空白折叠", in: "第一行\n第二行", want: "第一行 第二行"},
		{name: "正好到上限不截断", in: strings.Repeat("a", TitleLimit), want: strings.Repeat("a", TitleLimit)},
		{name: "超一个字符就截断", in: strings.Repeat("a", TitleLimit+1), want: strings.Repeat("a", TitleLimit) + "…"},
		// 中文必须按**字符**数截而不是字节数：18 个汉字是 54 字节，
		// 按字节截会把第 7 个字切成两半、拼出非法 UTF-8。
		{name: "中文按字符计", in: strings.Repeat("好", TitleLimit+5), want: strings.Repeat("好", TitleLimit) + "…"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DeriveTitle(tc.in)
			if got != tc.want {
				t.Fatalf("DeriveTitle(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("DeriveTitle(%q) 产生了非法 UTF-8：%q", tc.in, got)
			}
		})
	}
}

// TestTitleLimitIsPinned 是一道绊线，不是一个真正的断言。
//
// TitleLimit 的值同时被三处写死：本常量、前端 sessionTitleFrom 的默认参数、
// 以及历史数据回填 SQL 里的 left(..., 18)。它们分布在三种语言里、编译期互相
// 看不见，所以改这个常量时必须有人去改另外两处 —— 这条测试的作用就是让那次
// 改动不会静默通过 go test。
func TestTitleLimitIsPinned(t *testing.T) {
	if TitleLimit != 18 {
		t.Fatalf(
			"TitleLimit 改成了 %d。请同步改 src/api/model/chat.ts 的 sessionTitleFrom "+
				"默认参数与 session.go 顶部注释里提到的回填 SQL，然后更新这条测试。",
			TitleLimit,
		)
	}
}
