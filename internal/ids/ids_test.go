package ids

import (
	"bytes"
	"regexp"
	"testing"
)

func TestUUIDAndToken(t *testing.T) {
	id, err := UUID()
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(id) {
		t.Fatalf("invalid UUID: %s", id)
	}
	plain, hash, err := Token("jks.")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(hash, HashToken(plain)) {
		t.Fatal("token hash mismatch")
	}
}
