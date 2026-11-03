package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/model"
)

type fakeAdmins struct {
	roles map[string]string
	pass  map[string]string
	err   error
}

func (f fakeAdmins) Authenticate(_ context.Context, u, p string) (string, bool, error) {
	if f.err != nil {
		return "", false, f.err
	}
	if f.pass[u] != "" && f.pass[u] == p {
		return f.roles[u], true, nil
	}
	return "", false, nil
}

func (f fakeAdmins) RoleOf(_ context.Context, u string) (string, error) {
	if r, ok := f.roles[u]; ok {
		return r, nil
	}
	return "", errors.New("not found")
}

type fakeClientUser struct {
	id       int64
	password string
}

type fakeClients struct {
	users map[string]fakeClientUser
	err   error
}

func (f fakeClients) Authenticate(_ context.Context, u, p string) (model.ClientUser, bool, error) {
	if f.err != nil {
		return model.ClientUser{}, false, f.err
	}
	if cu, ok := f.users[u]; ok && cu.password == p {
		return model.ClientUser{ID: cu.id, Username: u}, true, nil
	}
	return model.ClientUser{}, false, nil
}

func (f fakeClients) ByID(_ context.Context, id int64) (model.ClientUser, bool, error) {
	if f.err != nil {
		return model.ClientUser{}, false, f.err
	}
	for name, cu := range f.users {
		if cu.id == id {
			return model.ClientUser{ID: id, Username: name}, true, nil
		}
	}
	return model.ClientUser{}, false, nil
}

func newWithAdmins() *Authenticator {
	return New("root", "bootstrap-password", secret, time.Hour, fakeAdmins{
		roles: map[string]string{"ana": config.RoleViewer},
		pass:  map[string]string{"ana": "ana-password-123", "root": "stored-root-password"},
	}, nil)
}

func TestLogin(t *testing.T) {
	a := newWithAdmins()
	ctx := context.Background()

	id, ok, err := a.Login(ctx, "root", "bootstrap-password")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, Identity{Username: "root", Role: config.RoleOwner}, id)

	id, ok, _ = a.Login(ctx, "ana", "ana-password-123")
	assert.True(t, ok)
	assert.Equal(t, config.RoleViewer, id.Role)

	_, ok, _ = a.Login(ctx, "ana", "wrong")
	assert.False(t, ok)

	_, ok, _ = a.Login(ctx, "root", "stored-root-password")
	assert.False(t, ok, "the bootstrap name can never be taken over by a stored admin")

	_, ok, _ = newAuth().Login(ctx, "ana", "x")
	assert.False(t, ok, "no admin store configured")

	failing := New("root", "p", secret, time.Hour, fakeAdmins{err: errors.New("db down")}, nil)
	_, _, err = failing.Login(ctx, "ana", "x")
	assert.Error(t, err)
}

func TestLoginFallsThroughToClients(t *testing.T) {
	ctx := context.Background()
	store := fakeClients{users: map[string]fakeClientUser{"bea": {id: 7, password: "bea-password-123"}}}
	a := New("root", "p", secret, time.Hour, nil, store)

	id, ok, err := a.Login(ctx, "bea", "bea-password-123")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, config.RoleClient, id.Role)
	require.NotNil(t, id.ClientUserID)
	assert.Equal(t, int64(7), *id.ClientUserID)

	_, ok, _ = a.Login(ctx, "bea", "wrong")
	assert.False(t, ok)

	failing := New("root", "p", secret, time.Hour, nil, fakeClients{err: errors.New("db down")})
	_, _, err = failing.Login(ctx, "bea", "x")
	assert.Error(t, err)
}

func TestMiddlewareResolvesClientIdentity(t *testing.T) {
	store := fakeClients{users: map[string]fakeClientUser{"bea": {id: 7, password: "x"}}}
	a := New("root", "p", secret, time.Hour, nil, store)
	var got Identity
	h := a.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = FromContext(r.Context()) }))

	token, _, err := a.Issue(ClientIdentity(model.ClientUser{ID: 7, Username: "bea"}).Subject())
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: config.SessionCookieName, Value: token})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, config.RoleClient, got.Role)
	require.NotNil(t, got.ClientUserID)
	assert.Equal(t, int64(7), *got.ClientUserID)
}

func TestRequireClient(t *testing.T) {
	h := RequireClient(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	for role, want := range map[string]int{config.RoleClient: http.StatusOK, config.RoleOwner: http.StatusForbidden, config.RoleAdmin: http.StatusForbidden, "": http.StatusForbidden} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req = req.WithContext(context.WithValue(req.Context(), ctxKey{}, Identity{Username: "x", Role: role}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assert.Equal(t, want, rec.Code, role)
	}
}

func TestMiddlewareResolvesCurrentRole(t *testing.T) {
	store := fakeAdmins{roles: map[string]string{"ana": config.RoleAdmin}}
	a := New("root", "p", secret, time.Hour, store, nil)
	var got Identity
	h := a.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = FromContext(r.Context()) }))

	call := func(user string) int {
		token, _, _ := a.Issue(user)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: config.SessionCookieName, Value: token})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	assert.Equal(t, http.StatusOK, call("ana"))
	assert.Equal(t, Identity{Username: "ana", Role: config.RoleAdmin}, got)
	assert.Equal(t, http.StatusOK, call("root"))
	assert.Equal(t, config.RoleOwner, got.Role)
	assert.Equal(t, http.StatusUnauthorized, call("deleted-user"), "removed admins lose access immediately")

	noStore := New("root", "p", secret, time.Hour, nil, nil)
	token, _, _ := noStore.Issue("ana")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: config.SessionCookieName, Value: token})
	rec := httptest.NewRecorder()
	noStore.Middleware(h).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAllowsAndRequireRole(t *testing.T) {
	assert.True(t, Allows(config.RoleOwner, config.RoleAdmin))
	assert.True(t, Allows(config.RoleAdmin, config.RoleAdmin))
	assert.False(t, Allows(config.RoleViewer, config.RoleAdmin))
	assert.False(t, Allows("", config.RoleViewer))
	assert.False(t, Allows(config.RoleOwner, "bogus"))

	h := RequireRole(config.RoleAdmin)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	for role, want := range map[string]int{config.RoleViewer: http.StatusForbidden, config.RoleAdmin: http.StatusOK} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req = req.WithContext(context.WithValue(req.Context(), ctxKey{}, Identity{Username: "x", Role: role}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assert.Equal(t, want, rec.Code, role)
	}
	assert.True(t, newAuth().IsBootstrap("admin"))
}

// A client who picked the same name as the owner or an administrator must
// stay a client: their session subject is namespaced by id.
func TestClientSessionNeverResolvesAsAdminWithTheSameName(t *testing.T) {
	clients := fakeClients{users: map[string]fakeClientUser{"root": {id: 7, password: "x"}, "bea": {id: 8, password: "x"}}}
	a := New("root", "p", secret, time.Hour, fakeAdmins{roles: map[string]string{"bea": config.RoleOwner}}, clients)
	resolve := func(subject string) (Identity, int) {
		token, _, err := a.Issue(subject)
		require.NoError(t, err)
		var got Identity
		h := a.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = FromContext(r.Context()) }))
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: config.SessionCookieName, Value: token})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return got, rec.Code
	}

	for name, id := range map[string]int64{"root": 7, "bea": 8} {
		got, code := resolve(ClientIdentity(model.ClientUser{ID: id, Username: name}).Subject())
		assert.Equal(t, http.StatusOK, code)
		assert.Equal(t, config.RoleClient, got.Role, "client %q must not become owner/admin", name)
	}
	got, _ := resolve("bea")
	assert.Equal(t, config.RoleOwner, got.Role, "the real admin keeps their plain subject")

	_, code := resolve("client:999")
	assert.Equal(t, http.StatusUnauthorized, code, "unknown client id")
	_, code = resolve("client:abc")
	assert.Equal(t, http.StatusUnauthorized, code, "malformed client subject")
}
