package storage

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/mr-kaynak/go-core/internal/core/config"
)

// parseEndpoint strips the scheme from an endpoint URL and returns
// the host and whether TLS should be used.
func parseEndpoint(endpoint string) (host string, secure bool) {
	if strings.HasPrefix(endpoint, "https://") {
		return strings.TrimPrefix(endpoint, "https://"), true
	}
	return strings.TrimPrefix(endpoint, "http://"), false
}

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
		if cfg.IsProduction() || cfg.IsStaging() {
			return nil, fmt.Errorf("bucket %q does not exist; auto-creation is disabled in %s",
				cfg.Storage.S3Bucket, cfg.App.Env)
		}
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
		publicHost, publicSecure := parseEndpoint(cfg.Storage.S3PublicEndpoint)
		pc, err := minio.New(publicHost, &minio.Options{
			Creds:  credentials.NewStaticV4(cfg.Storage.S3AccessKey, cfg.Storage.S3SecretKey, ""),
			Region: cfg.Storage.S3Region,
			Secure: publicSecure,
		})
		if err != nil || pc == nil {
			// Fall back to the internal client for presigned URLs
			presignClient = client
		} else {
			presignClient = pc
		}
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

// sanitizeKey validates an S3 object key, rejecting path traversal attempts.
func sanitizeKey(key string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("empty object key")
	}
	// Reject null bytes
	if strings.ContainsRune(key, 0) {
		return "", fmt.Errorf("object key contains null byte")
	}
	// Clean the path and reject traversal
	cleaned := path.Clean("/" + key)
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "" || cleaned == "." || strings.HasPrefix(cleaned, "..") || strings.Contains(cleaned, "/../") {
		return "", fmt.Errorf("path traversal denied: %s", key)
	}
	return cleaned, nil
}

// Upload puts an object into S3-compatible storage.
// Presign is best-effort: if the upload succeeds but presigning fails,
// the file is still stored and the returned FileInfo contains an empty URL.
// Callers can obtain a fresh URL via GetURL when needed.
func (s *S3Storage) Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) (*FileInfo, error) {
	key, err := sanitizeKey(key)
	if err != nil {
		return nil, err
	}
	info, err := s.client.PutObject(ctx, s.bucket, key, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to upload to S3: %w", err)
	}

	// Presign is separate from upload — a presign failure should not cause
	// the caller to retry the upload (which would duplicate the object).
	url, _ := s.GetURL(ctx, key)

	return &FileInfo{
		Key:         key,
		Size:        info.Size,
		ContentType: contentType,
		URL:         url,
	}, nil
}

// Delete removes an object from S3-compatible storage.
func (s *S3Storage) Delete(ctx context.Context, key string) error {
	key, err := sanitizeKey(key)
	if err != nil {
		return err
	}
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("failed to delete from S3: %w", err)
	}
	return nil
}

// GetURL returns a pre-signed GET URL for the given object key.
// Uses the public endpoint client so URLs are reachable from outside Docker.
func (s *S3Storage) GetURL(ctx context.Context, key string) (string, error) {
	key, err := sanitizeKey(key)
	if err != nil {
		return "", err
	}
	if s.presignClient == nil {
		return "", fmt.Errorf("presign client is not initialized")
	}
	u, err := s.presignClient.PresignedGetObject(ctx, s.bucket, key, s.presignTTL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}
	if u == nil {
		return "", fmt.Errorf("presigned URL is nil for key: %s", key)
	}
	return u.String(), nil
}

// GetUploadURL returns a pre-signed PUT URL for uploading an object.
// Uses the public endpoint client so URLs are reachable from outside Docker.
func (s *S3Storage) GetUploadURL(ctx context.Context, key string, contentType string) (string, error) {
	key, err := sanitizeKey(key)
	if err != nil {
		return "", err
	}
	if s.presignClient == nil {
		return "", fmt.Errorf("presign client is not initialized")
	}
	u, err := s.presignClient.PresignedPutObject(ctx, s.bucket, key, s.presignTTL)
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned upload URL: %w", err)
	}
	if u == nil {
		return "", fmt.Errorf("presigned upload URL is nil for key: %s", key)
	}
	return u.String(), nil
}

// GetObject returns a reader for the object contents.
// Note: caller should use StatObject first to verify existence — calling Stat()
// on the returned reader can interfere with streaming.
func (s *S3Storage) GetObject(ctx context.Context, key string) (io.ReadCloser, error) {
	key, err := sanitizeKey(key)
	if err != nil {
		return nil, err
	}
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get object from S3: %w", err)
	}
	return obj, nil
}

// StatObject returns metadata about an object without downloading it.
func (s *S3Storage) StatObject(ctx context.Context, key string) (*ObjectInfo, error) {
	key, err := sanitizeKey(key)
	if err != nil {
		return nil, err
	}
	info, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to stat object in S3: %w", err)
	}
	return &ObjectInfo{
		ETag:        info.ETag,
		Size:        info.Size,
		ContentType: info.ContentType,
	}, nil
}
