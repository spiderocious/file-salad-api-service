package security

import (
	"testing"
	"time"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("Password123")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if hash == "Password123" {
		t.Fatal("hash equals plaintext")
	}

	ok, err := VerifyPassword("Password123", hash)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !ok {
		t.Fatal("correct password did not verify")
	}

	bad, err := VerifyPassword("WrongPassword1", hash)
	if err != nil {
		t.Fatalf("verify wrong: %v", err)
	}
	if bad {
		t.Fatal("wrong password verified")
	}
}

func TestVerifyPasswordRejectsGarbageHash(t *testing.T) {
	if _, err := VerifyPassword("x", "not-a-real-hash"); err == nil {
		t.Fatal("expected error on malformed hash")
	}
}

func TestRefreshTokenHashing(t *testing.T) {
	raw, hash, err := NewRefreshToken()
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	if raw == hash {
		t.Fatal("raw token equals its hash")
	}
	if HashToken(raw) != hash {
		t.Fatal("HashToken not stable for the same input")
	}
	// Different tokens hash differently.
	raw2, _, _ := NewRefreshToken()
	if HashToken(raw2) == hash {
		t.Fatal("distinct tokens produced the same hash")
	}
}

func TestJWTSignVerify(t *testing.T) {
	signer := NewJWTSigner("0123456789abcdef0123456789abcdef", 15*time.Minute)
	tok, err := signer.Sign("u_123")
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	sub, err := signer.Verify(tok)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if sub != "u_123" {
		t.Fatalf("subject = %q, want u_123", sub)
	}
}

func TestJWTExpired(t *testing.T) {
	signer := NewJWTSigner("0123456789abcdef0123456789abcdef", -time.Minute) // already expired
	tok, err := signer.Sign("u_123")
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	_, err = signer.Verify(tok)
	if err != ErrTokenExpired {
		t.Fatalf("err = %v, want ErrTokenExpired", err)
	}
}

func TestJWTWrongSecret(t *testing.T) {
	a := NewJWTSigner("0123456789abcdef0123456789abcdef", time.Minute)
	b := NewJWTSigner("ffffffffffffffffffffffffffffffff", time.Minute)
	tok, _ := a.Sign("u_123")
	if _, err := b.Verify(tok); err != ErrTokenInvalid {
		t.Fatalf("err = %v, want ErrTokenInvalid", err)
	}
}
