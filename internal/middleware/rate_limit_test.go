package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"

	"github.com/feranmi/file-salad-backend/internal/middleware"
	"github.com/feranmi/file-salad-backend/internal/redis"
	"github.com/feranmi/file-salad-backend/internal/response"
)

func limited(t *testing.T, rdb *redis.Client, limit int) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.GET("/x", middleware.RateLimit(rdb, "test", limit, time.Minute), func(c *gin.Context) {
		response.OK(c, gin.H{"ok": true}, nil)
	})
	return r
}

func hit(e *gin.Engine) int {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/x", nil)
	r.RemoteAddr = "203.0.113.7:1234"
	e.ServeHTTP(rec, r)
	return rec.Code
}

func TestRateLimitAllowsThenBlocks(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()
	rc, _ := redis.Connect(context.Background(), "redis://"+mr.Addr())
	defer func() { _ = rc.Close() }()

	e := limited(t, rc, 3)
	for i := 1; i <= 3; i++ {
		if code := hit(e); code != 200 {
			t.Fatalf("request %d should pass, got %d", i, code)
		}
	}
	// 4th over the limit → 429 with Retry-After.
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/x", nil)
	r.RemoteAddr = "203.0.113.7:1234"
	e.ServeHTTP(rec, r)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("4th should be 429, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("429 must include Retry-After")
	}
}

func TestRateLimitPerIP(t *testing.T) {
	mr, _ := miniredis.Run()
	defer mr.Close()
	rc, _ := redis.Connect(context.Background(), "redis://"+mr.Addr())
	defer func() { _ = rc.Close() }()

	e := limited(t, rc, 1)
	// IP A uses its budget.
	recA := httptest.NewRecorder()
	rA := httptest.NewRequest("GET", "/x", nil)
	rA.RemoteAddr = "198.51.100.1:9"
	e.ServeHTTP(recA, rA)
	if recA.Code != 200 {
		t.Fatalf("A first = %d", recA.Code)
	}
	recA2 := httptest.NewRecorder()
	e.ServeHTTP(recA2, rA)
	if recA2.Code != 429 {
		t.Fatalf("A second should be 429, got %d", recA2.Code)
	}
	// IP B has its own budget.
	recB := httptest.NewRecorder()
	rB := httptest.NewRequest("GET", "/x", nil)
	rB.RemoteAddr = "198.51.100.2:9"
	e.ServeHTTP(recB, rB)
	if recB.Code != 200 {
		t.Fatalf("B should have its own budget, got %d", recB.Code)
	}
}

func TestRateLimitNilRedisFailsOpen(t *testing.T) {
	e := limited(t, nil, 1) // nil Redis → no limiting
	for i := 0; i < 5; i++ {
		if code := hit(e); code != 200 {
			t.Fatalf("nil-redis should not limit, got %d", code)
		}
	}
}
