// Package clients manages self-service tenants (ClientUser): people who
// register themselves, or whom the owner adds directly, to use the reduced
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
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/model"
)

const bcryptCost = 12

var usernamePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{2,31}$`)

// Errors shown to the operator or the registering client.
var (
	ErrUsername         = errors.New("el usuario debe empezar con una letra y tener de 3 a 32 caracteres a-z, 0-9, punto, guion o guion bajo")
	ErrWeakPassword     = fmt.Errorf("la contraseña debe tener al menos %d caracteres", config.MinClientPasswordLength)
	ErrWrongPassword    = errors.New("la contraseña actual no es correcta")
	ErrRegisterDisabled = errors.New("el registro de usuarios está desactivado")
)

// Store is the persistence the service needs.
type Store interface {
	ListClientUsers(ctx context.Context) ([]model.ClientUser, error)
	GetClientUser(ctx context.Context, id int64) (model.ClientUser, error)
	GetClientUserByUsername(ctx context.Context, username string) (model.ClientUser, error)
	CreateClientUser(ctx context.Context, username, passwordHash, createdBy string) (model.ClientUser, error)
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
	// dummyHash keeps login timing identical for unknown usernames.
	dummyHash []byte
}

// New builds the service.
func New(store Store, passwords Passwords, allowRegister bool) *Service {
	dummy, _ := bcrypt.GenerateFromPassword([]byte("timing-equaliser"), bcryptCost)
	return &Service{store: store, passwords: passwords, allowRegister: allowRegister, now: time.Now, cost: bcryptCost, dummyHash: dummy}
}

func normalizeUsername(raw string) (string, error) {
	u := strings.ToLower(strings.TrimSpace(raw))
	if !usernamePattern.MatchString(u) {
		return "", ErrUsername
	}
	return u, nil
}

// Register lets a client create their own account (public, rate-limited at
// the HTTP layer). Disabled entirely when ALLOW_SELF_REGISTER=false.
func (s *Service) Register(ctx context.Context, username, password string) (model.ClientUser, error) {
	if !s.allowRegister {
		return model.ClientUser{}, ErrRegisterDisabled
	}
	return s.create(ctx, username, password, "")
}

// Create lets the owner add a client directly, with a generated password
// shown once (mirrors admins.Service.Create).
func (s *Service) Create(ctx context.Context, username, actor string) (model.ClientUser, string, error) {
	password, err := s.passwords.Password(20)
	if err != nil {
		return model.ClientUser{}, "", err
	}
	u, err := s.create(ctx, username, password, actor)
	return u, password, err
}

func (s *Service) create(ctx context.Context, username, password, createdBy string) (model.ClientUser, error) {
	clean, err := normalizeUsername(username)
	if err != nil {
		return model.ClientUser{}, err
	}
	if len(password) < config.MinClientPasswordLength {
		return model.ClientUser{}, ErrWeakPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), s.cost)
	if err != nil {
		return model.ClientUser{}, err
	}
	u, err := s.store.CreateClientUser(ctx, clean, string(hash), createdBy)
	if err != nil {
		return u, err
	}
	actor := createdBy
	if actor == "" {
		actor = clean
	}
	action := config.ActionClientCreated
	if createdBy == "" {
		action = config.ActionClientRegistered
	}
	s.audit(ctx, actor, action, clean, nil)
	return u, nil
}

// Authenticate checks a client's password and, on success, touches their
// last-login timestamp.
func (s *Service) Authenticate(ctx context.Context, username, password string) (model.ClientUser, bool, error) {
	u, err := s.store.GetClientUserByUsername(ctx, username)
	if errors.Is(err, model.ErrNotFound) {
		_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(password))
		return model.ClientUser{}, false, nil
	}
	if err != nil {
		return model.ClientUser{}, false, err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return model.ClientUser{}, false, nil
	}
	_ = s.store.TouchClientUserLogin(ctx, u.ID, s.now())
	return u, true, nil
}

// Exists reports whether username still has an account, for re-verifying a
// session on every request (consistent with how deleting an admin revokes
// access immediately).
func (s *Service) Exists(ctx context.Context, username string) (int64, bool, error) {
	u, err := s.store.GetClientUserByUsername(ctx, username)
	if errors.Is(err, model.ErrNotFound) {
		return 0, false, nil
	}
	return u.ID, err == nil, err
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

// ChangeOwnPassword lets a client pick a new password.
func (s *Service) ChangeOwnPassword(ctx context.Context, id int64, current, next string) error {
	u, err := s.store.GetClientUser(ctx, id)
	if err != nil {
		return err
	}
	if len(next) < config.MinClientPasswordLength {
		return ErrWeakPassword
	}
	if _, ok, err := s.Authenticate(ctx, u.Username, current); err != nil || !ok {
		return errors.Join(ErrWrongPassword, err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(next), s.cost)
	if err != nil {
		return err
	}
	if err := s.store.UpdateClientUserPassword(ctx, id, string(hash)); err != nil {
		return err
	}
	s.audit(ctx, u.Username, config.ActionClientPasswordChanged, u.Username, nil)
	return nil
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
