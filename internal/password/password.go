package password

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	iterations = 310_000
	saltSize   = 16
	keySize    = 32
)

var dummyEncodedHash = encodeHash("jikim-invalid-account-password", []byte("jikim-auth-dummy"))

func Hash(value string) (string, error) {
	if len(value) < 12 {
		return "", errors.New("비밀번호는 12자 이상이어야 합니다")
	}
	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	return encodeHash(value, salt), nil
}

func encodeHash(value string, salt []byte) string {
	derived := pbkdf2([]byte(value), salt, iterations, keySize)
	return fmt.Sprintf("$pbkdf2-sha256$%d$%s$%s", iterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(derived))
}

func Verify(encoded, value string) bool {
	iter, salt, want, valid := parseHash(encoded)
	if !valid {
		iter, salt, want, _ = parseHash(dummyEncodedHash)
	}
	got := pbkdf2([]byte(value), salt, iter, len(want))
	return valid && subtle.ConstantTimeCompare(got, want) == 1
}

func parseHash(encoded string) (int, []byte, []byte, bool) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[1] != "pbkdf2-sha256" {
		return 0, nil, nil, false
	}
	iter, err := strconv.Atoi(parts[2])
	if err != nil || iter < 100_000 || iter > 2_000_000 {
		return 0, nil, nil, false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(salt) < 16 {
		return 0, nil, nil, false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(want) < 32 {
		return 0, nil, nil, false
	}
	return iter, salt, want, true
}

func pbkdf2(password, salt []byte, iter, length int) []byte {
	hLen := sha256.Size
	blocks := (length + hLen - 1) / hLen
	out := make([]byte, 0, blocks*hLen)
	for block := 1; block <= blocks; block++ {
		mac := hmac.New(sha256.New, password)
		mac.Write(salt)
		mac.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for i := 1; i < iter; i++ {
			mac = hmac.New(sha256.New, password)
			mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		out = append(out, t...)
	}
	return out[:length]
}
