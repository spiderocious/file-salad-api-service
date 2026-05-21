// Package health is the liveness feature. Mirrors features/health in Node:
// a single GET that reports service, env, and time.
package health

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/feranmi/file-salad-backend/internal/response"
)

// Register mounts the health routes under the given group (expected: /api/v1).
func Register(rg *gin.RouterGroup, nodeEnv string) {
	g := rg.Group("/health")
	g.GET("", func(c *gin.Context) {
		response.OK(c, gin.H{
			"status":  "ok",
			"service": "file-salad-backend",
			"env":     nodeEnv,
			"time":    time.Now().UTC().Format(time.RFC3339),
		}, nil)
	})
}
