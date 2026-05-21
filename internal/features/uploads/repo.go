package uploads

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Repo is the Mongo data-access layer for uploads (shared by hosted + web).
type Repo struct {
	col *mongo.Collection
}

func NewRepo(db *mongo.Database) *Repo {
	return &Repo{col: db.Collection("uploads")}
}

// EnsureIndexes creates the indexes uploads queries rely on. Idempotent.
func (r *Repo) EnsureIndexes(ctx context.Context) error {
	_, err := r.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "key", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("uniq_key"),
		},
		{
			// Owner listing, newest first (cursor pagination).
			Keys:    bson.D{{Key: "owner_id", Value: 1}, {Key: "_id", Value: -1}},
			Options: options.Index().SetName("owner_created"),
		},
	})
	return err
}

func (r *Repo) Insert(ctx context.Context, u *Upload) error {
	_, err := r.col.InsertOne(ctx, u)
	return err
}

var ErrNotFound = errors.New("upload not found")

// FindByIDOwner returns an upload scoped to an owner (hosted). Cross-owner
// access yields ErrNotFound (never reveal another user's records).
func (r *Repo) FindByIDOwner(ctx context.Context, id, ownerID string) (*Upload, error) {
	var u Upload
	err := r.col.FindOne(ctx, bson.M{"_id": id, "owner_id": ownerID}).Decode(&u)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// FindByIDWeb returns a web upload by id (no owner). Web records have no owner,
// so anyone with the id can resolve a download URL — acceptable for v1.
func (r *Repo) FindByIDWeb(ctx context.Context, id string) (*Upload, error) {
	var u Upload
	err := r.col.FindOne(ctx, bson.M{"_id": id, "surface": SurfaceWeb}).Decode(&u)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// MarkReady flips a pending upload to ready for the given owner.
func (r *Repo) MarkReady(ctx context.Context, id, ownerID string) (*Upload, error) {
	res := r.col.FindOneAndUpdate(
		ctx,
		bson.M{"_id": id, "owner_id": ownerID},
		bson.M{"$set": bson.M{"status": StatusReady}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	)
	var u Upload
	err := res.Decode(&u)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// ListByOwner returns up to limit+1 uploads for an owner, newest first, starting
// after the cursor id (empty cursor = first page). The caller uses the extra
// item to compute has_more.
func (r *Repo) ListByOwner(ctx context.Context, ownerID, cursor string, limit int) ([]Upload, error) {
	filter := bson.M{"owner_id": ownerID}
	if cursor != "" {
		// _id is a ULID — lexicographically sortable — so "older than cursor"
		// is _id < cursor under the descending sort.
		filter["_id"] = bson.M{"$lt": cursor}
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "_id", Value: -1}}).
		SetLimit(int64(limit) + 1)

	cur, err := r.col.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cur.Close(ctx) }()

	var out []Upload
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func nowExpiry(days int) time.Time {
	return time.Now().UTC().AddDate(0, 0, days)
}

func nowUTC() time.Time { return time.Now().UTC() }
