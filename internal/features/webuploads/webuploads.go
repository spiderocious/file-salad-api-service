// Package webuploads is the anonymous web one-pager upload feature: no account,
// no BYO. Presigned PUT into the shared FileSalad bucket, with a per-browser
// monthly cap enforced server-side keyed by IP + a client-sent fingerprint.
// The cap is best-effort (defeatable by VPN/profile rotation) and accepted as
// such for v1.
package webuploads

import (
	"context"
	"fmt"
	"net"
	"path"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/feranmi/file-salad-backend/internal/apperror"
	"github.com/feranmi/file-salad-backend/internal/features/uploads"
	"github.com/feranmi/file-salad-backend/internal/ids"
	"github.com/feranmi/file-salad-backend/internal/quota"
	"github.com/feranmi/file-salad-backend/internal/response"
	"github.com/feranmi/file-salad-backend/internal/storage"
)

const fingerprintHeader = "X-Fingerprint"

// statsCounter is the cross-cutting "total uploads" metric: an async increment
// (fire-and-forget, never blocks the presign) plus a read for the public
// /web/stats endpoint. Optional — a nil counter disables both.
type statsCounter interface {
	IncrementAsync()
	Total(ctx context.Context) (int64, error)
}

// Deps are what the feature needs from app.
type Deps struct {
	Repo        *uploads.Repo
	Store       *storage.Storage
	Quota       *quota.Counter
	Stats       statsCounter // optional; powers the global counter + /web/stats
	MaxFileSize int64
	LinkDays    int
}

// Register mounts the web upload routes (no auth).
func Register(rg *gin.RouterGroup, deps Deps) {
	h := &handlers{d: deps}
	g := rg.Group("/web")
	g.POST("/uploads/presign", h.presign)
	g.GET("/uploads/:id/download", h.download)
	g.GET("/usage", h.usage)
	g.GET("/stats", h.stats)
}

type handlers struct {
	d Deps
}

// scope keys the cap by IP + fingerprint (best-effort, per D-7).
func webScope(ip, fingerprint string) string {
	return "web:" + ip + ":" + fingerprint
}

func (h *handlers) fingerprint(c *gin.Context) (string, *apperror.Error) {
	fp := strings.TrimSpace(c.GetHeader(fingerprintHeader))
	if fp == "" {
		return "", apperror.Validation("Validation failed", map[string][]string{
			"fingerprint": {"X-Fingerprint header is required"},
		})
	}
	return fp, nil
}

// clientIP returns the request IP without the port (Gin's ClientIP honours
// trust-proxy config set in app).
func clientIP(c *gin.Context) string {
	ip := c.ClientIP()
	if host, _, err := net.SplitHostPort(ip); err == nil {
		return host
	}
	return ip
}

func (h *handlers) presign(c *gin.Context) {
	var body presignBody
	_ = c.ShouldBindJSON(&body)
	if verr := body.validate(); verr != nil {
		_ = c.Error(verr)
		return
	}
	fp, ferr := h.fingerprint(c)
	if ferr != nil {
		_ = c.Error(ferr)
		return
	}

	if body.Size > h.d.MaxFileSize {
		_ = c.Error(apperror.FileTooLarge(fmt.Sprintf("File exceeds the %d byte limit", h.d.MaxFileSize)))
		return
	}

	ctx := c.Request.Context()
	scope := webScope(clientIP(c), fp)

	used, ok, err := h.d.Quota.Reserve(ctx, scope)
	if err != nil {
		_ = c.Error(internalErr(err))
		return
	}
	if !ok {
		// No BYO upsell on web — there's no account.
		c.Header("X-RateLimit-Remaining", "0")
		_ = c.Error(apperror.QuotaExceeded("Monthly upload limit reached for this browser"))
		return
	}

	key := objectKey(body.Filename)
	uploadURL, err := h.d.Store.PresignUpload(ctx, key, body.ContentType)
	if err != nil {
		_ = h.d.Quota.Release(ctx, scope)
		_ = c.Error(internalErr(err))
		return
	}

	// public_url is a presigned GET — always resolves (even on a private bucket),
	// expires with DOWNLOAD_URL_TTL. Durable identifier is the key. Non-caching
	// presign so it doesn't pre-warm the download cache.
	publicURL, perr := h.d.Store.PresignDownloadURL(ctx, key)
	if perr != nil {
		_ = h.d.Quota.Release(ctx, scope)
		_ = c.Error(internalErr(perr))
		return
	}

	u := &uploads.Upload{
		ID:          ids.Prefixed("up"),
		Surface:     uploads.SurfaceWeb,
		Key:         key,
		Filename:    body.Filename,
		ContentType: body.ContentType,
		Size:        body.Size,
		Status:      uploads.StatusPending,
		ExpiresAt:   nowExpiry(h.d.LinkDays),
		CreatedAt:   nowUTC(),
	}
	if err := h.d.Repo.Insert(ctx, u); err != nil {
		_ = h.d.Quota.Release(ctx, scope)
		_ = c.Error(internalErr(err))
		return
	}

	remaining := h.d.Quota.Cap() - used
	if remaining < 0 {
		remaining = 0
	}
	response.Created(c, gin.H{
		"upload_id":  u.ID,
		"key":        u.Key,
		"upload_url": uploadURL,
		"public_url": publicURL,
		"expires_in": h.d.Store.UploadTTLSeconds(),
		"remaining":  remaining,
	})
	// Bump the global counter in the background — never blocks the response.
	if h.d.Stats != nil {
		h.d.Stats.IncrementAsync()
	}
}

// stats is the public "files shared so far" counter for the web one-pager. No
// auth, no fingerprint — just the running total.
func (h *handlers) stats(c *gin.Context) {
	if h.d.Stats == nil {
		response.OK(c, gin.H{"uploads_total": 0}, nil)
		return
	}
	total, err := h.d.Stats.Total(c.Request.Context())
	if err != nil {
		_ = c.Error(internalErr(err))
		return
	}
	response.OK(c, gin.H{"uploads_total": total}, nil)
}

func (h *handlers) download(c *gin.Context) {
	ctx := c.Request.Context()
	u, err := h.d.Repo.FindByIDWeb(ctx, c.Param("id"))
	if err != nil {
		if err == uploads.ErrNotFound {
			_ = c.Error(apperror.NotFound("Upload"))
			return
		}
		_ = c.Error(internalErr(err))
		return
	}
	url, cached, perr := h.d.Store.PresignDownload(ctx, u.Key)
	if perr != nil {
		_ = c.Error(internalErr(perr))
		return
	}
	response.OK(c, gin.H{
		"download_url": url,
		"expires_in":   h.d.Store.DownloadTTLSeconds(),
		"cached":       cached,
	}, nil)
}

func (h *handlers) usage(c *gin.Context) {
	fp, ferr := h.fingerprint(c)
	if ferr != nil {
		_ = c.Error(ferr)
		return
	}
	scope := webScope(clientIP(c), fp)
	used, err := h.d.Quota.Used(c.Request.Context(), scope)
	if err != nil {
		_ = c.Error(internalErr(err))
		return
	}
	remaining := h.d.Quota.Cap() - used
	if remaining < 0 {
		remaining = 0
	}
	response.OK(c, gin.H{"used": used, "limit": h.d.Quota.Cap(), "remaining": remaining}, nil)
}

func internalErr(_ error) *apperror.Error {
	return &apperror.Error{Code: "internal", Status: 500, Message: "An unexpected error occurred"}
}

func objectKey(filename string) string {
	ext := strings.ToLower(path.Ext(filename))
	return ids.Prefixed("f") + ext
}

func nowExpiry(days int) time.Time { return time.Now().UTC().AddDate(0, 0, days) }
func nowUTC() time.Time            { return time.Now().UTC() }
