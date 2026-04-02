package crypto

import (
	"testing"
)

func TestRoundTrip(t *testing.T) {
	key := DeriveKey("test-secret")
	c, err := NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}

	original := "my-api-key-12345"
	encrypted, err := c.Encrypt(original)
	if err != nil {
		t.Fatal(err)
	}

	if encrypted == original {
		t.Error("encrypted value should differ from original")
	}
	if !IsEncrypted(encrypted) {
		t.Error("encrypted value should have enc: prefix")
	}

	decrypted, err := c.Decrypt(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if decrypted != original {
		t.Errorf("decrypted = %q, want %q", decrypted, original)
	}
}

func TestDifferentNonces(t *testing.T) {
	key := DeriveKey("test-secret")
	c, err := NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}

	a, _ := c.Encrypt("same-plaintext")
	b, _ := c.Encrypt("same-plaintext")
	if a == b {
		t.Error("two encryptions of the same plaintext should produce different ciphertext")
	}

	da, _ := c.Decrypt(a)
	db, _ := c.Decrypt(b)
	if da != db {
		t.Error("both should decrypt to the same plaintext")
	}
}

func TestPlaintextPassthrough(t *testing.T) {
	key := DeriveKey("test-secret")
	c, err := NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}

	plain := "not-encrypted-value"
	result, err := c.Decrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	if result != plain {
		t.Errorf("passthrough = %q, want %q", result, plain)
	}
}

func TestWrongKeyFails(t *testing.T) {
	key1 := DeriveKey("secret-one")
	key2 := DeriveKey("secret-two")

	c1, _ := NewCipher(key1)
	c2, _ := NewCipher(key2)

	encrypted, _ := c1.Encrypt("sensitive-data")

	_, err := c2.Decrypt(encrypted)
	if err == nil {
		t.Error("decryption with wrong key should fail")
	}
}

func TestIsEncrypted(t *testing.T) {
	if IsEncrypted("plaintext") {
		t.Error("plaintext should not be detected as encrypted")
	}
	if !IsEncrypted("enc:abc123") {
		t.Error("enc: prefixed value should be detected as encrypted")
	}
	if IsEncrypted("") {
		t.Error("empty string should not be detected as encrypted")
	}
}

func TestDeriveKeyDeterministic(t *testing.T) {
	a := DeriveKey("same-secret")
	b := DeriveKey("same-secret")
	if string(a) != string(b) {
		t.Error("DeriveKey should be deterministic")
	}

	c := DeriveKey("different-secret")
	if string(a) == string(c) {
		t.Error("different secrets should produce different keys")
	}
}

func TestNewCipherRejectsWrongKeyLength(t *testing.T) {
	_, err := NewCipher([]byte("too-short"))
	if err == nil {
		t.Error("should reject key shorter than 32 bytes")
	}
}

func TestEmptyString(t *testing.T) {
	key := DeriveKey("test-secret")
	c, _ := NewCipher(key)

	encrypted, err := c.Encrypt("")
	if err != nil {
		t.Fatal(err)
	}

	decrypted, err := c.Decrypt(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if decrypted != "" {
		t.Errorf("decrypted = %q, want empty string", decrypted)
	}
}
