package credentials

import (
	"bytes"
	"crypto/rand"
	"errors"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("entropy exhausted") }

func TestPasswordLengthAndAlphabet(t *testing.T) {
	g := New()
	p, err := g.Password(32)
	require.NoError(t, err)
	assert.Len(t, p, 32)
	assert.Regexp(t, regexp.MustCompile(`^[A-Za-z0-9]+$`), p)
}

func TestPasswordsAreUnique(t *testing.T) {
	g := New()
	seen := map[string]bool{}
	for range 200 {
		p, err := g.Password(16)
		require.NoError(t, err)
		assert.False(t, seen[p], "duplicate password generated")
		seen[p] = true
	}
}

func TestPasswordErrors(t *testing.T) {
	_, err := New().Password(0)
	assert.ErrorIs(t, err, ErrInvalidLength)

	_, err = Generator{Random: failingReader{}}.Password(8)
	assert.Error(t, err)
}

func TestScramVerifierFormat(t *testing.T) {
	v, err := New().ScramVerifier("secret")
	require.NoError(t, err)
	assert.Regexp(t, regexp.MustCompile(`^SCRAM-SHA-256\$4096:[A-Za-z0-9+/=]{24}\$[A-Za-z0-9+/=]{44}:[A-Za-z0-9+/=]{44}$`), v)
	assert.NotContains(t, v, "secret")
}

func TestScramVerifierIsSalted(t *testing.T) {
	g := Generator{Random: rand.Reader}
	a, _ := g.ScramVerifier("same")
	b, _ := g.ScramVerifier("same")
	assert.NotEqual(t, a, b)
}

// Known-answer test: with a fixed salt the derivation must be deterministic.
// The integration suite additionally proves PostgreSQL accepts the verifier
// by logging in with the matching password.
func TestScramVerifierDeterministicWithSalt(t *testing.T) {
	salt := bytes.Repeat([]byte{1}, scramSaltLength)
	a, err := scramVerifierWithSalt("pencil", salt, scramIterations)
	require.NoError(t, err)
	b, err := Generator{Random: bytes.NewReader(salt)}.ScramVerifier("pencil")
	require.NoError(t, err)
	assert.Equal(t, a, b)
}

func TestScramVerifierSaltError(t *testing.T) {
	_, err := Generator{Random: failingReader{}}.ScramVerifier("x")
	assert.Error(t, err)
}
