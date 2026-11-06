package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

type Config struct {
	HTTPAddr        string        `env:"HTTP_ADDR,required,notEmpty"`
	LogLevel        string        `env:"LOG_LEVEL,required,notEmpty"`
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT,required"`
	IdempotencyTTL  time.Duration `env:"IDEMPOTENCY_TTL" envDefault:"24h"`

	DatabaseURL             string        `env:"DATABASE_URL,required,notEmpty"`
	DatabaseMaxConns        int32         `env:"DATABASE_MAX_CONNS,required"`
	DatabaseMinConns        int32         `env:"DATABASE_MIN_CONNS,required"`
	DatabaseMaxConnLifetime time.Duration `env:"DATABASE_MAX_CONN_LIFETIME,required"`
	DatabaseConnectTimeout  time.Duration `env:"DATABASE_CONNECT_TIMEOUT,required"`
	DatabaseQueryTimeout    time.Duration `env:"DATABASE_QUERY_TIMEOUT,required"`

	OTelServiceName          string `env:"OTEL_SERVICE_NAME" envDefault:"trip-service"`
	OTelExporterOTLPEndpoint string `env:"OTEL_EXPORTER_OTLP_ENDPOINT"`
	OTelExporterOTLPProtocol string `env:"OTEL_EXPORTER_OTLP_PROTOCOL" envDefault:"http/protobuf"`
	OTelResourceAttributes   string `env:"OTEL_RESOURCE_ATTRIBUTES"`
	OTelTracesSampler        string `env:"OTEL_TRACES_SAMPLER" envDefault:"parentbased_always_on"`
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return &cfg, nil
}

func (c *Config) Validate() error {
	if strings.TrimSpace(c.HTTPAddr) == "" {
		return errors.New("HTTP_ADDR must not be empty")
	}

	if strings.TrimSpace(c.DatabaseURL) == "" {
		return errors.New("DATABASE_URL must not be empty")
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(c.LogLevel)); err != nil {
		return fmt.Errorf("invalid LOG_LEVEL: %w", err)
	}

	if c.ShutdownTimeout <= 0 {
		return errors.New("SHUTDOWN_TIMEOUT must be positive")
	}

	if c.DatabaseConnectTimeout <= 0 {
		return errors.New("DATABASE_CONNECT_TIMEOUT must be positive")
	}

	if c.DatabaseQueryTimeout <= 0 {
		return errors.New("DATABASE_QUERY_TIMEOUT must be positive")
	}

	if c.DatabaseMaxConnLifetime <= 0 {
		return errors.New("DATABASE_MAX_CONN_LIFETIME must be positive")
	}

	if c.IdempotencyTTL <= 0 {
		return errors.New("IDEMPOTENCY_TTL must be positive")
	}

	if c.DatabaseMaxConns <= 0 {
		return errors.New("DATABASE_MAX_CONNS must be positive")
	}

	if c.DatabaseMinConns < 0 {
		return errors.New("DATABASE_MIN_CONNS must be non-negative")
	}

	if c.DatabaseMinConns > c.DatabaseMaxConns {
		return errors.New("DATABASE_MIN_CONNS must not exceed DATABASE_MAX_CONNS")
	}

	return nil
}

func (c *Config) ParsedLogLevel() slog.Level {
	var level slog.Level
	if err := level.UnmarshalText([]byte(c.LogLevel)); err != nil {
		return slog.LevelInfo
	}
	return level
}
