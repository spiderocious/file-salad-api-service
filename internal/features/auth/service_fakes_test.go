package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/feranmi/file-salad-backend/internal/security"
)

// fakeRepo / fakeSessions let us force the error and edge branches the live
// integration tests don't naturally hit (DB/Redis failures, etc.).
type fakeRepo struct {
	insertErr   error
	findByEmail func(string) (*User, error)
	findByID    func(string) (*User, error)
}

func (f *fakeRepo) Insert(_ context.Context, _ *User) error { return f.insertErr }
func (f *fakeRepo) FindByEmail(_ context.Context, e string) (*User, error) {
	return f.findByEmail(e)
}
func (f *fakeRepo) FindByID(_ context.Context, id string) (*User, error) {
	return f.findByID(id)
}

type fakeSessions struct {
	createErr error
	rotateFn  func(string) (string, string, error)
	revokeErr error
}

func (f *fakeSessions) Create(_ context.Context, _ string) (string, error) {
	return "raw-refresh", f.createErr
}
func (f *fakeSessions) Rotate(_ context.Context, raw string) (string, string, error) {
	return f.rotateFn(raw)
}
func (f *fakeSessions) Revoke(_ context.Context, _ string) error { return f.revokeErr }

func sig() *security.JWTSigner {
	return security.NewJWTSigner("0123456789abcdef0123456789abcdef", 15*time.Minute)
}

func TestRegisterInsertError(t *testing.T) {
	svc := NewService(&fakeRepo{insertErr: errors.New("db down")}, &fakeSessions{}, sig())
	_, err := svc.Register(context.Background(), "a@b.cc", "Password123")
	if err == nil || err.Status != 500 {
		t.Fatalf("expected 500, got %v", err)
	}
}

func TestRegisterIssueSessionError(t *testing.T) {
	svc := NewService(&fakeRepo{}, &fakeSessions{createErr: errors.New("redis down")}, sig())
	_, err := svc.Register(context.Background(), "a@b.cc", "Password123")
	if err == nil || err.Status != 500 {
		t.Fatalf("expected 500 from session create, got %v", err)
	}
}

func TestLoginRepoError(t *testing.T) {
	svc := NewService(&fakeRepo{findByEmail: func(string) (*User, error) {
		return nil, errors.New("db down")
	}}, &fakeSessions{}, sig())
	_, err := svc.Login(context.Background(), "a@b.cc", "Password123")
	if err == nil || err.Status != 500 {
		t.Fatalf("expected 500, got %v", err)
	}
}

func TestLoginUnknownUser(t *testing.T) {
	svc := NewService(&fakeRepo{findByEmail: func(string) (*User, error) {
		return nil, ErrUserNotFound
	}}, &fakeSessions{}, sig())
	_, err := svc.Login(context.Background(), "a@b.cc", "Password123")
	if err == nil || err.Code != "invalid_credentials" {
		t.Fatalf("expected invalid_credentials, got %v", err)
	}
}

func TestLoginSuccess(t *testing.T) {
	hash, _ := security.HashPassword("Password123")
	svc := NewService(&fakeRepo{findByEmail: func(string) (*User, error) {
		return &User{ID: "u_1", Email: "a@b.cc", PasswordHash: hash}, nil
	}}, &fakeSessions{}, sig())
	res, err := svc.Login(context.Background(), "a@b.cc", "Password123")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if res.AccessToken == "" || res.RefreshToken != "raw-refresh" {
		t.Fatalf("tokens wrong: %+v", res)
	}
}

func TestRefreshRotateError(t *testing.T) {
	svc := NewService(&fakeRepo{}, &fakeSessions{rotateFn: func(string) (string, string, error) {
		return "", "", errors.New("redis down")
	}}, sig())
	_, err := svc.Refresh(context.Background(), "tok")
	if err == nil || err.Status != 500 {
		t.Fatalf("expected 500, got %v", err)
	}
}

func TestRefreshEmptyToken(t *testing.T) {
	svc := NewService(&fakeRepo{}, &fakeSessions{}, sig())
	_, err := svc.Refresh(context.Background(), "   ")
	if err == nil || err.Code != "token_invalid" {
		t.Fatalf("expected token_invalid, got %v", err)
	}
}

func TestRefreshSuccess(t *testing.T) {
	svc := NewService(&fakeRepo{}, &fakeSessions{rotateFn: func(string) (string, string, error) {
		return "new-refresh", "u_1", nil
	}}, sig())
	pair, err := svc.Refresh(context.Background(), "old")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if pair.RefreshToken != "new-refresh" || pair.AccessToken == "" {
		t.Fatalf("pair wrong: %+v", pair)
	}
}

func TestLogoutError(t *testing.T) {
	svc := NewService(&fakeRepo{}, &fakeSessions{revokeErr: errors.New("redis down")}, sig())
	if err := svc.Logout(context.Background(), "tok"); err == nil || err.Status != 500 {
		t.Fatalf("expected 500, got %v", err)
	}
}

func TestMeRepoError(t *testing.T) {
	svc := NewService(&fakeRepo{findByID: func(string) (*User, error) {
		return nil, errors.New("db down")
	}}, &fakeSessions{}, sig())
	_, err := svc.Me(context.Background(), "u_1")
	if err == nil || err.Status != 500 {
		t.Fatalf("expected 500, got %v", err)
	}
}

func TestMeNotFound(t *testing.T) {
	svc := NewService(&fakeRepo{findByID: func(string) (*User, error) {
		return nil, ErrUserNotFound
	}}, &fakeSessions{}, sig())
	_, err := svc.Me(context.Background(), "u_gone")
	if err == nil || err.Code != "not_found" {
		t.Fatalf("expected not_found, got %v", err)
	}
}

func TestMeSuccess(t *testing.T) {
	svc := NewService(&fakeRepo{findByID: func(string) (*User, error) {
		return &User{ID: "u_1", Email: "a@b.cc"}, nil
	}}, &fakeSessions{}, sig())
	pub, err := svc.Me(context.Background(), "u_1")
	if err != nil || pub.ID != "u_1" {
		t.Fatalf("me: %v %+v", err, pub)
	}
}
