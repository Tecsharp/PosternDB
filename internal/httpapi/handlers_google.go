package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
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
	// state.nonce.verifier.mode.userID.remember.mac
	oauthCookieParts = 7
	oauthSignedParts = oauthCookieParts - 1

	modeLogin = "login"
	modeLink  = "link"
)

// Reasons shown on the login screen after a failed Google sign-in.
const (
	oauthCancelled  = "cancelled"
	oauthFailed     = "failed"
	oauthDisabled   = "disabled"
	oauthUnverified = "unverified"
	oauthMismatch   = "mismatch"
	oauthNotLinked  = "notlinked"
	oauthTaken      = "taken"
	oauthLinked     = "linked"
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

// oauthState is what survives the round trip to Google, in a signed cookie.
type oauthState struct {
	state, nonce, verifier string
	mode                   string
	userID                 int64
	remember               bool
}

func (s *server) encodeOAuthState(o oauthState) string {
	remember := "0"
	if o.remember {
		remember = "1"
	}
	signed := strings.Join([]string{o.state, o.nonce, o.verifier, o.mode, strconv.FormatInt(o.userID, 10), remember}, ".")
	return signed + "." + s.Auth.MAC(signed)
}

func (s *server) decodeOAuthState(r *http.Request) (oauthState, bool) {
	c, err := r.Cookie(config.OAuthCookieName)
	if err != nil {
		return oauthState{}, false
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != oauthCookieParts {
		return oauthState{}, false
	}
	signed := strings.Join(parts[:oauthSignedParts], ".")
	if !s.Auth.CheckMAC(signed, parts[oauthSignedParts]) {
		return oauthState{}, false
	}
	uid, err := strconv.ParseInt(parts[4], 10, 64)
	if err != nil || (parts[3] != modeLogin && parts[3] != modeLink) {
		return oauthState{}, false
	}
	return oauthState{state: parts[0], nonce: parts[1], verifier: parts[2], mode: parts[3], userID: uid, remember: parts[5] == "1"}, true
}

// beginGoogle sends the browser to Google. state, nonce and the PKCE verifier
// live only in a short-lived, signed, HttpOnly cookie scoped to the callback.
func (s *server) beginGoogle(w http.ResponseWriter, r *http.Request, mode string, userID int64, remember bool) {
	var t [3]string
	for i := range t {
		v, err := randomToken()
		if err != nil {
			s.internalError(w, "google start", err)
			return
		}
		t[i] = v
	}
	st := oauthState{state: t[0], nonce: t[1], verifier: t[2], mode: mode, userID: userID, remember: remember}
	http.SetCookie(w, oauthCookie(s.encodeOAuthState(st), int(oauthCookieTTL.Seconds()), isHTTPS(r)))
	http.Redirect(w, r, s.Google.AuthURL(st.state, st.nonce, st.verifier), http.StatusFound)
}

// googleStart is "Continuar con Google" on the login screen.
func (s *server) googleStart(w http.ResponseWriter, r *http.Request) {
	if s.Google == nil {
		http.NotFound(w, r)
		return
	}
	s.beginGoogle(w, r, modeLogin, 0, r.URL.Query().Get("remember") == "1")
}

// googleLink is "Vincular Google" in the user's Security page. The signed
// cookie carries which user asked, because the session cookie (SameSite=Strict)
// is not sent back on the redirect from Google.
func (s *server) googleLink(w http.ResponseWriter, r *http.Request) {
	if s.Google == nil {
		http.NotFound(w, r)
		return
	}
	s.beginGoogle(w, r, modeLink, clientID(r), false)
}

// googleCallback finishes either flow. It always answers with a redirect back
// to the panel: to the login screen with a reason, or to Seguridad.
func (s *server) googleCallback(w http.ResponseWriter, r *http.Request) {
	if s.Google == nil {
		http.NotFound(w, r)
		return
	}
	secure := isHTTPS(r)
	http.SetCookie(w, oauthCookie("", -1, secure))

	st, ok := s.decodeOAuthState(r)
	if !ok {
		redirectOAuth(w, r, modeLogin, oauthFailed)
		return
	}
	q := r.URL.Query()
	if q.Get("error") != "" {
		redirectOAuth(w, r, st.mode, oauthCancelled)
		return
	}
	code := q.Get("code")
	if code == "" || subtle.ConstantTimeCompare([]byte(st.state), []byte(q.Get("state"))) != 1 {
		redirectOAuth(w, r, st.mode, oauthFailed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), oauthExchangeTTL)
	defer cancel()
	profile, err := s.Google.Exchange(ctx, code, st.verifier, st.nonce)
	if err != nil {
		s.Logger.Error("google exchange", "error", err)
		redirectOAuth(w, r, st.mode, oauthFailed)
		return
	}

	if st.mode == modeLink {
		if _, err := s.Clients.LinkGoogle(ctx, st.userID, profile); err != nil {
			redirectOAuth(w, r, modeLink, s.googleFailure(err))
			return
		}
		redirectOAuth(w, r, modeLink, oauthLinked)
		return
	}

	u, err := s.Clients.LoginWithGoogle(ctx, profile)
	if err != nil {
		redirectOAuth(w, r, modeLogin, s.googleFailure(err))
		return
	}
	token, exp, err := s.Auth.IssueFor(auth.AccountIdentity(u).Subject(), s.Auth.SessionTTL(st.remember))
	if err != nil {
		s.Logger.Error("issue session", "error", err)
		redirectOAuth(w, r, modeLogin, oauthFailed)
		return
	}
	auth.SetSessionCookie(w, token, exp, st.remember, secure)
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
	case errors.Is(err, clients.ErrGoogleNotLinked):
		return oauthNotLinked
	case errors.Is(err, clients.ErrGoogleTaken):
		return oauthTaken
	}
	s.Logger.Error("google login", "error", err)
	return oauthFailed
}

// redirectOAuth sends the browser back: the login screen for sign-in, the
// Seguridad page for linking.
func redirectOAuth(w http.ResponseWriter, r *http.Request, mode, reason string) {
	if mode == modeLink {
		http.Redirect(w, r, "/security?google="+reason, http.StatusFound)
		return
	}
	http.Redirect(w, r, "/?oauth="+reason, http.StatusFound)
}
