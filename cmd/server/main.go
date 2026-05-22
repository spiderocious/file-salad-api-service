// Command server is the entry point: load env, init logging, connect Mongo +
// Redis, wire features, build the HTTP app, listen, and shut down gracefully on
// SIGTERM/SIGINT.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/feranmi/file-salad-backend/internal/app"
	"github.com/feranmi/file-salad-backend/internal/db"
	"github.com/feranmi/file-salad-backend/internal/env"
	"github.com/feranmi/file-salad-backend/internal/features/auth"
	"github.com/feranmi/file-salad-backend/internal/features/share"
	"github.com/feranmi/file-salad-backend/internal/features/uploads"
	"github.com/feranmi/file-salad-backend/internal/features/webuploads"
	"github.com/feranmi/file-salad-backend/internal/logger"
	"github.com/feranmi/file-salad-backend/internal/quota"
	appredis "github.com/feranmi/file-salad-backend/internal/redis"
	"github.com/feranmi/file-salad-backend/internal/security"
	"github.com/feranmi/file-salad-backend/internal/session"
	"github.com/feranmi/file-salad-backend/internal/sharecode"
	"github.com/feranmi/file-salad-backend/internal/stats"
	"github.com/feranmi/file-salad-backend/internal/storage"
)

func main() {
	// Load .env if present, so `go run ./cmd/server` works locally without
	// exporting vars by hand. No-ops when there's no file (e.g. production).
	_ = godotenv.Load()

	cfg, err := env.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	logger.Init(cfg.LogLevel, cfg.NodeEnv)
	ctx := context.Background()

	// --- MongoDB. Fatal in prod; warn-and-continue in dev. ---
	var database *mongo.Database
	mongoConn, err := db.Connect(ctx, cfg.MongoURI, cfg.MongoDB)
	if err != nil {
		if cfg.IsProduction() {
			logger.Error(ctx, "mongo connection failed", "err", err.Error())
			os.Exit(1)
		}
		logger.Warn(ctx, "mongo connection failed — continuing without DB (dev only)", "err", err.Error())
	} else {
		database = mongoConn.DB
		logger.Info(ctx, "mongo connected", "db", cfg.MongoDB)
	}

	// --- Redis. Fatal in prod; warn-and-continue in dev. ---
	var redisClient *appredis.Client
	rc, err := appredis.Connect(ctx, cfg.RedisURL)
	if err != nil {
		if cfg.IsProduction() {
			logger.Error(ctx, "redis connection failed", "err", err.Error())
			os.Exit(1)
		}
		logger.Warn(ctx, "redis connection failed — auth/sessions disabled (dev only)", "err", err.Error())
	} else {
		redisClient = rc
		logger.Info(ctx, "redis connected")
	}

	deps := buildDeps(ctx, cfg, database, redisClient)
	engine := app.Build(cfg, deps)

	srv := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.Port),
		Handler:           engine,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info(ctx, "file-salad-backend listening", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error(ctx, "listen failed", "err", err.Error())
			os.Exit(1)
		}
	}()

	// Block until a termination signal arrives.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	sig := <-stop

	logger.Info(ctx, "shutting down gracefully", "signal", sig.String())
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "http shutdown error", "err", err.Error())
	}
	if mongoConn != nil {
		if err := mongoConn.Disconnect(shutdownCtx); err != nil {
			logger.Error(ctx, "mongo disconnect error", "err", err.Error())
		}
	}
	if redisClient != nil {
		if err := redisClient.Close(); err != nil {
			logger.Error(ctx, "redis close error", "err", err.Error())
		}
	}
}

// buildDeps wires features from the live connections. A feature is only mounted
// when its dependencies are available: auth needs Mongo + Redis; uploads/web
// also need object storage configured. Missing deps in dev simply skip that feature.
func buildDeps(ctx context.Context, cfg *env.Env, database *mongo.Database, rdb *appredis.Client) app.Deps {
	deps := app.Deps{}

	jwt := security.NewJWTSigner(cfg.JWTAccessSecret, mustParseDuration(cfg.JWTAccessExpiresIn, 15*time.Minute))

	// Auth (Mongo + Redis).
	if database != nil && rdb != nil {
		refreshTTL := mustParseDuration(cfg.JWTRefreshExpiresIn, 30*24*time.Hour)
		userRepo := auth.NewRepo(database)
		if err := userRepo.EnsureIndexes(ctx); err != nil {
			logger.Warn(ctx, "auth EnsureIndexes failed", "err", err.Error())
		}
		sessions := session.NewStore(rdb, refreshTTL)
		deps.Auth = auth.Deps{Service: auth.NewService(userRepo, sessions, jwt), JWT: jwt}
	} else {
		logger.Warn(ctx, "auth feature not mounted (needs Mongo + Redis)")
	}

	// Uploads + web uploads (Mongo + storage; Redis optional for download cache).
	if database != nil && cfg.StorageConfigured() {
		uploadRepo := uploads.NewRepo(database)
		if err := uploadRepo.EnsureIndexes(ctx); err != nil {
			logger.Warn(ctx, "uploads EnsureIndexes failed", "err", err.Error())
		}
		store := storage.New(storage.Config{
			Endpoint:        cfg.StorageEndpoint,
			Region:          cfg.StorageRegion,
			AccessKeyID:     cfg.StorageAccessKeyID,
			SecretAccessKey: cfg.StorageSecretAccessKey,
			Bucket:          cfg.StorageBucket,
			UsePathStyle:    cfg.StorageUsePathStyle,
			UploadTTL:       cfg.UploadURLTTL,
			DownloadTTL:     cfg.DownloadURLTTL,
		}, rdb)
		counter := quota.NewCounter(database, cfg.MonthlyUploadCap)
		statsCounter := stats.NewCounter(database)

		uploadSvc := uploads.NewService(uploadRepo, store, counter, cfg.MaxFileSizeBytes, cfg.HostedLinkExpiryDays)
		deps.Uploads = &uploads.Deps{Service: uploadSvc, JWT: jwt, Counter: statsCounter}

		deps.WebUploads = &webuploads.Deps{
			Repo: uploadRepo, Store: store, Quota: counter, Stats: statsCounter,
			MaxFileSize: cfg.MaxFileSizeBytes, LinkDays: cfg.HostedLinkExpiryDays,
		}

		// Share codes need Redis (code store + redeem rate limiter).
		if rdb != nil {
			codes := sharecode.NewStore(rdb, cfg.ShareCodeTTL)
			deps.Share = &share.Deps{Uploads: uploadRepo, Codes: codes, Store: store, Redis: rdb}
		} else {
			logger.Warn(ctx, "share feature not mounted (needs Redis)")
		}
	} else {
		logger.Warn(ctx, "upload features not mounted (need Mongo + storage config)")
	}

	return deps
}

func mustParseDuration(s string, fallback time.Duration) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}
