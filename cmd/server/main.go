package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hkjang/jikim/internal/config"
	"github.com/hkjang/jikim/internal/cryptox"
	"github.com/hkjang/jikim/internal/httpapi"
	"github.com/hkjang/jikim/internal/store"
	"github.com/hkjang/jikim/internal/version"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)
	if err := run(logger); err != nil {
		logger.Error("jikim 서버 종료", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	master, err := cryptox.NewFromString(cfg.EncryptionKey)
	if err != nil {
		return err
	}
	startupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	database, err := store.New(startupCtx, cfg.PostgresDSN, master)
	if err != nil {
		return err
	}
	defer database.Close()
	if err := database.Migrate(startupCtx); err != nil {
		return err
	}
	if err := database.VerifyMasterKey(startupCtx); err != nil {
		return err
	}
	admin, created, err := database.BootstrapAdmin(startupCtx, cfg.BootstrapAdmin, cfg.BootstrapPassword)
	if err != nil {
		return err
	}
	logger.Info("bootstrap 관리자 준비", "username", admin.Username, "created", created)

	httpServer := &http.Server{
		Addr:              config.Address,
		Handler:           httpapi.New(database, logger),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
	}
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("jikim 시작", "address", config.Address, "version", version.Version)
		serverErrors <- httpServer.ListenAndServe()
	}()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case signal := <-signals:
		logger.Info("종료 신호 수신", "signal", signal.String())
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer shutdownCancel()
		return httpServer.Shutdown(shutdownCtx)
	}
}
