package container

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"

	"github.com/George1487/MalyginGeorge/internal/config"
	api "github.com/George1487/MalyginGeorge/internal/generated"
	internalHttp "github.com/George1487/MalyginGeorge/internal/http"
	httpCreate "github.com/George1487/MalyginGeorge/internal/http/create"
	httpFinish "github.com/George1487/MalyginGeorge/internal/http/finish"
	httpGet "github.com/George1487/MalyginGeorge/internal/http/get"
	"github.com/George1487/MalyginGeorge/internal/http/middleware"
	"github.com/George1487/MalyginGeorge/internal/infra/postgres"
	"github.com/George1487/MalyginGeorge/internal/infra/postgres/transaction"
	"github.com/George1487/MalyginGeorge/internal/infra/telemetry"
	"github.com/George1487/MalyginGeorge/internal/repository/idempotency"
	repoTrip "github.com/George1487/MalyginGeorge/internal/repository/trip"
	usecaseCreate "github.com/George1487/MalyginGeorge/internal/usecase/create"
	usecaseFinish "github.com/George1487/MalyginGeorge/internal/usecase/finish"
	usecaseGet "github.com/George1487/MalyginGeorge/internal/usecase/get"
)

type Container struct {
	HTTPServer *http.Server
	Pool       *pgxpool.Pool
	Logger     *slog.Logger
	Shutdown   func(ctx context.Context) error
}

func New(ctx context.Context, cfg *config.Config) (*Container, error) {
	logger := telemetry.NewLogger(cfg.ParsedLogLevel(), cfg.OTelServiceName)

	shutdownOTel, err := telemetry.Init(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("init telemetry: %w", err)
	}

	metrics, err := telemetry.NewMetrics()
	if err != nil {
		_ = shutdownOTel(ctx)
		return nil, fmt.Errorf("init metrics: %w", err)
	}

	pool, err := postgres.NewPool(ctx, cfg)
	if err != nil {
		_ = shutdownOTel(ctx)
		return nil, fmt.Errorf("init postgres pool: %w", err)
	}

	txManager := transaction.NewPgxTxManager(pool)
	tripRepo := repoTrip.New(pool)
	idempotencyStore := idempotency.NewStore(pool)

	usecaseCreateInstance := usecaseCreate.New(tripRepo, tripRepo, txManager, metrics)
	usecaseGetInstance := usecaseGet.New(tripRepo)
	usecaseFinishInstance := usecaseFinish.New(tripRepo, tripRepo, txManager, metrics)

	handlerCreate := httpCreate.New(usecaseCreateInstance)
	handlerGet := httpGet.New(usecaseGetInstance)
	handlerFinish := httpFinish.New(usecaseFinishInstance)

	server := internalHttp.NewServer(handlerCreate, handlerGet, handlerFinish, pool)

	r := chi.NewRouter()
	tracer := otel.GetTracerProvider().Tracer(cfg.OTelServiceName)

	r.Use(middleware.Recover(logger))
	r.Use(middleware.Tracing(tracer))
	r.Use(middleware.Logging(logger))
	r.Use(middleware.Metrics(metrics))

	apiHandler := api.HandlerWithOptions(server, api.ChiServerOptions{
		BaseRouter:       r,
		ErrorHandlerFunc: internalHttp.ErrorHandler,
		Middlewares: []api.MiddlewareFunc{
			middleware.IdempotencyMiddleware(idempotencyStore, txManager, cfg.IdempotencyTTL),
			middleware.RequestTimeout(cfg.DatabaseQueryTimeout),
		},
	})

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           apiHandler,
		ReadTimeout:       cfg.ShutdownTimeout,
		ReadHeaderTimeout: cfg.ShutdownTimeout,
		WriteTimeout:      cfg.ShutdownTimeout,
		IdleTimeout:       cfg.ShutdownTimeout,
	}

	shutdown := func(shutdownCtx context.Context) error {
		var errs []error
		if shutdownOTel != nil {
			if err := shutdownOTel(shutdownCtx); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}

	return &Container{
		HTTPServer: httpServer,
		Pool:       pool,
		Logger:     logger,
		Shutdown:   shutdown,
	}, nil
}
