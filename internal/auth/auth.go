// Package auth implements panel authentication: credential checks against
// ADMIN_USER / ADMIN_PASSWORD, JWT sessions in an HttpOnly cookie, CSRF
// protection and per-IP login rate limiting.
//
// Two kinds of principal share one login endpoint and one session cookie:
//
//  1. the owner (ADMIN_USER / ADMIN_PASSWORD from .env), who is not an account
//     and so can never be edited or revoked by anyone;
//  2. accounts (internal/clients). An account's role decides what it opens:
//     "user" unlocks only the reduced "panel de usuario" (/api/me/* routes),
//     while "admin" and "viewer" unlock the admin panel (/api/* routes).
//     config.RoleClient is never part of the admin role hierarchy (see
//     roleRank), so a plain user can never pass RequireRole.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/model"
)

// ErrInvalidSession covers missing, expired, tampered or foreign tokens.
var ErrInvalidSession = errors.New("invalid session")

type ctxKey struct{}

// Identity is the authenticated principal of a request.
type Identity struct {
	Username string `json:"user"`
	Role     string `json:"role"`
	// ClientUserID is set only for Role == config.RoleClient: the row id in
	// client_users, needed to look up "my databases" by id, not by name.
	ClientUserID *int64 `json:"-"`
}

const clientSubjectPrefix = "client:"

// Subject is what goes into the session token. Accounts are namespaced by id
// so one can never share a token subject with the owner, whatever name it picked.
func (i Identity) Subject() string {
	if i.ClientUserID != nil {
		return clientSubjectPrefix + strconv.FormatInt(*i.ClientUserID, 10)
	}
	return i.Username
}

// AccountIdentity builds the identity of an account: a plain user is a
// client, and an account with the admin or viewer role is an administrator.
func AccountIdentity(u model.ClientUser) Identity {
	id := u.ID
	role := config.RoleClient
	switch u.Role {
	case config.RoleAdmin:
		role = config.RoleAdmin
	case config.RoleViewer:
		role = config.RoleViewer
	}
	return Identity{Username: u.Username, Role: role, ClientUserID: &id}
}

// AccountStore authenticates accounts (internal/clients). The owner from .env
// is handled here, without the store.
type AccountStore interface {
	Authenticate(ctx context.Context, username, password string) (model.ClientUser, bool, error)
	ByID(ctx context.Context, id int64) (model.ClientUser, bool, error)
}

// Authenticator validates credentials and manages session tokens.
type Authenticator struct {
	bootstrapUser string
	userHash      [32]byte
	passHash      [32]byte
	secret        []byte
	ttl           time.Duration
	now           func() time.Time
	accounts      AccountStore
	// userPasswords is false when AUTH_MODE=google: plain users then have no
	// password login at all (administrators still do).
	userPasswords bool
}

// New builds an Authenticator from the configured owner credentials.
// accounts may be nil when no account store is wired up (tests).
func New(user, password, secret string, ttl time.Duration, accounts AccountStore) *Authenticator {
	return &Authenticator{
		bootstrapUser: user,
		userHash:      sha256.Sum256([]byte(user)),
		passHash:      sha256.Sum256([]byte(password)),
		secret:        []byte(secret),
		ttl:           ttl,
		now:           time.Now,
		accounts:      accounts,

		userPasswords: true,
	}
}

// SetUserPasswordLogin turns password login for plain users on or off.
// Administrators keep it either way.
func (a *Authenticator) SetUserPasswordLogin(enabled bool) { a.userPasswords = enabled }

// SessionTTL is how long a new session lasts: the normal lifetime, or the
// long one when the person asked to be remembered.
func (a *Authenticator) SessionTTL(remember bool) time.Duration {
	if remember {
		return config.RememberTTL
	}
	return a.ttl
}

// MAC signs data with the session secret, so values that travel through the
// browser (the OAuth cookie) cannot be forged.
func (a *Authenticator) MAC(data string) string {
	m := hmac.New(sha256.New, a.secret)
	m.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// CheckMAC verifies a value signed by MAC, in constant time.
func (a *Authenticator) CheckMAC(data, mac string) bool {
	return hmac.Equal([]byte(a.MAC(data)), []byte(mac))
}

// BootstrapUser is the owner's username from .env.
func (a *Authenticator) BootstrapUser() string { return a.bootstrapUser }

// IsBootstrap reports whether username is the .env owner.
func (a *Authenticator) IsBootstrap(username string) bool {
	return username == a.bootstrapUser
}

// Login checks the owner, then the accounts: whoever has a matching username
// (or email) and password.
func (a *Authenticator) Login(ctx context.Context, username, password string) (Identity, bool, error) {
	if a.CheckCredentials(username, password) {
		return Identity{Username: username, Role: config.RoleOwner}, true, nil
	}
	if a.IsBootstrap(username) || a.accounts == nil {
		return Identity{}, false, nil
	}
	u, ok, err := a.accounts.Authenticate(ctx, username, password)
	if err != nil || !ok {
		return Identity{}, false, err
	}
	if !a.userPasswords && u.Role == config.AccountRoleUser {
		return Identity{}, false, nil
	}
	return AccountIdentity(u), true, nil
}

// CheckCredentials compares in constant time. Hashing first gives both sides
// the same length, so neither the comparison time nor an early length check
// reveals anything about the configured values. Both fields are always
// compared to avoid leaking which one was wrong.
func (a *Authenticator) CheckCredentials(user, password string) bool {
	u := sha256.Sum256([]byte(user))
	p := sha256.Sum256([]byte(password))
	userOK := subtle.ConstantTimeCompare(u[:], a.userHash[:])
	passOK := subtle.ConstantTimeCompare(p[:], a.passHash[:])
	return userOK&passOK == 1
}

// Issue returns a signed HS256 token for subject. The role is not stored in
// the token: it is resolved on every request so demoting or deleting an
// administrator or client takes effect immediately.
func (a *Authenticator) Issue(subject string) (string, time.Time, error) {
	return a.IssueFor(subject, a.ttl)
}

// IssueFor is Issue with an explicit lifetime.
func (a *Authenticator) IssueFor(subject string, ttl time.Duration) (string, time.Time, error) {
	now := a.now()
	exp := now.Add(ttl)
	claims := jwt.RegisteredClaims{
		Issuer:    config.JWTIssuer,
		Subject:   subject,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(exp),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(a.secret)
	return signed, exp, err
}

// Verify parses a token and returns its subject. The signing method is
// pinned to HS256 to block "alg: none" and algorithm-confusion attacks.
func (a *Authenticator) Verify(token string) (string, error) {
	subject, _, err := a.verifyIssued(token)
	return subject, err
}

// verifyIssued is Verify that also returns when the token was issued.
func (a *Authenticator) verifyIssued(token string) (string, time.Time, error) {
	claims := &jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) { return a.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(config.JWTIssuer),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(a.now),
	)
	if err != nil || claims.Subject == "" || claims.IssuedAt == nil {
		return "", time.Time{}, ErrInvalidSession
	}
	return claims.Subject, claims.IssuedAt.Time, nil
}

// SetSessionCookie writes the session cookie. HttpOnly keeps it away from
// JavaScript (XSS cannot steal it), Secure because the panel is only served
// over HTTPS through Cloudflare, SameSite=Strict as the first CSRF barrier.
func SetSessionCookie(w http.ResponseWriter, token string, expires time.Time, persistent, secure bool) {
	c := &http.Cookie{
		Name:     config.SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	}
	// Without Expires it is a session cookie: it ends when the browser
	// closes. "Recordarme" makes it persistent.
	if persistent {
		c.Expires = expires
	}
	http.SetCookie(w, c)
}

// ClearSessionCookie expires the session cookie.
func ClearSessionCookie(w http.ResponseWriter, secure bool) {
	SetSessionCookie(w, "", time.Unix(0, 0), true, secure)
}

// Middleware rejects requests without a valid session and stores the
// identity (with its current role) in the request context.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := a.identify(r)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, id)))
	})
}

func (a *Authenticator) identify(r *http.Request) (Identity, error) {
	cookie, err := r.Cookie(config.SessionCookieName)
	if err != nil {
		return Identity{}, ErrInvalidSession
	}
	subject, issued, err := a.verifyIssued(cookie.Value)
	if err != nil {
		return Identity{}, err
	}
	// Account subjects are checked first and never fall through to the owner
	// lookup below, whatever name the account chose.
	if raw, isAccount := strings.CutPrefix(subject, clientSubjectPrefix); isAccount {
		return a.identifyAccount(r.Context(), raw, issued)
	}
	if a.IsBootstrap(subject) {
		return Identity{Username: subject, Role: config.RoleOwner}, nil
	}
	return Identity{}, ErrInvalidSession
}

// identifyAccount re-reads the account on every request, so a role change, a
// deletion or a new password takes effect immediately.
func (a *Authenticator) identifyAccount(ctx context.Context, raw string, issued time.Time) (Identity, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || a.accounts == nil {
		return Identity{}, ErrInvalidSession
	}
	u, ok, err := a.accounts.ByID(ctx, id)
	// Tokens carry whole seconds, so compare at that precision: a session
	// opened right after the password change must not be refused.
	if err != nil || !ok || issued.Before(u.SessionsValidFrom.Truncate(time.Second)) {
		return Identity{}, ErrInvalidSession
	}
	return AccountIdentity(u), nil
}

// FromContext returns the identity stored by Middleware.
func FromContext(ctx context.Context) Identity {
	id, _ := ctx.Value(ctxKey{}).(Identity)
	return id
}

// Subject returns the authenticated username stored by Middleware.
func Subject(ctx context.Context) string {
	return FromContext(ctx).Username
}

var roleRank = map[string]int{config.RoleViewer: 1, config.RoleAdmin: 2, config.RoleOwner: 3}

// Allows reports whether role is at least min. config.RoleClient is never in
// roleRank, so it is always false here — a client session can never satisfy
// an admin RequireRole check, by construction, not by convention.
func Allows(role, min string) bool {
	return roleRank[role] >= roleRank[min] && roleRank[min] > 0
}

// RequireRole answers 403 unless the identity has at least role min.
func RequireRole(min string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !Allows(FromContext(r.Context()).Role, min) {
				http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAccount answers 403 unless the identity is an account (not the owner
// from .env), whatever its role. For routes about the account itself: its
// password, username and linked Google.
func RequireAccount(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if FromContext(r.Context()).ClientUserID == nil {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireClient answers 403 unless the identity is a client user. It is an
// exact match, not a hierarchy level: an admin or owner hitting /api/me/*
// is refused rather than silently treated as a client.
func RequireClient(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if FromContext(r.Context()).Role != config.RoleClient {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireCSRFHeader blocks state-changing requests that lack the custom
// X-Requested-With header. Browsers cannot attach custom headers to
// cross-site form posts or simple requests without a CORS preflight (which
// we never allow), so this complements SameSite=Strict.
func RequireCSRFHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isSafeMethod(r.Method) || r.Header.Get(config.HeaderRequestedWith) == config.RequestedWithValue {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
	})
}

func isSafeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}
