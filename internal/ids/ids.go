// Package ids generates opaque identifiers: short random strings for request
// IDs, and resource-prefixed ULIDs for primary keys. Sufficient for IDs and
// idempotency keys — not for secrets/tokens.
package ids

import (
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/oklog/ulid/v2"
)

// NewRaw returns a 24-char hex string (12 random bytes).
func NewRaw() string {
	b := make([]byte, 12)
	// crypto/rand.Read never returns a short read; the error is effectively
	// impossible on supported platforms, but we degrade gracefully rather than
	// panic in a request path.
	if _, err := rand.Read(b); err != nil {
		return "0000000000000000000000000000000000000000000000"[:24]
	}
	return hex.EncodeToString(b)
}

// Prefixed returns a resource-prefixed, monotonically sortable ULID id, e.g.
// Prefixed("u") -> "u_01hv8qjz...". Clients treat these as opaque strings.
func Prefixed(prefix string) string {
	return prefix + "_" + strings.ToLower(ulid.Make().String())
}
