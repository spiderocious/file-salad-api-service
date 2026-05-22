package uploads

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/feranmi/file-salad-backend/internal/apperror"
	"github.com/feranmi/file-salad-backend/internal/ids"
	"github.com/feranmi/file-salad-backend/internal/quota"
	"github.com/feranmi/file-salad-backend/internal/storage"
)

// Service holds hosted-upload business logic. Returns (T, *apperror.Error).
type Service struct {
	repo        *Repo
	store       *storage.Storage
	quota       *quota.Counter
	maxFileSize int64
	linkDays    int
}

func NewService(repo *Repo, store *storage.Storage, q *quota.Counter, maxFileSize int64, linkDays int) *Service {
	return &Service{repo: repo, store: store, quota: q, maxFileSize: maxFileSize, linkDays: linkDays}
}

func internalErr(err error) *apperror.Error {
	return &apperror.Error{Code: "internal", Status: 500, Message: "An unexpected error occurred"}
}

// scope is the quota key for a hosted user.
func userScope(userID string) string { return "user:" + userID }

// Presign checks the per-file size limit and the monthly cap, reserves a quota
// slot, persists a pending upload, and returns a presigned PUT plus the public
// URL. Returns the authoritative used/limit alongside.
func (s *Service) Presign(ctx context.Context, userID, filename, contentType string, size int64) (*PresignResponse, int, int, *apperror.Error) {
	if size <= 0 {
		return nil, 0, 0, apperror.Validation("Validation failed", map[string][]string{"size": {"Must be greater than zero"}})
	}
	if size > s.maxFileSize {
		return nil, 0, 0, apperror.FileTooLarge(fmt.Sprintf("File exceeds the %d byte limit", s.maxFileSize))
	}

	scope := userScope(userID)
	used, ok, err := s.quota.Reserve(ctx, scope)
	if err != nil {
		return nil, 0, 0, internalErr(err)
	}
	if !ok {
		return nil, used, s.quota.Cap(), apperror.QuotaExceeded(
			"Monthly upload limit reached — connect your own bucket for unlimited uploads",
		)
	}

	key := objectKey(filename)
	uploadURL, err := s.store.PresignUpload(ctx, key, contentType)
	if err != nil {
		_ = s.quota.Release(ctx, scope) // compensate the reservation
		return nil, 0, 0, internalErr(err)
	}

	// public_url is a presigned GET — always resolves (even on a private bucket)
	// and expires with DOWNLOAD_URL_TTL. The client can re-fetch a fresh one any
	// time via the download endpoint; the durable identifier is the key. Uses the
	// non-caching presign so it doesn't pre-warm the download cache.
	publicURL, err := s.store.PresignDownloadURL(ctx, key)
	if err != nil {
		_ = s.quota.Release(ctx, scope)
		return nil, 0, 0, internalErr(err)
	}

	u := &Upload{
		ID:          ids.Prefixed("up"),
		OwnerID:     userID,
		Surface:     SurfaceHosted,
		Key:         key,
		Filename:    filename,
		ContentType: contentType,
		Size:        size,
		Status:      StatusPending,
		ExpiresAt:   nowExpiry(s.linkDays),
		CreatedAt:   nowUTC(),
	}
	if err := s.repo.Insert(ctx, u); err != nil {
		_ = s.quota.Release(ctx, scope)
		return nil, 0, 0, internalErr(err)
	}

	upSecs, upAt := s.store.UploadExpiry()
	pubSecs, pubAt := s.store.DownloadExpiry()
	return &PresignResponse{
		UploadID:           u.ID,
		Key:                u.Key,
		UploadURL:          uploadURL,
		UploadURLExpiresIn: upSecs,
		UploadURLExpiresAt: upAt,
		PublicURL:          publicURL,
		PublicURLExpiresIn: pubSecs,
		PublicURLExpiresAt: pubAt,
		ExpiresIn:          upSecs, // deprecated alias
	}, used, s.quota.Cap(), nil
}

// Complete flips a pending upload to ready.
func (s *Service) Complete(ctx context.Context, userID, uploadID string) (*Upload, *apperror.Error) {
	u, err := s.repo.MarkReady(ctx, uploadID, userID)
	if err != nil {
		if err == ErrNotFound {
			return nil, apperror.NotFound("Upload")
		}
		return nil, internalErr(err)
	}
	return u, nil
}

// List returns a page of the user's uploads plus the next cursor and has_more.
func (s *Service) List(ctx context.Context, userID, cursor string, limit int) ([]Upload, string, bool, *apperror.Error) {
	rows, err := s.repo.ListByOwner(ctx, userID, cursor, limit)
	if err != nil {
		return nil, "", false, internalErr(err)
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	next := ""
	if hasMore && len(rows) > 0 {
		next = rows[len(rows)-1].ID
	}
	return rows, next, hasMore, nil
}

// Download resolves a presigned GET URL for the user's upload, with both the
// relative TTL and the absolute expiry timestamp.
func (s *Service) Download(ctx context.Context, userID, uploadID string) (url string, cached bool, expiresIn int, expiresAt string, aerr *apperror.Error) {
	u, err := s.repo.FindByIDOwner(ctx, uploadID, userID)
	if err != nil {
		if err == ErrNotFound {
			return "", false, 0, "", apperror.NotFound("Upload")
		}
		return "", false, 0, "", internalErr(err)
	}
	signed, cached, err := s.store.PresignDownload(ctx, u.Key)
	if err != nil {
		return "", false, 0, "", internalErr(err)
	}
	secs, at := s.store.DownloadExpiry()
	return signed, cached, secs, at, nil
}

// objectKey builds a server-controlled key: {ulid}{ext}. Never trusts the
// client filename for the stored key (only its extension).
func objectKey(filename string) string {
	ext := strings.ToLower(path.Ext(filename))
	return ids.Prefixed("f") + ext
}
