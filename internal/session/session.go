// Package session manages refresh-token sessions in Redis, including rotation
// and reuse detection. The raw refresh token never touches Redis — only its
// sha256 hash is a key. TTL handles expiry (no sweep job).
//
// Keys:
//
//	refresh:{hash}          -> userID            (EX = refresh TTL)
//	user_sessions:{userID}  -> SET of {hash}     (so "revoke all" is one op)
package session

import (
	"context"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/feranmi/file-salad-backend/internal/redis"
	"github.com/feranmi/file-salad-backend/internal/security"
)

// ErrNotFound means the refresh token isn't an active session (expired, logged
// out, used, or never existed). Because rotation consumes tokens via GETDEL, a
// replay of a used token also surfaces as ErrNotFound — the service rejects it
// as an invalid token rather than trusting the bearer.
var ErrNotFound = errors.New("session not found")

type Store struct {
	rdb *redis.Client
	ttl time.Duration
}

func NewStore(rdb *redis.Client, ttl time.Duration) *Store {
	return &Store{rdb: rdb, ttl: ttl}
}

func refreshKey(hash string) string   { return "refresh:" + hash }
func userSetKey(userID string) string { return "user_sessions:" + userID }

// Create issues a new refresh token for the user and records it. Returns the
// raw token to hand to the client.
func (s *Store) Create(ctx context.Context, userID string) (rawToken string, err error) {
	raw, hash, err := security.NewRefreshToken()
	if err != nil {
		return "", err
	}
	pipe := s.rdb.TxPipeline()
	pipe.Set(ctx, refreshKey(hash), userID, s.ttl)
	pipe.SAdd(ctx, userSetKey(userID), hash)
	pipe.Expire(ctx, userSetKey(userID), s.ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return "", fmt.Errorf("session create: %w", err)
	}
	return raw, nil
}

// Rotate consumes the presented refresh token and issues a fresh one. On a hit,
// the old token is atomically removed and a new session created. On a miss for
// a token the caller believes is well-formed, Rotate revokes ALL of the user's
// sessions is impossible (we don't know the user) — so reuse handling lives in
// the service, which knows the user only after a successful lookup. Here we
// simply report ErrNotFound when the token isn't active.
func (s *Store) Rotate(ctx context.Context, presentedRaw string) (newRaw, userID string, err error) {
	hash := security.HashToken(presentedRaw)

	// GETDEL: atomically read the user id and consume the token.
	uid, err := s.rdb.GetDel(ctx, refreshKey(hash)).Result()
	if errors.Is(err, goredis.Nil) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", fmt.Errorf("session rotate lookup: %w", err)
	}

	// Remove the consumed hash from the user's set, then mint a new session.
	if err := s.rdb.SRem(ctx, userSetKey(uid), hash).Err(); err != nil {
		return "", "", fmt.Errorf("session rotate srem: %w", err)
	}
	newRaw, err = s.Create(ctx, uid)
	if err != nil {
		return "", "", err
	}
	return newRaw, uid, nil
}

// Revoke removes a single session (logout). Idempotent.
func (s *Store) Revoke(ctx context.Context, presentedRaw string) error {
	hash := security.HashToken(presentedRaw)
	uid, err := s.rdb.GetDel(ctx, refreshKey(hash)).Result()
	if errors.Is(err, goredis.Nil) {
		return nil // already gone — logout is idempotent
	}
	if err != nil {
		return fmt.Errorf("session revoke: %w", err)
	}
	return s.rdb.SRem(ctx, userSetKey(uid), hash).Err()
}

// RevokeAll deletes every session for a user — the reuse-detection hammer.
func (s *Store) RevokeAll(ctx context.Context, userID string) error {
	hashes, err := s.rdb.SMembers(ctx, userSetKey(userID)).Result()
	if err != nil {
		return fmt.Errorf("session revoke-all members: %w", err)
	}
	pipe := s.rdb.TxPipeline()
	for _, h := range hashes {
		pipe.Del(ctx, refreshKey(h))
	}
	pipe.Del(ctx, userSetKey(userID))
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("session revoke-all exec: %w", err)
	}
	return nil
}
