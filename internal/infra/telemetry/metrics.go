package telemetry

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

type Metrics struct {
	httpRequestDuration metric.Float64Histogram
	httpActiveRequests  metric.Int64UpDownCounter
	tripsCreated        metric.Int64Counter
	tripsCompleted      metric.Int64Counter
}

func NewMetrics() (*Metrics, error) {
	meter := otel.GetMeterProvider().Meter("trip-service")

	requestDuration, err := meter.Float64Histogram(
		"http.server.request.duration",
		metric.WithDescription("Duration of HTTP server requests"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("create http.server.request.duration histogram: %w", err)
	}

	activeRequests, err := meter.Int64UpDownCounter(
		"http.server.active_requests",
		metric.WithDescription("Number of active HTTP requests"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create http.server.active_requests counter: %w", err)
	}

	tripsCreated, err := meter.Int64Counter(
		"tripgo.trips.created",
		metric.WithDescription("Total number of created trips"),
		metric.WithUnit("{trip}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create tripgo.trips.created counter: %w", err)
	}

	tripsCompleted, err := meter.Int64Counter(
		"tripgo.trips.completed",
		metric.WithDescription("Total number of completed trips"),
		metric.WithUnit("{trip}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create tripgo.trips.completed counter: %w", err)
	}

	return &Metrics{
		httpRequestDuration: requestDuration,
		httpActiveRequests:  activeRequests,
		tripsCreated:        tripsCreated,
		tripsCompleted:      tripsCompleted,
	}, nil
}

func (m *Metrics) RecordHTTPRequest(ctx context.Context, method, route string, statusCode int, duration time.Duration) {
	if m == nil || m.httpRequestDuration == nil {
		return
	}
	m.httpRequestDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(
		semconv.HTTPRequestMethodKey.String(method),
		semconv.HTTPRouteKey.String(route),
		semconv.HTTPResponseStatusCodeKey.Int(statusCode),
	))
}

func (m *Metrics) AddActiveRequest(ctx context.Context, method, route string, delta int64) {
	if m == nil || m.httpActiveRequests == nil {
		return
	}
	m.httpActiveRequests.Add(ctx, delta, metric.WithAttributes(
		semconv.HTTPRequestMethodKey.String(method),
		semconv.HTTPRouteKey.String(route),
	))
}

func (m *Metrics) TripCreated(ctx context.Context) {
	if m == nil || m.tripsCreated == nil {
		return
	}
	m.tripsCreated.Add(ctx, 1)
}

func (m *Metrics) TripCompleted(ctx context.Context) {
	if m == nil || m.tripsCompleted == nil {
		return
	}
	m.tripsCompleted.Add(ctx, 1)
}
