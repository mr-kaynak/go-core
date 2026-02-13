package storage

import (
	"context"
	"fmt"
	"io"

	"github.com/mr-kaynak/go-core/internal/core/config"
)

// FileInfo holds metadata about an uploaded file.
type FileInfo struct {
	Key         string `json:"key"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
	URL         string `json:"url"`
}

// StorageService defines the interface for file storage operations.
type StorageService interface {
	Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) (*FileInfo, error)
	Delete(ctx context.Context, key string) error
	GetURL(ctx context.Context, key string) (string, error)
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
