//go:build integration

// Auth flow integration test against real Mongo + Redis via Testcontainers.
// Run with: go test -tags=integration ./internal/features/auth/...
package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	"github.com/feranmi/file-salad-backend/internal/app"
	"github.com/feranmi/file-salad-backend/internal/db"
	"github.com/feranmi/file-salad-backend/internal/env"
	"github.com/feranmi/file-salad-backend/internal/features/auth"
	appredis "github.com/feranmi/file-salad-backend/internal/redis"
	"github.com/feranmi/file-salad-backend/internal/security"
	"github.com/feranmi/file-salad-backend/internal/session"
	"github.com/gin-gonic/gin"
)

func setup(t *testing.T) *gin.Engine {
	t.Helper()
	ctx := context.Background()
	gin.SetMode(gin.TestMode)

	mongoC, err := mongodb.Run(ctx, "mongo:7")
	if err != nil {
		t.Fatalf("mongo container: %v", err)
	}
	t.Cleanup(func() { _ = mongoC.Terminate(ctx) })
	mongoURI, err := mongoC.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("mongo uri: %v", err)
	}

	redisC, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Fatalf("redis container: %v", err)
	}
	t.Cleanup(func() { _ = redisC.Terminate(ctx) })
	redisURI, err := redisC.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("redis uri: %v", err)
	}

	mc, err := db.Connect(ctx, mongoURI, "file_salad_test")
	if err != nil {
		t.Fatalf("mongo connect: %v", err)
	}
	t.Cleanup(func() { _ = mc.Disconnect(ctx) })

	rc, err := appredis.Connect(ctx, redisURI)
	if err != nil {
		t.Fatalf("redis connect: %v", err)
	}
	t.Cleanup(func() { _ = rc.Close() })

	repo := auth.NewRepo(mc.DB)
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("indexes: %v", err)
	}
	jwt := security.NewJWTSigner("0123456789abcdef0123456789abcdef", 15*time.Minute)
	sessions := session.NewStore(rc, 30*24*time.Hour)
	svc := auth.NewService(repo, sessions, jwt)

	cfg := &env.Env{NodeEnv: "test", WebBaseURL: "*"}
	return app.Build(cfg, app.Deps{Auth: auth.Deps{Service: svc, JWT: jwt}})
}

type tokens struct {
	Data struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		User         struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"user"`
	} `json:"data"`
}

func post(t *testing.T, e *gin.Engine, path string, body any, bearer string) (*httptest.ResponseRecorder, []byte) {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec, rec.Body.Bytes()
}

func TestAuthFlow(t *testing.T) {
	e := setup(t)

	// Register
	rec, raw := post(t, e, "/api/v1/auth/register", map[string]string{
		"email": "Alice@Test.test", "password": "Password123",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: code %d body %s", rec.Code, raw)
	}
	var reg tokens
	_ = json.Unmarshal(raw, &reg)
	if reg.Data.AccessToken == "" || reg.Data.RefreshToken == "" {
		t.Fatal("register: missing tokens")
	}
	if reg.Data.User.Email != "alice@test.test" {
		t.Fatalf("email not normalized: %q", reg.Data.User.Email)
	}

	// Duplicate email -> 409
	rec, _ = post(t, e, "/api/v1/auth/register", map[string]string{
		"email": "alice@test.test", "password": "Password123",
	}, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("dup register: code %d, want 409", rec.Code)
	}

	// Login wrong password -> 401
	rec, _ = post(t, e, "/api/v1/auth/login", map[string]string{
		"email": "alice@test.test", "password": "WrongPass1",
	}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad login: code %d, want 401", rec.Code)
	}

	// Login correct -> 200
	rec, raw = post(t, e, "/api/v1/auth/login", map[string]string{
		"email": "alice@test.test", "password": "Password123",
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login: code %d body %s", rec.Code, raw)
	}
	var login tokens
	_ = json.Unmarshal(raw, &login)

	// /me with the access token -> 200
	meReq := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+login.Data.AccessToken)
	meRec := httptest.NewRecorder()
	e.ServeHTTP(meRec, meReq)
	if meRec.Code != http.StatusOK {
		t.Fatalf("/me: code %d body %s", meRec.Code, meRec.Body.String())
	}

	// /me with no token -> 401
	meReq2 := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	meRec2 := httptest.NewRecorder()
	e.ServeHTTP(meRec2, meReq2)
	if meRec2.Code != http.StatusUnauthorized {
		t.Fatalf("/me no token: code %d, want 401", meRec2.Code)
	}

	// Refresh rotates: old refresh token must then be rejected.
	rec, raw = post(t, e, "/api/v1/auth/refresh", map[string]string{
		"refresh_token": login.Data.RefreshToken,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: code %d body %s", rec.Code, raw)
	}
	var refreshed tokens
	_ = json.Unmarshal(raw, &refreshed)
	if refreshed.Data.RefreshToken == login.Data.RefreshToken {
		t.Fatal("refresh did not rotate the token")
	}

	// Reusing the OLD (now consumed) refresh token -> rejected.
	rec, _ = post(t, e, "/api/v1/auth/refresh", map[string]string{
		"refresh_token": login.Data.RefreshToken,
	}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("reused refresh: code %d, want 401", rec.Code)
	}

	// Logout the rotated token -> 204, then it can't refresh.
	rec, _ = post(t, e, "/api/v1/auth/logout", map[string]string{
		"refresh_token": refreshed.Data.RefreshToken,
	}, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout: code %d, want 204", rec.Code)
	}
	rec, _ = post(t, e, "/api/v1/auth/refresh", map[string]string{
		"refresh_token": refreshed.Data.RefreshToken,
	}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("refresh after logout: code %d, want 401", rec.Code)
	}
}
