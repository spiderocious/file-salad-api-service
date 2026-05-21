// Package redis owns the Redis connection. Connect at boot, ping to verify, and
// hand back a *redis.Client. Used for refresh-token sessions and the presigned
// download-URL cache. Disconnect on shutdown.
package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Client is the package's thin wrapper so callers depend on us, not the driver
// directly (keeps the swap surface small).
type Client struct {
	*redis.Client
}

// Connect parses the URL, dials, and pings with a short timeout so boot stays
// snappy when Redis is down. The caller closes it on shutdown.
func Connect(ctx context.Context, url string) (*Client, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("redis parse url: %w", err)
	}
	rdb := redis.NewClient(opts)

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	return &Client{rdb}, nil
}

// Close closes the underlying client. Safe on a nil receiver.
func (c *Client) Close() error {
	if c == nil || c.Client == nil {
		return nil
	}
	return c.Client.Close()
}
