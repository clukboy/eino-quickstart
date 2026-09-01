package retrieval

import "testing"

func TestParseQueryModel(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		want     string
		hasModel bool
	}{
		{
			name:     "exact model",
			query:    "H11",
			want:     "H11",
			hasModel: true,
		},
		{
			name:     "model with question",
			query:    "H11安装方式是什么",
			want:     "H11",
			hasModel: true,
		},
		{
			name:     "model with suffix",
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
