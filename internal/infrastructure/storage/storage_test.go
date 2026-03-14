package storage

import (
	"path/filepath"
	"testing"

	"github.com/mr-kaynak/go-core/internal/core/config"
)

func TestNewStorageServiceLocal(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Type = "local"
	cfg.Storage.LocalPath = filepath.Join(t.TempDir(), "uploads")

	svc, err := NewStorageService(cfg)
	if err != nil {
		t.Fatalf("NewStorageService(local) failed: %v", err)
	}
	if svc == nil {
		t.Fatal("expected non-nil StorageService")
	}

	// Verify it's a LocalStorage instance
	if _, ok := svc.(*LocalStorage); !ok {
		t.Errorf("expected *LocalStorage, got %T", svc)
	}
}

func TestNewStorageServiceUnsupported(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Type = "ftp"

	_, err := NewStorageService(cfg)
	if err == nil {
		t.Fatal("expected error for unsupported storage type")
	}
}

func TestNewStorageServiceS3MissingEndpoint(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Type = "s3"
	// Empty S3 config will cause minio.New to fail with invalid endpoint

	_, err := NewStorageService(cfg)
	if err == nil {
		t.Fatal("expected error for S3 with no endpoint")
	}
}
