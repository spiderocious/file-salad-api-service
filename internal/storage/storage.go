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
	"strings"
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
	PublicBaseURL   string
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

// PublicURL returns the durable public URL for an object key (served by the
// bucket's public base / CDN, not by us).
func (s *Storage) PublicURL(key string) string {
	return strings.TrimRight(s.cfg.PublicBaseURL, "/") + "/" + key
}

// UploadTTLSeconds / DownloadTTLSeconds expose the configured TTLs in seconds
// for the expires_in response fields.
func (s *Storage) UploadTTLSeconds() int   { return int(s.cfg.UploadTTL.Seconds()) }
func (s *Storage) DownloadTTLSeconds() int { return int(s.cfg.DownloadTTL.Seconds()) }

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
// reports whether the URL came from the cache.
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

	req, err := s.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.cfg.Bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(s.cfg.DownloadTTL))
	if err != nil {
		return "", false, fmt.Errorf("presign get: %w", err)
	}

	if s.rdb != nil {
		// Expire the cache entry slightly before the signature so we never hand
		// out a near-dead URL.
		ttl := s.cfg.DownloadTTL - time.Minute
		if ttl <= 0 {
			ttl = s.cfg.DownloadTTL
		}
		_ = s.rdb.Set(ctx, cacheKey, req.URL, ttl).Err()
	}

	return req.URL, false, nil
}
