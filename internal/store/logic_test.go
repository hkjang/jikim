package store

import (
	"errors"
	"testing"

	"github.com/hkjang/jikim/internal/model"
)

func TestValidateSecretPath(t *testing.T) {
	valid := []string{"prod/payment/database", "DEV/app-1/API_KEY", "a.b/c_d"}
	for _, value := range valid {
		if err := ValidateSecretPath(value); err != nil {
			t.Errorf("valid path %q rejected: %v", value, err)
		}
	}
	invalid := []string{"", "/root", "root/", "a//b", "a/../b", "white space"}
	for _, value := range invalid {
		if err := ValidateSecretPath(value); !errors.Is(err, ErrInvalid) {
			t.Errorf("invalid path %q error = %v", value, err)
		}
	}
}

func TestPathMatchesAndCapabilities(t *testing.T) {
	tests := []struct {
		pattern, value string
		want           bool
	}{{"*", "anything/here", true}, {"prod/payment/*", "prod/payment/db", true}, {"prod/payment/*", "prod/other/db", false}, {"exact/path", "exact/path", true}}
	for _, test := range tests {
		if got := pathMatches(test.pattern, test.value); got != test.want {
			t.Errorf("pathMatches(%q,%q)=%v want %v", test.pattern, test.value, got, test.want)
		}
	}
}

func TestRiskScoreDoesNotInspectValues(t *testing.T) {
	score := riskScore(model.SecretWrite{Path: "prod/payment", Data: map[string]any{"admin_password": "plaintext-not-used"}})
	if score != 65 {
		t.Fatalf("risk score = %d, want 65", score)
	}
}

func TestEscapeLikeNeutralisesWildcards(t *testing.T) {
	tests := []struct{ value, want string }{
		{"prod/payment", "prod/payment"},
		{"app_1", `app\_1`},
		{"100%", `100\%`},
		{`back\slash`, `back\\slash`},
		{`%_\`, `\%\_\\`},
	}
	for _, test := range tests {
		if got := escapeLike(test.value); got != test.want {
			t.Errorf("escapeLike(%q)=%q want %q", test.value, got, test.want)
		}
	}
}

func TestSecretChildrenPatternScopesToPrefix(t *testing.T) {
	tests := []struct{ prefix, want string }{
		{"", "%"},
		{"prod", "prod/%"},
		{"app_1", `app\_1/%`},
		{"%", `\%/%`},
	}
	for _, test := range tests {
		if got := secretChildrenPattern(test.prefix); got != test.want {
			t.Errorf("secretChildrenPattern(%q)=%q want %q", test.prefix, got, test.want)
		}
	}
}
