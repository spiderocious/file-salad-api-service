// Package httpx holds the shared HTTP vocabulary: status codes, the stable
// error-code set, and the response envelope shape. Mirrors the Node backend's
// shared/constants + shared/types so the two services speak the same wire
// format.
package httpx

// Stable, machine-readable error codes. Clients key off these, never off the
// human message. Keep this list as the single source of truth — adding a new
// error means adding a constant here.
const (
	CodeValidationError = "validation_error"
	CodeUnauthorized    = "unauthorized"
	CodeForbidden       = "forbidden"
	CodeNotFound        = "not_found"
	CodeConflict        = "conflict"
	CodeRateLimited     = "rate_limited"
	CodeInternal        = "internal"

	// auth
	CodeInvalidCredentials = "invalid_credentials"
	CodeEmailExists        = "email_exists"
	CodeTokenExpired       = "token_expired"
	CodeTokenInvalid       = "token_invalid"

	// uploads / quota
	CodeQuotaExceeded   = "quota_exceeded"
	CodeFileTooLarge    = "file_too_large"
	CodeStorageDisabled = "storage_unavailable"
)

// APIError is the error half of the response envelope.
type APIError struct {
	Code        string              `json:"code"`
	Message     string              `json:"message"`
	FieldErrors map[string][]string `json:"field_errors,omitempty"`
}

// Envelope is the single response shape for the whole API:
// success -> {data, meta?}; failure -> {error}.
type Envelope struct {
	Data  any            `json:"data,omitempty"`
	Error *APIError      `json:"error,omitempty"`
	Meta  map[string]any `json:"meta,omitempty"`
}
