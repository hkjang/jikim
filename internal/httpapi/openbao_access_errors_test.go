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

// A capability lookup that could not run is a server fault. Reporting it as
// "permission denied" tells the caller its policy is wrong while the real cause
// is an unreachable database, and it hides the outage from retry logic.
func TestBaoAccessFailureSeparatesDenialFromServerFault(t *testing.T) {
	for _, test := range []struct {
		name        string
		failure     error
		wantStatus  int
		wantMessage string
	}{
		{
			name:        "database outage is a server fault",
			failure:     errors.New(`failed to connect to "host=postgres.internal": dial error`),
			wantStatus:  http.StatusInternalServerError,
			wantMessage: "failed to check permissions",
		},
		{
			name:        "missing policy target stays a denial",
			failure:     store.ErrNotFound,
			wantStatus:  http.StatusForbidden,
			wantMessage: "permission denied",
		},
		{
			name:        "forbidden stays a denial",
			failure:     store.ErrForbidden,
			wantStatus:  http.StatusForbidden,
			wantMessage: "permission denied",
		},
		{
			name:        "invalid input keeps its caller-safe reason",
			failure:     fmt.Errorf("%w: transit key 이름이 올바르지 않습니다", store.ErrInvalid),
			wantStatus:  http.StatusBadRequest,
			wantMessage: "transit key",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			status, message := baoAccessFailure(test.failure)
			if status != test.wantStatus || !strings.Contains(message, test.wantMessage) {
				t.Fatalf("status=%d message=%q", status, message)
			}
		})
	}
}

func TestOpenBaoTransitAuthorizationOutageIsServerFault(t *testing.T) {
	for _, decrypt := range []bool{false, true} {
		server := &Server{
			transitAuthorizer: func(context.Context, model.User, string, string) (bool, error) {
				return false, errors.New(`failed to connect to "host=postgres.internal user=jikim_app": SQLSTATE 28P01`)
			},
			auditRecorder: func(context.Context, model.AuditEvent) error { return nil },
		}
		response := httptest.NewRecorder()
		if decrypt {
			server.baoTransitDecrypt(response, transitBatchRequest("/v1/transit/decrypt/customer", "customer",
				`{"ciphertext":"vault:v1:a"}`))
		} else {
			server.baoTransitEncrypt(response, transitBatchRequest("/v1/transit/encrypt/customer", "customer",
				`{"plaintext":"aGk="}`))
		}
		if response.Code != http.StatusInternalServerError {
			t.Fatalf("decrypt=%t status=%d body=%s", decrypt, response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "postgres.internal") || strings.Contains(response.Body.String(), "SQLSTATE") {
			t.Fatalf("driver detail leaked to the client: %s", response.Body.String())
		}
	}
}

func TestOpenBaoTransitDenialStaysPermissionDenied(t *testing.T) {
	server := &Server{
		transitAuthorizer: func(context.Context, model.User, string, string) (bool, error) { return false, nil },
	}
	response := httptest.NewRecorder()
	server.baoTransitEncrypt(response, transitBatchRequest("/v1/transit/encrypt/customer", "customer",
		`{"plaintext":"aGk="}`))
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "permission denied") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestMCPAuthorizationOutageIsAuditedAsServerFault(t *testing.T) {
	var audited model.AuditEvent
	server := &Server{
		transitAuthorizer: func(context.Context, model.User, string, string) (bool, error) {
			return false, driverFailure
		},
		auditRecorder: func(_ context.Context, event model.AuditEvent) error {
			audited = event
			return nil
		},
	}
	body := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"transit.decrypt","arguments":{"key":"customer","ciphertext":"vault:v1:cipher"}}}`
	response := performAuthenticatedMCP(t, server, body)
	if !strings.Contains(response.Body.String(), `"isError":true`) {
		t.Fatalf("authorization outage was not reported as a tool error: %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "postgres.internal") || strings.Contains(response.Body.String(), "권한이 없습니다") {
		t.Fatalf("outage was leaked or disguised as a denial: %s", response.Body.String())
	}
	if audited.StatusCode != http.StatusInternalServerError {
		t.Fatalf("authorization outage was audited as %d, want 500", audited.StatusCode)
	}
}

func TestMCPAuthorizationDenialStaysForbidden(t *testing.T) {
	var audited model.AuditEvent
	server := &Server{
		transitAuthorizer: func(context.Context, model.User, string, string) (bool, error) { return false, nil },
		auditRecorder: func(_ context.Context, event model.AuditEvent) error {
			audited = event
			return nil
		},
	}
	body := `{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"transit.decrypt","arguments":{"key":"customer","ciphertext":"vault:v1:cipher"}}}`
	response := performAuthenticatedMCP(t, server, body)
	if !strings.Contains(response.Body.String(), "권한이 없습니다") {
		t.Fatalf("denial message changed: %s", response.Body.String())
	}
	if audited.StatusCode != http.StatusForbidden {
		t.Fatalf("denial was audited as %d, want 403", audited.StatusCode)
	}
}

func TestMCPSecretMetadataAuthorizationOutageIsServerFault(t *testing.T) {
	server := &Server{
		secretAuthorizer: func(context.Context, model.User, string, string) (bool, error) {
			return false, driverFailure
		},
	}
	body := `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"secrets.metadata","arguments":{"path":"team/api"}}}`
	response := performAuthenticatedMCP(t, server, body)
	if strings.Contains(response.Body.String(), "postgres.internal") || strings.Contains(response.Body.String(), "권한이 없습니다") {
		t.Fatalf("outage was leaked or disguised as a denial: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "도구를 실행할 수 없습니다") {
		t.Fatalf("generic failure message missing: %s", response.Body.String())
	}
}
