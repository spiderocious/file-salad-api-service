// Package response is the single way handlers write to the wire. Every response
// goes through here so the envelope shape stays consistent — the Go counterpart
// of the Node ResponseUtil. Handlers never call c.JSON directly.
package response

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/feranmi/file-salad-backend/internal/httpx"
)

// OK writes 200 with data and optional meta.
func OK(c *gin.Context, data any, meta map[string]any) {
	env := httpx.Envelope{Data: data}
	if len(meta) > 0 {
		env.Meta = meta
	}
	c.JSON(http.StatusOK, env)
}

// Created writes 201 with data.
func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, httpx.Envelope{Data: data})
}

// Accepted writes 202 with data.
func Accepted(c *gin.Context, data any) {
	c.JSON(http.StatusAccepted, httpx.Envelope{Data: data})
}

// NoContent writes 204 with no body.
func NoContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

// Error writes the error half of the envelope at the given status.
func Error(c *gin.Context, status int, err *httpx.APIError) {
	c.JSON(status, httpx.Envelope{Error: err})
}
