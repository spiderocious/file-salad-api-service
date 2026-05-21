package app_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/feranmi/file-salad-backend/internal/app"
	"github.com/feranmi/file-salad-backend/internal/env"
)

// Build with no feature deps still serves health, applies middleware, and 404s.
func buildBare() http.Handler {
	cfg := &env.Env{NodeEnv: "test", WebBaseURL: "*"}
	return app.Build(cfg, app.Deps{})
}

func TestHealthAndSecurityHeaders(t *testing.T) {
	h := buildBare()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/health", nil))

	if rec.Code != 200 {
		t.Fatalf("health code %d", rec.Code)
	}
	for _, hdr := range []string{"X-Content-Type-Options", "X-Frame-Options", "Strict-Transport-Security", "Referrer-Policy"} {
		if rec.Header().Get(hdr) == "" {
			t.Errorf("missing security header %s", hdr)
		}
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Error("missing X-Request-Id")
	}
}

func TestNoRoute(t *testing.T) {
	h := buildBare()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/does-not-exist", nil))
	if rec.Code != 404 {
		t.Fatalf("code %d", rec.Code)
	}
}

func TestCORSWildcardReflectsOrigin(t *testing.T) {
	h := buildBare()
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/health", nil)
	r.Header.Set("Origin", "https://example.com")
	h.ServeHTTP(rec, r)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("ACAO = %q, want reflected origin", got)
	}
}

func TestCORSPreflight(t *testing.T) {
	h := buildBare()
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("OPTIONS", "/api/v1/health", nil)
	r.Header.Set("Origin", "https://example.com")
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight code %d, want 204", rec.Code)
	}
}

func TestCORSExplicitOrigin(t *testing.T) {
	cfg := &env.Env{NodeEnv: "test", WebBaseURL: "https://app.filesalad.com"}
	h := app.Build(cfg, app.Deps{})

	// matching origin allowed
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/health", nil)
	r.Header.Set("Origin", "https://app.filesalad.com")
	h.ServeHTTP(rec, r)
	if rec.Header().Get("Access-Control-Allow-Origin") != "https://app.filesalad.com" {
		t.Error("matching origin should be allowed")
	}

	// non-matching origin → no ACAO header
	rec = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/api/v1/health", nil)
	r.Header.Set("Origin", "https://evil.com")
	h.ServeHTTP(rec, r)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("non-matching origin should not be allowed")
	}
}
