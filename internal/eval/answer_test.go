package eval

import "testing"

func TestEvaluateAnswerRequiresFactsAndAuthorizedCitation(t *testing.T) {
	result := EvaluateAnswer(AnswerCase{
		ID:              "hinge-answer",
		Answer:          "H105P 的开启角度是 105 度。[private/subject-a/hinge.md#chunk-7]",
		RequiredFacts:   []string{"H105P", "105 度"},
		ForbiddenFacts:  []string{"H105G"},
		AllowedSources:  []string{"private/subject-a/hinge.md"},
		RequireCitation: true,
	})
	if !result.Passed {
		t.Fatalf("有效回答应通过: %+v", result)
	}
	if len(result.Citations) != 1 || result.Citations[0].ChunkID != 7 {
		t.Fatalf("引用解析错误: %+v", result.Citations)
	}
}

func TestEvaluateAnswerRejectsMissingOrUnauthorizedEvidence(t *testing.T) {
	missing := EvaluateAnswer(AnswerCase{
		ID:             "missing-citation",
		Answer:         "H105P 的开启角度是 105 度。",
		RequiredFacts:  []string{"105 度"},
		AllowedSources: []string{"private/subject-a/hinge.md"},
	})
	if missing.Passed || !missing.MissingCitation {
		t.Fatalf("无引用回答必须失败: %+v", missing)
	}

	unauthorized := EvaluateAnswer(AnswerCase{
		ID:              "wrong-citation",
		Answer:          "H105P 的开启角度是 105 度。[private/subject-b/hinge.md#chunk-1]",
		RequiredFacts:   []string{"105 度"},
		AllowedSources:  []string{"private/subject-a/hinge.md"},
		RequireCitation: true,
	})
	if unauthorized.Passed || len(unauthorized.UnauthorizedCitations) != 1 {
		t.Fatalf("越权引用必须失败: %+v", unauthorized)
	}
}

func TestEvaluateAnswerRejectsForbiddenFact(t *testing.T) {
	result := EvaluateAnswer(AnswerCase{
		ID:             "conflict",
		Answer:         "目标型号是 H105G，不是 H105P。",
		RequiredFacts:  []string{"H105P"},
		ForbiddenFacts: []string{"H105G"},
	})
	if result.Passed || len(result.ForbiddenFacts) != 1 || len(result.MissingFacts) != 0 {
		t.Fatalf("冲突事实必须被明确拦截: %+v", result)
	}
}

func TestEvaluateAnswerRequiresExplicitAbstention(t *testing.T) {
	pass := EvaluateAnswer(AnswerCase{
		ID:       "no-answer-pass",
		Answer:   "信息不足，无法确定该型号的安装尺寸。",
		NoAnswer: true,
	})
	if !pass.Passed {
		t.Fatalf("明确无答案回答应通过: %+v", pass)
	}

	fail := EvaluateAnswer(AnswerCase{
		ID:             "no-answer-fail",
		Answer:         "该型号安装尺寸为 42 mm。",
		NoAnswer:       true,
		ForbiddenFacts: []string{"42 mm"},
	})
	if fail.Passed || fail.NoAnswerViolation == "" {
		t.Fatalf("无答案用例不得编造肯定回答: %+v", fail)
	}
}
