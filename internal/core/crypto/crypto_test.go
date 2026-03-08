package crypto

import (
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := NormalizeKey("test-secret-key-with-enough-entropy-32+")
	plaintext := "hello, world!"

	encrypted, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decrypted, err := Decrypt(encrypted, key)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if decrypted != plaintext {
		t.Fatalf("round-trip mismatch: got %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptDecryptEmptyPlaintext(t *testing.T) {
	key := NormalizeKey("test-secret-key-with-enough-entropy-32+")

	encrypted, err := Encrypt("", key)
	if err != nil {
		t.Fatalf("Encrypt empty failed: %v", err)
	}

	decrypted, err := Decrypt(encrypted, key)
	if err != nil {
		t.Fatalf("Decrypt empty failed: %v", err)
	}

	if decrypted != "" {
		t.Fatalf("expected empty string, got %q", decrypted)
	}
}

func TestDecryptWithWrongKey(t *testing.T) {
	key1 := NormalizeKey("correct-key-with-enough-entropy-here")
	key2 := NormalizeKey("wrong-key-with-enough-entropy-here!!")

	encrypted, err := Encrypt("secret data", key1)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	_, err = Decrypt(encrypted, key2)
	if err == nil {
		t.Fatal("expected Decrypt with wrong key to fail")
	}
}

func TestDecryptCorruptedCiphertext(t *testing.T) {
	key := NormalizeKey("test-secret-key-with-enough-entropy-32+")

	encrypted, err := Encrypt("test data", key)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Corrupt the ciphertext by modifying a character
	corrupted := encrypted[:len(encrypted)-2] + "XX"
	_, err = Decrypt(corrupted, key)
	if err == nil {
		t.Fatal("expected Decrypt of corrupted ciphertext to fail")
	}
}

func TestDecryptInvalidBase64(t *testing.T) {
	key := NormalizeKey("test-secret-key-with-enough-entropy-32+")

	_, err := Decrypt("not-valid-base64!!!", key)
	if err == nil {
		t.Fatal("expected Decrypt with invalid base64 to fail")
	}
}

func TestDecryptTruncatedCiphertext(t *testing.T) {
	key := NormalizeKey("test-secret-key-with-enough-entropy-32+")

	// Very short base64 that decodes to less than nonce size
	_, err := Decrypt("AQID", key)
	if err == nil {
		t.Fatal("expected Decrypt with truncated ciphertext to fail")
	}
}

func TestNormalizeKeyDeterminism(t *testing.T) {
	input := "same-input-string-for-testing"
	k1 := NormalizeKey(input)
	k2 := NormalizeKey(input)

	if len(k1) != 32 {
		t.Fatalf("expected 32-byte key, got %d", len(k1))
	}

	for i := range k1 {
		if k1[i] != k2[i] {
			t.Fatal("NormalizeKey is not deterministic")
		}
	}
}

func TestNormalizeKeyLength(t *testing.T) {
	inputs := []string{"short", "a-much-longer-input-string-that-exceeds-32-characters-significantly"}
	for _, input := range inputs {
		key := NormalizeKey(input)
		if len(key) != 32 {
			t.Fatalf("expected 32-byte key for input %q, got %d", input, len(key))
		}
	}
}

func TestEncryptProducesDifferentCiphertexts(t *testing.T) {
	key := NormalizeKey("test-secret-key-with-enough-entropy-32+")
	plaintext := "same plaintext"

	c1, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("first Encrypt failed: %v", err)
	}

	c2, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("second Encrypt failed: %v", err)
	}

	if c1 == c2 {
		t.Fatal("two encryptions of the same plaintext should produce different ciphertexts (random nonce)")
	}
}

func TestEncryptRejectsInvalidKeyLength(t *testing.T) {
	shortKey := []byte("too-short")
	_, err := Encrypt("test", shortKey)
	if err == nil {
		t.Fatal("expected Encrypt to reject non-32-byte key")
	}

	longKey := make([]byte, 64)
	_, err = Encrypt("test", longKey)
	if err == nil {
		t.Fatal("expected Encrypt to reject 64-byte key")
	}
}

func TestDecryptRejectsInvalidKeyLength(t *testing.T) {
	// First encrypt with valid key
	key := NormalizeKey("valid-key-for-encryption-testing!")
	encrypted, err := Encrypt("test", key)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	shortKey := []byte("too-short")
	_, err = Decrypt(encrypted, shortKey)
	if err == nil {
		t.Fatal("expected Decrypt to reject non-32-byte key")
	}
}

func TestHashSHA256Hex(t *testing.T) {
	hash := HashSHA256Hex("hello")
	// Known SHA-256 of "hello"
	expected := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if hash != expected {
		t.Fatalf("got %q, want %q", hash, expected)
	}
}
