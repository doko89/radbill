package infrastructure

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	authapp "boilerplate/api/internal/auth/application"
	"boilerplate/api/internal/shared/config"
	"boilerplate/api/internal/shared/id"
)

// S3AvatarStorage implements application.AvatarStorage on any S3-compatible
// backend (AWS S3, MinIO, R2, ...). Objects live at avatar/<uuid>.webp and
// are served back through /uploads/... by the API, so buckets stay private.
type S3AvatarStorage struct {
	client *minio.Client
	bucket string
}

// NewS3AvatarStorage connects and ensures the bucket exists (created when
// missing, so a fresh MinIO works with zero console clicks).
func NewS3AvatarStorage(ctx context.Context, cfg config.S3Config) (*S3AvatarStorage, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	exists, err := client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, err
	}
	if !exists {
		if err := client.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{Region: cfg.Region}); err != nil {
			var resp minio.ErrorResponse
			if !errors.As(err, &resp) || resp.Code != "BucketAlreadyOwnedByYou" {
				return nil, err
			}
		}
	}
	return &S3AvatarStorage{client: client, bucket: cfg.Bucket}, nil
}

// Save uploads data and returns its public /uploads/... URL.
func (s *S3AvatarStorage) Save(ctx context.Context, data []byte) (string, error) {
	name := id.New() + ".webp"
	_, err := s.client.PutObject(ctx, s.bucket, "avatar/"+name,
		bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: "image/webp"})
	if err != nil {
		return "", err
	}
	return "/uploads/avatar/" + name, nil
}

// Delete removes a local avatar URL. Anything else is a no-op.
func (s *S3AvatarStorage) Delete(ctx context.Context, url string) error {
	name, ok := strings.CutPrefix(url, "/uploads/avatar/")
	if !ok || !authapp.ValidAvatarPath("avatar", name) {
		return nil
	}
	return s.client.RemoveObject(ctx, s.bucket, "avatar/"+name, minio.RemoveObjectOptions{})
}

// Load fetches a validated avatar object.
func (s *S3AvatarStorage) Load(ctx context.Context, assetType, file string) ([]byte, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, assetType+"/"+file, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	if _, err := obj.Stat(); err != nil {
		var resp minio.ErrorResponse
		if errors.As(err, &resp) && (resp.Code == "NoSuchKey" || resp.Code == "NoSuchBucket") {
			return nil, authapp.ErrAvatarNotFound
		}
		return nil, err
	}
	return io.ReadAll(obj)
}
