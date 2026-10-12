// Package auth parses and verifies tenant API keys (docs/14-DECISIONS/0005-authentication-model.md)
// and the worker token.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Scopes granted to API keys.
const (
	ScopeJobsRead   = "jobs:read"
	ScopeJobsWrite  = "jobs:write"
	ScopeJobsCancel = "jobs:cancel"
)

// AllScopes lists every valid scope.
var AllScopes = []string{ScopeJobsRead, ScopeJobsWrite, ScopeJobsCancel}

// ErrUnauthenticated covers missing, malformed, unknown, revoked, and expired keys alike.
var ErrUnauthenticated = errors.New("unauthenticated")

const (
	keyPrefix   = "tp_"
	prefixLen   = 12
	secretLen   = 43 // base64url of 32 bytes, no padding
	secretBytes = 32
	prefixChars = "abcdefghijklmnopqrstuvwxyz0123456789"
)

// Principal is the authenticated caller. TenantID is the only tenant any query may use.
type Principal struct {
	TenantID   uuid.UUID `json:"tenant_id"`
	TenantName string    `json:"tenant_name"`
	KeyID      uuid.UUID `json:"key_id"`
	KeyName    string    `json:"key_name"`
	Scopes     []string  `json:"scopes"`
}

// NewKey generates a key. The full key is shown once; only prefix and SHA-256(secret) are stored.
func NewKey() (full, prefix string, hash []byte) {
	prefix = strings.ToLower(rand.Text()[:prefixLen]) // base32 → [a-z2-7]
	b := make([]byte, secretBytes)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error
	secret := base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(secret))
	return keyPrefix + prefix + "_" + secret, prefix, sum[:]
}

// ParseKey splits tp_<prefix>_<secret>. The secret may itself contain '_'.
func ParseKey(key string) (prefix, secret string, err error) {
	rest, ok := strings.CutPrefix(key, keyPrefix)
	if !ok {
		return "", "", ErrUnauthenticated
	}
	prefix, secret, ok = strings.Cut(rest, "_")
	if !ok || len(prefix) != prefixLen || len(secret) != secretLen ||
		strings.Trim(prefix, prefixChars) != "" {
		return "", "", ErrUnauthenticated
	}
	return prefix, secret, nil
}

// Authenticate resolves a bearer API key to a Principal.
func Authenticate(ctx context.Context, pool *pgxpool.Pool, key string) (Principal, error) {
	prefix, secret, err := ParseKey(key)
	if err != nil {
		return Principal{}, err
	}
	var p Principal
	var stored []byte
	err = pool.QueryRow(ctx, `
		SELECT k.id, k.tenant_id, t.name, k.name, k.scopes, k.secret_hash
		FROM api_keys k JOIN tenants t ON t.id = k.tenant_id
		WHERE k.prefix = $1 AND k.revoked_at IS NULL
		  AND (k.expires_at IS NULL OR k.expires_at > now())
		  AND t.disabled_at IS NULL`, prefix,
	).Scan(&p.KeyID, &p.TenantID, &p.TenantName, &p.KeyName, &p.Scopes, &stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, ErrUnauthenticated
	}
	if err != nil {
		return Principal{}, fmt.Errorf("look up api key: %w", err)
	}
	sum := sha256.Sum256([]byte(secret))
	if subtle.ConstantTimeCompare(sum[:], stored) != 1 {
		return Principal{}, ErrUnauthenticated
	}
	return p, nil
}

// WorkerTokenValid compares a presented worker token with every accepted token in constant time.
func WorkerTokenValid(presented string, accepted []string) bool {
	ok := 0
	for _, t := range accepted {
		ok |= subtle.ConstantTimeCompare([]byte(presented), []byte(t))
	}
	return ok == 1
}
