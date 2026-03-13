package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// LocalStorage implements StorageService using the local filesystem.
type LocalStorage struct {
	basePath string
}

const dirPerm = 0750
const defaultFilePermission = 0640

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

	f, err := os.OpenFile(fullPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, defaultFilePermission)
	if err != nil {
		return nil, fmt.Errorf("failed to create file: %w", err)
	}
	defer f.Close()

	// Enforce size limit: read at most size+1 bytes to detect overflow
	limitedReader := reader
	if size > 0 {
		limitedReader = io.LimitReader(reader, size+1)
	}

	written, err := io.Copy(f, limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	if size > 0 && written > size {
		// File exceeds declared size — clean up and return error
		f.Close()
		os.Remove(fullPath)
		return nil, fmt.Errorf("file size %d exceeds declared size %d", written, size)
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

// GetUploadURL returns the upload URL for a locally stored file.
// For local storage, uploads go through the backend, so this returns the same path.
func (l *LocalStorage) GetUploadURL(_ context.Context, key string, _ string) (string, error) {
	if _, err := l.safePath(key); err != nil {
		return "", err
	}
	return "/uploads/" + key, nil
}

// GetObject returns a reader for a locally stored file.
func (l *LocalStorage) GetObject(_ context.Context, key string) (io.ReadCloser, error) {
	fullPath, err := l.safePath(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	return f, nil
}

// StatObject returns metadata about a locally stored file.
func (l *LocalStorage) StatObject(_ context.Context, key string) (*ObjectInfo, error) {
	fullPath, err := l.safePath(key)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	ct := "application/octet-stream"
	if f, err := os.Open(fullPath); err == nil {
		defer f.Close()
		buf := make([]byte, 512)
		if n, _ := f.Read(buf); n > 0 {
			ct = http.DetectContentType(buf[:n])
		}
	}

	return &ObjectInfo{
		ETag:        fmt.Sprintf(`"%x-%x"`, fi.ModTime().UnixNano(), fi.Size()),
		Size:        fi.Size(),
		ContentType: ct,
	}, nil
}
