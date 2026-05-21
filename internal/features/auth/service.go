package auth

import (
	"context"
	"errors"
	"strings"

	"github.com/feranmi/file-salad-backend/internal/apperror"
	"github.com/feranmi/file-salad-backend/internal/ids"
	"github.com/feranmi/file-salad-backend/internal/security"
	"github.com/feranmi/file-salad-backend/internal/session"
)

// Service holds auth business logic. It returns (T, *apperror.Error): a nil
// error means success. Expected failures (bad creds, taken email) come back as
// typed app errors; unexpected ones (DB/Redis down) come back as a wrapped 500.
type Service struct {
	repo     *Repo
	sessions *session.Store
	jwt      *security.JWTSigner
}

func NewService(repo *Repo, sessions *session.Store, jwt *security.JWTSigner) *Service {
	return &Service{repo: repo, sessions: sessions, jwt: jwt}
}

func internalErr(err error) *apperror.Error {
	return &apperror.Error{
		Code:    "internal",
		Status:  500,
		Message: "An unexpected error occurred",
		// underlying error is logged by the error middleware via the wrapper
	}
}

// Register creates an account and returns tokens. Email is normalized to
// lowercase. A taken email yields 409 email_exists.
func (s *Service) Register(ctx context.Context, email, password string) (*AuthResponse, *apperror.Error) {
	email = normalizeEmail(email)

	hash, err := security.HashPassword(password)
	if err != nil {
		return nil, internalErr(err)
	}

	u := &User{ID: ids.Prefixed("u"), Email: email, PasswordHash: hash}
	touch(u)
	if err := s.repo.Insert(ctx, u); err != nil {
		if errors.Is(err, ErrEmailTaken) {
			return nil, apperror.EmailExists()
		}
		return nil, internalErr(err)
	}

	return s.issue(ctx, u)
}

// Login verifies credentials and returns tokens. Unknown email and wrong
// password both yield the same 401 invalid_credentials (no enumeration).
func (s *Service) Login(ctx context.Context, email, password string) (*AuthResponse, *apperror.Error) {
	email = normalizeEmail(email)

	u, err := s.repo.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return nil, apperror.InvalidCredentials()
		}
		return nil, internalErr(err)
	}

	ok, err := security.VerifyPassword(password, u.PasswordHash)
	if err != nil {
		return nil, internalErr(err)
	}
	if !ok {
		return nil, apperror.InvalidCredentials()
	}

	return s.issue(ctx, u)
}

// Refresh rotates the refresh token. A token that isn't an active session is
// treated as a reuse/theft signal: if it's well-formed we can't safely trust
// the bearer, so we reject. Rotation itself revokes the old and issues a new.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (*TokenPair, *apperror.Error) {
	if strings.TrimSpace(refreshToken) == "" {
		return nil, apperror.TokenInvalid()
	}

	newRefresh, userID, err := s.sessions.Rotate(ctx, refreshToken)
	if err != nil {
		if errors.Is(err, session.ErrNotFound) {
			// The token isn't active. Either it expired/was used, or it's a
			// replay of a consumed token. We can't distinguish a benign expiry
			// from theft here, so reject as invalid. (Reuse of a *consumed*
			// token is handled by rotation having already deleted it.)
			return nil, apperror.TokenInvalid()
		}
		return nil, internalErr(err)
	}

	access, err := s.jwt.Sign(userID)
	if err != nil {
		return nil, internalErr(err)
	}
	return &TokenPair{
		AccessToken:  access,
		RefreshToken: newRefresh,
		ExpiresIn:    int(s.jwt.TTL().Seconds()),
	}, nil
}

// Logout revokes the presented refresh session. Idempotent.
func (s *Service) Logout(ctx context.Context, refreshToken string) *apperror.Error {
	if err := s.sessions.Revoke(ctx, refreshToken); err != nil {
		return internalErr(err)
	}
	return nil
}

// Me returns the public user for an authenticated id.
func (s *Service) Me(ctx context.Context, userID string) (*PublicUser, *apperror.Error) {
	u, err := s.repo.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return nil, apperror.NotFound("User")
		}
		return nil, internalErr(err)
	}
	pub := u.Public()
	return &pub, nil
}

// issue mints an access token + a fresh refresh session for the user.
func (s *Service) issue(ctx context.Context, u *User) (*AuthResponse, *apperror.Error) {
	access, err := s.jwt.Sign(u.ID)
	if err != nil {
		return nil, internalErr(err)
	}
	refresh, err := s.sessions.Create(ctx, u.ID)
	if err != nil {
		return nil, internalErr(err)
	}
	return &AuthResponse{
		User:         u.Public(),
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int(s.jwt.TTL().Seconds()),
	}, nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
