package httpapi

import (
	"mime"
	"net/http"
	"os"

	"github.com/tecsharp/posterndb/internal/model"
	"github.com/tecsharp/posterndb/internal/provision"
)

// ---- backups -------------------------------------------------------------------

func (s *server) listBackups(w http.ResponseWriter, r *http.Request) {
	list, err := s.Databases.ListBackups(r.Context(), "")
	s.respond(w, "list backups", map[string]any{"backups": list}, err)
}

func (s *server) listDatabaseBackups(w http.ResponseWriter, r *http.Request) {
	list, err := s.Databases.ListBackups(r.Context(), urlSlug(r))
	s.respond(w, "list backups", map[string]any{"backups": list}, err)
}

func (s *server) listJobs(w http.ResponseWriter, r *http.Request) {
	list, err := s.Databases.ListJobs(r.Context(), urlSlug(r))
	s.respond(w, "list jobs", map[string]any{"jobs": list}, err)
}

func (s *server) backup(w http.ResponseWriter, r *http.Request) {
	job, err := s.Databases.Backup(r.Context(), urlSlug(r), actor(r))
	s.respondStatus(w, http.StatusAccepted, "backup", job, err)
}

func (s *server) downloadBackup(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	path, job, err := s.Databases.BackupPath(r.Context(), id)
	if err != nil {
		s.domainError(w, "download backup", err)
		return
	}
	s.serveBackup(w, r, path, job)
}

func (s *server) serveBackup(w http.ResponseWriter, r *http.Request, path string, job model.Job) {
	f, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "el archivo del respaldo ya no existe")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		s.internalError(w, "stat backup", err)
		return
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": job.File}))
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, job.File, info.ModTime(), f)
}

func (s *server) deleteBackup(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	s.noContent(w, "delete backup", s.Databases.DeleteBackup(r.Context(), id, actor(r)))
}

type restoreRequest struct {
	Name string `json:"name"`
}

func (s *server) restoreBackup(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req restoreRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	creds, err := s.Databases.Create(r.Context(), provision.CreateRequest{Name: req.Name, RestoreBackupID: id}, actor(r))
	s.secret(w, http.StatusCreated, "restore backup", creds, err)
}

// ---- templates -----------------------------------------------------------------

func (s *server) listTemplates(w http.ResponseWriter, r *http.Request) {
	list, err := s.Databases.ListTemplates(r.Context())
	s.respond(w, "list templates", map[string]any{"templates": list}, err)
}

type templateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	SQL         string `json:"sql"`
	Shared      bool   `json:"shared"`
}

func (s *server) createTemplate(w http.ResponseWriter, r *http.Request) {
	s.saveTemplate(w, r, 0, http.StatusCreated)
}

func (s *server) updateTemplate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	s.saveTemplate(w, r, id, http.StatusOK)
}

func (s *server) saveTemplate(w http.ResponseWriter, r *http.Request, id int64, status int) {
	var req templateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	t, err := s.Databases.SaveTemplate(r.Context(), model.Template{ID: id, Name: req.Name, Description: req.Description, SQL: req.SQL, Shared: req.Shared}, actor(r))
	s.respondStatus(w, status, "save template", t, err)
}

func (s *server) deleteTemplate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	s.noContent(w, "delete template", s.Databases.DeleteTemplate(r.Context(), id, actor(r)))
}
