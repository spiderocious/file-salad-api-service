//go:build integration

package quota_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/feranmi/file-salad-backend/internal/db"
	"github.com/feranmi/file-salad-backend/internal/quota"
)

func mongoURI() string {
	if v := os.Getenv("MONGODB_URI"); v != "" {
		return v
	}
	return "mongodb://localhost:27017"
}

func newCounter(t *testing.T, cap int) *quota.Counter {
	t.Helper()
	ctx := context.Background()
	dbName := fmt.Sprintf("filesalad_quota_it_%d", time.Now().UnixNano())
	mc, err := db.Connect(ctx, mongoURI(), dbName)
	if err != nil {
		t.Skipf("MongoDB not reachable (%v) — skipping", err)
	}
	t.Cleanup(func() {
		_ = mc.DB.Drop(ctx)
		_ = mc.Disconnect(ctx)
	})
	return quota.NewCounter(mc.DB, cap)
}

func TestReserveAndUsed(t *testing.T) {
	c := newCounter(t, 3)
	ctx := context.Background()
	scope := "user:u_1"

	if u, _ := c.Used(ctx, scope); u != 0 {
		t.Fatalf("initial used = %d", u)
	}

	for i := 1; i <= 3; i++ {
		used, ok, err := c.Reserve(ctx, scope)
		if err != nil || !ok {
			t.Fatalf("reserve %d: ok=%v err=%v", i, ok, err)
		}
		if used != i {
			t.Fatalf("reserve %d: used=%d", i, used)
		}
	}
	// 4th over cap → ok=false
	used, ok, err := c.Reserve(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("4th reserve should fail (cap 3)")
	}
	if used != 3 {
		t.Fatalf("over-cap used = %d, want 3", used)
	}

	if u, _ := c.Used(ctx, scope); u != 3 {
		t.Fatalf("final used = %d", u)
	}
}

func TestRelease(t *testing.T) {
	c := newCounter(t, 5)
	ctx := context.Background()
	scope := "user:u_2"

	_, _, _ = c.Reserve(ctx, scope)
	_, _, _ = c.Reserve(ctx, scope)
	if err := c.Release(ctx, scope); err != nil {
		t.Fatal(err)
	}
	if u, _ := c.Used(ctx, scope); u != 1 {
		t.Fatalf("after release used = %d, want 1", u)
	}
	// Release never goes below zero.
	_ = c.Release(ctx, scope)
	_ = c.Release(ctx, scope)
	if u, _ := c.Used(ctx, scope); u != 0 {
		t.Fatalf("over-release used = %d, want 0", u)
	}
}

func TestScopesAreIndependent(t *testing.T) {
	c := newCounter(t, 2)
	ctx := context.Background()
	_, _, _ = c.Reserve(ctx, "user:a")
	_, _, _ = c.Reserve(ctx, "user:a")
	// a is at cap; b is fresh
	if _, ok, _ := c.Reserve(ctx, "user:a"); ok {
		t.Fatal("a should be capped")
	}
	if _, ok, _ := c.Reserve(ctx, "web:1.2.3.4:fp"); !ok {
		t.Fatal("different scope should have its own counter")
	}
}

// The cap must hold under concurrency: many goroutines racing at the boundary,
// exactly `cap` should succeed.
func TestReserveConcurrent(t *testing.T) {
	cap := 5
	c := newCounter(t, cap)
	ctx := context.Background()
	scope := "user:race"

	const n = 50
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok, err := c.Reserve(ctx, scope); err == nil && ok {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if successes != cap {
		t.Fatalf("concurrent reserve: %d succeeded, want exactly %d", successes, cap)
	}
	if u, _ := c.Used(ctx, scope); u != cap {
		t.Fatalf("final used = %d, want %d", u, cap)
	}
}
