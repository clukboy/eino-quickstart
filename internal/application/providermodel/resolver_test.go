package providermodel

import (
	"context"
	"fmt"
	"testing"

	"eino-quickstart/ent"
	"eino-quickstart/ent/enttest"
	entmodel "eino-quickstart/ent/model"
	"eino-quickstart/ent/modelprovider"
	"eino-quickstart/internal/platform/secretbox"

	"entgo.io/ent/dialect"
	_ "github.com/mattn/go-sqlite3"
)

func newTestClient(t *testing.T) *ent.Client {
	t.Helper()

	// 每个用例一个独立的内存库：共享 cache 的内存库在连接存活期间一直存在，
	// 用同一个名字会让前一个用例写的 provider / routing 泄漏到后一个。
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1", t.Name())
	client := enttest.Open(t, dialect.SQLite, dsn)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func newTestBox(t *testing.T) *secretbox.Box {
	t.Helper()

	// 32 字节全 1 的 base64：测试里不需要随机性，固定的 key 更容易复现失败。
	box, err := secretbox.New("AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=")
	if err != nil {
		t.Fatalf("new box: %v", err)
	}
	return box
}

// seedProvider 建一条 provider + model，并把密钥按生产路径加密后落库。
func seedProvider(t *testing.T, client *ent.Client, box *secretbox.Box, modelKey string) {
	t.Helper()

	ctx := context.Background()

	sealed, err := box.Seal("sk-test")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	provider, err := client.ModelProvider.Create().
		SetKey("test-provider").
		SetName("Test Provider").
		SetBaseURL("https://example.invalid/v1").
		SetAuthType(modelprovider.AuthTypeBearer).
		SetSecretCiphertext(sealed).
		SetEnabled(true).
		Save(ctx)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	if _, err := client.Model.Create().
		SetKey(modelKey).
		SetProviderID(provider.ID).
		SetProviderKey(provider.Key).
		SetKind(entmodel.KindChat).
		SetEnabled(true).
		Save(ctx); err != nil {
		t.Fatalf("create model: %v", err)
	}
}

func TestDefaultChatModelKeyIsEmptyWithoutRouting(t *testing.T) {
	client := newTestClient(t)
	resolver := NewResolver(client, newTestBox(t))

	key, err := resolver.DefaultChatModelKey(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "" {
		t.Fatalf("key = %q, want empty when routing is not configured", key)
	}
}

func TestChatModelResolvesSeededProvider(t *testing.T) {
	client := newTestClient(t)
	box := newTestBox(t)
	seedProvider(t, client, box, "test-chat")

	if _, err := client.ModelRouting.Create().
		SetConfigKey("default").
		SetDefaultChatModel("test-chat").
		SetFallbackChain([]string{}).
		Save(context.Background()); err != nil {
		t.Fatalf("create routing: %v", err)
	}

	resolver := NewResolver(client, box)

	key, err := resolver.DefaultChatModelKey(context.Background())
	if err != nil {
		t.Fatalf("load routing: %v", err)
	}
	if key != "test-chat" {
		t.Fatalf("key = %q, want test-chat", key)
	}

	chatModel, err := resolver.ChatModel(context.Background(), key)
	if err != nil {
		t.Fatalf("resolve chat model: %v", err)
	}
	if chatModel == nil {
		t.Fatal("resolve returned a nil chat model")
	}

	// 第二次走缓存，仍然必须成功（缓存里存的是可用的实例，不是半成品）。
	if _, err := resolver.ChatModel(context.Background(), key); err != nil {
		t.Fatalf("resolve from cache: %v", err)
	}
}

func TestChatModelFailsWhenProviderSecretCannotBeDecrypted(t *testing.T) {
	client := newTestClient(t)
	seedProvider(t, client, newTestBox(t), "test-chat")

	// 没有主密钥：应当明确报错，而不是带着空 APIKey 去请求外部平台。
	resolver := NewResolver(client, nil)
	if _, err := resolver.ChatModel(context.Background(), "test-chat"); err == nil {
		t.Fatal("expected an error when the master key is missing")
	}
}

func TestChatModelFailsForDisabledModel(t *testing.T) {
	client := newTestClient(t)
	box := newTestBox(t)
	seedProvider(t, client, box, "test-chat")

	if err := client.Model.Update().
		Where(entmodel.KeyEQ("test-chat")).
		SetEnabled(false).
		Exec(context.Background()); err != nil {
		t.Fatalf("disable model: %v", err)
	}

	resolver := NewResolver(client, box)
	if _, err := resolver.ChatModel(context.Background(), "test-chat"); err == nil {
		t.Fatal("expected an error for a disabled model")
	}
}

func TestChatModelFailsForUnknownModel(t *testing.T) {
	client := newTestClient(t)
	resolver := NewResolver(client, newTestBox(t))

	if _, err := resolver.ChatModel(context.Background(), "does-not-exist"); err == nil {
		t.Fatal("expected an error for an unknown model")
	}
}
