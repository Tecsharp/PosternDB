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

func newWithAdmins() *Authenticator {
	return New("root", "bootstrap-password", secret, time.Hour, fakeAdmins{
		roles: map[string]string{"ana": config.RoleViewer},
		pass:  map[string]string{"ana": "ana-password-123", "root": "stored-root-password"},
	})
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

	failing := New("root", "p", secret, time.Hour, fakeAdmins{err: errors.New("db down")})
	_, _, err = failing.Login(ctx, "ana", "x")
	assert.Error(t, err)
}

func TestMiddlewareResolvesCurrentRole(t *testing.T) {
	store := fakeAdmins{roles: map[string]string{"ana": config.RoleAdmin}}
	a := New("root", "p", secret, time.Hour, store)
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

	noStore := New("root", "p", secret, time.Hour, nil)
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
