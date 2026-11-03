package model

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewClientCodeIsAValidDatabaseNamePrefix(t *testing.T) {
	valid := regexp.MustCompile(`^[a-z][a-z0-9]{5}$`)
	seen := map[string]bool{}
	for i := 0; i < 2000; i++ {
		c := NewClientCode()
		assert.Regexp(t, valid, c, "six lowercase characters, starting with a letter, so %q-name stays a valid slug", c)
		seen[c] = true
	}
	assert.Greater(t, len(seen), 1990, "codes are random, not a counter")
}
