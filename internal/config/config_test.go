package config

import (
	"strings"
	"testing"
)

func TestLoadRequiresExactlyBootstrapInputs(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://jikim:test@db/jikim")
	t.Setenv("BOOTSTRAP_ADMIN", "admin")
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "correct-horse-battery")
	t.Setenv("ENCRYPTION_KEY", strings.Repeat("k", 32))
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.PostgresDSN == "" || cfg.BootstrapAdmin != "admin" || cfg.BootstrapPassword == "" || cfg.EncryptionKey == "" {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}

func TestLoadReportsAllMissingInputs(t *testing.T) {
	for _, key := range []string{"POSTGRES_DSN", "BOOTSTRAP_ADMIN", "BOOTSTRAP_ADMIN_PASSWORD", "ENCRYPTION_KEY"} {
		t.Setenv(key, "")
	}
	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected error")
	}
	for _, key := range []string{"POSTGRES_DSN", "BOOTSTRAP_ADMIN", "BOOTSTRAP_ADMIN_PASSWORD", "ENCRYPTION_KEY"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error does not mention %s: %v", key, err)
		}
	}
}

func TestLoadRejectsWeakBootstrapPassword(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://db/jikim")
	t.Setenv("BOOTSTRAP_ADMIN", "admin")
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "short")
	t.Setenv("ENCRYPTION_KEY", strings.Repeat("k", 32))
	if _, err := Load(); err == nil {
		t.Fatal("Load() expected password length error")
	}
}
