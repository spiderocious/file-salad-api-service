// Package storage wraps an S3-compatible object store (Tigris, R2, MinIO, S3)
// for presigned uploads and downloads. It is provider-neutral: the endpoint,
// region, and addressing style come from config, so switching providers is an
// env change, not a code change.
//
// Presigning is a local HMAC computation — no network call — so the only caching
// that earns its keep is reusing a download URL for the same key within its
// validity window (URL stability + CDN/dedupe friendliness), which we do in
// Redis (Level 1). Upload URLs are per-object and never cached.
package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	goredis "github.com/redis/go-redis/v9"

	"github.com/feranmi/file-salad-backend/internal/redis"
)

type Config struct {
	Endpoint        string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
	// UsePathStyle forces path-style addressing (bucket in the path). Default
	// false → virtual-hosted (bucket in the host), which Tigris and modern S3
	// expect. Some R2/MinIO setups want true.
	UsePathStyle bool
	UploadTTL    time.Duration
	DownloadTTL  time.Duration
}

// Storage presigns PUT/GET URLs against the bucket and caches download URLs.
type Storage struct {
	client  *s3.Client
	presign *s3.PresignClient
	cfg     Config
	rdb     *redis.Client
}

// New builds an S3 client pointed at the configured endpoint and a presigner.
func New(cfg Config, rdb *redis.Client) *Storage {
	region := cfg.Region
	if region == "" {
		region = "auto"
	}
	client := s3.New(s3.Options{
		Region:       region,
		BaseEndpoint: aws.String(cfg.Endpoint),
		UsePathStyle: cfg.UsePathStyle,
		Credentials: credentials.NewStaticCredentialsProvider(
			cfg.AccessKeyID, cfg.SecretAccessKey, "",
		),
	})
	return &Storage{
		client:  client,
		presign: s3.NewPresignClient(client),
		cfg:     cfg,
		rdb:     rdb,
	}
}

// UploadTTLSeconds / DownloadTTLSeconds expose the configured TTLs in seconds
// for the expires_in response fields.
func (s *Storage) UploadTTLSeconds() int   { return int(s.cfg.UploadTTL.Seconds()) }
func (s *Storage) DownloadTTLSeconds() int { return int(s.cfg.DownloadTTL.Seconds()) }

// UploadExpiry / DownloadExpiry return the TTL both as relative seconds and as
// an absolute UTC timestamp (now + TTL, RFC3339). The absolute form lets a
// client that *stores* a URL decide "is it stale now?" with a simple now > at
// comparison, without having to record when it received the response.
func (s *Storage) UploadExpiry() (seconds int, at string) {
	return int(s.cfg.UploadTTL.Seconds()), time.Now().UTC().Add(s.cfg.UploadTTL).Format(time.RFC3339)
}

func (s *Storage) DownloadExpiry() (seconds int, at string) {
	return int(s.cfg.DownloadTTL.Seconds()), time.Now().UTC().Add(s.cfg.DownloadTTL).Format(time.RFC3339)
}

// PresignUpload returns a presigned PUT URL for a fresh object key. The
// content-length-range condition isn't expressible on a plain PUT presign; we
// enforce the per-file limit at issuance (size is supplied) and rely on the
// client honouring it. (A POST-policy presign would let the bucket reject
// oversized PUTs; PUT presign trades that for simplicity — see api-docs.)
func (s *Storage) PresignUpload(ctx context.Context, key, contentType string) (string, error) {
	req, err := s.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.cfg.Bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}, s3.WithPresignExpires(s.cfg.UploadTTL))
	if err != nil {
		return "", fmt.Errorf("presign put: %w", err)
	}
	return req.URL, nil
}

// PresignDownload returns a presigned GET URL for a key, caching it in Redis
// (Level 1) so repeated requests within the window get the same URL. cached
// reports whether the URL came from the cache. Use this for the download
// endpoint, where the same key is fetched repeatedly.
func (s *Storage) PresignDownload(ctx context.Context, key string) (url string, cached bool, err error) {
	cacheKey := "dluri:" + key

	if s.rdb != nil {
		if v, gerr := s.rdb.Get(ctx, cacheKey).Result(); gerr == nil && v != "" {
			return v, true, nil
		} else if gerr != nil && gerr != goredis.Nil {
			// Cache read failure is non-fatal — fall through to a fresh presign.
			_ = gerr
		}
	}

	signed, err := s.presignGet(ctx, key)
	if err != nil {
		return "", false, err
	}

	if s.rdb != nil {
		// Expire the cache entry slightly before the signature so we never hand
		// out a near-dead URL.
		ttl := s.cfg.DownloadTTL - time.Minute
		if ttl <= 0 {
			ttl = s.cfg.DownloadTTL
		}
		_ = s.rdb.Set(ctx, cacheKey, signed, ttl).Err()
	}

	return signed, false, nil
}

// PresignDownloadURL returns a presigned GET URL without touching the cache.
// Used for the public_url returned at upload time — that's a one-off value, not
// a repeated lookup, so it shouldn't pollute (or read from) the download cache.
func (s *Storage) PresignDownloadURL(ctx context.Context, key string) (string, error) {
	return s.presignGet(ctx, key)
}

// presignGet is the raw presigned-GET computation (local HMAC, no cache).
func (s *Storage) presignGet(ctx context.Context, key string) (string, error) {
	req, err := s.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.cfg.Bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(s.cfg.DownloadTTL))
	if err != nil {
		return "", fmt.Errorf("presign get: %w", err)
	}
	return req.URL, nil
}
