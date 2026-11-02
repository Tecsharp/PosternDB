package httpapi

import (
	"net/http"
	"strings"

	"github.com/tecsharp/posterndb/internal/auth"
	"github.com/tecsharp/posterndb/internal/config"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
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
	token, exp, err := s.Auth.Issue(id.Username)
	if err != nil {
		s.internalError(w, "issue session", err)
		return
	}
	auth.SetSessionCookie(w, token, exp, isHTTPS(r))
	s.Databases.RecordAudit(r.Context(), id.Username, config.ActionLoginSuccess, "", map[string]any{"ip": ip})
	writeJSON(w, http.StatusOK, map[string]any{"user": id.Username, "role": id.Role, "expiresAt": exp})
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	auth.ClearSessionCookie(w, isHTTPS(r))
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) session(w http.ResponseWriter, r *http.Request) {
	id := auth.FromContext(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"user": id.Username, "role": id.Role, "bootstrap": s.Auth.IsBootstrap(id.Username),
	})
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
