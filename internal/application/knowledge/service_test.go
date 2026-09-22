package knowledge

import (
	"context"
	"errors"
	"strings"
	"testing"

	"eino-quickstart/internal/rag/constant"
)

// 正文长度上限要拦住，且必须是**拒绝**而不是截断。
//
// 截断一份正文等于把索引建在一份不完整的原文上：切出来的分块少了几段，而调用方
// 从响应里看不出少了什么。超过上限只能是调用方的问题，报出来比默默少存强。
func TestCheckContentSizeEnforcesLimit(t *testing.T) {
	service := &Service{maxDocumentBytes: 8}

	if err := service.checkContentSize("12345678"); err != nil {
		t.Fatalf("正好到上限不该被拒: %v", err)
	}
	if err := service.checkContentSize("123456789"); !errors.Is(err, ErrContentTooLarge) {
		t.Fatalf("超上限应返回 ErrContentTooLarge，实际 %v", err)
	}
	// 上限为 0 表示没配，此时不设限（缺省值在 NewService 里补）。
	unlimited := &Service{}
	if err := unlimited.checkContentSize(strings.Repeat("铰", 1<<16)); err != nil {
		t.Fatalf("没配上限时不该拒绝: %v", err)
	}
}

// 产品文档的元数据必须同时带上产品块的业务键、溯源信息、以及这次写入的指纹。
//
// 覆盖关系是这份函数唯一容易写错的地方：产品块的键要盖过调用方给的键（业务身份
// 的真相在正文的 YAML 头里），而 source/title/指纹又必须盖过产品块的键（它们是
// 服务端说了算的，调用方和正文都不能改）。
func TestMergeProductMetadataLayering(t *testing.T) {
	spec := productDocument{
		Metadata: map[string]any{
			constant.MetaModel:  "H105P",
			constant.MetaSource: "文档里伪造的 source", // 必须被服务端的值盖掉
			"extra_from_block":  "块里的键",
		},
		Fingerprint: "abc123",
	}
	extra := map[string]string{
		constant.MetaModel: "调用方伪造的型号", // 必须被产品块的值盖掉
		"from_caller":      "调用方的键",
	}

	metadata := mergeProductMetadata(spec, extra, "documents/2/h105p-abc.md", "H105P 铰链")

	if metadata[constant.MetaModel] != "H105P" {
		t.Fatalf("产品块里的型号应当盖过调用方给的，实际 %v", metadata[constant.MetaModel])
	}
	if metadata[constant.MetaSource] != "documents/2/h105p-abc.md" {
		t.Fatalf("source 必须由服务端决定，实际 %v", metadata[constant.MetaSource])
	}
	if metadata[constant.MetaTitle] != "H105P 铰链" {
		t.Fatalf("title 必须由服务端决定，实际 %v", metadata[constant.MetaTitle])
	}
	if metadata[constant.MetaSpecHash] != "abc123" {
		t.Fatalf("指纹要落库，否则下次重传无从比对，实际 %v", metadata[constant.MetaSpecHash])
	}
	if metadata["from_caller"] != "调用方的键" || metadata["extra_from_block"] != "块里的键" {
		t.Fatalf("两边的键都该保留，实际 %#v", metadata)
	}
}

// 读不出指纹时要当作「不确定」而不是「没变」。
//
// 返回空串与任何指纹都不相等，于是这次上传按内容变了处理、重新写一遍。反过来
// （拿零值当「没变」）的坏结果是**真的改动被永久忽略**，而症状是「明明传了新版本，
// 搜出来还是旧的」，且没有任何报错。
func TestStoredSpecHashTreatsUnreadableAsChanged(t *testing.T) {
	if got := storedSpecHash(nil); got != "" {
		t.Fatalf("没有元数据时应返回空串，实际 %q", got)
	}
	if got := storedSpecHash(map[string]any{constant.MetaSpecHash: 42}); got != "" {
		t.Fatalf("类型不对时应返回空串，实际 %q", got)
	}
	if got := storedSpecHash(map[string]any{constant.MetaSpecHash: "abc"}); got != "abc" {
		t.Fatalf("正常值应原样返回，实际 %q", got)
	}
}

// 补正文的查询要按文档去重，并且跳过没有 document_id 的命中。
//
// 不跳过零值会白跑一趟查询（0 永远查不到东西）；不去重则会把同一个文档问好几遍，
// 而代价换不来任何东西。
func TestDocumentContentsDedupesAndSkipsZeroID(t *testing.T) {
	stub := &stubContent{contents: map[uint64]string{1: "一", 2: "二"}}
	hits := []SearchHit{
		{DocumentID: 1},
		{DocumentID: 0},
		{DocumentID: 2},
		{DocumentID: 1},
	}

	got, err := documentContents(context.Background(), stub, hits)
	if err != nil {
		t.Fatalf("取正文失败: %v", err)
	}
	if len(stub.asked) != 2 {
		t.Fatalf("应当只问 2 个文档，实际 %v", stub.asked)
	}
	if got[1] != "一" || got[2] != "二" {
		t.Fatalf("正文不对: %#v", got)
	}
}

func TestDocumentContentsWithoutReaderOrIDs(t *testing.T) {
	hits := []SearchHit{{DocumentID: 1}}

	if got, err := documentContents(context.Background(), nil, hits); err != nil || got != nil {
		t.Fatalf("没有端口时应安静返回空，实际 %#v, %v", got, err)
	}

	stub := &stubContent{}
	if got, err := documentContents(context.Background(), stub, []SearchHit{{DocumentID: 0}}); err != nil || got != nil {
		t.Fatalf("全是无 id 的命中时不该查库，实际 %#v, %v", got, err)
	}
	if stub.calls != 0 {
		t.Fatalf("不该发出查询，实际 %d 次", stub.calls)
	}
}
