package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tecsharp/posterndb/internal/admins"
	"github.com/tecsharp/posterndb/internal/auth"
	"github.com/tecsharp/posterndb/internal/clients"
	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/expiry"
	"github.com/tecsharp/posterndb/internal/model"
	"github.com/tecsharp/posterndb/internal/naming"
	"github.com/tecsharp/posterndb/internal/provision"
	"github.com/tecsharp/posterndb/internal/testsupport"
)

const (
	adminUser = "admin"
	adminPass = "correct horse battery"
	viewer    = "viewer1"
	operator  = "operator1"
)

type pinger struct{ err error }

func (p pinger) Ping(context.Context) error { return p.err }

type env struct {
	handler  http.Handler
	meta     *testsupport.FakeMetadata
	cluster  *testsupport.FakeCluster
	cf       *testsupport.FakeCloudflareClient
	insp     *testsupport.FakeInspector
	notifier *testsupport.FakeNotifier
	auth     *auth.Authenticator
	backups  string
}

func newEnv(t *testing.T, health error) env {
	t.Helper()
	e := env{
		meta:     testsupport.NewFakeMetadata(),
		cluster:  testsupport.NewFakeCluster(),
		cf:       testsupport.NewFakeCloudflareClient(),
		insp:     testsupport.NewFakeInspector(),
		notifier: &testsupport.FakeNotifier{},
		backups:  t.TempDir(),
	}
	e.meta.Admins[viewer] = &model.Admin{Username: viewer, Role: config.RoleViewer}
	e.meta.Admins[operator] = &model.Admin{Username: operator, Role: config.RoleAdmin}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adminSvc := admins.New(e.meta, testsupport.FakeSecrets{}, adminUser)
	clientSvc := clients.New(e.meta, testsupport.FakeSecrets{}, true)
	e.auth = auth.New(adminUser, adminPass, strings.Repeat("k", 32), time.Hour, adminSvc, clientSvc)
	svc := provision.New(e.meta, e.cluster, e.cf, e.insp, testsupport.FakeSecrets{}, provision.Options{
		Resolver:           naming.Resolver{BaseDomain: "pachislab.com", HostPrefix: "postern-", PanelHostname: "postern.pachislab.com"},
		Policy:             expiry.Policy{MaxIdle: 30 * 24 * time.Hour, WarnBefore: 7 * 24 * time.Hour},
		BackupBeforeDelete: true,
		TunnelDBService:    "tcp://db:5432",
		ClientLocalPort:    5432,
		BackupDir:          e.backups,
		Logger:             logger,
	})
	e.handler = NewRouter(Deps{
		Auth:      e.auth,
		Limiter:   auth.NewRateLimiter(config.LoginRatePerMinute, config.LoginRateBurst),
		Databases: svc,
		Admins:    adminSvc,
		Clients:   clientSvc,
		Audit:     e.meta,
		Health:    pinger{err: health},
		Notifier:  e.notifier,
		Settings:  Settings{BaseDomain: "pachislab.com", ExampleHostname: "postern-cliente1.pachislab.com", ExpirationDays: 30, AllowSelfRegister: true, MaxDatabasesPerClient: 3},
		Static: fstest.MapFS{
			"index.html":        {Data: []byte("<html>panel</html>")},
			"assets/app-abc.js": {Data: []byte("console.log(1)")},
		},
		Logger: logger,
	})
	return e
}

type req struct {
	body string
	as   string // username for the session cookie; empty = anonymous
	ip   string
}

func (e env) do(t *testing.T, method, path string, o req) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if o.body != "" {
		body = strings.NewReader(o.body)
	}
	r := httptest.NewRequest(method, path, body)
	if o.body != "" {
		r.Header.Set(config.HeaderContentType, config.MimeJSON)
	}
	r.Header.Set(config.HeaderRequestedWith, config.RequestedWithValue)
	if o.ip != "" {
		r.Header.Set(config.HeaderCFConnectIP, o.ip)
	}
	if o.as != "" {
		token, _, err := e.auth.Issue(o.as)
		require.NoError(t, err)
		r.AddCookie(&http.Cookie{Name: config.SessionCookieName, Value: token})
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, r)
	return rec
}

func (e env) owner(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	return e.do(t, method, path, req{body: body, as: adminUser})
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &v), rec.Body.String())
	return v
}

func (e env) createDB(t *testing.T, name string) model.Credentials {
	t.Helper()
	rec := e.owner(t, http.MethodPost, "/api/databases", `{"name":"`+name+`"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	return decode[model.Credentials](t, rec)
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// ---- infrastructure ------------------------------------------------------------

func TestHealth(t *testing.T) {
	assert.Equal(t, http.StatusOK, newEnv(t, nil).do(t, http.MethodGet, "/health", req{}).Code)
	assert.Equal(t, http.StatusServiceUnavailable, newEnv(t, errors.New("down")).do(t, http.MethodGet, "/health", req{}).Code)
}

func TestSecurityHeaders(t *testing.T) {
	rec := newEnv(t, nil).do(t, http.MethodGet, "/", req{})
	csp := rec.Header().Get("Content-Security-Policy")
	assert.Contains(t, csp, "default-src 'self'")
	assert.Contains(t, csp, "style-src 'self'")
	assert.NotContains(t, csp, "script-src 'unsafe-inline'")
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
}

func TestStaticAndSPAFallback(t *testing.T) {
	e := newEnv(t, nil)
	assert.Contains(t, e.do(t, http.MethodGet, "/", req{}).Body.String(), "panel")
	rec := e.do(t, http.MethodGet, "/databases/acme/schema", req{})
	assert.Contains(t, rec.Body.String(), "panel")
	assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
	rec = e.do(t, http.MethodGet, "/assets/app-abc.js", req{})
	assert.Contains(t, rec.Header().Get("Cache-Control"), "immutable")
	assert.Equal(t, http.StatusNotFound, e.do(t, http.MethodGet, "/api/nope", req{as: adminUser}).Code)
}

func TestMissingFrontendBuild(t *testing.T) {
	rec := httptest.NewRecorder()
	spaHandler(fstest.MapFS{})(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

// ---- authentication ----------------------------------------------------------------

func TestLoginBootstrapOwner(t *testing.T) {
	e := newEnv(t, nil)
	rec := e.do(t, http.MethodPost, "/api/auth/login", req{body: `{"username":"admin","password":"correct horse battery"}`, ip: "203.0.113.1"})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, config.RoleOwner, decode[map[string]any](t, rec)["role"])
	assert.True(t, rec.Result().Cookies()[0].HttpOnly)
	assert.Equal(t, []string{config.ActionLoginSuccess}, e.meta.Actions())
}

func TestLoginStoredAdmin(t *testing.T) {
	e := newEnv(t, nil)
	created := decode[map[string]any](t, e.owner(t, http.MethodPost, "/api/admins", `{"username":"ana","role":"viewer"}`))
	password := created["password"].(string)
	rec := e.do(t, http.MethodPost, "/api/auth/login", req{body: `{"username":"ana","password":"` + password + `"}`})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, config.RoleViewer, decode[map[string]any](t, rec)["role"])
}

func TestLoginIgnoresSurroundingWhitespace(t *testing.T) {
	rec := newEnv(t, nil).do(t, http.MethodPost, "/api/auth/login", req{body: `{"username":" admin\n","password":"correct horse battery \n"}`})
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestLoginSecureCookieBehindCloudflare(t *testing.T) {
	e := newEnv(t, nil)
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"admin","password":"correct horse battery"}`))
	r.Header.Set(config.HeaderContentType, config.MimeJSON)
	r.Header.Set(config.HeaderRequestedWith, config.RequestedWithValue)
	r.Header.Set("Cf-Visitor", `{"scheme":"https"}`)
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, r)
	assert.True(t, rec.Result().Cookies()[0].Secure)
}

func TestLoginFailureAndRateLimit(t *testing.T) {
	e := newEnv(t, nil)
	rec := e.do(t, http.MethodPost, "/api/auth/login", req{body: `{"username":"oops-my-password","password":"x"}`})
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.NotContains(t, e.meta.Audit[0].Target, "oops")

	var last int
	for range config.LoginRateBurst + 1 {
		last = e.do(t, http.MethodPost, "/api/auth/login", req{body: `{"username":"a","password":"b"}`, ip: "198.51.100.7"}).Code
	}
	assert.Equal(t, http.StatusTooManyRequests, last)
}

func TestLoginStoreError(t *testing.T) {
	e := newEnv(t, nil)
	e.meta.FailList = true
	rec := e.do(t, http.MethodPost, "/api/auth/login", req{body: `{"username":"someone","password":"b"}`})
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestRequestValidation(t *testing.T) {
	e := newEnv(t, nil)
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{}`))
	r.Header.Set(config.HeaderContentType, config.MimeJSON)
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, r)
	assert.Equal(t, http.StatusForbidden, rec.Code, "CSRF header required")

	r = httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader("username=a"))
	r.Header.Set(config.HeaderRequestedWith, config.RequestedWithValue)
	r.Header.Set(config.HeaderContentType, "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	e.handler.ServeHTTP(rec, r)
	assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code)

	assert.Equal(t, http.StatusBadRequest, e.do(t, http.MethodPost, "/api/auth/login", req{body: `{"user":1}`}).Code)
	assert.Equal(t, http.StatusBadRequest, e.owner(t, http.MethodDelete, "/api/backups/abc", "").Code, "invalid id")
}

func TestLogoutAndSession(t *testing.T) {
	e := newEnv(t, nil)
	assert.Equal(t, http.StatusNoContent, e.do(t, http.MethodPost, "/api/auth/logout", req{}).Code)
	s := decode[map[string]any](t, e.do(t, http.MethodGet, "/api/auth/session", req{as: adminUser}))
	assert.Equal(t, true, s["bootstrap"])
	s = decode[map[string]any](t, e.do(t, http.MethodGet, "/api/auth/session", req{as: viewer}))
	assert.Equal(t, config.RoleViewer, s["role"])
	assert.Equal(t, http.StatusUnauthorized, e.do(t, http.MethodGet, "/api/auth/session", req{as: "deleted"}).Code)
}

// ---- role enforcement -------------------------------------------------------------------

func TestRoleMatrix(t *testing.T) {
	e := newEnv(t, nil)
	e.createDB(t, "acme")
	cases := []struct {
		method, path, body string
		viewerCode         int
		operatorCode       int
	}{
		{http.MethodGet, "/api/databases", "", 200, 200},
		{http.MethodGet, "/api/databases/acme/schema", "", 200, 200},
		{http.MethodPost, "/api/databases/acme/query", `{"sql":"SELECT 1"}`, 403, 200},
		{http.MethodGet, "/api/databases/acme/tables/public/x/rows", "", 403, 200},
		{http.MethodPost, "/api/databases", `{"name":"other"}`, 403, 201},
		{http.MethodGet, "/api/admins", "", 403, 403},
		{http.MethodPost, "/api/admins", `{"username":"zed","role":"viewer"}`, 403, 403},
		// Only the panel owner touches the inactivity policy, global or per-database.
		{http.MethodPut, "/api/databases/acme/expiry", `{"neverExpires":true,"maxIdleDays":null}`, 403, 403},
		{http.MethodPost, "/api/databases/acme/extend", `{"days":10}`, 403, 403},
		{http.MethodGet, "/api/settings/expiry", "", 403, 403},
		{http.MethodPut, "/api/settings/expiry", `{"expirationDays":45,"expiringSoonDays":10}`, 403, 403},
		{http.MethodGet, "/api/clients", "", 403, 403},
		{http.MethodPost, "/api/clients", `{"username":"ana"}`, 403, 403},
		{http.MethodPut, "/api/clients/1/expiry", `{"neverExpires":true}`, 403, 403},
		{http.MethodDelete, "/api/clients/1", "", 403, 403},
	}
	for _, c := range cases {
		assert.Equal(t, c.viewerCode, e.do(t, c.method, c.path, req{body: c.body, as: viewer}).Code, "viewer %s %s", c.method, c.path)
		assert.Equal(t, c.operatorCode, e.do(t, c.method, c.path, req{body: c.body, as: operator}).Code, "operator %s %s", c.method, c.path)
	}
	for _, p := range []string{"/api/databases", "/api/audit", "/api/settings", "/api/backups"} {
		assert.Equal(t, http.StatusUnauthorized, e.do(t, http.MethodGet, p, req{}).Code, p)
	}
}

// ---- databases ----------------------------------------------------------------------------

func TestCreateListGetDelete(t *testing.T) {
	e := newEnv(t, nil)
	creds := e.createDB(t, "acme")
	assert.Equal(t, "postern-acme.pachislab.com", creds.Hostname)
	assert.True(t, e.cf.HasRoute("postern-acme.pachislab.com"), "fakeCloudflareClient received the route")

	list := decode[struct{ Databases []provision.DatabaseView }](t, e.owner(t, http.MethodGet, "/api/databases", ""))
	require.Len(t, list.Databases, 1)
	assert.NotContains(t, e.owner(t, http.MethodGet, "/api/databases", "").Body.String(), creds.Password)

	one := decode[provision.DatabaseView](t, e.owner(t, http.MethodGet, "/api/databases/acme", ""))
	assert.Equal(t, config.StatusActive, one.Status)
	info := decode[model.ConnectionInfo](t, e.owner(t, http.MethodGet, "/api/databases/acme/connection", ""))
	assert.Equal(t, "pdb_acme", info.Username)
	assert.Equal(t, http.StatusNotFound, e.owner(t, http.MethodGet, "/api/databases/ghost/connection", "").Code)

	assert.Equal(t, http.StatusBadRequest, e.owner(t, http.MethodDelete, "/api/databases/acme", `{"confirm":"acm"}`).Code)
	assert.Equal(t, http.StatusNoContent, e.owner(t, http.MethodDelete, "/api/databases/acme", `{"confirm":"acme"}`).Code)
	assert.Equal(t, http.StatusNotFound, e.owner(t, http.MethodGet, "/api/databases/acme", "").Code)
}

func TestCreateErrors(t *testing.T) {
	e := newEnv(t, nil)
	rec := e.owner(t, http.MethodPost, "/api/databases", `{"name":"Bad Name"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "el nombre", "validation messages are in Spanish")

	e.createDB(t, "acme")
	assert.Equal(t, http.StatusConflict, e.owner(t, http.MethodPost, "/api/databases", `{"name":"acme"}`).Code)

	e.cf.FailEnsure = true
	rec = e.owner(t, http.MethodPost, "/api/databases", `{"name":"other"}`)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, rec.Body.String(), "injected", "internal details stay in the logs")
}

func TestPasswordExpiryAndStats(t *testing.T) {
	e := newEnv(t, nil)
	e.createDB(t, "acme")
	e.insp.SchemaValue = model.Schema{Schemas: []string{"public"}, Tables: []model.Table{{Schema: "public", Name: "t", Rows: 5}}}

	rec := e.owner(t, http.MethodPost, "/api/databases/acme/password", "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))

	v := decode[provision.DatabaseView](t, e.owner(t, http.MethodPut, "/api/databases/acme/expiry", `{"neverExpires":true,"maxIdleDays":null}`))
	assert.True(t, v.NeverExpires)
	v = decode[provision.DatabaseView](t, e.owner(t, http.MethodPut, "/api/databases/acme/expiry", `{"neverExpires":false,"maxIdleDays":60}`))
	assert.Equal(t, 60, *v.MaxIdleDays)
	assert.Equal(t, http.StatusOK, e.owner(t, http.MethodPost, "/api/databases/acme/extend", `{"days":15}`).Code)
	assert.Equal(t, http.StatusBadRequest, e.owner(t, http.MethodPost, "/api/databases/acme/extend", `{"days":0}`).Code)

	st := decode[model.DatabaseStats](t, e.owner(t, http.MethodGet, "/api/databases/acme/stats", ""))
	assert.Equal(t, int64(5), st.TotalRows)
	sc := decode[model.Schema](t, e.owner(t, http.MethodGet, "/api/databases/acme/schema", ""))
	assert.Len(t, sc.Tables, 1)
}

func TestGlobalExpirySettings(t *testing.T) {
	e := newEnv(t, nil)

	// Still just the .env defaults until the owner overrides them.
	before := decode[Settings](t, e.owner(t, http.MethodGet, "/api/settings", ""))
	assert.Equal(t, 30, before.ExpirationDays)
	raw := decode[model.PlatformSettings](t, e.owner(t, http.MethodGet, "/api/settings/expiry", ""))
	assert.Nil(t, raw.ExpirationDays)

	rec := e.owner(t, http.MethodPut, "/api/settings/expiry", `{"expirationDays":45,"expiringSoonDays":10}`)
	require.Equal(t, http.StatusOK, rec.Code)
	saved := decode[model.PlatformSettings](t, rec)
	assert.Equal(t, 45, *saved.ExpirationDays)
	assert.Equal(t, "admin", saved.UpdatedBy)

	// Propagates to the plain /settings endpoint everyone reads.
	after := decode[Settings](t, e.owner(t, http.MethodGet, "/api/settings", ""))
	assert.Equal(t, 45, after.ExpirationDays)
	assert.Equal(t, 10, after.ExpiringSoonDays)
	assert.Equal(t, config.ActionGlobalExpiryChanged, e.meta.Audit[len(e.meta.Audit)-1].Action)

	// Warn days must stay below the expiration days.
	rec = e.owner(t, http.MethodPut, "/api/settings/expiry", `{"expirationDays":5,"expiringSoonDays":10}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	rec = e.owner(t, http.MethodPut, "/api/settings/expiry", `{"expirationDays":0,"expiringSoonDays":10}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	// Resetting a field back to the .env default with null.
	rec = e.owner(t, http.MethodPut, "/api/settings/expiry", `{"expirationDays":null,"expiringSoonDays":10}`)
	require.Equal(t, http.StatusOK, rec.Code)
	reset := decode[model.PlatformSettings](t, rec)
	assert.Nil(t, reset.ExpirationDays)
	after = decode[Settings](t, e.owner(t, http.MethodGet, "/api/settings", ""))
	assert.Equal(t, 30, after.ExpirationDays, "back to the .env default")

	e.meta.FailList = true
	fallback := decode[Settings](t, e.owner(t, http.MethodGet, "/api/settings", ""))
	assert.Equal(t, 30, fallback.ExpirationDays, "a settings read failure falls back to the .env default")
}

func TestClientsEndpoints(t *testing.T) {
	e := newEnv(t, nil)

	empty := decode[struct{ Clients []model.ClientUser }](t, e.owner(t, http.MethodGet, "/api/clients", ""))
	assert.Empty(t, empty.Clients)

	rec := e.owner(t, http.MethodPost, "/api/clients", `{"username":"ana"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	created := decode[struct {
		Client   model.ClientUser `json:"client"`
		Password string           `json:"password"`
	}](t, rec)
	require.NotEmpty(t, created.Password)
	anaID := created.Client.ID

	rec = e.owner(t, http.MethodPut, "/api/clients/"+itoa(anaID)+"/expiry", `{"neverExpires":true}`)
	require.Equal(t, http.StatusOK, rec.Code)
	saved := decode[model.ClientUser](t, rec)
	assert.True(t, saved.NeverExpires)

	list := decode[struct{ Clients []model.ClientUser }](t, e.owner(t, http.MethodGet, "/api/clients", ""))
	require.Len(t, list.Clients, 1)
	assert.Equal(t, "ana", list.Clients[0].Username)

	assert.Equal(t, http.StatusBadRequest, e.owner(t, http.MethodPut, "/api/clients/"+itoa(anaID)+"/expiry", `{"neverExpires":false,"maxIdleDays":0}`).Code)

	assert.Equal(t, http.StatusNoContent, e.owner(t, http.MethodDelete, "/api/clients/"+itoa(anaID), "").Code)
	assert.Equal(t, http.StatusNotFound, e.owner(t, http.MethodDelete, "/api/clients/"+itoa(anaID), "").Code)

	e.meta.FailList = true
	assert.Equal(t, http.StatusInternalServerError, e.owner(t, http.MethodGet, "/api/clients", "").Code)
}

func TestConsoleRowsAndCount(t *testing.T) {
	e := newEnv(t, nil)
	e.createDB(t, "acme")
	e.insp.Result = model.QueryResult{Columns: []string{"a"}, Rows: [][]any{{"1"}}, RowCount: 1}
	e.insp.Rows = 9

	res := decode[model.QueryResult](t, e.owner(t, http.MethodPost, "/api/databases/acme/query", `{"sql":"SELECT 1"}`))
	assert.Equal(t, 1, res.RowCount)
	res = decode[model.QueryResult](t, e.owner(t, http.MethodGet, "/api/databases/acme/tables/public/orders/rows", ""))
	assert.Equal(t, []string{"a"}, res.Columns)
	assert.Equal(t, "preview public.orders", e.insp.Queries[len(e.insp.Queries)-1])
	n := decode[map[string]int64](t, e.do(t, http.MethodGet, "/api/databases/acme/tables/public/orders/count", req{as: viewer}))
	assert.Equal(t, int64(9), n["rows"])

	e.insp.FailRead = true
	rec := e.owner(t, http.MethodPost, "/api/databases/acme/query", `{"sql":"SELEC 1"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "SQL errors are shown to the operator")
}

func TestUsersEndpoints(t *testing.T) {
	e := newEnv(t, nil)
	e.createDB(t, "acme")
	rec := e.owner(t, http.MethodPost, "/api/databases/acme/users", `{"name":"bi","access":"read"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, "pdb_acme__bi", decode[model.Credentials](t, rec).Username)

	users := decode[struct{ Users []model.DatabaseUser }](t, e.owner(t, http.MethodGet, "/api/databases/acme/users", ""))
	assert.Len(t, users.Users, 1)
	assert.Equal(t, http.StatusOK, e.owner(t, http.MethodPost, "/api/databases/acme/users/bi/password", "").Code)
	assert.Equal(t, http.StatusNoContent, e.owner(t, http.MethodDelete, "/api/databases/acme/users/bi", "").Code)
	assert.Equal(t, http.StatusNotFound, e.owner(t, http.MethodDelete, "/api/databases/acme/users/bi", "").Code)
}

// ---- backups -------------------------------------------------------------------------------

func TestBackupsEndpoints(t *testing.T) {
	e := newEnv(t, nil)
	e.createDB(t, "acme")
	rec := e.owner(t, http.MethodPost, "/api/databases/acme/backups", "")
	require.Equal(t, http.StatusAccepted, rec.Code)
	job := decode[model.Job](t, rec)

	jobs := decode[struct{ Jobs []model.Job }](t, e.owner(t, http.MethodGet, "/api/databases/acme/jobs", ""))
	assert.Len(t, jobs.Jobs, 1)

	// The sidecar finishes the dump.
	e.meta.SetJobStatus(job.ID, config.JobDone)
	require.NoError(t, os.WriteFile(filepath.Join(e.backups, job.File), []byte("PGDMP"), 0o600))

	list := decode[struct{ Backups []model.Job }](t, e.owner(t, http.MethodGet, "/api/backups", ""))
	assert.Len(t, list.Backups, 1)
	list = decode[struct{ Backups []model.Job }](t, e.owner(t, http.MethodGet, "/api/databases/acme/backups", ""))
	assert.Len(t, list.Backups, 1)

	rec = e.owner(t, http.MethodGet, "/api/backups/"+itoa(job.ID)+"/download", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "PGDMP", rec.Body.String())
	assert.Contains(t, rec.Header().Get("Content-Disposition"), job.File)

	rec = e.owner(t, http.MethodPost, "/api/backups/"+itoa(job.ID)+"/restore", `{"name":"acme-copia"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, "pdb_acme_copia", decode[model.Credentials](t, rec).Database)

	assert.Equal(t, http.StatusNoContent, e.owner(t, http.MethodDelete, "/api/backups/"+itoa(job.ID), "").Code)
	assert.Equal(t, http.StatusBadRequest, e.owner(t, http.MethodGet, "/api/backups/999/download", "").Code)
	assert.Equal(t, http.StatusBadRequest, e.owner(t, http.MethodGet, "/api/backups/x/download", "").Code)
	assert.Equal(t, http.StatusBadRequest, e.owner(t, http.MethodPost, "/api/backups/x/restore", `{"name":"a"}`).Code)
}

func TestDownloadMissingFile(t *testing.T) {
	e := newEnv(t, nil)
	j, _ := e.meta.EnqueueJob(context.Background(), model.Job{Kind: config.JobDump, File: "gone.dump"})
	e.meta.SetJobStatus(j.ID, config.JobDone)
	assert.Equal(t, http.StatusNotFound, e.owner(t, http.MethodGet, "/api/backups/"+itoa(j.ID)+"/download", "").Code)
}

// ---- templates, admins, notifications, audit --------------------------------------------------

func TestTemplatesEndpoints(t *testing.T) {
	e := newEnv(t, nil)
	rec := e.owner(t, http.MethodPost, "/api/templates", `{"name":"Demo","description":"d","sql":"CREATE TABLE t (id int)"}`)
	require.Equal(t, http.StatusCreated, rec.Code)
	tpl := decode[model.Template](t, rec)

	rec = e.owner(t, http.MethodPut, "/api/templates/"+itoa(tpl.ID), `{"name":"Demo 2","description":"","sql":"SELECT 1"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	list := decode[struct{ Templates []model.Template }](t, e.owner(t, http.MethodGet, "/api/templates", ""))
	assert.Equal(t, "Demo 2", list.Templates[0].Name)

	rec = e.owner(t, http.MethodPost, "/api/databases", `{"name":"acme","templateId":`+itoa(tpl.ID)+`}`)
	require.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, []string{"SELECT 1"}, e.insp.Applied)

	assert.Equal(t, http.StatusBadRequest, e.owner(t, http.MethodPost, "/api/templates", `{"name":"","sql":"x"}`).Code)
	assert.Equal(t, http.StatusNotFound, e.owner(t, http.MethodPut, "/api/templates/999", `{"name":"x","sql":"y"}`).Code)
	assert.Equal(t, http.StatusBadRequest, e.owner(t, http.MethodPut, "/api/templates/x", `{"name":"x","sql":"y"}`).Code)
	assert.Equal(t, http.StatusNoContent, e.owner(t, http.MethodDelete, "/api/templates/"+itoa(tpl.ID), "").Code)
	assert.Equal(t, http.StatusNotFound, e.owner(t, http.MethodDelete, "/api/templates/"+itoa(tpl.ID), "").Code)
	assert.Equal(t, http.StatusBadRequest, e.owner(t, http.MethodDelete, "/api/templates/x", "").Code)
}

func TestAdminsEndpoints(t *testing.T) {
	e := newEnv(t, nil)
	list := decode[struct{ Admins []model.Admin }](t, e.owner(t, http.MethodGet, "/api/admins", ""))
	assert.True(t, list.Admins[0].Bootstrap)

	rec := e.owner(t, http.MethodPost, "/api/admins", `{"username":"ana","role":"admin"}`)
	require.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	assert.Equal(t, http.StatusBadRequest, e.owner(t, http.MethodPost, "/api/admins", `{"username":"x","role":"admin"}`).Code)

	assert.Equal(t, http.StatusNoContent, e.owner(t, http.MethodPatch, "/api/admins/ana", `{"role":"viewer"}`).Code)
	assert.Equal(t, http.StatusBadRequest, e.owner(t, http.MethodPatch, "/api/admins/admin", `{"role":"viewer"}`).Code, "bootstrap is immutable")
	rec = e.owner(t, http.MethodPost, "/api/admins/ana/password", "")
	require.Equal(t, http.StatusOK, rec.Code)
	newPass := decode[map[string]string](t, rec)["password"]

	rec = e.do(t, http.MethodPost, "/api/auth/password", req{body: `{"current":"` + newPass + `","next":"a-brand-new-password"}`, as: "ana"})
	assert.Equal(t, http.StatusNoContent, rec.Code)
	rec = e.do(t, http.MethodPost, "/api/auth/password", req{body: `{"current":"x","next":"short"}`, as: "ana"})
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	assert.Equal(t, http.StatusNoContent, e.owner(t, http.MethodDelete, "/api/admins/ana", "").Code)
	assert.Equal(t, http.StatusNotFound, e.owner(t, http.MethodDelete, "/api/admins/ana", "").Code)
}

func TestNotificationsAndSettings(t *testing.T) {
	e := newEnv(t, nil)
	s := decode[Settings](t, e.owner(t, http.MethodGet, "/api/settings", ""))
	assert.Equal(t, []string{"webhook"}, s.NotifyChannels)

	assert.Equal(t, http.StatusNoContent, e.owner(t, http.MethodPost, "/api/notifications/test", "").Code)
	require.Len(t, e.notifier.Sent, 1)

	e.notifier.Err = errors.New("boom")
	assert.Equal(t, http.StatusBadGateway, e.owner(t, http.MethodPost, "/api/notifications/test", "").Code)
	e.notifier.Disabled = true
	assert.Equal(t, http.StatusBadRequest, e.owner(t, http.MethodPost, "/api/notifications/test", "").Code)
}

func TestAuditAndFailures(t *testing.T) {
	e := newEnv(t, nil)
	e.createDB(t, "acme")
	audit := decode[struct{ Entries []model.AuditEntry }](t, e.owner(t, http.MethodGet, "/api/audit", ""))
	assert.Equal(t, config.ActionDBCreated, audit.Entries[0].Action)

	e.meta.FailList = true
	for _, p := range []string{"/api/databases", "/api/audit", "/api/backups", "/api/templates"} {
		assert.Equal(t, http.StatusInternalServerError, e.owner(t, http.MethodGet, p, "").Code, p)
	}
}

func TestDataEditorEndpoints(t *testing.T) {
	e := newEnv(t, nil)
	e.createDB(t, "acme")
	e.insp.Data = model.TableData{Columns: []model.DataColumn{{Name: "id", PrimaryKey: true}}, Rows: [][]*string{{nil}}, Total: 1, Editable: true}
	base := "/api/databases/acme/tables/public/orders/data"

	d := decode[model.TableData](t, e.owner(t, http.MethodGet, base+"?page=2&sort=id&dir=desc", ""))
	assert.Equal(t, int64(1), d.Total)
	assert.Equal(t, "browse public.orders page=2 size=50 sort=id desc=true", e.insp.Queries[len(e.insp.Queries)-1])

	assert.Equal(t, http.StatusNoContent, e.owner(t, http.MethodPost, base, `{"values":{"name":"Ana","note":null}}`).Code)
	assert.Equal(t, http.StatusNoContent, e.owner(t, http.MethodPatch, base, `{"key":{"id":"1"},"values":{"name":"Bea"}}`).Code)
	assert.Equal(t, http.StatusNoContent, e.owner(t, http.MethodDelete, base, `{"key":{"id":"1"}}`).Code)
	assert.Len(t, e.insp.Writes, 3)

	assert.Equal(t, http.StatusForbidden, e.do(t, http.MethodGet, base, req{as: viewer}).Code, "viewers cannot read client data")
	assert.Equal(t, http.StatusForbidden, e.do(t, http.MethodPatch, base, req{as: viewer, body: `{"key":{},"values":{}}`}).Code)
	assert.Equal(t, http.StatusOK, e.do(t, http.MethodGet, base, req{as: operator}).Code)

	e.insp.FailWrite = true
	assert.Equal(t, http.StatusBadRequest, e.owner(t, http.MethodPost, base, `{"values":{}}`).Code)
	assert.Equal(t, http.StatusBadRequest, e.owner(t, http.MethodPost, base, `not json`).Code)
	assert.Equal(t, http.StatusBadRequest, e.owner(t, http.MethodPatch, base, `[]`).Code)
	assert.Equal(t, http.StatusBadRequest, e.owner(t, http.MethodDelete, base, `x`).Code)
}
