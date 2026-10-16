package store

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// unreachablePool은 연결이 즉시 거부되는 lazy pool을 만듭니다. TransitDecrypt가
// 입력을 저장소까지 내려보내면 연결 오류(ErrInvalid가 아님)로 돌아오므로
// "DB에 닿기 전에 거절했는가"를 DSN 없이 구분할 수 있습니다.
func unreachablePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	config, err := pgxpool.ParseConfig("postgres://u:p@127.0.0.1:1/none")
	if err != nil {
		t.Fatalf("DSN 파싱 실패: %v", err)
	}
	config.MinConns = 0
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("pool 생성 실패: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestTransitDecryptRejectsNonCanonicalKeyVersionTokens(t *testing.T) {
	store := &Store{pool: unreachablePool(t)}
	payload := base64.RawStdEncoding.EncodeToString(make([]byte, 32))

	tests := []struct {
		name    string
		version string
		invalid bool
	}{
		{name: "leading_zero", version: "v01", invalid: true},
		{name: "triple_leading_zero", version: "v001", invalid: true},
		{name: "explicit_plus_sign", version: "v+1", invalid: true},
		{name: "plus_sign_with_leading_zero", version: "v+01", invalid: true},
		{name: "zero_version", version: "v0", invalid: true},
		{name: "negative_version", version: "v-1", invalid: true},
		{name: "non_numeric_version", version: "vx", invalid: true},
		{name: "empty_version", version: "v", invalid: true},
		{name: "canonical_v1", version: "v1", invalid: false},
		{name: "canonical_v12", version: "v12", invalid: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := store.TransitDecrypt(ctx, "k", "vault:"+test.version+":"+payload)
			if err == nil {
				t.Fatalf("TransitDecrypt(%q) error = nil", test.version)
			}
			if got := errors.Is(err, ErrInvalid); got != test.invalid {
				t.Fatalf("TransitDecrypt(%q): errors.Is(err, ErrInvalid)=%v want %v (err=%v)", test.version, got, test.invalid, err)
			}
		})
	}
}

func TestTransitCiphertextKeyVersion(t *testing.T) {
	tests := []struct {
		token  string
		want   int
		wantOK bool
	}{
		{token: "v1", want: 1, wantOK: true},
		{token: "v12", want: 12, wantOK: true},
		{token: "v10", want: 10, wantOK: true},
		{token: "v01", wantOK: false},
		{token: "v001", wantOK: false},
		{token: "v+1", wantOK: false},
		{token: "v+01", wantOK: false},
		{token: "v-1", wantOK: false},
		{token: "v0", wantOK: false},
		{token: "v00", wantOK: false},
		{token: "v", wantOK: false},
		{token: "vx", wantOK: false},
		{token: "v1x", wantOK: false},
		{token: "v 1", wantOK: false},
		{token: "v1 ", wantOK: false},
		{token: "v99999999999999999999", wantOK: false},
		{token: "1", wantOK: false},
		{token: "", wantOK: false},
	}
	for _, test := range tests {
		got, ok := transitCiphertextKeyVersion(test.token)
		if ok != test.wantOK || (ok && got != test.want) {
			t.Errorf("transitCiphertextKeyVersion(%q) = (%d,%v), want (%d,%v)", test.token, got, ok, test.want, test.wantOK)
		}
	}
}

func TestTransitDecryptRejectsMalformedCiphertext(t *testing.T) {
	store := &Store{pool: unreachablePool(t)}
	payload := base64.RawStdEncoding.EncodeToString(make([]byte, 32))

	tests := []struct{ name, encoded string }{
		{name: "wrong_prefix", encoded: "bao:v1:" + payload},
		{name: "missing_version_prefix", encoded: "vault:1:" + payload},
		{name: "too_few_parts", encoded: "vault:v1"},
		{name: "short_payload", encoded: "vault:v1:" + base64.RawStdEncoding.EncodeToString(make([]byte, 12))},
		{name: "not_base64", encoded: "vault:v1:!!!!not-base64!!!!"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := store.TransitDecrypt(ctx, "k", test.encoded); !errors.Is(err, ErrInvalid) {
				t.Fatalf("TransitDecrypt(%q) error = %v, want ErrInvalid", test.encoded, err)
			}
		})
	}
}
