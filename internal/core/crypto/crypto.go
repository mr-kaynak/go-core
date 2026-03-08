package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
)

// NormalizeKey normalizes a high-entropy key string into a 32-byte AES-256 key using SHA-256.
// IMPORTANT: This function is NOT suitable for low-entropy or user-provided passwords.
// The input MUST have at least 256 bits of entropy (e.g. a 32+ char random secret).
// For password-based key derivation, use a proper KDF (PBKDF2, scrypt, Argon2).
func NormalizeKey(key string) []byte {
	h := sha256.Sum256([]byte(key))
	return h[:]
}

// DeriveKey is a deprecated alias for NormalizeKey.
//
// Deprecated: Use NormalizeKey instead.
func DeriveKey(passphrase string) []byte {
	return NormalizeKey(passphrase)
}

// Encrypt encrypts plaintext using AES-256-GCM and returns a base64-encoded ciphertext
// containing the nonce prepended to the encrypted data.
// Key must be exactly 32 bytes for AES-256.
func Encrypt(plaintext string, key []byte) (string, error) {
	if len(key) != 32 {
		return "", fmt.Errorf("crypto: key must be 32 bytes for AES-256, got %d", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("crypto: failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("crypto: failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("crypto: failed to generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts a base64-encoded AES-256-GCM ciphertext.
// Key must be exactly 32 bytes for AES-256.
func Decrypt(encoded string, key []byte) (string, error) {
	if len(key) != 32 {
		return "", fmt.Errorf("crypto: key must be 32 bytes for AES-256, got %d", len(key))
	}

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("crypto: failed to decode base64: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("crypto: failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("crypto: failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("crypto: ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("crypto: failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}

// DeriveHMACKey derives a purpose-specific key from a master key using HMAC-SHA256.
// This produces a 32-byte key suitable for HMAC signing or AES-256 encryption.
func DeriveHMACKey(masterKey []byte, purpose string) []byte {
	mac := hmac.New(sha256.New, masterKey)
	mac.Write([]byte(purpose))
	return mac.Sum(nil)
}

// HashSHA256Hex returns the hex-encoded SHA-256 hash of the input.
func HashSHA256Hex(input string) string {
	h := sha256.Sum256([]byte(input))
	return hex.EncodeToString(h[:])
}
