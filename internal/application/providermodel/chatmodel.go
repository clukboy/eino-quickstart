package providermodel

import (
	"context"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// Dynamic 是一个把「用哪家平台」推迟到每次调用的 ChatModel。
//
// 为什么需要它：adk.NewChatModelAgent 在构造时就把 Model 冻住了，之后改数据库里
// 的 provider 只能靠重启生效 —— 那等于把配置搬进数据库却没有得到数据库的好处。
// Dynamic 实现的是一个很薄的代理，agent 冻住的是代理，真实 client 每次现解析
// （带 TTL 缓存），所以 console 改完最多等一个 TTL 就生效。
//
// 它**只实现 Generate 与 Stream**，也就是 model.BaseChatModel，不实现
// ToolCallingChatModel —— 这是刻意的：兜底专员没有工具，给它 BindTools 能力只会
// 让"兜底不能有工具"这条硬约束变得可以绕过。
type Dynamic struct {
	resolver *Resolver
	key      string
}

func NewDynamic(resolver *Resolver, modelKey string) *Dynamic {
	return &Dynamic{resolver: resolver, key: modelKey}
}

func (d *Dynamic) Generate(
	ctx context.Context,
	input []*schema.Message,
	opts ...einomodel.Option,
) (*schema.Message, error) {
	chatModel, err := d.resolver.ChatModel(ctx, d.key)
	if err != nil {
		return nil, err
	}

	return chatModel.Generate(ctx, input, opts...)
}

func (d *Dynamic) Stream(
	ctx context.Context,
	input []*schema.Message,
	opts ...einomodel.Option,
) (*schema.StreamReader[*schema.Message], error) {
	chatModel, err := d.resolver.ChatModel(ctx, d.key)
	if err != nil {
		return nil, err
	}

	return chatModel.Stream(ctx, input, opts...)
}

// 编译期断言：Dynamic 必须满足 adk 需要的模型接口。
var _ einomodel.BaseChatModel = (*Dynamic)(nil)
