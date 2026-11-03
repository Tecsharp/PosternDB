package clients

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/model"
	"github.com/tecsharp/posterndb/internal/oauth"
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

func form(email string) RegisterInput {
	return RegisterInput{Email: email, FullName: "Ana López", Password: strongPassword, AcceptEmailUse: true}
}

func google(sub, email string) oauth.Profile {
	return oauth.Profile{Subject: sub, Email: email, Name: "Ana López", EmailVerified: true}
}

func TestRegisterAndAuthenticate(t *testing.T) {
	s, store := newService(t, true)
	ctx := context.Background()

	u, err := s.Register(ctx, form(" Ana.Lopez@Example.com "))
	require.NoError(t, err)
	assert.Equal(t, "ana.lopez@example.com", u.Username, "the email is the username, lower-cased")
	assert.Equal(t, "ana.lopez@example.com", u.Email)
	assert.Equal(t, "Ana López", u.FullName)
	assert.True(t, u.HasPassword)
	assert.False(t, u.GoogleLinked)
	assert.Nil(t, u.EmailVerifiedAt, "a typed email is not verified")
	assert.Contains(t, store.Actions(), config.ActionClientRegistered)

	got, ok, err := s.Authenticate(ctx, "ANA.LOPEZ@example.com", strongPassword)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, u.ID, got.ID)
	assert.NotNil(t, store.ClientUsers[u.ID].LastLoginAt)

	_, ok, _ = s.Authenticate(ctx, "ana.lopez@example.com", "wrong")
	assert.False(t, ok)
	_, ok, _ = s.Authenticate(ctx, "ghost@example.com", "x")
	assert.False(t, ok)

	byID, ok, err := s.ByID(ctx, u.ID)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "ana.lopez@example.com", byID.Username)
	_, ok, err = s.ByID(ctx, 9999)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestRegisterDisabled(t *testing.T) {
	s, _ := newService(t, false)
	_, err := s.Register(context.Background(), form("ana@example.com"))
	assert.ErrorIs(t, err, ErrRegisterDisabled)
	_, err = s.LoginWithGoogle(context.Background(), google("g-1", "ana@example.com"))
	assert.ErrorIs(t, err, ErrRegisterDisabled, "Google cannot be used to get around a closed sign-up")
}

func TestRegisterValidation(t *testing.T) {
	s, _ := newService(t, true)
	ctx := context.Background()
	cases := map[string]struct {
		in   RegisterInput
		want error
	}{
		"no email":               {RegisterInput{Email: "", FullName: "Ana López", Password: strongPassword, AcceptEmailUse: true}, ErrEmail},
		"not an email":           {RegisterInput{Email: "ana", FullName: "Ana López", Password: strongPassword, AcceptEmailUse: true}, ErrEmail},
		"no domain dot":          {RegisterInput{Email: "ana@localhost", FullName: "Ana López", Password: strongPassword, AcceptEmailUse: true}, ErrEmail},
		"display name form":      {RegisterInput{Email: "Ana <ana@example.com>", FullName: "Ana López", Password: strongPassword, AcceptEmailUse: true}, ErrEmail},
		"short name":             {RegisterInput{Email: "ana@example.com", FullName: "A", Password: strongPassword, AcceptEmailUse: true}, ErrFullName},
		"blank name":             {RegisterInput{Email: "ana@example.com", FullName: "   ", Password: strongPassword, AcceptEmailUse: true}, ErrFullName},
		"weak password":          {RegisterInput{Email: "ana@example.com", FullName: "Ana López", Password: "short", AcceptEmailUse: true}, ErrWeakPassword},
		"email use not accepted": {RegisterInput{Email: "ana@example.com", FullName: "Ana López", Password: strongPassword}, ErrEmailUseNotAccepted},
	}
	for name, c := range cases {
		_, err := s.Register(ctx, c.in)
		assert.ErrorIs(t, err, c.want, name)
	}

	_, err := s.Register(ctx, form("ana@example.com"))
	require.NoError(t, err)
	_, err = s.Register(ctx, form("ANA@example.com"))
	assert.ErrorIs(t, err, ErrEmailTaken, "emails are unique regardless of case")
}

func TestCreateByOwner(t *testing.T) {
	s, store := newService(t, false) // owner can add clients even when sign-up is closed
	ctx := context.Background()

	u, password, err := s.Create(ctx, "bea@example.com", "Bea Ruiz", "root")
	require.NoError(t, err)
	assert.Equal(t, "root", u.CreatedBy)
	assert.Equal(t, "Bea Ruiz", u.FullName)
	assert.GreaterOrEqual(t, len(password), config.MinClientPasswordLength)
	assert.Contains(t, store.Actions(), config.ActionClientCreated)

	_, ok, _ := s.Authenticate(ctx, "bea@example.com", password)
	assert.True(t, ok)

	_, _, err = s.Create(ctx, "bea@example.com", "Bea Ruiz", "root")
	assert.ErrorIs(t, err, ErrEmailTaken)
	_, _, err = s.Create(ctx, "nope", "Bea Ruiz", "root")
	assert.ErrorIs(t, err, ErrEmail)
}

func TestGoogleCreatesAccountWithoutPassword(t *testing.T) {
	s, store := newService(t, true)
	ctx := context.Background()

	u, err := s.LoginWithGoogle(ctx, google("g-1", "Ana@Example.com"))
	require.NoError(t, err)
	assert.Equal(t, "ana@example.com", u.Username)
	assert.Equal(t, "Ana López", u.FullName)
	assert.True(t, u.GoogleLinked)
	assert.False(t, u.HasPassword)
	assert.NotNil(t, u.EmailVerifiedAt)
	assert.Contains(t, store.Actions(), config.ActionClientRegistered)

	_, ok, _ := s.Authenticate(ctx, "ana@example.com", "")
	assert.False(t, ok, "a Google-only account has no password to guess")
	_, ok, _ = s.Authenticate(ctx, "ana@example.com", strongPassword)
	assert.False(t, ok)

	again, err := s.LoginWithGoogle(ctx, google("g-1", "ana@example.com"))
	require.NoError(t, err)
	assert.Equal(t, u.ID, again.ID, "same Google account, same user")
}

func TestGoogleRejectsUnverifiedEmail(t *testing.T) {
	s, _ := newService(t, true)
	g := google("g-1", "ana@example.com")
	g.EmailVerified = false
	_, err := s.LoginWithGoogle(context.Background(), g)
	assert.ErrorIs(t, err, ErrGoogleUnverified)
	_, err = s.LoginWithGoogle(context.Background(), oauth.Profile{Email: "ana@example.com", EmailVerified: true})
	assert.ErrorIs(t, err, ErrGoogleUnverified, "no subject, no identity")
}

func TestGoogleFallsBackToEmailPrefixWhenNameIsMissing(t *testing.T) {
	s, _ := newService(t, true)
	u, err := s.LoginWithGoogle(context.Background(), oauth.Profile{Subject: "g-2", Email: "carlos@example.com", EmailVerified: true})
	require.NoError(t, err)
	assert.Equal(t, "carlos", u.FullName)
}

func TestGoogleLinksExistingAccountAndDropsAnUnverifiedPassword(t *testing.T) {
	s, store := newService(t, true)
	ctx := context.Background()
	// Whoever typed this email first set a password, without proving the email.
	squatted, err := s.Register(ctx, form("ana@example.com"))
	require.NoError(t, err)

	linked, err := s.LoginWithGoogle(ctx, google("g-1", "ana@example.com"))
	require.NoError(t, err)
	assert.Equal(t, squatted.ID, linked.ID, "same account, now linked")
	assert.True(t, linked.GoogleLinked)
	assert.False(t, linked.HasPassword, "the password set before the email was proven is removed")
	assert.NotNil(t, linked.EmailVerifiedAt)
	assert.Contains(t, store.Actions(), config.ActionClientGoogleLinked)

	_, ok, _ := s.Authenticate(ctx, "ana@example.com", strongPassword)
	assert.False(t, ok, "the old password no longer works")

	// With Google proof they can set a new password without a current one.
	require.NoError(t, s.ChangeOwnPassword(ctx, linked.ID, "", "another long password"))
	_, ok, _ = s.Authenticate(ctx, "ana@example.com", "another long password")
	assert.True(t, ok)
}

func TestGoogleKeepsThePasswordOfAVerifiedAccount(t *testing.T) {
	s, _ := newService(t, true)
	ctx := context.Background()
	first, err := s.LoginWithGoogle(ctx, google("g-1", "ana@example.com"))
	require.NoError(t, err)
	require.NoError(t, s.ChangeOwnPassword(ctx, first.ID, "", strongPassword))

	again, err := s.LoginWithGoogle(ctx, google("g-1", "ana@example.com"))
	require.NoError(t, err)
	assert.True(t, again.HasPassword, "signing in with Google again does not remove a password the user chose")
	_, ok, _ := s.Authenticate(ctx, "ana@example.com", strongPassword)
	assert.True(t, ok)
}

func TestGoogleRefusesADifferentGoogleAccountForTheSameEmail(t *testing.T) {
	s, _ := newService(t, true)
	ctx := context.Background()
	_, err := s.LoginWithGoogle(ctx, google("g-1", "ana@example.com"))
	require.NoError(t, err)

	_, err = s.LoginWithGoogle(ctx, google("g-OTHER", "ana@example.com"))
	assert.ErrorIs(t, err, ErrGoogleMismatch)
}

func TestListAndSetExpiry(t *testing.T) {
	s, store := newService(t, true)
	ctx := context.Background()
	a, err := s.Register(ctx, form("ana@example.com"))
	require.NoError(t, err)
	_, _, err = s.Create(ctx, "bea@example.com", "Bea Ruiz", "root")
	require.NoError(t, err)

	list, err := s.List(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 2)

	days := 90
	saved, err := s.SetExpiry(ctx, a.ID, false, &days, "root")
	require.NoError(t, err)
	require.NotNil(t, saved.MaxIdleDays)
	assert.Equal(t, 90, *saved.MaxIdleDays)
	assert.Contains(t, store.Actions(), config.ActionClientExpiryChanged)

	_, err = s.SetExpiry(ctx, 9999, true, nil, "root")
	assert.ErrorIs(t, err, model.ErrNotFound)
}

func TestChangeOwnPassword(t *testing.T) {
	s, _ := newService(t, true)
	ctx := context.Background()
	u, err := s.Register(ctx, form("ana@example.com"))
	require.NoError(t, err)

	assert.ErrorIs(t, s.ChangeOwnPassword(ctx, u.ID, "wrong", "a brand new password"), ErrWrongPassword)
	assert.ErrorIs(t, s.ChangeOwnPassword(ctx, u.ID, strongPassword, "short"), ErrWeakPassword)
	require.NoError(t, s.ChangeOwnPassword(ctx, u.ID, strongPassword, "a brand new password"))

	_, ok, _ := s.Authenticate(ctx, "ana@example.com", strongPassword)
	assert.False(t, ok)
	_, ok, _ = s.Authenticate(ctx, "ana@example.com", "a brand new password")
	assert.True(t, ok)
	assert.ErrorIs(t, s.ChangeOwnPassword(ctx, 9999, "x", "a brand new password"), model.ErrNotFound)
}

func TestOwnerResetsAPassword(t *testing.T) {
	s, store := newService(t, true)
	ctx := context.Background()
	u, err := s.Register(ctx, form("ana@example.com"))
	require.NoError(t, err)

	password, err := s.ResetPassword(ctx, u.ID, "root")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(password), config.MinClientPasswordLength)
	assert.Contains(t, store.Actions(), config.ActionClientPasswordReset)
	_, ok, _ := s.Authenticate(ctx, "ana@example.com", strongPassword)
	assert.False(t, ok)
	_, ok, _ = s.Authenticate(ctx, "ana@example.com", password)
	assert.True(t, ok)

	_, err = s.ResetPassword(ctx, 9999, "root")
	assert.ErrorIs(t, err, model.ErrNotFound)
}

func TestDelete(t *testing.T) {
	s, store := newService(t, true)
	ctx := context.Background()
	u, err := s.Register(ctx, form("ana@example.com"))
	require.NoError(t, err)

	require.NoError(t, s.Delete(ctx, u.ID, "root"))
	_, ok, err := s.ByID(ctx, u.ID)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Contains(t, store.Actions(), config.ActionClientDeleted)
	assert.ErrorIs(t, s.Delete(ctx, u.ID, "root"), model.ErrNotFound)
}
