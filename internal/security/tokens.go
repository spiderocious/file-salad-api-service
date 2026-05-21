package security

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// NewRefreshToken returns a fresh opaque refresh token (the raw value handed to
// the client) alongside its sha256 hash (what we store server-side). The raw
// value is never persisted — only the hash — so a store leak can't be replayed.
func NewRefreshToken() (raw, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("read random: %w", err)
	}
	raw = hex.EncodeToString(b)
	return raw, HashToken(raw), nil
}

// HashToken returns the sha256 hex digest of a token. Used to look a refresh
// token up by its hash without storing the raw value.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
