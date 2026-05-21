package webuploads

import (
	"strings"
	"testing"
)

func TestWebScope(t *testing.T) {
	got := webScope("1.2.3.4", "fp_abc")
	if got != "web:1.2.3.4:fp_abc" {
		t.Fatalf("scope = %q", got)
	}
}

func TestObjectKey(t *testing.T) {
	key := objectKey("photo.JPEG")
	if !strings.HasPrefix(key, "f_") || !strings.HasSuffix(key, ".jpeg") {
		t.Fatalf("key shape wrong: %s", key)
	}
}

func TestPresignBodyValidate(t *testing.T) {
	if (presignBody{Filename: "a.png", ContentType: "image/png", Size: 1}).validate() != nil {
		t.Error("valid body rejected")
	}
	for _, b := range []presignBody{
		{},
		{Filename: "a.png", ContentType: "image/png", Size: 0},
		{ContentType: "image/png", Size: 1},
		{Filename: "a.png", Size: 1},
	} {
		if b.validate() == nil {
			t.Errorf("invalid body %+v passed", b)
		}
	}
}
