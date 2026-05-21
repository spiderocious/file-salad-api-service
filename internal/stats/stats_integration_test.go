//go:build integration

package stats_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/feranmi/file-salad-backend/internal/db"
	"github.com/feranmi/file-salad-backend/internal/stats"
)

func mongoURI() string {
	if v := os.Getenv("MONGODB_URI"); v != "" {
		return v
	}
	return "mongodb://localhost:27017"
}

func newCounter(t *testing.T) *stats.Counter {
	t.Helper()
	ctx := context.Background()
	name := fmt.Sprintf("filesalad_stats_it_%d", time.Now().UnixNano())
	mc, err := db.Connect(ctx, mongoURI(), name)
	if err != nil {
		t.Skipf("MongoDB not reachable (%v) — skipping", err)
	}
	t.Cleanup(func() {
		_ = mc.DB.Drop(ctx)
		_ = mc.Disconnect(ctx)
	})
	return stats.NewCounter(mc.DB)
}

func TestIncrementAndTotal(t *testing.T) {
	c := newCounter(t)
	ctx := context.Background()

	if total, err := c.Total(ctx); err != nil || total != 0 {
		t.Fatalf("initial total = %d err=%v, want 0", total, err)
	}

	for i := 0; i < 5; i++ {
		if err := c.Increment(ctx); err != nil {
			t.Fatalf("increment %d: %v", i, err)
		}
	}

	total, err := c.Total(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 {
		t.Fatalf("total = %d, want 5", total)
	}
}

func TestIncrementAsyncEventuallyCounts(t *testing.T) {
	c := newCounter(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		c.IncrementAsync()
	}

	// Async — poll until the writes land.
	deadline := time.Now().Add(3 * time.Second)
	var total int64
	for time.Now().Before(deadline) {
		total, _ = c.Total(ctx)
		if total >= 3 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if total != 3 {
		t.Fatalf("async total = %d, want 3", total)
	}
}
