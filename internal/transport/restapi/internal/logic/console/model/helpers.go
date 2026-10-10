package model

import (
	"eino-quickstart/ent"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/types"
)

func fail(err error) error {
	switch {
	case ent.IsNotFound(err):
		return httpx.NotFound("model resource not found")
	case ent.IsConstraintError(err):
		return httpx.Conflict("model resource already exists")
	default:
		return httpx.Internal("model operation failed")
	}
}
func providerDTO(x *ent.ModelProvider) *types.ModelProviderResp {
	return &types.ModelProviderResp{Key: x.Key, Name: x.Name, BaseURL: x.BaseURL, AuthType: string(x.AuthType), HasSecret: x.SecretCiphertext != nil && *x.SecretCiphertext != "", Enabled: x.Enabled, UpdatedAt: x.UpdatedAt.UnixMilli()}
}
func modelDTO(x *ent.Model) *types.ModelResp {
	return &types.ModelResp{Key: x.Key, ProviderKey: x.ProviderKey, Kind: string(x.Kind), ContextWindow: int64(x.ContextWindow), MaxOutputTokens: int64(x.MaxOutputTokens), InputPriceMicrosPerK: x.InputPriceMicrosPerK, OutputPriceMicrosPerK: x.OutputPriceMicrosPerK, Enabled: x.Enabled, UpdatedAt: x.UpdatedAt.UnixMilli()}
}
func routingDTO(x *ent.ModelRouting) *types.ModelRoutingResp {
	return &types.ModelRoutingResp{DefaultChatModel: x.DefaultChatModel, DefaultEmbeddingModel: x.DefaultEmbeddingModel, FallbackChain: x.FallbackChain, UpdatedAt: x.UpdatedAt.UnixMilli()}
}
