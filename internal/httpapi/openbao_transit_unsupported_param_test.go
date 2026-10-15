package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hkjang/jikim/internal/model"
)

// The batch path refuses context, nonce, associated_data and a non-zero
// key_version as a per-item error so a caller never receives a result that
// silently dropped the derivation context it asked for. The single-item path
// must refuse the same parameters with the same message instead of encrypting
// without them.
func TestOpenBaoTransitEncryptRejectsUnsupportedParameters(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		want string
	}{
		{name: "context", body: `{"plaintext":"aGk=","context":"Y3R4"}`, want: "unsupported transit parameter: context"},
		{name: "nonce", body: `{"plaintext":"aGk=","nonce":"bm9uY2U="}`, want: "unsupported transit parameter: nonce"},
		{name: "associated_data", body: `{"plaintext":"aGk=","associated_data":"YWFk"}`, want: "unsupported transit parameter: associated_data"},
		{name: "key_version", body: `{"plaintext":"aGk=","key_version":2}`, want: "unsupported transit parameter: key_version"},
		// The unsupported check runs before the missing-field check, as it does
		// in the batch loop, so both paths answer the same body identically.
		{name: "context without plaintext", body: `{"context":"Y3R4"}`, want: "unsupported transit parameter: context"},
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
			if !strings.Contains(response.Body.String(), test.want) {
				t.Fatalf("body=%s, want %q", response.Body.String(), test.want)
			}
			if calls != 0 {
				t.Fatalf("transitEncryptor calls=%d, want 0 body=%s", calls, response.Body.String())
			}
		})
	}
}

// The same four parameters are refused on the single-item decrypt path.
func TestOpenBaoTransitDecryptRejectsUnsupportedParameters(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		want string
	}{
		{name: "context", body: `{"ciphertext":"vault:v1:a","context":"Y3R4"}`, want: "unsupported transit parameter: context"},
		{name: "nonce", body: `{"ciphertext":"vault:v1:a","nonce":"bm9uY2U="}`, want: "unsupported transit parameter: nonce"},
		{name: "associated_data", body: `{"ciphertext":"vault:v1:a","associated_data":"YWFk"}`, want: "unsupported transit parameter: associated_data"},
		{name: "key_version", body: `{"ciphertext":"vault:v1:a","key_version":3}`, want: "unsupported transit parameter: key_version"},
		{name: "nonce without ciphertext", body: `{"nonce":"bm9uY2U="}`, want: "unsupported transit parameter: nonce"},
	} {
		t.Run(test.name, func(t *testing.T) {
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
			server.baoTransitDecrypt(response, transitBatchRequest("/v1/transit/decrypt/customer", "customer", test.body))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), test.want) {
				t.Fatalf("body=%s, want %q", response.Body.String(), test.want)
			}
			if calls != 0 {
				t.Fatalf("transitDecryptor calls=%d, want 0 body=%s", calls, response.Body.String())
			}
		})
	}
}

// An empty string context/nonce/associated_data and key_version 0 mean "not
// asked for", exactly as unsupportedParameter() already decides for batch
// items, so the single-item path keeps its 200.
func TestOpenBaoTransitSingleItemAcceptsEmptyUnsupportedParameters(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "empty context", body: `{"plaintext":"aGk=","context":""}`},
		{name: "empty nonce", body: `{"plaintext":"aGk=","nonce":""}`},
		{name: "empty associated_data", body: `{"plaintext":"aGk=","associated_data":""}`},
		{name: "zero key_version", body: `{"plaintext":"aGk=","key_version":0}`},
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
			if data := decodeBaoData(t, response.Body.Bytes()); data["ciphertext"] != "vault:v1:aGk=" {
				t.Fatalf("ciphertext=%v", data["ciphertext"])
			}
		})
	}
}

// batch_input ignores the single-item fields (compatibility.md), so a batch
// request that also carries a top-level context must still succeed: the new
// check has to stay behind the batch branch.
func TestOpenBaoTransitBatchIgnoresTopLevelUnsupportedParameters(t *testing.T) {
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
		`{"batch_input":[{"plaintext":"aGk="}],"context":"Y3R4"}`))
	if encrypted.Code != http.StatusOK {
		t.Fatalf("encrypt status=%d body=%s", encrypted.Code, encrypted.Body.String())
	}
	results := decodeBatchResults(t, encrypted.Body.Bytes())
	if len(results) != 1 || results[0]["ciphertext"] != "vault:v1:aGk=" {
		t.Fatalf("encrypt batch_results=%v", results)
	}

	decrypted := httptest.NewRecorder()
	server.baoTransitDecrypt(decrypted, transitBatchRequest("/v1/transit/decrypt/customer", "customer",
		`{"batch_input":[{"ciphertext":"vault:v1:a"}],"key_version":4}`))
	if decrypted.Code != http.StatusOK {
		t.Fatalf("decrypt status=%d body=%s", decrypted.Code, decrypted.Body.String())
	}
	results = decodeBatchResults(t, decrypted.Body.Bytes())
	if len(results) != 1 || results[0]["plaintext"] != "aGk=" {
		t.Fatalf("decrypt batch_results=%v", results)
	}
}
