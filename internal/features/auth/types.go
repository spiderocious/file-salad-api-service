package auth

import "time"

// User is the persisted account. PasswordHash is never serialized to JSON.
type User struct {
	ID           string    `bson:"_id" json:"id"`
	Email        string    `bson:"email" json:"email"`
	PasswordHash string    `bson:"password_hash" json:"-"`
	CreatedAt    time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt    time.Time `bson:"updated_at" json:"updated_at"`
}

// PublicUser is the user shape returned to clients (no hash).
type PublicUser struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

func (u *User) Public() PublicUser {
	return PublicUser{ID: u.ID, Email: u.Email, CreatedAt: u.CreatedAt}
}

// AuthResponse is returned by register/login.
type AuthResponse struct {
	User         PublicUser `json:"user"`
	AccessToken  string     `json:"access_token"`
	RefreshToken string     `json:"refresh_token"`
	ExpiresIn    int        `json:"expires_in"` // access-token lifetime, seconds
}

// TokenPair is returned by refresh.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}
