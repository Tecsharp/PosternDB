package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthURLCarriesPKCEStateAndNonce(t *testing.T) {
	g := NewGoogle(context.Background(), "client-id.apps.googleusercontent.com", "secret", "https://app.example.com/api/auth/google/callback")
	verifier := strings.Repeat("v", 43)

	raw := g.AuthURL("the-state", "the-nonce", verifier)
	u, err := url.Parse(raw)
	require.NoError(t, err)
	assert.Equal(t, "accounts.google.com", u.Host)
	q := u.Query()

	assert.Equal(t, "code", q.Get("response_type"), "authorization code flow, never implicit")
	assert.Equal(t, "client-id.apps.googleusercontent.com", q.Get("client_id"))
	assert.Equal(t, "https://app.example.com/api/auth/google/callback", q.Get("redirect_uri"))
	assert.Equal(t, "the-state", q.Get("state"))
	assert.Equal(t, "the-nonce", q.Get("nonce"))
	assert.ElementsMatch(t, []string{"openid", "email", "profile"}, strings.Fields(q.Get("scope")))

	sum := sha256.Sum256([]byte(verifier))
	assert.Equal(t, "S256", q.Get("code_challenge_method"))
	assert.Equal(t, base64.RawURLEncoding.EncodeToString(sum[:]), q.Get("code_challenge"), "only the hash of the verifier is sent to Google")
	assert.NotContains(t, raw, verifier, "the verifier itself never leaves the server")
	assert.NotContains(t, raw, "secret", "the client secret is never in the browser URL")
}

func TestExchangeFailsCleanlyWithoutReachingGoogle(t *testing.T) {
	g := NewGoogle(context.Background(), "id", "secret", "https://app.example.com/cb")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := g.Exchange(ctx, "code", "verifier", "nonce")
	assert.Error(t, err)
}
