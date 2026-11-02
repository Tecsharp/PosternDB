// Package admins manages panel administrators stored in the database.
//
// The ADMIN_USER from .env is the bootstrap owner: it always exists, always
// has the owner role and cannot be edited from the panel, so there is no way
// to lock everyone out. Additional administrators get a generated password
// (shown once) hashed with bcrypt.
package admins

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

const (
	bcryptCost        = 12
	generatedPassword = 20
	minPassword       = 12
)

var usernamePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{2,31}$`)

// Errors shown to the operator.
var (
	ErrUsername      = errors.New("el usuario debe empezar con una letra y tener de 3 a 32 caracteres a-z, 0-9, punto, guion o guion bajo")
	ErrRole          = errors.New("rol inválido")
	ErrBootstrap     = errors.New("el administrador de .env no se puede modificar desde el panel")
	ErrSelf          = errors.New("no puedes eliminarte ni cambiar tu propio rol")
	ErrWeakPassword  = fmt.Errorf("la contraseña debe tener al menos %d caracteres", minPassword)
	ErrWrongPassword = errors.New("la contraseña actual no es correcta")
)

// Store is the persistence the service needs.
type Store interface {
	ListAdmins(ctx context.Context) ([]model.Admin, error)
	GetAdmin(ctx context.Context, username string) (model.Admin, error)
	CreateAdmin(ctx context.Context, a model.Admin) (model.Admin, error)
	UpdateAdminRole(ctx context.Context, username, role string) error
	UpdateAdminPassword(ctx context.Context, username, hash string) error
	TouchAdminLogin(ctx context.Context, username string, at time.Time) error
	DeleteAdmin(ctx context.Context, username string) error
	RecordAudit(ctx context.Context, e model.AuditEntry) error
}

// Passwords generates random passwords.
type Passwords interface {
	Password(n int) (string, error)
}

// Service implements administrator management and auth.AdminStore.
type Service struct {
	store     Store
	passwords Passwords
	bootstrap string
	now       func() time.Time
	cost      int
	// dummyHash keeps login timing identical for unknown usernames.
	dummyHash []byte
}

// New builds the service. bootstrap is ADMIN_USER from .env.
func New(store Store, passwords Passwords, bootstrap string) *Service {
	dummy, _ := bcrypt.GenerateFromPassword([]byte("timing-equaliser"), bcryptCost)
	return &Service{store: store, passwords: passwords, bootstrap: bootstrap, now: time.Now, cost: bcryptCost, dummyHash: dummy}
}

// ValidRole reports whether role is one of owner/admin/viewer.
func ValidRole(role string) bool {
	return role == config.RoleOwner || role == config.RoleAdmin || role == config.RoleViewer
}

// Authenticate checks a stored administrator's password.
func (s *Service) Authenticate(ctx context.Context, username, password string) (string, bool, error) {
	a, err := s.store.GetAdmin(ctx, username)
	if errors.Is(err, model.ErrNotFound) {
		_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(password))
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if bcrypt.CompareHashAndPassword([]byte(a.PasswordHash), []byte(password)) != nil {
		return "", false, nil
	}
	_ = s.store.TouchAdminLogin(ctx, username, s.now())
	return a.Role, true, nil
}

// RoleOf returns the current role of a stored administrator.
func (s *Service) RoleOf(ctx context.Context, username string) (string, error) {
	a, err := s.store.GetAdmin(ctx, username)
	return a.Role, err
}

// List returns the bootstrap owner followed by stored administrators.
func (s *Service) List(ctx context.Context) ([]model.Admin, error) {
	stored, err := s.store.ListAdmins(ctx)
	if err != nil {
		return nil, err
	}
	out := []model.Admin{{Username: s.bootstrap, Role: config.RoleOwner, CreatedBy: ".env", Bootstrap: true}}
	return append(out, stored...), nil
}

// Create adds an administrator and returns its generated password once.
func (s *Service) Create(ctx context.Context, username, role, actor string) (model.Admin, string, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if !usernamePattern.MatchString(username) {
		return model.Admin{}, "", ErrUsername
	}
	if username == s.bootstrap {
		return model.Admin{}, "", fmt.Errorf("admin %q: %w", username, model.ErrConflict)
	}
	if !ValidRole(role) {
		return model.Admin{}, "", ErrRole
	}
	password, err := s.passwords.Password(generatedPassword)
	if err != nil {
		return model.Admin{}, "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), s.cost)
	if err != nil {
		return model.Admin{}, "", err
	}
	a, err := s.store.CreateAdmin(ctx, model.Admin{Username: username, PasswordHash: string(hash), Role: role, CreatedBy: actor})
	if err != nil {
		return a, "", err
	}
	s.audit(ctx, actor, config.ActionAdminCreated, username, map[string]any{"role": role})
	return a, password, nil
}

// SetRole changes another administrator's role.
func (s *Service) SetRole(ctx context.Context, username, role, actor string) error {
	if err := s.editable(username, actor); err != nil {
		return err
	}
	if !ValidRole(role) {
		return ErrRole
	}
	if err := s.store.UpdateAdminRole(ctx, username, role); err != nil {
		return err
	}
	s.audit(ctx, actor, config.ActionAdminUpdated, username, map[string]any{"role": role})
	return nil
}

// ResetPassword gives another administrator a new generated password.
func (s *Service) ResetPassword(ctx context.Context, username, actor string) (string, error) {
	if username == s.bootstrap {
		return "", ErrBootstrap
	}
	password, err := s.passwords.Password(generatedPassword)
	if err != nil {
		return "", err
	}
	if err := s.setPassword(ctx, username, password); err != nil {
		return "", err
	}
	s.audit(ctx, actor, config.ActionAdminUpdated, username, map[string]any{"passwordReset": true})
	return password, nil
}

// ChangeOwnPassword lets a stored administrator pick a new password.
func (s *Service) ChangeOwnPassword(ctx context.Context, username, current, next string) error {
	if username == s.bootstrap {
		return ErrBootstrap
	}
	if len(next) < minPassword {
		return ErrWeakPassword
	}
	if _, ok, err := s.Authenticate(ctx, username, current); err != nil || !ok {
		return errors.Join(ErrWrongPassword, err)
	}
	if err := s.setPassword(ctx, username, next); err != nil {
		return err
	}
	s.audit(ctx, username, config.ActionAdminUpdated, username, map[string]any{"passwordChanged": true})
	return nil
}

// Delete removes another administrator.
func (s *Service) Delete(ctx context.Context, username, actor string) error {
	if err := s.editable(username, actor); err != nil {
		return err
	}
	if err := s.store.DeleteAdmin(ctx, username); err != nil {
		return err
	}
	s.audit(ctx, actor, config.ActionAdminDeleted, username, nil)
	return nil
}

func (s *Service) editable(username, actor string) error {
	if username == s.bootstrap {
		return ErrBootstrap
	}
	if username == actor {
		return ErrSelf
	}
	return nil
}

func (s *Service) setPassword(ctx context.Context, username, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), s.cost)
	if err != nil {
		return err
	}
	return s.store.UpdateAdminPassword(ctx, username, string(hash))
}

func (s *Service) audit(ctx context.Context, actor, action, target string, details map[string]any) {
	_ = s.store.RecordAudit(ctx, model.AuditEntry{Actor: actor, Action: action, Target: target, Details: details})
}
