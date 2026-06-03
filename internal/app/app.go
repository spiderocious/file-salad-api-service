// Package app composes the Gin engine: middleware stack, feature registration,
// 404, and the central error handler. main wires concrete dependencies (Mongo,
// Redis, storage) into a Deps and calls Build; Build only knows how to assemble them.
package app

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/feranmi/file-salad-backend/internal/env"
	"github.com/feranmi/file-salad-backend/internal/features/auth"
	"github.com/feranmi/file-salad-backend/internal/features/features"
	"github.com/feranmi/file-salad-backend/internal/features/health"
	"github.com/feranmi/file-salad-backend/internal/features/share"
	"github.com/feranmi/file-salad-backend/internal/features/uploads"
	webuploads "github.com/feranmi/file-salad-backend/internal/features/webuploads"
	"github.com/feranmi/file-salad-backend/internal/httpx"
	"github.com/feranmi/file-salad-backend/internal/middleware"
	"github.com/feranmi/file-salad-backend/internal/response"
)

// Deps are the wired-up feature dependencies. A nil feature dep means that
// feature is not mounted (e.g. uploads when storage isn't configured).
type Deps struct {
	Auth       auth.Deps
	Uploads    *uploads.Deps    // nil when storage isn't configured
	WebUploads *webuploads.Deps // nil when storage isn't configured
	Share      *share.Deps      // nil when storage isn't configured
}

// Build returns the configured Gin engine.
func Build(cfg *env.Env, deps Deps) *gin.Engine {
	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	// New() (not Default()) so we own the full middleware stack — no Gin logger.
	r := gin.New()
	r.Use(gin.Recovery())

	// Order matters. ErrorHandler runs first so its post-Next body executes last,
	// translating any c.Error() set deeper in the chain.
	r.Use(middleware.ErrorHandler())
	r.Use(middleware.RequestID())
	r.Use(middleware.RequestLog())
	r.Use(securityHeaders())
	r.Use(corsMiddleware(cfg.WebBaseURL))

	api := r.Group("/api/v1")
	health.Register(api, cfg.NodeEnv)
	features.Register(api, features.Deps{
		ShouldShowCodes:   cfg.FeatureShouldShowCodes,
		ShouldSupportBYOK: cfg.FeatureShouldSupportBYOK,
		TTL:               cfg.FeatureFlagTTL,
	})
	auth.Register(api, deps.Auth)
	if deps.Uploads != nil {
		uploads.Register(api, *deps.Uploads)
	}
	if deps.WebUploads != nil {
		webuploads.Register(api, *deps.WebUploads)
	}
	if deps.Share != nil {
		share.Register(api, *deps.Share)
	}

	r.NoRoute(func(c *gin.Context) {
		response.Error(c, http.StatusNotFound, &httpx.APIError{
			Code:    httpx.CodeNotFound,
			Message: "Route not found",
		})
	})

	return r
}

// corsMiddleware mirrors the Node CORS setup: "*" reflects the request origin
// (any origin), otherwise only the explicit WEB_BASE_URL is allowed. Kept as a
// small inline handler to avoid a third-party CORS dependency for so few rules.
func corsMiddleware(webBaseURL string) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		allow := ""
		switch {
		case webBaseURL == "*" && origin != "":
			// Reflect the origin so credentialed requests work (a literal "*"
			// is disallowed alongside Allow-Credentials).
			allow = origin
		case webBaseURL != "*" && origin == webBaseURL:
			allow = webBaseURL
		}

		if allow != "" {
			h := c.Writer.Header()
			h.Set("Access-Control-Allow-Origin", allow)
			h.Set("Vary", "Origin")
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization, X-Request-Id, X-Fingerprint")
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// securityHeaders sets a baseline of hardening headers — the helmet() analogue.
func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("X-DNS-Prefetch-Control", "off")
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		c.Next()
	}
}
