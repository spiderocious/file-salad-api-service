package middleware

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/feranmi/file-salad-backend/internal/apperror"
	"github.com/feranmi/file-salad-backend/internal/httpx"
	"github.com/feranmi/file-salad-backend/internal/logger"
	"github.com/feranmi/file-salad-backend/internal/response"
)

// ErrorHandler is the central error translator. Handlers attach failures with
// c.Error(err) and return; this middleware (running after them) inspects the
// collected errors and writes the envelope. An *apperror.Error maps to its
// code/status; anything else is a 500 with the internal code and no leaked
// detail. Mirrors the Node errorHandler middleware.
//
// Registered first so its deferred body runs last, after the handler chain.
func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if len(c.Errors) == 0 {
			return
		}
		// If a handler already wrote a body, don't double-write.
		if c.Writer.Written() {
			return
		}

		err := c.Errors.Last().Err
		ctx := c.Request.Context()

		var appErr *apperror.Error
		if errors.As(err, &appErr) {
			if appErr.Status >= 500 {
				logger.Error(ctx, appErr.Message, "err", appErr.Error())
			}
			if appErr.RetryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(appErr.RetryAfter))
			}
			apiErr := &httpx.APIError{Code: appErr.Code, Message: appErr.Message}
			if len(appErr.FieldErrors) > 0 {
				apiErr.FieldErrors = appErr.FieldErrors
			}
			response.Error(c, appErr.Status, apiErr)
			return
		}

		// Unknown error — never leak internals.
		logger.Error(ctx, "Unhandled error", "err", err.Error())
		response.Error(c, http.StatusInternalServerError, &httpx.APIError{
			Code:    httpx.CodeInternal,
			Message: "An unexpected error occurred",
		})
	}
}
