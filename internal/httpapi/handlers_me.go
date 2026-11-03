package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/tecsharp/posterndb/internal/auth"
	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/provision"
)

// clientID reads the authenticated client's id. clientRoutes is only
// reachable after auth.RequireClient, so this is always set there.
func clientID(r *http.Request) int64 {
	id := auth.FromContext(r.Context()).ClientUserID
	if id == nil {
		return 0
	}
	return *id
}

// ownDatabase loads a database by slug and confirms it belongs to the
// authenticated client. A slug that exists but belongs to someone else — or
// to no client at all (an admin-panel database) — answers 404, the same as
// a slug that does not exist, so guessing never distinguishes the two.
func (s *server) ownDatabase(w http.ResponseWriter, r *http.Request) (provision.DatabaseView, bool) {
	db, err := s.Databases.Get(r.Context(), urlSlug(r))
	if err != nil {
		s.domainError(w, "get my database", err)
		return provision.DatabaseView{}, false
	}
	if db.ClientUserID == nil || *db.ClientUserID != clientID(r) {
		writeError(w, http.StatusNotFound, "no encontrado")
		return provision.DatabaseView{}, false
	}
	return db, true
}

func (s *server) listMyDatabases(w http.ResponseWriter, r *http.Request) {
	dbs, err := s.Databases.ClientDatabases(r.Context(), clientID(r))
	s.respond(w, "list my databases", map[string]any{"databases": dbs}, err)
}

// createMyDatabaseRequest is deliberately narrower than provision.CreateRequest:
// a client may pick a shared template, but never clone another database or
// restore a backup (that would copy someone else's data).
type createMyDatabaseRequest struct {
	Name       string `json:"name"`
	TemplateID int64  `json:"templateId,omitempty"`
}

func (s *server) createMyDatabase(w http.ResponseWriter, r *http.Request) {
	var body createMyDatabaseRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	req := provision.CreateRequest{Name: body.Name, TemplateID: body.TemplateID}
	if req.TemplateID != 0 && !s.isSharedTemplate(w, r, req.TemplateID) {
		return
	}
	id := clientID(r)
	n, err := s.Databases.CountByClient(r.Context(), id)
	if err != nil {
		s.internalError(w, "count my databases", err)
		return
	}
	if n >= s.Settings.MaxDatabasesPerClient {
		writeError(w, http.StatusForbidden, "llegaste al máximo de bases de datos permitidas para tu cuenta")
		return
	}
	req.ClientUserID = &id
	creds, err := s.Databases.Create(r.Context(), req, actor(r))
	s.secret(w, http.StatusCreated, "create my database", creds, err)
}

func (s *server) isSharedTemplate(w http.ResponseWriter, r *http.Request, id int64) bool {
	shared, err := s.Databases.SharedTemplates(r.Context())
	if err != nil {
		s.internalError(w, "list shared templates", err)
		return false
	}
	for _, t := range shared {
		if t.ID == id {
			return true
		}
	}
	writeError(w, http.StatusBadRequest, "esa plantilla no está disponible")
	return false
}

func (s *server) myTemplates(w http.ResponseWriter, r *http.Request) {
	list, err := s.Databases.SharedTemplates(r.Context())
	s.respond(w, "list my templates", map[string]any{"templates": list}, err)
}

func (s *server) myLiveConnections(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.ownDatabase(w, r); !ok {
		return
	}
	list, err := s.Databases.Connections(r.Context(), urlSlug(r))
	s.respond(w, "my live connections", map[string]any{"connections": list}, err)
}

func (s *server) getMyDatabase(w http.ResponseWriter, r *http.Request) {
	db, ok := s.ownDatabase(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, db)
}

func (s *server) deleteMyDatabase(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.ownDatabase(w, r); !ok {
		return
	}
	var req deleteRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	slug := urlSlug(r)
	if req.Confirm != slug {
		writeError(w, http.StatusBadRequest, "la confirmación no coincide con el nombre de la base de datos")
		return
	}
	s.noContent(w, "delete my database", s.Databases.Delete(r.Context(), slug, actor(r)))
}

func (s *server) myConnection(w http.ResponseWriter, r *http.Request) {
	db, ok := s.ownDatabase(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.Databases.ConnectionInfo(db.ClientDatabase))
}

func (s *server) myStats(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.ownDatabase(w, r); !ok {
		return
	}
	st, err := s.Databases.Stats(r.Context(), urlSlug(r))
	s.respond(w, "my stats", st, err)
}

func (s *server) mySchema(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.ownDatabase(w, r); !ok {
		return
	}
	sc, err := s.Databases.Schema(r.Context(), urlSlug(r))
	s.respond(w, "my schema", sc, err)
}

func (s *server) resetMyPassword(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.ownDatabase(w, r); !ok {
		return
	}
	creds, err := s.Databases.ResetPassword(r.Context(), urlSlug(r), actor(r))
	s.secret(w, http.StatusOK, "reset my password", creds, err)
}

func (s *server) browseMyTable(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.ownDatabase(w, r); !ok {
		return
	}
	s.browse(w, r)
}

func (s *server) insertMyRow(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.ownDatabase(w, r); !ok {
		return
	}
	s.insertRow(w, r)
}

func (s *server) updateMyRow(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.ownDatabase(w, r); !ok {
		return
	}
	s.updateRow(w, r)
}

func (s *server) deleteMyRow(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.ownDatabase(w, r); !ok {
		return
	}
	s.deleteRow(w, r)
}

func (s *server) countMyRows(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.ownDatabase(w, r); !ok {
		return
	}
	n, err := s.Databases.CountRows(r.Context(), urlSlug(r), chi.URLParam(r, config.URLParamSchema), chi.URLParam(r, config.URLParamTable))
	s.respond(w, "count my rows", map[string]int64{"rows": n}, err)
}
