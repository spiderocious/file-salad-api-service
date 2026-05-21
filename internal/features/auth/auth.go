// Package auth is the desktop auth feature: email+password register/login,
// JWT access tokens, Redis-backed refresh sessions with rotation, and /me.
// The web one-pager has no auth — this is desktop-only.
package auth

import (
	"github.com/gin-gonic/gin"

	"github.com/feranmi/file-salad-backend/internal/middleware"
	"github.com/feranmi/file-salad-backend/internal/response"
	"github.com/feranmi/file-salad-backend/internal/security"
)

// Deps are what the feature needs wired in from app.
type Deps struct {
	Service *Service
	JWT     *security.JWTSigner
}

// Register mounts the auth routes under the given group (expected: /api/v1).
func Register(rg *gin.RouterGroup, deps Deps) {
	h := &handlers{svc: deps.Service}

	g := rg.Group("/auth")
	g.POST("/register", h.register)
	g.POST("/login", h.login)
	g.POST("/refresh", h.refresh)
	g.POST("/logout", h.logout)

	// /me is authenticated — it also proves the RequireAuth middleware end to end.
	rg.GET("/me", middleware.RequireAuth(deps.JWT), h.me)
}

type handlers struct {
	svc *Service
}

func (h *handlers) register(c *gin.Context) {
	var body registerBody
	_ = c.ShouldBindJSON(&body)
	if verr := body.validate(); verr != nil {
		_ = c.Error(verr)
		return
	}
	res, err := h.svc.Register(c.Request.Context(), body.Email, body.Password)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Created(c, res)
}

func (h *handlers) login(c *gin.Context) {
	var body loginBody
	_ = c.ShouldBindJSON(&body)
	if verr := body.validate(); verr != nil {
		_ = c.Error(verr)
		return
	}
	res, err := h.svc.Login(c.Request.Context(), body.Email, body.Password)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OK(c, res, nil)
}

func (h *handlers) refresh(c *gin.Context) {
	var body refreshBody
	_ = c.ShouldBindJSON(&body)
	if verr := body.validate(); verr != nil {
		_ = c.Error(verr)
		return
	}
	res, err := h.svc.Refresh(c.Request.Context(), body.RefreshToken)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OK(c, res, nil)
}

func (h *handlers) logout(c *gin.Context) {
	var body logoutBody
	_ = c.ShouldBindJSON(&body)
	if verr := body.validate(); verr != nil {
		_ = c.Error(verr)
		return
	}
	if err := h.svc.Logout(c.Request.Context(), body.RefreshToken); err != nil {
		_ = c.Error(err)
		return
	}
	response.NoContent(c)
}

func (h *handlers) me(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserID)
	user, err := h.svc.Me(c.Request.Context(), userID)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OK(c, gin.H{"user": user}, nil)
}
