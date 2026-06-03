package features

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func setup(d Deps) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	rg := r.Group("/api/v1")
	Register(rg, d)
	return r
}

func TestGetFeaturesShape(t *testing.T) {
	e := setup(Deps{ShouldShowCodes: true, ShouldSupportBYOK: false, TTL: time.Hour})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/features", nil)
	e.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("code = %d", rec.Code)
	}
	var env map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	d := env["data"].(map[string]any)
	if d["should_show_codes"] != true {
		t.Fatalf("should_show_codes = %v", d["should_show_codes"])
	}
	if d["should_support_byok"] != false {
		t.Fatalf("should_support_byok = %v", d["should_support_byok"])
	}
	at, ok := d["expires_at"].(string)
	if !ok || at == "" {
		t.Fatalf("expires_at missing/wrong type: %v", d["expires_at"])
	}
	// Parse + bounds-check: expires_at must be roughly now + TTL.
	parsed, err := time.Parse(time.RFC3339, at)
	if err != nil {
		t.Fatalf("expires_at not RFC3339: %v", err)
	}
	delta := time.Until(parsed)
	if delta < 59*time.Minute || delta > 61*time.Minute {
		t.Fatalf("expires_at = %v (delta %v), want ~1h from now", parsed, delta)
	}
}

func TestGetFeaturesBothOff(t *testing.T) {
	e := setup(Deps{ShouldShowCodes: false, ShouldSupportBYOK: false, TTL: time.Minute})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/features", nil))
	if rec.Code != 200 {
		t.Fatalf("code = %d", rec.Code)
	}
	var env map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	d := env["data"].(map[string]any)
	if d["should_show_codes"] != false || d["should_support_byok"] != false {
		t.Fatalf("flags should both be false: %v", d)
	}
}

func TestGetFeaturesBothOn(t *testing.T) {
	e := setup(Deps{ShouldShowCodes: true, ShouldSupportBYOK: true, TTL: 30 * time.Second})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/features", nil))
	var env map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	d := env["data"].(map[string]any)
	if d["should_show_codes"] != true || d["should_support_byok"] != true {
		t.Fatalf("flags should both be true: %v", d)
	}
}
