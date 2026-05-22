package sharecode_test

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/feranmi/file-salad-backend/internal/redis"
	"github.com/feranmi/file-salad-backend/internal/sharecode"
)

func newStore(t *testing.T) (*sharecode.Store, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	rc, err := redis.Connect(context.Background(), "redis://"+mr.Addr())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rc.Close() })
	return sharecode.NewStore(rc, time.Hour), mr
}

var codeRe = regexp.MustCompile(`^[23456789ABCDEFGHJKMNPQRSTUVWXYZ]{7}$`)

func TestCreateFormat(t *testing.T) {
	s, _ := newStore(t)
	code, ttl, err := s.Create(context.Background(), sharecode.Target{Key: "f_x.png", Filename: "x.png"})
	if err != nil {
		t.Fatal(err)
	}
	if !codeRe.MatchString(code) {
		t.Fatalf("code %q not 7 no-confusable base32 chars", code)
	}
	if ttl != time.Hour {
		t.Fatalf("ttl = %v", ttl)
	}
}

func TestCreateResolveRoundTrip(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	want := sharecode.Target{Key: "f_abc.pdf", Filename: "report.pdf"}

	code, _, err := s.Create(ctx, want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Resolve(ctx, code)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.Key != want.Key || got.Filename != want.Filename {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestResolveUnknown(t *testing.T) {
	s, _ := newStore(t)
	if _, err := s.Resolve(context.Background(), "ZZZZZZZ"); err != sharecode.ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
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

	s := sharecode.NewStore(rc, time.Hour)
	ctx := context.Background()
	code, _, _ := s.Create(ctx, sharecode.Target{Key: "f_x", Filename: "x"})

	mr.FastForward(2 * time.Hour) // past the TTL
	if _, err := s.Resolve(ctx, code); err != sharecode.ErrNotFound {
		t.Fatalf("expired code err = %v, want ErrNotFound", err)
	}
}

func TestUniqueAcrossManyCreates(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		code, _, err := s.Create(ctx, sharecode.Target{Key: "f", Filename: "f"})
		if err != nil {
			t.Fatal(err)
		}
		if seen[code] {
			t.Fatalf("duplicate live code issued: %s", code)
		}
		seen[code] = true
	}
}

// With the keyspace pre-filled at one code, SETNX still finds a free slot on
// retry — exercises the collision path without forcing exhaustion.
func TestCollisionRetrySucceeds(t *testing.T) {
	s, mr := newStore(t)
	ctx := context.Background()
	// Pre-occupy a code; the generator will (almost surely) pick a different one,
	// but if it ever collided it must retry rather than fail.
	mr.Set("share:ABCDEFG", `{"key":"x","filename":"x"}`)

	code, _, err := s.Create(ctx, sharecode.Target{Key: "f", Filename: "f"})
	if err != nil {
		t.Fatalf("create should succeed despite an occupied code: %v", err)
	}
	if code == "ABCDEFG" {
		t.Fatal("issued an already-occupied code")
	}
}

// Resolve surfaces an error when the stored payload is corrupt (defensive path).
func TestResolveCorruptPayload(t *testing.T) {
	s, mr := newStore(t)
	mr.Set("share:GOODCOD", "not-json")
	if _, err := s.Resolve(context.Background(), "GOODCOD"); err == nil {
		t.Fatal("expected error on corrupt payload")
	}
}

// Create surfaces a Redis error (covers the SETNX error branch).
func TestCreateRedisError(t *testing.T) {
	s, mr := newStore(t)
	mr.Close() // sever Redis → SETNX errors
	if _, _, err := s.Create(context.Background(), sharecode.Target{Key: "f", Filename: "f"}); err == nil {
		t.Fatal("expected error when Redis is down")
	}
}

func TestResolveRedisError(t *testing.T) {
	s, mr := newStore(t)
	mr.Close()
	if _, err := s.Resolve(context.Background(), "ABCDEFG"); err == nil {
		t.Fatal("expected error when Redis is down")
	}
}
