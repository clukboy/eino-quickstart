package eval

import (
	"eino-quickstart/ent"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/types"
)

func fail(err error) error {
	if ent.IsNotFound(err) {
		return httpx.NotFound("evaluation resource not found")
	}
	if ent.IsConstraintError(err) {
		return httpx.Conflict("evaluation resource already exists")
	}
	return httpx.Internal("evaluation operation failed")
}
func str(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func setDTO(x *ent.EvalSet, count int, last *ent.EvalRun) *types.EvalSetResp {
	agent := ""
	if x.AgentKey != nil {
		agent = *x.AgentKey
	}
	r := &types.EvalSetResp{ID: x.ID, Name: x.Name, Description: x.Description, AgentKey: agent, CaseCount: count, CreatedAt: x.CreatedAt.UnixMilli()}
	if last != nil {
		r.LastRunAt = last.StartedAt.UnixMilli()
		r.LastScore = last.Score
	}
	return r
}
func runDTO(x *ent.EvalRun) *types.EvalRunResp {
	r := &types.EvalRunResp{RunID: x.RunID, Status: string(x.Status), Total: x.Total, Completed: x.Completed, Passed: x.Passed, Score: x.Score, StartedAt: x.StartedAt.UnixMilli(), Error: str(x.Error)}
	if x.FinishedAt != nil {
		r.FinishedAt = x.FinishedAt.UnixMilli()
	}
	return r
}
func resultDTO(x *ent.EvalCaseResult) *types.EvalCaseResultResp {
	return &types.EvalCaseResultResp{CaseID: x.CaseID, Question: x.Question, Answer: x.Answer, Score: x.Score, Passed: x.Passed, Reason: str(x.Reason), LatencyMS: x.LatencyMs}
}
