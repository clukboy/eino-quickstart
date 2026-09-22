package rag

import (
	"context"
	"errors"
	"strings"
	"testing"

	"eino-quickstart/internal/rag/constant"
	ragparser "eino-quickstart/internal/rag/parser"

	"github.com/cloudwego/eino/components/document"
)

// 正文在内存里时不需要 DocRoot：知识库的正文存在 documents.content，索引时
// 直接进内存，整条链没有任何一步碰磁盘。
//
// 这条守的是「正文只有一处真相」：只要 Pipeline 还要求一个目录，就迟早会有人
// 把正文写一份回去，然后盘上和库里各有一份可以各自演化。
func TestIngestContentNeedsNoDocRoot(t *testing.T) {
	pipeline, err := NewPipeline(context.Background(), Config{ChunkMaxChars: 60}, nil)
	if err != nil {
		t.Fatalf("建 Pipeline 失败: %v", err)
	}

	chunks, err := pipeline.IngestContent(context.Background(), "documents/2/h105p-abc.md", "# 标题\n\n这是一段正文。\n")
	if err != nil {
		t.Fatalf("切块失败: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("应当切出至少一个分块")
	}
	if !strings.Contains(chunks[0].Content, "这是一段正文") {
		t.Fatalf("分块内容不对: %q", chunks[0].Content)
	}
	// source 要原样进 _source 元数据：检索侧靠它回指原文，丢了它命中就只剩
	// 一段没有出处的文字。
	if got := MetaString(chunks[0], ragparser.MetaKeySource); got != "documents/2/h105p-abc.md" {
		t.Fatalf("_source 元数据不对: %q", got)
	}
}

// 产品文档的正文里**不该**再有 YAML 头（它已经进了 documents.metadata），
// 而这一步也必须能正常切块 —— 顺带钉住「不走 ProductParser」。
//
// 走 ProductParser 的话正文里没有围栏，它会拆出 0 个块，然后以「切不出任何分块」
// 的名义把文档判死。这种失败看起来像数据坏了，实际是解析路径选错了。
func TestIngestContentHandlesFrontMatterlessBody(t *testing.T) {
	pipeline, err := NewPipeline(context.Background(), Config{ChunkMaxChars: 60}, nil)
	if err != nil {
		t.Fatalf("建 Pipeline 失败: %v", err)
	}

	body := "## H105P 产品简介\n\nH105P 是二段力小角度偏心轮快装缓冲铰链。\n"
	chunks, err := pipeline.IngestContent(context.Background(), "documents/1/h105p-abc.md", body)
	if err != nil {
		t.Fatalf("切块失败: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("没有 YAML 头的正文也该切出分块")
	}
	// heading_path 是这一段在原文里的位置，检索结果靠它解释「哪一节命中了」。
	if got := MetaString(chunks[0], constant.MetaHeadingPath); !strings.Contains(got, "H105P 产品简介") {
		t.Fatalf("标题路径不对: %q", got)
	}
}

// 空正文要明确报错，不能安静地返回 0 个分块。
//
// 0 个分块在调用方那里会被读成「这份内容没有可索引的东西」，于是文档以「切不出
// 分块」的理由落成 failed —— 一个把「输入是空的」说成「内容有问题」的错误归因。
func TestIngestContentRejectsEmpty(t *testing.T) {
	pipeline, err := NewPipeline(context.Background(), Config{ChunkMaxChars: 60}, nil)
	if err != nil {
		t.Fatalf("建 Pipeline 失败: %v", err)
	}

	if _, err := pipeline.IngestContent(context.Background(), "documents/1/a.md", "   \n"); err == nil {
		t.Fatal("空正文应当报错")
	}
}

// 没配 DocRoot 时读文件要报装配错误，而不是「文件不存在」。
//
// 后者会让运维去磁盘上找那个文件，而真正的问题是这条链压根没装配读文件的能力。
func TestIngestFileWithoutDocRootIsExplicitError(t *testing.T) {
	pipeline, err := NewPipeline(context.Background(), Config{ChunkMaxChars: 60}, nil)
	if err != nil {
		t.Fatalf("建 Pipeline 失败: %v", err)
	}

	if _, err := pipeline.IngestFile(context.Background(), document.Source{URI: "/tmp/whatever.md"}); !errors.Is(err, ErrIngestFileUnavailable) {
		t.Fatalf("期望 ErrIngestFileUnavailable，实际 %v", err)
	}
}
