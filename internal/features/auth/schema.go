package auth

import (
	"regexp"
	"unicode"

	"github.com/feranmi/file-salad-backend/internal/apperror"
)

// Request bodies. We bind loosely then validate explicitly so we can produce a
// field_errors envelope.

type registerBody struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginBody struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type refreshBody struct {
	RefreshToken string `json:"refresh_token"`
}

type logoutBody struct {
	RefreshToken string `json:"refresh_token"`
}

var emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// validatePassword enforces the policy: min 8, at least one each of
// lower/upper/digit. Returns the per-field messages (empty if OK).
func validatePassword(pw string) []string {
	var msgs []string
	if len(pw) < 8 {
		msgs = append(msgs, "Must be at least 8 characters")
	}
	var hasLower, hasUpper, hasDigit bool
	for _, r := range pw {
		switch {
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}
	if !hasLower || !hasUpper || !hasDigit {
		msgs = append(msgs, "Must contain upper, lower, and a digit")
	}
	return msgs
}

func (b registerBody) validate() *apperror.Error {
	fe := map[string][]string{}
	if b.Email == "" {
		fe["email"] = []string{"Required"}
	} else if !emailRe.MatchString(b.Email) {
		fe["email"] = []string{"Invalid email"}
	}
	if b.Password == "" {
		fe["password"] = []string{"Required"}
	} else if msgs := validatePassword(b.Password); len(msgs) > 0 {
		fe["password"] = msgs
	}
	if len(fe) > 0 {
		return apperror.Validation("Validation failed", fe)
	}
	return nil
}

func (b loginBody) validate() *apperror.Error {
	fe := map[string][]string{}
	if b.Email == "" {
		fe["email"] = []string{"Required"}
	} else if !emailRe.MatchString(b.Email) {
		fe["email"] = []string{"Invalid email"}
	}
	if b.Password == "" {
		fe["password"] = []string{"Required"}
	}
	if len(fe) > 0 {
		return apperror.Validation("Validation failed", fe)
	}
	return nil
}

func (b refreshBody) validate() *apperror.Error {
	if b.RefreshToken == "" {
		return apperror.Validation("Validation failed", map[string][]string{"refresh_token": {"Required"}})
	}
	return nil
}

func (b logoutBody) validate() *apperror.Error {
	if b.RefreshToken == "" {
		return apperror.Validation("Validation failed", map[string][]string{"refresh_token": {"Required"}})
	}
	return nil
}
