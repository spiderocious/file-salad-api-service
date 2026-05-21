package middleware

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/feranmi/file-salad-backend/internal/apperror"
	"github.com/feranmi/file-salad-backend/internal/logger"
	"github.com/feranmi/file-salad-backend/internal/security"
)

// ContextUserID is the gin key under which RequireAuth stores the authenticated
// user id. Handlers read it with c.GetString(ContextUserID).
const ContextUserID = "userId"

// RequireAuth verifies the Bearer access token. On success it puts the user id
// on the gin context and on the request context (so logs carry it). On failure
// it aborts with the right token error — never calls the handler.
func RequireAuth(jwt *security.JWTSigner) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := c.GetHeader("Authorization")
		token, ok := strings.CutPrefix(raw, "Bearer ")
		if !ok || strings.TrimSpace(token) == "" {
			_ = c.Error(apperror.Unauthorized("Missing bearer token"))
			c.Abort()
			return
		}

		userID, err := jwt.Verify(token)
		if err != nil {
			if errors.Is(err, security.ErrTokenExpired) {
				_ = c.Error(apperror.TokenExpired())
			} else {
				_ = c.Error(apperror.TokenInvalid())
			}
			c.Abort()
			return
		}

		c.Set(ContextUserID, userID)
		// Fold the user id into the request-scoped log fields (keeping the
		// existing requestId).
		fields := logger.Fields{UserID: userID}
		if rid := c.GetString("requestId"); rid != "" {
			fields.RequestID = rid
		}
		c.Request = c.Request.WithContext(logger.WithFields(c.Request.Context(), fields))

		c.Next()
	}
}
