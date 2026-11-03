package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/model"
	"github.com/tecsharp/posterndb/internal/oauth"
)

type fakeGoogle struct {
	profile oauth.Profile
	err     error

	code, verifier, nonce string
}

func (f *fakeGoogle) AuthURL(state, nonce, verifier string) string {
	return "https://accounts.example/auth?" + url.Values{"state": {state}, "nonce": {nonce}, "challenge": {verifier}}.Encode()
}

func (f *fakeGoogle) Exchange(_ context.Context, code, verifier, nonce string) (oauth.Profile, error) {
	f.code, f.verifier, f.nonce = code, verifier, nonce
	return f.profile, f.err
}

func ana() oauth.Profile {
	return oauth.Profile{Subject: "g-ana", Email: "ana@example.com", Name: "Ana López", EmailVerified: true}
}

func get(t *testing.T, e env, path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Header.Set(config.HeaderRequestedWith, config.RequestedWithValue)
	for _, c := range cookies {
		r.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, r)
	return rec
}

func cookieNamed(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// start runs the first leg and returns the oauth cookie plus the state the
// browser would carry back from Google.
func startGoogle(t *testing.T, e env) (*http.Cookie, string) {
	t.Helper()
	rec := get(t, e, "/api/auth/google/start")
	require.Equal(t, http.StatusFound, rec.Code)
	loc, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "accounts.example", loc.Host)
	c := cookieNamed(rec, config.OAuthCookieName)
	require.NotNil(t, c, "state, nonce and verifier travel in a cookie")
	return c, loc.Query().Get("state")
}

func TestGoogleIsOffUnlessConfigured(t *testing.T) {
	e := newEnv(t, nil)
	assert.Equal(t, http.StatusNotFound, get(t, e, "/api/auth/google/start").Code)
	assert.Equal(t, http.StatusNotFound, get(t, e, "/api/auth/google/callback?code=x&state=y").Code)
	assert.Equal(t, false, decode[map[string]any](t, get(t, e, "/api/auth/config"))["googleEnabled"])

	on := newEnvWith(t, nil, &fakeGoogle{})
	assert.Equal(t, true, decode[map[string]any](t, get(t, on, "/api/auth/config"))["googleEnabled"])
}

func TestGoogleCookieIsHardened(t *testing.T) {
	e := newEnvWith(t, nil, &fakeGoogle{})
	c, _ := startGoogle(t, e)
	assert.True(t, c.HttpOnly, "scripts must not read the state")
	assert.Equal(t, http.SameSiteLaxMode, c.SameSite, "it has to survive the redirect back from Google")
	assert.Equal(t, config.OAuthCookiePath, c.Path)
	assert.Positive(t, c.MaxAge)
}

func TestGoogleLoginCreatesAClientSession(t *testing.T) {
	g := &fakeGoogle{profile: ana()}
	e := newEnvWith(t, nil, g)
	c, state := startGoogle(t, e)
	parts := strings.Split(c.Value, ".")
	require.Len(t, parts, 7, "state.nonce.verifier.mode.user.remember.signature")
	assert.Equal(t, "login", parts[3])

	rec := get(t, e, "/api/auth/google/callback?code=abc&state="+url.QueryEscape(state), c)
	require.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "/", rec.Header().Get("Location"))
	assert.Equal(t, "abc", g.code)
	assert.Equal(t, parts[2], g.verifier, "the PKCE verifier from the cookie is what gets redeemed")
	assert.Equal(t, parts[1], g.nonce)
	cleared := cookieNamed(rec, config.OAuthCookieName)
	require.NotNil(t, cleared)
	assert.Negative(t, cleared.MaxAge, "the oauth cookie is single use")

	session := cookieNamed(rec, config.SessionCookieName)
	require.NotNil(t, session)
	out := get(t, e, "/api/auth/session", session)
	require.Equal(t, http.StatusOK, out.Code)
	body := decode[map[string]any](t, out)
	assert.Equal(t, config.RoleClient, body["role"])
	assert.Equal(t, "ana@example.com", body["email"])
	assert.Equal(t, "Ana López", body["fullName"])
	assert.Equal(t, false, body["hasPassword"], "a Google-only account has no password yet")

	assert.Equal(t, http.StatusForbidden, get(t, e, "/api/admins", session).Code, "Google never opens the admin panel")
	assert.Equal(t, http.StatusOK, get(t, e, "/api/me/databases", session).Code)
}

// Owning the email is not enough to enter an account someone registered with a
// password: it has to be linked from inside, in Seguridad.
func TestGoogleDoesNotEnterAnExistingPasswordAccount(t *testing.T) {
	g := &fakeGoogle{profile: ana()}
	e := newEnvWith(t, nil, g)
	rec := e.do(t, http.MethodPost, "/api/auth/register", req{body: registerBody("ana@example.com", "Ana López")})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	c, state := startGoogle(t, e)
	out := get(t, e, "/api/auth/google/callback?code=abc&state="+url.QueryEscape(state), c)
	assert.Equal(t, "/?oauth=notlinked", out.Header().Get("Location"))
	assert.Nil(t, cookieNamed(out, config.SessionCookieName), "no session for an unlinked account")

	assert.Equal(t, http.StatusOK, e.do(t, http.MethodPost, "/api/auth/login",
		req{body: `{"username":"ana@example.com","password":"a-long-enough-password"}`}).Code,
		"the owner's password still works, nothing was taken over")
}

func TestGoogleCallbackRejectsBadRequests(t *testing.T) {
	cases := map[string]struct {
		profile oauth.Profile
		err     error
		query   func(state string) string
		cookie  bool
		want    string
	}{
		"wrong state":      {ana(), nil, func(string) string { return "code=abc&state=forged" }, true, "failed"},
		"no cookie":        {ana(), nil, func(s string) string { return "code=abc&state=" + s }, false, "failed"},
		"no code":          {ana(), nil, func(s string) string { return "state=" + s }, true, "failed"},
		"user denied":      {ana(), nil, func(s string) string { return "error=access_denied&state=" + s }, true, "cancelled"},
		"exchange fails":   {ana(), errors.New("boom"), func(s string) string { return "code=abc&state=" + s }, true, "failed"},
		"unverified email": {oauth.Profile{Subject: "g", Email: "ana@example.com", EmailVerified: false}, nil, func(s string) string { return "code=abc&state=" + s }, true, "unverified"},
	}
	for name, c := range cases {
		g := &fakeGoogle{profile: c.profile, err: c.err}
		e := newEnvWith(t, nil, g)
		cookie, state := startGoogle(t, e)
		var cookies []*http.Cookie
		if c.cookie {
			cookies = append(cookies, cookie)
		}
		rec := get(t, e, "/api/auth/google/callback?"+c.query(state), cookies...)
		assert.Equal(t, http.StatusFound, rec.Code, name)
		assert.Equal(t, "/?oauth="+c.want, rec.Header().Get("Location"), name)
		assert.Nil(t, cookieNamed(rec, config.SessionCookieName), "%s must not start a session", name)
	}
}

func registerBody(email, name string) string {
	return `{"email":"` + email + `","fullName":"` + name + `","password":"a-long-enough-password","acceptEmailUse":true}`
}

func TestRegisterWithEmailNameAndPassword(t *testing.T) {
	e := newEnv(t, nil)
	rec := e.do(t, http.MethodPost, "/api/auth/register", req{body: registerBody("Ana@Example.com", "Ana López")})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	session := cookieNamed(rec, config.SessionCookieName)
	require.NotNil(t, session)

	body := decode[map[string]any](t, get(t, e, "/api/auth/session", session))
	assert.Equal(t, "ana@example.com", body["user"])
	assert.Equal(t, "Ana López", body["fullName"])
	assert.Equal(t, true, body["hasPassword"])

	// Logging in later works with the email and the password.
	assert.Equal(t, http.StatusOK, e.do(t, http.MethodPost, "/api/auth/login",
		req{body: `{"username":"ana@example.com","password":"a-long-enough-password"}`}).Code)

	var stored *model.ClientUser
	for _, u := range e.meta.ClientUsers {
		stored = u
	}
	require.NotNil(t, stored)
	assert.Nil(t, stored.EmailVerifiedAt)
}

func TestRegisterRejectsIncompleteForms(t *testing.T) {
	e := newEnv(t, nil)
	n := 0
	ip := func() string { n++; return "203.0.113." + itoa(int64(n)) } // the sign-up form is rate limited per IP
	for name, body := range map[string]string{
		"no consent":     `{"email":"a@example.com","fullName":"Ana López","password":"a-long-enough-password"}`,
		"bad email":      registerBody("nope", "Ana López"),
		"no name":        registerBody("a@example.com", " "),
		"short password": `{"email":"a@example.com","fullName":"Ana López","password":"short","acceptEmailUse":true}`,
		"old shape":      `{"username":"ana","password":"a-long-enough-password"}`,
	} {
		assert.Equal(t, http.StatusBadRequest, e.do(t, http.MethodPost, "/api/auth/register", req{body: body, ip: ip()}).Code, name)
	}
	assert.Empty(t, e.meta.ClientUsers)

	require.Equal(t, http.StatusCreated, e.do(t, http.MethodPost, "/api/auth/register", req{body: registerBody("a@example.com", "Ana López"), ip: ip()}).Code)
	assert.Equal(t, http.StatusConflict, e.do(t, http.MethodPost, "/api/auth/register", req{body: registerBody("A@example.com", "Otra Ana"), ip: ip()}).Code)
}

func TestGoogleOnlyAccountSetsPasswordWithoutTheCurrentOne(t *testing.T) {
	g := &fakeGoogle{profile: ana()}
	e := newEnvWith(t, nil, g)
	c, state := startGoogle(t, e)
	session := cookieNamed(get(t, e, "/api/auth/google/callback?code=abc&state="+url.QueryEscape(state), c), config.SessionCookieName)
	require.NotNil(t, session)

	r := httptest.NewRequest(http.MethodPost, "/api/me/password", strings.NewReader(`{"current":"","next":"my brand new password"}`))
	r.Header.Set(config.HeaderRequestedWith, config.RequestedWithValue)
	r.Header.Set(config.HeaderContentType, config.MimeJSON)
	r.AddCookie(session)
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, r)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	assert.Equal(t, http.StatusOK, e.do(t, http.MethodPost, "/api/auth/login",
		req{body: `{"username":"ana@example.com","password":"my brand new password"}`}).Code)
}
