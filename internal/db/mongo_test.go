package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/feranmi/file-salad-backend/internal/db"
)

func TestConnectBadURI(t *testing.T) {
	if _, err := db.Connect(context.Background(), "not-a-mongo-uri", "x"); err == nil {
		t.Fatal("expected error for bad URI")
	}
}

func TestConnectUnreachable(t *testing.T) {
	// Valid URI shape, nothing listening on this port → ping times out (~3s).
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	if _, err := db.Connect(ctx, "mongodb://127.0.0.1:27999", "x"); err == nil {
		t.Fatal("expected ping error for unreachable mongo")
	}
}

func TestDisconnectNilSafe(t *testing.T) {
	var m *db.Mongo
	if err := m.Disconnect(context.Background()); err != nil {
		t.Fatalf("nil disconnect: %v", err)
	}
}
