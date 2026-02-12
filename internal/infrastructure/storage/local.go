package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// LocalStorage implements StorageService using the local filesystem.
type LocalStorage struct {
	basePath string
}

// NewLocalStorage creates a new LocalStorage instance.
func NewLocalStorage(basePath string) (*LocalStorage, error) {
	if err := os.MkdirAll(basePath, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}
	return &LocalStorage{basePath: basePath}, nil
}

// Upload writes a file to the local filesystem.
func (l *LocalStorage) Upload(_ context.Context, key string, reader io.Reader, size int64, contentType string) (*FileInfo, error) {
	fullPath := filepath.Join(l.basePath, key)

	// Ensure parent directories exist
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
		return nil, fmt.Errorf("failed to create directories: %w", err)
	}

	f, err := os.Create(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create file: %w", err)
	}
	defer f.Close()

	written, err := io.Copy(f, reader)
	if err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	return &FileInfo{
		Key:         key,
		Size:        written,
		ContentType: contentType,
		URL:         "/uploads/" + key,
	}, nil
}

// Delete removes a file from the local filesystem.
func (l *LocalStorage) Delete(_ context.Context, key string) error {
	fullPath := filepath.Join(l.basePath, key)
	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete file: %w", err)
	}
	return nil
}

// GetURL returns the public URL path for a locally stored file.
func (l *LocalStorage) GetURL(_ context.Context, key string) (string, error) {
	return "/uploads/" + key, nil
}
