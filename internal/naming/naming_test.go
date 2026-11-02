package naming

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tecsharp/posterndb/internal/config"
)

var sharedDomain = Resolver{BaseDomain: "pachislab.com", HostPrefix: "postern-", PanelHostname: "postern.pachislab.com"}

func TestValidateSlug(t *testing.T) {
	valid := []string{"acme", "cliente1", "taller-motos", "a1b", "x-1-2", strings.Repeat("a", MaxSlugLength)}
	for _, s := range valid {
		assert.NoError(t, ValidateSlug(s), s)
	}
	cases := map[string]error{
		"ab":                                 ErrSlugLength,
		strings.Repeat("a", MaxSlugLength+1): ErrSlugLength,
		"1acme":                              ErrSlugFormat,
		"-acme":                              ErrSlugFormat,
		"acme-":                              ErrSlugFormat,
		"ac--me":                             ErrSlugFormat,
		"Acme":                               ErrSlugFormat,
		"ac_me":                              ErrSlugFormat,
		"acme;drop":                          ErrSlugFormat,
		`ac"me`:                              ErrSlugFormat,
		"acmé":                               ErrSlugFormat,
		"ac me":                              ErrSlugFormat,
	}
	for in, want := range cases {
		assert.ErrorIs(t, ValidateSlug(in), want, in)
	}
}

func TestMaxSlugLengthFitsArchiveIdentifier(t *testing.T) {
	name := ArchiveName(strings.Repeat("a", MaxSlugLength), time.Now())
	assert.LessOrEqual(t, len(name), 63)
}

func TestResolveSharedDomain(t *testing.T) {
	n, err := sharedDomain.Resolve("taller-motos")
	require.NoError(t, err)
	assert.Equal(t, ClientNames{
		Slug:     "taller-motos",
		Database: "pdb_taller_motos",
		Role:     "pdb_taller_motos",
		Hostname: "postern-taller-motos.pachislab.com",
	}, n)
}

func TestResolveDedicatedDomain(t *testing.T) {
	r := Resolver{BaseDomain: "posterndb.com", PanelHostname: "posterndb.com"}
	n, err := r.Resolve("acme")
	require.NoError(t, err)
	assert.Equal(t, "acme.posterndb.com", n.Hostname)
}

func TestResolveRejectsPanelCollision(t *testing.T) {
	r := Resolver{BaseDomain: "pachislab.com", PanelHostname: "postern.pachislab.com"}
	_, err := r.Resolve("postern")
	assert.ErrorIs(t, err, ErrPanelConflict)
}

func TestResolveRejectsLongLabel(t *testing.T) {
	r := Resolver{BaseDomain: "x.com", HostPrefix: strings.Repeat("p", 30)}
	_, err := r.Resolve(strings.Repeat("a", 34))
	assert.ErrorIs(t, err, ErrLabelTooLong)
}

func TestResolveRejectsInvalidSlug(t *testing.T) {
	_, err := sharedDomain.Resolve("Bad Name")
	assert.ErrorIs(t, err, ErrSlugFormat)
}

func TestDatabaseNameIsInjective(t *testing.T) {
	assert.NotEqual(t, DatabaseName("a-bc"), DatabaseName("ab-c"))
	assert.True(t, strings.HasPrefix(DatabaseName("acme"), config.ClientDBPrefix))
}

func TestArchiveName(t *testing.T) {
	at := time.Date(2026, 11, 2, 12, 30, 5, 0, time.FixedZone("x", -6*3600))
	assert.Equal(t, "pdbarch_taller_motos_20261102183005", ArchiveName("taller-motos", at))
}

func TestNormalize(t *testing.T) {
	assert.Equal(t, "acme", Normalize("  ACME \n"))
}

func TestNewResolver(t *testing.T) {
	r := NewResolver(&config.Config{BaseDomain: "d.com", ClientHostPrefix: "p-", PanelHostname: "panel.d.com"})
	assert.Equal(t, "p-x.d.com", r.Hostname("x"))
}

func TestLocalResolver(t *testing.T) {
	r := Resolver{Local: true}
	n, err := r.Resolve("acme")
	require.NoError(t, err)
	assert.Equal(t, ClientNames{Slug: "acme", Database: "pdb_acme", Role: "pdb_acme"}, n)
	assert.Empty(t, r.Hostname("acme"))
	assert.True(t, NewResolver(&config.Config{LocalMode: true}).Local)
}

func TestExtraUsers(t *testing.T) {
	assert.NoError(t, ValidateUserName("bi"))
	assert.NoError(t, ValidateUserName("reporting2026"))
	for _, bad := range []string{"b", "1bi", "Bi", "bi-x", strings.Repeat("a", 17)} {
		assert.ErrorIs(t, ValidateUserName(bad), ErrUserName, bad)
	}
	role := ExtraRoleName(DatabaseName(strings.Repeat("a", MaxSlugLength)), strings.Repeat("u", 16))
	assert.LessOrEqual(t, len(role), 63)
	assert.NotEqual(t, ExtraRoleName("pdb_acme", "bi"), DatabaseName("acme-bi"), "extra roles never collide with owner roles")
}
