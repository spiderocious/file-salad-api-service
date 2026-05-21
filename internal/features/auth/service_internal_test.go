package auth

import (
	"errors"
	"testing"
)

func TestInternalErr(t *testing.T) {
	e := internalErr(errors.New("db down"))
	if e.Code != "internal" || e.Status != 500 {
		t.Fatalf("internalErr = %+v", e)
	}
	if e.Message == "" {
		t.Fatal("internalErr message empty")
	}
}

func TestTouchSetsTimestamps(t *testing.T) {
	u := &User{}
	touch(u)
	if u.CreatedAt.IsZero() || u.UpdatedAt.IsZero() {
		t.Fatal("touch did not set timestamps")
	}
	created := u.CreatedAt
	touch(u)
	if u.CreatedAt != created {
		t.Fatal("touch should not change CreatedAt on a second call")
	}
}
