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

func TestSecuritySettingsRejectMistypedValues(t *testing.T) {
	rejected := []struct {
		name  string
		value map[string]any
	}{
		{"string allow_local_login", map[string]any{"allow_local_login": "false"}},
		{"string require_password_change", map[string]any{"require_password_change": "true"}},
		{"string session_timeout_minutes", map[string]any{"session_timeout_minutes": "60"}},
		{"fractional password_min_length", map[string]any{"password_min_length": 12.5}},
		{"string audit_retention_days", map[string]any{"audit_retention_days": "180"}},
		{"non-string allowed_networks", map[string]any{"allowed_networks": []any{"10.0.0.0/8"}}},
		{"out of range session_timeout_minutes", map[string]any{"session_timeout_minutes": 4}},
	}
	for _, item := range rejected {
		if err := ValidateSetting("security", item.value); err == nil {
			t.Fatalf("security 설정이 잘못된 값을 수용했습니다: %s", item.name)
		}
	}
	if err := ValidateSetting("security", map[string]any{
		"allow_local_login": false, "require_password_change": true,
		"session_timeout_minutes": float64(720), "password_min_length": float64(16),
		"audit_retention_days": float64(180), "allowed_networks": "10.10.0.0/16",
	}); err != nil {
		t.Fatalf("정상 security 설정이 거부되었습니다: %v", err)
	}
	if err := ValidateSetting("security", map[string]any{}); err != nil {
		t.Fatalf("빈 security 설정이 거부되었습니다: %v", err)
	}
}

func TestTrustedProxiesValidationAndParsing(t *testing.T) {
	for _, raw := range []string{"10.10.0.0/16, nope", "10.10.0.0/64", "10.10.0.0-10.10.0.9"} {
		if err := ValidateSetting("security", map[string]any{"trusted_proxies": raw}); err == nil {
			t.Fatalf("잘못된 trusted_proxies가 수용되었습니다: %s", raw)
		}
	}
	if err := ValidateSetting("security", map[string]any{"trusted_proxies": []any{"10.10.0.0/16"}}); err == nil {
		t.Fatal("trusted_proxies의 배열 값이 수용되었습니다")
	}
	if err := ValidateSetting("security", map[string]any{"trusted_proxies": "10.10.0.0/16 192.0.2.10, 2001:db8::/32"}); err != nil {
		t.Fatalf("정상 trusted_proxies가 거부되었습니다: %v", err)
	}
	empty, err := ParseTrustedProxies("  ")
	if err != nil || len(empty) != 0 {
		t.Fatalf("ParseTrustedProxies(빈 값) = %v, %v", empty, err)
	}
	prefixes, err := ParseTrustedProxies("10.10.0.5/16, 192.0.2.10\n2001:db8::/32")
	if err != nil {
		t.Fatalf("ParseTrustedProxies() error = %v", err)
	}
	want := []string{"10.10.0.0/16", "192.0.2.10/32", "2001:db8::/32"}
	if len(prefixes) != len(want) {
		t.Fatalf("ParseTrustedProxies() = %v, want %v", prefixes, want)
	}
	for index, expected := range want {
		if prefixes[index].String() != expected {
			t.Fatalf("prefix %d = %s, want %s", index, prefixes[index], expected)
		}
	}
}

func TestGeneralAndAISettingsRejectMistypedValues(t *testing.T) {
	if err := ValidateSetting("service", map[string]any{"service_name": 42}); err == nil {
		t.Fatal("service_name의 숫자 값이 수용되었습니다")
	}
	if err := ValidateSetting("service", map[string]any{
		"service_name": "jikim", "default_language": "ko", "timezone": "Asia/Seoul",
	}); err != nil {
		t.Fatalf("정상 service 설정이 거부되었습니다: %v", err)
	}
	if err := ValidateSetting("ai", map[string]any{"max_tokens": "4096"}); err == nil {
		t.Fatal("max_tokens의 문자열 값이 수용되었습니다")
	}
	if err := ValidateSetting("ai", map[string]any{"timeout_seconds": "600"}); err == nil {
		t.Fatal("timeout_seconds의 문자열 값이 수용되었습니다")
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
