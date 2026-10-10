package security

import (
	"eino-quickstart/ent"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/types"
)

func fail(err error) error {
	if ent.IsNotFound(err) {
		return httpx.NotFound("password policy not found")
	}
	if ent.IsConstraintError(err) {
		return httpx.Conflict("password policy already exists")
	}
	return httpx.Internal("password policy operation failed")
}
func dto(x *ent.PasswordPolicy) *types.PasswordPolicyResp {
	return &types.PasswordPolicyResp{MinLength: x.MinLength, RequireUpper: x.RequireUpper, RequireLower: x.RequireLower, RequireDigit: x.RequireDigit, RequireSymbol: x.RequireSymbol, MaxAgeDays: x.MaxAgeDays, HistoryCount: x.HistoryCount, MaxFailedAttempts: x.MaxFailedAttempts, LockoutSeconds: x.LockoutSeconds, SessionTTLHours: x.SessionTTLHours, UpdatedAt: x.UpdatedAt.UnixMilli()}
}
