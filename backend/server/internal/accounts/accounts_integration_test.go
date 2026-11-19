//go:build integration

package accounts

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/url"
	"os"
	"regexp"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/auth"
	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/mail"
	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/store"
)

var testPool *pgxpool.Pool

// TestMain creates a throwaway database from TASKPILOT_TEST_DATABASE_URL (an owner URL), migrates it,
// runs the tests, and drops it.
func TestMain(m *testing.M) {
	os.Exit(func() int {
		base := os.Getenv("TASKPILOT_TEST_DATABASE_URL")
		if base == "" {
			fmt.Println("TASKPILOT_TEST_DATABASE_URL not set")
			return 1
		}
		ctx := context.Background()
		admin, err := store.Open(ctx, base, 2)
		if err != nil {
			fmt.Println(err)
			return 1
		}
		defer admin.Close()
		name := fmt.Sprintf("test_accounts_%d", rand.Uint32())
		if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
			fmt.Println(err)
			return 1
		}
		defer admin.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)") //nolint:errcheck // best-effort cleanup
		u, _ := url.Parse(base)
		u.Path = "/" + name
		testPool, err = store.Open(ctx, u.String(), 10)
		if err != nil {
			fmt.Println(err)
			return 1
		}
		defer testPool.Close()
		if _, err := store.Migrate(ctx, testPool); err != nil {
			fmt.Println(err)
			return 1
		}
		return m.Run()
	}())
}

// inbox records sent emails instead of delivering them.
type inbox struct {
	mu   sync.Mutex
	sent []string
}

func (b *inbox) Send(_ context.Context, to string, m mail.Message) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sent = append(b.sent, to+"\n"+m.Text)
	return nil
}

var codePattern = regexp.MustCompile(`code is (\d{6})`)

func (b *inbox) lastCode(t *testing.T) string {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	require.NotEmpty(t, b.sent)
	m := codePattern.FindStringSubmatch(b.sent[len(b.sent)-1])
	require.Len(t, m, 2)
	return m[1]
}

func newEmail() string { return "user-" + uuid.NewString()[:8] + "@example.com" }

func signIn(t *testing.T, s *Service, box *inbox, email string) Session {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, s.RequestCode(ctx, email))
	session, err := s.VerifyCode(ctx, email, box.lastCode(t))
	require.NoError(t, err)
	return session
}

func TestSignIn_InvitedOnly(t *testing.T) {
	ctx := context.Background()
	box := &inbox{}
	s := New(testPool, box)

	// Uninvited: the request "succeeds" but nothing is sent, and no code can work.
	stranger := newEmail()
	require.NoError(t, s.RequestCode(ctx, stranger))
	require.Empty(t, box.sent)
	_, err := s.VerifyCode(ctx, stranger, "123456")
	require.ErrorIs(t, err, ErrInvalidCode)

	email := newEmail()
	_, err = s.Invite(ctx, email, nil)
	require.NoError(t, err)
	session := signIn(t, s, box, "  "+email+" ") // case and spacing don't matter
	require.Equal(t, email, session.Email)
	require.Regexp(t, `^tps_`, session.Token)

	u, err := s.Authenticate(ctx, session.Token)
	require.NoError(t, err)
	require.Equal(t, email, u.Email)

	// A code works once.
	_, err = s.VerifyCode(ctx, email, box.lastCode(t))
	require.ErrorIs(t, err, ErrInvalidCode)

	require.NoError(t, s.SignOut(ctx, session.Token))
	_, err = s.Authenticate(ctx, session.Token)
	require.ErrorIs(t, err, auth.ErrUnauthenticated)
}

func TestSignIn_CodeLimits(t *testing.T) {
	ctx := context.Background()
	box := &inbox{}
	s := New(testPool, box)
	email := newEmail()
	_, err := s.Invite(ctx, email, nil)
	require.NoError(t, err)

	require.NoError(t, s.RequestCode(ctx, email))
	require.NoError(t, s.RequestCode(ctx, email)) // within a minute: no second email
	require.Len(t, box.sent, 1)
	code := box.lastCode(t)

	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	for range maxCodeAttempts {
		_, err := s.VerifyCode(ctx, email, wrong)
		require.ErrorIs(t, err, ErrInvalidCode)
	}
	// Too many wrong guesses: even the right code no longer works.
	_, err = s.VerifyCode(ctx, email, code)
	require.ErrorIs(t, err, ErrInvalidCode)

	// Expired codes don't work either.
	_, err = testPool.Exec(ctx, `UPDATE login_codes SET attempts = 0, expires_at = now() - interval '1 second'
		WHERE user_id = (SELECT id FROM users WHERE email = $1)`, email)
	require.NoError(t, err)
	_, err = s.VerifyCode(ctx, email, code)
	require.ErrorIs(t, err, ErrInvalidCode)
}

func TestApps_MembershipAndKeys(t *testing.T) {
	ctx := context.Background()
	box := &inbox{}
	s := New(testPool, box)
	ownerEmail, otherEmail := newEmail(), newEmail()
	for _, e := range []string{ownerEmail, otherEmail} {
		_, err := s.Invite(ctx, e, nil)
		require.NoError(t, err)
	}
	owner, err := s.Authenticate(ctx, signIn(t, s, box, ownerEmail).Token)
	require.NoError(t, err)
	other, err := s.Authenticate(ctx, signIn(t, s, box, otherEmail).Token)
	require.NoError(t, err)

	name := "app-" + uuid.NewString()[:8]
	app, key, err := s.CreateApp(ctx, owner, " "+name+" ")
	require.NoError(t, err)
	require.Equal(t, name, app.Name)
	_, _, err = s.CreateApp(ctx, owner, name)
	require.ErrorIs(t, err, ErrNameTaken)

	// The returned key is a working API key for the new application.
	p, err := auth.Authenticate(ctx, testPool, key.Secret)
	require.NoError(t, err)
	require.Equal(t, app.ID, p.TenantID)
	require.ElementsMatch(t, auth.AllScopes, p.Scopes)

	apps, err := s.Apps(ctx, owner)
	require.NoError(t, err)
	require.Equal(t, []App{app}, apps)

	up, err := s.Principal(ctx, owner, app.ID)
	require.NoError(t, err)
	require.Equal(t, app.ID, up.TenantID)
	kind, id := up.Actor()
	require.Equal(t, "user", kind)
	require.Equal(t, owner.ID.String(), id)

	// Non-members can't see the application or its keys.
	_, err = s.Principal(ctx, other, app.ID)
	require.ErrorIs(t, err, ErrNotMember)
	_, err = s.Keys(ctx, other, app.ID)
	require.ErrorIs(t, err, ErrNotMember)
	_, err = s.CreateKey(ctx, other, app.ID, "x")
	require.ErrorIs(t, err, ErrNotMember)

	second, err := s.CreateKey(ctx, owner, app.ID, "ci")
	require.NoError(t, err)
	keys, err := s.Keys(ctx, owner, app.ID)
	require.NoError(t, err)
	require.Len(t, keys, 2)
	require.Equal(t, second.ID, keys[0].ID) // newest first
	require.NotNil(t, keys[0].ExpiresAt)

	// Inviting into an existing application makes the user a member.
	_, err = s.Invite(ctx, otherEmail, &app.ID)
	require.NoError(t, err)
	_, err = s.Principal(ctx, other, app.ID)
	require.NoError(t, err)
	missing := uuid.New()
	_, err = s.Invite(ctx, otherEmail, &missing)
	require.ErrorIs(t, err, ErrNotMember)
}
