package storage

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/mr-kaynak/go-core/internal/core/config"
)

// FileInfo holds metadata about an uploaded file.
type FileInfo struct {
	Key         string `json:"key"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
	URL         string `json:"url"`
}

// ObjectInfo holds metadata about a stored object.
type ObjectInfo struct {
	ETag        string
	Size        int64
	ContentType string
}

// hasDotDotSegment reports whether any "/"-separated segment of key is exactly
// "..". Such keys are rejected rather than normalized: callers authorize the RAW
// key (e.g. by owner prefix) before the storage layer sees it, so normalizing
// "files/<self>/../<victim>/x" would silently retarget another owner's object.
// Dots inside a name ("report..pdf") are not traversal and are allowed.
func hasDotDotSegment(key string) bool {
	for _, seg := range strings.Split(strings.ReplaceAll(key, "\\", "/"), "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

// StorageService defines the interface for file storage operations.
type StorageService interface {
	Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) (*FileInfo, error)
	Delete(ctx context.Context, key string) error
	GetURL(ctx context.Context, key string) (string, error)
	GetUploadURL(ctx context.Context, key string, contentType string) (string, error)
	GetObject(ctx context.Context, key string) (io.ReadCloser, error)
	StatObject(ctx context.Context, key string) (*ObjectInfo, error)
}

// NewStorageService creates a StorageService based on the configured storage type.
func NewStorageService(cfg *config.Config) (StorageService, error) {
	switch cfg.Storage.Type {
	case "local":
		ls, err := NewLocalStorage(cfg.Storage.LocalPath)
		if err != nil {
			return nil, err
		}
		return ls, nil
	case "s3":
		s3, err := NewS3Storage(cfg)
		if err != nil {
			return nil, err
		}
		return s3, nil
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", cfg.Storage.Type)
	}
}
