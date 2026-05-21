package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/feranmi/file-salad-backend/internal/apperror"
	"github.com/feranmi/file-salad-backend/internal/httpx"
	"github.com/feranmi/file-salad-backend/internal/middleware"
	"github.com/feranmi/file-salad-backend/internal/response"
	"github.com/feranmi/file-salad-backend/internal/security"
)

func engine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(middleware.RequestID())
	r.Use(middleware.RequestLog())
	return r
}

func req(t *testing.T, e *gin.Engine, method, path string, headers map[string]string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	r := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, r)
	var out map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
	}
	return rec, out
}

func TestRequestIDGeneratedAndEchoed(t *testing.T) {
	e := engine()
	e.GET("/x", func(c *gin.Context) { response.OK(c, gin.H{}, nil) })

	rec, _ := req(t, e, "GET", "/x", nil)
	if rec.Header().Get("X-Request-Id") == "" {
		t.Fatal("no request id generated")
	}

	rec, _ = req(t, e, "GET", "/x", map[string]string{"X-Request-Id": "custom-123"})
	if rec.Header().Get("X-Request-Id") != "custom-123" {
		t.Fatalf("request id not echoed: %s", rec.Header().Get("X-Request-Id"))
	}
}

func TestErrorHandlerAppError(t *testing.T) {
	e := engine()
	e.GET("/boom", func(c *gin.Context) { _ = c.Error(apperror.Conflict("dup")) })

	rec, body := req(t, e, "GET", "/boom", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("code %d", rec.Code)
	}
	if body["error"].(map[string]any)["code"] != "conflict" {
		t.Fatalf("code %v", body["error"])
	}
}

func TestErrorHandlerRetryAfter(t *testing.T) {
	e := engine()
	e.GET("/limited", func(c *gin.Context) {
		_ = c.Error(&apperror.Error{Code: httpx.CodeRateLimited, Status: 429, Message: "slow down", RetryAfter: 30})
	})
	rec, _ := req(t, e, "GET", "/limited", nil)
	if rec.Code != 429 || rec.Header().Get("Retry-After") != "30" {
		t.Fatalf("code %d retry-after %q", rec.Code, rec.Header().Get("Retry-After"))
	}
}

func TestErrorHandlerUnknownError(t *testing.T) {
	e := engine()
	e.GET("/panic", func(c *gin.Context) { _ = c.Error(http.ErrAbortHandler) })
	rec, body := req(t, e, "GET", "/panic", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code %d", rec.Code)
	}
	if body["error"].(map[string]any)["code"] != "internal" {
		t.Fatalf("unknown error should map to internal: %v", body["error"])
	}
}

func TestRequireAuth(t *testing.T) {
	jwt := security.NewJWTSigner("0123456789abcdef0123456789abcdef", 15*time.Minute)
	e := engine()
	e.GET("/secret", middleware.RequireAuth(jwt), func(c *gin.Context) {
		response.OK(c, gin.H{"uid": c.GetString(middleware.ContextUserID)}, nil)
	})

	// no token → 401 unauthorized
	rec, body := req(t, e, "GET", "/secret", nil)
	if rec.Code != 401 || body["error"].(map[string]any)["code"] != "unauthorized" {
		t.Fatalf("no token: %d %v", rec.Code, body["error"])
	}

	// garbage token → 401 token_invalid
	rec, body = req(t, e, "GET", "/secret", map[string]string{"Authorization": "Bearer garbage"})
	if rec.Code != 401 || body["error"].(map[string]any)["code"] != "token_invalid" {
		t.Fatalf("garbage: %d %v", rec.Code, body["error"])
	}

	// wrong scheme → 401 unauthorized
	rec, _ = req(t, e, "GET", "/secret", map[string]string{"Authorization": "Token abc"})
	if rec.Code != 401 {
		t.Fatalf("wrong scheme code %d", rec.Code)
	}

	// valid token → 200, uid on context
	tok, _ := jwt.Sign("u_42")
	rec, body = req(t, e, "GET", "/secret", map[string]string{"Authorization": "Bearer " + tok})
	if rec.Code != 200 {
		t.Fatalf("valid token code %d", rec.Code)
	}
	if body["data"].(map[string]any)["uid"] != "u_42" {
		t.Fatalf("uid not on context: %v", body["data"])
	}
}

func TestRequireAuthExpired(t *testing.T) {
	jwt := security.NewJWTSigner("0123456789abcdef0123456789abcdef", -time.Minute)
	e := engine()
	e.GET("/secret", middleware.RequireAuth(jwt), func(c *gin.Context) { response.OK(c, gin.H{}, nil) })
	tok, _ := jwt.Sign("u_1")
	rec, body := req(t, e, "GET", "/secret", map[string]string{"Authorization": "Bearer " + tok})
	if rec.Code != 401 || body["error"].(map[string]any)["code"] != "token_expired" {
		t.Fatalf("expired: %d %v", rec.Code, body["error"])
	}
}
