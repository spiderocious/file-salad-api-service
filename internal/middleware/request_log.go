package middleware

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/feranmi/file-salad-backend/internal/logger"
)

// RequestLog logs one line per request after it completes, with method, path,
// status, and duration — at info/warn/error depending on the status class.
// Mirrors the Node requestLog middleware.
func RequestLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		durationMs := time.Since(start).Milliseconds()
		ctx := c.Request.Context()

		attrs := []any{
			"method", c.Request.Method,
			"path", c.Request.URL.RequestURI(),
			"statusCode", status,
			"durationMs", durationMs,
			"ip", c.ClientIP(),
			"userAgent", c.Request.UserAgent(),
		}
		msg := c.Request.Method + " " + c.Request.URL.RequestURI()

		switch {
		case status >= 500:
			logger.Error(ctx, msg, attrs...)
		case status >= 400:
			logger.Warn(ctx, msg, attrs...)
		default:
			logger.Info(ctx, msg, attrs...)
		}
	}
}
