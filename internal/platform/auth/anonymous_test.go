package auth

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

const testAnonymousSecret = "test-anonymous-secret"

func newTestIssuer(t *testing.T) *AnonymousIssuer {
	t.Helper()

	issuer, err := NewAnonymousIssuer(AnonymousConfig{
		Secret: testAnonymousSecret,
		TTL:    time.Hour,
		Role:   RoleAgent,
	})
	if err != nil {
		t.Fatalf("new issuer: %v", err)
	}

	return issuer
}

func TestAnonymousIssueAndVerifyRoundTrip(t *testing.T) {
	issuer := newTestIssuer(t)

	token, subject, expiresAt, err := issuer.Issue("")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if !strings.HasPrefix(subject, AnonymousSubjectPrefix) {
		t.Fatalf("subject %q lost the %q prefix", subject, AnonymousSubjectPrefix)
	}
	if !strings.HasPrefix(token, anonymousTokenPrefix) {
		t.Fatalf("token %q lost the %q prefix", token, anonymousTokenPrefix)
	}
	if !expiresAt.After(time.Now()) {
		t.Fatalf("expiresAt %v is not in the future", expiresAt)
	}

	identity, ok := issuer.Verify(token)
	if !ok {
		t.Fatal("verify rejected a token it just issued")
	}
	if identity.Subject != subject {
		t.Fatalf("subject round-trip: got %q, want %q", identity.Subject, subject)
	}
	if identity.Role != RoleAgent {
		t.Fatalf("role: got %q, want %q", identity.Role, RoleAgent)
	}
}

// 续期是匿名身份能「一直用下去」的关键：刷新页面/重启浏览器后拿旧令牌换新的，
// subject 必须保持不变，否则历史会话留在一个再也拿不到的 subject 名下。
func TestAnonymousIssueReusesSubjectOnRenewal(t *testing.T) {
	issuer := newTestIssuer(t)

	first, subject, _, err := issuer.Issue("")
	if err != nil {
		t.Fatalf("first issue: %v", err)
	}

	identity, ok := issuer.Verify(first)
	if !ok {
		t.Fatal("first token did not verify")
	}

	renewed, renewedSubject, _, err := issuer.Issue(identity.Subject)
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if renewedSubject != subject {
		t.Fatalf("renewal changed the subject: %q -> %q", subject, renewedSubject)
	}

	// 不断言「新令牌与旧令牌一定不同」：载荷里的 exp 精度到秒，同一秒内续期
	// 必然得到逐字节相同的令牌。那是幂等（同一 subject、同一有效期），不是
	// 缺陷 —— 所以这里断言的是**可继续使用**，而不是字节差异。
	renewedIdentity, ok := issuer.Verify(renewed)
	if !ok {
		t.Fatal("renewed token did not verify")
	}
	if renewedIdentity.Subject != subject {
		t.Fatalf("renewed token carries %q, want %q", renewedIdentity.Subject, subject)
	}
}

// 只允许续自己这一类的 subject：否则「拿任意 subject 换一个合法令牌」就是
// 越权，匿名接口会变成给 admin 签发身份的后门。
func TestAnonymousIssueRejectsForeignSubject(t *testing.T) {
	issuer := newTestIssuer(t)

	if _, _, _, err := issuer.Issue("platform-admin"); err == nil {
		t.Fatal("issued a token for a subject without the anonymous prefix")
	}
}

func TestAnonymousVerifyRejectsExpiredToken(t *testing.T) {
	issuer := newTestIssuer(t)

	token, _, expiresAt, err := issuer.Issue("")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	// 过期判定用的是 >=：把时钟挪到「截止那一刻」，令牌即失效。
	issuer.now = func() time.Time { return expiresAt }
	if _, ok := issuer.Verify(token); ok {
		t.Fatal("token still verified at its own expiry instant")
	}
}

func TestAnonymousVerifyRejectsTamperedPayload(t *testing.T) {
	issuer := newTestIssuer(t)

	token, _, _, err := issuer.Issue("")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	// 把载荷换成另一个 subject，签名不动 —— 必须验签失败。
	forged := base64.RawURLEncoding.EncodeToString(
		[]byte(`{"s":"anon:00000000-0000-0000-0000-000000000000","exp":4102444800}`),
	)
	_, signature, _ := strings.Cut(strings.TrimPrefix(token, anonymousTokenPrefix), ".")

	forgedToken := anonymousTokenPrefix + forged + "." + signature
	if _, ok := issuer.Verify(forgedToken); ok {
		t.Fatal("verified a token whose payload was rewritten")
	}
}

func TestAnonymousVerifyRejectsOtherSecret(t *testing.T) {
	issuer := newTestIssuer(t)
	token, _, _, err := issuer.Issue("")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	other, err := NewAnonymousIssuer(AnonymousConfig{
		Secret: "another-secret",
		TTL:    time.Hour,
		Role:   RoleAgent,
	})
	if err != nil {
		t.Fatalf("new issuer: %v", err)
	}

	if _, ok := other.Verify(token); ok {
		t.Fatal("a different secret verified the token")
	}
}

func TestAnonymousVerifyRejectsMalformedTokens(t *testing.T) {
	issuer := newTestIssuer(t)

	cases := map[string]string{
		"empty":            "",
		"static key shape": "some-static-api-key",
		"prefix only":      anonymousTokenPrefix,
		"no signature":     anonymousTokenPrefix + "cGF5bG9hZA",
		"empty payload":    anonymousTokenPrefix + ".c2ln",
		"empty signature":  anonymousTokenPrefix + "cGF5bG9hZA.",
		"not base64":       anonymousTokenPrefix + "!!!.???",
		"not json": anonymousTokenPrefix +
			base64.RawURLEncoding.EncodeToString([]byte("not-json")) + ".c2ln",
	}

	for name, token := range cases {
		if _, ok := issuer.Verify(token); ok {
			t.Errorf("%s: token %q verified, want rejection", name, token)
		}
	}
}

func TestAnonymousIssuerRejectsBadConfig(t *testing.T) {
	cases := map[string]AnonymousConfig{
		"empty secret": {TTL: time.Hour},
		"zero ttl":     {Secret: "s"},
		"unknown role": {Secret: "s", TTL: time.Hour, Role: "superuser"},
	}

	for name, cfg := range cases {
		if _, err := NewAnonymousIssuer(cfg); err == nil {
			t.Errorf("%s: constructor accepted the config", name)
		}
	}
}

// 匿名身份只能拿到配置里的角色。默认是 agent —— 这是刻意的：匿名用户要能用
// 对话，但绝不该碰到 admin 的 dataset 组。
func TestAnonymousDefaultRoleIsAgent(t *testing.T) {
	issuer, err := NewAnonymousIssuer(AnonymousConfig{
		Secret: "s",
		TTL:    time.Hour,
	})
	if err != nil {
		t.Fatalf("new issuer: %v", err)
	}

	token, _, _, err := issuer.Issue("")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	identity, ok := issuer.Verify(token)
	if !ok {
		t.Fatal("verify failed")
	}
	if identity.Role != RoleAgent {
		t.Fatalf("default role: got %q, want %q", identity.Role, RoleAgent)
	}
}

func TestAuthenticatorAcceptsAnonymousToken(t *testing.T) {
	issuer := newTestIssuer(t)

	authenticator, err := New(nil, WithAnonymous(issuer))
	if err != nil {
		t.Fatalf("new authenticator: %v", err)
	}

	token, subject, _, err := issuer.Issue("")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	identity, ok := authenticator.IdentityFor(token)
	if !ok {
		t.Fatal("authenticator rejected an anonymous token")
	}
	if identity.Subject != subject {
		t.Fatalf("subject: got %q, want %q", identity.Subject, subject)
	}
}

// 静态 key 与匿名令牌共存时，静态 key 必须仍解析成它自己配置的 subject ——
// 若匿名分支抢在前面，整个管理台的归属会突然变成 anon:*。
func TestAuthenticatorKeepsStaticKeysAheadOfAnonymous(t *testing.T) {
	issuer := newTestIssuer(t)

	authenticator, err := New(
		[]APIKey{{
			Secret:   "static-admin-key",
			Identity: Identity{Subject: "platform-admin", Role: RoleAdmin},
		}},
		WithAnonymous(issuer),
	)
	if err != nil {
		t.Fatalf("new authenticator: %v", err)
	}

	identity, ok := authenticator.IdentityFor("static-admin-key")
	if !ok {
		t.Fatal("static key stopped resolving")
	}
	if identity.Subject != "platform-admin" || identity.Role != RoleAdmin {
		t.Fatalf("static key identity changed: %+v", identity)
	}

	if _, ok := authenticator.IdentityFor("anon.not-a-real-token"); ok {
		t.Fatal("a malformed anonymous token resolved to an identity")
	}
}

func TestNewRequiresSomeCredentialSource(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatal("authenticator built with no key and no anonymous issuer")
	}
}
