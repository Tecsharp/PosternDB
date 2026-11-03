package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tecsharp/posterndb/internal/config"
)

const secret = "0123456789abcdef0123456789abcdef"

func newAuth() *Authenticator {
	return New("admin", "correct horse battery", secret, time.Hour, nil, nil)
}

func TestCheckCredentials(t *testing.T) {
	a := newAuth()
	assert.True(t, a.CheckCredentials("admin", "correct horse battery"))
	assert.False(t, a.CheckCredentials("admin", "wrong"))
	assert.False(t, a.CheckCredentials("root", "correct horse battery"))
	assert.False(t, a.CheckCredentials("", ""))
}

func TestIssueAndVerify(t *testing.T) {
	a := newAuth()
	token, exp, err := a.Issue("admin")
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(time.Hour), exp, 5*time.Second)

	sub, err := a.Verify(token)
	require.NoError(t, err)
	assert.Equal(t, "admin", sub)
}

func TestVerifyRejectsExpired(t *testing.T) {
	a := newAuth()
	a.now = func() time.Time { return time.Now().Add(-2 * time.Hour) }
	token, _, err := a.Issue("admin")
	require.NoError(t, err)
	a.now = time.Now
	_, err = a.Verify(token)
	assert.ErrorIs(t, err, ErrInvalidSession)
}

func TestVerifyRejectsForeignSignature(t *testing.T) {
	other := New("admin", "x", strings.Repeat("z", 32), time.Hour, nil, nil)
	token, _, _ := other.Issue("admin")
	_, err := newAuth().Verify(token)
	assert.ErrorIs(t, err, ErrInvalidSession)
}

func TestVerifyRejectsAlgNone(t *testing.T) {
	claims := jwt.RegisteredClaims{
		Issuer: config.JWTIssuer, Subject: "admin",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)
	_, err = newAuth().Verify(token)
	assert.ErrorIs(t, err, ErrInvalidSession)
}

func TestVerifyRejectsWrongIssuerAndGarbage(t *testing.T) {
	claims := jwt.RegisteredClaims{Issuer: "evil", Subject: "admin", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	_, err := newAuth().Verify(token)
	assert.ErrorIs(t, err, ErrInvalidSession)

	_, err = newAuth().Verify("not-a-jwt")
	assert.ErrorIs(t, err, ErrInvalidSession)
}

func protected(a *Authenticator) http.Handler {
	return a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(Subject(r.Context())))
	}))
}

func TestMiddleware(t *testing.T) {
	a := newAuth()
	h := protected(a)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "no cookie")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: config.SessionCookieName, Value: "bad"})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "bad cookie")

	token, _, _ := a.Issue("admin")
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: config.SessionCookieName, Value: token})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "admin", rec.Body.String())
}

func TestSessionCookieAttributes(t *testing.T) {
	rec := httptest.NewRecorder()
	SetSessionCookie(rec, "tok", time.Now().Add(time.Hour), true, true)
	c := rec.Result().Cookies()[0]
	assert.True(t, c.HttpOnly)
	assert.True(t, c.Secure)
	assert.Equal(t, http.SameSiteStrictMode, c.SameSite)

	rec = httptest.NewRecorder()
	ClearSessionCookie(rec, false)
	c = rec.Result().Cookies()[0]
	assert.Empty(t, c.Value)
	assert.True(t, c.Expires.Before(time.Now()))
}

func TestRequireCSRFHeader(t *testing.T) {
	h := RequireCSRFHeader(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))

	for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(m, "/", nil))
		assert.Equal(t, http.StatusOK, rec.Code, m)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	assert.Equal(t, http.StatusForbidden, rec.Code)

	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	req.Header.Set(config.HeaderRequestedWith, config.RequestedWithValue)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestSubjectWithoutValue(t *testing.T) {
	assert.Empty(t, Subject(httptest.NewRequest(http.MethodGet, "/", nil).Context()))
}

func TestMACSignsAndRejectsTampering(t *testing.T) {
	a := New("root", "p", secret, time.Hour, nil, nil)
	mac := a.MAC("state.nonce.1")
	assert.True(t, a.CheckMAC("state.nonce.1", mac))
	assert.False(t, a.CheckMAC("state.nonce.2", mac), "changed data")
	assert.False(t, a.CheckMAC("state.nonce.1", mac+"x"), "changed signature")
	assert.False(t, a.CheckMAC("state.nonce.1", ""), "no signature")

	other := New("root", "p", strings.Repeat("z", 40), time.Hour, nil, nil)
	assert.False(t, other.CheckMAC("state.nonce.1", mac), "another secret signs differently")
}

func TestSessionTTLRememberMeIsLongerAndCookieIsPersistentOnlyThen(t *testing.T) {
	a := New("root", "p", secret, time.Hour, nil, nil)
	assert.Equal(t, time.Hour, a.SessionTTL(false))
	assert.Equal(t, config.RememberTTL, a.SessionTTL(true))
	assert.Greater(t, config.RememberTTL, 24*time.Hour, "it has to survive days, not hours")

	rec := httptest.NewRecorder()
	SetSessionCookie(rec, "tok", time.Now().Add(time.Hour), false, true)
	assert.True(t, rec.Result().Cookies()[0].Expires.IsZero(), "ends with the browser")
	rec = httptest.NewRecorder()
	SetSessionCookie(rec, "tok", time.Now().Add(time.Hour), true, true)
	assert.False(t, rec.Result().Cookies()[0].Expires.IsZero(), "persists")
}

func TestClientPasswordLoginCanBeSwitchedOff(t *testing.T) {
	clients := fakeClients{users: map[string]fakeClientUser{"bea": {id: 7, password: "bea-password-123"}}}
	a := New("root", "p", secret, time.Hour, nil, clients)
	_, ok, err := a.Login(context.Background(), "bea", "bea-password-123")
	require.NoError(t, err)
	assert.True(t, ok)

	a.SetClientPasswordLogin(false)
	_, ok, err = a.Login(context.Background(), "bea", "bea-password-123")
	require.NoError(t, err)
	assert.False(t, ok, "AUTH_MODE=google: registered users have no password login")
	_, ok, _ = a.Login(context.Background(), "root", "p")
	assert.True(t, ok, "the owner always can")
}
