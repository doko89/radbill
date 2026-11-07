package application

import (
	"context"
	"errors"
	"strings"

	"boilerplate/api/internal/shared/apperr"
)

// MaxAvatarBytes caps uploaded avatars at 512 KB.
const MaxAvatarBytes = 512 * 1024

// ErrAvatarNotFound is returned when a stored avatar is missing.
var ErrAvatarNotFound = errors.New("avatar not found")

// AvatarStorage persists avatar bytes behind STORAGE_DRIVER (local disk or
// S3-compatible). URLs stay /uploads/... either way: the server proxies S3
// objects so buckets can remain private.
type AvatarStorage interface {
	// Save stores data and returns the public URL path (/uploads/avatar/<uuid>.webp).
	Save(ctx context.Context, data []byte) (string, error)
	// Delete removes a previously saved URL. Non-local URLs are a no-op.
	Delete(ctx context.Context, url string) error
	// Load returns bytes for a validated (assetType, file) pair.
	Load(ctx context.Context, assetType, file string) ([]byte, error)
}

// AvatarDTO is the upload response.
type AvatarDTO struct {
	URL string `json:"url"`
}

// SetAvatar validates, stores and links a new avatar, replacing the old one.
// The previous local avatar file is removed best-effort.
func (s *Service) SetAvatar(ctx context.Context, store AvatarStorage, userID string, data []byte) (AvatarDTO, error) {
	if len(data) == 0 {
		return AvatarDTO{}, apperr.Invalid("file", "required")
	}
	if len(data) > MaxAvatarBytes {
		return AvatarDTO{}, apperr.Invalid("file", "max 512 KB")
	}
	if !IsWebP(data) {
		return AvatarDTO{}, apperr.Invalid("file", "must be a webp image")
	}
	u, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return AvatarDTO{}, err
	}
	url, err := store.Save(ctx, data)
	if err != nil {
		return AvatarDTO{}, err
	}
	if err := s.users.SetAvatarURL(ctx, userID, url); err != nil {
		_ = store.Delete(ctx, url)
		return AvatarDTO{}, err
	}
	if u.AvatarURL != "" {
		_ = store.Delete(ctx, u.AvatarURL)
	}
	return AvatarDTO{URL: url}, nil
}

// IsWebP checks the RIFF/WEBP magic bytes. The client converts to webp, but
// the server never trusts that claim.
func IsWebP(data []byte) bool {
	return len(data) >= 12 &&
		data[0] == 'R' && data[1] == 'I' && data[2] == 'F' && data[3] == 'F' &&
		data[8] == 'W' && data[9] == 'E' && data[10] == 'B' && data[11] == 'P'
}

// ValidAvatarPath reports whether an /uploads/<type>/<file> pair is servable.
// Only avatar/<32-hex>.webp exists today; extend the type switch for more.
func ValidAvatarPath(assetType, file string) bool {
	if assetType != "avatar" || len(file) != 37 || !strings.HasSuffix(file, ".webp") {
		return false
	}
	for _, r := range file[:32] {
		isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
		if !isHex {
			return false
		}
	}
	return true
}
