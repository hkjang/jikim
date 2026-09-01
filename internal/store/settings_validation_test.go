package store

import "testing"

func TestIntegrationSettingsRequireCompleteSecureConfiguration(t *testing.T) {
	if err := ValidateSetting("oidc", map[string]any{"enabled": true, "issuer_url": "https://id.example/realms/jikim", "client_id": "jikim"}); err == nil {
		t.Fatal("enabled OIDC without redirect_url was accepted")
	}
	if err := ValidateSetting("oidc", map[string]any{
		"enabled": true, "issuer_url": "https://id.example/realms/jikim", "client_id": "jikim",
		"redirect_url": "https://jikim.example/api/v1/oidc/callback", "scopes": []any{"openid", "profile"},
	}); err != nil {
		t.Fatalf("valid OIDC rejected: %v", err)
	}
	if err := ValidateSetting("ai", map[string]any{"enabled": true, "base_url": "http://10.0.0.5:8000", "model": "local"}); err == nil {
		t.Fatal("insecure non-loopback AI URL was accepted without opt-in")
	}
	if err := ValidateSetting("ai", map[string]any{
		"enabled": true, "base_url": "http://10.0.0.5:8000", "model": "local", "allow_insecure_http": true,
	}); err != nil {
		t.Fatalf("explicit internal HTTP opt-in rejected: %v", err)
	}
	if err := ValidateSetting("notifications", map[string]any{"enabled": true, "events": []any{"unknown.event"}}); err == nil {
		t.Fatal("unsupported webhook event was accepted")
	}
}

func TestWebhookURLValidation(t *testing.T) {
	if err := ValidateWebhookURL("https://hooks.example/jikim?tenant=one", false); err != nil {
		t.Fatalf("valid HTTPS webhook rejected: %v", err)
	}
	if err := ValidateWebhookURL("http://hooks.example/jikim", false); err == nil {
		t.Fatal("insecure webhook accepted without opt-in")
	}
	if err := ValidateWebhookURL("http://127.0.0.1:8080/jikim", false); err != nil {
		t.Fatalf("loopback development webhook rejected: %v", err)
	}
}
