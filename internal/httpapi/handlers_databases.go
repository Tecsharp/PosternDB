package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/model"
	"github.com/tecsharp/posterndb/internal/provision"
)

func urlSlug(r *http.Request) string { return chi.URLParam(r, config.URLParamSlug) }
func urlName(r *http.Request) string { return chi.URLParam(r, config.URLParamName) }

func (s *server) listDatabases(w http.ResponseWriter, r *http.Request) {
	dbs, err := s.Databases.List(r.Context())
	s.respond(w, "list databases", map[string]any{"databases": dbs}, err)
}

func (s *server) getDatabase(w http.ResponseWriter, r *http.Request) {
	db, err := s.Databases.Get(r.Context(), urlSlug(r))
	s.respond(w, "get database", db, err)
}

func (s *server) createDatabase(w http.ResponseWriter, r *http.Request) {
	var req provision.CreateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	creds, err := s.Databases.Create(r.Context(), req, actor(r))
	// Credentials are returned exactly once; the password is not stored.
	s.secret(w, http.StatusCreated, "create database", creds, err)
}

type deleteRequest struct {
	Confirm string `json:"confirm"`
}

// deleteDatabase requires the client to echo the database name in the body.
// The panel enforces this in the dialog too, but checking it server-side
// guarantees no script or misclick can delete without explicit intent.
func (s *server) deleteDatabase(w http.ResponseWriter, r *http.Request) {
	slug := urlSlug(r)
	var req deleteRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Confirm != slug {
		writeError(w, http.StatusBadRequest, "la confirmación no coincide con el nombre de la base de datos")
		return
	}
	s.noContent(w, "delete database", s.Databases.Delete(r.Context(), slug, actor(r)))
}

func (s *server) connection(w http.ResponseWriter, r *http.Request) {
	db, err := s.Databases.Get(r.Context(), urlSlug(r))
	var info model.ConnectionInfo
	if err == nil {
		info = s.Databases.ConnectionInfo(db.ClientDatabase)
	}
	s.respond(w, "connection info", info, err)
}

func (s *server) resetPassword(w http.ResponseWriter, r *http.Request) {
	creds, err := s.Databases.ResetPassword(r.Context(), urlSlug(r), actor(r))
	s.secret(w, http.StatusOK, "reset password", creds, err)
}

func (s *server) setExpiry(w http.ResponseWriter, r *http.Request) {
	var req provision.ExpiryRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	db, err := s.Databases.SetExpiry(r.Context(), urlSlug(r), req, actor(r))
	s.respond(w, "set expiry", db, err)
}

type extendRequest struct {
	Days int `json:"days"`
}

func (s *server) extend(w http.ResponseWriter, r *http.Request) {
	var req extendRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	db, err := s.Databases.Extend(r.Context(), urlSlug(r), req.Days, actor(r))
	s.respond(w, "extend expiry", db, err)
}

// ---- global inactivity policy (owner only) ------------------------------------

func (s *server) getGlobalExpiry(w http.ResponseWriter, r *http.Request) {
	st, err := s.Databases.GlobalSettings(r.Context())
	s.respond(w, "get global expiry", st, err)
}

type globalExpiryRequest struct {
	ExpirationDays   *int `json:"expirationDays"`
	ExpiringSoonDays *int `json:"expiringSoonDays"`
}

func (s *server) setGlobalExpiry(w http.ResponseWriter, r *http.Request) {
	var req globalExpiryRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	st, err := s.Databases.SetGlobalSettings(r.Context(), req.ExpirationDays, req.ExpiringSoonDays, actor(r))
	s.respond(w, "set global expiry", st, err)
}

func (s *server) stats(w http.ResponseWriter, r *http.Request) {
	st, err := s.Databases.Stats(r.Context(), urlSlug(r))
	s.respond(w, "stats", st, err)
}

func (s *server) liveConnections(w http.ResponseWriter, r *http.Request) {
	list, err := s.Databases.Connections(r.Context(), urlSlug(r))
	s.respond(w, "live connections", map[string]any{"connections": list}, err)
}

func (s *server) schema(w http.ResponseWriter, r *http.Request) {
	sc, err := s.Databases.Schema(r.Context(), urlSlug(r))
	s.respond(w, "schema", sc, err)
}

func (s *server) previewRows(w http.ResponseWriter, r *http.Request) {
	res, err := s.Databases.Preview(r.Context(), urlSlug(r), chi.URLParam(r, config.URLParamSchema), chi.URLParam(r, config.URLParamTable))
	s.respond(w, "preview rows", res, err)
}

func (s *server) countRows(w http.ResponseWriter, r *http.Request) {
	n, err := s.Databases.CountRows(r.Context(), urlSlug(r), chi.URLParam(r, config.URLParamSchema), chi.URLParam(r, config.URLParamTable))
	s.respond(w, "count rows", map[string]int64{"rows": n}, err)
}

type queryRequest struct {
	SQL string `json:"sql"`
}

func (s *server) query(w http.ResponseWriter, r *http.Request) {
	var req queryRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := s.Databases.Query(r.Context(), urlSlug(r), req.SQL, actor(r))
	s.respond(w, "query", res, err)
}

// ---- additional users --------------------------------------------------------

func (s *server) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.Databases.ListUsers(r.Context(), urlSlug(r))
	s.respond(w, "list users", map[string]any{"users": users}, err)
}

type userRequest struct {
	Name   string `json:"name"`
	Access string `json:"access"`
}

func (s *server) createUser(w http.ResponseWriter, r *http.Request) {
	var req userRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	creds, err := s.Databases.CreateUser(r.Context(), urlSlug(r), req.Name, req.Access, actor(r))
	s.secret(w, http.StatusCreated, "create user", creds, err)
}

func (s *server) resetUserPassword(w http.ResponseWriter, r *http.Request) {
	creds, err := s.Databases.ResetUserPassword(r.Context(), urlSlug(r), urlName(r), actor(r))
	s.secret(w, http.StatusOK, "reset user password", creds, err)
}

func (s *server) deleteUser(w http.ResponseWriter, r *http.Request) {
	s.noContent(w, "delete user", s.Databases.DeleteUser(r.Context(), urlSlug(r), urlName(r), actor(r)))
}
