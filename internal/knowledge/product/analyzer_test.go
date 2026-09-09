package product

import (
	"testing"
)

func TestAnalyzer_ProductModel(t *testing.T) {
	analyzer := NewAnalyzer()

	signals := analyzer.Analyze(
		"H105G 固装铰链安装方法",
	)

	if len(signals.Entities) != 1 {
		t.Fatalf(
			"expected 1 entity, got %d",
			len(signals.Entities),
		)
	}

	if signals.Entities[0].Type != "product_model" {
		t.Fatalf(
			"unexpected entity type: %s",
			signals.Entities[0].Type,
		)
	}

	if signals.Entities[0].Value != "H105G" {
		t.Fatalf(
			"expected H105G, got %s",
			signals.Entities[0].Value,
		)
	}
}

func TestAnalyzer_LowerCaseModel(t *testing.T) {
	analyzer := NewAnalyzer()

	signals := analyzer.Analyze(
		"h105g 安装说明",
	)

	if len(signals.Entities) != 1 {
		t.Fatalf(
			"expected 1 entity, got %d",
			len(signals.Entities),
		)
	}

	if signals.Entities[0].Value != "H105G" {
		t.Fatalf(
			"expected H105G, got %s",
			signals.Entities[0].Value,
		)
	}
}

func TestAnalyzer_ChineseQuery(t *testing.T) {
	analyzer := NewAnalyzer()

	signals := analyzer.Analyze(
		"H105G 固装怎么安装",
	)

	if signals.Language != "zh" {
		t.Fatalf(
			"expected zh, got %s",
			signals.Language,
		)
	}

	if len(signals.Terms) == 0 {
		t.Fatal("expected terms")
	}
}
