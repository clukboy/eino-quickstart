package knowledge

import (
	"testing"

	"eino-quickstart/internal/platform/queue/tasks"
)

// TestShouldRechunk 钉住「常规写入」与「显式 reindex」的分歧点。
//
// 这条不变量原先只写在注释里：IndexDocument 收下了 mode 却从不读它，于是显式
// reindex 在正文没变时静默空转 —— 分块行、向量、检索文档一个都没重写，而文档
// 状态会诚实地变回 ready。对外表现是「reindex 成功返回，但搜出来的还是老样子」，
// 且没有任何报错或降级标记可供定位，只能靠人肉比对索引内容才能发现。
//
// 三处注释都写明了下述意图（tasks.go 的 IndexModeRebuild、indexqueue.go 的入队
// 说明、IndexDocument 的文档注释），唯一的漏洞就是判断本身 —— 所以这里逐一列出，
// 让「收下参数却不生效」这种缺口不能再悄悄回来。
func TestShouldRechunk(t *testing.T) {
	cases := []struct {
		name           string
		mode           tasks.IndexMode
		alreadyChunked bool
		want           bool
	}{
		{
			name:           "常规写入且指纹一致：复用已有分块，不重新 embedding",
			mode:           tasks.IndexModeCatchUp,
			alreadyChunked: true,
			want:           false,
		},
		{
			name:           "常规写入但指纹变了：重切",
			mode:           tasks.IndexModeCatchUp,
			alreadyChunked: false,
			want:           true,
		},
		{
			name:           "显式 reindex 且指纹一致：仍然要重切（这条就是曾经失效的那条）",
			mode:           tasks.IndexModeRebuild,
			alreadyChunked: true,
			want:           true,
		},
		{
			name:           "显式 reindex 且指纹变了：重切",
			mode:           tasks.IndexModeRebuild,
			alreadyChunked: false,
			want:           true,
		},
		{
			name:           "空 mode 等价于常规写入（旧 payload 不需要迁移）",
			mode:           tasks.IndexMode(""),
			alreadyChunked: true,
			want:           false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldRechunk(tc.mode, tc.alreadyChunked); got != tc.want {
				t.Fatalf("shouldRechunk(%q, %v) = %v, want %v",
					tc.mode, tc.alreadyChunked, got, tc.want)
			}
		})
	}
}
