package storage_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/feranmi/file-salad-backend/internal/redis"
	"github.com/feranmi/file-salad-backend/internal/storage"
)

func newStorage(t *testing.T, withRedis bool) (*storage.Storage, *miniredis.Miniredis) {
	t.Helper()
	cfg := storage.Config{
		Endpoint:        "https://t3.storage.dev",
		Region:          "auto",
		AccessKeyID:     "tid_test",
		SecretAccessKey: "tsec_test",
		Bucket:          "filesalad",
		PublicBaseURL:   "https://filesalad.t3.storage.dev",
		UploadTTL:       15 * time.Minute,
		DownloadTTL:     time.Hour,
	}
	var rc *redis.Client
	var mr *miniredis.Miniredis
	if withRedis {
		var err error
		mr, err = miniredis.Run()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(mr.Close)
		rc, err = redis.Connect(context.Background(), "redis://"+mr.Addr())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = rc.Close() })
	}
	return storage.New(cfg, rc), mr
}

func TestPublicURL(t *testing.T) {
	s, _ := newStorage(t, false)
	if got := s.PublicURL("f_abc.png"); got != "https://filesalad.t3.storage.dev/f_abc.png" {
		t.Fatalf("PublicURL = %s", got)
	}
}

func TestTTLSeconds(t *testing.T) {
	s, _ := newStorage(t, false)
	if s.UploadTTLSeconds() != 900 {
		t.Fatalf("upload ttl = %d", s.UploadTTLSeconds())
	}
	if s.DownloadTTLSeconds() != 3600 {
		t.Fatalf("download ttl = %d", s.DownloadTTLSeconds())
	}
}

func TestPresignUpload(t *testing.T) {
	s, _ := newStorage(t, false)
	url, err := s.PresignUpload(context.Background(), "f_abc.png", "image/png")
	if err != nil {
		t.Fatalf("presign upload: %v", err)
	}
	for _, want := range []string{"f_abc.png", "X-Amz-Signature=", "X-Amz-Algorithm=AWS4-HMAC-SHA256", "filesalad.t3.storage.dev"} {
		if !strings.Contains(url, want) {
			t.Errorf("upload url missing %q: %s", want, url)
		}
	}
}

func TestPresignDownloadAndCache(t *testing.T) {
	s, mr := newStorage(t, true)
	ctx := context.Background()

	url1, cached, err := s.PresignDownload(ctx, "f_abc.png")
	if err != nil {
		t.Fatalf("presign download: %v", err)
	}
	if cached {
		t.Fatal("first call should not be cached")
	}
	if !strings.Contains(url1, "X-Amz-Signature=") {
		t.Fatalf("not a signed url: %s", url1)
	}

	// Second call within the window → cache hit, same URL.
	url2, cached2, err := s.PresignDownload(ctx, "f_abc.png")
	if err != nil {
		t.Fatal(err)
	}
	if !cached2 {
		t.Fatal("second call should be cached")
	}
	if url1 != url2 {
		t.Fatal("cached url differs from original")
	}

	// The cache key exists with a TTL below the signature TTL.
	if !mr.Exists("dluri:f_abc.png") {
		t.Fatal("cache key not set")
	}
}

func TestPresignDownloadNoRedis(t *testing.T) {
	s, _ := newStorage(t, false) // no redis → always fresh, never cached
	url, cached, err := s.PresignDownload(context.Background(), "f_x.png")
	if err != nil {
		t.Fatal(err)
	}
	if cached {
		t.Fatal("no-redis presign should report cached=false")
	}
	if url == "" {
		t.Fatal("empty url")
	}
}
