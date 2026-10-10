package agent

import (
	"eino-quickstart/ent"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/types"
)

func fail(err error) error {
	switch {
	case ent.IsNotFound(err):
		return httpx.NotFound("agent resource not found")
	case ent.IsConstraintError(err):
		return httpx.Conflict("agent resource already exists")
	default:
		return httpx.Internal("agent operation failed")
	}
}

func AgentDefDTO(base *ent.AgentDef) *types.AgentDefResp {
	return &types.AgentDefResp{
		ID: base.ID, Name: base.Name, Description: base.Description,
		Owner: base.OwnerSubject, Status: string(base.Status),
		CurrentVersion: base.CurrentVersion, PublishedVersion: base.PublishedVersion,
		Model: base.Model, RiskTier: base.RiskTier,
		CreatedAt: base.CreatedAt.UnixMilli(), UpdatedAt: base.UpdatedAt.UnixMilli(),
	}
}

func stringValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func AgentVersionDTO(base *ent.AgentVersion) *types.AgentVersionResp {
	return &types.AgentVersionResp{
		Version: base.Version, SystemPrompt: base.SystemPrompt, Model: base.Model,
		Temperature: base.Temperature, MaxIterations: base.MaxIterations,
		ToolKeys: base.ToolKeys, DatasetIDs: base.DatasetIds,
		Status: string(base.Status), ChangeNote: stringValue(base.ChangeNote),
		CreatedBy: base.CreatedBy, CreatedAt: base.CreatedAt.UnixMilli(),
	}
}
