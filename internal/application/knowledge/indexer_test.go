package knowledge

import (
	"strings"
	"testing"

	"eino-quickstart/internal/platform/queue/tasks"
)

// 存量文档的正文还留在旧文件里时，导入进来的必须是**去掉 YAML 头的正文**，
// 而不是原文 —— 正文里再带一份 YAML，同一批型号词就会在索引里被计两次词频。
func TestLegacyBodyStripsFrontMatter(t *testing.T) {
	raw := "```yaml\nproduct_id: \"H105P\"\n```\n\n## H105P 产品简介\n\n正文。\n"

	body, err := legacyBody(raw)
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if body != "## H105P 产品简介\n\n正文。" {
		t.Fatalf("导入的正文应当不含 YAML 头，实际 %q", body)
	}
}

// 没有产品围栏的旧文件就是普通 Markdown，正文原样 —— 任何「顺手裁一下」都会
// 让存量语料少掉内容，而少掉的部分没有任何痕迹。
func TestLegacyBodyKeepsPlainMarkdown(t *testing.T) {
	raw := "# 公司简介\n\n图特成立于 2003 年。\n"

	body, err := legacyBody(raw)
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if body != raw {
		t.Fatalf("普通 Markdown 应当原样导入，实际 %q", body)
	}
}

// 一个旧文件里有多个产品块时不能导入：那份文件对应的是好几条文档，而这一行只是
// 其中一条。硬塞进来会把别的产品的正文也算到它头上，调用方无从分辨哪一段是谁的。
//
// 这种数据只能重新上传（会按产品拆成多条），所以这里必须报错，让文档落成 failed
// 并带上可执行的提示，而不是悄悄导入一份「混了别的产品」的正文。
func TestLegacyBodyRejectsMultiProductFile(t *testing.T) {
	raw := "```yaml\nproduct_id: \"H105P\"\n```\n\n正文一\n\n" +
		"```yaml\nproduct_id: \"H105G\"\n```\n\n正文二\n"

	_, err := legacyBody(raw)
	if err == nil {
		t.Fatal("多个产品块的旧文件应当被拒绝，而不是随便挑一块导入")
	}
	if !strings.Contains(err.Error(), "re-upload") {
		t.Fatalf("错误信息里要给出可执行的动作，实际 %v", err)
	}
}

// 坏掉的产品块（围栏不闭合、YAML 不合法）要报错，而不是当成普通 Markdown 导入：
// 那样会把一段解析不了的 YAML 当成正文，而它既不可读也不可检索。
func TestLegacyBodyRejectsBrokenBlock(t *testing.T) {
	if _, err := legacyBody("```yaml\nproduct_id: \"H105P\"\n\n正文没有闭合围栏\n"); err == nil {
		t.Fatal("围栏不闭合的块应当报错")
	}
}

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
