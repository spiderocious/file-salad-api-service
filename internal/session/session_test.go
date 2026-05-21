package session_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/feranmi/file-salad-backend/internal/redis"
	"github.com/feranmi/file-salad-backend/internal/session"
)

func newStore(t *testing.T) (*session.Store, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	rc, err := redis.Connect(context.Background(), "redis://"+mr.Addr())
	if err != nil {
		t.Fatalf("redis connect: %v", err)
	}
	t.Cleanup(func() { _ = rc.Close() })

	return session.NewStore(rc, time.Hour), mr
}

func TestCreateAndRotate(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	raw, err := s.Create(ctx, "u_1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if raw == "" {
		t.Fatal("empty token")
	}

	newRaw, uid, err := s.Rotate(ctx, raw)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if uid != "u_1" {
		t.Fatalf("uid = %q", uid)
	}
	if newRaw == raw {
		t.Fatal("rotate did not change the token")
	}

	// The old token is consumed → rotating it again is ErrNotFound.
	if _, _, err := s.Rotate(ctx, raw); err != session.ErrNotFound {
		t.Fatalf("reused token err = %v, want ErrNotFound", err)
	}
	// The new token still works.
	if _, _, err := s.Rotate(ctx, newRaw); err != nil {
		t.Fatalf("new token rotate: %v", err)
	}
}

func TestRotateUnknown(t *testing.T) {
	s, _ := newStore(t)
	if _, _, err := s.Rotate(context.Background(), "deadbeef"); err != session.ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestRevokeIdempotent(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	raw, _ := s.Create(ctx, "u_2")
	if err := s.Revoke(ctx, raw); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	// Revoking again is a no-op (idempotent).
	if err := s.Revoke(ctx, raw); err != nil {
		t.Fatalf("second revoke: %v", err)
	}
	// Revoked token can't rotate.
	if _, _, err := s.Rotate(ctx, raw); err != session.ErrNotFound {
		t.Fatalf("rotate after revoke = %v, want ErrNotFound", err)
	}
}

func TestRevokeAll(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	a, _ := s.Create(ctx, "u_3")
	b, _ := s.Create(ctx, "u_3")

	if err := s.RevokeAll(ctx, "u_3"); err != nil {
		t.Fatalf("revoke all: %v", err)
	}
	for _, tok := range []string{a, b} {
		if _, _, err := s.Rotate(ctx, tok); err != session.ErrNotFound {
			t.Fatalf("token still valid after RevokeAll: %v", err)
		}
	}
}

func TestExpiry(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()
	rc, _ := redis.Connect(context.Background(), "redis://"+mr.Addr())
	defer func() { _ = rc.Close() }()

	s := session.NewStore(rc, time.Hour)
	ctx := context.Background()
	raw, _ := s.Create(ctx, "u_4")

	// Fast-forward past the TTL — miniredis honours expiry on FastForward.
	mr.FastForward(2 * time.Hour)
	if _, _, err := s.Rotate(ctx, raw); err != session.ErrNotFound {
		t.Fatalf("expired token err = %v, want ErrNotFound", err)
	}
}
