// Package share exposes shareable codes: a short, human-friendly code that maps
// to an uploaded file for 24h. The uploader creates a code from their upload_id;
// anyone who knows the code redeems it for a presigned download URL.
//
// POST /share          { upload_id }           -> { code, expires_in }
// GET  /share/:code                            -> { filename, download_url, expires_in, cached }  (rate-limited)
//
// The redeem endpoint is rate-limited per IP so a short code can't be brute-
// forced to harvest files from the shared, public bucket.
package share

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/feranmi/file-salad-backend/internal/apperror"
	"github.com/feranmi/file-salad-backend/internal/features/uploads"
	"github.com/feranmi/file-salad-backend/internal/middleware"
	"github.com/feranmi/file-salad-backend/internal/redis"
	"github.com/feranmi/file-salad-backend/internal/response"
	"github.com/feranmi/file-salad-backend/internal/sharecode"
	"github.com/feranmi/file-salad-backend/internal/storage"
)

// uploadFinder is the slice of the uploads repo this feature needs.
type uploadFinder interface {
	FindByID(ctx context.Context, id string) (*uploads.Upload, error)
}

// presigner is the slice of storage this feature needs.
type presigner interface {
	PresignDownload(ctx context.Context, key string) (url string, cached bool, err error)
	DownloadExpiry() (seconds int, at string)
}

// Deps are what the feature needs from app.
type Deps struct {
	Uploads uploadFinder
	Codes   *sharecode.Store
	Store   presigner
	Redis   *redis.Client // for the redeem rate limiter (nil disables it)
}

const (
	redeemLimit  = 10               // redeem attempts per window, per IP
	redeemWindow = 60 * time.Second // the window
)

// Register mounts the share routes. Both are public; redeem is rate-limited.
func Register(rg *gin.RouterGroup, deps Deps) {
	h := &handlers{d: deps}
	g := rg.Group("/share")
	g.POST("", h.create)
	g.GET("/:code",
		middleware.RateLimit(deps.Redis, "share_redeem", redeemLimit, redeemWindow),
		h.redeem,
	)
}

type handlers struct {
	d Deps
}

type createBody struct {
	UploadID string `json:"upload_id"`
}

func (h *handlers) create(c *gin.Context) {
	var body createBody
	_ = c.ShouldBindJSON(&body)
	if strings.TrimSpace(body.UploadID) == "" {
		_ = c.Error(apperror.Validation("Validation failed", map[string][]string{
			"upload_id": {"Required"},
		}))
		return
	}

	ctx := c.Request.Context()
	u, err := h.d.Uploads.FindByID(ctx, body.UploadID)
	if err != nil {
		if errors.Is(err, uploads.ErrNotFound) {
			_ = c.Error(apperror.NotFound("Upload"))
			return
		}
		_ = c.Error(internalErr(err))
		return
	}

	code, ttl, err := h.d.Codes.Create(ctx, sharecode.Target{Key: u.Key, Filename: u.Filename})
	if err != nil {
		_ = c.Error(internalErr(err))
		return
	}

	response.Created(c, gin.H{
		"code":       code,
		"expires_in": int(ttl.Seconds()),
	})
}

func (h *handlers) redeem(c *gin.Context) {
	code := strings.ToUpper(strings.TrimSpace(c.Param("code")))
	ctx := c.Request.Context()

	target, err := h.d.Codes.Resolve(ctx, code)
	if err != nil {
		if errors.Is(err, sharecode.ErrNotFound) {
			// Unknown or expired — same response either way (don't leak which).
			_ = c.Error(apperror.NotFound("Code"))
			return
		}
		_ = c.Error(internalErr(err))
		return
	}

	url, cached, err := h.d.Store.PresignDownload(ctx, target.Key)
	if err != nil {
		_ = c.Error(internalErr(err))
		return
	}

	secs, at := h.d.Store.DownloadExpiry()
	response.OK(c, gin.H{
		"filename":     target.Filename,
		"download_url": url,
		"expires_in":   secs,
		"expires_at":   at,
		"cached":       cached,
	}, nil)
}

func internalErr(_ error) *apperror.Error {
	return &apperror.Error{Code: "internal", Status: 500, Message: "An unexpected error occurred"}
}

// compile-time guard that the concrete storage satisfies the presigner slice.
var _ presigner = (*storage.Storage)(nil)
