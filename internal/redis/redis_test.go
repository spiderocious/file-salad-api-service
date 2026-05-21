package redis_test

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"

	"github.com/feranmi/file-salad-backend/internal/redis"
)

func TestConnectSuccess(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	c, err := redis.Connect(context.Background(), "redis://"+mr.Addr())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestConnectBadURL(t *testing.T) {
	if _, err := redis.Connect(context.Background(), "://not a url"); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestConnectUnreachable(t *testing.T) {
	// Valid URL, nothing listening → ping fails fast.
	if _, err := redis.Connect(context.Background(), "redis://127.0.0.1:6390"); err == nil {
		t.Fatal("expected ping error for unreachable redis")
	}
}

func TestCloseNilSafe(t *testing.T) {
	var c *redis.Client
	if err := c.Close(); err != nil {
		t.Fatalf("nil close: %v", err)
	}
}
