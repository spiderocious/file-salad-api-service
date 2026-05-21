package env

import "testing"

// validBase sets the minimum required env for a successful Load.
func validBase(t *testing.T) {
	t.Setenv("JWT_ACCESS_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("JWT_REFRESH_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("MONGODB_URI", "mongodb://localhost:27017")
	t.Setenv("REDIS_URL", "redis://localhost:6379")
}

func TestLoadDefaults(t *testing.T) {
	validBase(t)
	e, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if e.NodeEnv != "development" || e.Port != 8080 || e.LogLevel != "info" {
		t.Fatalf("defaults wrong: %+v", e)
	}
	if e.MongoDB != "file_salad" || e.WebBaseURL != "*" {
		t.Fatalf("string defaults wrong: %+v", e)
	}
	if e.MonthlyUploadCap != 50 || e.MaxFileSizeBytes != 52428800 || e.HostedLinkExpiryDays != 90 {
		t.Fatalf("numeric defaults wrong: %+v", e)
	}
	if e.UploadURLTTL.Minutes() != 15 || e.DownloadURLTTL.Hours() != 1 {
		t.Fatalf("ttl defaults wrong: %+v", e)
	}
	if e.StorageRegion != "auto" {
		t.Fatalf("region default = %q", e.StorageRegion)
	}
}

func TestLoadMissingSecrets(t *testing.T) {
	t.Setenv("MONGODB_URI", "mongodb://localhost:27017")
	t.Setenv("REDIS_URL", "redis://localhost:6379")
	// JWT secrets unset → must error.
	if _, err := Load(); err == nil {
		t.Fatal("expected error for missing JWT secrets")
	}
}

func TestLoadShortSecret(t *testing.T) {
	validBase(t)
	t.Setenv("JWT_ACCESS_SECRET", "tooshort")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for short secret")
	}
}

func TestLoadMissingMongoRedis(t *testing.T) {
	t.Setenv("JWT_ACCESS_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("JWT_REFRESH_SECRET", "0123456789abcdef0123456789abcdef")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for missing MONGODB_URI/REDIS_URL")
	}
}

func TestLoadInvalidEnums(t *testing.T) {
	validBase(t)
	t.Setenv("NODE_ENV", "staging")
	t.Setenv("LOG_LEVEL", "loud")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for invalid NODE_ENV/LOG_LEVEL")
	}
}

func TestLoadInvalidNumbers(t *testing.T) {
	validBase(t)
	t.Setenv("PORT", "notanumber")
	t.Setenv("MONTHLY_UPLOAD_CAP", "x")
	t.Setenv("MAX_FILE_SIZE_BYTES", "y")
	t.Setenv("UPLOAD_URL_TTL", "notaduration")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for invalid numbers/duration")
	}
}

func TestLoadCustomValues(t *testing.T) {
	validBase(t)
	t.Setenv("NODE_ENV", "production")
	t.Setenv("PORT", "9000")
	t.Setenv("MONTHLY_UPLOAD_CAP", "10")
	t.Setenv("STORAGE_USE_PATH_STYLE", "true")
	e, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !e.IsProduction() {
		t.Fatal("IsProduction should be true")
	}
	if e.Port != 9000 || e.MonthlyUploadCap != 10 || !e.StorageUsePathStyle {
		t.Fatalf("custom values wrong: %+v", e)
	}
}

func TestStorageConfigured(t *testing.T) {
	validBase(t)
	e, _ := Load()
	if e.StorageConfigured() {
		t.Fatal("storage should be unconfigured without STORAGE_* vars")
	}

	t.Setenv("STORAGE_ENDPOINT", "https://t3.storage.dev")
	t.Setenv("STORAGE_ACCESS_KEY_ID", "k")
	t.Setenv("STORAGE_SECRET_ACCESS_KEY", "s")
	t.Setenv("STORAGE_BUCKET", "b")
	e, _ = Load()
	if !e.StorageConfigured() {
		t.Fatal("storage should be configured with all STORAGE_* vars")
	}
}
