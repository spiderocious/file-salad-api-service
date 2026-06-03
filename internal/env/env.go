// Package env loads and validates configuration once, at boot. The process
// refuses to start if anything required is missing or malformed — same
// fail-fast contract as the Node backend's env.ts (Zod). Mongo's URI is
// required; production-only requirements would be checked in main, not here,
// so dev can still boot.
package env

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Env struct {
	NodeEnv  string // development | test | production
	Port     int
	LogLevel string // trace | debug | info | warn | error

	JWTAccessSecret     string
	JWTRefreshSecret    string
	JWTAccessExpiresIn  string
	JWTRefreshExpiresIn string

	// CORS allow-origin for the web one-pager. "*" allows any origin.
	WebBaseURL string

	// MongoDB connection.
	MongoURI string
	MongoDB  string

	// Redis connection (refresh-token sessions + presign download cache).
	RedisURL string

	// S3-compatible object storage (Tigris, R2, MinIO, S3) for hosted/web
	// uploads. Optional in development so the server boots without storage
	// configured; required in production (enforced in main, not here).
	StorageEndpoint        string // S3 API base, e.g. https://t3.storage.dev
	StorageRegion          string // usually "auto" for Tigris/R2
	StorageAccessKeyID     string
	StorageSecretAccessKey string
	StorageBucket          string
	StorageUsePathStyle    bool // force path-style addressing (default virtual-hosted)

	// Upload tuning.
	UploadURLTTL         time.Duration // presigned PUT validity
	DownloadURLTTL       time.Duration // presigned GET validity
	MonthlyUploadCap     int           // hosted free tier + web cap (per month)
	MaxFileSizeBytes     int64         // per-file size limit
	HostedLinkExpiryDays int           // public link lifetime (default 90)

	// Share codes.
	ShareCodeTTL time.Duration // how long a shareable code stays valid (default 24h)

	// Feature flags surfaced to the frontend via GET /api/v1/features.
	FeatureShouldShowCodes   bool          // expose share-codes UI
	FeatureShouldSupportBYOK bool          // expose BYO-bucket UI
	FeatureFlagTTL           time.Duration // how long the FE should cache the flag response (default 5m)
}

const minSecretLen = 32

// Load reads from the process environment and validates. Returns a typed Env
// or an aggregated error listing every problem at once.
func Load() (*Env, error) {
	var problems []string

	nodeEnv := getDefault("NODE_ENV", "development")
	if !oneOf(nodeEnv, "development", "test", "production") {
		problems = append(problems, "NODE_ENV: must be development | test | production")
	}

	port := 8080
	if raw := os.Getenv("PORT"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			problems = append(problems, "PORT: must be a number")
		} else {
			port = n
		}
	}

	logLevel := getDefault("LOG_LEVEL", "info")
	if !oneOf(logLevel, "trace", "debug", "info", "warn", "error") {
		problems = append(problems, "LOG_LEVEL: must be trace | debug | info | warn | error")
	}

	accessSecret := os.Getenv("JWT_ACCESS_SECRET")
	if len(accessSecret) < minSecretLen {
		problems = append(problems, fmt.Sprintf("JWT_ACCESS_SECRET: must be at least %d characters", minSecretLen))
	}
	refreshSecret := os.Getenv("JWT_REFRESH_SECRET")
	if len(refreshSecret) < minSecretLen {
		problems = append(problems, fmt.Sprintf("JWT_REFRESH_SECRET: must be at least %d characters", minSecretLen))
	}

	webBaseURL := getDefault("WEB_BASE_URL", "*")

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		problems = append(problems, "MONGODB_URI: required")
	}
	mongoDB := getDefault("MONGODB_DB", "file_salad")

	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		problems = append(problems, "REDIS_URL: required")
	}

	uploadTTL, err := time.ParseDuration(getDefault("UPLOAD_URL_TTL", "15m"))
	if err != nil {
		problems = append(problems, "UPLOAD_URL_TTL: must be a Go duration (e.g. 15m)")
	}
	downloadTTL, err := time.ParseDuration(getDefault("DOWNLOAD_URL_TTL", "1h"))
	if err != nil {
		problems = append(problems, "DOWNLOAD_URL_TTL: must be a Go duration (e.g. 1h)")
	}
	shareCodeTTL, err := time.ParseDuration(getDefault("SHARE_CODE_TTL", "24h"))
	if err != nil {
		problems = append(problems, "SHARE_CODE_TTL: must be a Go duration (e.g. 24h)")
	}
	featureFlagTTL, err := time.ParseDuration(getDefault("FEATURE_FLAG_TTL", "5m"))
	if err != nil {
		problems = append(problems, "FEATURE_FLAG_TTL: must be a Go duration (e.g. 5m)")
	}

	monthlyCap := mustInt("MONTHLY_UPLOAD_CAP", 50, &problems)
	maxFileSize := mustInt64("MAX_FILE_SIZE_BYTES", 52428800, &problems) // 50 MB
	linkExpiryDays := mustInt("HOSTED_LINK_EXPIRY_DAYS", 90, &problems)

	if len(problems) > 0 {
		return nil, fmt.Errorf("invalid environment variables:\n  %s", strings.Join(problems, "\n  "))
	}

	return &Env{
		NodeEnv:                  nodeEnv,
		Port:                     port,
		LogLevel:                 logLevel,
		JWTAccessSecret:          accessSecret,
		JWTRefreshSecret:         refreshSecret,
		JWTAccessExpiresIn:       getDefault("JWT_ACCESS_EXPIRES_IN", "15m"),
		JWTRefreshExpiresIn:      getDefault("JWT_REFRESH_EXPIRES_IN", "30d"),
		WebBaseURL:               webBaseURL,
		MongoURI:                 mongoURI,
		MongoDB:                  mongoDB,
		RedisURL:                 redisURL,
		StorageEndpoint:          os.Getenv("STORAGE_ENDPOINT"),
		StorageRegion:            getDefault("STORAGE_REGION", "auto"),
		StorageAccessKeyID:       os.Getenv("STORAGE_ACCESS_KEY_ID"),
		StorageSecretAccessKey:   os.Getenv("STORAGE_SECRET_ACCESS_KEY"),
		StorageBucket:            os.Getenv("STORAGE_BUCKET"),
		StorageUsePathStyle:      os.Getenv("STORAGE_USE_PATH_STYLE") == "true",
		UploadURLTTL:             uploadTTL,
		DownloadURLTTL:           downloadTTL,
		MonthlyUploadCap:         monthlyCap,
		MaxFileSizeBytes:         maxFileSize,
		HostedLinkExpiryDays:     linkExpiryDays,
		ShareCodeTTL:             shareCodeTTL,
		FeatureShouldShowCodes:   os.Getenv("FEATURE_SHOULD_SHOW_CODES") == "true",
		FeatureShouldSupportBYOK: os.Getenv("FEATURE_SHOULD_SUPPORT_BYOK") == "true",
		FeatureFlagTTL:           featureFlagTTL,
	}, nil
}

// StorageConfigured reports whether the storage vars needed to presign are set.
func (e *Env) StorageConfigured() bool {
	return e.StorageEndpoint != "" && e.StorageAccessKeyID != "" &&
		e.StorageSecretAccessKey != "" && e.StorageBucket != ""
}

func (e *Env) IsProduction() bool { return e.NodeEnv == "production" }

func getDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func oneOf(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

func mustInt(key string, fallback int, problems *[]string) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		*problems = append(*problems, key+": must be an integer")
		return fallback
	}
	return n
}

func mustInt64(key string, fallback int64, problems *[]string) int64 {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		*problems = append(*problems, key+": must be an integer")
		return fallback
	}
	return n
}
