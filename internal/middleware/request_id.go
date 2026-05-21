package middleware

import (
	"github.com/gin-gonic/gin"

	"github.com/feranmi/file-salad-backend/internal/ids"
	"github.com/feranmi/file-salad-backend/internal/logger"
)

const requestIDHeader = "X-Request-Id"

// RequestID seeds the request context with a request id (honouring an inbound
// X-Request-Id) and echoes it back on the response. Downstream middleware,
// handlers, and the logger read it from the context — the Go-idiomatic stand-in
// for the Node backend's AsyncLocalStorage.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader(requestIDHeader)
		if rid == "" {
			rid = ids.NewRaw()
		}
		c.Header(requestIDHeader, rid)

		// Stash on gin for cheap access and on the request context for the logger.
		c.Set("requestId", rid)
		ctx := logger.WithFields(c.Request.Context(), logger.Fields{RequestID: rid})
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}
