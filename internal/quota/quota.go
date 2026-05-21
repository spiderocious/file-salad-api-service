// Package quota is the monthly upload counter — the server-side source of truth
// for both the hosted free-tier cap and the web per-browser cap. Counts live in
// Mongo (durable: clearing client state can't reset the cap) and are checked +
// incremented atomically at presign issuance, so concurrent requests can't race
// past the cap.
package quota

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Counter increments and reads monthly counts keyed by an opaque scope string
// (e.g. "user:u_123" or "web:1.2.3.4:fp_abc").
type Counter struct {
	col *mongo.Collection
	cap int
}

func NewCounter(db *mongo.Database, cap int) *Counter {
	return &Counter{col: db.Collection("upload_counts"), cap: cap}
}

// Cap returns the configured monthly limit.
func (c *Counter) Cap() int { return c.cap }

// docID is "{scope}:{YYYY-MM}" so each scope rolls over monthly.
func docID(scope string) string {
	return scope + ":" + time.Now().UTC().Format("2006-01")
}

// Used returns the current count for a scope this month (0 if none).
func (c *Counter) Used(ctx context.Context, scope string) (int, error) {
	var doc struct {
		Count int `bson:"count"`
	}
	err := c.col.FindOne(ctx, bson.M{"_id": docID(scope)}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("quota used: %w", err)
	}
	return doc.Count, nil
}

// Reserve atomically checks the cap and increments the count if room remains.
// Returns the new count and whether the reservation succeeded. The check and
// increment are one atomic findOneAndUpdate guarded by a filter on count, so
// two concurrent callers at count == cap-1 can't both succeed.
func (c *Counter) Reserve(ctx context.Context, scope string) (used int, ok bool, err error) {
	id := docID(scope)
	// Only increment when current count < cap. upsert creates the doc at 1.
	res := c.col.FindOneAndUpdate(
		ctx,
		bson.M{"_id": id, "count": bson.M{"$lt": c.cap}},
		bson.M{
			"$inc":         bson.M{"count": 1},
			"$setOnInsert": bson.M{"_id": id, "created_at": time.Now().UTC()},
		},
		options.FindOneAndUpdate().
			SetUpsert(true).
			SetReturnDocument(options.After),
	)

	var doc struct {
		Count int `bson:"count"`
	}
	derr := res.Decode(&doc)
	if derr == nil {
		return doc.Count, true, nil
	}
	if derr == mongo.ErrNoDocuments {
		// Filter didn't match (count >= cap) and upsert couldn't insert because
		// the _id already exists → cap reached.
		return c.cap, false, nil
	}
	if mongo.IsDuplicateKeyError(derr) {
		// Rare race: two upserts collided. Treat as cap-check miss; caller can
		// retry or report full. Report current count.
		used, uerr := c.Used(ctx, scope)
		if uerr != nil {
			return 0, false, uerr
		}
		return used, used < c.cap, nil
	}
	return 0, false, fmt.Errorf("quota reserve: %w", derr)
}

// Release decrements a previously reserved count (best-effort compensation if a
// downstream step fails after Reserve). Never goes below zero.
func (c *Counter) Release(ctx context.Context, scope string) error {
	id := docID(scope)
	_, err := c.col.UpdateOne(
		ctx,
		bson.M{"_id": id, "count": bson.M{"$gt": 0}},
		bson.M{"$inc": bson.M{"count": -1}},
	)
	if err != nil {
		return fmt.Errorf("quota release: %w", err)
	}
	return nil
}
