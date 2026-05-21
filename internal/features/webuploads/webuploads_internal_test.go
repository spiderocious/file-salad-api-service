package webuploads

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

func TestNowHelpers(t *testing.T) {
	if nowUTC().Location() != time.UTC {
		t.Fatal("nowUTC not UTC")
	}
	exp := nowExpiry(1)
	if exp.Before(time.Now().UTC()) {
		t.Fatal("nowExpiry should be in the future")
	}
}
