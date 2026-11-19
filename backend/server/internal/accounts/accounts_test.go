package accounts

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeEmail(t *testing.T) {
	t.Parallel()
	got, err := NormalizeEmail("  Dev@Example.COM ")
	require.NoError(t, err)
	require.Equal(t, "dev@example.com", got)
	for _, bad := range []string{"", "dev", "@example.com", "Dev <dev@example.com>", "a@b.c, d@e.f", "dev@example.com\r\nBcc: x@y.z"} {
		_, err := NormalizeEmail(bad)
		require.ErrorIs(t, err, ErrInvalidEmail, "email %q", bad)
	}
}

func TestIsSessionToken(t *testing.T) {
	t.Parallel()
	require.True(t, IsSessionToken("tps_abc"))
	require.False(t, IsSessionToken("tp_abcdefghijkl_secret"))
	require.False(t, IsSessionToken(""))
}

func TestLoginEmail(t *testing.T) {
	t.Parallel()
	m, err := loginEmail("565345", 10)
	require.NoError(t, err)
	require.Equal(t, "565345 is your TaskPilot sign-in code", m.Subject)
	require.Contains(t, m.Text, "Your sign-in code: 565345")
	require.Contains(t, m.Text, "expires in 10 minutes")
	require.Contains(t, m.HTML, ">565345<")
	require.Contains(t, m.HTML, "Expires in 10 minutes")
	require.Contains(t, m.HTML, `src="cid:taskpilot-logo"`)
	require.NotContains(t, m.HTML+m.Text, "signing in as", "the email never repeats the recipient's address")
}

func TestCleanName(t *testing.T) {
	t.Parallel()
	got, err := cleanName("  Billing service ")
	require.NoError(t, err)
	require.Equal(t, "Billing service", got)
	for _, bad := range []string{"", "   ", string(make([]rune, 101))} {
		_, err := cleanName(bad)
		require.ErrorIs(t, err, ErrInvalidName)
	}
}
