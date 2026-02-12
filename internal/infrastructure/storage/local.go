package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LocalStorage implements StorageService using the local filesystem.
type LocalStorage struct {
	basePath string
}

const dirPerm = 0o750

// NewLocalStorage creates a new LocalStorage instance.
func NewLocalStorage(basePath string) (*LocalStorage, error) {
	absBase, err := filepath.Abs(basePath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve storage base path: %w", err)
	}
	if err := os.MkdirAll(absBase, dirPerm); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}
	return &LocalStorage{basePath: absBase}, nil
}

// safePath resolves key against basePath and ensures the result stays within basePath.
func (l *LocalStorage) safePath(key string) (string, error) {
	fullPath := filepath.Join(l.basePath, filepath.Clean("/"+key))
	if !strings.HasPrefix(fullPath, l.basePath+string(filepath.Separator)) && fullPath != l.basePath {
		return "", fmt.Errorf("path traversal denied: %s", key)
	}
	return fullPath, nil
}

// Upload writes a file to the local filesystem.
func (l *LocalStorage) Upload(_ context.Context, key string, reader io.Reader, size int64, contentType string) (*FileInfo, error) {
	fullPath, err := l.safePath(key)
	if err != nil {
		return nil, err
	}

	// Ensure parent directories exist
	if err := os.MkdirAll(filepath.Dir(fullPath), dirPerm); err != nil {
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
	fullPath, err := l.safePath(key)
	if err != nil {
		return err
	}
	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete file: %w", err)
	}
	return nil
}

// GetURL returns the public URL path for a locally stored file.
func (l *LocalStorage) GetURL(_ context.Context, key string) (string, error) {
	if _, err := l.safePath(key); err != nil {
		return "", err
	}
	return "/uploads/" + key, nil
}
