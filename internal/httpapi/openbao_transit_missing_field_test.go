package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hkjang/jikim/internal/model"
)

func decodeBaoData(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode response: %v body=%s", err, body)
	}
	return envelope.Data
}

// An absent plaintext field is not an empty plaintext: the single-item path
// must reject it the way the batch path already does instead of encrypting ""
// and charging the store for it.
func TestOpenBaoTransitEncryptRejectsMissingPlaintextField(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "empty object", body: `{}`},
		{name: "reference without plaintext", body: `{"reference":"a"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			server := &Server{
				transitAuthorizer: allowAllTransit(),
				auditRecorder:     func(context.Context, model.AuditEvent) error { return nil },
				transitEncryptor: func(_ context.Context, _, plaintext, _ string) (string, error) {
					calls++
					return "vault:v1:" + plaintext, nil
				},
			}
			response := httptest.NewRecorder()
			server.baoTransitEncrypt(response, transitBatchRequest("/v1/transit/encrypt/customer", "customer", test.body))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), "missing plaintext to encrypt") {
				t.Fatalf("body=%s", response.Body.String())
			}
			if calls != 0 {
				t.Fatalf("transitEncryptor calls=%d, want 0 body=%s", calls, response.Body.String())
			}
		})
	}
}

// An explicitly empty plaintext stays a valid input, as it is in OpenBao, and
// a normal plaintext keeps its 200.
func TestOpenBaoTransitEncryptAcceptsPresentPlaintext(t *testing.T) {
	for _, test := range []struct {
		name           string
		body           string
		wantCiphertext string
	}{
		{name: "explicit empty plaintext", body: `{"plaintext":""}`, wantCiphertext: "vault:v1:"},
		{name: "normal plaintext", body: `{"plaintext":"aGk="}`, wantCiphertext: "vault:v1:aGk="},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			server := &Server{
				transitAuthorizer: allowAllTransit(),
				auditRecorder:     func(context.Context, model.AuditEvent) error { return nil },
				transitEncryptor: func(_ context.Context, _, plaintext, _ string) (string, error) {
					calls++
					return "vault:v1:" + plaintext, nil
				},
			}
			response := httptest.NewRecorder()
			server.baoTransitEncrypt(response, transitBatchRequest("/v1/transit/encrypt/customer", "customer", test.body))
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if calls != 1 {
				t.Fatalf("transitEncryptor calls=%d, want 1", calls)
			}
			data := decodeBaoData(t, response.Body.Bytes())
			if data["ciphertext"] != test.wantCiphertext {
				t.Fatalf("ciphertext=%v, want %q", data["ciphertext"], test.wantCiphertext)
			}
			if version, ok := data["key_version"].(float64); !ok || int(version) != 1 {
				t.Fatalf("key_version=%v", data["key_version"])
			}
		})
	}
}

// An absent ciphertext field must be refused with the batch path's message
// before the store is asked to parse "".
func TestOpenBaoTransitDecryptRejectsMissingCiphertextField(t *testing.T) {
	calls := 0
	server := &Server{
		transitAuthorizer: allowAllTransit(),
		auditRecorder:     func(context.Context, model.AuditEvent) error { return nil },
		transitDecryptor: func(context.Context, string, string) (string, error) {
			calls++
			return "aGk=", nil
		},
	}
	response := httptest.NewRecorder()
	server.baoTransitDecrypt(response, transitBatchRequest("/v1/transit/decrypt/customer", "customer", `{}`))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "missing ciphertext to decrypt") {
		t.Fatalf("body=%s", response.Body.String())
	}
	if calls != 0 {
		t.Fatalf("transitDecryptor calls=%d, want 0 body=%s", calls, response.Body.String())
	}
}

// The single-item decrypt success path keeps its 200 and its plaintext.
func TestOpenBaoTransitDecryptAcceptsPresentCiphertext(t *testing.T) {
	calls := 0
	server := &Server{
		transitAuthorizer: allowAllTransit(),
		auditRecorder:     func(context.Context, model.AuditEvent) error { return nil },
		transitDecryptor: func(_ context.Context, _, ciphertext string) (string, error) {
			calls++
			if ciphertext != "vault:v1:a" {
				t.Fatalf("ciphertext=%q", ciphertext)
			}
			return "aGk=", nil
		},
	}
	response := httptest.NewRecorder()
	server.baoTransitDecrypt(response, transitBatchRequest("/v1/transit/decrypt/customer", "customer",
		`{"ciphertext":"vault:v1:a"}`))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if calls != 1 {
		t.Fatalf("transitDecryptor calls=%d, want 1", calls)
	}
	if data := decodeBaoData(t, response.Body.Bytes()); data["plaintext"] != "aGk=" {
		t.Fatalf("plaintext=%v", data["plaintext"])
	}
}

// batch_input without any top-level plaintext/ciphertext field is the
// documented shape (compatibility.md: batch_input ignores the single fields),
// so the new nil checks must stay behind the batch branch.
func TestOpenBaoTransitBatchWithoutTopLevelFieldStaysSupported(t *testing.T) {
	server := &Server{
		transitAuthorizer: allowAllTransit(),
		auditRecorder:     func(context.Context, model.AuditEvent) error { return nil },
		transitEncryptor: func(_ context.Context, _, plaintext, _ string) (string, error) {
			return "vault:v1:" + plaintext, nil
		},
		transitDecryptor: func(context.Context, string, string) (string, error) { return "aGk=", nil },
	}

	encrypted := httptest.NewRecorder()
	server.baoTransitEncrypt(encrypted, transitBatchRequest("/v1/transit/encrypt/customer", "customer",
		`{"batch_input":[{"plaintext":"aGk="}]}`))
	if encrypted.Code != http.StatusOK {
		t.Fatalf("encrypt status=%d body=%s", encrypted.Code, encrypted.Body.String())
	}
	results := decodeBatchResults(t, encrypted.Body.Bytes())
	if len(results) != 1 || results[0]["ciphertext"] != "vault:v1:aGk=" {
		t.Fatalf("encrypt batch_results=%v", results)
	}

	decrypted := httptest.NewRecorder()
	server.baoTransitDecrypt(decrypted, transitBatchRequest("/v1/transit/decrypt/customer", "customer",
		`{"batch_input":[{"ciphertext":"vault:v1:a"}]}`))
	if decrypted.Code != http.StatusOK {
		t.Fatalf("decrypt status=%d body=%s", decrypted.Code, decrypted.Body.String())
	}
	results = decodeBatchResults(t, decrypted.Body.Bytes())
	if len(results) != 1 || results[0]["plaintext"] != "aGk=" {
		t.Fatalf("decrypt batch_results=%v", results)
	}
}
