package audit

import (
	"eino-quickstart/ent"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/types"
)

func fail(err error) error {
	if ent.IsNotFound(err) {
		return httpx.NotFound("audit log not found")
	}
	return httpx.Internal("audit log operation failed")
}
func str(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func dto(x *ent.AuditEvent) *types.AuditLogResp {
	r := &types.AuditLogResp{ID: x.ID, Actor: str(x.ActorSubject), Action: str(x.Action), ResourceType: str(x.ResourceType), ResourceID: str(x.ResourceID), ResourceName: str(x.ResourceName), Summary: str(x.Summary), IP: str(x.IP), CreatedAt: x.CreatedAt.UnixMilli()}
	if len(x.Before) > 0 || len(x.After) > 0 {
		r.Diff = &types.AuditDiffResp{Before: x.Before, After: x.After}
	}
	return r /* TODO: hydrate actor display name from the user directory. */
}
