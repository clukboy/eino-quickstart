package run

import (
	"eino-quickstart/ent"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/types"
)

func fail(err error) error {
	if ent.IsNotFound(err) {
		return httpx.NotFound("run not found")
	}
	return httpx.Internal("run operation failed")
}
func str(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func runDTO(x *ent.AgentRun) *types.RunResp {
	r := &types.RunResp{RunID: x.RunID, SessionID: x.SessionID, AgentKey: x.AgentKey, AgentVersion: x.AgentVersion, Subject: x.RequestedBy, Status: string(x.Status), Steps: x.Steps, ToolCalls: x.ToolCalls, TokensIn: x.TokensIn, TokensOut: x.TokensOut, LatencyMS: x.LatencyMs, CostMicros: x.CostMicros, StartedAt: x.CreatedAt.UnixMilli(), Error: str(x.Error)}
	if x.CompletedAt != nil {
		r.FinishedAt = x.CompletedAt.UnixMilli()
	}
	return r
}
func spanDTO(x *ent.RunSpan) *types.RunSpanResp {
	return &types.RunSpanResp{SpanID: x.SpanID, ParentID: str(x.ParentID), Kind: x.Kind, Name: x.Name, Status: x.Status, LatencyMS: x.LatencyMs, TokensIn: x.TokensIn, TokensOut: x.TokensOut, Input: str(x.Input), Output: str(x.Output), InputTruncated: x.InputTruncated, OutputTruncated: x.OutputTruncated, Error: str(x.Error), StartedAt: x.StartedAt.UnixMilli()}
}
