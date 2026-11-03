// Package auth implements panel authentication: credential checks against
// ADMIN_USER / ADMIN_PASSWORD, JWT sessions in an HttpOnly cookie, CSRF
// protection and per-IP login rate limiting.
//
// Three principal tiers share one login endpoint and one session cookie:
//
//  1. the bootstrap owner (ADMIN_USER / ADMIN_PASSWORD from .env);
//  2. stored administrators (internal/admins), role owner/admin/viewer —
//     together these unlock the admin panel (/api/* routes);
//  3. client users (internal/clients), role "client" — unlocks only the
//     reduced "panel de usuario" (/api/me/* routes). config.RoleClient is
//     never part of the admin role hierarchy (see roleRank), so a client
//     session can never pass RequireRole and reach an admin route.
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

// Subject is what goes into the session token. Clients are namespaced by id
// so a client can never share a token subject with the bootstrap owner or an
// administrator that happens to use the same name.
func (i Identity) Subject() string {
	if i.Role == config.RoleClient && i.ClientUserID != nil {
		return clientSubjectPrefix + strconv.FormatInt(*i.ClientUserID, 10)
	}
	return i.Username
}

// ClientIdentity builds the identity of a client user.
func ClientIdentity(u model.ClientUser) Identity {
	id := u.ID
	return Identity{Username: u.Username, Role: config.RoleClient, ClientUserID: &id}
}

// AdminStore authenticates administrators stored in the database (the
// bootstrap owner from .env is handled here, without the store).
type AdminStore interface {
	Authenticate(ctx context.Context, username, password string) (role string, ok bool, err error)
	RoleOf(ctx context.Context, username string) (string, error)
}

// ClientStore authenticates self-service client users (internal/clients).
type ClientStore interface {
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
	admins        AdminStore
	clients       ClientStore
	// clientPasswords is false when AUTH_MODE=google: registered users then
	// have no password login at all (administrators still do).
	clientPasswords bool
}

// New builds an Authenticator from the configured bootstrap credentials.
// admins/clients may be nil when that tier is not wired up (tests).
func New(user, password, secret string, ttl time.Duration, admins AdminStore, clients ClientStore) *Authenticator {
	return &Authenticator{
		bootstrapUser: user,
		userHash:      sha256.Sum256([]byte(user)),
		passHash:      sha256.Sum256([]byte(password)),
		secret:        []byte(secret),
		ttl:           ttl,
		now:           time.Now,
		admins:        admins,
		clients:       clients,

		clientPasswords: true,
	}
}

// SetClientPasswordLogin turns password login for registered users on or off.
func (a *Authenticator) SetClientPasswordLogin(enabled bool) { a.clientPasswords = enabled }

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

// IsBootstrap reports whether username is the .env owner.
func (a *Authenticator) IsBootstrap(username string) bool {
	return username == a.bootstrapUser
}

// Login checks the bootstrap owner, then stored administrators, then client
// users — whichever tier has a matching username and password.
func (a *Authenticator) Login(ctx context.Context, username, password string) (Identity, bool, error) {
	if a.CheckCredentials(username, password) {
		return Identity{Username: username, Role: config.RoleOwner}, true, nil
	}
	if a.IsBootstrap(username) {
		return Identity{}, false, nil
	}
	if a.admins != nil {
		role, ok, err := a.admins.Authenticate(ctx, username, password)
		if err != nil {
			return Identity{}, false, err
		}
		if ok {
			return Identity{Username: username, Role: role}, true, nil
		}
	}
	if a.clients != nil && a.clientPasswords {
		u, ok, err := a.clients.Authenticate(ctx, username, password)
		if err != nil {
			return Identity{}, false, err
		}
		if ok {
			return ClientIdentity(u), true, nil
		}
	}
	return Identity{}, false, nil
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
	claims := &jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) { return a.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(config.JWTIssuer),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(a.now),
	)
	if err != nil || claims.Subject == "" {
		return "", ErrInvalidSession
	}
	return claims.Subject, nil
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
	subject, err := a.Verify(cookie.Value)
	if err != nil {
		return Identity{}, err
	}
	// Client subjects are checked first and never fall through to the
	// owner/admin lookups below, whatever name the client chose.
	if raw, isClient := strings.CutPrefix(subject, clientSubjectPrefix); isClient {
		return a.identifyClient(r.Context(), raw)
	}
	if a.IsBootstrap(subject) {
		return Identity{Username: subject, Role: config.RoleOwner}, nil
	}
	if a.admins != nil {
		if role, err := a.admins.RoleOf(r.Context(), subject); err == nil {
			return Identity{Username: subject, Role: role}, nil
		}
	}
	return Identity{}, ErrInvalidSession
}

func (a *Authenticator) identifyClient(ctx context.Context, raw string) (Identity, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || a.clients == nil {
		return Identity{}, ErrInvalidSession
	}
	u, ok, err := a.clients.ByID(ctx, id)
	if err != nil || !ok {
		return Identity{}, ErrInvalidSession
	}
	return ClientIdentity(u), nil
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
