package cryptox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const KeySize = 32

type Cipher struct {
	aead cipher.AEAD
}

func ParseKey(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, errors.New("암호화 키가 비어 있습니다")
	}
	decoders := []func(string) ([]byte, error){
		hex.DecodeString,
		base64.StdEncoding.DecodeString,
		base64.RawStdEncoding.DecodeString,
		base64.URLEncoding.DecodeString,
		base64.RawURLEncoding.DecodeString,
	}
	for _, decode := range decoders {
		if decoded, err := decode(value); err == nil && len(decoded) == KeySize {
			return decoded, nil
		}
	}
	if len([]byte(value)) == KeySize {
		return []byte(value), nil
	}
	return nil, errors.New("ENCRYPTION_KEY는 32바이트 원문, 64자리 hex 또는 32바이트 base64여야 합니다")
}

func New(key []byte) (*Cipher, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("AES-256 키 길이는 %d바이트여야 합니다", KeySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

func NewFromString(value string) (*Cipher, error) {
	key, err := ParseKey(value)
	if err != nil {
		return nil, err
	}
	return New(key)
}

func RandomKey() ([]byte, error) {
	key := make([]byte, KeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	return key, nil
}

func (c *Cipher) Encrypt(plaintext, additionalData []byte) (ciphertext, nonce []byte, err error) {
	nonce = make([]byte, c.aead.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	return c.aead.Seal(nil, nonce, plaintext, additionalData), nonce, nil
}

func (c *Cipher) Decrypt(ciphertext, nonce, additionalData []byte) ([]byte, error) {
	if len(nonce) != c.aead.NonceSize() {
		return nil, errors.New("잘못된 nonce 길이")
	}
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, additionalData)
	if err != nil {
		return nil, errors.New("암호문 인증에 실패했습니다")
	}
	return plaintext, nil
}

func (c *Cipher) EncryptJSON(value any, additionalData []byte) ([]byte, []byte, error) {
	plaintext, err := json.Marshal(value)
	if err != nil {
		return nil, nil, err
	}
	return c.Encrypt(plaintext, additionalData)
}

func (c *Cipher) DecryptJSON(ciphertext, nonce, additionalData []byte, dst any) error {
	plaintext, err := c.Decrypt(ciphertext, nonce, additionalData)
	if err != nil {
		return err
	}
	return json.Unmarshal(plaintext, dst)
}

func EncodeEnvelope(version int, nonce, ciphertext []byte) string {
	payload := append(append([]byte(nil), nonce...), ciphertext...)
	return fmt.Sprintf("jikim:v%d:%s", version, base64.RawStdEncoding.EncodeToString(payload))
}

func DecodeEnvelope(value string, nonceSize int) (int, []byte, []byte, error) {
	var version int
	var encoded string
	if _, err := fmt.Sscanf(value, "jikim:v%d:%s", &version, &encoded); err != nil {
		return 0, nil, nil, errors.New("지원하지 않는 암호문 형식")
	}
	payload, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil || len(payload) <= nonceSize {
		return 0, nil, nil, errors.New("손상된 암호문")
	}
	return version, payload[:nonceSize], payload[nonceSize:], nil
}
