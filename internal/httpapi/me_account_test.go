package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/model"
	"github.com/tecsharp/posterndb/internal/oauth"
)

// signUp registers a user through the public form and returns their session.
func (e env) signUp(t *testing.T, email, name, ip string) *http.Cookie {
	t.Helper()
	rec := e.do(t, http.MethodPost, "/api/auth/register", req{body: registerBody(email, name), ip: ip})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	c := cookieNamed(rec, config.SessionCookieName)
	require.NotNil(t, c)
	return c
}

// slugOf is the full name of a user's database: their personal code, a dash
// and the name they typed.
func (e env) slugOf(t *testing.T, email, name string) string {
	t.Helper()
	for _, u := range e.meta.ClientUsers {
		if u.Email == email {
			return u.Code + "-" + name
		}
	}
	t.Fatalf("no user %s", email)
	return ""
}

func send(t *testing.T, e env, method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set(config.HeaderContentType, config.MimeJSON)
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	r.Header.Set(config.HeaderRequestedWith, config.RequestedWithValue)
	for _, c := range cookies {
		r.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, r)
	return rec
}

// ---- linking Google from Seguridad -------------------------------------------

func TestLinkingGoogleFromSecurity(t *testing.T) {
	g := &fakeGoogle{profile: ana()}
	e := newEnvWith(t, nil, g)
	session := e.signUp(t, "ana@example.com", "Ana López", "203.0.113.1")

	start := send(t, e, http.MethodGet, "/api/me/google/link", "", session)
	require.Equal(t, http.StatusFound, start.Code)
	loc, err := url.Parse(start.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "accounts.example", loc.Host)
	oauthCk := cookieNamed(start, config.OAuthCookieName)
	require.NotNil(t, oauthCk)

	out := get(t, e, "/api/auth/google/callback?code=abc&state="+url.QueryEscape(loc.Query().Get("state")), oauthCk)
	assert.Equal(t, "/security?google=linked", out.Header().Get("Location"))

	body := decode[map[string]any](t, send(t, e, http.MethodGet, "/api/auth/session", "", session))
	assert.Equal(t, true, body["googleLinked"])
	assert.Equal(t, true, body["hasPassword"], "linking keeps the password")

	// From now on Google signs them in.
	c, state := startGoogle(t, e)
	in := get(t, e, "/api/auth/google/callback?code=abc&state="+url.QueryEscape(state), c)
	assert.Equal(t, "/", in.Header().Get("Location"))
	assert.NotNil(t, cookieNamed(in, config.SessionCookieName))
}

func TestLinkingGoogleNeedsAClientSession(t *testing.T) {
	e := newEnvWith(t, nil, &fakeGoogle{profile: ana()})
	assert.Equal(t, http.StatusUnauthorized, get(t, e, "/api/me/google/link").Code)
	assert.Equal(t, http.StatusForbidden, e.do(t, http.MethodGet, "/api/me/google/link", req{as: adminUser}).Code, "admins do not link Google")

	off := newEnv(t, nil)
	session := off.signUp(t, "ana@example.com", "Ana López", "203.0.113.1")
	assert.Equal(t, http.StatusNotFound, send(t, off, http.MethodGet, "/api/me/google/link", "", session).Code, "nothing exists when Google is off")
}

func TestLinkCookieCannotBeForgedToLinkSomeoneElse(t *testing.T) {
	g := &fakeGoogle{profile: ana()}
	e := newEnvWith(t, nil, g)
	attacker := e.signUp(t, "attacker@example.com", "Mala Persona", "203.0.113.1")
	victim := e.signUp(t, "victim@example.com", "Víctima", "203.0.113.2")
	_ = victim

	start := send(t, e, http.MethodGet, "/api/me/google/link", "", attacker)
	ck := cookieNamed(start, config.OAuthCookieName)
	require.NotNil(t, ck)
	state := strings.Split(ck.Value, ".")[0]

	// Point the (signed) cookie at another user id without re-signing it.
	parts := strings.Split(ck.Value, ".")
	parts[4] = "2"
	forged := &http.Cookie{Name: ck.Name, Value: strings.Join(parts, ".")}
	out := get(t, e, "/api/auth/google/callback?code=abc&state="+url.QueryEscape(state), forged)
	assert.Equal(t, "/?oauth=failed", out.Header().Get("Location"), "a tampered cookie is rejected")
	for _, u := range e.meta.ClientUsers {
		assert.False(t, u.GoogleLinked, "nobody got linked: %s", u.Username)
	}
}

func TestLinkingAGoogleAccountAlreadyUsedByAnotherUser(t *testing.T) {
	g := &fakeGoogle{profile: ana()}
	e := newEnvWith(t, nil, g)
	first := e.signUp(t, "ana@example.com", "Ana López", "203.0.113.1")
	second := e.signUp(t, "bea@example.com", "Bea Ruiz", "203.0.113.2")

	link := func(session *http.Cookie) *httptest.ResponseRecorder {
		start := send(t, e, http.MethodGet, "/api/me/google/link", "", session)
		ck := cookieNamed(start, config.OAuthCookieName)
		require.NotNil(t, ck)
		return get(t, e, "/api/auth/google/callback?code=abc&state="+url.QueryEscape(strings.Split(ck.Value, ".")[0]), ck)
	}
	assert.Equal(t, "/security?google=linked", link(first).Header().Get("Location"))
	assert.Equal(t, "/security?google=taken", link(second).Header().Get("Location"))
}

// ---- account ---------------------------------------------------------------------

func TestMyAccountAndUsernameChoice(t *testing.T) {
	e := newEnv(t, nil)
	session := e.signUp(t, "ana@example.com", "Ana López", "203.0.113.1")

	acct := decode[map[string]any](t, send(t, e, http.MethodGet, "/api/me/account", "", session))
	assert.Equal(t, "ana@example.com", acct["email"])
	assert.Equal(t, "Ana López", acct["fullName"])
	assert.EqualValues(t, 3, acct["maxDatabases"])
	assert.EqualValues(t, 0, acct["databaseCount"])

	assert.Equal(t, http.StatusBadRequest, send(t, e, http.MethodPatch, "/api/me/account", `{}`, session).Code, "nothing to change")
	assert.Equal(t, http.StatusBadRequest, send(t, e, http.MethodPatch, "/api/me/account", `{"username":"a b"}`, session).Code)

	rec := send(t, e, http.MethodPatch, "/api/me/account", `{"username":"Ana.Lopez","fullName":"Ana María López"}`, session)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	updated := decode[map[string]any](t, rec)
	assert.Equal(t, "ana.lopez", updated["username"])
	assert.Equal(t, "Ana María López", updated["fullName"])
	assert.Equal(t, "ana@example.com", updated["email"], "the email never goes away")

	// The same session keeps working (it is bound to the id, not the name) and
	// the new username signs in alongside the email.
	assert.Equal(t, http.StatusOK, send(t, e, http.MethodGet, "/api/me/account", "", session).Code)
	for i, login := range []string{"ana.lopez", "ana@example.com"} {
		rec := e.do(t, http.MethodPost, "/api/auth/login", req{body: `{"username":"` + login + `","password":"a-long-enough-password"}`, ip: "198.51.100." + itoa(int64(i+1))})
		assert.Equal(t, http.StatusOK, rec.Code, login)
	}
}

func TestUsernameCannotBeAnAdministratorsOrTaken(t *testing.T) {
	e := newEnv(t, nil)
	a := e.signUp(t, "ana@example.com", "Ana López", "203.0.113.1")
	b := e.signUp(t, "bea@example.com", "Bea Ruiz", "203.0.113.2")
	require.Equal(t, http.StatusOK, send(t, e, http.MethodPatch, "/api/me/account", `{"username":"ana.lopez"}`, a).Code)

	assert.Equal(t, http.StatusConflict, send(t, e, http.MethodPatch, "/api/me/account", `{"username":"ana.lopez"}`, b).Code, "already taken")
}

func TestAccountEndpointsAreForClientsOnly(t *testing.T) {
	e := newEnv(t, nil)
	assert.Equal(t, http.StatusUnauthorized, send(t, e, http.MethodGet, "/api/me/account", "").Code)
	assert.Equal(t, http.StatusForbidden, e.do(t, http.MethodGet, "/api/me/account", req{as: adminUser}).Code)
	assert.Equal(t, http.StatusForbidden, e.do(t, http.MethodGet, "/api/me/backups", req{as: operator}).Code)
	assert.Equal(t, http.StatusForbidden, e.do(t, http.MethodGet, "/api/me/activity", req{as: viewer}).Code)
}

// ---- remember me and AUTH_MODE -------------------------------------------------

func TestRememberMeKeepsTheSessionForDays(t *testing.T) {
	e := newEnv(t, nil)
	login := func(remember string, ip string) *http.Cookie {
		rec := e.do(t, http.MethodPost, "/api/auth/login", req{body: `{"username":"admin","password":"correct horse battery","remember":` + remember + `}`, ip: ip})
		require.Equal(t, http.StatusOK, rec.Code)
		return cookieNamed(rec, config.SessionCookieName)
	}
	short := login("false", "203.0.113.1")
	assert.True(t, short.Expires.IsZero(), "without Recordarme it ends when the browser closes")

	long := login("true", "203.0.113.2")
	assert.False(t, long.Expires.IsZero())
	assert.WithinDuration(t, time.Now().Add(config.RememberTTL), long.Expires, time.Minute, "Recordarme keeps it open for weeks")
}

func TestRememberedGoogleSession(t *testing.T) {
	g := &fakeGoogle{profile: ana()}
	e := newEnvWith(t, nil, g)
	start := get(t, e, "/api/auth/google/start?remember=1")
	ck := cookieNamed(start, config.OAuthCookieName)
	require.NotNil(t, ck)
	out := get(t, e, "/api/auth/google/callback?code=abc&state="+url.QueryEscape(strings.Split(ck.Value, ".")[0]), ck)
	session := cookieNamed(out, config.SessionCookieName)
	require.NotNil(t, session)
	assert.WithinDuration(t, time.Now().Add(config.RememberTTL), session.Expires, time.Minute)
}

func TestAuthModeGoogleOnlyBlocksClientPasswords(t *testing.T) {
	newcomer := oauth.Profile{Subject: "g-new", Email: "nueva@example.com", Name: "Nueva Persona", EmailVerified: true}
	e := newEnvFull(t, nil, &fakeGoogle{profile: newcomer}, config.AuthModeGoogle)
	e.addClient(t, "ana")
	cfg := decode[map[string]any](t, get(t, e, "/api/auth/config"))
	assert.Equal(t, false, cfg["passwordEnabled"])
	assert.Equal(t, true, cfg["googleEnabled"])

	assert.Equal(t, http.StatusBadRequest, e.do(t, http.MethodPost, "/api/auth/register", req{body: registerBody("x@example.com", "Equis Uno")}).Code, "no password sign-up")
	assert.Equal(t, http.StatusUnauthorized, e.do(t, http.MethodPost, "/api/auth/login",
		req{body: `{"username":"ana@example.com","password":"anything-at-all-1"}`}).Code, "registered users cannot use a password")
	assert.Equal(t, http.StatusOK, e.do(t, http.MethodPost, "/api/auth/login",
		req{body: `{"username":"admin","password":"correct horse battery"}`, ip: "203.0.113.9"}).Code, "administrators always can")

	c, state := startGoogle(t, e)
	assert.Equal(t, "/", get(t, e, "/api/auth/google/callback?code=abc&state="+url.QueryEscape(state), c).Header().Get("Location"), "Google works")
}

func TestAuthModePasswordHasNoGoogle(t *testing.T) {
	e := newEnvFull(t, nil, nil, config.AuthModePassword)
	cfg := decode[map[string]any](t, get(t, e, "/api/auth/config"))
	assert.Equal(t, true, cfg["passwordEnabled"])
	assert.Equal(t, false, cfg["googleEnabled"])
	assert.Equal(t, http.StatusNotFound, get(t, e, "/api/auth/google/start").Code)
	e.signUp(t, "ana@example.com", "Ana López", "203.0.113.1")
}

// ---- backups --------------------------------------------------------------------

func (e env) markBackupsDone() {
	for i := range e.meta.Jobs {
		if e.meta.Jobs[i].Kind == config.JobDump {
			e.meta.Jobs[i].Status = config.JobDone
		}
	}
}

func TestClientBackupsAreScopedToTheirOwnDatabases(t *testing.T) {
	e := newEnv(t, nil)
	e.createDB(t, "ajena")
	ana := e.signUp(t, "ana@example.com", "Ana López", "203.0.113.1")
	bea := e.signUp(t, "bea@example.com", "Bea Ruiz", "203.0.113.2")
	require.Equal(t, http.StatusCreated, send(t, e, http.MethodPost, "/api/me/databases", `{"name":"mia"}`, ana).Code)
	require.Equal(t, http.StatusCreated, send(t, e, http.MethodPost, "/api/me/databases", `{"name":"suya"}`, bea).Code)

	mia, suya := e.slugOf(t, "ana@example.com", "mia"), e.slugOf(t, "bea@example.com", "suya")
	assert.Equal(t, http.StatusAccepted, send(t, e, http.MethodPost, "/api/me/databases/"+mia+"/backups", "", ana).Code)
	assert.Equal(t, http.StatusAccepted, send(t, e, http.MethodPost, "/api/me/databases/"+suya+"/backups", "", bea).Code)
	assert.Equal(t, http.StatusNotFound, send(t, e, http.MethodPost, "/api/me/databases/"+suya+"/backups", "", ana).Code, "not another user's database")
	assert.Equal(t, http.StatusNotFound, send(t, e, http.MethodPost, "/api/me/databases/ajena/backups", "", ana).Code, "nor an admin database")
	e.markBackupsDone()

	list := decode[struct{ Backups []model.Job }](t, send(t, e, http.MethodGet, "/api/me/backups", "", ana))
	require.Len(t, list.Backups, 1)
	assert.Equal(t, mia, list.Backups[0].Slug)

	var mine, theirs int64
	for _, j := range e.meta.Jobs {
		if j.Slug == mia {
			mine = j.ID
		}
		if j.Slug == suya {
			theirs = j.ID
		}
	}
	require.NotZero(t, mine)
	require.NotZero(t, theirs)
	for _, call := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/me/backups/" + itoa(theirs) + "/download", ""},
		{http.MethodDelete, "/api/me/backups/" + itoa(theirs), ""},
		{http.MethodPost, "/api/me/backups/" + itoa(theirs) + "/restore", `{"name":"robada"}`},
	} {
		assert.Equal(t, http.StatusNotFound, send(t, e, call.method, call.path, call.body, ana).Code, "%s %s must not reach another user's backup", call.method, call.path)
	}

	assert.Equal(t, http.StatusNoContent, send(t, e, http.MethodDelete, "/api/me/backups/"+itoa(mine), "", ana).Code, "their own can be deleted")
}

func TestClientBackupLimits(t *testing.T) {
	e := newEnv(t, nil)
	ana := e.signUp(t, "ana@example.com", "Ana López", "203.0.113.1")
	require.Equal(t, http.StatusCreated, send(t, e, http.MethodPost, "/api/me/databases", `{"name":"mia"}`, ana).Code)

	mia := e.slugOf(t, "ana@example.com", "mia")
	require.Equal(t, http.StatusAccepted, send(t, e, http.MethodPost, "/api/me/databases/"+mia+"/backups", "", ana).Code)
	assert.Equal(t, http.StatusConflict, send(t, e, http.MethodPost, "/api/me/databases/"+mia+"/backups", "", ana).Code, "one at a time")

	e.markBackupsDone()
	for i := 1; i < config.MaxClientBackupsPerDatabase; i++ {
		require.Equal(t, http.StatusAccepted, send(t, e, http.MethodPost, "/api/me/databases/"+mia+"/backups", "", ana).Code, "backup %d", i+1)
		e.markBackupsDone()
	}
	assert.Equal(t, http.StatusForbidden, send(t, e, http.MethodPost, "/api/me/databases/"+mia+"/backups", "", ana).Code, "capped so nobody fills the disk")
}

// ---- activity -------------------------------------------------------------------

func TestClientActivityShowsOnlyTheirOwnThings(t *testing.T) {
	now := time.Now()
	entries := []model.AuditEntry{
		{ID: 1, OccurredAt: now.Add(-1 * time.Hour), Actor: "ana@example.com", Action: "db_created", Target: "mia", Details: map[string]any{"hostname": "x"}},
		{ID: 2, OccurredAt: now.Add(-2 * time.Hour), Actor: "root", Action: "expiry_changed", Target: "mia", Details: map[string]any{"secret": "no"}},
		{ID: 3, OccurredAt: now.Add(-3 * time.Hour), Actor: config.ActorAuditor, Action: "db_expired", Target: "mia"},
		{ID: 4, OccurredAt: now.Add(-4 * time.Hour), Actor: "bea@example.com", Action: "db_created", Target: "suya"},
		{ID: 5, OccurredAt: now.Add(-5 * time.Hour), Actor: "root", Action: "client_password_reset", Target: "ana@example.com"},
		{ID: 6, OccurredAt: now.Add(-6 * time.Hour), Actor: config.ActorSystem, Action: "login_failed", Target: ""},
		{ID: 7, OccurredAt: now.Add(-7 * time.Hour), Actor: "root", Action: "db_deleted", Target: "ajena"},
	}
	got := personActivity(entries, []string{"ana@example.com", "ana.lopez"}, []string{"mia"}, true)

	var ids []int64
	for _, e := range got {
		ids = append(ids, e.ID)
	}
	assert.Equal(t, []int64{1, 2, 3, 5}, ids, "their actions, plus what happened to their account and databases")

	byID := map[int64]model.AuditEntry{}
	for _, e := range got {
		byID[e.ID] = e
	}
	assert.Equal(t, "ana@example.com", byID[1].Actor)
	assert.NotNil(t, byID[1].Details, "details of their own actions are kept")
	assert.Equal(t, "administrador", byID[2].Actor, "an administrator's name is not revealed")
	assert.Nil(t, byID[2].Details, "nor what they typed")
	assert.Equal(t, config.ActorAuditor, byID[3].Actor, "the automatic audit keeps its label")
}

func TestClientActivityEndpoint(t *testing.T) {
	e := newEnv(t, nil)
	ana := e.signUp(t, "ana@example.com", "Ana López", "203.0.113.1")
	e.createDB(t, "ajena")
	require.Equal(t, http.StatusCreated, send(t, e, http.MethodPost, "/api/me/databases", `{"name":"mia"}`, ana).Code)

	list := decode[struct{ Entries []model.AuditEntry }](t, send(t, e, http.MethodGet, "/api/me/activity", "", ana))
	require.NotEmpty(t, list.Entries)
	for _, entry := range list.Entries {
		assert.NotEqual(t, "ajena", entry.Target, "another database's activity never shows")
	}
}

// ---- database names carry the user's code ---------------------------------------

func TestClientDatabaseNameStartsWithTheUsersCode(t *testing.T) {
	e := newEnv(t, nil)
	ana := e.signUp(t, "ana@example.com", "Ana López", "203.0.113.1")
	code := e.slugOf(t, "ana@example.com", "")

	rec := send(t, e, http.MethodPost, "/api/me/databases", `{"name":"Tienda"}`, ana)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	creds := decode[model.Credentials](t, rec)
	assert.Equal(t, "pdb_"+strings.TrimSuffix(strings.ReplaceAll(code, "-", "_"), "_")+"_tienda", creds.Database, "the database and role carry the code too")
	assert.Equal(t, "postern-"+code+"tienda.pachislab.com", creds.Hostname, "and so does the public host")

	list := decode[struct{ Databases []provisionView }](t, send(t, e, http.MethodGet, "/api/me/databases", "", ana))
	require.Len(t, list.Databases, 1)
	assert.Equal(t, code+"tienda", list.Databases[0].Slug)
}

type provisionView struct {
	Slug string `json:"slug"`
}

func TestTwoUsersCanUseTheSameDatabaseName(t *testing.T) {
	e := newEnv(t, nil)
	ana := e.signUp(t, "ana@example.com", "Ana López", "203.0.113.1")
	bea := e.signUp(t, "bea@example.com", "Bea Ruiz", "203.0.113.2")

	a := send(t, e, http.MethodPost, "/api/me/databases", `{"name":"triburide"}`, ana)
	b := send(t, e, http.MethodPost, "/api/me/databases", `{"name":"triburide"}`, bea)
	require.Equal(t, http.StatusCreated, a.Code, a.Body.String())
	require.Equal(t, http.StatusCreated, b.Code, b.Body.String())
	assert.NotEqual(t, decode[model.Credentials](t, a).Hostname, decode[model.Credentials](t, b).Hostname, "same name, different public address")

	assert.Equal(t, http.StatusConflict, send(t, e, http.MethodPost, "/api/me/databases", `{"name":"triburide"}`, ana).Code, "but not twice for the same person")
}

func TestClientDatabaseNameRules(t *testing.T) {
	e := newEnv(t, nil)
	ana := e.signUp(t, "ana@example.com", "Ana López", "203.0.113.1")
	e.createDB(t, "triburide") // an admin database with the bare name

	for name, body := range map[string]string{
		"too short":      `{"name":"ab"}`,
		"bad characters": `{"name":"mi tienda"}`,
		"starts digit":   `{"name":"1tienda"}`,
		"too long":       `{"name":"` + strings.Repeat("a", 40) + `"}`,
	} {
		rec := send(t, e, http.MethodPost, "/api/me/databases", body, ana)
		assert.Equal(t, http.StatusBadRequest, rec.Code, name)
	}
	long := send(t, e, http.MethodPost, "/api/me/databases", `{"name":"`+strings.Repeat("a", 40)+`"}`, ana)
	assert.Contains(t, long.Body.String(), "tu código", "the message explains the code takes room")

	assert.Equal(t, http.StatusCreated, send(t, e, http.MethodPost, "/api/me/databases", `{"name":"triburide"}`, ana).Code,
		"a bare admin name never blocks a user's prefixed one")
	longest := strings.Repeat("a", 33)
	assert.Equal(t, http.StatusCreated, send(t, e, http.MethodPost, "/api/me/databases", `{"name":"`+longest+`"}`, ana).Code, "the longest name that still fits")
}

func TestAccountShowsTheCodeAndWhereDatabasesLive(t *testing.T) {
	e := newEnv(t, nil)
	ana := e.signUp(t, "ana@example.com", "Ana López", "203.0.113.1")
	acct := decode[map[string]any](t, send(t, e, http.MethodGet, "/api/me/account", "", ana))
	assert.Equal(t, strings.TrimSuffix(e.slugOf(t, "ana@example.com", ""), "-"), acct["code"])
	assert.Len(t, acct["code"], 6)
	assert.Equal(t, "postern-", acct["hostPrefix"])
	assert.Equal(t, "pachislab.com", acct["baseDomain"])
	assert.Equal(t, false, acct["localMode"])
}

func TestRestoringABackupAlsoUsesTheCode(t *testing.T) {
	e := newEnv(t, nil)
	ana := e.signUp(t, "ana@example.com", "Ana López", "203.0.113.1")
	require.Equal(t, http.StatusCreated, send(t, e, http.MethodPost, "/api/me/databases", `{"name":"mia"}`, ana).Code)
	mia := e.slugOf(t, "ana@example.com", "mia")
	require.Equal(t, http.StatusAccepted, send(t, e, http.MethodPost, "/api/me/databases/"+mia+"/backups", "", ana).Code)
	e.markBackupsDone()
	var id int64
	for _, j := range e.meta.Jobs {
		if j.Slug == mia {
			id = j.ID
		}
	}
	require.NotZero(t, id)
	require.NoError(t, os.WriteFile(filepath.Join(e.backups, e.meta.Jobs[0].File), []byte("dump"), 0o600))

	rec := send(t, e, http.MethodPost, "/api/me/backups/"+itoa(id)+"/restore", `{"name":"copia"}`, ana)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, e.slugOf(t, "ana@example.com", "copia"), strings.TrimPrefix(strings.TrimSuffix(decode[model.Credentials](t, rec).Hostname, ".pachislab.com"), "postern-"))
}

func TestPublicConfigTellsTheLoginWhereThePolicyLives(t *testing.T) {
	none := newEnv(t, nil)
	assert.Equal(t, "", decode[map[string]any](t, get(t, none, "/api/auth/config"))["landingUrl"], "no link until LANDING_URL is set")

	e := newEnvFull(t, nil, nil, config.AuthModePassword, func(s *Settings) { s.LandingURL = "https://posterndb.com" })
	assert.Equal(t, "https://posterndb.com", decode[map[string]any](t, get(t, e, "/api/auth/config"))["landingUrl"])
}
