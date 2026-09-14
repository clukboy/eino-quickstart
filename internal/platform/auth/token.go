package auth

// IdentityFor resolves an API key secret to its identity, using the same
// constant-time fingerprint comparison as the net/http Authenticator
// middleware. It is exported so alternative transports (for example the Hertz
// server in internal/transport/hertzapi) can reuse key verification without
// re-implementing hashing or comparison.
func (a *Authenticator) IdentityFor(secret string) (Identity, bool) {
	if a == nil || secret == "" {
		return Identity{}, false
	}
	return a.identityFor(secret)
}
