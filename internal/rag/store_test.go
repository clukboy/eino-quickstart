package rag

import (
	"strings"
	"testing"
)

func containsTerm(terms []string, want string) bool {
	for _, term := range terms {
		if term == want {
			return true
		}
	}
	return false
}

func TestTokenizeKeepsModelWhole(t *testing.T) {
	terms := Tokenize("H105P")

	if !containsTerm(terms, "h105p") {
		t.Fatalf("型号必须整体保留（小写化），实际 %v", terms)
	}
}

func TestTokenizeSplitsChineseIntoBigrams(t *testing.T) {
	terms := Tokenize("滑入式铰链")

	for _, want := range []string{"滑入", "入式", "式铰", "铰链"} {
		if !containsTerm(terms, want) {
			t.Fatalf("缺少 2-gram %q，实际 %v", want, terms)
		}
	}
	// 整串也作为一个词元：完整短语命中比单个 2-gram 更有分量。
	if !containsTerm(terms, "滑入式铰链") {
		t.Fatalf("缺少整串词元，实际 %v", terms)
	}
}

func TestTokenizeHandlesMixedScriptAndPunctuation(t *testing.T) {
	terms := Tokenize("H105P 二段力铰链")

	if !containsTerm(terms, "h105p") {
		t.Fatalf("缺少型号词元，实际 %v", terms)
	}
	if !containsTerm(terms, "二段") || !containsTerm(terms, "铰链") {
		t.Fatalf("缺少中文 2-gram，实际 %v", terms)
	}
	// 空格是分隔符，不该出现在任何词元里。
	for _, term := range terms {
		if strings.ContainsAny(term, " \t\n") && term != strings.ToLower("H105P 二段力铰链") {
			t.Fatalf("词元 %q 里残留了空白字符", term)
		}
	}
}

// LIKE 的通配符不是字面量，带进 SQL 会变成无意义的宽匹配，必须在词元层面丢掉。
func TestTokenizeDropsLikeWildcards(t *testing.T) {
	terms := Tokenize("100% 的 _ 参数")

	for _, term := range terms {
		if strings.ContainsAny(term, "%_\\") {
			t.Fatalf("词元 %q 含 LIKE 通配符", term)
		}
	}
}

func TestTokenizeEmptyInput(t *testing.T) {
	for _, input := range []string{"", "   ", "\t\n"} {
		if terms := Tokenize(input); len(terms) != 0 {
			t.Fatalf("空白输入应得到空词元，输入 %q 得到 %v", input, terms)
		}
	}
}

func TestTokenizeDeduplicates(t *testing.T) {
	terms := Tokenize("铰链 铰链")
	seen := make(map[string]int, len(terms))
	for _, term := range terms {
		seen[term]++
	}
	for term, count := range seen {
		if count > 1 {
			t.Fatalf("词元 %q 重复出现 %d 次", term, count)
		}
	}
}

// 长查询不做整串词元：它不可能作为子串命中，只会多带一个恒不成立的 LIKE 条件。
func TestTokenizeSkipsOverlongPhrase(t *testing.T) {
	long := strings.Repeat("铰链", maxPhraseRunes)
	terms := Tokenize(long)

	if containsTerm(terms, strings.ToLower(long)) {
		t.Fatalf("超过 %d 字的整串不该作为词元", maxPhraseRunes)
	}
	if !containsTerm(terms, "铰链") {
		t.Fatalf("2-gram 仍应存在，实际 %v", terms)
	}
}
