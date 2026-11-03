// Package testsupport provides in-memory fakes shared by unit tests of the
// provisioning service, the auditor, the admins service and the HTTP
// handlers. It is excluded from coverage in sonar-project.properties.
package testsupport

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/model"
	"github.com/tecsharp/posterndb/internal/notify"
)

// ErrInjected is returned by fakes configured to fail.
var ErrInjected = errors.New("injected failure")

// ---- Cloudflare -----------------------------------------------------------------

// FakeCloudflareClient implements cloudflare.Router in memory.
type FakeCloudflareClient struct {
	mu         sync.Mutex
	Routes     map[string]string
	FailEnsure bool
	FailRemove bool
	Removed    []string
}

// NewFakeCloudflareClient returns an empty fake.
func NewFakeCloudflareClient() *FakeCloudflareClient {
	return &FakeCloudflareClient{Routes: map[string]string{}}
}

// EnsureRoute records hostname -> service.
func (f *FakeCloudflareClient) EnsureRoute(_ context.Context, hostname, service string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.FailEnsure {
		return ErrInjected
	}
	f.Routes[hostname] = service
	return nil
}

// RemoveRoute deletes hostname.
func (f *FakeCloudflareClient) RemoveRoute(_ context.Context, hostname string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.FailRemove {
		return ErrInjected
	}
	delete(f.Routes, hostname)
	f.Removed = append(f.Removed, hostname)
	return nil
}

// HasRoute reports whether hostname is published.
func (f *FakeCloudflareClient) HasRoute(hostname string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.Routes[hostname]
	return ok
}

// ---- PostgreSQL cluster -----------------------------------------------------------

// FakeCluster implements the PostgreSQL cluster operations in memory.
type FakeCluster struct {
	mu            sync.Mutex
	Roles         map[string]string // role -> verifier
	Databases     map[string]string // db -> owner
	Archived      map[string]string // archive name -> original db
	ConsoleGrants map[string]bool
	Activity      map[string]model.Activity
	Stats         model.DatabaseStats
	FailRole      bool
	FailDB        bool
	FailDrop      bool
	FailArchive   bool
	FailStats     bool
	FailGrant     bool
	FailPassword  bool
}

// NewFakeCluster returns an empty fake cluster.
func NewFakeCluster() *FakeCluster {
	return &FakeCluster{
		Roles: map[string]string{}, Databases: map[string]string{}, Archived: map[string]string{},
		ConsoleGrants: map[string]bool{}, Activity: map[string]model.Activity{},
	}
}

func (c *FakeCluster) locked(fail bool, fn func()) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if fail {
		return ErrInjected
	}
	fn()
	return nil
}

// CreateRole adds a role.
func (c *FakeCluster) CreateRole(_ context.Context, role, verifier string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.FailRole {
		return ErrInjected
	}
	if _, ok := c.Roles[role]; ok {
		return model.ErrConflict
	}
	c.Roles[role] = verifier
	return nil
}

// DropRole removes a role.
func (c *FakeCluster) DropRole(_ context.Context, role string) error {
	return c.locked(false, func() { delete(c.Roles, role) })
}

// CreateDatabase adds a database.
func (c *FakeCluster) CreateDatabase(_ context.Context, db, owner string) error {
	return c.locked(c.FailDB, func() { c.Databases[db] = owner })
}

// DropClient removes a database and its role.
func (c *FakeCluster) DropClient(_ context.Context, db, role string) error {
	return c.locked(c.FailDrop, func() {
		delete(c.Databases, db)
		delete(c.Roles, role)
	})
}

// ArchiveClient renames a database and its role.
func (c *FakeCluster) ArchiveClient(_ context.Context, db, role, archive string) error {
	return c.locked(c.FailArchive, func() {
		delete(c.Databases, db)
		delete(c.Roles, role)
		c.Archived[archive] = db
	})
}

// GrantConsoleAccess records the grant.
func (c *FakeCluster) GrantConsoleAccess(_ context.Context, db, _ string) error {
	return c.locked(c.FailGrant, func() { c.ConsoleGrants[db] = true })
}

// SetPassword replaces a role's verifier.
func (c *FakeCluster) SetPassword(_ context.Context, role, verifier string) error {
	return c.locked(c.FailPassword, func() { c.Roles[role] = verifier })
}

// CreateUserRole adds an extra role.
func (c *FakeCluster) CreateUserRole(ctx context.Context, role, _, verifier string) error {
	return c.CreateRole(ctx, role, verifier)
}

// DatabaseStats returns the configured stats.
func (c *FakeCluster) DatabaseConnections(context.Context, string) ([]model.Connection, error) {
	return []model.Connection{{PID: 4242, User: "pdb_demo", Application: "psql", State: "active", Query: "SELECT 1"}}, nil
}

// DatabaseStats returns canned stats.
func (c *FakeCluster) DatabaseStats(context.Context, string) (model.DatabaseStats, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.FailStats {
		return model.DatabaseStats{}, ErrInjected
	}
	return c.Stats, nil
}

// ActivitySnapshot returns the configured activity.
func (c *FakeCluster) ActivitySnapshot(context.Context) (map[string]model.Activity, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.FailStats {
		return nil, ErrInjected
	}
	out := make(map[string]model.Activity, len(c.Activity))
	for k, v := range c.Activity {
		out[k] = v
	}
	return out, nil
}

// ---- inspector ----------------------------------------------------------------------

// FakeInspector records calls into client databases.
type FakeInspector struct {
	mu          sync.Mutex
	Applied     []string
	Grants      map[string]string // user -> access
	Dropped     []string
	SchemaValue model.Schema
	Result      model.QueryResult
	Rows        int64
	Queries     []string
	Data        model.TableData
	Writes      []string
	FailWrite   bool
	FailApply   bool
	FailGrant   bool
	FailDrop    bool
	FailRead    bool
}

// NewFakeInspector returns an empty fake.
func NewFakeInspector() *FakeInspector {
	return &FakeInspector{Grants: map[string]string{}}
}

func (f *FakeInspector) locked(fail bool, fn func()) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if fail {
		return ErrInjected
	}
	fn()
	return nil
}

// Schema returns the configured schema.
func (f *FakeInspector) Schema(context.Context, string) (model.Schema, error) {
	var s model.Schema
	err := f.locked(f.FailRead, func() { s = f.SchemaValue })
	return s, err
}

// TableSummary derives a summary from the configured schema.
func (f *FakeInspector) TableSummary(context.Context, string) (int, int64, []model.TableBrief, error) {
	var total int64
	top := []model.TableBrief{}
	err := f.locked(f.FailRead, func() {
		for _, t := range f.SchemaValue.Tables {
			total += t.Rows
			top = append(top, model.TableBrief{Schema: t.Schema, Name: t.Name, Rows: t.Rows, SizeBytes: t.SizeBytes})
		}
	})
	return len(top), total, top, err
}

// Query records the SQL and returns the configured result.
func (f *FakeInspector) Query(_ context.Context, _, _, sql string) (model.QueryResult, error) {
	var r model.QueryResult
	err := f.locked(f.FailRead, func() {
		f.Queries = append(f.Queries, sql)
		r = f.Result
	})
	return r, err
}

// Preview returns the configured result.
func (f *FakeInspector) Preview(ctx context.Context, db, owner, schema, table string) (model.QueryResult, error) {
	return f.Query(ctx, db, owner, fmt.Sprintf("preview %s.%s", schema, table))
}

// CountRows returns the configured count.
func (f *FakeInspector) CountRows(context.Context, string, string, string, string) (int64, error) {
	var n int64
	err := f.locked(f.FailRead, func() { n = f.Rows })
	return n, err
}

// ApplySQL records template SQL.
func (f *FakeInspector) ApplySQL(_ context.Context, _, _, sql string) error {
	return f.locked(f.FailApply, func() { f.Applied = append(f.Applied, sql) })
}

// GrantUser records the grant.
func (f *FakeInspector) GrantUser(_ context.Context, _, _, user, access string) error {
	return f.locked(f.FailGrant, func() { f.Grants[user] = access })
}

// DropUserObjects records the revoke.
func (f *FakeInspector) DropUserObjects(_ context.Context, _, _, user string) error {
	return f.locked(f.FailDrop, func() {
		delete(f.Grants, user)
		f.Dropped = append(f.Dropped, user)
	})
}

// Browse returns the configured page.
func (f *FakeInspector) Browse(_ context.Context, _, _, schema, table string, page, size int, sort string, desc bool) (model.TableData, error) {
	var d model.TableData
	err := f.locked(f.FailRead, func() {
		f.Queries = append(f.Queries, fmt.Sprintf("browse %s.%s page=%d size=%d sort=%s desc=%t", schema, table, page, size, sort, desc))
		d = f.Data
	})
	return d, err
}

// InsertRow records the write.
func (f *FakeInspector) InsertRow(_ context.Context, _, _, schema, table string, values model.RowValues) error {
	return f.locked(f.FailWrite, func() { f.Writes = append(f.Writes, fmt.Sprintf("insert %s.%s %d", schema, table, len(values))) })
}

// UpdateRow records the write.
func (f *FakeInspector) UpdateRow(_ context.Context, _, _, schema, table string, key, values model.RowValues) error {
	return f.locked(f.FailWrite, func() {
		f.Writes = append(f.Writes, fmt.Sprintf("update %s.%s key=%d values=%d", schema, table, len(key), len(values)))
	})
}

// DeleteRow records the write.
func (f *FakeInspector) DeleteRow(_ context.Context, _, _, schema, table string, key model.RowValues) error {
	return f.locked(f.FailWrite, func() { f.Writes = append(f.Writes, fmt.Sprintf("delete %s.%s key=%d", schema, table, len(key))) })
}

// ---- metadata ---------------------------------------------------------------------------

// FakeMetadata is an in-memory metadata repository.
type FakeMetadata struct {
	mu          sync.Mutex
	nextID      int64
	nextUserID  int64
	Rows        map[int64]*model.ClientDatabase
	Audit       []model.AuditEntry
	Users       map[int64]*model.DatabaseUser
	Jobs        []model.Job
	Templates   map[int64]*model.Template
	Admins      map[string]*model.Admin
	Internal    map[string]int
	Settings    model.PlatformSettings
	ClientUsers map[int64]*model.ClientUser
	FailInsert  bool
	FailList    bool
	FailUpdate  bool
	FailAudit   bool
	FailJob     bool
}

// NewFakeMetadata returns an empty repository.
func NewFakeMetadata() *FakeMetadata {
	return &FakeMetadata{
		Rows: map[int64]*model.ClientDatabase{}, Users: map[int64]*model.DatabaseUser{},
		Templates: map[int64]*model.Template{}, Admins: map[string]*model.Admin{}, Internal: map[string]int{},
		ClientUsers: map[int64]*model.ClientUser{},
	}
}

func (m *FakeMetadata) id() int64 {
	m.nextID++
	return m.nextID
}

// Seed inserts a row directly and returns it.
func (m *FakeMetadata) Seed(db model.ClientDatabase) model.ClientDatabase {
	m.mu.Lock()
	defer m.mu.Unlock()
	db.ID = m.id()
	if db.State == "" {
		db.State = config.StateActive
	}
	if db.CreatedAt.IsZero() {
		db.CreatedAt = db.LastSeenAt
	}
	m.Rows[db.ID] = &db
	return db
}

// InsertDatabase stores a new active row.
func (m *FakeMetadata) InsertDatabase(_ context.Context, db model.ClientDatabase) (model.ClientDatabase, error) {
	if m.FailInsert {
		return model.ClientDatabase{}, ErrInjected
	}
	return m.Seed(db), nil
}

// ListDatabases returns non-deleted rows, newest first.
func (m *FakeMetadata) ListDatabases(context.Context) ([]model.ClientDatabase, error) {
	return m.filter(func(d *model.ClientDatabase) bool { return d.State != config.StateDeleted })
}

// ListActive returns active rows.
func (m *FakeMetadata) ListActive(context.Context) ([]model.ClientDatabase, error) {
	return m.filter(func(d *model.ClientDatabase) bool { return d.State == config.StateActive })
}

func (m *FakeMetadata) filter(keep func(*model.ClientDatabase) bool) ([]model.ClientDatabase, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailList {
		return nil, ErrInjected
	}
	var out []model.ClientDatabase
	for _, d := range m.Rows {
		if keep(d) {
			out = append(out, *d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

// GetDatabase finds a live row by slug.
func (m *FakeMetadata) GetDatabase(_ context.Context, slug string) (model.ClientDatabase, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailList {
		return model.ClientDatabase{}, ErrInjected
	}
	for _, d := range m.Rows {
		if d.Slug == slug && d.State != config.StateDeleted {
			return *d, nil
		}
	}
	return model.ClientDatabase{}, model.ErrNotFound
}

func (m *FakeMetadata) update(id int64, fn func(*model.ClientDatabase)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailUpdate {
		return ErrInjected
	}
	d, ok := m.Rows[id]
	if !ok {
		return model.ErrNotFound
	}
	fn(d)
	return nil
}

// RecordActivity updates last_seen_at and the baselines.
func (m *FakeMetadata) RecordActivity(_ context.Context, id int64, t time.Time, sessions, internal int64) error {
	return m.update(id, func(d *model.ClientDatabase) {
		d.LastSeenAt, d.SessionsBaseline, d.InternalBaseline, d.ExpiryWarnedAt = t, sessions, internal, nil
	})
}

// UpdateBaseline updates the baselines only.
func (m *FakeMetadata) UpdateBaseline(_ context.Context, id int64, sessions, internal int64) error {
	return m.update(id, func(d *model.ClientDatabase) { d.SessionsBaseline, d.InternalBaseline = sessions, internal })
}

// MarkWarned records an expiry warning.
func (m *FakeMetadata) MarkWarned(_ context.Context, id int64, at time.Time) error {
	return m.update(id, func(d *model.ClientDatabase) { d.ExpiryWarnedAt = &at })
}

// SetExpiry stores expiry overrides.
func (m *FakeMetadata) SetExpiry(_ context.Context, id int64, never bool, maxIdle *int, grace *time.Time) error {
	return m.update(id, func(d *model.ClientDatabase) {
		d.NeverExpires, d.MaxIdleDays, d.GraceUntil, d.ExpiryWarnedAt = never, maxIdle, grace, nil
	})
}

// MarkExpired sets the expired state.
func (m *FakeMetadata) MarkExpired(_ context.Context, id int64, archived string, at time.Time) error {
	return m.update(id, func(d *model.ClientDatabase) {
		d.State = config.StateExpired
		d.ExpiredAt = &at
		d.ArchivedAs = archived
	})
}

// MarkDeleted sets the deleted state.
func (m *FakeMetadata) MarkDeleted(_ context.Context, id int64, archived string, _ time.Time) error {
	return m.update(id, func(d *model.ClientDatabase) {
		d.State = config.StateDeleted
		if archived != "" {
			d.ArchivedAs = archived
		}
	})
}

// UpdateHostname changes the hostname.
func (m *FakeMetadata) UpdateHostname(_ context.Context, id int64, host string) error {
	return m.update(id, func(d *model.ClientDatabase) { d.Hostname = host })
}

// IncrementInternalSessions counts inspector connections.
func (m *FakeMetadata) IncrementInternalSessions(_ context.Context, db string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Internal[db]++
	return nil
}

// RecordAudit appends an entry.
func (m *FakeMetadata) RecordAudit(_ context.Context, e model.AuditEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailAudit {
		return ErrInjected
	}
	e.ID = int64(len(m.Audit) + 1)
	m.Audit = append(m.Audit, e)
	return nil
}

// ListAudit returns entries newest first.
func (m *FakeMetadata) ListAudit(_ context.Context, limit int) ([]model.AuditEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailList {
		return nil, ErrInjected
	}
	out := make([]model.AuditEntry, 0, len(m.Audit))
	for i := len(m.Audit) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, m.Audit[i])
	}
	return out, nil
}

// Actions returns the recorded audit actions in order.
func (m *FakeMetadata) Actions() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.Audit))
	for i, e := range m.Audit {
		out[i] = e.Action
	}
	return out
}

// ---- users ---------------------------------------------------------------------------

// ListUsers returns users of a database.
func (m *FakeMetadata) ListUsers(_ context.Context, dbID int64) ([]model.DatabaseUser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailList {
		return nil, ErrInjected
	}
	out := []model.DatabaseUser{}
	for _, u := range m.Users {
		if u.DatabaseID == dbID {
			out = append(out, *u)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// GetUser finds one user.
func (m *FakeMetadata) GetUser(_ context.Context, dbID int64, name string) (model.DatabaseUser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.Users {
		if u.DatabaseID == dbID && u.Name == name {
			return *u, nil
		}
	}
	return model.DatabaseUser{}, model.ErrNotFound
}

// InsertUser stores a user.
func (m *FakeMetadata) InsertUser(_ context.Context, u model.DatabaseUser) (model.DatabaseUser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailInsert {
		return u, ErrInjected
	}
	u.ID = m.id()
	m.Users[u.ID] = &u
	return u, nil
}

// DeleteUser removes a user.
func (m *FakeMetadata) DeleteUser(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.Users, id)
	return nil
}

// ---- jobs ------------------------------------------------------------------------------

// EnqueueJob stores a pending job.
func (m *FakeMetadata) EnqueueJob(_ context.Context, j model.Job) (model.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailJob {
		return j, ErrInjected
	}
	j.ID = m.id()
	if j.Status == "" {
		j.Status = config.JobPending
	}
	m.Jobs = append(m.Jobs, j)
	return j, nil
}

// GetJob finds a job.
func (m *FakeMetadata) GetJob(_ context.Context, id int64) (model.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.Jobs {
		if j.ID == id {
			return j, nil
		}
	}
	return model.Job{}, model.ErrNotFound
}

// ListBackups returns finished dumps.
func (m *FakeMetadata) ListBackups(_ context.Context, slug string) ([]model.Job, error) {
	return m.jobs(func(j model.Job) bool {
		return (j.Kind == config.JobDump || j.Kind == config.JobArchive) && j.Status == config.JobDone && (slug == "" || j.Slug == slug)
	})
}

// ListJobs returns jobs of a database.
func (m *FakeMetadata) ListJobs(_ context.Context, slug string) ([]model.Job, error) {
	return m.jobs(func(j model.Job) bool { return j.Slug == slug })
}

func (m *FakeMetadata) jobs(keep func(model.Job) bool) ([]model.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailList {
		return nil, ErrInjected
	}
	out := []model.Job{}
	for _, j := range m.Jobs {
		if keep(j) {
			out = append(out, j)
		}
	}
	return out, nil
}

// SetJobStatus is a test helper emulating the sidecar.
func (m *FakeMetadata) SetJobStatus(id int64, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.Jobs {
		if m.Jobs[i].ID == id {
			m.Jobs[i].Status = status
		}
	}
}

// ---- templates ---------------------------------------------------------------------------

// ListTemplates returns templates.
func (m *FakeMetadata) ListTemplates(context.Context) ([]model.Template, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailList {
		return nil, ErrInjected
	}
	out := []model.Template{}
	for _, t := range m.Templates {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// GetTemplate finds a template.
func (m *FakeMetadata) GetTemplate(_ context.Context, id int64) (model.Template, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.Templates[id]; ok {
		return *t, nil
	}
	return model.Template{}, model.ErrNotFound
}

// SaveTemplate inserts or updates.
func (m *FakeMetadata) SaveTemplate(_ context.Context, t model.Template) (model.Template, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailInsert {
		return t, ErrInjected
	}
	if t.ID == 0 {
		t.ID = m.id()
	} else if _, ok := m.Templates[t.ID]; !ok {
		return t, model.ErrNotFound
	}
	m.Templates[t.ID] = &t
	return t, nil
}

// DeleteTemplate removes a template.
func (m *FakeMetadata) DeleteTemplate(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.Templates, id)
	return nil
}

// ---- platform settings -----------------------------------------------------------------------

// GetPlatformSettings returns the configured override.
func (m *FakeMetadata) GetPlatformSettings(context.Context) (model.PlatformSettings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailList {
		return model.PlatformSettings{}, ErrInjected
	}
	return m.Settings, nil
}

// SetPlatformSettings stores the override.
func (m *FakeMetadata) SetPlatformSettings(_ context.Context, days, warnDays *int, actor string) (model.PlatformSettings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailUpdate {
		return model.PlatformSettings{}, ErrInjected
	}
	m.Settings = model.PlatformSettings{ExpirationDays: days, ExpiringSoonDays: warnDays, UpdatedBy: actor}
	return m.Settings, nil
}

// ---- client users (self-service tenants) -------------------------------------------------------

// ListClientUsers returns stored clients.
func (m *FakeMetadata) ListClientUsers(context.Context) ([]model.ClientUser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailList {
		return nil, ErrInjected
	}
	out := []model.ClientUser{}
	for _, u := range m.ClientUsers {
		out = append(out, *u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	return out, nil
}

// GetClientUser finds a client by id.
func (m *FakeMetadata) GetClientUser(_ context.Context, id int64) (model.ClientUser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, ok := m.ClientUsers[id]; ok {
		return *u, nil
	}
	return model.ClientUser{}, model.ErrNotFound
}

// GetClientUserByUsername finds a client by username.
func (m *FakeMetadata) GetClientUserByUsername(_ context.Context, username string) (model.ClientUser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.ClientUsers {
		if u.Username == username {
			return *u, nil
		}
	}
	return model.ClientUser{}, model.ErrNotFound
}

// CreateClientUser stores a new client.
func (m *FakeMetadata) CreateClientUser(_ context.Context, username, passwordHash, createdBy string) (model.ClientUser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailInsert {
		return model.ClientUser{}, ErrInjected
	}
	for _, u := range m.ClientUsers {
		if u.Username == username {
			return model.ClientUser{}, model.ErrConflict
		}
	}
	m.nextUserID++
	u := model.ClientUser{ID: m.nextUserID, Username: username, PasswordHash: passwordHash, CreatedBy: createdBy}
	m.ClientUsers[u.ID] = &u
	return u, nil
}

func (m *FakeMetadata) clientUser(id int64, fn func(*model.ClientUser)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailUpdate {
		return ErrInjected
	}
	u, ok := m.ClientUsers[id]
	if !ok {
		return model.ErrNotFound
	}
	fn(u)
	return nil
}

// UpdateClientUserPassword stores a new password hash.
func (m *FakeMetadata) UpdateClientUserPassword(_ context.Context, id int64, hash string) error {
	return m.clientUser(id, func(u *model.ClientUser) { u.PasswordHash = hash })
}

// TouchClientUserLogin records a login.
func (m *FakeMetadata) TouchClientUserLogin(_ context.Context, id int64, at time.Time) error {
	return m.clientUser(id, func(u *model.ClientUser) { u.LastLoginAt = &at })
}

// SetClientUserExpiry stores the owner's exception.
func (m *FakeMetadata) SetClientUserExpiry(_ context.Context, id int64, never bool, maxIdleDays *int, actor string) (model.ClientUser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailUpdate {
		return model.ClientUser{}, ErrInjected
	}
	u, ok := m.ClientUsers[id]
	if !ok {
		return model.ClientUser{}, model.ErrNotFound
	}
	u.NeverExpires, u.MaxIdleDays, u.UpdatedBy = never, maxIdleDays, actor
	return *u, nil
}

// DeleteClientUser removes a client.
func (m *FakeMetadata) DeleteClientUser(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.ClientUsers[id]; !ok {
		return model.ErrNotFound
	}
	delete(m.ClientUsers, id)
	return nil
}

// ListByClientUser returns the non-deleted databases owned by one client.
func (m *FakeMetadata) ListByClientUser(_ context.Context, clientUserID int64) ([]model.ClientDatabase, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailList {
		return nil, ErrInjected
	}
	out := []model.ClientDatabase{}
	for _, db := range m.Rows {
		if db.ClientUserID != nil && *db.ClientUserID == clientUserID && db.State != config.StateDeleted {
			out = append(out, *db)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// CountByClientUser counts the non-deleted databases a client owns.
func (m *FakeMetadata) CountByClientUser(_ context.Context, clientUserID int64) (int, error) {
	dbs, err := m.ListByClientUser(context.Background(), clientUserID)
	return len(dbs), err
}

// ---- admins --------------------------------------------------------------------------------

// ListAdmins returns stored admins.
func (m *FakeMetadata) ListAdmins(context.Context) ([]model.Admin, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailList {
		return nil, ErrInjected
	}
	out := []model.Admin{}
	for _, a := range m.Admins {
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	return out, nil
}

// GetAdmin finds an admin.
func (m *FakeMetadata) GetAdmin(_ context.Context, username string) (model.Admin, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailList {
		return model.Admin{}, ErrInjected
	}
	if a, ok := m.Admins[username]; ok {
		return *a, nil
	}
	return model.Admin{}, model.ErrNotFound
}

// CreateAdmin stores an admin.
func (m *FakeMetadata) CreateAdmin(_ context.Context, a model.Admin) (model.Admin, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.Admins[a.Username]; ok {
		return a, model.ErrConflict
	}
	a.ID = m.id()
	m.Admins[a.Username] = &a
	return a, nil
}

func (m *FakeMetadata) admin(username string, fn func(*model.Admin)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailUpdate {
		return ErrInjected
	}
	a, ok := m.Admins[username]
	if !ok {
		return model.ErrNotFound
	}
	fn(a)
	return nil
}

// UpdateAdminRole changes a role.
func (m *FakeMetadata) UpdateAdminRole(_ context.Context, username, role string) error {
	return m.admin(username, func(a *model.Admin) { a.Role = role })
}

// UpdateAdminPassword stores a hash.
func (m *FakeMetadata) UpdateAdminPassword(_ context.Context, username, hash string) error {
	return m.admin(username, func(a *model.Admin) { a.PasswordHash = hash })
}

// TouchAdminLogin records a login.
func (m *FakeMetadata) TouchAdminLogin(_ context.Context, username string, at time.Time) error {
	return m.admin(username, func(a *model.Admin) { a.LastLoginAt = &at })
}

// DeleteAdmin removes an admin.
func (m *FakeMetadata) DeleteAdmin(_ context.Context, username string) error {
	return m.admin(username, func(*model.Admin) { delete(m.Admins, username) })
}

// ---- secrets & notifications -------------------------------------------------------------------

// FakeSecrets returns deterministic credentials.
type FakeSecrets struct {
	FailPassword bool
	FailVerifier bool
}

// Password returns a fixed password of length n.
func (s FakeSecrets) Password(n int) (string, error) {
	if s.FailPassword {
		return "", ErrInjected
	}
	return fmt.Sprintf("%0*d", n, 7), nil
}

// ScramVerifier returns a recognisable fake verifier.
func (s FakeSecrets) ScramVerifier(p string) (string, error) {
	if s.FailVerifier {
		return "", ErrInjected
	}
	return "SCRAM-SHA-256$fake:" + p, nil
}

// FakeNotifier records messages.
type FakeNotifier struct {
	mu       sync.Mutex
	Sent     []notify.Message
	Err      error
	Disabled bool
}

// Send records the message.
func (n *FakeNotifier) Send(_ context.Context, m notify.Message) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.Disabled {
		return notify.ErrNoChannels
	}
	if n.Err != nil {
		return n.Err
	}
	n.Sent = append(n.Sent, m)
	return nil
}

// Channels lists fake channels.
func (n *FakeNotifier) Channels() []string {
	if n.Disabled {
		return []string{}
	}
	return []string{"webhook"}
}
