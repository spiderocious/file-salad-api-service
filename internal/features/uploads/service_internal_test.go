package uploads

import (
	"errors"
	"testing"
	"time"
)

func TestInternalErr(t *testing.T) {
	e := internalErr(errors.New("boom"))
	if e.Code != "internal" || e.Status != 500 {
		t.Fatalf("internalErr = %+v", e)
	}
}

func TestUserScope(t *testing.T) {
	if userScope("u_1") != "user:u_1" {
		t.Fatal("userScope wrong")
	}
}

func TestNowExpiry(t *testing.T) {
	now := time.Now().UTC()
	exp := nowExpiry(90)
	if exp.Before(now.AddDate(0, 0, 89)) || exp.After(now.AddDate(0, 0, 91)) {
		t.Fatalf("nowExpiry(90) = %v, not ~90 days out", exp)
	}
	if nowUTC().Location() != time.UTC {
		t.Fatal("nowUTC not UTC")
	}
}
