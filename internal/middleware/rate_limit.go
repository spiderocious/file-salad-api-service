package middleware

import (
	"net"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/feranmi/file-salad-backend/internal/apperror"
	"github.com/feranmi/file-salad-backend/internal/httpx"
	"github.com/feranmi/file-salad-backend/internal/redis"
)

// RateLimit is a per-IP fixed-window limiter backed by Redis. It allows `limit`
// requests per `window` per client IP; over that, it aborts with 429 and a
// Retry-After header. Used to make brute-forcing short share codes impractical.
//
// `name` namespaces the counter so different routes have independent budgets.
// A nil Redis client disables limiting (fail-open) — the feature still works,
// it's just unthrottled (dev without Redis).
func RateLimit(rdb *redis.Client, name string, limit int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if rdb == nil {
			c.Next()
			return
		}

		ip := clientIP(c)
		// Bucket by the current window so the key rolls over and self-expires.
		bucket := time.Now().UTC().Truncate(window).Unix()
		k := "ratelimit:" + name + ":" + ip + ":" + strconv.FormatInt(bucket, 10)

		count, err := rdb.Incr(c.Request.Context(), k).Result()
		if err != nil {
			// Redis hiccup → fail open rather than block legitimate traffic.
			c.Next()
			return
		}
		if count == 1 {
			// First hit in this window — set the expiry. (A little slack beyond
			// the window so the key is sure to outlive its bucket.)
			_ = rdb.Expire(c.Request.Context(), k, window+time.Second).Err()
		}

		if count > int64(limit) {
			retry := int(window.Seconds())
			if retry < 1 {
				retry = 1
			}
			c.Header("Retry-After", strconv.Itoa(retry))
			_ = c.Error(&apperror.Error{
				Code:       httpx.CodeRateLimited,
				Status:     429,
				Message:    "Too many requests — slow down",
				RetryAfter: retry,
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// clientIP returns the request IP without the port. Honours the trust-proxy
// config set in app (X-Forwarded-For etc.).
func clientIP(c *gin.Context) string {
	ip := c.ClientIP()
	if host, _, err := net.SplitHostPort(ip); err == nil {
		return host
	}
	return ip
}
