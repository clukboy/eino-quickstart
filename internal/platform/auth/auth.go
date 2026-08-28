package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
)

type Role string

const (
	RoleAgent    Role = "agent"
	RoleApprover Role = "approver"
	RoleAdmin    Role = "admin"
)

type Identity struct {
	Subject string
	Role    Role
}

type APIKey struct {
	Secret   string
	Identity Identity
}

type storedKey struct {
	fingerprint [sha256.Size]byte
	identity    Identity
}

type Authenticator struct {
	keys []storedKey
}

func New(keys []APIKey) (*Authenticator, error) {
	if len(keys) == 0 {
		return nil, errors.New("at least one API key is required")
	}

	stored := make([]storedKey, 0, len(keys))

	for _, key := range keys {
		if key.Secret == "" {
			return nil, errors.New("API key secret is empty")
		}
		if key.Identity.Subject == "" {
			return nil, errors.New("API key subject is empty")
		}
		if key.Identity.Role == "" {
			return nil, errors.New("API key role is empty")
		}

		stored = append(stored, storedKey{
			fingerprint: sha256.Sum256([]byte(key.Secret)),
			identity:    key.Identity,
		})
	}

	return &Authenticator{keys: stored}, nil
}

type identityContextKey struct{}

func IdentityFromContext(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(identityContextKey{}).(Identity)
	return identity, ok
}

func (a *Authenticator) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if header == "" {
			next.ServeHTTP(w, r)
			return
		}

		secret, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || strings.TrimSpace(secret) == "" {
			writeUnauthorized(w)
			return
		}

		identity, ok := a.identityFor(secret)
		if !ok {
			writeUnauthorized(w)
			return
		}

		ctx := context.WithValue(
			r.Context(),
			identityContextKey{},
			identity,
		)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *Authenticator) identityFor(secret string) (Identity, bool) {
	fingerprint := sha256.Sum256([]byte(secret))

	for _, key := range a.keys {
		if subtle.ConstantTimeCompare(
			fingerprint[:],
			key.fingerprint[:],
		) == 1 {
			return key.identity, true
		}
	}

	return Identity{}, false
}

func Require(roles ...Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identity, ok := IdentityFromContext(r.Context())
			if !ok {
				writeUnauthorized(w)
				return
			}

			for _, role := range roles {
				if identity.Role == RoleAdmin || identity.Role == role {
					next.ServeHTTP(w, r)
					return
				}
			}

			writeForbidden(w)
		})
	}
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
}

func writeForbidden(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"error":"forbidden"}`))
}
