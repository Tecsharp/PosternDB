package clients

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

func newService(t *testing.T, allowRegister bool) (*Service, *testsupport.FakeMetadata) {
	t.Helper()
	store := testsupport.NewFakeMetadata()
	s := New(store, testsupport.FakeSecrets{}, allowRegister)
	s.cost = bcrypt.MinCost
	return s, store
}

const strongPassword = "correct horse battery staple"

func TestRegisterAndAuthenticate(t *testing.T) {
	s, store := newService(t, true)
	ctx := context.Background()

	u, err := s.Register(ctx, " Ana.Lopez ", strongPassword)
	require.NoError(t, err)
	assert.Equal(t, "ana.lopez", u.Username)
	assert.Equal(t, "", u.CreatedBy, "self-registration leaves created_by empty")
	assert.NotEqual(t, strongPassword, store.ClientUsers[u.ID].PasswordHash, "only the hash is stored")

	got, ok, err := s.Authenticate(ctx, "ana.lopez", strongPassword)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, u.ID, got.ID)
	assert.NotNil(t, store.ClientUsers[u.ID].LastLoginAt)

	_, ok, _ = s.Authenticate(ctx, "ana.lopez", "wrong")
	assert.False(t, ok)
	_, ok, _ = s.Authenticate(ctx, "ghost", "x")
	assert.False(t, ok)

	id, ok, err := s.Exists(ctx, "ana.lopez")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, u.ID, id)
	_, ok, err = s.Exists(ctx, "ghost")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestRegisterDisabled(t *testing.T) {
	s, _ := newService(t, false)
	_, err := s.Register(context.Background(), "ana", strongPassword)
	assert.ErrorIs(t, err, ErrRegisterDisabled)
}

func TestRegisterValidation(t *testing.T) {
	s, _ := newService(t, true)
	ctx := context.Background()
	_, err := s.Register(ctx, "x", strongPassword)
	assert.ErrorIs(t, err, ErrUsername)
	_, err = s.Register(ctx, "valid", "short")
	assert.ErrorIs(t, err, ErrWeakPassword)

	_, err = s.Register(ctx, "ana", strongPassword)
	require.NoError(t, err)
	_, err = s.Register(ctx, "ana", strongPassword)
	assert.ErrorIs(t, err, model.ErrConflict)

	failing, _ := newService(t, true)
	failing.passwords = testsupport.FakeSecrets{FailPassword: true}
	_, _, err2 := failing.Create(ctx, "bob", "root")
	assert.Error(t, err2)
}

func TestCreateByOwner(t *testing.T) {
	s, store := newService(t, false)
	ctx := context.Background()
	u, password, err := s.Create(ctx, "bea", "root")
	require.NoError(t, err)
	assert.Equal(t, "root", u.CreatedBy)
	assert.NotEmpty(t, password)
	assert.NotEqual(t, password, store.ClientUsers[u.ID].PasswordHash)

	_, ok, err := s.Authenticate(ctx, "bea", password)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestListAndSetExpiry(t *testing.T) {
	s, store := newService(t, true)
	ctx := context.Background()
	u, err := s.Register(ctx, "ana", strongPassword)
	require.NoError(t, err)

	list, err := s.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "ana", list[0].Username)

	days := 10
	saved, err := s.SetExpiry(ctx, u.ID, false, &days, "root")
	require.NoError(t, err)
	assert.Equal(t, 10, *saved.MaxIdleDays)
	assert.Contains(t, store.Actions(), config.ActionClientExpiryChanged)
}

func TestChangeOwnPassword(t *testing.T) {
	s, _ := newService(t, true)
	ctx := context.Background()
	u, err := s.Register(ctx, "ana", strongPassword)
	require.NoError(t, err)

	err = s.ChangeOwnPassword(ctx, u.ID, "wrong", "new-strong-password-123")
	assert.ErrorIs(t, err, ErrWrongPassword)

	err = s.ChangeOwnPassword(ctx, u.ID, strongPassword, "short")
	assert.ErrorIs(t, err, ErrWeakPassword)

	require.NoError(t, s.ChangeOwnPassword(ctx, u.ID, strongPassword, "new-strong-password-123"))
	_, ok, err := s.Authenticate(ctx, "ana", "new-strong-password-123")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestDelete(t *testing.T) {
	s, store := newService(t, true)
	ctx := context.Background()
	u, err := s.Register(ctx, "ana", strongPassword)
	require.NoError(t, err)

	require.NoError(t, s.Delete(ctx, u.ID, "root"))
	_, ok, err := s.Exists(ctx, "ana")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Contains(t, store.Actions(), config.ActionClientDeleted)
	assert.ErrorIs(t, s.Delete(ctx, u.ID, "root"), model.ErrNotFound)
}
