// Package accounts is dashboard sign-in: invited users sign in with a one-time code sent to their
// email, get a session, and manage the applications (tenants) they belong to and those applications'
// API keys. API keys remain the only credential applications use to call TaskPilot.
package accounts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	netmail "net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/auth"
	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/mail"
	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/store"
)

// Sign-in limits.
const (
	CodeTTL            = 10 * time.Minute
	SessionTTL         = 8 * time.Hour
	codeResendAfter    = 60 * time.Second // at most one code per user per minute
	maxCodeAttempts    = 5                // wrong guesses before a code stops working
	keyLifetime        = 90 * 24 * time.Hour
	sessionPrefix      = "tps_"
	sessionSecretBytes = 32
)

var (
	// ErrInvalidEmail: not a plain email address.
	ErrInvalidEmail = errors.New("invalid email address")
	// ErrInvalidCode covers wrong, expired, used, and exhausted codes, and unknown emails alike.
	ErrInvalidCode = errors.New("invalid or expired sign-in code")
	// ErrNotMember: the user is not a member of that application (or it does not exist).
	ErrNotMember = errors.New("application not found")
	// ErrInvalidName: application and key names are 1-100 characters.
	ErrInvalidName = errors.New("name must be 1-100 characters")
	// ErrNameTaken: another application already uses that name.
	ErrNameTaken = errors.New("application name already taken")
)

// User is a signed-in dashboard user.
type User struct {
	ID    uuid.UUID
	Email string
}

// Session is a new dashboard session. Token is shown once; only its hash is stored.
type Session struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	Email     string    `json:"email"`
}

// App is an application (a tenant) the user belongs to.
type App struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// Key describes an API key without its secret.
type Key struct {
	ID        uuid.UUID  `json:"id"`
	Name      string     `json:"name"`
	Prefix    string     `json:"prefix"`
	Scopes    []string   `json:"scopes"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at"`
}

// NewKey is a freshly created API key; Secret is the full key, shown once.
type NewKey struct {
	Key
	Secret string `json:"key"`
}

// Service runs account operations.
type Service struct {
	pool *pgxpool.Pool
	mail mail.Sender
}

// New builds a Service. sender may be nil for callers that never send codes (the admin CLI).
func New(pool *pgxpool.Pool, sender mail.Sender) *Service {
	return &Service{pool: pool, mail: sender}
}

// NormalizeEmail trims and lower-cases a plain address ("a@b.c", not "Name <a@b.c>").
func NormalizeEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	addr, err := netmail.ParseAddress(email)
	if err != nil || addr.Address != email || len(email) > 254 || !strings.Contains(email, "@") {
		return "", ErrInvalidEmail
	}
	return email, nil
}

func hash(parts ...string) []byte {
	sum := sha256.Sum256([]byte(strings.Join(parts, ":")))
	return sum[:]
}

// RequestCode emails a 6-digit sign-in code to an invited, enabled user. Unknown emails and requests
// within a minute of the previous code succeed silently, so the response never reveals who is invited.
func (s *Service) RequestCode(ctx context.Context, rawEmail string) error {
	email, err := NormalizeEmail(rawEmail)
	if err != nil {
		return err
	}
	var userID uuid.UUID
	var recent bool
	err = s.pool.QueryRow(ctx, `
		SELECT u.id, EXISTS (SELECT 1 FROM login_codes c WHERE c.user_id = u.id
			AND c.created_at > now() - make_interval(secs => $2))
		FROM users u WHERE u.email = $1 AND u.disabled_at IS NULL`,
		email, codeResendAfter.Seconds()).Scan(&userID, &recent)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && recent) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("look up user: %w", err)
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return fmt.Errorf("generate code: %w", err)
	}
	code := fmt.Sprintf("%06d", n.Int64())
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("new code id: %w", err)
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO login_codes (id, user_id, code_hash, expires_at) VALUES ($1, $2, $3, now() + make_interval(secs => $4))`,
		id, userID, hash(id.String(), code), CodeTTL.Seconds()); err != nil {
		return fmt.Errorf("store code: %w", err)
	}
	msg, err := loginEmail(code, int(CodeTTL.Minutes()))
	if err != nil {
		return err
	}
	return s.mail.Send(ctx, email, msg)
}

// VerifyCode checks the newest code for email and starts a session. A wrong guess counts against the
// code; after maxCodeAttempts it stops working and a new code must be requested.
func (s *Service) VerifyCode(ctx context.Context, rawEmail, code string) (Session, error) {
	email, err := NormalizeEmail(rawEmail)
	if err != nil || len(code) != 6 || strings.Trim(code, "0123456789") != "" {
		return Session{}, ErrInvalidCode
	}
	var session Session
	ok := false
	err = store.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var codeID, userID uuid.UUID
		var stored []byte
		var attempts int
		err := tx.QueryRow(ctx, `
			SELECT c.id, c.user_id, c.code_hash, c.attempts
			FROM login_codes c JOIN users u ON u.id = c.user_id
			WHERE u.email = $1 AND u.disabled_at IS NULL AND c.consumed_at IS NULL AND c.expires_at > now()
			ORDER BY c.created_at DESC LIMIT 1 FOR UPDATE OF c`, email).Scan(&codeID, &userID, &stored, &attempts)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("load code: %w", err)
		}
		if attempts >= maxCodeAttempts {
			return nil
		}
		if subtle.ConstantTimeCompare(hash(codeID.String(), code), stored) != 1 {
			_, err := tx.Exec(ctx, `UPDATE login_codes SET attempts = attempts + 1 WHERE id = $1`, codeID)
			return err // committed, so the guess counts
		}
		if _, err := tx.Exec(ctx, `UPDATE login_codes SET consumed_at = now() WHERE id = $1`, codeID); err != nil {
			return fmt.Errorf("consume code: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET last_login_at = now() WHERE id = $1`, userID); err != nil {
			return fmt.Errorf("record login: %w", err)
		}
		b := make([]byte, sessionSecretBytes)
		_, _ = rand.Read(b) // crypto/rand.Read never returns an error
		token := sessionPrefix + base64.RawURLEncoding.EncodeToString(b)
		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("new session id: %w", err)
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO user_sessions (id, user_id, token_hash, expires_at) VALUES ($1, $2, $3, now() + make_interval(secs => $4))
			RETURNING expires_at`, id, userID, hash(token), SessionTTL.Seconds()).Scan(&session.ExpiresAt); err != nil {
			return fmt.Errorf("create session: %w", err)
		}
		session.Token, session.Email, ok = token, email, true
		return nil
	})
	if err != nil {
		return Session{}, err
	}
	if !ok {
		return Session{}, ErrInvalidCode
	}
	return session, nil
}

// IsSessionToken reports whether a bearer token is a dashboard session token (not an API key).
func IsSessionToken(token string) bool { return strings.HasPrefix(token, sessionPrefix) }

// Authenticate resolves a session token to its user.
func (s *Service) Authenticate(ctx context.Context, token string) (User, error) {
	if !IsSessionToken(token) || len(token) > 100 {
		return User{}, auth.ErrUnauthenticated
	}
	var u User
	err := s.pool.QueryRow(ctx, `
		SELECT u.id, u.email FROM user_sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.revoked_at IS NULL AND s.expires_at > now() AND u.disabled_at IS NULL`,
		hash(token)).Scan(&u.ID, &u.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, auth.ErrUnauthenticated
	}
	if err != nil {
		return User{}, fmt.Errorf("look up session: %w", err)
	}
	return u, nil
}

// SignOut revokes a session token.
func (s *Service) SignOut(ctx context.Context, token string) error {
	if _, err := s.pool.Exec(ctx, `UPDATE user_sessions SET revoked_at = now() WHERE token_hash = $1 AND revoked_at IS NULL`,
		hash(token)); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

// Principal lets a user act on one of their applications with every scope.
func (s *Service) Principal(ctx context.Context, u User, appID uuid.UUID) (auth.Principal, error) {
	var name string
	err := s.pool.QueryRow(ctx, `
		SELECT t.name FROM tenant_members m JOIN tenants t ON t.id = m.tenant_id
		WHERE m.user_id = $1 AND m.tenant_id = $2 AND t.disabled_at IS NULL`, u.ID, appID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.Principal{}, ErrNotMember
	}
	if err != nil {
		return auth.Principal{}, fmt.Errorf("check membership: %w", err)
	}
	return auth.Principal{TenantID: appID, TenantName: name, Scopes: auth.AllScopes, UserID: &u.ID, UserEmail: u.Email}, nil
}

// Apps lists the user's applications, oldest first.
func (s *Service) Apps(ctx context.Context, u User) ([]App, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.name, t.created_at FROM tenant_members m JOIN tenants t ON t.id = m.tenant_id
		WHERE m.user_id = $1 AND t.disabled_at IS NULL ORDER BY t.created_at, t.id`, u.ID)
	if err != nil {
		return nil, fmt.Errorf("list apps: %w", err)
	}
	apps, err := pgx.CollectRows(rows, pgx.RowToStructByPos[App])
	if err != nil {
		return nil, fmt.Errorf("list apps: %w", err)
	}
	return apps, nil
}

func cleanName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if n := utf8.RuneCountInString(name); n < 1 || n > 100 {
		return "", ErrInvalidName
	}
	return name, nil
}

// CreateApp registers a new application owned by u, with a first API key that has every scope.
func (s *Service) CreateApp(ctx context.Context, u User, rawName string) (App, NewKey, error) {
	name, err := cleanName(rawName)
	if err != nil {
		return App{}, NewKey{}, err
	}
	var app App
	var key NewKey
	err = store.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("new app id: %w", err)
		}
		err = tx.QueryRow(ctx, `INSERT INTO tenants (id, name) VALUES ($1, $2) RETURNING id, name, created_at`, id, name).
			Scan(&app.ID, &app.Name, &app.CreatedAt)
		if pgErr := (*pgconn.PgError)(nil); errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrNameTaken
		}
		if err != nil {
			return fmt.Errorf("create app: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO tenant_members (tenant_id, user_id) VALUES ($1, $2)`, app.ID, u.ID); err != nil {
			return fmt.Errorf("add member: %w", err)
		}
		key, err = insertKey(ctx, tx, app.ID, "default")
		return err
	})
	if err != nil {
		return App{}, NewKey{}, err
	}
	return app, key, nil
}

// Keys lists the application's active API keys (never their secrets).
func (s *Service) Keys(ctx context.Context, u User, appID uuid.UUID) ([]Key, error) {
	if _, err := s.Principal(ctx, u, appID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, prefix, scopes, created_at, expires_at FROM api_keys
		WHERE tenant_id = $1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now())
		ORDER BY created_at DESC, id DESC`, appID)
	if err != nil {
		return nil, fmt.Errorf("list keys: %w", err)
	}
	keys, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Key])
	if err != nil {
		return nil, fmt.Errorf("list keys: %w", err)
	}
	return keys, nil
}

// CreateKey adds an API key with every scope to one of the user's applications.
func (s *Service) CreateKey(ctx context.Context, u User, appID uuid.UUID, rawName string) (NewKey, error) {
	name, err := cleanName(rawName)
	if err != nil {
		return NewKey{}, err
	}
	if _, err := s.Principal(ctx, u, appID); err != nil {
		return NewKey{}, err
	}
	var key NewKey
	err = store.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		key, err = insertKey(ctx, tx, appID, name)
		return err
	})
	return key, err
}

func insertKey(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, name string) (NewKey, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return NewKey{}, fmt.Errorf("new key id: %w", err)
	}
	full, prefix, secretHash := auth.NewKey()
	k := NewKey{Secret: full}
	err = tx.QueryRow(ctx, `
		INSERT INTO api_keys (id, tenant_id, name, prefix, secret_hash, scopes, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, now() + make_interval(secs => $7))
		RETURNING id, name, prefix, scopes, created_at, expires_at`,
		id, tenantID, name, prefix, secretHash, auth.AllScopes, keyLifetime.Seconds()).
		Scan(&k.ID, &k.Name, &k.Prefix, &k.Scopes, &k.CreatedAt, &k.ExpiresAt)
	if err != nil {
		return NewKey{}, fmt.Errorf("create key: %w", err)
	}
	return k, nil
}

// Invite lets email sign in (creating the user if needed) and optionally adds them to an application.
// Used by taskpilot-admin; there is no self sign-up.
func (s *Service) Invite(ctx context.Context, rawEmail string, appID *uuid.UUID) (uuid.UUID, error) {
	email, err := NormalizeEmail(rawEmail)
	if err != nil {
		return uuid.Nil, err
	}
	var userID uuid.UUID
	err = store.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("new user id: %w", err)
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO users (id, email) VALUES ($1, $2)
			ON CONFLICT (email) DO UPDATE SET disabled_at = NULL RETURNING id`, id, email).Scan(&userID); err != nil {
			return fmt.Errorf("invite user: %w", err)
		}
		if appID == nil {
			return nil
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO tenant_members (tenant_id, user_id) SELECT id, $2 FROM tenants WHERE id = $1
			ON CONFLICT DO NOTHING`, *appID, userID)
		if err != nil {
			return fmt.Errorf("add member: %w", err)
		}
		if tag.RowsAffected() == 0 {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM tenants WHERE id = $1)`, *appID).Scan(&exists); err != nil {
				return fmt.Errorf("check app: %w", err)
			}
			if !exists {
				return ErrNotMember
			}
		}
		return nil
	})
	return userID, err
}
