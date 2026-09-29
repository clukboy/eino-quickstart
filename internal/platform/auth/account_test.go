package auth

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

const testAccountSecret = "test-account-secret"

func newTestAccountIssuer(t *testing.T) *AccountIssuer {
	t.Helper()

	issuer, err := NewAccountIssuer(AccountConfig{
		Secret: testAccountSecret,
		TTL:    time.Hour,
	})
	if err != nil {
		t.Fatalf("new issuer: %v", err)
	}

	return issuer
}

func TestAccountIssueForAndVerifyRoundTrip(t *testing.T) {
	issuer := newTestAccountIssuer(t)

	subject := NewAccountSubject(42)
	token, expiresAt, err := issuer.IssueFor(subject, RoleApprover, true)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if !strings.HasPrefix(token, accountTokenPrefix) {
		t.Fatalf("token %q lost the %q prefix", token, accountTokenPrefix)
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
	if identity.Role != RoleApprover {
		t.Fatalf("role round-trip: got %q, want %q", identity.Role, RoleApprover)
	}
	// mcp 必须跟着令牌走：它是「还没改过初始密码」的唯一载体，丢了就等于
	// 放行一个管理员设定的、管理员本人知道的密码。
	if !identity.MustChangePassword {
		t.Fatal("must_change_password was lost in the round-trip")
	}
}

func TestAccountIssueForCarriesPasswordChangedState(t *testing.T) {
	issuer := newTestAccountIssuer(t)

	token, _, err := issuer.IssueFor(NewAccountSubject(7), RoleAgent, false)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	identity, ok := issuer.Verify(token)
	if !ok {
		t.Fatal("verify failed")
	}
	if identity.MustChangePassword {
		t.Fatal("must_change_password came back true for a token issued with false")
	}
}

// subject 是账号身份的全部（归属、审计、越权边界都挂在它上面），所以签发时
// 不允许为空、也不允许拿别人的前缀 —— 后者等于给任意账号发一枚合法令牌。
func TestAccountIssueForRejectsBadSubject(t *testing.T) {
	issuer := newTestAccountIssuer(t)

	cases := map[string]string{
		"empty":            "",
		"anonymous prefix": AnonymousSubjectPrefix + "00000000-0000-0000-0000-000000000000",
		"static key shape": "platform-admin",
		"prefix only":      AccountSubjectPrefix,
	}

	for name, subject := range cases {
		if _, _, err := issuer.IssueFor(subject, RoleAgent, false); err == nil {
			t.Errorf("%s: issued a token for subject %q", name, subject)
		}
	}
}

// 未知角色在**签发时**就报错，而不是签一枚谁也验不过的令牌出去：后者会表现为
// 「登录成功但每个请求都 401」，排查成本远高于启动即失败。
func TestAccountIssueForRejectsUnknownRole(t *testing.T) {
	issuer := newTestAccountIssuer(t)

	if _, _, err := issuer.IssueFor(NewAccountSubject(1), Role("superuser"), false); err == nil {
		t.Fatal("issued a token with an unknown role")
	}
}

func TestAccountVerifyRejectsExpiredToken(t *testing.T) {
	issuer := newTestAccountIssuer(t)

	token, expiresAt, err := issuer.IssueFor(NewAccountSubject(1), RoleAgent, false)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	// 过期判定用的是 >=：把时钟挪到「截止那一刻」，令牌即失效。
	issuer.now = func() time.Time { return expiresAt }
	if _, ok := issuer.Verify(token); ok {
		t.Fatal("token still verified at its own expiry instant")
	}
}

func TestAccountVerifyRejectsTamperedPayload(t *testing.T) {
	issuer := newTestAccountIssuer(t)

	token, _, err := issuer.IssueFor(NewAccountSubject(1), RoleAgent, false)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	// 把角色改成 admin、把 mcp 设成 false —— 签名不动，必须验签失败。
	forged := base64.RawURLEncoding.EncodeToString(
		[]byte(`{"s":"user:1","exp":4102444800,"r":"admin"}`),
	)
	_, signature, _ := strings.Cut(strings.TrimPrefix(token, accountTokenPrefix), ".")

	if _, ok := issuer.Verify(accountTokenPrefix + forged + "." + signature); ok {
		t.Fatal("verified a token whose payload was rewritten to escalate the role")
	}
}

func TestAccountVerifyRejectsOtherSecret(t *testing.T) {
	issuer := newTestAccountIssuer(t)
	token, _, err := issuer.IssueFor(NewAccountSubject(1), RoleAgent, false)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	other, err := NewAccountIssuer(AccountConfig{Secret: "another-secret", TTL: time.Hour})
	if err != nil {
		t.Fatalf("new issuer: %v", err)
	}

	if _, ok := other.Verify(token); ok {
		t.Fatal("a different secret verified the token")
	}
}

func TestAccountVerifyRejectsMalformedTokens(t *testing.T) {
	issuer := newTestAccountIssuer(t)

	cases := map[string]string{
		"empty":            "",
		"static key shape": "some-static-api-key",
		"prefix only":      accountTokenPrefix,
		"no signature":     accountTokenPrefix + "cGF5bG9hZA",
		"empty payload":    accountTokenPrefix + ".c2ln",
		"empty signature":  accountTokenPrefix + "cGF5bG9hZA.",
		"not base64":       accountTokenPrefix + "!!!.???",
		"not json": accountTokenPrefix +
			base64.RawURLEncoding.EncodeToString([]byte("not-json")) + ".c2ln",
	}

	for name, token := range cases {
		if _, ok := issuer.Verify(token); ok {
			t.Errorf("%s: token %q verified, want rejection", name, token)
		}
	}
}

func TestAccountIssuerRejectsBadConfig(t *testing.T) {
	cases := map[string]AccountConfig{
		"empty secret": {TTL: time.Hour},
		"zero ttl":     {Secret: "s"},
	}

	for name, cfg := range cases {
		if _, err := NewAccountIssuer(cfg); err == nil {
			t.Errorf("%s: constructor accepted the config", name)
		}
	}
}

// 两个 issuer 的密钥与令牌前缀都不同，所以任何一方的令牌都不能被另一方接受。
// 这条守的是「令牌类型串用」：拿匿名令牌去当账号令牌用，会得到一个
// role=agent 的身份，而 if 那套判断如果写错（比如忘了加前缀分流）就会静默成立。
func TestIssuersDoNotAcceptEachOtherTokens(t *testing.T) {
	anonymous := newTestIssuer(t)
	accounts := newTestAccountIssuer(t)

	anonymousToken, _, _, err := anonymous.Issue("")
	if err != nil {
		t.Fatalf("issue anonymous: %v", err)
	}
	accountToken, _, err := accounts.IssueFor(NewAccountSubject(1), RoleAdmin, false)
	if err != nil {
		t.Fatalf("issue account: %v", err)
	}

	if _, ok := accounts.Verify(anonymousToken); ok {
		t.Fatal("the account issuer accepted an anonymous token")
	}
	if _, ok := anonymous.Verify(accountToken); ok {
		t.Fatal("the anonymous issuer accepted an account token")
	}
}

func TestAccountSubjectRoundTrip(t *testing.T) {
	if got, want := NewAccountSubject(42), "user:42"; got != want {
		t.Fatalf("NewAccountSubject(42) = %q, want %q", got, want)
	}

	id, ok := ParseAccountSubject("user:42")
	if !ok || id != 42 {
		t.Fatalf("ParseAccountSubject(\"user:42\") = (%d, %v), want (42, true)", id, ok)
	}

	// 非账号身份必须被认出来，调用方靠这个区分「登录用户」与「访客」。
	for _, subject := range []string{
		"",
		"anon:00000000-0000-0000-0000-000000000000",
		"platform-admin",
		"user:",
		"user:not-a-number",
		"user:-1",
		"user:42:extra",
	} {
		if _, ok := ParseAccountSubject(subject); ok {
			t.Errorf("ParseAccountSubject(%q) reported a valid account subject", subject)
		}
	}
}

func TestAuthenticatorAcceptsAccountToken(t *testing.T) {
	issuer := newTestAccountIssuer(t)

	authenticator, err := New(nil, WithAccounts(issuer))
	if err != nil {
		t.Fatalf("new authenticator: %v", err)
	}

	subject := NewAccountSubject(9)
	token, _, err := issuer.IssueFor(subject, RoleApprover, false)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	identity, ok := authenticator.IdentityFor(token)
	if !ok {
		t.Fatal("authenticator rejected an account token")
	}
	if identity.Subject != subject || identity.Role != RoleApprover {
		t.Fatalf("account identity changed: %+v", identity)
	}
}

// 静态 key 与账号令牌共存时，静态 key 必须仍解析成它自己配置的 subject ——
// 若某个签发分支抢在前面，整个管理台的归属会突然变成 user:*。
func TestAuthenticatorKeepsStaticKeysAheadOfAccounts(t *testing.T) {
	issuer := newTestAccountIssuer(t)

	authenticator, err := New(
		[]APIKey{{
			Secret:   "static-admin-key",
			Identity: Identity{Subject: "platform-admin", Role: RoleAdmin},
		}},
		WithAccounts(issuer),
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
	// 静态 key 没有密码可改，这个标记必须是零值 —— 否则每个管理台请求都会被
	// 密码守卫拦成 403。
	if identity.MustChangePassword {
		t.Fatal("a static API key came back flagged as must_change_password")
	}
}

// 三种凭证同时开着时，各自必须解析成自己的 subject —— 这是「一个 Bearer 通道、
// 多种判定方式」这套设计成立的唯一证明。
func TestAuthenticatorResolvesAllCredentialSources(t *testing.T) {
	anonymous := newTestIssuer(t)
	accounts := newTestAccountIssuer(t)

	authenticator, err := New(
		[]APIKey{{
			Secret:   "static-admin-key",
			Identity: Identity{Subject: "platform-admin", Role: RoleAdmin},
		}},
		WithAnonymous(anonymous),
		WithAccounts(accounts),
	)
	if err != nil {
		t.Fatalf("new authenticator: %v", err)
	}

	anonymousToken, anonymousSubject, _, err := anonymous.Issue("")
	if err != nil {
		t.Fatalf("issue anonymous: %v", err)
	}
	accountSubject := NewAccountSubject(11)
	accountToken, _, err := accounts.IssueFor(accountSubject, RoleAgent, true)
	if err != nil {
		t.Fatalf("issue account: %v", err)
	}

	cases := []struct {
		name    string
		secret  string
		subject string
		role    Role
		mcp     bool
	}{
		{"static key", "static-admin-key", "platform-admin", RoleAdmin, false},
		{"anonymous token", anonymousToken, anonymousSubject, RoleAgent, false},
		{"account token", accountToken, accountSubject, RoleAgent, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			identity, ok := authenticator.IdentityFor(tc.secret)
			if !ok {
				t.Fatalf("%s did not resolve", tc.name)
			}
			if identity.Subject != tc.subject {
				t.Errorf("subject: got %q, want %q", identity.Subject, tc.subject)
			}
			if identity.Role != tc.role {
				t.Errorf("role: got %q, want %q", identity.Role, tc.role)
			}
			if identity.MustChangePassword != tc.mcp {
				t.Errorf("must_change_password: got %v, want %v", identity.MustChangePassword, tc.mcp)
			}
		})
	}
}

// 账号功能关掉（组合根不构造 issuer）时，账号令牌必须完全不认 —— 不能因为
// 「另一个 issuer 恰好验过了」而放行。
func TestAuthenticatorWithoutAccountsRejectsAccountTokens(t *testing.T) {
	anonymous := newTestIssuer(t)
	accounts := newTestAccountIssuer(t)

	authenticator, err := New(nil, WithAnonymous(anonymous))
	if err != nil {
		t.Fatalf("new authenticator: %v", err)
	}

	token, _, err := accounts.IssueFor(NewAccountSubject(3), RoleAgent, false)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	if _, ok := authenticator.IdentityFor(token); ok {
		t.Fatal("account token resolved even though the account issuer is not wired")
	}
}

func TestNewAcceptsAccountsAsSoleCredentialSource(t *testing.T) {
	if _, err := New(nil, WithAccounts(newTestAccountIssuer(t))); err != nil {
		t.Fatalf("account-only deployment was rejected: %v", err)
	}
}
