package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hkjang/jikim/internal/model"
)

func transitBatchRequest(target, key, body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	request.SetPathValue("key", key)
	return request.WithContext(context.WithValue(request.Context(), sessionKey,
		model.Session{User: model.User{ID: "user-1", Username: "hong", Role: "manager"}}))
}

func decodeBatchResults(t *testing.T, body []byte) []map[string]any {
	t.Helper()
	var envelope struct {
		Data struct {
			BatchResults []map[string]any `json:"batch_results"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode batch response: %v body=%s", err, body)
	}
	return envelope.Data.BatchResults
}

func allowAllTransit() func(context.Context, model.User, string, string) (bool, error) {
	return func(context.Context, model.User, string, string) (bool, error) { return true, nil }
}

func TestOpenBaoTransitEncryptBatchKeepsOrderAndReportsItemErrors(t *testing.T) {
	server := &Server{
		transitAuthorizer: allowAllTransit(),
		transitEncryptor: func(_ context.Context, key, plaintext, actorID string) (string, error) {
			if key != "customer" || actorID != "user-1" {
				return "", fmt.Errorf("unexpected key=%q actor=%q", key, actorID)
			}
			if plaintext == "!!" {
				return "", errors.New("plaintext는 base64여야 합니다")
			}
			return "vault:v2:" + plaintext, nil
		},
	}
	body := `{"batch_input":[{"plaintext":"aGVsbG8=","reference":"first"},{"plaintext":"!!"},` +
		`{"reference":"third"},{"plaintext":"aGk=","context":"Y3R4"}]}`
	response := httptest.NewRecorder()
	server.baoTransitEncrypt(response, transitBatchRequest("/v1/transit/encrypt/customer", "customer", body))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	results := decodeBatchResults(t, response.Body.Bytes())
	if len(results) != 4 {
		t.Fatalf("batch_results=%v", results)
	}
	if results[0]["ciphertext"] != "vault:v2:aGVsbG8=" || results[0]["reference"] != "first" {
		t.Fatalf("first result=%v", results[0])
	}
	if version, ok := results[0]["key_version"].(float64); !ok || int(version) != 2 {
		t.Fatalf("first key_version=%v", results[0]["key_version"])
	}
	if _, ok := results[1]["ciphertext"]; ok {
		t.Fatalf("failed item returned ciphertext: %v", results[1])
	}
	if message, _ := results[1]["error"].(string); !strings.Contains(message, "base64") {
		t.Fatalf("second result=%v", results[1])
	}
	if message, _ := results[2]["error"].(string); message != "missing plaintext to encrypt" || results[2]["reference"] != "third" {
		t.Fatalf("third result=%v", results[2])
	}
	if message, _ := results[3]["error"].(string); !strings.Contains(message, "context") {
		t.Fatalf("unsupported context was not rejected: %v", results[3])
	}
}

func TestOpenBaoTransitEncryptBatchRejectsEmptyAndFullyFailedBatches(t *testing.T) {
	server := &Server{
		transitAuthorizer: allowAllTransit(),
		transitEncryptor: func(context.Context, string, string, string) (string, error) {
			return "", errors.New("plaintext는 base64여야 합니다")
		},
	}
	empty := httptest.NewRecorder()
	server.baoTransitEncrypt(empty, transitBatchRequest("/v1/transit/encrypt/customer", "customer", `{"batch_input":[]}`))
	if empty.Code != http.StatusBadRequest || !strings.Contains(empty.Body.String(), "missing batch input") {
		t.Fatalf("empty batch: status=%d body=%s", empty.Code, empty.Body.String())
	}
	failed := httptest.NewRecorder()
	server.baoTransitEncrypt(failed, transitBatchRequest("/v1/transit/encrypt/customer", "customer",
		`{"batch_input":[{"plaintext":"!!"}]}`))
	if failed.Code != http.StatusBadRequest {
		t.Fatalf("fully failed batch: status=%d body=%s", failed.Code, failed.Body.String())
	}
	if results := decodeBatchResults(t, failed.Body.Bytes()); len(results) != 1 || results[0]["error"] == nil {
		t.Fatalf("fully failed batch results=%v", results)
	}
}

func TestOpenBaoTransitDecryptBatchKeepsOrderAndReportsItemErrors(t *testing.T) {
	server := &Server{
		transitAuthorizer: allowAllTransit(),
		transitDecryptor: func(_ context.Context, _, ciphertext string) (string, error) {
			if ciphertext == "vault:v1:bad" {
				return "", errors.New("ciphertext 형식")
			}
			return base64.StdEncoding.EncodeToString([]byte(ciphertext)), nil
		},
		auditRecorder: func(context.Context, model.AuditEvent) error { return nil },
	}
	body := `{"batch_input":[{"ciphertext":"vault:v1:ok","reference":"a"},{"ciphertext":"vault:v1:bad"},` +
		`{},{"ciphertext":"vault:v1:ok","key_version":2}]}`
	response := httptest.NewRecorder()
	server.baoTransitDecrypt(response, transitBatchRequest("/v1/transit/decrypt/customer", "customer", body))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	results := decodeBatchResults(t, response.Body.Bytes())
	if len(results) != 4 {
		t.Fatalf("batch_results=%v", results)
	}
	want := base64.StdEncoding.EncodeToString([]byte("vault:v1:ok"))
	if results[0]["plaintext"] != want || results[0]["reference"] != "a" {
		t.Fatalf("first result=%v", results[0])
	}
	if _, ok := results[1]["plaintext"]; ok || results[1]["error"] == nil {
		t.Fatalf("second result=%v", results[1])
	}
	if message, _ := results[2]["error"].(string); message != "missing ciphertext to decrypt" {
		t.Fatalf("third result=%v", results[2])
	}
	if message, _ := results[3]["error"].(string); !strings.Contains(message, "key_version") {
		t.Fatalf("unsupported key_version was not rejected: %v", results[3])
	}
}

func TestOpenBaoTransitDecryptBatchWithholdsEveryPlaintextWhenAuditFails(t *testing.T) {
	audited := 0
	server := &Server{
		transitAuthorizer: allowAllTransit(),
		transitDecryptor:  func(context.Context, string, string) (string, error) { return "aGVsbG8=", nil },
		auditRecorder: func(context.Context, model.AuditEvent) error {
			audited++
			return errors.New("audit offline")
		},
	}
	response := httptest.NewRecorder()
	server.baoTransitDecrypt(response, transitBatchRequest("/v1/transit/decrypt/customer", "customer",
		`{"batch_input":[{"ciphertext":"vault:v1:a"},{"ciphertext":"vault:v1:b"}]}`))
	if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "aGVsbG8=") {
		t.Fatalf("plaintext was not withheld: status=%d body=%s", response.Code, response.Body.String())
	}
	if audited != 1 {
		t.Fatalf("batch decrypt recorded %d audit events, want 1", audited)
	}
}

func TestOpenBaoTransitBatchRequiresAuthorization(t *testing.T) {
	server := &Server{transitAuthorizer: func(context.Context, model.User, string, string) (bool, error) {
		return false, nil
	}}
	for _, test := range []struct {
		name, target, body string
		handler            http.HandlerFunc
	}{
		{"encrypt", "/v1/transit/encrypt/customer", `{"batch_input":[{"plaintext":"aGk="}]}`, server.baoTransitEncrypt},
		{"decrypt", "/v1/transit/decrypt/customer", `{"batch_input":[{"ciphertext":"vault:v1:a"}]}`, server.baoTransitDecrypt},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			test.handler(response, transitBatchRequest(test.target, "customer", test.body))
			if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "permission denied") {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}
