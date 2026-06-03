// Package features exposes a tiny public endpoint the frontend hits to learn
// which UI features should be on. Values come straight from env — no DB, no
// Redis, no auth. The response includes an absolute expires_at so the FE knows
// how long to cache it (computed as now + FEATURE_FLAG_TTL).
//
//	GET /api/v1/features  ->  { should_show_codes, should_support_byok, expires_at }
package features

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/feranmi/file-salad-backend/internal/response"
)

// Deps are what the feature needs from app — just the env-derived values.
type Deps struct {
	ShouldShowCodes   bool
	ShouldSupportBYOK bool
	TTL               time.Duration // cache hint window
}

// Register mounts the route. Always-on (no infra gates).
func Register(rg *gin.RouterGroup, deps Deps) {
	h := &handlers{d: deps}
	rg.GET("/features", h.get)
}

type handlers struct {
	d Deps
}

func (h *handlers) get(c *gin.Context) {
	response.OK(c, gin.H{
		"should_show_codes":   h.d.ShouldShowCodes,
		"should_support_byok": h.d.ShouldSupportBYOK,
		"expires_at":          time.Now().UTC().Add(h.d.TTL).Format(time.RFC3339),
	}, nil)
}
