package tasks

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestDecodeKnowledgeIndexWithoutModeIsCatchUp 守住「旧 payload 不需要迁移」。
//
// 两个进程是分开部署、分别重启的：升级期间 worker 还在跑旧版本、或者队列里
// 躺着升级前投出去的消息，都是常态。旧 payload 里没有 mode 字段，解出来必须
// 是「补齐」，而不是零值之外的任何东西 —— 一旦被当成「重建」，每次重试都会把
// 分块全部重切一遍，白烧 embedding。
func TestDecodeKnowledgeIndexWithoutModeIsCatchUp(t *testing.T) {
	payload, err := DecodeKnowledgeIndex([]byte(`{"dataset_id":"2","document_id":"17"}`))
	if err != nil {
		t.Fatalf("解析旧 payload: %v", err)
	}
	if payload.Mode != IndexModeCatchUp {
		t.Errorf("旧 payload 的 mode 应当是 CatchUp，实际 %q", payload.Mode)
	}
	if payload.DocumentID != "17" {
		t.Errorf("document_id 解析错误: %q", payload.DocumentID)
	}
}

// TestKnowledgeIndexModeRoundTrip 钉住重建意图能跨进程传递。
//
// 这条是「显式 reindex 真的会重切」的前提：意图只存在于 HTTP 进程的内存里
// 是不够的，必须随任务落到 Redis，worker 那边才看得见。
func TestKnowledgeIndexModeRoundTrip(t *testing.T) {
	raw, err := EncodeKnowledgeIndex(KnowledgeIndexPayload{
		DatasetID:  "2",
		DocumentID: "17",
		Mode:       IndexModeRebuild,
	})
	if err != nil {
		t.Fatalf("编码: %v", err)
	}
	if !strings.Contains(string(raw), `"mode":"rebuild"`) {
		t.Errorf("重建意图没有写进 payload: %s", raw)
	}
	decoded, err := DecodeKnowledgeIndex(raw)
	if err != nil {
		t.Fatalf("解码: %v", err)
	}
	if decoded.Mode != IndexModeRebuild {
		t.Errorf("mode 应当原样带回来，实际 %q", decoded.Mode)
	}
}

// TestEncodeKnowledgeIndexOmitsCatchUpMode 钉住常规写入的 payload 形状不变。
//
// 补齐是绝大多数任务（创建、改正文）的模式，给它们多写一个 "mode":"" 既没有
// 信息量，又会让「回滚到旧 worker」多一个无谓的变量。
func TestEncodeKnowledgeIndexOmitsCatchUpMode(t *testing.T) {
	raw, err := EncodeKnowledgeIndex(KnowledgeIndexPayload{DatasetID: "2", DocumentID: "17"})
	if err != nil {
		t.Fatalf("编码: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("payload 不是合法 JSON: %v", err)
	}
	if _, exists := fields["mode"]; exists {
		t.Errorf("补齐模式不该写出 mode 字段: %s", raw)
	}
}
