package storage

import (
	"context"
	"strings"
	"testing"
)

func TestSanitizeKeyValidCases(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"images/photo.jpg", "images/photo.jpg"},
		{"simple.txt", "simple.txt"},
		{"a/b/c/d.txt", "a/b/c/d.txt"},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := sanitizeKey(tc.input)
			if err != nil {
				t.Fatalf("sanitizeKey(%q) returned error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("sanitizeKey(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestSanitizeKeyRejectsInvalid(t *testing.T) {
	// These keys are truly rejected (empty, dot-dot, or null bytes)
	rejected := []string{
		"",
		"..",
		"has\x00null",
	}

	for _, key := range rejected {
		t.Run("rejected_"+key, func(t *testing.T) {
			_, err := sanitizeKey(key)
			if err == nil {
				t.Errorf("sanitizeKey(%q) should have returned an error", key)
			}
		})
	}
}

func TestSanitizeKeyRejectsDotDotSegments(t *testing.T) {
	// A ".." segment must be rejected, not normalized away. Callers such as
	// /files/url authorize the RAW key by owner prefix before the storage
	// layer sees it, so "files/<self>/../<victim>/x" would pass the prefix
	// check and then be normalized onto another owner's object.
	cases := []string{
		"../etc/passwd",
		"foo/../../bar",
		"a/b/../c.txt",
		"files/self/../victim/secret.pdf",
		"..",
		"a/..",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			if _, err := sanitizeKey(in); err == nil {
				t.Fatalf("sanitizeKey(%q) accepted a key with a .. segment", in)
			}
		})
	}
}

func TestSanitizeKeyAllowsDotsInsideNames(t *testing.T) {
	// Only a segment that IS ".." is traversal; dots inside a name are fine,
	// and a "." segment is harmless and may be normalized.
	cases := []struct{ input, want string }{
		{"report..pdf", "report..pdf"},
		{"a/..b/c", "a/..b/c"},
		{"./simple.txt", "simple.txt"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := sanitizeKey(tc.input)
			if err != nil {
				t.Fatalf("sanitizeKey(%q) returned error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("sanitizeKey(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestS3MethodsRejectInvalidKeys verifies that each S3Storage method rejects
// invalid keys via sanitizeKey before attempting any network call. We construct
// a zero-value S3Storage (no live client) because the sanitizeKey check runs first.
func TestS3MethodsRejectInvalidKeys(t *testing.T) {
	s := &S3Storage{} // no minio client needed — sanitizeKey fails first
	ctx := context.Background()
	badKey := "" // empty key is always rejected

	t.Run("Upload", func(t *testing.T) {
		_, err := s.Upload(ctx, badKey, strings.NewReader("data"), 4, "text/plain")
		if err == nil {
			t.Error("expected error for empty key")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		err := s.Delete(ctx, badKey)
		if err == nil {
			t.Error("expected error for empty key")
		}
	})

	t.Run("GetURL", func(t *testing.T) {
		_, err := s.GetURL(ctx, badKey)
		if err == nil {
			t.Error("expected error for empty key")
		}
	})

	t.Run("GetUploadURL", func(t *testing.T) {
		_, err := s.GetUploadURL(ctx, badKey, "text/plain")
		if err == nil {
			t.Error("expected error for empty key")
		}
	})

	t.Run("GetObject", func(t *testing.T) {
		_, err := s.GetObject(ctx, badKey)
		if err == nil {
			t.Error("expected error for empty key")
		}
	})

	t.Run("StatObject", func(t *testing.T) {
		_, err := s.StatObject(ctx, badKey)
		if err == nil {
			t.Error("expected error for empty key")
		}
	})
}

func TestParseEndpoint(t *testing.T) {
	cases := []struct {
		input      string
		wantHost   string
		wantSecure bool
	}{
		{"https://s3.amazonaws.com", "s3.amazonaws.com", true},
		{"http://minio:9000", "minio:9000", false},
		{"localhost:9000", "localhost:9000", false},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			host, secure := parseEndpoint(tc.input)
			if host != tc.wantHost {
				t.Errorf("parseEndpoint(%q) host = %q, want %q", tc.input, host, tc.wantHost)
			}
			if secure != tc.wantSecure {
				t.Errorf("parseEndpoint(%q) secure = %v, want %v", tc.input, secure, tc.wantSecure)
			}
		})
	}
}
