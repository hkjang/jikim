package store

import (
	"bytes"
	"testing"

	"github.com/hkjang/jikim/internal/cryptox"
)

func TestVerifyMasterKeySentinelRejectsWrongKey(t *testing.T) {
	correct, _ := cryptox.New(bytes.Repeat([]byte{1}, cryptox.KeySize))
	wrong, _ := cryptox.New(bytes.Repeat([]byte{2}, cryptox.KeySize))
	ciphertext, nonce, err := correct.Encrypt([]byte(masterKeySentinel), []byte("master-key-sentinel"))
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyMasterKeySentinel(correct, ciphertext, nonce); err != nil {
		t.Fatalf("correct key rejected: %v", err)
	}
	if err := verifyMasterKeySentinel(wrong, ciphertext, nonce); err == nil {
		t.Fatal("wrong master key accepted")
	}
}
