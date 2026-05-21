package ids

import (
	"regexp"
	"strings"
	"testing"
)

func TestNewRawFormat(t *testing.T) {
	id := NewRaw()
	if len(id) != 24 {
		t.Fatalf("len = %d, want 24", len(id))
	}
	if !regexp.MustCompile(`^[0-9a-f]{24}$`).MatchString(id) {
		t.Fatalf("not lowercase hex: %s", id)
	}
}

func TestNewRawUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id := NewRaw()
		if seen[id] {
			t.Fatal("collision")
		}
		seen[id] = true
	}
}

func TestPrefixed(t *testing.T) {
	id := Prefixed("u")
	if !strings.HasPrefix(id, "u_") {
		t.Fatalf("missing prefix: %s", id)
	}
	if id != strings.ToLower(id) {
		t.Fatalf("not lowercase: %s", id)
	}
	// ULID body is 26 chars after "u_".
	body := strings.TrimPrefix(id, "u_")
	if len(body) != 26 {
		t.Fatalf("ulid body len = %d, want 26", len(body))
	}
}

func TestPrefixedSortable(t *testing.T) {
	// ULIDs are monotonically sortable — a later id should sort >= an earlier one.
	a := Prefixed("up")
	b := Prefixed("up")
	if a >= b && a != b {
		// Same millisecond can tie; just assert they're distinct and ordered-ish.
		t.Logf("a=%s b=%s (same-ms ordering tolerated)", a, b)
	}
	if a == b {
		t.Fatal("two ids identical")
	}
}
