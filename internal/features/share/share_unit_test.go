package share

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"

	"github.com/feranmi/file-salad-backend/internal/apperror"
	"github.com/feranmi/file-salad-backend/internal/features/uploads"
	"github.com/feranmi/file-salad-backend/internal/redis"
	"github.com/feranmi/file-salad-backend/internal/sharecode"
)

func TestInternalErr(t *testing.T) {
	e := internalErr(errors.New("boom"))
	if e.Code != "internal" || e.Status != 500 {
		t.Fatalf("internalErr = %+v", e)
	}
}

type fakeFinder struct {
	u   *uploads.Upload
	err error
}

func (f *fakeFinder) FindByID(_ context.Context, _ string) (*uploads.Upload, error) {
	return f.u, f.err
}

type fakePresigner struct {
	url string
	err error
}

func (f *fakePresigner) PresignDownload(_ context.Context, _ string) (string, bool, error) {
	return f.url, false, f.err
}
func (f *fakePresigner) DownloadExpiry() (int, string) { return 3600, "2026-05-22T12:00:00Z" }

func newStore(t *testing.T) *sharecode.Store {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	rc, err := redis.Connect(context.Background(), "redis://"+mr.Addr())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rc.Close() })
	return sharecode.NewStore(rc, time.Hour)
}

func ctxJSON(method, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(method, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, rec
}

// errCode returns the apperror code a handler attached via c.Error, or "".
func errCode(c *gin.Context) string {
	if len(c.Errors) == 0 {
		return ""
	}
	var ae *apperror.Error
	if errors.As(c.Errors[0].Err, &ae) {
		return ae.Code
	}
	return ""
}

func TestCreateUploadLookupError(t *testing.T) {
	h := &handlers{d: Deps{Uploads: &fakeFinder{err: errors.New("db down")}, Codes: newStore(t), Store: &fakePresigner{}}}
	c, _ := ctxJSON("POST", "/share", `{"upload_id":"up_1"}`)
	h.create(c)
	if errCode(c) != "internal" {
		t.Fatalf("expected internal, got %q", errCode(c))
	}
}

func TestCreateUploadNotFound(t *testing.T) {
	h := &handlers{d: Deps{Uploads: &fakeFinder{err: uploads.ErrNotFound}, Codes: newStore(t), Store: &fakePresigner{}}}
	c, _ := ctxJSON("POST", "/share", `{"upload_id":"up_x"}`)
	h.create(c)
	if errCode(c) != "not_found" {
		t.Fatalf("expected not_found, got %q", errCode(c))
	}
}

func TestCreateMissingID(t *testing.T) {
	h := &handlers{d: Deps{Uploads: &fakeFinder{}, Codes: newStore(t), Store: &fakePresigner{}}}
	c, _ := ctxJSON("POST", "/share", `{}`)
	h.create(c)
	if errCode(c) != "validation_error" {
		t.Fatalf("expected validation_error, got %q", errCode(c))
	}
}

func TestCreateSuccess(t *testing.T) {
	h := &handlers{d: Deps{
		Uploads: &fakeFinder{u: &uploads.Upload{Key: "f_a.png", Filename: "a.png"}},
		Codes:   newStore(t), Store: &fakePresigner{},
	}}
	c, rec := ctxJSON("POST", "/share", `{"upload_id":"up_1"}`)
	h.create(c)
	if rec.Code != 201 {
		t.Fatalf("create code %d (errs=%v)", rec.Code, c.Errors)
	}
	var env map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env["data"].(map[string]any)["code"] == nil {
		t.Fatal("missing code in response")
	}
}

func TestRedeemPresignError(t *testing.T) {
	codes := newStore(t)
	code, _, _ := codes.Create(context.Background(), sharecode.Target{Key: "f_a", Filename: "a"})
	h := &handlers{d: Deps{Codes: codes, Store: &fakePresigner{err: errors.New("s3 down")}}}
	c, _ := ctxJSON("GET", "/share/"+code, "")
	c.Params = gin.Params{{Key: "code", Value: code}}
	h.redeem(c)
	if errCode(c) != "internal" {
		t.Fatalf("expected internal, got %q", errCode(c))
	}
}

func TestRedeemUnknownCode(t *testing.T) {
	h := &handlers{d: Deps{Codes: newStore(t), Store: &fakePresigner{}}}
	c, _ := ctxJSON("GET", "/share/ZZZZZZZ", "")
	c.Params = gin.Params{{Key: "code", Value: "ZZZZZZZ"}}
	h.redeem(c)
	if errCode(c) != "not_found" {
		t.Fatalf("expected not_found, got %q", errCode(c))
	}
}

func TestRedeemSuccess(t *testing.T) {
	codes := newStore(t)
	code, _, _ := codes.Create(context.Background(), sharecode.Target{Key: "f_a", Filename: "report.pdf"})
	h := &handlers{d: Deps{Codes: codes, Store: &fakePresigner{url: "https://x?X-Amz-Signature=z"}}}
	c, rec := ctxJSON("GET", "/share/"+code, "")
	c.Params = gin.Params{{Key: "code", Value: code}}
	h.redeem(c)
	if rec.Code != 200 {
		t.Fatalf("redeem code %d", rec.Code)
	}
	var env map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	d := env["data"].(map[string]any)
	if d["filename"] != "report.pdf" || d["download_url"] == nil {
		t.Fatalf("redeem body wrong: %v", d)
	}
}
