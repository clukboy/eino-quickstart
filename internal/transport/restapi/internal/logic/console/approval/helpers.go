package approval

import (
	"eino-quickstart/ent"
	"eino-quickstart/ent/approvalpolicy"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/types"
)

func fail(err error) error {
	if ent.IsNotFound(err) {
		return httpx.NotFound("approval policy not found")
	}
	if ent.IsConstraintError(err) {
		return httpx.Conflict("approval policy already exists")
	}
	return httpx.Internal("approval policy operation failed")
}
func dto(x *ent.ApprovalPolicy) *types.ApprovalPolicyResp {
	return &types.ApprovalPolicyResp{ToolKey: x.ToolKey, RiskTier: x.RiskTier, Mode: string(x.Mode), ApproverRoles: x.ApproverRoles, Quorum: x.Quorum, TimeoutSeconds: x.TimeoutSeconds, OnTimeout: x.OnTimeout, Enabled: x.Enabled, UpdatedAt: x.UpdatedAt.UnixMilli()}
}
func parseMode(s string) (approvalpolicy.Mode, error) {
	m := approvalpolicy.Mode(s)
	return m, approvalpolicy.ModeValidator(m)
}
