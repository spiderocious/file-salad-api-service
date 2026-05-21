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

func TestVerifyPasswordRejectsMalformedHashes(t *testing.T) {
	cases := []string{
		"not-a-real-hash",
		"$argon2id$v=19$m=65536,t=3,p=1$onlyfivefields",      // too few segments
		"$bcrypt$v=19$m=1,t=1,p=1$c2FsdA$aGFzaA",             // wrong algorithm
		"$argon2id$vXX$m=65536,t=3,p=1$c2FsdA$aGFzaA",        // bad version
		"$argon2id$v=19$bad-params$c2FsdA$aGFzaA",            // bad params
		"$argon2id$v=19$m=65536,t=3,p=1$!!!notbase64$aGFzaA", // bad salt b64
		"$argon2id$v=19$m=65536,t=3,p=1$c2FsdA$!!!notbase64", // bad hash b64
	}
	for _, h := range cases {
		if _, err := VerifyPassword("x", h); err == nil {
			t.Errorf("expected error on malformed hash: %q", h)
		}
	}
}

func TestHashProducesDistinctSalts(t *testing.T) {
	h1, _ := HashPassword("Password123")
	h2, _ := HashPassword("Password123")
	if h1 == h2 {
		t.Fatal("same password produced identical hashes — salt not random")
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
