package uploads

import "time"

// Upload is a persisted upload record. Surface is "hosted" (this feature) or
// "web" (the anonymous one-pager). OwnerID is empty for web.
//
// We persist only the object Key, not a URL. Every URL handed to a client is a
// freshly presigned GET (the durable identifier is the key); a stored URL would
// just go stale.
type Upload struct {
	ID          string    `bson:"_id" json:"id"`
	OwnerID     string    `bson:"owner_id,omitempty" json:"owner_id,omitempty"`
	Surface     string    `bson:"surface" json:"surface"`
	Key         string    `bson:"key" json:"key"`
	Filename    string    `bson:"filename" json:"filename"`
	ContentType string    `bson:"content_type" json:"content_type"`
	Size        int64     `bson:"size" json:"size"`
	Status      string    `bson:"status" json:"status"` // pending | ready
	ExpiresAt   time.Time `bson:"expires_at" json:"expires_at"`
	CreatedAt   time.Time `bson:"created_at" json:"created_at"`
}

const (
	SurfaceHosted = "hosted"
	SurfaceWeb    = "web"
	StatusPending = "pending"
	StatusReady   = "ready"
)

// PresignResponse is returned by the presign endpoints. There are TWO presigned
// URLs with DIFFERENT lifetimes, so each carries its own expiry:
//   - upload_url: presigned PUT, lives UPLOAD_URL_TTL (e.g. 15m)
//   - public_url: presigned GET, lives DOWNLOAD_URL_TTL (e.g. 2h)
//
// expires_in is kept (= the upload_url's TTL) for backward compatibility, but new
// clients should read the explicit *_expires_in / *_expires_at fields. The _at
// fields are absolute UTC RFC3339 timestamps, handy for deciding "is my stored
// URL stale now?" without tracking when the response was received.
type PresignResponse struct {
	UploadID           string `json:"upload_id"`
	Key                string `json:"key"`
	UploadURL          string `json:"upload_url"`
	UploadURLExpiresIn int    `json:"upload_url_expires_in"`
	UploadURLExpiresAt string `json:"upload_url_expires_at"`
	PublicURL          string `json:"public_url"`
	PublicURLExpiresIn int    `json:"public_url_expires_in"`
	PublicURLExpiresAt string `json:"public_url_expires_at"`
	ExpiresIn          int    `json:"expires_in"` // deprecated: == upload_url_expires_in
}
