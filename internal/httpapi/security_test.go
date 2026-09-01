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

func TestValidateAIInputMessagesAndTokenLimit(t *testing.T) {
	if err := ValidateAIInput(aiChatInput{Messages: []aiMessage{{Role: "user", Content: "위험도 요약을 설명해줘"}}, MaxTokens: maximumAITokens}); err != nil {
		t.Fatalf("valid messages rejected: %v", err)
	}
	if err := ValidateAIInput(aiChatInput{Messages: []aiMessage{{Role: "tool", Content: "x"}}}); err == nil {
		t.Fatal("unsupported role accepted")
	}
	if err := ValidateAIInput(aiChatInput{Prompt: "password=super-secret-value"}); err == nil {
		t.Fatal("secret material accepted")
	}
	if err := ValidateAIInput(aiChatInput{Prompt: "hello", MaxTokens: maximumAITokens + 1}); err == nil {
		t.Fatal("oversized max_tokens accepted")
	}
	for _, secret := range []string{
		"AWS credential " + "AKIA" + "1234567890ABCDEF" + "를 분석해줘",
		`context: {"password":"super-secret-value"}`,
		"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTYifQ.abcdefghijklmnop",
	} {
		if err := ValidateAIInput(aiChatInput{Prompt: secret}); err == nil {
			t.Fatalf("secret-like input accepted: %q", secret)
		}
	}
	// Client-provided context is accepted for wire compatibility but is never forwarded.
	if err := ValidateAIInput(aiChatInput{Prompt: "검토"}); err != nil {
		t.Fatalf("safe prompt rejected: %v", err)
	}
}

func TestServerAIContextContainsOnlyServerOwnedAggregates(t *testing.T) {
	context := serverAIContext("user", model.Dashboard{
		Secrets: 9, Applications: 3, Users: 4, Policies: 2, Keys: 5,
		PendingApprovals: 1, HighRiskSecrets: 2,
		RecentAudit: []model.AuditEvent{{Username: "must-not-leak", Resource: "prod/secret"}},
	})
	encoded, err := json.Marshal(context)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "must-not-leak") || strings.Contains(string(encoded), "prod/secret") {
		t.Fatalf("audit detail leaked into AI context: %s", encoded)
	}
	if context.Inventory["secrets"] != 9 || context.RiskSummary["high_risk_secrets"] != 2 {
		t.Fatalf("server aggregate context is incomplete: %#v", context)
	}
}

func TestChatCompletionsURL(t *testing.T) {
	tests := map[string]string{
		"https://ai.example":                     "https://ai.example/v1/chat/completions",
		"https://ai.example/v1":                  "https://ai.example/v1/chat/completions",
		"https://ai.example/v1/chat/completions": "https://ai.example/v1/chat/completions",
	}
	for input, want := range tests {
		got, err := chatCompletionsURL(input)
		if err != nil || got != want {
			t.Errorf("chatCompletionsURL(%q)=%q,%v want %q", input, got, err, want)
		}
	}
}

func TestOIDCRedirectAndRoleMapping(t *testing.T) {
	if !safeFrontendRedirect("/oidc/callback") || safeFrontendRedirect("https://evil.example/callback") || safeFrontendRedirect("//evil.example") {
		t.Fatal("frontend redirect validation failed")
	}
	claims := map[string]any{"realm_access": map[string]any{"roles": []any{"jikim-manager"}}}
	if got := oidcRole(claims, "realm_access.roles"); got != "manager" {
		t.Fatalf("oidcRole = %q", got)
	}
	if role, sync := synchronizedOIDCRole(map[string]any{}, "realm_access.roles"); !sync || role != "user" {
		t.Fatalf("missing configured role claim did not downgrade to user: role=%q sync=%v", role, sync)
	}
	if _, sync := synchronizedOIDCRole(map[string]any{}, ""); sync {
		t.Fatal("role synchronization enabled without a configured role claim")
	}
	groupClaims := map[string]any{"groups": []any{"/platform/jikim-auditor"}}
	if role, sync := synchronizedOIDCRoleClaims(groupClaims, "realm_access.roles", "groups"); !sync || role != "auditor" {
		t.Fatalf("group claim mapping failed: role=%q sync=%v", role, sync)
	}
	if role, sync := synchronizedOIDCRoleClaims(map[string]any{}, "", "groups"); !sync || role != "user" {
		t.Fatalf("removed group claim did not downgrade: role=%q sync=%v", role, sync)
	}
	request := httptest.NewRequest(http.MethodGet, "https://jikim.example/api/v1/oidc/login", nil)
	if got, ok := normalizeFrontendRedirect(request, "https://jikim.example/oidc/callback"); !ok || got == "" {
		t.Fatal("same-origin absolute redirect rejected")
	}
	if _, ok := normalizeFrontendRedirect(request, "https://evil.example/oidc/callback"); ok {
		t.Fatal("cross-origin redirect accepted")
	}
}

func TestOpenBaoTransitEndpointsDenyWithoutAuthorization(t *testing.T) {
	server := &Server{transitAuthorizer: func(context.Context, model.User, string, string) (bool, error) {
		return false, nil
	}}
	tests := []struct {
		name, target, body string
		handler            http.HandlerFunc
	}{{"encrypt", "/v1/transit/encrypt/customer", `{"plaintext":"aGVsbG8="}`, server.baoTransitEncrypt},
		{"decrypt", "/v1/transit/decrypt/customer", `{"ciphertext":"vault:v1:bad"}`, server.baoTransitDecrypt}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, test.target, strings.NewReader(test.body))
			request.SetPathValue("key", "customer")
			request = request.WithContext(context.WithValue(request.Context(), sessionKey, model.Session{User: model.User{ID: "user-1", Role: "user"}}))
			response := httptest.NewRecorder()
			test.handler(response, request)
			if response.Code != http.StatusForbidden {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), "permission denied") {
				t.Fatalf("unexpected body: %s", response.Body.String())
			}
		})
	}
}

func TestDashboardRedactsAuditForRegularUser(t *testing.T) {
	value := model.Dashboard{RecentAudit: []model.AuditEvent{{Username: "another-user", Resource: "prod/secret"}}}
	if got := dashboardForRole(value, "user"); len(got.RecentAudit) != 0 {
		t.Fatal("regular user received recent audit events")
	}
	if got := dashboardForRole(value, "auditor"); len(got.RecentAudit) != 1 {
		t.Fatal("auditor did not receive permitted audit events")
	}
}

func TestApprovalReviewerRole(t *testing.T) {
	tests := []struct {
		configured string
		actual     string
		want       bool
	}{
		{configured: "admin", actual: "admin", want: true},
		{configured: "admin", actual: "manager", want: false},
		{configured: "manager", actual: "manager", want: true},
		{configured: "manager", actual: "admin", want: true},
		{configured: "manager", actual: "user", want: false},
	}
	for _, test := range tests {
		if got := approvalReviewerAllowed(test.configured, test.actual); got != test.want {
			t.Errorf("configured=%q actual=%q got=%v want=%v", test.configured, test.actual, got, test.want)
		}
	}
}

func TestTransitManagementAdminOverride(t *testing.T) {
	if !transitManagementAllowed("admin", false) {
		t.Fatal("admin cannot recover a transit key with disabled management permission")
	}
	if transitManagementAllowed("manager", false) {
		t.Fatal("manager bypassed stored transit key permission")
	}
	if !transitManagementAllowed("manager", true) {
		t.Fatal("manager with stored permission was rejected")
	}
}

func TestTransitCiphertextVersion(t *testing.T) {
	if got, err := transitCiphertextVersion("vault:v7:encoded"); err != nil || got != 7 {
		t.Fatalf("version=%d error=%v", got, err)
	}
	for _, value := range []string{"", "vault:v0:data", "vault:bad:data"} {
		if _, err := transitCiphertextVersion(value); err == nil {
			t.Fatalf("invalid ciphertext accepted: %q", value)
		}
	}
}
