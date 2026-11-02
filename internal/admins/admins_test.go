package admins

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/model"
	"github.com/tecsharp/posterndb/internal/testsupport"
)

func newService(t *testing.T) (*Service, *testsupport.FakeMetadata) {
	t.Helper()
	store := testsupport.NewFakeMetadata()
	s := New(store, testsupport.FakeSecrets{}, "root")
	s.cost = bcrypt.MinCost
	return s, store
}

func TestCreateAndAuthenticate(t *testing.T) {
	s, store := newService(t)
	ctx := context.Background()

	a, password, err := s.Create(ctx, " Ana.Lopez ", config.RoleAdmin, "root")
	require.NoError(t, err)
	assert.Equal(t, "ana.lopez", a.Username)
	assert.Len(t, password, generatedPassword)
	assert.NotEqual(t, password, store.Admins["ana.lopez"].PasswordHash, "only the hash is stored")

	role, ok, err := s.Authenticate(ctx, "ana.lopez", password)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, config.RoleAdmin, role)
	assert.NotNil(t, store.Admins["ana.lopez"].LastLoginAt)

	_, ok, _ = s.Authenticate(ctx, "ana.lopez", "wrong")
	assert.False(t, ok)
	_, ok, _ = s.Authenticate(ctx, "ghost", "x")
	assert.False(t, ok)

	r, err := s.RoleOf(ctx, "ana.lopez")
	require.NoError(t, err)
	assert.Equal(t, config.RoleAdmin, r)
}

func TestCreateValidation(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	_, _, err := s.Create(ctx, "x", config.RoleAdmin, "root")
	assert.ErrorIs(t, err, ErrUsername)
	_, _, err = s.Create(ctx, "valid", "superuser", "root")
	assert.ErrorIs(t, err, ErrRole)
	_, _, err = s.Create(ctx, "root", config.RoleAdmin, "root")
	assert.ErrorIs(t, err, model.ErrConflict)
	_, _, err = s.Create(ctx, "ana", config.RoleViewer, "root")
	require.NoError(t, err)
	_, _, err = s.Create(ctx, "ana", config.RoleViewer, "root")
	assert.ErrorIs(t, err, model.ErrConflict)

	failing := New(testsupport.NewFakeMetadata(), testsupport.FakeSecrets{FailPassword: true}, "root")
	_, _, err = failing.Create(ctx, "bob", config.RoleViewer, "root")
	assert.Error(t, err)
}

func TestListIncludesBootstrap(t *testing.T) {
	s, _ := newService(t)
	_, _, _ = s.Create(context.Background(), "ana", config.RoleViewer, "root")
	list, err := s.List(context.Background())
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.True(t, list[0].Bootstrap)
	assert.Equal(t, config.RoleOwner, list[0].Role)

	_, store := newService(t)
	store.FailList = true
	_, err = New(store, testsupport.FakeSecrets{}, "root").List(context.Background())
	assert.Error(t, err)
}

func TestSetRoleAndDeleteRules(t *testing.T) {
	s, store := newService(t)
	ctx := context.Background()
	_, _, _ = s.Create(ctx, "ana", config.RoleViewer, "root")

	require.NoError(t, s.SetRole(ctx, "ana", config.RoleOwner, "root"))
	assert.Equal(t, config.RoleOwner, store.Admins["ana"].Role)

	assert.ErrorIs(t, s.SetRole(ctx, "root", config.RoleViewer, "ana"), ErrBootstrap)
	assert.ErrorIs(t, s.SetRole(ctx, "ana", config.RoleViewer, "ana"), ErrSelf)
	assert.ErrorIs(t, s.SetRole(ctx, "ana", "bogus", "root"), ErrRole)
	assert.ErrorIs(t, s.SetRole(ctx, "ghost", config.RoleViewer, "root"), model.ErrNotFound)

	assert.ErrorIs(t, s.Delete(ctx, "ana", "ana"), ErrSelf)
	assert.ErrorIs(t, s.Delete(ctx, "root", "ana"), ErrBootstrap)
	require.NoError(t, s.Delete(ctx, "ana", "root"))
	assert.Empty(t, store.Admins)
	assert.Error(t, s.Delete(ctx, "ana", "root"))
}

func TestPasswords(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	_, _, _ = s.Create(ctx, "ana", config.RoleAdmin, "root")

	reset, err := s.ResetPassword(ctx, "ana", "root")
	require.NoError(t, err)
	_, ok, _ := s.Authenticate(ctx, "ana", reset)
	assert.True(t, ok)
	_, err = s.ResetPassword(ctx, "root", "root")
	assert.ErrorIs(t, err, ErrBootstrap)
	_, err = s.ResetPassword(ctx, "ghost", "root")
	assert.Error(t, err)

	assert.ErrorIs(t, s.ChangeOwnPassword(ctx, "ana", reset, "short"), ErrWeakPassword)
	assert.ErrorIs(t, s.ChangeOwnPassword(ctx, "ana", "wrong", "a-long-new-password"), ErrWrongPassword)
	assert.ErrorIs(t, s.ChangeOwnPassword(ctx, "root", "x", "a-long-new-password"), ErrBootstrap)
	require.NoError(t, s.ChangeOwnPassword(ctx, "ana", reset, "a-long-new-password"))
	_, ok, _ = s.Authenticate(ctx, "ana", "a-long-new-password")
	assert.True(t, ok)

	failing := New(testsupport.NewFakeMetadata(), testsupport.FakeSecrets{FailPassword: true}, "root")
	_, err = failing.ResetPassword(ctx, "ana", "root")
	assert.Error(t, err)
}

func TestAuthenticateStoreError(t *testing.T) {
	s, store := newService(t)
	store.FailList = true
	_, _, err := s.Authenticate(context.Background(), "ana", "x")
	assert.Error(t, err)
	assert.True(t, ValidRole(config.RoleViewer))
	assert.False(t, ValidRole("root"))
}
