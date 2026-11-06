package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

type metricsRecorder interface {
	RecordHTTPRequest(ctx context.Context, method, route string, statusCode int, duration time.Duration)
	AddActiveRequest(ctx context.Context, method, route string, delta int64)
}

func Metrics(m metricsRecorder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := newStatusResponseWriter(w)

			route := ""
			if chiCtx := chi.RouteContext(r.Context()); chiCtx != nil {
				route = chiCtx.RoutePattern()
			}
			if route == "" {
				route = r.URL.Path
			}

			if m != nil {
				m.AddActiveRequest(r.Context(), r.Method, route, 1)
				defer func() {
					m.AddActiveRequest(r.Context(), r.Method, route, -1)
				}()
			}

			next.ServeHTTP(rw, r)

			duration := time.Since(start)

			if chiCtx := chi.RouteContext(r.Context()); chiCtx != nil && chiCtx.RoutePattern() != "" {
				route = chiCtx.RoutePattern()
			}

			if m != nil {
				m.RecordHTTPRequest(r.Context(), r.Method, route, rw.statusCode, duration)
			}
		})
	}
}
