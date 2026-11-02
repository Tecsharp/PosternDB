// Package auth implements panel authentication: credential checks against
// ADMIN_USER / ADMIN_PASSWORD, JWT sessions in an HttpOnly cookie, CSRF
// protection and per-IP login rate limiting.
package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/tecsharp/posterndb/internal/config"
)

// ErrInvalidSession covers missing, expired, tampered or foreign tokens.
var ErrInvalidSession = errors.New("invalid session")

type ctxKey struct{}

// Identity is the authenticated administrator of a request.
type Identity struct {
	Username string `json:"user"`
	Role     string `json:"role"`
}

// AdminStore authenticates administrators stored in the database (the
// bootstrap owner from .env is handled here, without the store).
type AdminStore interface {
	Authenticate(ctx context.Context, username, password string) (role string, ok bool, err error)
	RoleOf(ctx context.Context, username string) (string, error)
}

// Authenticator validates admin credentials and manages session tokens.
type Authenticator struct {
	bootstrapUser string
	userHash      [32]byte
	passHash      [32]byte
	secret        []byte
	ttl           time.Duration
	now           func() time.Time
	admins        AdminStore
}

// New builds an Authenticator from the configured bootstrap credentials.
// admins may be nil when only the bootstrap owner exists.
func New(user, password, secret string, ttl time.Duration, admins AdminStore) *Authenticator {
	return &Authenticator{
		bootstrapUser: user,
		userHash:      sha256.Sum256([]byte(user)),
		passHash:      sha256.Sum256([]byte(password)),
		secret:        []byte(secret),
		ttl:           ttl,
		now:           time.Now,
		admins:        admins,
	}
}

// IsBootstrap reports whether username is the .env owner.
func (a *Authenticator) IsBootstrap(username string) bool {
	return username == a.bootstrapUser
}

// Login checks the bootstrap owner first, then stored administrators.
func (a *Authenticator) Login(ctx context.Context, username, password string) (Identity, bool, error) {
	if a.CheckCredentials(username, password) {
		return Identity{Username: username, Role: config.RoleOwner}, true, nil
	}
	if a.admins == nil || a.IsBootstrap(username) {
		return Identity{}, false, nil
	}
	role, ok, err := a.admins.Authenticate(ctx, username, password)
	return Identity{Username: username, Role: role}, ok, err
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
// administrator takes effect immediately.
func (a *Authenticator) Issue(subject string) (string, time.Time, error) {
	now := a.now()
	exp := now.Add(a.ttl)
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
func SetSessionCookie(w http.ResponseWriter, token string, expires time.Time, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     config.SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

// ClearSessionCookie expires the session cookie.
func ClearSessionCookie(w http.ResponseWriter, secure bool) {
	SetSessionCookie(w, "", time.Unix(0, 0), secure)
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
	if a.IsBootstrap(subject) {
		return Identity{Username: subject, Role: config.RoleOwner}, nil
	}
	if a.admins == nil {
		return Identity{}, ErrInvalidSession
	}
	role, err := a.admins.RoleOf(r.Context(), subject)
	if err != nil {
		return Identity{}, ErrInvalidSession
	}
	return Identity{Username: subject, Role: role}, nil
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

// Allows reports whether role is at least min.
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
