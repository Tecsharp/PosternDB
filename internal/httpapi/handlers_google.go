package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/tecsharp/posterndb/internal/auth"
	"github.com/tecsharp/posterndb/internal/clients"
	"github.com/tecsharp/posterndb/internal/config"
)

const (
	oauthCookieTTL   = 10 * time.Minute
	oauthExchangeTTL = 15 * time.Second
	oauthTokenBytes  = 32
	oauthCookieParts = 3
)

// Reasons shown on the login screen after a failed Google sign-in.
const (
	oauthCancelled  = "cancelled"
	oauthFailed     = "failed"
	oauthDisabled   = "disabled"
	oauthUnverified = "unverified"
	oauthMismatch   = "mismatch"
)

func randomToken() (string, error) {
	b := make([]byte, oauthTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func oauthCookie(value string, maxAge int, secure bool) *http.Cookie {
	return &http.Cookie{
		Name: config.OAuthCookieName, Value: value, Path: config.OAuthCookiePath, MaxAge: maxAge,
		HttpOnly: true, Secure: secure,
		// Lax, not Strict: the cookie must come back on the top-level
		// redirect from accounts.google.com.
		SameSite: http.SameSiteLaxMode,
	}
}

// googleStart sends the browser to Google. state, nonce and the PKCE
// verifier live only in a short-lived HttpOnly cookie scoped to the callback.
func (s *server) googleStart(w http.ResponseWriter, r *http.Request) {
	if s.Google == nil {
		http.NotFound(w, r)
		return
	}
	var parts [oauthCookieParts]string
	for i := range parts {
		t, err := randomToken()
		if err != nil {
			s.internalError(w, "google start", err)
			return
		}
		parts[i] = t
	}
	state, nonce, verifier := parts[0], parts[1], parts[2]
	http.SetCookie(w, oauthCookie(strings.Join(parts[:], "."), int(oauthCookieTTL.Seconds()), isHTTPS(r)))
	http.Redirect(w, r, s.Google.AuthURL(state, nonce, verifier), http.StatusFound)
}

// googleCallback finishes the sign-in: it checks state, redeems the code,
// finds or creates the account and starts the same kind of session as a
// password login. It always answers with a redirect back to the panel.
func (s *server) googleCallback(w http.ResponseWriter, r *http.Request) {
	if s.Google == nil {
		http.NotFound(w, r)
		return
	}
	secure := isHTTPS(r)
	http.SetCookie(w, oauthCookie("", -1, secure))

	parts, ok := oauthParts(r)
	if !ok {
		redirectOAuth(w, r, oauthFailed)
		return
	}
	q := r.URL.Query()
	if q.Get("error") != "" {
		redirectOAuth(w, r, oauthCancelled)
		return
	}
	code := q.Get("code")
	if code == "" || subtle.ConstantTimeCompare([]byte(parts[0]), []byte(q.Get("state"))) != 1 {
		redirectOAuth(w, r, oauthFailed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), oauthExchangeTTL)
	defer cancel()
	profile, err := s.Google.Exchange(ctx, code, parts[2], parts[1])
	if err != nil {
		s.Logger.Error("google exchange", "error", err)
		redirectOAuth(w, r, oauthFailed)
		return
	}
	u, err := s.Clients.LoginWithGoogle(ctx, profile)
	if err != nil {
		redirectOAuth(w, r, s.googleFailure(err))
		return
	}
	token, exp, err := s.Auth.Issue(auth.ClientIdentity(u).Subject())
	if err != nil {
		s.Logger.Error("issue session", "error", err)
		redirectOAuth(w, r, oauthFailed)
		return
	}
	auth.SetSessionCookie(w, token, exp, secure)
	s.Databases.RecordAudit(ctx, u.Username, config.ActionLoginSuccess, "", map[string]any{"ip": auth.ClientIP(r), "method": "google"})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *server) googleFailure(err error) string {
	switch {
	case errors.Is(err, clients.ErrRegisterDisabled):
		return oauthDisabled
	case errors.Is(err, clients.ErrGoogleUnverified):
		return oauthUnverified
	case errors.Is(err, clients.ErrGoogleMismatch):
		return oauthMismatch
	}
	s.Logger.Error("google login", "error", err)
	return oauthFailed
}

func oauthParts(r *http.Request) ([oauthCookieParts]string, bool) {
	var out [oauthCookieParts]string
	c, err := r.Cookie(config.OAuthCookieName)
	if err != nil {
		return out, false
	}
	fields := strings.Split(c.Value, ".")
	if len(fields) != oauthCookieParts {
		return out, false
	}
	copy(out[:], fields)
	return out, true
}

func redirectOAuth(w http.ResponseWriter, r *http.Request, reason string) {
	http.Redirect(w, r, "/?oauth="+reason, http.StatusFound)
}
