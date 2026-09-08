package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hkjang/jikim/internal/model"
	"github.com/hkjang/jikim/internal/store"
)

// driverFailure stands in for the pgx errors a store call returns when the
// database is unreachable: the text carries DSN and SQLSTATE detail.
var driverFailure = fmt.Errorf(`failed to connect to "host=postgres.internal user=jikim_app": SQLSTATE 28P01`)

func TestMCPEncryptHidesStoreInternals(t *testing.T) {
	server := &Server{
		transitAuthorizer: func(context.Context, model.User, string, string) (bool, error) { return true, nil },
		transitEncryptor: func(context.Context, string, string, string) (string, error) {
			return "", driverFailure
		},
	}
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"transit.encrypt","arguments":{"key":"customer","plaintext":"aGk="}}}`
	response := performAuthenticatedMCP(t, server, body)
	if !strings.Contains(response.Body.String(), `"isError":true`) {
		t.Fatalf("store failure was not reported as a tool error: %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "postgres.internal") || strings.Contains(response.Body.String(), "SQLSTATE") {
		t.Fatalf("driver detail leaked to the MCP client: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "도구를 실행할 수 없습니다") {
		t.Fatalf("generic failure message missing: %s", response.Body.String())
	}
}

func TestMCPDecryptServerFaultIsAuditedAsServerFault(t *testing.T) {
	var audited model.AuditEvent
	server := &Server{
		transitAuthorizer: func(context.Context, model.User, string, string) (bool, error) { return true, nil },
		transitDecryptor: func(context.Context, string, string) (string, error) {
			return "", driverFailure
		},
		auditRecorder: func(_ context.Context, event model.AuditEvent) error {
			audited = event
			return nil
		},
	}
	body := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"transit.decrypt","arguments":{"key":"customer","ciphertext":"vault:v1:cipher"}}}`
	response := performAuthenticatedMCP(t, server, body)
	if strings.Contains(response.Body.String(), "postgres.internal") {
		t.Fatalf("driver detail leaked to the MCP client: %s", response.Body.String())
	}
	if audited.StatusCode != http.StatusInternalServerError || audited.Success {
		t.Fatalf("server fault was audited as %d success=%t, want 500 failure", audited.StatusCode, audited.Success)
	}
}

func TestMCPDecryptKeepsInvalidInputReason(t *testing.T) {
	var audited model.AuditEvent
	server := &Server{
		transitAuthorizer: func(context.Context, model.User, string, string) (bool, error) { return true, nil },
		transitDecryptor: func(context.Context, string, string) (string, error) {
			return "", fmt.Errorf("%w: transit ciphertext 형식이 올바르지 않습니다", store.ErrInvalid)
		},
		auditRecorder: func(_ context.Context, event model.AuditEvent) error {
			audited = event
			return nil
		},
	}
	body := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"transit.decrypt","arguments":{"key":"customer","ciphertext":"not-a-ciphertext"}}}`
	response := performAuthenticatedMCP(t, server, body)
	if !strings.Contains(response.Body.String(), "transit ciphertext") {
		t.Fatalf("caller-fixable reason was withheld: %s", response.Body.String())
	}
	if audited.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid input was audited as %d, want 400", audited.StatusCode)
	}
}

func TestMCPToolFailureKeepsDispatchSentinels(t *testing.T) {
	cases := []struct {
		err     error
		status  int
		message string
	}{
		{storeForbidden(), http.StatusForbidden, "권한이 없습니다"},
		{storeNotFound(), http.StatusNotFound, "대상을 찾을 수 없습니다"},
		{auditUnavailable(), http.StatusInternalServerError, "감사 로그를 저장할 수 없어 복호화 결과를 표시하지 않습니다"},
		{store.ErrConflict, http.StatusConflict, store.ErrConflict.Error()},
		{store.ErrNotFound, http.StatusNotFound, store.ErrNotFound.Error()},
		{driverFailure, http.StatusInternalServerError, "도구를 실행할 수 없습니다"},
	}
	for _, testCase := range cases {
		status, message := mcpToolFailure(testCase.err)
		if status != testCase.status || message != testCase.message {
			t.Fatalf("mcpToolFailure(%v) = %d %q, want %d %q", testCase.err, status, message, testCase.status, testCase.message)
		}
	}
}
