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
	assert.Equal(t, "ana@example.com", u.Username, "the email is the username until they pick one")
	assert.Equal(t, "Ana López", u.FullName)
	assert.True(t, u.GoogleLinked)
	assert.False(t, u.HasPassword)
	assert.True(t, u.NeedsUsername, "a new Google account must choose a username")
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

// The email exists because someone registered it with a password. Proving the
// address through Google must NOT hand that account over: it may have been
// created by a third party, so the owner links Google from inside the account.
func TestGoogleDoesNotEnterAnExistingUnlinkedAccount(t *testing.T) {
	s, store := newService(t, true)
	ctx := context.Background()
	existing, err := s.Register(ctx, form("ana@example.com"))
	require.NoError(t, err)

	_, err = s.LoginWithGoogle(ctx, google("g-1", "ana@example.com"))
	assert.ErrorIs(t, err, ErrGoogleNotLinked)

	after := store.ClientUsers[existing.ID]
	assert.False(t, after.GoogleLinked, "nothing was linked behind the owner's back")
	assert.True(t, after.HasPassword, "and the password is untouched")
	_, ok, _ := s.Authenticate(ctx, "ana@example.com", strongPassword)
	assert.True(t, ok)
	assert.Len(t, store.ClientUsers, 1, "no second account was created")
}

func TestLinkingGoogleFromInsideTheAccount(t *testing.T) {
	s, store := newService(t, true)
	ctx := context.Background()
	u, err := s.Register(ctx, form("ana@example.com"))
	require.NoError(t, err)

	linked, err := s.LinkGoogle(ctx, u.ID, google("g-1", "ANA@example.com"))
	require.NoError(t, err)
	assert.True(t, linked.GoogleLinked)
	assert.True(t, linked.HasPassword, "linking keeps the password")
	assert.NotNil(t, linked.EmailVerifiedAt, "Google vouched for the account's own email")
	assert.Contains(t, store.Actions(), config.ActionClientGoogleLinked)

	again, err := s.LoginWithGoogle(ctx, google("g-1", "ana@example.com"))
	require.NoError(t, err, "after linking, Google signs them in")
	assert.Equal(t, u.ID, again.ID)

	idem, err := s.LinkGoogle(ctx, u.ID, google("g-1", "ana@example.com"))
	require.NoError(t, err)
	assert.Equal(t, u.ID, idem.ID, "linking the same Google account twice is harmless")
}

func TestLinkingADifferentGoogleEmailDoesNotVerifyTheAccountEmail(t *testing.T) {
	s, _ := newService(t, true)
	ctx := context.Background()
	u, err := s.Register(ctx, form("ana@work.example"))
	require.NoError(t, err)

	linked, err := s.LinkGoogle(ctx, u.ID, google("g-1", "ana.personal@gmail.com"))
	require.NoError(t, err)
	assert.True(t, linked.GoogleLinked)
	assert.Nil(t, linked.EmailVerifiedAt, "Google proved another address, not this one")

	in, err := s.LoginWithGoogle(ctx, google("g-1", "ana.personal@gmail.com"))
	require.NoError(t, err)
	assert.Equal(t, u.ID, in.ID, "the link is by Google's id, not by email")
}

func TestLinkingGoogleRefusals(t *testing.T) {
	s, _ := newService(t, true)
	ctx := context.Background()
	ana, err := s.Register(ctx, form("ana@example.com"))
	require.NoError(t, err)
	bea, err := s.Register(ctx, form("bea@example.com"))
	require.NoError(t, err)
	_, err = s.LinkGoogle(ctx, ana.ID, google("g-ana", "ana@example.com"))
	require.NoError(t, err)

	_, err = s.LinkGoogle(ctx, bea.ID, google("g-ana", "ana@example.com"))
	assert.ErrorIs(t, err, ErrGoogleTaken, "a Google account belongs to one user")
	_, err = s.LinkGoogle(ctx, ana.ID, google("g-other", "ana@example.com"))
	assert.ErrorIs(t, err, ErrGoogleMismatch, "an account links one Google account")
	g := google("g-bea", "bea@example.com")
	g.EmailVerified = false
	_, err = s.LinkGoogle(ctx, bea.ID, g)
	assert.ErrorIs(t, err, ErrGoogleUnverified)
	_, err = s.LinkGoogle(ctx, 9999, google("g-x", "x@example.com"))
	assert.ErrorIs(t, err, model.ErrNotFound)
}

func TestGoogleOnEmailLinkedToAnotherGoogleAccount(t *testing.T) {
	s, _ := newService(t, true)
	ctx := context.Background()
	_, err := s.LoginWithGoogle(ctx, google("g-1", "ana@example.com"))
	require.NoError(t, err)

	_, err = s.LoginWithGoogle(ctx, google("g-OTHER", "ana@example.com"))
	assert.ErrorIs(t, err, ErrGoogleMismatch)
}

func TestGoogleOnlyAccountCanCreateAPasswordLater(t *testing.T) {
	s, _ := newService(t, true)
	ctx := context.Background()
	u, err := s.LoginWithGoogle(ctx, google("g-1", "ana@example.com"))
	require.NoError(t, err)

	require.NoError(t, s.ChangeOwnPassword(ctx, u.ID, "", strongPassword))
	_, ok, _ := s.Authenticate(ctx, "ana@example.com", strongPassword)
	assert.True(t, ok)
	again, err := s.LoginWithGoogle(ctx, google("g-1", "ana@example.com"))
	require.NoError(t, err)
	assert.True(t, again.HasPassword, "signing in with Google again keeps the password")
}

func TestUsernameChoice(t *testing.T) {
	s, store := newService(t, true)
	s.reserved = func(_ context.Context, name string) (bool, error) { return name == "admin" || name == "operator1", nil }
	ctx := context.Background()
	u, err := s.LoginWithGoogle(ctx, google("g-1", "ana@example.com"))
	require.NoError(t, err)
	require.True(t, u.NeedsUsername)

	for name, want := range map[string]error{
		"":                ErrUsername,
		"ab":              ErrUsername,
		"1ana":            ErrUsername,
		"ana lopez":       ErrUsername,
		"ana@example.com": ErrUsername,
		"admin":           ErrUsernameTaken,
		"operator1":       ErrUsernameTaken,
	} {
		_, err := s.SetUsername(ctx, u.ID, name)
		assert.ErrorIs(t, err, want, "%q", name)
	}

	chosen, err := s.SetUsername(ctx, u.ID, "  Ana.Lopez ")
	require.NoError(t, err)
	assert.Equal(t, "ana.lopez", chosen.Username)
	assert.False(t, chosen.NeedsUsername)
	assert.Equal(t, "ana@example.com", chosen.Email, "the email is always kept")
	assert.Contains(t, store.Actions(), config.ActionClientUsernameChanged)

	// Password sign-in works with the new username and with the email.
	require.NoError(t, s.ChangeOwnPassword(ctx, u.ID, "", strongPassword))
	for _, login := range []string{"ana.lopez", "ANA.LOPEZ", "ana@example.com"} {
		got, ok, err := s.Authenticate(ctx, login, strongPassword)
		require.NoError(t, err, login)
		assert.True(t, ok, login)
		assert.Equal(t, u.ID, got.ID)
	}

	other, err := s.Register(ctx, form("bea@example.com"))
	require.NoError(t, err)
	_, err = s.SetUsername(ctx, other.ID, "ana.lopez")
	assert.ErrorIs(t, err, ErrUsernameTaken, "usernames are unique")
	_, err = s.SetUsername(ctx, 9999, "nobody")
	assert.ErrorIs(t, err, model.ErrNotFound)
}

func TestSetFullName(t *testing.T) {
	s, _ := newService(t, true)
	ctx := context.Background()
	u, err := s.Register(ctx, form("ana@example.com"))
	require.NoError(t, err)

	got, err := s.SetFullName(ctx, u.ID, "  Ana   María  López ")
	require.NoError(t, err)
	assert.Equal(t, "Ana María López", got.FullName)
	_, err = s.SetFullName(ctx, u.ID, "A")
	assert.ErrorIs(t, err, ErrFullName)
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
