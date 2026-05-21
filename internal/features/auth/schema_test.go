package auth

import "testing"

func TestValidatePassword(t *testing.T) {
	cases := []struct {
		name string
		pw   string
		ok   bool
	}{
		{"valid", "Password123", true},
		{"too short", "Ab1", false},
		{"no upper", "password123", false},
		{"no lower", "PASSWORD123", false},
		{"no digit", "PasswordOnly", false},
		{"exactly 8 valid", "Abcdef1g", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := len(validatePassword(tc.pw)) == 0
			if got != tc.ok {
				t.Errorf("validatePassword(%q) ok=%v, want %v", tc.pw, got, tc.ok)
			}
		})
	}
}

func TestRegisterBodyValidate(t *testing.T) {
	cases := []struct {
		name  string
		body  registerBody
		valid bool
	}{
		{"valid", registerBody{Email: "a@b.cc", Password: "Password123"}, true},
		{"missing email", registerBody{Password: "Password123"}, false},
		{"bad email", registerBody{Email: "nope", Password: "Password123"}, false},
		{"missing password", registerBody{Email: "a@b.cc"}, false},
		{"weak password", registerBody{Email: "a@b.cc", Password: "short"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.body.validate()
			if (err == nil) != tc.valid {
				t.Errorf("validate() err=%v, want valid=%v", err, tc.valid)
			}
		})
	}
}

func TestLoginBodyValidate(t *testing.T) {
	if (loginBody{Email: "a@b.cc", Password: "x"}).validate() != nil {
		t.Error("login only checks presence + email format, not policy")
	}
	if (loginBody{Email: "bad", Password: "x"}).validate() == nil {
		t.Error("bad email should fail")
	}
	if (loginBody{}).validate() == nil {
		t.Error("empty should fail")
	}
}

func TestRefreshLogoutBodyValidate(t *testing.T) {
	if (refreshBody{RefreshToken: "x"}).validate() != nil {
		t.Error("non-empty refresh should pass")
	}
	if (refreshBody{}).validate() == nil {
		t.Error("empty refresh should fail")
	}
	if (logoutBody{RefreshToken: "x"}).validate() != nil {
		t.Error("non-empty logout should pass")
	}
	if (logoutBody{}).validate() == nil {
		t.Error("empty logout should fail")
	}
}

func TestUserPublicHidesHash(t *testing.T) {
	u := User{ID: "u_1", Email: "a@b.cc", PasswordHash: "secret"}
	pub := u.Public()
	if pub.ID != "u_1" || pub.Email != "a@b.cc" {
		t.Fatal("public user fields wrong")
	}
	// PublicUser has no PasswordHash field at all (compile-time guarantee);
	// this asserts the conversion doesn't carry it.
}
