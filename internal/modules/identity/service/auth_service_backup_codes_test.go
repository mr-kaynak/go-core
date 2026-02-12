package service

import (
	"encoding/hex"
	"testing"
)

func TestGenerateBackupCodesEntropyAndFormat(t *testing.T) {
	codes, err := generateBackupCodes(defaultBackupCodeCount)
	if err != nil {
		t.Fatalf("expected backup codes generation to succeed, got %v", err)
	}
	if len(codes) != defaultBackupCodeCount {
		t.Fatalf("expected %d backup codes, got %d", defaultBackupCodeCount, len(codes))
	}

	for _, code := range codes {
		b, decodeErr := hex.DecodeString(code)
		if decodeErr != nil {
			t.Fatalf("expected valid hex backup code, got %q (%v)", code, decodeErr)
		}
		if len(b) != backupCodeBytes {
			t.Fatalf("expected %d-byte backup code, got %d bytes", backupCodeBytes, len(b))
		}
	}
}

func TestSecureHashEqual(t *testing.T) {
	if !secureHashEqual("abc123", "abc123") {
		t.Fatalf("expected equal hashes to match")
	}
	if secureHashEqual("abc123", "abc124") {
		t.Fatalf("expected different hashes not to match")
	}
}
