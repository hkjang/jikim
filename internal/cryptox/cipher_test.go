package cryptox

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

func TestCipherRoundTripAndAuthentication(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, KeySize)
	cipher, err := New(key)
	if err != nil {
		t.Fatal(err)
	}
	aad := []byte("secret:test:1")
	ciphertext, nonce, err := cipher.Encrypt([]byte("sensitive"), aad)
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := cipher.Decrypt(ciphertext, nonce, aad)
	if err != nil || string(plaintext) != "sensitive" {
		t.Fatalf("round trip = %q, %v", plaintext, err)
	}
	ciphertext[0] ^= 0xff
	if _, err := cipher.Decrypt(ciphertext, nonce, aad); err == nil {
		t.Fatal("tampered ciphertext was accepted")
	}
}

func TestCipherBindsAdditionalData(t *testing.T) {
	cipher, _ := New(bytes.Repeat([]byte{0x11}, KeySize))
	ciphertext, nonce, _ := cipher.Encrypt([]byte("value"), []byte("path:a"))
	if _, err := cipher.Decrypt(ciphertext, nonce, []byte("path:b")); err == nil {
		t.Fatal("ciphertext decrypted with different AAD")
	}
}

func TestParseKeyFormats(t *testing.T) {
	want := bytes.Repeat([]byte{0x7f}, KeySize)
	values := []string{string(want), hex.EncodeToString(want), base64.StdEncoding.EncodeToString(want), base64.RawURLEncoding.EncodeToString(want)}
	for _, value := range values {
		got, err := ParseKey(value)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("ParseKey(%q) = %x, %v", value, got, err)
		}
	}
	if _, err := ParseKey(strings.Repeat("x", 31)); err == nil {
		t.Fatal("31-byte key accepted")
	}
}

func TestJSONRoundTrip(t *testing.T) {
	cipher, _ := New(bytes.Repeat([]byte{0x22}, KeySize))
	value := map[string]any{"password": "not-logged", "port": float64(5432)}
	ciphertext, nonce, err := cipher.EncryptJSON(value, []byte("setting:test"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := cipher.DecryptJSON(ciphertext, nonce, []byte("setting:test"), &got); err != nil {
		t.Fatal(err)
	}
	if got["password"] != value["password"] || got["port"] != value["port"] {
		t.Fatalf("got %#v", got)
	}
}
