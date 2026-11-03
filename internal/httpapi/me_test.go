package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/tecsharp/posterndb/internal/auth"
	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/model"
)

func (e env) addClient(t *testing.T, username string) {
	t.Helper()
	rec := e.owner(t, http.MethodPost, "/api/clients", `{"email":"`+username+`@example.com","fullName":"Cuenta `+username+`"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
}

func (e env) addTemplate(t *testing.T, name, sql string, shared bool) model.Template {
	t.Helper()
	body := `{"name":"` + name + `","description":"d","sql":"` + sql + `","shared":`
	if shared {
		body += "true}"
	} else {
		body += "false}"
	}
	rec := e.owner(t, http.MethodPost, "/api/templates", body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	return decode[model.Template](t, rec)
}

func TestPublicConfigIsAnonymous(t *testing.T) {
	e := newEnv(t, nil)
	rec := e.do(t, http.MethodGet, "/api/auth/config", req{})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, decode[map[string]bool](t, rec)["allowSelfRegister"])
}

func TestLiveConnectionsForAdmins(t *testing.T) {
	e := newEnv(t, nil)
	e.createDB(t, "acme")

	for _, as := range []string{adminUser, operator, viewer} {
		rec := e.do(t, http.MethodGet, "/api/databases/acme/live-connections", req{as: as})
		require.Equal(t, http.StatusOK, rec.Code, as)
		list := decode[struct{ Connections []model.Connection }](t, rec)
		require.Len(t, list.Connections, 1, as)
		assert.Equal(t, "psql", list.Connections[0].Application)
	}
	assert.Equal(t, http.StatusUnauthorized, e.do(t, http.MethodGet, "/api/databases/acme/live-connections", req{}).Code)
	assert.Equal(t, http.StatusNotFound, e.owner(t, http.MethodGet, "/api/databases/nope/live-connections", "").Code)
}

func TestClientSeesOnlySharedTemplates(t *testing.T) {
	e := newEnv(t, nil)
	e.addClient(t, "ana")
	e.addTemplate(t, "Publica", "SELECT 1", true)
	e.addTemplate(t, "Privada", "SELECT 2", false)

	rec := e.do(t, http.MethodGet, "/api/me/templates", req{as: "ana@example.com"})
	require.Equal(t, http.StatusOK, rec.Code)
	list := decode[struct{ Templates []model.Template }](t, rec)
	require.Len(t, list.Templates, 1)
	assert.Equal(t, "Publica", list.Templates[0].Name)

	assert.Equal(t, http.StatusForbidden, e.owner(t, http.MethodGet, "/api/me/templates", "").Code, "admins use /api/templates")
}

func TestClientCreateUsesSharedTemplateOnly(t *testing.T) {
	e := newEnv(t, nil)
	e.addClient(t, "ana")
	shared := e.addTemplate(t, "Publica", "SELECT 1", true)
	private := e.addTemplate(t, "Privada", "SELECT 2", false)

	rec := e.do(t, http.MethodPost, "/api/me/databases", req{as: "ana@example.com", body: `{"name":"tienda","templateId":` + itoa(shared.ID) + `}`})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, []string{"SELECT 1"}, e.insp.Applied)

	rec = e.do(t, http.MethodPost, "/api/me/databases", req{as: "ana@example.com", body: `{"name":"otra","templateId":` + itoa(private.ID) + `}`})
	assert.Equal(t, http.StatusBadRequest, rec.Code, "private templates are not offered to clients")
	assert.Equal(t, []string{"SELECT 1"}, e.insp.Applied)
}

func TestClientCannotCloneOrRestoreOtherDatabases(t *testing.T) {
	e := newEnv(t, nil)
	e.createDB(t, "ajena")
	e.addClient(t, "ana")
	before := len(e.meta.Jobs)

	for _, body := range []string{
		`{"name":"copia","cloneFrom":"ajena"}`,
		`{"name":"copia","restoreBackupId":1}`,
	} {
		rec := e.do(t, http.MethodPost, "/api/me/databases", req{as: "ana@example.com", body: body})
		assert.Equal(t, http.StatusBadRequest, rec.Code, "clone/restore fields are rejected for clients: %s", body)
	}
	assert.Len(t, e.meta.Jobs, before, "no content job may be queued")
	assert.Equal(t, http.StatusNotFound, e.do(t, http.MethodGet, "/api/me/databases/copia", req{as: "ana@example.com"}).Code, "nothing was created")
}

func TestClientLiveConnectionsAreOwnedOnly(t *testing.T) {
	e := newEnv(t, nil)
	e.createDB(t, "ajena")
	e.addClient(t, "ana")
	rec := e.do(t, http.MethodPost, "/api/me/databases", req{as: "ana@example.com", body: `{"name":"mia"}`})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	assert.Equal(t, http.StatusOK, e.do(t, http.MethodGet, "/api/me/databases/mia/live-connections", req{as: "ana@example.com"}).Code)
	assert.Equal(t, http.StatusNotFound, e.do(t, http.MethodGet, "/api/me/databases/ajena/live-connections", req{as: "ana@example.com"}).Code)
}

// A client whose username equals an administrator's must stay a client: the
// session subject is namespaced by id, so the name decides nothing.
func TestClientWithAnAdminsUsernameDoesNotEscalate(t *testing.T) {
	e := newEnv(t, nil)
	hash, err := bcrypt.GenerateFromPassword([]byte("a-long-enough-password"), bcrypt.MinCost)
	require.NoError(t, err)
	// A legacy account (created before emails) that shares a stored admin's name.
	_, err = e.meta.CreateClientUser(context.Background(), model.NewClientUser{Username: operator, PasswordHash: string(hash)})
	require.NoError(t, err)

	rec := e.do(t, http.MethodPost, "/api/auth/login", req{body: `{"username":"` + operator + `","password":"a-long-enough-password"}`})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, config.RoleClient, decode[map[string]any](t, rec)["role"])

	r := httptest.NewRequest(http.MethodGet, "/api/admins", nil)
	r.Header.Set(config.HeaderRequestedWith, config.RequestedWithValue)
	r.AddCookie(rec.Result().Cookies()[0])
	out := httptest.NewRecorder()
	e.handler.ServeHTTP(out, r)
	assert.Equal(t, http.StatusForbidden, out.Code, "a client named like an admin gets no admin access")
}

func TestSessionOfAClientNamedLikeTheOwnerIsNotBootstrap(t *testing.T) {
	e := newEnv(t, nil)
	u, err := e.meta.CreateClientUser(context.Background(), model.NewClientUser{Username: adminUser, PasswordHash: "x"})
	require.NoError(t, err)
	token, _, err := e.auth.Issue(auth.ClientIdentity(u).Subject())
	require.NoError(t, err)
	r := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	r.Header.Set(config.HeaderRequestedWith, config.RequestedWithValue)
	r.AddCookie(&http.Cookie{Name: config.SessionCookieName, Value: token})
	out := httptest.NewRecorder()
	e.handler.ServeHTTP(out, r)
	require.Equal(t, http.StatusOK, out.Code)
	body := decode[map[string]any](t, out)
	assert.Equal(t, config.RoleClient, body["role"])
	assert.Equal(t, false, body["bootstrap"])
}
