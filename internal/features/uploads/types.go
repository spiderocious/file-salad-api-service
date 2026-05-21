package uploads

import "time"

// Upload is a persisted upload record. Surface is "hosted" (this feature) or
// "web" (the anonymous one-pager). OwnerID is empty for web.
type Upload struct {
	ID          string    `bson:"_id" json:"id"`
	OwnerID     string    `bson:"owner_id,omitempty" json:"owner_id,omitempty"`
	Surface     string    `bson:"surface" json:"surface"`
	Key         string    `bson:"key" json:"key"`
	Filename    string    `bson:"filename" json:"filename"`
	ContentType string    `bson:"content_type" json:"content_type"`
	Size        int64     `bson:"size" json:"size"`
	Status      string    `bson:"status" json:"status"` // pending | ready
	PublicURL   string    `bson:"public_url" json:"public_url"`
	ExpiresAt   time.Time `bson:"expires_at" json:"expires_at"`
	CreatedAt   time.Time `bson:"created_at" json:"created_at"`
}

const (
	SurfaceHosted = "hosted"
	SurfaceWeb    = "web"
	StatusPending = "pending"
	StatusReady   = "ready"
)

// PresignResponse is returned by the presign endpoints.
type PresignResponse struct {
	UploadID  string `json:"upload_id"`
	Key       string `json:"key"`
	UploadURL string `json:"upload_url"`
	PublicURL string `json:"public_url"`
	ExpiresIn int    `json:"expires_in"`
}
