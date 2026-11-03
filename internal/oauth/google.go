package oauth

import (
	"context"
	"errors"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const googleKeysURL = "https://www.googleapis.com/oauth2/v3/certs"

// Google's ID tokens use either spelling of the issuer.
var googleIssuers = map[string]bool{"https://accounts.google.com": true, "accounts.google.com": true}

// ErrBadToken covers every way an ID token can fail verification.
var ErrBadToken = errors.New("google: token de identidad no válido")

// Google runs the authorization code flow with PKCE against Google.
type Google struct {
	cfg      oauth2.Config
	verifier *oidc.IDTokenVerifier
}

// NewGoogle builds the client. Google's signing keys are fetched lazily on
// the first verification, so a network blip at startup does not stop the app.
func NewGoogle(ctx context.Context, clientID, clientSecret, redirectURL string) *Google {
	keys := oidc.NewRemoteKeySet(ctx, googleKeysURL)
	return &Google{
		cfg: oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Endpoint:     google.Endpoint,
			Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		},
		// The issuer is checked by hand below because Google uses two spellings.
		verifier: oidc.NewVerifier("https://accounts.google.com", keys, &oidc.Config{ClientID: clientID, SkipIssuerCheck: true}),
	}
}

// AuthURL is where to send the browser. state defends against forged
// callbacks, nonce binds the ID token to this login, and the PKCE verifier
// proves the code is being redeemed by whoever started the flow.
func (g *Google) AuthURL(state, nonce, verifier string) string {
	return g.cfg.AuthCodeURL(state,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("nonce", nonce),
		oauth2.SetAuthURLParam("prompt", "select_account"),
	)
}

// Exchange redeems the authorization code and returns the verified profile.
func (g *Google) Exchange(ctx context.Context, code, verifier, nonce string) (Profile, error) {
	token, err := g.cfg.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return Profile{}, fmt.Errorf("google: canjear el código: %w", err)
	}
	raw, _ := token.Extra("id_token").(string)
	if raw == "" {
		return Profile{}, ErrBadToken
	}
	idToken, err := g.verifier.Verify(ctx, raw)
	if err != nil || !googleIssuers[idToken.Issuer] || idToken.Nonce != nonce {
		return Profile{}, ErrBadToken
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return Profile{}, ErrBadToken
	}
	return Profile{Subject: idToken.Subject, Email: claims.Email, Name: claims.Name, EmailVerified: claims.EmailVerified}, nil
}
