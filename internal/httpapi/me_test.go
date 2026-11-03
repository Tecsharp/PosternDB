package httpapi

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tecsharp/posterndb/internal/model"
)

func (e env) addClient(t *testing.T, username string) {
	t.Helper()
	rec := e.owner(t, http.MethodPost, "/api/clients", `{"username":"`+username+`"}`)
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

	rec := e.do(t, http.MethodGet, "/api/me/templates", req{as: "ana"})
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

	rec := e.do(t, http.MethodPost, "/api/me/databases", req{as: "ana", body: `{"name":"tienda","templateId":` + itoa(shared.ID) + `}`})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, []string{"SELECT 1"}, e.insp.Applied)

	rec = e.do(t, http.MethodPost, "/api/me/databases", req{as: "ana", body: `{"name":"otra","templateId":` + itoa(private.ID) + `}`})
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
		rec := e.do(t, http.MethodPost, "/api/me/databases", req{as: "ana", body: body})
		assert.Equal(t, http.StatusBadRequest, rec.Code, "clone/restore fields are rejected for clients: %s", body)
	}
	assert.Len(t, e.meta.Jobs, before, "no content job may be queued")
	assert.Equal(t, http.StatusNotFound, e.do(t, http.MethodGet, "/api/me/databases/copia", req{as: "ana"}).Code, "nothing was created")
}

func TestClientLiveConnectionsAreOwnedOnly(t *testing.T) {
	e := newEnv(t, nil)
	e.createDB(t, "ajena")
	e.addClient(t, "ana")
	rec := e.do(t, http.MethodPost, "/api/me/databases", req{as: "ana", body: `{"name":"mia"}`})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	assert.Equal(t, http.StatusOK, e.do(t, http.MethodGet, "/api/me/databases/mia/live-connections", req{as: "ana"}).Code)
	assert.Equal(t, http.StatusNotFound, e.do(t, http.MethodGet, "/api/me/databases/ajena/live-connections", req{as: "ana"}).Code)
}
