package security

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// AccessClaims is the JWT payload for short-lived access tokens.
type AccessClaims struct {
	jwt.RegisteredClaims
}

// ErrTokenExpired / ErrTokenInvalid let callers map verification failures onto
// the right apperror without importing this package's internals.
var (
	ErrTokenExpired = errors.New("token expired")
	ErrTokenInvalid = errors.New("token invalid")
)

// JWTSigner mints and verifies HS256 access tokens. Constructed once from env.
type JWTSigner struct {
	secret []byte
	ttl    time.Duration
}

// NewJWTSigner builds a signer. ttl is the access-token lifetime.
func NewJWTSigner(secret string, ttl time.Duration) *JWTSigner {
	return &JWTSigner{secret: []byte(secret), ttl: ttl}
}

// TTL exposes the access-token lifetime (for the expires_in field).
func (s *JWTSigner) TTL() time.Duration { return s.ttl }

// Sign mints an access token whose subject is the user id.
func (s *JWTSigner) Sign(userID string) (string, error) {
	now := time.Now()
	claims := AccessClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.ttl)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString(s.secret)
}

// Verify parses and validates an access token, returning the user id (subject).
// Distinguishes expiry from other failures so the handler can pick the right
// error code.
func (s *JWTSigner) Verify(tokenStr string) (string, error) {
	claims := &AccessClaims{}
	_, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return "", ErrTokenExpired
		}
		return "", ErrTokenInvalid
	}
	if claims.Subject == "" {
		return "", ErrTokenInvalid
	}
	return claims.Subject, nil
}
