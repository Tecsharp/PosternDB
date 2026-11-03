// Package clients manages self-service tenants (ClientUser): people who
// register themselves (email + full name + password, or Google), or whom the
// owner adds directly, to use the reduced
// "panel de usuario" — browsing and editing only the data of the databases
// they own, never an admin feature and never another client's data.
//
// It mirrors internal/admins (bcrypt, constant-time login, audit log) but is
// a separate principal: config.RoleClient is never part of the admin role
// hierarchy, so a client session can never reach an admin route, and every
// /api/me/* handler re-checks server-side that a requested database is
// actually theirs (see httpapi.ownershipMiddleware).
package clients

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/model"
	"github.com/tecsharp/posterndb/internal/oauth"
)

const (
	bcryptCost   = 12
	maxEmailLen  = 254
	minNameChars = 2
	maxNameChars = 80
)

// Errors shown to the operator or the registering client.
var (
	ErrEmail               = errors.New("escribe un correo válido")
	ErrFullName            = fmt.Errorf("escribe tu nombre completo (de %d a %d caracteres)", minNameChars, maxNameChars)
	ErrEmailUseNotAccepted = errors.New("debes aceptar cómo usaremos tu correo para crear la cuenta")
	ErrEmailTaken          = errors.New("ya existe una cuenta con ese correo: entra con tu contraseña o con Google")
	ErrWeakPassword        = fmt.Errorf("la contraseña debe tener al menos %d caracteres", config.MinClientPasswordLength)
	ErrWrongPassword       = errors.New("la contraseña actual no es correcta")
	ErrRegisterDisabled    = errors.New("el registro de usuarios está desactivado")
	ErrGoogleUnverified    = errors.New("Google no confirmó que ese correo sea tuyo")
	ErrGoogleMismatch      = errors.New("ese correo ya está vinculado a otra cuenta de Google")
)

// Store is the persistence the service needs.
type Store interface {
	ListClientUsers(ctx context.Context) ([]model.ClientUser, error)
	GetClientUser(ctx context.Context, id int64) (model.ClientUser, error)
	GetClientUserByUsername(ctx context.Context, username string) (model.ClientUser, error)
	GetClientUserByEmail(ctx context.Context, email string) (model.ClientUser, error)
	GetClientUserByGoogleSub(ctx context.Context, sub string) (model.ClientUser, error)
	CreateClientUser(ctx context.Context, n model.NewClientUser) (model.ClientUser, error)
	LinkClientGoogle(ctx context.Context, id int64, sub, fullName string, resetPassword bool) (model.ClientUser, error)
	UpdateClientUserPassword(ctx context.Context, id int64, hash string) error
	TouchClientUserLogin(ctx context.Context, id int64, at time.Time) error
	SetClientUserExpiry(ctx context.Context, id int64, never bool, maxIdleDays *int, actor string) (model.ClientUser, error)
	DeleteClientUser(ctx context.Context, id int64) error
	RecordAudit(ctx context.Context, e model.AuditEntry) error
}

// Passwords generates random passwords for owner-created accounts.
type Passwords interface {
	Password(n int) (string, error)
}

// Service implements client management and auth.ClientStore.
type Service struct {
	store         Store
	passwords     Passwords
	allowRegister bool
	now           func() time.Time
	cost          int
	// dummyHash keeps login timing identical for unknown accounts.
	dummyHash []byte
}

// New builds the service.
func New(store Store, passwords Passwords, allowRegister bool) *Service {
	dummy, _ := bcrypt.GenerateFromPassword([]byte("timing-equaliser"), bcryptCost)
	return &Service{store: store, passwords: passwords, allowRegister: allowRegister, now: time.Now, cost: bcryptCost, dummyHash: dummy}
}

// RegisterInput is the public sign-up form.
type RegisterInput struct {
	Email    string
	FullName string
	Password string
	// AcceptEmailUse records that the person read how the email is used.
	AcceptEmailUse bool
}

func normalizeEmail(raw string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(raw))
	addr, err := mail.ParseAddress(e)
	if err != nil || addr.Address != e || len(e) > maxEmailLen || !strings.Contains(e[strings.LastIndex(e, "@"):], ".") {
		return "", ErrEmail
	}
	return e, nil
}

func normalizeName(raw string) (string, error) {
	n := strings.Join(strings.Fields(raw), " ")
	if c := utf8.RuneCountInString(n); c < minNameChars || c > maxNameChars {
		return "", ErrFullName
	}
	return n, nil
}

func (s *Service) hash(password string) (string, error) {
	if len(password) < config.MinClientPasswordLength {
		return "", ErrWeakPassword
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), s.cost)
	return string(h), err
}

// Register lets a person create their own account (public, rate-limited at
// the HTTP layer). Disabled entirely when ALLOW_SELF_REGISTER=false.
func (s *Service) Register(ctx context.Context, in RegisterInput) (model.ClientUser, error) {
	if !s.allowRegister {
		return model.ClientUser{}, ErrRegisterDisabled
	}
	email, err := normalizeEmail(in.Email)
	if err != nil {
		return model.ClientUser{}, err
	}
	name, err := normalizeName(in.FullName)
	if err != nil {
		return model.ClientUser{}, err
	}
	if !in.AcceptEmailUse {
		return model.ClientUser{}, ErrEmailUseNotAccepted
	}
	hash, err := s.hash(in.Password)
	if err != nil {
		return model.ClientUser{}, err
	}
	u, err := s.store.CreateClientUser(ctx, model.NewClientUser{
		Username: email, Email: email, FullName: name, PasswordHash: hash, EmailUseAccepted: true,
	})
	if err != nil {
		return u, emailTaken(err)
	}
	s.audit(ctx, email, config.ActionClientRegistered, email, map[string]any{"method": "password"})
	return u, nil
}

// Create lets the owner add a client directly, with a generated password
// shown once (mirrors admins.Service.Create).
func (s *Service) Create(ctx context.Context, email, fullName, actor string) (model.ClientUser, string, error) {
	clean, err := normalizeEmail(email)
	if err != nil {
		return model.ClientUser{}, "", err
	}
	name, err := normalizeName(fullName)
	if err != nil {
		return model.ClientUser{}, "", err
	}
	password, err := s.passwords.Password(20)
	if err != nil {
		return model.ClientUser{}, "", err
	}
	hash, err := s.hash(password)
	if err != nil {
		return model.ClientUser{}, "", err
	}
	u, err := s.store.CreateClientUser(ctx, model.NewClientUser{Username: clean, Email: clean, FullName: name, PasswordHash: hash, CreatedBy: actor})
	if err != nil {
		return u, "", emailTaken(err)
	}
	s.audit(ctx, actor, config.ActionClientCreated, clean, nil)
	return u, password, nil
}

func emailTaken(err error) error {
	if errors.Is(err, model.ErrConflict) {
		return ErrEmailTaken
	}
	return err
}

// Authenticate checks a client's password (login by email or username) and,
// on success, touches their last-login timestamp. Accounts that only use
// Google have no password and never pass here.
func (s *Service) Authenticate(ctx context.Context, login, password string) (model.ClientUser, bool, error) {
	u, err := s.store.GetClientUserByUsername(ctx, strings.ToLower(strings.TrimSpace(login)))
	if errors.Is(err, model.ErrNotFound) {
		_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(password))
		return model.ClientUser{}, false, nil
	}
	if err != nil {
		return model.ClientUser{}, false, err
	}
	hash := []byte(u.PasswordHash)
	if u.PasswordHash == "" {
		hash = s.dummyHash
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(password)) != nil || u.PasswordHash == "" {
		return model.ClientUser{}, false, nil
	}
	_ = s.store.TouchClientUserLogin(ctx, u.ID, s.now())
	return u, true, nil
}

// LoginWithGoogle resolves the account for a Google identity, linking or
// creating it as needed:
//
//  1. already linked to this Google account: sign in;
//  2. same email already registered: link it. If that email had never been
//     verified (it was typed into the sign-up form by whoever got there
//     first), the password is removed, so a squatter cannot keep access to
//     an account its real owner just proved with Google;
//  3. otherwise create an account, when self-registration is allowed.
func (s *Service) LoginWithGoogle(ctx context.Context, g oauth.Profile) (model.ClientUser, error) {
	if !g.EmailVerified || g.Subject == "" {
		return model.ClientUser{}, ErrGoogleUnverified
	}
	email, err := normalizeEmail(g.Email)
	if err != nil {
		return model.ClientUser{}, err
	}
	u, err := s.store.GetClientUserByGoogleSub(ctx, g.Subject)
	if err == nil {
		return s.touch(ctx, u), nil
	}
	if !errors.Is(err, model.ErrNotFound) {
		return model.ClientUser{}, err
	}
	existing, err := s.store.GetClientUserByEmail(ctx, email)
	switch {
	case err == nil:
		return s.linkGoogle(ctx, existing, g)
	case !errors.Is(err, model.ErrNotFound):
		return model.ClientUser{}, err
	}
	if !s.allowRegister {
		return model.ClientUser{}, ErrRegisterDisabled
	}
	name := strings.Join(strings.Fields(g.Name), " ")
	if utf8.RuneCountInString(name) < minNameChars {
		name = strings.SplitN(email, "@", 2)[0]
	}
	created, err := s.store.CreateClientUser(ctx, model.NewClientUser{
		Username: email, Email: email, FullName: truncate(name, maxNameChars), GoogleSub: g.Subject,
		EmailVerified: true, EmailUseAccepted: true,
	})
	if err != nil {
		return created, emailTaken(err)
	}
	s.audit(ctx, email, config.ActionClientRegistered, email, map[string]any{"method": "google"})
	return s.touch(ctx, created), nil
}

func (s *Service) linkGoogle(ctx context.Context, existing model.ClientUser, g oauth.Profile) (model.ClientUser, error) {
	if existing.GoogleSub != "" && existing.GoogleSub != g.Subject {
		return model.ClientUser{}, ErrGoogleMismatch
	}
	resetPassword := existing.HasPassword && existing.EmailVerifiedAt == nil
	linked, err := s.store.LinkClientGoogle(ctx, existing.ID, g.Subject, truncate(strings.Join(strings.Fields(g.Name), " "), maxNameChars), resetPassword)
	if err != nil {
		return linked, err
	}
	s.audit(ctx, linked.Username, config.ActionClientGoogleLinked, linked.Username, map[string]any{"passwordRemoved": resetPassword})
	return s.touch(ctx, linked), nil
}

func (s *Service) touch(ctx context.Context, u model.ClientUser) model.ClientUser {
	_ = s.store.TouchClientUserLogin(ctx, u.ID, s.now())
	return u
}

func truncate(v string, n int) string {
	if utf8.RuneCountInString(v) <= n {
		return v
	}
	return string([]rune(v)[:n])
}

// ByID re-verifies a session on every request: deleting a client revokes
// their access immediately, like deleting an admin does.
func (s *Service) ByID(ctx context.Context, id int64) (model.ClientUser, bool, error) {
	u, err := s.store.GetClientUser(ctx, id)
	if errors.Is(err, model.ErrNotFound) {
		return model.ClientUser{}, false, nil
	}
	return u, err == nil, err
}

// List returns every registered client, for the owner-only Usuarios page.
func (s *Service) List(ctx context.Context) ([]model.ClientUser, error) {
	return s.store.ListClientUsers(ctx)
}

// SetExpiry stores the owner's exception for this client.
func (s *Service) SetExpiry(ctx context.Context, id int64, never bool, maxIdleDays *int, actor string) (model.ClientUser, error) {
	saved, err := s.store.SetClientUserExpiry(ctx, id, never, maxIdleDays, actor)
	if err != nil {
		return saved, err
	}
	s.audit(ctx, actor, config.ActionClientExpiryChanged, saved.Username, map[string]any{"neverExpires": never, "maxIdleDays": maxIdleDays})
	return saved, nil
}

// ChangeOwnPassword lets a client pick a new password. An account that only
// uses Google has no current password: signing in with Google is the proof,
// so it can set one without typing a current password.
func (s *Service) ChangeOwnPassword(ctx context.Context, id int64, current, next string) error {
	u, err := s.store.GetClientUser(ctx, id)
	if err != nil {
		return err
	}
	if len(next) < config.MinClientPasswordLength {
		return ErrWeakPassword
	}
	if u.HasPassword {
		if _, ok, err := s.Authenticate(ctx, u.Username, current); err != nil || !ok {
			return errors.Join(ErrWrongPassword, err)
		}
	}
	hash, err := s.hash(next)
	if err != nil {
		return err
	}
	if err := s.store.UpdateClientUserPassword(ctx, id, hash); err != nil {
		return err
	}
	s.audit(ctx, u.Username, config.ActionClientPasswordChanged, u.Username, nil)
	return nil
}

// ResetPassword gives a client a new generated password, shown once to the
// owner. It is the way back in when Google sign-in is not set up.
func (s *Service) ResetPassword(ctx context.Context, id int64, actor string) (string, error) {
	u, err := s.store.GetClientUser(ctx, id)
	if err != nil {
		return "", err
	}
	password, err := s.passwords.Password(20)
	if err != nil {
		return "", err
	}
	hash, err := s.hash(password)
	if err != nil {
		return "", err
	}
	if err := s.store.UpdateClientUserPassword(ctx, id, hash); err != nil {
		return "", err
	}
	s.audit(ctx, actor, config.ActionClientPasswordReset, u.Username, nil)
	return password, nil
}

// Delete removes a client account (owner only). Their databases are kept,
// unassigned, and fall back to the global expiry policy.
func (s *Service) Delete(ctx context.Context, id int64, actor string) error {
	u, err := s.store.GetClientUser(ctx, id)
	if err != nil {
		return err
	}
	if err := s.store.DeleteClientUser(ctx, id); err != nil {
		return err
	}
	s.audit(ctx, actor, config.ActionClientDeleted, u.Username, nil)
	return nil
}

func (s *Service) audit(ctx context.Context, actor, action, target string, details map[string]any) {
	_ = s.store.RecordAudit(ctx, model.AuditEntry{Actor: actor, Action: action, Target: target, Details: details})
}
