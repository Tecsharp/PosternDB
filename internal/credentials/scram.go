package credentials

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

const (
	// scramIterations matches PostgreSQL's default scram_iterations.
	scramIterations = 4096
	scramSaltLength = 16
	scramKeyLength  = 32
)

// ScramVerifier computes a PostgreSQL SCRAM-SHA-256 password verifier.
//
// Security note: sending a pre-computed verifier in CREATE ROLE ... PASSWORD
// means the plaintext password never reaches the database server, so it
// cannot leak through log_statement, pg_stat_statements or server errors.
// PostgreSQL recognises the "SCRAM-SHA-256$" format and stores it as is.
func (g Generator) ScramVerifier(password string) (string, error) {
	salt := make([]byte, scramSaltLength)
	if _, err := io.ReadFull(g.Random, salt); err != nil {
		return "", err
	}
	return scramVerifierWithSalt(password, salt, scramIterations)
}

// scramVerifierWithSalt implements RFC 5802 / RFC 7677 key derivation.
func scramVerifierWithSalt(password string, salt []byte, iterations int) (string, error) {
	salted, err := pbkdf2.Key(sha256.New, password, salt, iterations, scramKeyLength)
	if err != nil {
		return "", err
	}
	clientKey := hmacSHA256(salted, "Client Key")
	storedKey := sha256.Sum256(clientKey)
	serverKey := hmacSHA256(salted, "Server Key")
	enc := base64.StdEncoding
	return fmt.Sprintf("SCRAM-SHA-256$%d:%s$%s:%s",
		iterations, enc.EncodeToString(salt), enc.EncodeToString(storedKey[:]), enc.EncodeToString(serverKey)), nil
}

func hmacSHA256(key []byte, msg string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(msg))
	return m.Sum(nil)
}
