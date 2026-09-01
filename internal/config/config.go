package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

const Address = ":8080"

type Config struct {
	PostgresDSN       string
	BootstrapAdmin    string
	BootstrapPassword string
	EncryptionKey     string
}

func Load() (Config, error) {
	cfg := Config{
		PostgresDSN:       strings.TrimSpace(os.Getenv("POSTGRES_DSN")),
		BootstrapAdmin:    strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN")),
		BootstrapPassword: os.Getenv("BOOTSTRAP_ADMIN_PASSWORD"),
		EncryptionKey:     strings.TrimSpace(os.Getenv("ENCRYPTION_KEY")),
	}
	var missing []string
	if cfg.PostgresDSN == "" {
		missing = append(missing, "POSTGRES_DSN")
	}
	if cfg.BootstrapAdmin == "" {
		missing = append(missing, "BOOTSTRAP_ADMIN")
	}
	if cfg.BootstrapPassword == "" {
		missing = append(missing, "BOOTSTRAP_ADMIN_PASSWORD")
	}
	if cfg.EncryptionKey == "" {
		missing = append(missing, "ENCRYPTION_KEY")
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("필수 환경변수가 없습니다: %s", strings.Join(missing, ", "))
	}
	if len(cfg.BootstrapAdmin) < 3 || len(cfg.BootstrapAdmin) > 128 {
		return Config{}, errors.New("BOOTSTRAP_ADMIN은 3~128자여야 합니다")
	}
	if len(cfg.BootstrapPassword) < 12 {
		return Config{}, errors.New("BOOTSTRAP_ADMIN_PASSWORD는 12자 이상이어야 합니다")
	}
	return cfg, nil
}
