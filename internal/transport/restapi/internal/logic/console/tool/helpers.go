package tool

import (
	"eino-quickstart/ent"
	enttool "eino-quickstart/ent/tool"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/types"
)

func fail(err error) error {
	switch {
	case ent.IsNotFound(err):
		return httpx.NotFound("tool not found")
	case ent.IsConstraintError(err):
		return httpx.Conflict("tool already exists")
	default:
		return httpx.Internal("tool operation failed")
	}
}

func dto(row *ent.Tool) *types.ToolResp {
	return &types.ToolResp{Key: row.Key, Name: row.Name, Kind: string(row.Kind), Description: row.Description, RiskTier: row.RiskTier, Enabled: row.Enabled, ConfigSchema: value(row.ConfigSchema), AuthType: string(row.AuthType), HasSecret: row.SecretCiphertext != nil && *row.SecretCiphertext != "", TimeoutMS: int64(row.TimeoutMs), UpdatedAt: row.UpdatedAt.UnixMilli()}
}

func value(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func parseKind(s string) (enttool.Kind, error) {
	k := enttool.Kind(s)
	if err := enttool.KindValidator(k); err != nil {
		return "", err
	}
	return k, nil
}
func parseAuth(s string) (enttool.AuthType, error) {
	if s == "" {
		s = "none"
	}
	a := enttool.AuthType(s)
	if err := enttool.AuthTypeValidator(a); err != nil {
		return "", err
	}
	return a, nil
}
