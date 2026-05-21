// Package apperror is the domain error type. Services and handlers return an
// *Error for expected failures (validation, not-found, conflict); the central
// error middleware translates it into the response envelope. This mirrors the
// Node backend's AppError hierarchy — except in Go we return errors as values
// rather than throwing, which is the same "don't throw for expected failures"
// discipline the house style asks for.
package apperror

import (
	"fmt"
	"net/http"

	"github.com/feranmi/file-salad-backend/internal/httpx"
)

// Error carries a stable code, an HTTP status, a human message, and optional
// field-level validation errors. It implements the error interface.
type Error struct {
	Code        string
	Status      int
	Message     string
	FieldErrors map[string][]string
	// RetryAfter, when > 0, is emitted as the Retry-After header (seconds).
	RetryAfter int
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// New builds an error with an explicit code/status/message.
func New(code string, status int, message string) *Error {
	return &Error{Code: code, Status: status, Message: message}
}

// Validation is a 400 with field-level detail.
func Validation(message string, fieldErrors map[string][]string) *Error {
	return &Error{
		Code:        httpx.CodeValidationError,
		Status:      http.StatusBadRequest,
		Message:     message,
		FieldErrors: fieldErrors,
	}
}

// Unauthorized is a 401.
func Unauthorized(message string) *Error {
	if message == "" {
		message = "Unauthorized"
	}
	return New(httpx.CodeUnauthorized, http.StatusUnauthorized, message)
}

// Forbidden is a 403.
func Forbidden(message string) *Error {
	if message == "" {
		message = "Forbidden"
	}
	return New(httpx.CodeForbidden, http.StatusForbidden, message)
}

// NotFound is a 404. Pass the resource name, e.g. NotFound("Upload").
func NotFound(resource string) *Error {
	if resource == "" {
		resource = "Resource"
	}
	return New(httpx.CodeNotFound, http.StatusNotFound, resource+" not found")
}

// Conflict is a 409.
func Conflict(message string) *Error {
	return New(httpx.CodeConflict, http.StatusConflict, message)
}

// InvalidCredentials is a 401 used for both unknown-email and wrong-password so
// the two are indistinguishable (no user enumeration).
func InvalidCredentials() *Error {
	return New(httpx.CodeInvalidCredentials, http.StatusUnauthorized, "Invalid email or password")
}

// EmailExists is a 409 for register on a taken email.
func EmailExists() *Error {
	return New(httpx.CodeEmailExists, http.StatusConflict, "An account with that email already exists")
}

// TokenExpired is a 401 for an expired access/refresh token.
func TokenExpired() *Error {
	return New(httpx.CodeTokenExpired, http.StatusUnauthorized, "Token has expired")
}

// TokenInvalid is a 401 for a malformed/unverifiable/revoked token.
func TokenInvalid() *Error {
	return New(httpx.CodeTokenInvalid, http.StatusUnauthorized, "Token is invalid")
}

// QuotaExceeded is a 403 when the monthly upload cap is hit.
func QuotaExceeded(message string) *Error {
	return New(httpx.CodeQuotaExceeded, http.StatusForbidden, message)
}

// FileTooLarge is a 413 when the requested upload exceeds the per-file limit.
func FileTooLarge(message string) *Error {
	return New(httpx.CodeFileTooLarge, http.StatusRequestEntityTooLarge, message)
}

// StorageUnavailable is a 503 when object storage isn't configured/reachable.
func StorageUnavailable() *Error {
	return New(httpx.CodeStorageDisabled, http.StatusServiceUnavailable, "Storage is not available")
}
