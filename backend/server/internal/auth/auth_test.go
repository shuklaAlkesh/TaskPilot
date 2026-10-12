package auth

import (
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewKey_ParsesBack(t *testing.T) {
	t.Parallel()
	for range 100 { // secrets often contain '_' or '-'; parsing must still split at the prefix
		full, prefix, hash := NewKey()
		gotPrefix, secret, err := ParseKey(full)
		require.NoError(t, err)
		require.Equal(t, prefix, gotPrefix)
		sum := sha256.Sum256([]byte(secret))
		require.Equal(t, hash, sum[:])
		require.Regexp(t, `^[a-z0-9]{12}$`, prefix)
	}
}

func TestParseKey_RejectsMalformed(t *testing.T) {
	t.Parallel()
	valid, _, _ := NewKey()
	for _, k := range []string{
		"", "Bearer x", "tp_", "tp_abc_def", valid[3:], "xx" + valid[2:],
		"tp_ABCDEFGHIJKL_" + valid[16:], valid + "x",
	} {
		_, _, err := ParseKey(k)
		require.ErrorIs(t, err, ErrUnauthenticated, "key %q", k)
	}
}

func TestWorkerTokenValid(t *testing.T) {
	t.Parallel()
	require.True(t, WorkerTokenValid("current", []string{"current", "previous"}))
	require.True(t, WorkerTokenValid("previous", []string{"current", "previous"}))
	require.False(t, WorkerTokenValid("other", []string{"current", "previous"}))
	require.False(t, WorkerTokenValid("", []string{"current"}))
}
