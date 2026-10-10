package org

import (
	"eino-quickstart/ent"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/types"
)

func fail(err error) error {
	if ent.IsNotFound(err) {
		return httpx.NotFound("organization resource not found")
	}
	if ent.IsConstraintError(err) {
		return httpx.Conflict("organization resource already exists")
	}
	return httpx.Internal("organization operation failed")
}
func unitDTO(x *ent.OrgUnit, count int) *types.OrgUnitResp {
	parent := uint64(0)
	if x.ParentID != nil {
		parent = *x.ParentID
	}
	path := ""
	if x.Path != nil {
		path = *x.Path
	}
	return &types.OrgUnitResp{ID: x.ID, Name: x.Name, ParentID: parent, Path: path, MemberCount: count, CreatedAt: x.CreatedAt.UnixMilli()}
}
func memberDTO(x *ent.OrgMember) *types.OrgMemberResp {
	return &types.OrgMemberResp{Subject: x.Subject, JoinedAt: x.JoinedAt.UnixMilli()} /* TODO: hydrate username/nickname/role from the user directory. */
}
