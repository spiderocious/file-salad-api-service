package response

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/feranmi/file-salad-backend/internal/httpx"
)

func ctx() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	return c, rec
}

func TestOK(t *testing.T) {
	c, rec := ctx()
	OK(c, map[string]int{"n": 1}, nil)
	if rec.Code != 200 {
		t.Fatalf("code %d", rec.Code)
	}
	var env map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if _, ok := env["data"]; !ok {
		t.Fatal("missing data")
	}
	if _, ok := env["meta"]; ok {
		t.Fatal("meta should be absent when nil")
	}
}

func TestOKWithMeta(t *testing.T) {
	c, rec := ctx()
	OK(c, []int{}, map[string]any{"has_more": false})
	var env map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if _, ok := env["meta"]; !ok {
		t.Fatal("meta should be present")
	}
}

func TestCreatedAcceptedNoContent(t *testing.T) {
	c, rec := ctx()
	Created(c, gin.H{"x": 1})
	if rec.Code != 201 {
		t.Fatalf("created code %d", rec.Code)
	}

	c, rec = ctx()
	Accepted(c, gin.H{"x": 1})
	if rec.Code != 202 {
		t.Fatalf("accepted code %d", rec.Code)
	}

	c, rec = ctx()
	NoContent(c)
	// c.Status sets the status on gin's writer; assert there (the bare recorder
	// stays at its 200 default until a body is written).
	if c.Writer.Status() != 204 {
		t.Fatalf("nocontent status %d", c.Writer.Status())
	}
	if rec.Body.Len() != 0 {
		t.Fatal("204 must have empty body")
	}
}

func TestError(t *testing.T) {
	c, rec := ctx()
	Error(c, 409, &httpx.APIError{Code: httpx.CodeConflict, Message: "dup"})
	if rec.Code != 409 {
		t.Fatalf("code %d", rec.Code)
	}
	var env map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	e := env["error"].(map[string]any)
	if e["code"] != "conflict" {
		t.Fatalf("code %v", e["code"])
	}
}
