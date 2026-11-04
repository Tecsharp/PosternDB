package httpapi

import (
	"net/http"
	"sort"
	"time"

	"github.com/tecsharp/posterndb/internal/auth"
	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/model"
	"github.com/tecsharp/posterndb/internal/provision"
)

// ---- account -------------------------------------------------------------------

type accountView struct {
	Username      string    `json:"username"`
	Email         string    `json:"email"`
	FullName      string    `json:"fullName"`
	GoogleLinked  bool      `json:"googleLinked"`
	HasPassword   bool      `json:"hasPassword"`
	NeedsUsername bool      `json:"needsUsername"`
	CreatedAt     time.Time `json:"createdAt"`
	MaxDatabases  int       `json:"maxDatabases"`
	DatabaseCount int       `json:"databaseCount"`
	// Code leads the name of the user's databases; HostPrefix and BaseDomain
	// let the panel preview the public address before creating one.
	Code       string `json:"code"`
	HostPrefix string `json:"hostPrefix"`
	BaseDomain string `json:"baseDomain"`
	LocalMode  bool   `json:"localMode"`
}

func (s *server) accountOf(u model.ClientUser, count int) accountView {
	return accountView{
		Username: u.Username, Email: u.Email, FullName: u.FullName, GoogleLinked: u.GoogleLinked,
		HasPassword: u.HasPassword, NeedsUsername: u.NeedsUsername, CreatedAt: u.CreatedAt,
		MaxDatabases: s.Settings.MaxDatabasesPerClient, DatabaseCount: count,
		Code: u.Code, HostPrefix: s.Settings.ClientHostPrefix, BaseDomain: s.Settings.BaseDomain, LocalMode: s.Settings.LocalMode,
	}
}

func (s *server) myAccount(w http.ResponseWriter, r *http.Request) {
	u, ok, err := s.Clients.ByID(r.Context(), clientID(r))
	if err != nil || !ok {
		s.domainError(w, "my account", orNotFound(err))
		return
	}
	n, err := s.Databases.CountByClient(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, "count my databases", err)
		return
	}
	writeJSON(w, http.StatusOK, s.accountOf(u, n))
}

func orNotFound(err error) error {
	if err != nil {
		return err
	}
	return model.ErrNotFound
}

type accountUpdate struct {
	Username *string `json:"username"`
	FullName *string `json:"fullName"`
}

func (s *server) updateMyAccount(w http.ResponseWriter, r *http.Request) {
	var req accountUpdate
	if !decodeJSON(w, r, &req) {
		return
	}
	id := clientID(r)
	if req.Username == nil && req.FullName == nil {
		writeError(w, http.StatusBadRequest, "no hay nada que cambiar")
		return
	}
	var u model.ClientUser
	var err error
	if req.Username != nil {
		if u, err = s.Clients.SetUsername(r.Context(), id, *req.Username); err != nil {
			s.domainError(w, "set username", err)
			return
		}
	}
	if req.FullName != nil {
		if u, err = s.Clients.SetFullName(r.Context(), id, *req.FullName); err != nil {
			s.domainError(w, "set full name", err)
			return
		}
	}
	n, err := s.Databases.CountByClient(r.Context(), id)
	if err != nil {
		s.internalError(w, "count my databases", err)
		return
	}
	writeJSON(w, http.StatusOK, s.accountOf(u, n))
}

// ---- backups -------------------------------------------------------------------

func (s *server) myBackups(w http.ResponseWriter, r *http.Request) {
	list, err := s.Databases.ClientBackups(r.Context(), clientID(r))
	s.respond(w, "my backups", map[string]any{"backups": list}, err)
}

func (s *server) createMyBackup(w http.ResponseWriter, r *http.Request) {
	db, ok := s.ownDatabase(w, r)
	if !ok {
		return
	}
	if !s.canTakeBackup(w, r, db.Slug) {
		return
	}
	job, err := s.Databases.Backup(r.Context(), db.Slug, actor(r))
	s.respondStatus(w, http.StatusAccepted, "my backup", job, err)
}

// canTakeBackup keeps one backup running at a time and caps how many a user
// keeps per database.
func (s *server) canTakeBackup(w http.ResponseWriter, r *http.Request, slug string) bool {
	jobs, err := s.Databases.ListJobs(r.Context(), slug)
	if err != nil {
		s.domainError(w, "list jobs", err)
		return false
	}
	for _, j := range jobs {
		if j.Kind == config.JobDump && (j.Status == config.JobPending || j.Status == config.JobRunning) {
			writeError(w, http.StatusConflict, "ya hay un respaldo en curso: espera a que termine")
			return false
		}
	}
	kept, err := s.Databases.ListBackups(r.Context(), slug)
	if err != nil {
		s.domainError(w, "list backups", err)
		return false
	}
	if len(kept) >= config.MaxClientBackupsPerDatabase {
		writeError(w, http.StatusForbidden, "ya tienes el máximo de respaldos de esta base: elimina uno para crear otro")
		return false
	}
	return true
}

// ownBackup loads a backup and confirms it belongs to a database the client
// owns. Anything else answers 404, exactly like a backup that does not exist.
func (s *server) ownBackup(w http.ResponseWriter, r *http.Request) (string, model.Job, bool) {
	id, ok := pathID(w, r)
	if !ok {
		return "", model.Job{}, false
	}
	path, job, err := s.Databases.BackupPath(r.Context(), id)
	if err != nil {
		s.domainError(w, "own backup", err)
		return "", job, false
	}
	db, err := s.Databases.Get(r.Context(), job.Slug)
	if err != nil || db.ClientUserID == nil || *db.ClientUserID != clientID(r) {
		writeError(w, http.StatusNotFound, "no encontrado")
		return "", job, false
	}
	return path, job, true
}

func (s *server) downloadMyBackup(w http.ResponseWriter, r *http.Request) {
	path, job, ok := s.ownBackup(w, r)
	if !ok {
		return
	}
	s.serveBackup(w, r, path, job)
}

func (s *server) deleteMyBackup(w http.ResponseWriter, r *http.Request) {
	_, job, ok := s.ownBackup(w, r)
	if !ok {
		return
	}
	s.noContent(w, "delete my backup", s.Databases.DeleteBackup(r.Context(), job.ID, actor(r)))
}

func (s *server) restoreMyBackup(w http.ResponseWriter, r *http.Request) {
	_, job, ok := s.ownBackup(w, r)
	if !ok {
		return
	}
	var req restoreRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	id := clientID(r)
	defer s.lockCreating(id)()
	if !s.hasRoomForDatabase(w, r) {
		return
	}
	slug, ok := s.clientSlug(w, r, req.Name)
	if !ok {
		return
	}
	creds, err := s.Databases.Create(r.Context(), provision.CreateRequest{Name: slug, RestoreBackupID: job.ID, ClientUserID: &id}, actor(r))
	s.secret(w, http.StatusCreated, "restore my backup", creds, err)
}

// ---- activity ------------------------------------------------------------------

// ownSlugs lists the slugs of the databases an account owns.
func (s *server) ownSlugs(r *http.Request, accountID int64) ([]string, error) {
	dbs, err := s.Databases.ClientDatabases(r.Context(), accountID)
	if err != nil {
		return nil, err
	}
	slugs := make([]string, 0, len(dbs))
	for _, d := range dbs {
		slugs = append(slugs, d.Slug)
	}
	return slugs, nil
}

// activityOf answers with the audit entries about one person, newest first.
// redact hides the name and details of whoever else acted (a user's own view).
func (s *server) activityOf(w http.ResponseWriter, r *http.Request, identities, slugs []string, redact bool) {
	entries, err := s.Audit.ListAudit(r.Context(), config.ClientActivityScan)
	if err != nil {
		s.internalError(w, "activity", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": personActivity(entries, identities, slugs, redact)})
}

// myActivity is the registered user's own feed: what happened to their account
// and databases. Other people's actions appear without their name or details.
func (s *server) myActivity(w http.ResponseWriter, r *http.Request) {
	u, ok, err := s.Clients.ByID(r.Context(), clientID(r))
	if err != nil || !ok {
		s.domainError(w, "my activity", orNotFound(err))
		return
	}
	slugs, err := s.ownSlugs(r, u.ID)
	if err != nil {
		s.domainError(w, "my activity", err)
		return
	}
	s.activityOf(w, r, []string{u.Username, u.Email}, slugs, true)
}

// myStaffActivity is the feed of whoever is signed in to the admin panel: what
// they did and what happened to their account. For the owner (no account) it is
// what was done under the ADMIN_USER name.
func (s *server) myStaffActivity(w http.ResponseWriter, r *http.Request) {
	id := auth.FromContext(r.Context())
	if id.ClientUserID == nil {
		s.activityOf(w, r, []string{id.Username}, nil, false)
		return
	}
	u, ok, err := s.Clients.ByID(r.Context(), *id.ClientUserID)
	if err != nil || !ok {
		s.domainError(w, "my activity", orNotFound(err))
		return
	}
	slugs, err := s.ownSlugs(r, u.ID)
	if err != nil {
		s.domainError(w, "my activity", err)
		return
	}
	s.activityOf(w, r, []string{u.Username, u.Email}, slugs, false)
}

// clientActivityFor is the feed of one person, for the administrators who
// manage them: user or administrator, the same account.
func (s *server) clientActivityFor(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u, found, err := s.Clients.ByID(r.Context(), id)
	if err != nil || !found {
		s.domainError(w, "client activity", orNotFound(err))
		return
	}
	slugs, err := s.ownSlugs(r, u.ID)
	if err != nil {
		s.domainError(w, "client activity", err)
		return
	}
	s.activityOf(w, r, []string{u.Username, u.Email}, slugs, false)
}

// personActivity keeps the entries that concern one person: their own actions
// and anything that happened to their account or databases. With redact, what
// others did is shown without their identity and without details.
func personActivity(entries []model.AuditEntry, identities, slugs []string, redact bool) []model.AuditEntry {
	mine := set(identities)
	own := set(slugs)
	out := []model.AuditEntry{}
	for _, e := range entries {
		self := mine[e.Actor]
		if !self && !mine[e.Target] && !own[e.Target] {
			continue
		}
		if redact && !self {
			if e.Actor != config.ActorSystem && e.Actor != config.ActorAuditor {
				e.Actor = "administrador"
			}
			e.Details = nil
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].OccurredAt.After(out[j].OccurredAt) })
	if len(out) > config.ClientActivityShown {
		out = out[:config.ClientActivityShown]
	}
	return out
}

func set(items []string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, v := range items {
		if v != "" {
			m[v] = true
		}
	}
	return m
}
