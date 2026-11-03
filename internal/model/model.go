// Package model holds the domain types shared by the store, the provisioning
// service, the auditor and the HTTP layer.
package model

import (
	"crypto/rand"
	"errors"
	"math/big"
	"time"
)

// Domain errors translated to HTTP status codes by the API layer.
var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("already exists")
)

// ClientDatabase is one row of the client_databases metadata table.
type ClientDatabase struct {
	ID         int64      `json:"id"`
	Slug       string     `json:"slug"`
	DBName     string     `json:"database"`
	RoleName   string     `json:"username"`
	Hostname   string     `json:"hostname"`
	State      string     `json:"state"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastSeenAt time.Time  `json:"lastSeenAt"`
	ExpiredAt  *time.Time `json:"expiredAt,omitempty"`
	ArchivedAs string     `json:"archivedAs,omitempty"`
	// SessionsBaseline is the pg_stat_database.sessions counter observed in
	// the previous audit run. A higher value now means new connections were
	// opened in between, even if none is open at the moment of the check.
	SessionsBaseline int64 `json:"-"`
	// InternalSessions counts sessions PosternDB opened itself; the delta
	// since InternalBaseline is subtracted from new client sessions.
	InternalSessions int64      `json:"-"`
	InternalBaseline int64      `json:"-"`
	NeverExpires     bool       `json:"neverExpires"`
	MaxIdleDays      *int       `json:"maxIdleDays,omitempty"`
	GraceUntil       *time.Time `json:"graceUntil,omitempty"`
	ExpiryWarnedAt   *time.Time `json:"-"`
	// ClientUserID, when set, is the self-service client (see ClientUser)
	// this database belongs to. A database with no client user was created
	// through the admin panel and is managed by administrators only.
	ClientUserID *int64 `json:"clientUserId,omitempty"`
}

// AuditEntry is one row of audit_log.
type AuditEntry struct {
	ID         int64          `json:"id"`
	OccurredAt time.Time      `json:"occurredAt"`
	Actor      string         `json:"actor"`
	Action     string         `json:"action"`
	Target     string         `json:"target"`
	Details    map[string]any `json:"details,omitempty"`
}

// Activity is the per-database activity snapshot read from PostgreSQL
// statistics views.
type Activity struct {
	// Connections currently open (pg_stat_activity, client backends only).
	Connections int64
	// Sessions is the cumulative pg_stat_database.sessions counter.
	Sessions int64
}

// DatabaseUser is an additional login role attached to a client database.
type DatabaseUser struct {
	ID         int64     `json:"id"`
	DatabaseID int64     `json:"-"`
	Name       string    `json:"name"`
	RoleName   string    `json:"username"`
	Access     string    `json:"access"`
	CreatedAt  time.Time `json:"createdAt"`
}

// Job is a unit of work for the backup sidecar.
type Job struct {
	ID          int64      `json:"id"`
	Kind        string     `json:"kind"`
	Status      string     `json:"status"`
	Slug        string     `json:"slug"`
	SourceDB    string     `json:"sourceDb,omitempty"`
	TargetDB    string     `json:"targetDb,omitempty"`
	TargetRole  string     `json:"-"`
	File        string     `json:"file,omitempty"`
	Bytes       *int64     `json:"bytes,omitempty"`
	Reason      string     `json:"reason,omitempty"`
	Error       string     `json:"error,omitempty"`
	RequestedBy string     `json:"requestedBy"`
	CreatedAt   time.Time  `json:"createdAt"`
	FinishedAt  *time.Time `json:"finishedAt,omitempty"`
}

// Template is reusable SQL applied to new databases.
type Template struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	SQL         string `json:"sql"`
	// Shared templates are offered to self-service users when they create
	// a database; the others stay admin-only.
	Shared    bool      `json:"shared"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// DatabaseStats summarises size and usage of one client database.
type DatabaseStats struct {
	SizeBytes       int64        `json:"sizeBytes"`
	OpenConnections int64        `json:"openConnections"`
	TotalSessions   int64        `json:"totalSessions"`
	Commits         int64        `json:"commits"`
	Rollbacks       int64        `json:"rollbacks"`
	TableCount      int          `json:"tableCount"`
	TotalRows       int64        `json:"totalRows"`
	TopTables       []TableBrief `json:"topTables"`
}

// Connection is one client session open right now on a database.
type Connection struct {
	PID         int64      `json:"pid"`
	User        string     `json:"user"`
	Application string     `json:"application"`
	State       string     `json:"state"`
	ConnectedAt time.Time  `json:"connectedAt"`
	StateSince  *time.Time `json:"stateSince,omitempty"`
	// Query is only filled while a statement is running or a transaction
	// is open, and truncated.
	Query string `json:"query,omitempty"`
}

// TableBrief is one row of the "largest tables" list.
type TableBrief struct {
	Schema    string `json:"schema"`
	Name      string `json:"name"`
	Rows      int64  `json:"rows"`
	SizeBytes int64  `json:"sizeBytes"`
}

// Schema is the structure of a client database, used by the tree view and
// the entity-relationship diagram.
type Schema struct {
	Schemas []string `json:"schemas"`
	Tables  []Table  `json:"tables"`
}

// Table describes one table or view.
type Table struct {
	Schema      string       `json:"schema"`
	Name        string       `json:"name"`
	Kind        string       `json:"kind"`
	Rows        int64        `json:"rows"`
	SizeBytes   int64        `json:"sizeBytes"`
	Comment     string       `json:"comment,omitempty"`
	Columns     []Column     `json:"columns"`
	ForeignKeys []ForeignKey `json:"foreignKeys"`
	Indexes     []Index      `json:"indexes"`
}

// Column describes one table column.
type Column struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Nullable   bool   `json:"nullable"`
	Default    string `json:"default,omitempty"`
	PrimaryKey bool   `json:"primaryKey"`
}

// ForeignKey links columns of one table to another table.
type ForeignKey struct {
	Name       string   `json:"name"`
	Columns    []string `json:"columns"`
	RefSchema  string   `json:"refSchema"`
	RefTable   string   `json:"refTable"`
	RefColumns []string `json:"refColumns"`
}

// Index describes one index.
type Index struct {
	Name       string `json:"name"`
	Definition string `json:"definition"`
	Unique     bool   `json:"unique"`
}

// QueryResult is the output of a read-only console query.
type QueryResult struct {
	Columns    []string `json:"columns"`
	Rows       [][]any  `json:"rows"`
	RowCount   int      `json:"rowCount"`
	Truncated  bool     `json:"truncated"`
	DurationMs int64    `json:"durationMs"`
}

// ConnectionInfo is everything a client needs to connect, minus the
// password. The panel builds every connection snippet from it.
type ConnectionInfo struct {
	Hostname  string `json:"hostname"`
	Database  string `json:"database"`
	Username  string `json:"username"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	LocalMode bool   `json:"localMode"`
}

// Credentials are returned exactly once, right after a database (or a
// database user) is created or its password is reset. PosternDB never
// stores the password.
type Credentials struct {
	ConnectionInfo
	Password string `json:"password"`
}

// DataColumn describes a column in the data editor.
type DataColumn struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Nullable   bool   `json:"nullable"`
	PrimaryKey bool   `json:"primaryKey"`
	HasDefault bool   `json:"hasDefault"`
	// Generated columns (GENERATED ALWAYS) can never be written.
	Generated bool `json:"generated"`
}

// TableData is one page of rows for the data editor. Values use the
// PostgreSQL text representation; nil means NULL.
type TableData struct {
	Columns  []DataColumn `json:"columns"`
	Rows     [][]*string  `json:"rows"`
	Total    int64        `json:"total"`
	Page     int          `json:"page"`
	PageSize int          `json:"pageSize"`
	// Editable is false for views and for tables without a primary key,
	// because a row could not be addressed unambiguously.
	Editable bool `json:"editable"`
}

// RowValues maps column names to text values (nil = NULL).
type RowValues map[string]*string

// ClientUser is a self-service tenant: someone who registers (or whom the
// owner adds directly) to manage only their own databases through the
// reduced "panel de usuario" — never the admin panel, and never another
// client's data. NeverExpires/MaxIdleDays are the owner-set exception
// applied to every database this client owns that has no exception of its
// own (see expiry.ApplyClientOverride).
type ClientUser struct {
	ID            int64      `json:"id"`
	Username      string     `json:"username"`
	PasswordHash  string     `json:"-"`
	NeverExpires  bool       `json:"neverExpires"`
	MaxIdleDays   *int       `json:"maxIdleDays,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	CreatedBy     string     `json:"createdBy,omitempty"` // "" = self-registered
	UpdatedAt     time.Time  `json:"updatedAt"`
	UpdatedBy     string     `json:"updatedBy,omitempty"`
	DatabaseCount int        `json:"databaseCount,omitempty"`
	LastLoginAt   *time.Time `json:"lastLoginAt,omitempty"`
	// Identity: the email is also the username of every new account.
	Email           string     `json:"email,omitempty"`
	FullName        string     `json:"fullName,omitempty"`
	GoogleSub       string     `json:"-"`
	EmailVerifiedAt *time.Time `json:"emailVerifiedAt,omitempty"`
	// Code is the user's fixed short code; it leads the name of their
	// databases so two people can use the same name.
	Code string `json:"code"`
	// Role is user, viewer or admin. The owner (ADMIN_USER in .env) is not an
	// account, so no role can ever refer to it.
	Role string `json:"role"`
	// NeedsUsername is set for accounts created with Google until the person
	// picks a username.
	NeedsUsername bool `json:"needsUsername"`
	// Derived from the sensitive fields above by Derive; safe to serialise.
	GoogleLinked bool `json:"googleLinked"`
	HasPassword  bool `json:"hasPassword"`
}

// Derive fills the fields computed from PasswordHash and GoogleSub.
func (u *ClientUser) Derive() {
	u.GoogleLinked = u.GoogleSub != ""
	u.HasPassword = u.PasswordHash != ""
}

// NewClientUser is everything needed to create an account. PasswordHash and
// GoogleSub may each be empty, but not both.
type NewClientUser struct {
	Username         string
	Email            string
	FullName         string
	PasswordHash     string
	GoogleSub        string
	CreatedBy        string // "" = self-registered
	EmailVerified    bool
	EmailUseAccepted bool
	NeedsUsername    bool
	// Code is generated by the service; the database falls back to a random
	// one when it is empty.
	Code string
	// Role defaults to user.
	Role string
}

// PlatformSettings overrides the .env inactivity policy at runtime. A nil
// field means "use the .env value"; only the owner can change this.
type PlatformSettings struct {
	ExpirationDays   *int      `json:"expirationDays"`
	ExpiringSoonDays *int      `json:"expiringSoonDays"`
	UpdatedAt        time.Time `json:"updatedAt"`
	UpdatedBy        string    `json:"updatedBy,omitempty"`
}

const (
	clientCodeLength   = 6
	clientCodeLetters  = "abcdefghijklmnopqrstuvwxyz"
	clientCodeAlphabet = clientCodeLetters + "0123456789"
)

// NewClientCode returns a random user code: six lowercase characters that
// start with a letter, so the database names built from it stay valid.
func NewClientCode() string {
	out := make([]byte, clientCodeLength)
	for i := range out {
		set := clientCodeAlphabet
		if i == 0 {
			set = clientCodeLetters
		}
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(set))))
		if err != nil {
			panic("crypto/rand unavailable: " + err.Error())
		}
		out[i] = set[n.Int64()]
	}
	return string(out)
}
