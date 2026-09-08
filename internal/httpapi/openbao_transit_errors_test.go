package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hkjang/jikim/internal/model"
	"github.com/hkjang/jikim/internal/store"
)

// A store failure must not be flattened into "400 + raw error": a database or
// crypto fault is a server fault and its detail must never reach the client.
func TestOpenBaoTransitSingleMapsStoreErrorsToStatus(t *testing.T) {
	for _, test := range []struct {
		name       string
		failure    error
		wantStatus int
		wantBody   string
		notInBody  string
		decrypt    bool
	}{
		{
			name:       "encrypt invalid input stays a request error",
			failure:    fmt.Errorf("%w: plaintext는 base64여야 합니다", store.ErrInvalid),
			wantStatus: http.StatusBadRequest,
			wantBody:   "base64",
		},
		{
			name:       "encrypt database outage is a server fault",
			failure:    errors.New(`ERROR: relation "transit_keys" does not exist (SQLSTATE 42P01)`),
			wantStatus: http.StatusInternalServerError,
			wantBody:   "failed to encrypt plaintext",
			notInBody:  "SQLSTATE",
		},
		{
			name:       "decrypt of a missing key stays a request error",
			failure:    store.ErrNotFound,
			wantStatus: http.StatusBadRequest,
			wantBody:   "encryption key not found",
			decrypt:    true,
		},
		{
			name:       "decrypt database outage is a server fault",
			failure:    errors.New("failed to connect to `host=db`: dial error"),
			wantStatus: http.StatusInternalServerError,
			wantBody:   "failed to decrypt ciphertext",
			notInBody:  "host=db",
			decrypt:    true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := &Server{
				transitAuthorizer: allowAllTransit(),
				auditRecorder:     func(context.Context, model.AuditEvent) error { return nil },
				transitEncryptor: func(context.Context, string, string, string) (string, error) {
					return "", test.failure
				},
				transitDecryptor: func(context.Context, string, string) (string, error) {
					return "", test.failure
				},
			}
			response := httptest.NewRecorder()
			if test.decrypt {
				server.baoTransitDecrypt(response, transitBatchRequest("/v1/transit/decrypt/customer", "customer",
					`{"ciphertext":"vault:v1:a"}`))
			} else {
				server.baoTransitEncrypt(response, transitBatchRequest("/v1/transit/encrypt/customer", "customer",
					`{"plaintext":"aGk="}`))
			}
			if response.Code != test.wantStatus || !strings.Contains(response.Body.String(), test.wantBody) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if test.notInBody != "" && strings.Contains(response.Body.String(), test.notInBody) {
				t.Fatalf("internal error detail leaked: %s", response.Body.String())
			}
		})
	}
}

// A batch whose every item failed for a server-side reason must report the
// server fault rather than blaming the caller with 400.
func TestOpenBaoTransitBatchReportsServerFaults(t *testing.T) {
	server := &Server{
		transitAuthorizer: allowAllTransit(),
		auditRecorder:     func(context.Context, model.AuditEvent) error { return nil },
		transitEncryptor: func(_ context.Context, _, plaintext, _ string) (string, error) {
			if plaintext == "!!" {
				return "", fmt.Errorf("%w: plaintext는 base64여야 합니다", store.ErrInvalid)
			}
			return "", errors.New(`ERROR: connection refused (SQLSTATE 08006)`)
		},
	}
	response := httptest.NewRecorder()
	server.baoTransitEncrypt(response, transitBatchRequest("/v1/transit/encrypt/customer", "customer",
		`{"batch_input":[{"plaintext":"!!"},{"plaintext":"aGk="}]}`))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "SQLSTATE") {
		t.Fatalf("internal error detail leaked: %s", response.Body.String())
	}
	results := decodeBatchResults(t, response.Body.Bytes())
	if len(results) != 2 {
		t.Fatalf("batch_results=%v", results)
	}
	if message, _ := results[0]["error"].(string); !strings.Contains(message, "base64") {
		t.Fatalf("first result=%v", results[0])
	}
	if message, _ := results[1]["error"].(string); message != "failed to encrypt plaintext" {
		t.Fatalf("second result=%v", results[1])
	}
}

// A batch that still produced a plaintext keeps 200 even though another item
// hit a server fault, so partial success stays the OpenBao contract.
func TestOpenBaoTransitBatchKeepsPartialSuccessOnServerFault(t *testing.T) {
	server := &Server{
		transitAuthorizer: allowAllTransit(),
		auditRecorder:     func(context.Context, model.AuditEvent) error { return nil },
		transitDecryptor: func(_ context.Context, _, ciphertext string) (string, error) {
			if ciphertext == "vault:v1:boom" {
				return "", errors.New("dial tcp 10.0.0.5:5432: connect: connection refused")
			}
			return "aGk=", nil
		},
	}
	response := httptest.NewRecorder()
	server.baoTransitDecrypt(response, transitBatchRequest("/v1/transit/decrypt/customer", "customer",
		`{"batch_input":[{"ciphertext":"vault:v1:boom"},{"ciphertext":"vault:v1:ok"}]}`))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "10.0.0.5") {
		t.Fatalf("internal error detail leaked: %s", response.Body.String())
	}
	results := decodeBatchResults(t, response.Body.Bytes())
	if len(results) != 2 {
		t.Fatalf("batch_results=%v", results)
	}
	if message, _ := results[0]["error"].(string); message != "failed to decrypt ciphertext" {
		t.Fatalf("first result=%v", results[0])
	}
	if results[1]["plaintext"] != "aGk=" {
		t.Fatalf("second result=%v", results[1])
	}
}
