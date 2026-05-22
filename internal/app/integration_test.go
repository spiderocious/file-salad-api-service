//go:build integration

// End-to-end integration tests driving the full Gin engine via httptest against
// real MongoDB (required) + in-memory Redis (miniredis) and a fake-but-local
// presigner. Run with: go test -tags=integration ./...
//
// Mongo is required because the quota counter relies on atomic findOneAndUpdate
// and unique indexes that no in-memory fake reproduces faithfully. If Mongo
// isn't reachable the tests skip (not fail).
package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"

	"github.com/feranmi/file-salad-backend/internal/app"
	"github.com/feranmi/file-salad-backend/internal/db"
	"github.com/feranmi/file-salad-backend/internal/env"
	"github.com/feranmi/file-salad-backend/internal/features/auth"
	"github.com/feranmi/file-salad-backend/internal/features/share"
	"github.com/feranmi/file-salad-backend/internal/features/uploads"
	"github.com/feranmi/file-salad-backend/internal/features/webuploads"
	"github.com/feranmi/file-salad-backend/internal/quota"
	appredis "github.com/feranmi/file-salad-backend/internal/redis"
	"github.com/feranmi/file-salad-backend/internal/security"
	"github.com/feranmi/file-salad-backend/internal/session"
	"github.com/feranmi/file-salad-backend/internal/sharecode"
	"github.com/feranmi/file-salad-backend/internal/stats"
	"github.com/feranmi/file-salad-backend/internal/storage"
)

type harness struct {
	engine *gin.Engine
	dbName string
	mc     *db.Mongo
}

func mongoURI() string {
	if v := os.Getenv("MONGODB_URI"); v != "" {
		return v
	}
	return "mongodb://localhost:27017"
}

func setup(t *testing.T) *harness {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	dbName := fmt.Sprintf("filesalad_it_%d", time.Now().UnixNano())
	mc, err := db.Connect(ctx, mongoURI(), dbName)
	if err != nil {
		t.Skipf("MongoDB not reachable (%v) — skipping integration tests", err)
	}
	t.Cleanup(func() {
		_ = mc.DB.Drop(ctx)
		_ = mc.Disconnect(ctx)
	})

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	rc, err := appredis.Connect(ctx, "redis://"+mr.Addr())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rc.Close() })

	jwt := security.NewJWTSigner("0123456789abcdef0123456789abcdef", 15*time.Minute)

	userRepo := auth.NewRepo(mc.DB)
	if err := userRepo.EnsureIndexes(ctx); err != nil {
		t.Fatal(err)
	}
	sessions := session.NewStore(rc, time.Hour)
	authDeps := auth.Deps{Service: auth.NewService(userRepo, sessions, jwt), JWT: jwt}

	uploadRepo := uploads.NewRepo(mc.DB)
	if err := uploadRepo.EnsureIndexes(ctx); err != nil {
		t.Fatal(err)
	}
	store := storage.New(storage.Config{
		Endpoint: "https://t3.storage.dev", Region: "auto",
		AccessKeyID: "k", SecretAccessKey: "s", Bucket: "filesalad",
		UploadTTL: 15 * time.Minute, DownloadTTL: time.Hour,
	}, rc)
	counter := quota.NewCounter(mc.DB, 2) // small cap so we can hit it fast
	statsCounter := stats.NewCounter(mc.DB)
	uploadSvc := uploads.NewService(uploadRepo, store, counter, 1000, 90)
	codes := sharecode.NewStore(rc, time.Hour)

	cfg := &env.Env{NodeEnv: "test", WebBaseURL: "*"}
	engine := app.Build(cfg, app.Deps{
		Auth:    authDeps,
		Uploads: &uploads.Deps{Service: uploadSvc, JWT: jwt, Counter: statsCounter},
		WebUploads: &webuploads.Deps{
			Repo: uploadRepo, Store: store, Quota: counter, Stats: statsCounter,
			MaxFileSize: 1000, LinkDays: 90,
		},
		Share: &share.Deps{Uploads: uploadRepo, Codes: codes, Store: store, Redis: rc},
	})

	return &harness{engine: engine, dbName: dbName, mc: mc}
}

func (h *harness) do(t *testing.T, method, path string, body any, headers map[string]string) (int, map[string]any) {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.engine.ServeHTTP(rec, req)

	var out map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
	}
	return rec.Code, out
}

func dataMap(m map[string]any) map[string]any {
	if d, ok := m["data"].(map[string]any); ok {
		return d
	}
	return nil
}
func errCode(m map[string]any) string {
	if e, ok := m["error"].(map[string]any); ok {
		if c, ok := e["code"].(string); ok {
			return c
		}
	}
	return ""
}

func TestAuthFlowIntegration(t *testing.T) {
	h := setup(t)
	bearer := func(tok string) map[string]string { return map[string]string{"Authorization": "Bearer " + tok} }

	// register
	code, body := h.do(t, "POST", "/api/v1/auth/register",
		map[string]string{"email": "Alice@Test.test", "password": "Password123"}, nil)
	if code != http.StatusCreated {
		t.Fatalf("register code %d body %v", code, body)
	}
	d := dataMap(body)
	if dataMap(d)["email"] == nil && d["user"].(map[string]any)["email"] != "alice@test.test" {
		t.Fatalf("email not normalized: %v", d["user"])
	}
	access := d["access_token"].(string)
	refresh := d["refresh_token"].(string)

	// dup → 409
	if code, _ := h.do(t, "POST", "/api/v1/auth/register",
		map[string]string{"email": "alice@test.test", "password": "Password123"}, nil); code != 409 {
		t.Fatalf("dup register code %d", code)
	}

	// wrong password → 401 invalid_credentials
	code, body = h.do(t, "POST", "/api/v1/auth/login",
		map[string]string{"email": "alice@test.test", "password": "Wrongggg1"}, nil)
	if code != 401 || errCode(body) != "invalid_credentials" {
		t.Fatalf("bad login %d %s", code, errCode(body))
	}

	// /me ok
	if code, _ := h.do(t, "GET", "/api/v1/me", nil, bearer(access)); code != 200 {
		t.Fatalf("/me code %d", code)
	}
	// /me no token → 401
	if code, _ := h.do(t, "GET", "/api/v1/me", nil, nil); code != 401 {
		t.Fatalf("/me no token code %d", code)
	}

	// refresh rotates
	code, body = h.do(t, "POST", "/api/v1/auth/refresh", map[string]string{"refresh_token": refresh}, nil)
	if code != 200 {
		t.Fatalf("refresh code %d", code)
	}
	newRefresh := dataMap(body)["refresh_token"].(string)
	if newRefresh == refresh {
		t.Fatal("refresh did not rotate")
	}
	// reuse old → 401
	if code, _ := h.do(t, "POST", "/api/v1/auth/refresh", map[string]string{"refresh_token": refresh}, nil); code != 401 {
		t.Fatalf("reuse refresh code %d", code)
	}
	// logout → 204, then refresh fails
	if code, _ := h.do(t, "POST", "/api/v1/auth/logout", map[string]string{"refresh_token": newRefresh}, nil); code != 204 {
		t.Fatalf("logout code %d", code)
	}
	if code, _ := h.do(t, "POST", "/api/v1/auth/refresh", map[string]string{"refresh_token": newRefresh}, nil); code != 401 {
		t.Fatalf("refresh after logout code %d", code)
	}
}

func TestHostedUploadFlowIntegration(t *testing.T) {
	h := setup(t)

	// register to get a token
	_, body := h.do(t, "POST", "/api/v1/auth/register",
		map[string]string{"email": "up@test.test", "password": "Password123"}, nil)
	access := dataMap(body)["access_token"].(string)
	bear := map[string]string{"Authorization": "Bearer " + access}

	// presign
	code, body := h.do(t, "POST", "/api/v1/uploads/presign",
		map[string]any{"filename": "a.png", "content_type": "image/png", "size": 100}, bear)
	if code != http.StatusCreated {
		t.Fatalf("presign code %d body %v", code, body)
	}
	d := dataMap(body)
	id := d["upload_id"].(string)
	if d["upload_url"] == nil || d["public_url"] == nil || d["key"] == nil {
		t.Fatalf("presign missing fields: %v", d)
	}
	// public_url must be a presigned GET (resolves on a private bucket), not a
	// bare constructed URL.
	if pu, _ := d["public_url"].(string); !strings.Contains(pu, "X-Amz-Signature=") {
		t.Fatalf("public_url is not a presigned GET: %v", pu)
	}
	// Each URL carries its own expiry; the public (download) TTL must exceed the
	// upload TTL, and the absolute timestamps must be present.
	upIn := d["upload_url_expires_in"].(float64)
	pubIn := d["public_url_expires_in"].(float64)
	if upIn <= 0 || pubIn <= 0 || pubIn <= upIn {
		t.Fatalf("expiry seconds wrong: upload=%v public=%v", upIn, pubIn)
	}
	if d["upload_url_expires_at"] == "" || d["public_url_expires_at"] == "" {
		t.Fatalf("missing *_expires_at: %v", d)
	}
	if d["expires_in"].(float64) != upIn {
		t.Fatal("deprecated expires_in should equal upload_url_expires_in")
	}
	usage := d["usage"].(map[string]any)
	if usage["used"].(float64) != 1 {
		t.Fatalf("usage.used = %v", usage["used"])
	}

	// over size limit (limit is 1000) → 413
	if code, _ := h.do(t, "POST", "/api/v1/uploads/presign",
		map[string]any{"filename": "big.bin", "content_type": "application/octet-stream", "size": 99999}, bear); code != 413 {
		t.Fatalf("oversize code %d", code)
	}

	// complete
	code, body = h.do(t, "POST", "/api/v1/uploads/"+id+"/complete", nil, bear)
	if code != 200 || dataMap(body)["upload"].(map[string]any)["status"] != "ready" {
		t.Fatalf("complete code %d body %v", code, body)
	}
	// complete unknown → 404
	if code, _ := h.do(t, "POST", "/api/v1/uploads/up_nope/complete", nil, bear); code != 404 {
		t.Fatalf("complete unknown code %d", code)
	}

	// list
	code, body = h.do(t, "GET", "/api/v1/uploads", nil, bear)
	if code != 200 {
		t.Fatalf("list code %d", code)
	}
	if arr, ok := body["data"].([]any); !ok || len(arr) < 1 {
		t.Fatalf("list data wrong: %v", body["data"])
	}

	// download → signed url, cached on 2nd call
	code, body = h.do(t, "GET", "/api/v1/uploads/"+id+"/download", nil, bear)
	if code != 200 || dataMap(body)["download_url"] == nil {
		t.Fatalf("download code %d body %v", code, body)
	}
	if dataMap(body)["cached"].(bool) {
		t.Fatal("first download should not be cached")
	}
	_, body = h.do(t, "GET", "/api/v1/uploads/"+id+"/download", nil, bear)
	if !dataMap(body)["cached"].(bool) {
		t.Fatal("second download should be cached")
	}
}

func TestHostedUploadCapAndCrossOwnerIntegration(t *testing.T) {
	h := setup(t)

	tok := func(email string) map[string]string {
		_, body := h.do(t, "POST", "/api/v1/auth/register",
			map[string]string{"email": email, "password": "Password123"}, nil)
		return map[string]string{"Authorization": "Bearer " + dataMap(body)["access_token"].(string)}
	}
	alice := tok("cap-alice@test.test")
	bob := tok("cap-bob@test.test")

	presign := func(hdr map[string]string) int {
		code, _ := h.do(t, "POST", "/api/v1/uploads/presign",
			map[string]any{"filename": "a.png", "content_type": "image/png", "size": 10}, hdr)
		return code
	}

	// cap is 2 → first two succeed, third is 403 quota_exceeded
	if presign(alice) != 201 || presign(alice) != 201 {
		t.Fatal("first two uploads should succeed")
	}
	code, body := h.do(t, "POST", "/api/v1/uploads/presign",
		map[string]any{"filename": "a.png", "content_type": "image/png", "size": 10}, alice)
	if code != 403 || errCode(body) != "quota_exceeded" {
		t.Fatalf("third upload code %d code=%s", code, errCode(body))
	}

	// bob has his own counter — unaffected
	if presign(bob) != 201 {
		t.Fatal("bob's first upload should succeed (separate quota)")
	}

	// cross-owner: bob creates an upload, alice can't complete/download it
	_, body = h.do(t, "POST", "/api/v1/uploads/presign",
		map[string]any{"filename": "b.png", "content_type": "image/png", "size": 10}, bob)
	// bob is now at cap too (cap 2); use the id from his first... simpler: this
	// presign may 403. Re-fetch via list instead.
	code, listBody := h.do(t, "GET", "/api/v1/uploads", nil, bob)
	if code != 200 {
		t.Fatalf("bob list code %d", code)
	}
	arr := listBody["data"].([]any)
	bobUploadID := arr[0].(map[string]any)["id"].(string)
	if code, _ := h.do(t, "GET", "/api/v1/uploads/"+bobUploadID+"/download", nil, alice); code != 404 {
		t.Fatalf("alice downloading bob's upload should be 404, got %d", code)
	}
}

func TestWebUploadFlowIntegration(t *testing.T) {
	h := setup(t)
	fp := map[string]string{"X-Fingerprint": "fp_web_1"}

	// missing fingerprint → 400
	if code, _ := h.do(t, "POST", "/api/v1/web/uploads/presign",
		map[string]any{"filename": "a.png", "content_type": "image/png", "size": 10}, nil); code != 400 {
		t.Fatalf("missing fingerprint code %d", code)
	}

	// usage starts at 0
	code, body := h.do(t, "GET", "/api/v1/web/usage", nil, fp)
	if code != 200 || dataMap(body)["used"].(float64) != 0 {
		t.Fatalf("initial usage %d %v", code, body)
	}

	// presign x2 (cap 2)
	var id string
	for i := 0; i < 2; i++ {
		code, body = h.do(t, "POST", "/api/v1/web/uploads/presign",
			map[string]any{"filename": "a.png", "content_type": "image/png", "size": 10}, fp)
		if code != 201 {
			t.Fatalf("web presign %d code %d", i, code)
		}
		d := dataMap(body)
		id = d["upload_id"].(string)
		// public_url is a presigned GET (works on a private bucket).
		if pu, _ := d["public_url"].(string); !strings.Contains(pu, "X-Amz-Signature=") {
			t.Fatalf("web public_url not a presigned GET: %v", pu)
		}
	}
	// 3rd → 403 quota_exceeded (cap reached, no upsell)
	if code, body := h.do(t, "POST", "/api/v1/web/uploads/presign",
		map[string]any{"filename": "a.png", "content_type": "image/png", "size": 10}, fp); code != 403 || errCode(body) != "quota_exceeded" {
		t.Fatalf("web cap code %d code=%s", code, errCode(body))
	}

	// download a web upload — carries both relative and absolute expiry
	if code, body := h.do(t, "GET", "/api/v1/web/uploads/"+id+"/download", nil, fp); code != 200 ||
		dataMap(body)["download_url"] == nil || dataMap(body)["expires_at"] == "" ||
		dataMap(body)["expires_in"].(float64) <= 0 {
		t.Fatalf("web download %d %v", code, body)
	}
	// unknown web id → 404
	if code, _ := h.do(t, "GET", "/api/v1/web/uploads/up_nope/download", nil, fp); code != 404 {
		t.Fatalf("web download unknown code %d", code)
	}

	// usage reflects 2 used
	_, body = h.do(t, "GET", "/api/v1/web/usage", nil, fp)
	if dataMap(body)["used"].(float64) != 2 {
		t.Fatalf("usage.used = %v", dataMap(body)["used"])
	}
}

func TestAuthErrorBranchesIntegration(t *testing.T) {
	h := setup(t)

	// validation errors (cover the schema-fail handler branches)
	if code, _ := h.do(t, "POST", "/api/v1/auth/register", map[string]string{"email": "bad"}, nil); code != 400 {
		t.Fatalf("register validation code %d", code)
	}
	if code, _ := h.do(t, "POST", "/api/v1/auth/login", map[string]string{}, nil); code != 400 {
		t.Fatalf("login validation code %d", code)
	}
	if code, _ := h.do(t, "POST", "/api/v1/auth/refresh", map[string]string{}, nil); code != 400 {
		t.Fatalf("refresh validation code %d", code)
	}
	if code, _ := h.do(t, "POST", "/api/v1/auth/logout", map[string]string{}, nil); code != 400 {
		t.Fatalf("logout validation code %d", code)
	}
	// refresh with a non-empty but unknown token → 401 token_invalid
	if code, body := h.do(t, "POST", "/api/v1/auth/refresh", map[string]string{"refresh_token": "deadbeef"}, nil); code != 401 || errCode(body) != "token_invalid" {
		t.Fatalf("unknown refresh %d %s", code, errCode(body))
	}
	// logout an unknown token → still 204 (idempotent)
	if code, _ := h.do(t, "POST", "/api/v1/auth/logout", map[string]string{"refresh_token": "deadbeef"}, nil); code != 204 {
		t.Fatalf("logout unknown token code %d", code)
	}
}

func TestUploadValidationBranchesIntegration(t *testing.T) {
	h := setup(t)
	_, body := h.do(t, "POST", "/api/v1/auth/register",
		map[string]string{"email": "val@test.test", "password": "Password123"}, nil)
	bear := map[string]string{"Authorization": "Bearer " + dataMap(body)["access_token"].(string)}

	// missing fields → 400
	if code, _ := h.do(t, "POST", "/api/v1/uploads/presign", map[string]any{}, bear); code != 400 {
		t.Fatalf("presign validation code %d", code)
	}
	// list with explicit cursor + limit params (covers the cursor/limit branches)
	if code, _ := h.do(t, "GET", "/api/v1/uploads?limit=1&cursor=up_zzz", nil, bear); code != 200 {
		t.Fatalf("list with params code %d", code)
	}
	// download unknown → 404
	if code, _ := h.do(t, "GET", "/api/v1/uploads/up_nope/download", nil, bear); code != 404 {
		t.Fatalf("download unknown code %d", code)
	}

	// web: oversize → 413, missing fields → 400
	fp := map[string]string{"X-Fingerprint": "fp_val"}
	if code, _ := h.do(t, "POST", "/api/v1/web/uploads/presign",
		map[string]any{"filename": "big", "content_type": "x", "size": 99999}, fp); code != 413 {
		t.Fatalf("web oversize code %d", code)
	}
	if code, _ := h.do(t, "POST", "/api/v1/web/uploads/presign", map[string]any{}, fp); code != 400 {
		t.Fatalf("web validation code %d", code)
	}
	// web usage missing fingerprint → 400
	if code, _ := h.do(t, "GET", "/api/v1/web/usage", nil, nil); code != 400 {
		t.Fatalf("web usage no-fp code %d", code)
	}
}

func TestWebStatsIntegration(t *testing.T) {
	h := setup(t)
	fp := map[string]string{"X-Fingerprint": "fp_stats"}

	// public, no auth, no fingerprint required → starts at 0
	code, body := h.do(t, "GET", "/api/v1/web/stats", nil, nil)
	if code != 200 {
		t.Fatalf("stats code %d", code)
	}
	if dataMap(body)["uploads_total"].(float64) != 0 {
		t.Fatalf("initial total = %v, want 0", dataMap(body)["uploads_total"])
	}

	// two web presigns (cap is 2) → counter should reach 2 in the background
	for i := 0; i < 2; i++ {
		if code, _ := h.do(t, "POST", "/api/v1/web/uploads/presign",
			map[string]any{"filename": "a.png", "content_type": "image/png", "size": 10}, fp); code != 201 {
			t.Fatalf("presign %d code %d", i, code)
		}
	}
	// one hosted presign too (counter spans both surfaces)
	_, rb := h.do(t, "POST", "/api/v1/auth/register",
		map[string]string{"email": "stats@test.test", "password": "Password123"}, nil)
	bear := map[string]string{"Authorization": "Bearer " + dataMap(rb)["access_token"].(string)}
	if code, _ := h.do(t, "POST", "/api/v1/uploads/presign",
		map[string]any{"filename": "h.png", "content_type": "image/png", "size": 10}, bear); code != 201 {
		t.Fatal("hosted presign failed")
	}

	// Increments are async; poll until they land (or time out).
	var total float64
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, body = h.do(t, "GET", "/api/v1/web/stats", nil, nil)
		total = dataMap(body)["uploads_total"].(float64)
		if total >= 3 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if total != 3 {
		t.Fatalf("uploads_total = %v, want 3 (2 web + 1 hosted)", total)
	}
}

func TestShareCodeFlowIntegration(t *testing.T) {
	h := setup(t)
	fp := map[string]string{"X-Fingerprint": "fp_share"}

	// Upload a web file to share.
	_, body := h.do(t, "POST", "/api/v1/web/uploads/presign",
		map[string]any{"filename": "doc.pdf", "content_type": "application/pdf", "size": 10}, fp)
	uploadID := dataMap(body)["upload_id"].(string)

	// Create a share code.
	code, body := h.do(t, "POST", "/api/v1/share", map[string]string{"upload_id": uploadID}, nil)
	if code != http.StatusCreated {
		t.Fatalf("create share code %d body %v", code, body)
	}
	shareCode := dataMap(body)["code"].(string)
	if len(shareCode) != 7 {
		t.Fatalf("share code wrong length: %q", shareCode)
	}
	if dataMap(body)["expires_in"].(float64) <= 0 {
		t.Fatal("expires_in should be positive")
	}

	// Redeem it → presigned download URL + filename.
	code, body = h.do(t, "GET", "/api/v1/share/"+shareCode, nil, nil)
	if code != 200 {
		t.Fatalf("redeem %d body %v", code, body)
	}
	d := dataMap(body)
	if d["filename"] != "doc.pdf" {
		t.Fatalf("filename = %v", d["filename"])
	}
	if pu, _ := d["download_url"].(string); !strings.Contains(pu, "X-Amz-Signature=") {
		t.Fatalf("download_url not a presigned GET: %v", pu)
	}
	if d["expires_at"] == "" || d["expires_in"].(float64) <= 0 {
		t.Fatalf("redeem missing expiry fields: %v", d)
	}

	// Unknown code → 404.
	if code, _ := h.do(t, "GET", "/api/v1/share/ZZZZZZZ", nil, nil); code != 404 {
		t.Fatalf("unknown code should be 404, got %d", code)
	}

	// Create with unknown upload_id → 404; missing → 400.
	if code, _ := h.do(t, "POST", "/api/v1/share", map[string]string{"upload_id": "up_nope"}, nil); code != 404 {
		t.Fatalf("unknown upload should be 404, got %d", code)
	}
	if code, _ := h.do(t, "POST", "/api/v1/share", map[string]string{}, nil); code != 400 {
		t.Fatalf("missing upload_id should be 400, got %d", code)
	}
}

func TestShareRedeemRateLimitIntegration(t *testing.T) {
	h := setup(t)
	// The redeem limiter allows 10/min/IP. The 11th (even for an unknown code)
	// must be 429 — proving brute-force is throttled.
	var got429 bool
	for i := 0; i < 12; i++ {
		code, _ := h.do(t, "GET", "/api/v1/share/ABCDEFG", nil, nil)
		if code == 429 {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Fatal("expected a 429 within 12 redeem attempts (rate limit not enforced)")
	}
}

func TestHealthAnd404Integration(t *testing.T) {
	h := setup(t)
	if code, body := h.do(t, "GET", "/api/v1/health", nil, nil); code != 200 || dataMap(body)["status"] != "ok" {
		t.Fatalf("health %d %v", code, body)
	}
	if code, body := h.do(t, "GET", "/nope", nil, nil); code != 404 || errCode(body) != "not_found" {
		t.Fatalf("404 %d %s", code, errCode(body))
	}
}
