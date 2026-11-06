package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/George1487/MalyginGeorge/internal/config"
	"github.com/George1487/MalyginGeorge/internal/container"
)

func main() {
	if err := run(); err != nil {
		slog.Error("service stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	appCtx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	c, err := container.New(appCtx, cfg)
	if err != nil {
		return fmt.Errorf("init container: %w", err)
	}
	defer func() {
		if c.Shutdown != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
			defer cancel()
			_ = c.Shutdown(shutdownCtx)
		}
		if c.Pool != nil {
			c.Pool.Close()
		}
	}()

	serverErr := make(chan error, 1)
	go func() {
		c.Logger.Info("starting HTTP server", "addr", cfg.HTTPAddr)
		serverErr <- c.HTTPServer.ListenAndServe()
	}()

	select {
	case err = <-serverErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-appCtx.Done():
	}

	c.Logger.Info("shutting down HTTP server")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err = c.HTTPServer.Shutdown(shutdownCtx); err != nil {
		closeErr := c.HTTPServer.Close()
		if closeErr != nil {
			return errors.Join(
				fmt.Errorf("shutdown HTTP server: %w", err),
				fmt.Errorf("force close HTTP server: %w", closeErr),
			)
		}
		return fmt.Errorf("shutdown HTTP server: %w", err)
	}

	if err = <-serverErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve HTTP during shutdown: %w", err)
	}

	return nil
}
