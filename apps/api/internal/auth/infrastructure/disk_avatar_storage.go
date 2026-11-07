package infrastructure

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	authapp "boilerplate/api/internal/auth/application"
	"boilerplate/api/internal/shared/id"
)

// DiskAvatarStorage implements application.AvatarStorage on the filesystem
// at <uploadDir>/avatar/<uuid>.webp.
type DiskAvatarStorage struct {
	base string
}

// NewDiskAvatarStorage builds storage, creating the avatar directory.
func NewDiskAvatarStorage(uploadDir string) (*DiskAvatarStorage, error) {
	if err := os.MkdirAll(filepath.Join(uploadDir, "avatar"), 0o755); err != nil {
		return nil, err
	}
	return &DiskAvatarStorage{base: uploadDir}, nil
}

// Save writes data under a fresh uuid name and returns its public URL.
func (s *DiskAvatarStorage) Save(_ context.Context, data []byte) (string, error) {
	name := id.New() + ".webp"
	if err := os.WriteFile(filepath.Join(s.base, "avatar", name), data, 0o644); err != nil {
		return "", err
	}
	return "/uploads/avatar/" + name, nil
}

// Delete removes a local avatar URL. Anything else (remote URLs, other
// paths) is a no-op, so callers can pass through blindly.
func (s *DiskAvatarStorage) Delete(_ context.Context, url string) error {
	assetType, file, ok := strings.Cut(strings.TrimPrefix(url, "/uploads/"), "/")
	if !ok || !authapp.ValidAvatarPath(assetType, file) {
		return nil
	}
	if err := os.Remove(filepath.Join(s.base, assetType, file)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Load reads a validated avatar file.
func (s *DiskAvatarStorage) Load(_ context.Context, assetType, file string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(s.base, assetType, file))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, authapp.ErrAvatarNotFound
		}
		return nil, err
	}
	return data, nil
}
