package webuploads

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestWebScope(t *testing.T) {
	got := webScope("1.2.3.4", "fp_abc")
	if got != "web:1.2.3.4:fp_abc" {
		t.Fatalf("scope = %q", got)
	}
}

func TestObjectKey(t *testing.T) {
	key := objectKey("photo.JPEG")
	if !strings.HasPrefix(key, "f_") || !strings.HasSuffix(key, ".jpeg") {
		t.Fatalf("key shape wrong: %s", key)
	}
}

// When no stats counter is wired, /web/stats returns 0 rather than erroring.
func TestStatsHandlerNilCounter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("GET", "/api/v1/web/stats", nil)

	h := &handlers{d: Deps{}} // Stats is nil
	h.stats(c)

	if rec.Code != 200 {
		t.Fatalf("code %d", rec.Code)
	}
	var env map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env["data"].(map[string]any)["uploads_total"].(float64) != 0 {
		t.Fatalf("nil-counter total should be 0: %v", env["data"])
	}
}

func TestPresignBodyValidate(t *testing.T) {
	if (presignBody{Filename: "a.png", ContentType: "image/png", Size: 1}).validate() != nil {
		t.Error("valid body rejected")
	}
	for _, b := range []presignBody{
		{},
		{Filename: "a.png", ContentType: "image/png", Size: 0},
		{ContentType: "image/png", Size: 1},
		{Filename: "a.png", Size: 1},
	} {
		if b.validate() == nil {
			t.Errorf("invalid body %+v passed", b)
		}
	}
}
