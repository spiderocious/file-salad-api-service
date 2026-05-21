// Package uploads is the hosted free-tier upload feature (desktop, authenticated).
// The backend checks quota, issues a presigned PUT to the FileSalad bucket,
// and records the upload — it never touches the bytes.
package uploads

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/feranmi/file-salad-backend/internal/middleware"
	"github.com/feranmi/file-salad-backend/internal/response"
	"github.com/feranmi/file-salad-backend/internal/security"
)

const (
	defaultLimit = 20
	maxLimit     = 50
)

// uploadCounter is the cross-cutting "total presigns" metric. Fire-and-forget:
// the handler calls it after the response is written, so it never affects the
// presign path. A nil counter is tolerated (feature works without it).
type uploadCounter interface {
	IncrementAsync()
}

// Deps are what the feature needs from app.
type Deps struct {
	Service *Service
	JWT     *security.JWTSigner
	Counter uploadCounter // optional; nil disables the global upload counter
}

// Register mounts the hosted-upload routes (all authenticated).
func Register(rg *gin.RouterGroup, deps Deps) {
	h := &handlers{svc: deps.Service, counter: deps.Counter}
	g := rg.Group("/uploads", middleware.RequireAuth(deps.JWT))

	g.POST("/presign", h.presign)
	g.POST("/:id/complete", h.complete)
	g.GET("", h.list)
	g.GET("/:id/download", h.download)
}

type handlers struct {
	svc     *Service
	counter uploadCounter
}

func (h *handlers) presign(c *gin.Context) {
	var body presignBody
	_ = c.ShouldBindJSON(&body)
	if verr := body.validate(); verr != nil {
		_ = c.Error(verr)
		return
	}
	userID := c.GetString(middleware.ContextUserID)
	res, used, limit, err := h.svc.Presign(c.Request.Context(), userID, body.Filename, body.ContentType, body.Size)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Created(c, gin.H{
		"upload_id":  res.UploadID,
		"key":        res.Key,
		"upload_url": res.UploadURL,
		"public_url": res.PublicURL,
		"expires_in": res.ExpiresIn,
		"usage":      gin.H{"used": used, "limit": limit},
	})
	// Bump the global counter in the background — never blocks the response.
	if h.counter != nil {
		h.counter.IncrementAsync()
	}
}

func (h *handlers) complete(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserID)
	u, err := h.svc.Complete(c.Request.Context(), userID, c.Param("id"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OK(c, gin.H{"upload": u}, nil)
}

func (h *handlers) list(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserID)
	cursor := c.Query("cursor")
	limit := clampLimit(c.Query("limit"))

	rows, next, hasMore, err := h.svc.List(c.Request.Context(), userID, cursor, limit)
	if err != nil {
		_ = c.Error(err)
		return
	}
	meta := map[string]any{"has_more": hasMore}
	if next != "" {
		meta["next_cursor"] = next
	} else {
		meta["next_cursor"] = nil
	}
	response.OK(c, rows, meta)
}

func (h *handlers) download(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserID)
	url, cached, expiresIn, err := h.svc.Download(c.Request.Context(), userID, c.Param("id"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OK(c, gin.H{"download_url": url, "expires_in": expiresIn, "cached": cached}, nil)
}

func clampLimit(raw string) int {
	if raw == "" {
		return defaultLimit
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultLimit
	}
	if n > maxLimit {
		return maxLimit
	}
	return n
}
