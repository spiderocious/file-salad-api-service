package apperror

import (
	"net/http"
	"testing"

	"github.com/feranmi/file-salad-backend/internal/httpx"
)

func TestErrorImplementsError(t *testing.T) {
	e := New(httpx.CodeNotFound, 404, "gone")
	if e.Error() != "not_found: gone" {
		t.Fatalf("Error() = %q", e.Error())
	}
}

func TestConstructors(t *testing.T) {
	cases := []struct {
		name   string
		err    *Error
		code   string
		status int
	}{
		{"validation", Validation("bad", map[string][]string{"x": {"y"}}), httpx.CodeValidationError, http.StatusBadRequest},
		{"unauthorized", Unauthorized(""), httpx.CodeUnauthorized, http.StatusUnauthorized},
		{"forbidden", Forbidden(""), httpx.CodeForbidden, http.StatusForbidden},
		{"notfound", NotFound("Upload"), httpx.CodeNotFound, http.StatusNotFound},
		{"conflict", Conflict("dup"), httpx.CodeConflict, http.StatusConflict},
		{"invalidcreds", InvalidCredentials(), httpx.CodeInvalidCredentials, http.StatusUnauthorized},
		{"emailexists", EmailExists(), httpx.CodeEmailExists, http.StatusConflict},
		{"tokenexpired", TokenExpired(), httpx.CodeTokenExpired, http.StatusUnauthorized},
		{"tokeninvalid", TokenInvalid(), httpx.CodeTokenInvalid, http.StatusUnauthorized},
		{"quota", QuotaExceeded("full"), httpx.CodeQuotaExceeded, http.StatusForbidden},
		{"toolarge", FileTooLarge("big"), httpx.CodeFileTooLarge, http.StatusRequestEntityTooLarge},
		{"storage", StorageUnavailable(), httpx.CodeStorageDisabled, http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.Code != tc.code {
				t.Errorf("code = %q, want %q", tc.err.Code, tc.code)
			}
			if tc.err.Status != tc.status {
				t.Errorf("status = %d, want %d", tc.err.Status, tc.status)
			}
			if tc.err.Message == "" {
				t.Error("message is empty")
			}
		})
	}
}

func TestDefaultMessages(t *testing.T) {
	if Unauthorized("").Message != "Unauthorized" {
		t.Error("default unauthorized message")
	}
	if Forbidden("").Message != "Forbidden" {
		t.Error("default forbidden message")
	}
	if NotFound("").Message != "Resource not found" {
		t.Errorf("default notfound message: %q", NotFound("").Message)
	}
}

func TestValidationCarriesFieldErrors(t *testing.T) {
	e := Validation("bad", map[string][]string{"email": {"required"}})
	if len(e.FieldErrors["email"]) != 1 {
		t.Fatal("field errors not carried")
	}
}
