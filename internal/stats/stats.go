// Package stats maintains a lightweight, monotonically-increasing counter of
// total presigns issued (across hosted + web) — a public "files shared so far"
// number for the web one-pager.
//
// The increment is deliberately decoupled from the presign request path: handlers
// call IncrementAsync after the response is already written, so a slow or failing
// counter write can never delay or break a presign. The counter is a vanity
// metric, so losing an increment on a crash is acceptable.
package stats

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/feranmi/file-salad-backend/internal/logger"
)

// counterID is the single doc that holds the running total.
const counterID = "uploads_total"

// Counter increments and reads the global upload count in Mongo.
type Counter struct {
	col *mongo.Collection
}

func NewCounter(db *mongo.Database) *Counter {
	return &Counter{col: db.Collection("counters")}
}

// Increment adds one to the total (upserting the doc on first use). Synchronous;
// returns any error. Most callers want IncrementAsync.
func (c *Counter) Increment(ctx context.Context) error {
	_, err := c.col.UpdateByID(
		ctx,
		counterID,
		bson.M{
			"$inc": bson.M{"count": 1},
			"$set": bson.M{"updated_at": time.Now().UTC()},
		},
		options.Update().SetUpsert(true),
	)
	return err
}

// IncrementAsync fires the increment in a background goroutine and returns
// immediately. It uses a fresh, timeout-bounded context (not the request's,
// which is canceled once the response is sent) and recovers from any panic, so
// it can never delay a response or crash the process. Failures are logged, not
// surfaced.
func (c *Counter) IncrementAsync() {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error(context.Background(), "stats increment panicked", "recover", r)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := c.Increment(ctx); err != nil {
			logger.Warn(ctx, "stats increment failed", "err", err.Error())
		}
	}()
}

// Total returns the current count (0 if the counter doc doesn't exist yet).
func (c *Counter) Total(ctx context.Context) (int64, error) {
	var doc struct {
		Count int64 `bson:"count"`
	}
	err := c.col.FindOne(ctx, bson.M{"_id": counterID}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return doc.Count, nil
}
