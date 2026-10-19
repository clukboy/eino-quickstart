package providermodel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"eino-quickstart/ent"
	entmodel "eino-quickstart/ent/model"
	"eino-quickstart/ent/modelprovider"
	"eino-quickstart/ent/modelrouting"
	"eino-quickstart/internal/platform/secretbox"

	"github.com/cloudwego/eino-ext/components/model/openai"
	einomodel "github.com/cloudwego/eino/components/model"
)

// resolveCacheTTL 是「改完 console 要等多久生效」的上界。
//
// 缓存存在的理由是别让每次 Generate 都打一遍数据库；TTL 不设更长是因为
// provider 的连接信息一旦改错，用户希望尽快恢复，而不是重启进程。
const resolveCacheTTL = 60 * time.Second

// defaultRoutingKey 是 ModelRouting 的单例行主键，与 console 写入时用的值一致
// （见 logic/console/model/put_model_routing_logic.go）。
const defaultRoutingKey = "default"

type Resolver struct {
	client *ent.Client
	box    *secretbox.Box
	cache  sync.Map // modelKey -> cachedModel
}

type cachedModel struct {
	chatModel einomodel.BaseChatModel
	builtAt   time.Time
}

// NewResolver 构造解析器。client 为 nil 时所有方法都返回 error（而不是 panic），
// 让组合根可以在没有数据库的场景下安全地退化成"跟随统一配置"。
func NewResolver(client *ent.Client, box *secretbox.Box) *Resolver {
	return &Resolver{client: client, box: box}
}

// DefaultChatModelKey 返回兜底专员应当使用的模型 key。
//
// 空的返回值不是错误：它表示 console 还没配过路由，调用方应当回落到
// config.yaml 的 Model 段。把"没配"和"配错了"分开，是因为前者是正常初始状态，
// 后者是部署问题 —— 后者要 fail-fast，前者不该拦住启动。
func (r *Resolver) DefaultChatModelKey(ctx context.Context) (string, error) {
	if r == nil || r.client == nil {
		return "", nil
	}

	row, err := r.client.ModelRouting.Query().
		Where(modelrouting.ConfigKeyEQ(defaultRoutingKey)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return "", nil
		}
		return "", fmt.Errorf("providermodel: load model routing: %w", err)
	}

	return strings.TrimSpace(row.DefaultChatModel), nil
}

// ChatModel 按 modelKey 解析出一个可用的 chat model，结果带 TTL 缓存。
//
// 任何一步失败都返回 error，由调用方决定是安静回落还是报错 —— 这个包不做降级
// 决策，它只负责"如实告诉你解析不了"。
func (r *Resolver) ChatModel(ctx context.Context, modelKey string) (einomodel.BaseChatModel, error) {
	modelKey = strings.TrimSpace(modelKey)
	if modelKey == "" {
		return nil, errors.New("providermodel: model key is empty")
	}
	if r == nil || r.client == nil {
		return nil, errors.New("providermodel: resolver is not configured")
	}

	if cached, ok := r.cache.Load(modelKey); ok {
		entry, valid := cached.(cachedModel)
		if valid && time.Since(entry.builtAt) < resolveCacheTTL {
			return entry.chatModel, nil
		}
	}

	built, err := r.build(ctx, modelKey)
	if err != nil {
		return nil, err
	}

	r.cache.Store(modelKey, cachedModel{chatModel: built, builtAt: time.Now()})
	return built, nil
}

func (r *Resolver) build(ctx context.Context, modelKey string) (einomodel.BaseChatModel, error) {
	row, err := r.client.Model.Query().
		Where(
			entmodel.KeyEQ(modelKey),
			entmodel.KindEQ(entmodel.KindChat),
			entmodel.EnabledEQ(true),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("providermodel: chat model %q is not configured or disabled", modelKey)
		}
		return nil, fmt.Errorf("providermodel: load model %q: %w", modelKey, err)
	}

	provider, err := r.client.ModelProvider.Query().
		Where(
			modelprovider.KeyEQ(row.ProviderKey),
			modelprovider.EnabledEQ(true),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("providermodel: provider %q is not configured or disabled", row.ProviderKey)
		}
		return nil, fmt.Errorf("providermodel: load provider %q: %w", row.ProviderKey, err)
	}

	apiKey, err := r.apiKey(provider)
	if err != nil {
		return nil, err
	}

	// Model.key 必须是平台自己的模型名（例如 deepseek-chat）：OpenAI 兼容协议里
	// model 字段就是它，这里不做任何映射。
	chatModel, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		APIKey:  apiKey,
		BaseURL: provider.BaseURL,
		Model:   row.Key,
	})
	if err != nil {
		return nil, fmt.Errorf("providermodel: build chat model %q: %w", modelKey, err)
	}

	return chatModel, nil
}

// apiKey 解出 provider 的明文密钥。
//
// oauth2 明确不支持：它需要令牌刷新与持久化，塞进一个请求级的 resolver 里只会
// 写出一个看起来能用、实际会过期的实现。宁可现在报错。
func (r *Resolver) apiKey(provider *ent.ModelProvider) (string, error) {
	switch provider.AuthType {
	case modelprovider.AuthTypeNone:
		return "", nil
	case modelprovider.AuthTypeOauth2:
		return "", fmt.Errorf("providermodel: provider %q uses oauth2, which is not supported", provider.Key)
	}

	if provider.SecretCiphertext == nil || *provider.SecretCiphertext == "" {
		return "", fmt.Errorf("providermodel: provider %q has no stored secret", provider.Key)
	}
	if r.box == nil {
		return "", fmt.Errorf("providermodel: master key is not configured, cannot decrypt provider %q", provider.Key)
	}

	secret, err := r.box.Open(*provider.SecretCiphertext)
	if err != nil {
		return "", fmt.Errorf("providermodel: decrypt provider %q: %w", provider.Key, err)
	}

	return secret, nil
}
