package retrieval

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseQueryModel(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		want     string
		hasModel bool
	}{
		{
			name:     "H model",
			query:    "H11",
			want:     "H11",
			hasModel: true,
		},
		{
			name:     "H model with question",
			query:    "H11安装方式是什么",
			want:     "H11",
			hasModel: true,
		},
		{
			name:     "H model suffix",
			query:    "H105G适合什么门",
			want:     "H105G",
			hasModel: true,
		},
		{
			name:     "lower case model",
			query:    "h17s",
			want:     "H17S",
			hasModel: true,
		},
		{
			name:     "T model",
			query:    "T206怎么安装",
			want:     "T206",
			hasModel: true,
		},
		{
			name:     "WF model",
			query:    "WF10参数是什么",
			want:     "WF10",
			hasModel: true,
		},
		{
			name:     "model with punctuation",
			query:    "H42：安装角度是多少？",
			want:     "H42",
			hasModel: true,
		},
		{
			name:     "no model",
			query:    "快装二段力铰链",
			want:     "",
			hasModel: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseQuery(tt.query)

			if got.Model != tt.want {
				t.Fatalf(
					"model=%q, want=%q",
					got.Model,
					tt.want,
				)
			}

			if got.HasModel != tt.hasModel {
				t.Fatalf(
					"hasModel=%v, want=%v",
					got.HasModel,
					tt.hasModel,
				)
			}
		})
	}
}

func TestParseQueryTopicsAndIntent(t *testing.T) {
	got := ParseQuery(
		"H105安装后关门有异响怎么办",
	)

	if got.Model != "H105" {
		t.Fatalf(
			"model=%q, want H105",
			got.Model,
		)
	}

	if got.Intent != "problem_solving" {
		t.Fatalf(
			"intent=%q, want problem_solving",
			got.Intent,
		)
	}

	if !containsString(
		got.Topics,
		"installation",
	) {
		t.Fatalf(
			"topics=%v missing installation",
			got.Topics,
		)
	}

	if !containsString(
		got.Topics,
		"troubleshooting",
	) {
		t.Fatalf(
			"topics=%v missing troubleshooting",
			got.Topics,
		)
	}
}

func TestBuildKeywordQuery(t *testing.T) {
	info := ParseQuery(
		"H105安装后关门有异响怎么办",
	)

	got := BuildKeywordQuery(info)

	for _, expected := range []string{
		"H105",
		"installation",
		"troubleshooting",
		"problem_solving",
	} {
		if !containsString(
			splitKeywords(got),
			expected,
		) {
			t.Errorf(
				"keyword query %q missing %q",
				got,
				expected,
			)
		}
	}
}

func TestSearchScopeNormalized(t *testing.T) {
	scope := SearchScope{
		ActorSubject: "actor",
		KnowledgeBaseIDs: []uint64{
			1,
			2,
			2,
			0,
			-1,
			1,
		},
	}

	got := scope.Normalized()

	want := SearchScope{
		ActorSubject: "actor",
		KnowledgeBaseIDs: []uint64{
			1,
			2,
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf(
			"scope=%#v, want %#v",
			got,
			want,
		)
	}
}

func containsString(
	values []string,
	target string,
) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}

	return false
}

func splitKeywords(value string) []string {
	var result []string

	for _, item := range []rune(value) {
		_ = item
	}

	for _, item := range []string{} {
		result = append(result, item)
	}

	// 保持测试简单。
	// 实际 BuildKeywordQuery 使用空格分隔。
	for _, item := range strings.Fields(value) {
		result = append(result, item)
	}

	return result
}
