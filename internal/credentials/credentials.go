// Package credentials generates secrets for client database roles.
package credentials

import (
	"crypto/rand"
	"errors"
	"io"
	"math/big"
)

// alphabet is deliberately alphanumeric only: the password ends up inside a
// single-quoted SQL literal (CREATE ROLE ... PASSWORD cannot be bound) and in
// JDBC URLs/properties files, so avoiding quotes, backslashes and URL
// metacharacters removes a whole class of escaping bugs. 62^32 ≈ 190 bits of
// entropy is far beyond brute-force range.
const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// ErrInvalidLength is returned for non-positive lengths.
var ErrInvalidLength = errors.New("password length must be positive")

// Generator creates random passwords from a cryptographic source.
type Generator struct {
	Random io.Reader
}

// New returns a Generator backed by crypto/rand.
func New() Generator {
	return Generator{Random: rand.Reader}
}

// Password returns a uniformly random alphanumeric string of length n.
// crypto/rand.Int uses rejection sampling, so there is no modulo bias.
func (g Generator) Password(n int) (string, error) {
	if n <= 0 {
		return "", ErrInvalidLength
	}
	max := big.NewInt(int64(len(alphabet)))
	out := make([]byte, n)
	for i := range out {
		idx, err := rand.Int(g.Random, max)
		if err != nil {
			return "", err
		}
		out[i] = alphabet[idx.Int64()]
	}
	return string(out), nil
}
