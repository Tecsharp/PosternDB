package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/tecsharp/posterndb/internal/auth"
	"github.com/tecsharp/posterndb/internal/clients"
	"github.com/tecsharp/posterndb/internal/config"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	// Remember keeps the session open for days instead of ending with the browser.
	Remember bool `json:"remember"`
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	// Values in .env are trimmed when loaded, so trim here too: credentials
	// pasted from a terminal often carry a trailing space or newline.
	req.Username = strings.TrimSpace(req.Username)
	req.Password = strings.TrimSpace(req.Password)
	ip := auth.ClientIP(r)
	id, ok, err := s.Auth.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		s.internalError(w, "login", err)
		return
	}
	if !ok {
		// The attempted username is not logged: users sometimes type their
		// password into the username field.
		s.Databases.RecordAudit(r.Context(), config.ActorSystem, config.ActionLoginFailed, "", map[string]any{"ip": ip})
		writeError(w, http.StatusUnauthorized, "usuario o contraseña incorrectos")
		return
	}
	token, exp, err := s.Auth.IssueFor(id.Subject(), s.Auth.SessionTTL(req.Remember))
	if err != nil {
		s.internalError(w, "issue session", err)
		return
	}
	auth.SetSessionCookie(w, token, exp, req.Remember, isHTTPS(r))
	s.Databases.RecordAudit(r.Context(), id.Username, config.ActionLoginSuccess, "", map[string]any{"ip": ip})
	writeJSON(w, http.StatusOK, map[string]any{"user": id.Username, "role": id.Role, "expiresAt": exp})
}

// register lets a client create their own account, publicly and without an
// admin. s.Clients.Register itself refuses when ALLOW_SELF_REGISTER=false.
type registerRequest struct {
	Email          string `json:"email"`
	FullName       string `json:"fullName"`
	Password       string `json:"password"`
	AcceptEmailUse bool   `json:"acceptEmailUse"`
}

func (s *server) register(w http.ResponseWriter, r *http.Request) {
	if s.Settings.AuthMode == config.AuthModeGoogle {
		writeError(w, http.StatusBadRequest, "el registro con contraseña está desactivado: usa Continuar con Google")
		return
	}
	var req registerRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	u, err := s.Clients.Register(r.Context(), clients.RegisterInput{
		Email: req.Email, FullName: req.FullName, Password: req.Password, AcceptEmailUse: req.AcceptEmailUse,
	})
	if err != nil {
		s.domainError(w, "register", err)
		return
	}
	token, exp, err := s.Auth.Issue(auth.ClientIdentity(u).Subject())
	if err != nil {
		s.internalError(w, "issue session", err)
		return
	}
	auth.SetSessionCookie(w, token, exp, false, isHTTPS(r))
	writeJSON(w, http.StatusCreated, map[string]any{"user": u.Username, "role": config.RoleClient, "expiresAt": exp})
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	auth.ClearSessionCookie(w, isHTTPS(r))
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) session(w http.ResponseWriter, r *http.Request) {
	id := auth.FromContext(r.Context())
	out := map[string]any{
		"user": id.Username, "role": id.Role, "bootstrap": id.Role == config.RoleOwner && s.Auth.IsBootstrap(id.Username),
	}
	if id.ClientUserID != nil {
		if u, ok, err := s.Clients.ByID(r.Context(), *id.ClientUserID); err == nil && ok {
			out["email"], out["fullName"], out["hasPassword"] = u.Email, u.FullName, u.HasPassword
			out["needsUsername"], out["googleLinked"] = u.NeedsUsername, u.GoogleLinked
		}
	}
	writeJSON(w, http.StatusOK, out)
}

type passwordChange struct {
	Current string `json:"current"`
	Next    string `json:"next"`
}

func (s *server) changeOwnPassword(w http.ResponseWriter, r *http.Request) {
	var req passwordChange
	if !decodeJSON(w, r, &req) {
		return
	}
	s.noContent(w, "change password", s.Admins.ChangeOwnPassword(r.Context(), actor(r), req.Current, req.Next))
}

func (s *server) changeOwnClientPassword(w http.ResponseWriter, r *http.Request) {
	var req passwordChange
	if !decodeJSON(w, r, &req) {
		return
	}
	id := auth.FromContext(r.Context()).ClientUserID
	if id == nil {
		s.internalError(w, "change client password", errors.New("missing client id in session"))
		return
	}
	s.noContent(w, "change client password", s.Clients.ChangeOwnPassword(r.Context(), *id, req.Current, req.Next))
}

// ---- administrators (owner only) ------------------------------------------------

func (s *server) listAdmins(w http.ResponseWriter, r *http.Request) {
	list, err := s.Admins.List(r.Context())
	s.respond(w, "list admins", map[string]any{"admins": list}, err)
}

type adminRequest struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

func (s *server) createAdmin(w http.ResponseWriter, r *http.Request) {
	var req adminRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	a, password, err := s.Admins.Create(r.Context(), req.Username, req.Role, actor(r))
	s.secret(w, http.StatusCreated, "create admin", map[string]any{"admin": a, "password": password}, err)
}

func (s *server) updateAdmin(w http.ResponseWriter, r *http.Request) {
	var req adminRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	s.noContent(w, "update admin", s.Admins.SetRole(r.Context(), urlName(r), req.Role, actor(r)))
}

func (s *server) resetAdminPassword(w http.ResponseWriter, r *http.Request) {
	password, err := s.Admins.ResetPassword(r.Context(), urlName(r), actor(r))
	s.secret(w, http.StatusOK, "reset admin password", map[string]string{"password": password}, err)
}

func (s *server) deleteAdmin(w http.ResponseWriter, r *http.Request) {
	s.noContent(w, "delete admin", s.Admins.Delete(r.Context(), urlName(r), actor(r)))
}

// ---- usuarios / clientes (owner only) --------------------------------------------

func (s *server) listClients(w http.ResponseWriter, r *http.Request) {
	list, err := s.Databases.ListClients(r.Context())
	s.respond(w, "list clients", map[string]any{"clients": list}, err)
}

type clientRequest struct {
	Email    string `json:"email"`
	FullName string `json:"fullName"`
}

func (s *server) createClient(w http.ResponseWriter, r *http.Request) {
	var req clientRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	u, password, err := s.Clients.Create(r.Context(), req.Email, req.FullName, actor(r))
	s.secret(w, http.StatusCreated, "create client", map[string]any{"client": u, "password": password}, err)
}

func (s *server) resetClientPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	password, err := s.Clients.ResetPassword(r.Context(), id, actor(r))
	s.secret(w, http.StatusOK, "reset client password", map[string]string{"password": password}, err)
}

func (s *server) deleteClient(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	s.noContent(w, "delete client", s.Clients.Delete(r.Context(), id, actor(r)))
}

type clientExpiryRequest struct {
	NeverExpires bool `json:"neverExpires"`
	MaxIdleDays  *int `json:"maxIdleDays"`
}

func (s *server) setClientExpiry(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req clientExpiryRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	saved, err := s.Databases.SetClientExpiry(r.Context(), id, req.NeverExpires, req.MaxIdleDays, actor(r))
	s.respond(w, "set client expiry", saved, err)
}
