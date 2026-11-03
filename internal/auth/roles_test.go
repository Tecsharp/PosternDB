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

type fakeAccount struct {
	id       int64
	password string
	role     string
}

// fakeAccounts is an in-memory account store; the role is read on every call,
// so a test can change it between requests.
type fakeAccounts struct {
	users map[string]*fakeAccount
	err   error
}

func (f fakeAccounts) user(name string, a *fakeAccount) model.ClientUser {
	role := a.role
	if role == "" {
		role = config.AccountRoleUser
	}
	return model.ClientUser{ID: a.id, Username: name, Role: role}
}

func (f fakeAccounts) Authenticate(_ context.Context, u, p string) (model.ClientUser, bool, error) {
	if f.err != nil {
		return model.ClientUser{}, false, f.err
	}
	if a, ok := f.users[u]; ok && a.password == p {
		return f.user(u, a), true, nil
	}
	return model.ClientUser{}, false, nil
}

func (f fakeAccounts) ByID(_ context.Context, id int64) (model.ClientUser, bool, error) {
	if f.err != nil {
		return model.ClientUser{}, false, f.err
	}
	for name, a := range f.users {
		if a.id == id {
			return f.user(name, a), true, nil
		}
	}
	return model.ClientUser{}, false, nil
}

func accounts() fakeAccounts {
	return fakeAccounts{users: map[string]*fakeAccount{
		"ana": {id: 1, password: "ana-password-123", role: config.RoleViewer},
		"bea": {id: 7, password: "bea-password-123"},
		"cai": {id: 9, password: "cai-password-123", role: config.RoleAdmin},
	}}
}

func call(a *Authenticator, subject string) (Identity, int) {
	token, _, _ := a.Issue(subject)
	var got Identity
	h := a.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = FromContext(r.Context()) }))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: config.SessionCookieName, Value: token})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return got, rec.Code
}

func TestLogin(t *testing.T) {
	a := New("root", "bootstrap-password", secret, time.Hour, accounts())
	ctx := context.Background()

	id, ok, err := a.Login(ctx, "root", "bootstrap-password")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, Identity{Username: "root", Role: config.RoleOwner}, id)
	assert.Nil(t, id.ClientUserID, "the owner is not an account")

	id, ok, _ = a.Login(ctx, "ana", "ana-password-123")
	assert.True(t, ok)
	assert.Equal(t, config.RoleViewer, id.Role, "an account with the viewer role opens the admin panel read-only")
	require.NotNil(t, id.ClientUserID)

	id, ok, _ = a.Login(ctx, "cai", "cai-password-123")
	assert.True(t, ok)
	assert.Equal(t, config.RoleAdmin, id.Role)

	id, ok, _ = a.Login(ctx, "bea", "bea-password-123")
	assert.True(t, ok)
	assert.Equal(t, config.RoleClient, id.Role, "a plain user stays in the reduced panel")
	require.NotNil(t, id.ClientUserID)
	assert.Equal(t, int64(7), *id.ClientUserID)

	_, ok, _ = a.Login(ctx, "ana", "wrong")
	assert.False(t, ok)
	_, ok, _ = a.Login(ctx, "nobody", "x")
	assert.False(t, ok)

	_, ok, _ = New("root", "p", secret, time.Hour, nil).Login(ctx, "ana", "x")
	assert.False(t, ok, "no account store configured")

	failing := New("root", "p", secret, time.Hour, fakeAccounts{err: errors.New("db down")})
	_, _, err = failing.Login(ctx, "ana", "x")
	assert.Error(t, err)
}

func TestTheOwnersNameCannotBeTakenByAnAccount(t *testing.T) {
	a := New("root", "bootstrap-password", secret, time.Hour, fakeAccounts{users: map[string]*fakeAccount{
		"root": {id: 3, password: "an-account-password", role: config.RoleAdmin},
	}})
	_, ok, _ := a.Login(context.Background(), "root", "an-account-password")
	assert.False(t, ok, "an account named like the owner never logs in by that name")
}

func TestMiddlewareResolvesTheCurrentRoleOfAnAccount(t *testing.T) {
	store := accounts()
	a := New("root", "p", secret, time.Hour, store)
	subject := func(name string) string { return AccountIdentity(store.user(name, store.users[name])).Subject() }

	got, code := call(a, subject("bea"))
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, config.RoleClient, got.Role)

	// The owner promotes bea: the very next request already opens the admin panel,
	// with the same session and no new login.
	store.users["bea"].role = config.RoleAdmin
	got, code = call(a, subject("bea"))
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, config.RoleAdmin, got.Role)
	require.NotNil(t, got.ClientUserID, "an administrator is still an account")
	assert.Equal(t, int64(7), *got.ClientUserID)

	store.users["bea"].role = config.AccountRoleUser
	got, _ = call(a, subject("bea"))
	assert.Equal(t, config.RoleClient, got.Role, "and demoting takes effect immediately too")

	delete(store.users, "bea")
	_, code = call(a, AccountIdentity(model.ClientUser{ID: 7, Username: "bea"}).Subject())
	assert.Equal(t, http.StatusUnauthorized, code, "a deleted account loses access at once")

	got, code = call(a, "root")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, config.RoleOwner, got.Role)

	_, code = call(New("root", "p", secret, time.Hour, nil), "client:1")
	assert.Equal(t, http.StatusUnauthorized, code, "no account store configured")
}

func TestPlainSubjectsOnlyEverMeanTheOwner(t *testing.T) {
	a := New("root", "p", secret, time.Hour, accounts())
	_, code := call(a, "cai")
	assert.Equal(t, http.StatusUnauthorized, code, "a bare username is not a session for any account")
	_, code = call(a, "client:999")
	assert.Equal(t, http.StatusUnauthorized, code, "unknown account id")
	_, code = call(a, "client:abc")
	assert.Equal(t, http.StatusUnauthorized, code, "malformed account subject")
}

// An account named like the owner must never resolve as the owner: the session
// subject is namespaced by the account id.
func TestAccountSessionNeverResolvesAsTheOwnerWithTheSameName(t *testing.T) {
	store := fakeAccounts{users: map[string]*fakeAccount{"root": {id: 7, password: "x", role: config.RoleAdmin}}}
	a := New("root", "p", secret, time.Hour, store)

	got, code := call(a, AccountIdentity(store.user("root", store.users["root"])).Subject())
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, config.RoleAdmin, got.Role, "an administrator account, never the owner")
	assert.NotEqual(t, config.RoleOwner, got.Role)
}

func TestUserPasswordLoginCanBeSwitchedOffButNotForAdministrators(t *testing.T) {
	a := New("root", "p", secret, time.Hour, accounts())
	a.SetUserPasswordLogin(false)
	ctx := context.Background()

	_, ok, err := a.Login(ctx, "bea", "bea-password-123")
	require.NoError(t, err)
	assert.False(t, ok, "AUTH_MODE=google: plain users have no password login")

	_, ok, _ = a.Login(ctx, "cai", "cai-password-123")
	assert.True(t, ok, "administrators always can")
	_, ok, _ = a.Login(ctx, "ana", "ana-password-123")
	assert.True(t, ok, "viewers too")
	_, ok, _ = a.Login(ctx, "root", "p")
	assert.True(t, ok, "and the owner")
}

func TestAccountIdentityMapsStoredRolesToPanels(t *testing.T) {
	for stored, want := range map[string]string{
		config.AccountRoleUser: config.RoleClient,
		config.RoleViewer:      config.RoleViewer,
		config.RoleAdmin:       config.RoleAdmin,
		"":                     config.RoleClient,
		"owner":                config.RoleClient,
		"something-else":       config.RoleClient,
	} {
		id := AccountIdentity(model.ClientUser{ID: 3, Username: "x", Role: stored})
		assert.Equal(t, want, id.Role, "stored role %q", stored)
		assert.Equal(t, "client:3", id.Subject(), "every account uses its namespaced subject")
	}
	assert.NotEqual(t, config.RoleOwner, AccountIdentity(model.ClientUser{ID: 1, Role: "owner"}).Role, "no stored value can ever produce the owner")
}

func TestRequireClient(t *testing.T) {
	h := RequireClient(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	for role, want := range map[string]int{config.RoleClient: http.StatusOK, config.RoleOwner: http.StatusForbidden, config.RoleAdmin: http.StatusForbidden, config.RoleViewer: http.StatusForbidden, "": http.StatusForbidden} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req = req.WithContext(context.WithValue(req.Context(), ctxKey{}, Identity{Username: "x", Role: role}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assert.Equal(t, want, rec.Code, role)
	}
}

func TestRequireAccount(t *testing.T) {
	h := RequireAccount(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	id := int64(4)
	for name, c := range map[string]struct {
		identity Identity
		want     int
	}{
		"plain user":    {Identity{Username: "u", Role: config.RoleClient, ClientUserID: &id}, http.StatusOK},
		"administrator": {Identity{Username: "a", Role: config.RoleAdmin, ClientUserID: &id}, http.StatusOK},
		"viewer":        {Identity{Username: "v", Role: config.RoleViewer, ClientUserID: &id}, http.StatusOK},
		"the owner":     {Identity{Username: "root", Role: config.RoleOwner}, http.StatusForbidden},
	} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req = req.WithContext(context.WithValue(req.Context(), ctxKey{}, c.identity))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assert.Equal(t, c.want, rec.Code, name)
	}
}

func TestAllowsAndRequireRole(t *testing.T) {
	assert.True(t, Allows(config.RoleOwner, config.RoleAdmin))
	assert.True(t, Allows(config.RoleAdmin, config.RoleAdmin))
	assert.False(t, Allows(config.RoleViewer, config.RoleAdmin))
	assert.False(t, Allows(config.RoleClient, config.RoleViewer), "a plain user never passes an admin check")
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
