package uploads

import (
	"strings"
	"testing"
)

func TestObjectKeyUsesExtensionNotFilename(t *testing.T) {
	key := objectKey("My Secret Report.PDF")
	if strings.Contains(key, "Secret") || strings.Contains(key, " ") {
		t.Fatalf("key leaked client filename: %s", key)
	}
	if !strings.HasPrefix(key, "f_") || !strings.HasSuffix(key, ".pdf") {
		t.Fatalf("key shape wrong: %s", key)
	}
}

func TestObjectKeyNoExtension(t *testing.T) {
	key := objectKey("noext")
	if !strings.HasPrefix(key, "f_") {
		t.Fatalf("key shape wrong: %s", key)
	}
	if strings.Contains(key, ".") {
		t.Fatalf("no-ext file should have no dot: %s", key)
	}
}

func TestClampLimit(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", defaultLimit},
		{"abc", defaultLimit},
		{"0", defaultLimit},
		{"-5", defaultLimit},
		{"10", 10},
		{"50", 50},
		{"999", maxLimit},
	}
	for _, tc := range cases {
		if got := clampLimit(tc.in); got != tc.want {
			t.Errorf("clampLimit(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestPresignBodyValidate(t *testing.T) {
	if (presignBody{Filename: "a.png", ContentType: "image/png", Size: 1}).validate() != nil {
		t.Error("valid body rejected")
	}
	if (presignBody{}).validate() == nil {
		t.Error("empty body should fail")
	}
	if (presignBody{Filename: "a.png", ContentType: "image/png", Size: 0}).validate() == nil {
		t.Error("size 0 should fail")
	}
}
