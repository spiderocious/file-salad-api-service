// Package db owns the MongoDB connection. Connect at boot, ping to verify, and
// hand back a *mongo.Database the features use. Disconnect on shutdown. The Node
// skeleton had no DB yet; this is the foundation the Mongo-backed features will
// build on.
package db

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

// Mongo bundles the client and the resolved database handle.
type Mongo struct {
	Client *mongo.Client
	DB     *mongo.Database
}

// Connect dials Mongo and verifies the connection with a ping, then returns the
// handle. A short server-selection timeout keeps boot snappy: if Mongo is down,
// this fails in ~3s rather than blocking on the driver's 30s default. The
// caller is responsible for Disconnect on shutdown.
func Connect(ctx context.Context, uri, dbName string) (*Mongo, error) {
	opts := options.Client().
		ApplyURI(uri).
		SetServerSelectionTimeout(3 * time.Second)

	client, err := mongo.Connect(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx, readpref.Primary()); err != nil {
		// Best-effort cleanup of the half-open client.
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("mongo ping: %w", err)
	}

	return &Mongo{Client: client, DB: client.Database(dbName)}, nil
}

// Disconnect closes the client. Safe to call on a nil receiver.
func (m *Mongo) Disconnect(ctx context.Context) error {
	if m == nil || m.Client == nil {
		return nil
	}
	return m.Client.Disconnect(ctx)
}
