// Package sharecode mints short, human-shareable codes that map to an uploaded
// object for a limited time. A code is 7 characters from a 32-symbol alphabet
// with confusable characters removed (no 0/O/1/I/L), so it's easy to read aloud
// or type. Codes live in Redis with a TTL, so they expire on their own — an old
// code never silently resolves to a different file (the key is gone), and a new
// code can never collide with a live one (SETNX guarantees uniqueness at issue).
package sharecode

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/feranmi/file-salad-backend/internal/redis"
)

// alphabet: digits 2-9 plus A-Z with the confusable letters removed (I, L, O).
// 31 symbols → 31^7 ≈ 27.5 billion codes, so collisions among live codes are
// effectively never (and SETNX makes them impossible to actually issue).
const alphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

const (
	codeLen     = 7
	maxAttempts = 5 // SETNX collision retries; the keyspace is huge, so this is plenty
)

// ErrNotFound means the code is unknown or expired.
var ErrNotFound = errors.New("share code not found")

// ErrExhausted means we couldn't mint a unique code within maxAttempts (only
// possible if the keyspace is implausibly full — treated as a 500 by the caller).
var ErrExhausted = errors.New("could not allocate a unique share code")

// Target is what a code resolves to: the object key plus a display filename.
type Target struct {
	Key      string `json:"key"`
	Filename string `json:"filename"`
}

// Store mints and resolves share codes in Redis.
type Store struct {
	rdb *redis.Client
	ttl time.Duration
}

func NewStore(rdb *redis.Client, ttl time.Duration) *Store {
	return &Store{rdb: rdb, ttl: ttl}
}

func key(code string) string { return "share:" + code }

// Create mints a unique code for the target and stores it with the TTL. Retries
// on the (astronomically rare) collision. Returns the code and its TTL.
func (s *Store) Create(ctx context.Context, t Target) (code string, ttl time.Duration, err error) {
	payload, err := json.Marshal(t)
	if err != nil {
		return "", 0, fmt.Errorf("sharecode marshal: %w", err)
	}
	for i := 0; i < maxAttempts; i++ {
		c, gerr := generate()
		if gerr != nil {
			return "", 0, gerr
		}
		ok, serr := s.rdb.SetNX(ctx, key(c), payload, s.ttl).Result()
		if serr != nil {
			return "", 0, fmt.Errorf("sharecode setnx: %w", serr)
		}
		if ok {
			return c, s.ttl, nil
		}
		// collision: code already live → try another
	}
	return "", 0, ErrExhausted
}

// Resolve returns the target a code points to, or ErrNotFound if it's unknown or
// expired. (Reading does not extend the TTL — the 24h window is from issuance.)
func (s *Store) Resolve(ctx context.Context, code string) (*Target, error) {
	raw, err := s.rdb.Get(ctx, key(code)).Result()
	if errors.Is(err, goredis.Nil) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("sharecode get: %w", err)
	}
	var t Target
	if err := json.Unmarshal([]byte(raw), &t); err != nil {
		return nil, fmt.Errorf("sharecode unmarshal: %w", err)
	}
	return &t, nil
}

// generate returns a fresh random code of codeLen symbols from the alphabet,
// using rejection sampling for an unbiased draw.
func generate() (string, error) {
	out := make([]byte, codeLen)
	buf := make([]byte, codeLen)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("sharecode rand: %w", err)
	}
	n := byte(len(alphabet))
	// 256 % n introduces tiny bias; with n=31 it's negligible, but reject the
	// top of the range to stay unbiased.
	limit := byte(256 - (256 % int(n)))
	for i := 0; i < codeLen; i++ {
		b := buf[i]
		for b >= limit {
			one := make([]byte, 1)
			if _, err := rand.Read(one); err != nil {
				return "", fmt.Errorf("sharecode rand: %w", err)
			}
			b = one[0]
		}
		out[i] = alphabet[b%n]
	}
	return string(out), nil
}
