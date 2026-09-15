package storage

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestLocalStorage(t *testing.T) *LocalStorage {
	t.Helper()
	base := filepath.Join(t.TempDir(), "storage")
	ls, err := NewLocalStorage(base)
	if err != nil {
		t.Fatalf("NewLocalStorage failed: %v", err)
	}
	return ls
}

func TestNewLocalStorageCreatesDirectory(t *testing.T) {
	deep := filepath.Join(t.TempDir(), "a", "b", "c", "storage")
	ls, err := NewLocalStorage(deep)
	if err != nil {
		t.Fatalf("NewLocalStorage failed: %v", err)
	}

	info, err := os.Stat(ls.basePath)
	if err != nil {
		t.Fatalf("directory was not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("basePath is not a directory")
	}
}

func TestLocalStorageUploadAndGetObject(t *testing.T) {
	ls := newTestLocalStorage(t)
	ctx := context.Background()

	content := "hello, world!"
	key := "docs/readme.txt"
	fi, err := ls.Upload(ctx, key, strings.NewReader(content), int64(len(content)), "text/plain")
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	if fi.Key != key {
		t.Errorf("FileInfo.Key = %q, want %q", fi.Key, key)
	}
	if fi.Size != int64(len(content)) {
		t.Errorf("FileInfo.Size = %d, want %d", fi.Size, len(content))
	}
	if fi.ContentType != "text/plain" {
		t.Errorf("FileInfo.ContentType = %q, want %q", fi.ContentType, "text/plain")
	}
	if fi.URL != "/uploads/"+key {
		t.Errorf("FileInfo.URL = %q, want %q", fi.URL, "/uploads/"+key)
	}

	rc, err := ls.GetObject(ctx, key)
	if err != nil {
		t.Fatalf("GetObject failed: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if string(got) != content {
		t.Errorf("GetObject content = %q, want %q", string(got), content)
	}
}

func TestLocalStorageUploadSizeOverflow(t *testing.T) {
	ls := newTestLocalStorage(t)
	ctx := context.Background()

	data := bytes.Repeat([]byte("x"), 10)
	_, err := ls.Upload(ctx, "overflow.txt", bytes.NewReader(data), 5, "text/plain")
	if err == nil {
		t.Fatal("expected error for oversized upload, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds declared size") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLocalStorageUploadZeroSize(t *testing.T) {
	ls := newTestLocalStorage(t)
	ctx := context.Background()

	data := bytes.Repeat([]byte("y"), 1024)
	fi, err := ls.Upload(ctx, "unlimited.bin", bytes.NewReader(data), 0, "application/octet-stream")
	if err != nil {
		t.Fatalf("Upload with size=0 should succeed: %v", err)
	}
	if fi.Size != 1024 {
		t.Errorf("FileInfo.Size = %d, want 1024", fi.Size)
	}
}

func TestLocalStorageDelete(t *testing.T) {
	ls := newTestLocalStorage(t)
	ctx := context.Background()

	content := "delete me"
	_, err := ls.Upload(ctx, "tmp.txt", strings.NewReader(content), int64(len(content)), "text/plain")
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	if err := ls.Delete(ctx, "tmp.txt"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err = ls.GetObject(ctx, "tmp.txt")
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}
}

func TestLocalStorageDeleteNonExistent(t *testing.T) {
	ls := newTestLocalStorage(t)
	ctx := context.Background()

	if err := ls.Delete(ctx, "no-such-file.txt"); err != nil {
		t.Fatalf("Delete of non-existent file should not error: %v", err)
	}
}

func TestLocalStorageGetURL(t *testing.T) {
	ls := newTestLocalStorage(t)
	ctx := context.Background()

	url, err := ls.GetURL(ctx, "images/photo.jpg")
	if err != nil {
		t.Fatalf("GetURL failed: %v", err)
	}
	if url != "/uploads/images/photo.jpg" {
		t.Errorf("GetURL = %q, want %q", url, "/uploads/images/photo.jpg")
	}
}

func TestLocalStorageGetUploadURL(t *testing.T) {
	ls := newTestLocalStorage(t)
	ctx := context.Background()

	url, err := ls.GetUploadURL(ctx, "images/photo.jpg", "image/jpeg")
	if err != nil {
		t.Fatalf("GetUploadURL failed: %v", err)
	}
	if url != "/uploads/images/photo.jpg" {
		t.Errorf("GetUploadURL = %q, want %q", url, "/uploads/images/photo.jpg")
	}
}

func TestLocalStorageStatObject(t *testing.T) {
	ls := newTestLocalStorage(t)
	ctx := context.Background()

	content := "stat me"
	_, err := ls.Upload(ctx, "stat.txt", strings.NewReader(content), int64(len(content)), "text/plain")
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	oi, err := ls.StatObject(ctx, "stat.txt")
	if err != nil {
		t.Fatalf("StatObject failed: %v", err)
	}
	if oi.Size != int64(len(content)) {
		t.Errorf("ObjectInfo.Size = %d, want %d", oi.Size, len(content))
	}
	if oi.ETag == "" {
		t.Error("ObjectInfo.ETag is empty")
	}
	if oi.ContentType == "" {
		t.Error("ObjectInfo.ContentType is empty")
	}
}

func TestLocalStorageStatObjectNotFound(t *testing.T) {
	ls := newTestLocalStorage(t)
	ctx := context.Background()

	_, err := ls.StatObject(ctx, "missing.txt")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLocalStoragePathTraversalBlocked(t *testing.T) {
	ls := newTestLocalStorage(t)
	ctx := context.Background()

	// A ".." segment is rejected outright. Normalizing it would let a key that
	// passed an owner-prefix check upstream resolve onto another owner's file.
	rejected := []string{
		"../../../etc/passwd",
		"../../secret.txt",
		"foo/../bar",
		"files/self/../victim/secret.pdf",
	}
	for _, key := range rejected {
		t.Run("Rejected_"+key, func(t *testing.T) {
			if _, err := ls.safePath(key); err == nil {
				t.Fatalf("safePath(%q) accepted a key with a .. segment", key)
			}
			content := "x"
			if _, err := ls.Upload(ctx, key, strings.NewReader(content), int64(len(content)), "text/plain"); err == nil {
				t.Fatalf("Upload(%q) accepted a key with a .. segment", key)
			}
		})
	}

	// Dots inside a name are not traversal; a "." segment is harmless.
	accepted := []string{"report..pdf", "a/..b/c.txt", "./simple.txt"}
	for _, key := range accepted {
		t.Run("Accepted_"+key, func(t *testing.T) {
			p, err := ls.safePath(key)
			if err != nil {
				t.Fatalf("safePath(%q) rejected a safe key: %v", key, err)
			}
			if !strings.HasPrefix(p, ls.basePath) {
				t.Errorf("safePath(%q) = %q escaped basePath %q", key, p, ls.basePath)
			}
		})
	}
}

func TestLocalStorageGetObjectNotFound(t *testing.T) {
	ls := newTestLocalStorage(t)
	ctx := context.Background()

	_, err := ls.GetObject(ctx, "does-not-exist.txt")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}
