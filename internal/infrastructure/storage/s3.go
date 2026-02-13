package storage

import (
	"context"
	"fmt"
	"time"

	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/mr-kaynak/go-core/internal/core/config"
)

// S3Storage implements StorageService using S3-compatible storage (MinIO).
type S3Storage struct {
	client        *minio.Client
	presignClient *minio.Client
	bucket        string
	presignTTL    time.Duration
}

// NewS3Storage creates a new S3Storage instance and ensures the bucket exists.
func NewS3Storage(cfg *config.Config) (*S3Storage, error) {
	client, err := minio.New(cfg.Storage.S3Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.Storage.S3AccessKey, cfg.Storage.S3SecretKey, ""),
		Region: cfg.Storage.S3Region,
		Secure: cfg.Storage.S3UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 client: %w", err)
	}

	ctx := context.Background()
	exists, err := client.BucketExists(ctx, cfg.Storage.S3Bucket)
	if err != nil {
		return nil, fmt.Errorf("failed to check bucket: %w", err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, cfg.Storage.S3Bucket, minio.MakeBucketOptions{
			Region: cfg.Storage.S3Region,
		}); err != nil {
			return nil, fmt.Errorf("failed to create bucket: %w", err)
		}
	}

	// Create a separate client for presigned URLs if a public endpoint is configured.
	// This allows the internal client to talk to MinIO via Docker network (e.g. minio:9000)
	// while presigned URLs use the externally reachable address (e.g. localhost:9000).
	presignClient := client
	if cfg.Storage.S3PublicEndpoint != "" && cfg.Storage.S3PublicEndpoint != cfg.Storage.S3Endpoint {
		pc, err := minio.New(cfg.Storage.S3PublicEndpoint, &minio.Options{
			Creds:  credentials.NewStaticV4(cfg.Storage.S3AccessKey, cfg.Storage.S3SecretKey, ""),
			Region: cfg.Storage.S3Region,
			Secure: cfg.Storage.S3UseSSL,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create presign S3 client: %w", err)
		}
		presignClient = pc
	}

	ttl := cfg.Storage.S3PresignTTL
	if ttl == 0 {
		ttl = 15 * time.Minute
	}

	return &S3Storage{
		client:        client,
		presignClient: presignClient,
		bucket:        cfg.Storage.S3Bucket,
		presignTTL:    ttl,
	}, nil
}

// Upload puts an object into S3-compatible storage.
func (s *S3Storage) Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) (*FileInfo, error) {
	info, err := s.client.PutObject(ctx, s.bucket, key, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to upload to S3: %w", err)
	}

	url, err := s.GetURL(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("failed to generate presigned URL: %w", err)
	}

	return &FileInfo{
		Key:         key,
		Size:        info.Size,
		ContentType: contentType,
		URL:         url,
	}, nil
}

// Delete removes an object from S3-compatible storage.
func (s *S3Storage) Delete(ctx context.Context, key string) error {
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("failed to delete from S3: %w", err)
	}
	return nil
}

// GetURL returns a pre-signed URL for the given object key.
// Uses the public endpoint client so URLs are reachable from outside Docker.
func (s *S3Storage) GetURL(ctx context.Context, key string) (string, error) {
	u, err := s.presignClient.PresignedGetObject(ctx, s.bucket, key, s.presignTTL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}
	return u.String(), nil
}
