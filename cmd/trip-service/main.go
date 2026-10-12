package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/George1487/MalyginGeorge/internal"
	api "github.com/George1487/MalyginGeorge/internal/generated"
)

type Config struct {
	HTTPAddr                string
	LogLevel                slog.Level
	ShutdownTimeout         time.Duration
	IdempotencyTTL          time.Duration
	DatabaseURL             string
	DatabaseMaxConns        int32
	DatabaseMinConns        int32
	DatabaseMaxConnLifetime time.Duration
	DatabaseConnectTimeout  time.Duration
	DatabaseQueryTimeout    time.Duration
}

func main() {
	if err := run(); err != nil {
		slog.Error("service stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	})).With("service", "trip-service")
	slog.SetDefault(logger)

	applicationCtx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	pool, err := newPool(applicationCtx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	tripRepository := internal.NewTripRepository(pool)
	tripHistoryRepository := internal.NewTripHistoryRepository(pool)
	txManager := internal.NewPgxTxManager(pool)
	tripService := internal.NewTripService(tripRepository, tripHistoryRepository, txManager)
	idempotencyStore := internal.NewIdempotencyStore(pool)
	server := internal.NewServer(tripService, pool)

	handler := api.HandlerWithOptions(server, api.ChiServerOptions{
		ErrorHandlerFunc: internal.ErrorHandler,
		Middlewares: []api.MiddlewareFunc{
			internal.IdempotencyMiddleware(idempotencyStore, txManager, cfg.IdempotencyTTL),
			requestTimeout(cfg.DatabaseQueryTimeout),
		},
	})

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadTimeout:       cfg.ShutdownTimeout,
		ReadHeaderTimeout: cfg.ShutdownTimeout,
		WriteTimeout:      cfg.ShutdownTimeout,
		IdleTimeout:       cfg.ShutdownTimeout,
	}

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- httpServer.ListenAndServe()
	}()

	select {
	case err = <-serverErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-applicationCtx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err = httpServer.Shutdown(shutdownCtx); err != nil {
		closeErr := httpServer.Close()
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

func newPool(ctx context.Context, cfg *Config) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database config: %w", err)
	}

	poolConfig.MaxConns = cfg.DatabaseMaxConns
	poolConfig.MinConns = cfg.DatabaseMinConns
	poolConfig.MaxConnLifetime = cfg.DatabaseMaxConnLifetime
	poolConfig.ConnConfig.ConnectTimeout = cfg.DatabaseConnectTimeout
	poolConfig.PingTimeout = cfg.DatabaseQueryTimeout

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, cfg.DatabaseConnectTimeout)
	defer cancel()

	if err = pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}

func requestTimeout(timeout time.Duration) api.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			ctx, cancel := context.WithTimeout(request.Context(), timeout)
			defer cancel()

			next.ServeHTTP(w, request.WithContext(ctx))
		})
	}
}

func loadConfig() (*Config, error) {
	httpAddr, err := requiredEnv("HTTP_ADDR")
	if err != nil {
		return nil, err
	}
	databaseURL, err := requiredEnv("DATABASE_URL")
	if err != nil {
		return nil, err
	}
	logLevelText, err := requiredEnv("LOG_LEVEL")
	if err != nil {
		return nil, err
	}

	var logLevel slog.Level
	if err = logLevel.UnmarshalText([]byte(logLevelText)); err != nil {
		return nil, fmt.Errorf("parse LOG_LEVEL: %w", err)
	}

	shutdownTimeout, err := positiveDurationEnv("SHUTDOWN_TIMEOUT")
	if err != nil {
		return nil, err
	}
	databaseMaxConnLifetime, err := positiveDurationEnv("DATABASE_MAX_CONN_LIFETIME")
	if err != nil {
		return nil, err
	}
	databaseConnectTimeout, err := positiveDurationEnv("DATABASE_CONNECT_TIMEOUT")
	if err != nil {
		return nil, err
	}
	databaseQueryTimeout, err := positiveDurationEnv("DATABASE_QUERY_TIMEOUT")
	if err != nil {
		return nil, err
	}
	idempotencyTTL, err := positiveDurationEnvOrDefault("IDEMPOTENCY_TTL", 24*time.Hour)
	if err != nil {
		return nil, err
	}
	databaseMaxConns, err := positiveInt32Env("DATABASE_MAX_CONNS")
	if err != nil {
		return nil, err
	}
	databaseMinConns, err := nonNegativeInt32Env("DATABASE_MIN_CONNS")
	if err != nil {
		return nil, err
	}
	if databaseMinConns > databaseMaxConns {
		return nil, errors.New("DATABASE_MIN_CONNS must not exceed DATABASE_MAX_CONNS")
	}

	return &Config{
		HTTPAddr:                httpAddr,
		LogLevel:                logLevel,
		ShutdownTimeout:         shutdownTimeout,
		IdempotencyTTL:          idempotencyTTL,
		DatabaseURL:             databaseURL,
		DatabaseMaxConns:        databaseMaxConns,
		DatabaseMinConns:        databaseMinConns,
		DatabaseMaxConnLifetime: databaseMaxConnLifetime,
		DatabaseConnectTimeout:  databaseConnectTimeout,
		DatabaseQueryTimeout:    databaseQueryTimeout,
	}, nil
}

func requiredEnv(name string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}

func positiveDurationEnv(name string) (time.Duration, error) {
	value, err := requiredEnv(name)
	if err != nil {
		return 0, err
	}

	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	return duration, nil
}

func positiveDurationEnvOrDefault(name string, defaultValue time.Duration) (time.Duration, error) {
	if strings.TrimSpace(os.Getenv(name)) == "" {
		return defaultValue, nil
	}

	return positiveDurationEnv(name)
}

func positiveInt32Env(name string) (int32, error) {
	value, err := parseInt32Env(name)
	if err != nil {
		return 0, err
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	return value, nil
}

func nonNegativeInt32Env(name string) (int32, error) {
	value, err := parseInt32Env(name)
	if err != nil {
		return 0, err
	}
	if value < 0 {
		return 0, fmt.Errorf("%s must not be negative", name)
	}
	return value, nil
}

func parseInt32Env(name string) (int32, error) {
	value, err := requiredEnv(name)
	if err != nil {
		return 0, err
	}

	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	return int32(parsed), nil
}
