// Package naming turns a client name typed in the panel into the identifiers
// used in PostgreSQL and DNS.
//
// Security note: CREATE DATABASE / CREATE ROLE cannot take bind parameters,
// so identifiers end up inside SQL text. The strict whitelist below is the
// first line of defence (the second is pgx.Identifier quoting in the store).
// Only lowercase letters, digits and single hyphens are accepted, which is a
// safe subset of both DNS labels and PostgreSQL identifiers.
package naming

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/tecsharp/posterndb/internal/config"
)

const (
	// MinSlugLength keeps names meaningful and avoids accidental one-letter DBs.
	MinSlugLength = 3
	// MaxSlugLength is bounded by the archived database name, the longest
	// identifier we derive: "pdbarch_" + slug + "_" + 14-digit timestamp
	// must fit PostgreSQL's 63-byte identifier limit.
	MaxSlugLength = 63 - len(config.ArchivedDBPrefix) - 1 - len(config.ArchiveTimeLayout)
	// maxDNSLabel is the RFC 1035 limit for a single label.
	maxDNSLabel = 63
)

var (
	slugPattern     = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	userNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]{1,15}$`)
)

// Validation errors returned to the panel (shown to the operator as is).
var (
	ErrSlugLength    = fmt.Errorf("el nombre debe tener entre %d y %d caracteres", MinSlugLength, MaxSlugLength)
	ErrSlugFormat    = errors.New("el nombre debe empezar con una letra y usar solo a-z, 0-9 y guiones sueltos")
	ErrLabelTooLong  = errors.New("el nombre junto con el prefijo del host supera los 63 caracteres que permite DNS")
	ErrPanelConflict = errors.New("ese nombre chocaría con el host del panel")
	ErrUserName      = errors.New("el nombre de usuario debe empezar con una letra y tener de 2 a 16 caracteres a-z o 0-9")
)

// ClientNames are all identifiers derived from one client slug.
type ClientNames struct {
	Slug     string
	Database string
	Role     string
	Hostname string
}

// Resolver derives names using the hostname scheme configured in .env:
// <CLIENT_HOST_PREFIX><slug>.<BASE_DOMAIN>. With BASE_DOMAIN=pachislab.com
// and CLIENT_HOST_PREFIX=postern- a client "acme" becomes
// postern-acme.pachislab.com (one level deep, so Cloudflare's free Universal
// SSL wildcard covers it).
//
// In local mode there is no public hostname: Hostname returns "".
type Resolver struct {
	BaseDomain    string
	HostPrefix    string
	PanelHostname string
	Local         bool
}

// NewResolver builds a Resolver from configuration.
func NewResolver(cfg *config.Config) Resolver {
	return Resolver{BaseDomain: cfg.BaseDomain, HostPrefix: cfg.ClientHostPrefix, PanelHostname: cfg.PanelHostname, Local: cfg.LocalMode}
}

// Normalize trims and lowercases user input before validation.
func Normalize(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// ValidateSlug checks the format shared by every route that receives a slug
// (create, delete, lookups), so handlers never touch unvalidated names.
func ValidateSlug(slug string) error {
	if len(slug) < MinSlugLength || len(slug) > MaxSlugLength {
		return ErrSlugLength
	}
	if !slugPattern.MatchString(slug) {
		return ErrSlugFormat
	}
	return nil
}

// Resolve validates the slug and returns the derived identifiers.
func (r Resolver) Resolve(slug string) (ClientNames, error) {
	if err := ValidateSlug(slug); err != nil {
		return ClientNames{}, err
	}
	db := DatabaseName(slug)
	if r.Local {
		return ClientNames{Slug: slug, Database: db, Role: db}, nil
	}
	if len(r.HostPrefix+slug) > maxDNSLabel {
		return ClientNames{}, ErrLabelTooLong
	}
	host := r.Hostname(slug)
	if host == r.PanelHostname {
		return ClientNames{}, ErrPanelConflict
	}
	return ClientNames{Slug: slug, Database: db, Role: db, Hostname: host}, nil
}

// Hostname returns the public hostname for a slug under the current scheme.
func (r Resolver) Hostname(slug string) string {
	if r.Local {
		return ""
	}
	return r.HostPrefix + slug + "." + r.BaseDomain
}

// ValidateUserName checks the short name of an additional database user.
func ValidateUserName(name string) error {
	if !userNamePattern.MatchString(name) {
		return ErrUserName
	}
	return nil
}

// ExtraRoleName derives the login role of an additional user. Slug-derived
// names never contain "__" (hyphens become single underscores and slugs have
// no consecutive hyphens), so the double underscore keeps extra roles from
// ever colliding with another client's owner role.
func ExtraRoleName(database, user string) string {
	return database + config.ExtraUserSep + user
}

// DatabaseName maps a slug to its PostgreSQL database (and role) name.
// Hyphens become underscores; because slugs never contain underscores the
// mapping is injective, so two slugs can never produce the same identifier.
func DatabaseName(slug string) string {
	return config.ClientDBPrefix + strings.ReplaceAll(slug, "-", "_")
}

// ArchiveName returns the name a database is renamed to while it waits for
// the backup sidecar to dump and drop it. The distinct prefix guarantees it
// cannot collide with a live client database.
func ArchiveName(slug string, at time.Time) string {
	return config.ArchivedDBPrefix + strings.ReplaceAll(slug, "-", "_") + "_" + at.UTC().Format(config.ArchiveTimeLayout)
}
