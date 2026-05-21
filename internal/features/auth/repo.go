package auth

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Repo is the Mongo data-access layer for users. SQL/queries only — no business
// logic. Mirrors the Node backend's *.repo.ts.
type Repo struct {
	users *mongo.Collection
}

func NewRepo(db *mongo.Database) *Repo {
	return &Repo{users: db.Collection("users")}
}

// EnsureIndexes creates the unique email index. Idempotent — safe to call at
// every boot.
func (r *Repo) EnsureIndexes(ctx context.Context) error {
	_, err := r.users.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "email", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("uniq_email"),
	})
	return err
}

// Insert creates a user. Returns ErrEmailTaken on a duplicate-key violation.
func (r *Repo) Insert(ctx context.Context, u *User) error {
	_, err := r.users.InsertOne(ctx, u)
	if mongo.IsDuplicateKeyError(err) {
		return ErrEmailTaken
	}
	return err
}

// FindByEmail returns the user or ErrUserNotFound.
func (r *Repo) FindByEmail(ctx context.Context, email string) (*User, error) {
	var u User
	err := r.users.FindOne(ctx, bson.M{"email": email}).Decode(&u)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// FindByID returns the user or ErrUserNotFound.
func (r *Repo) FindByID(ctx context.Context, id string) (*User, error) {
	var u User
	err := r.users.FindOne(ctx, bson.M{"_id": id}).Decode(&u)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

var (
	// ErrEmailTaken is returned by Insert on a duplicate email.
	ErrEmailTaken = errors.New("email taken")
	// ErrUserNotFound is returned by the finders when no row matches.
	ErrUserNotFound = errors.New("user not found")
)

// touch sets created/updated timestamps (UTC).
func touch(u *User) {
	now := time.Now().UTC()
	if u.CreatedAt.IsZero() {
		u.CreatedAt = now
	}
	u.UpdatedAt = now
}
