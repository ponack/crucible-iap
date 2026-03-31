// SPDX-License-Identifier: AGPL-3.0-or-later
package metrics

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// HTTP
	httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "crucible_http_requests_total",
		Help: "Total HTTP requests by method, path group, and status code.",
	}, []string{"method", "path", "status"})

	httpRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "crucible_http_request_duration_seconds",
		Help:    "HTTP request duration in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})

	// Business
	RunsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "crucible_runs_total",
		Help: "Total run transitions by final status and run type.",
	}, []string{"status", "run_type"})

	QueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "crucible_queue_depth",
		Help: "Number of River jobs currently in the available state.",
	})

	BuildInfo = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "crucible_build_info",
		Help: "Build metadata.",
	}, []string{"version"})
)

// Middleware returns an Echo middleware that records HTTP request metrics.
// The path label is the route template (e.g. /stacks/:id) to avoid high cardinality.
func Middleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			err := next(c)
			duration := time.Since(start).Seconds()

			status := c.Response().Status
			if err != nil {
				if he, ok := err.(*echo.HTTPError); ok {
					status = he.Code
				} else {
					status = http.StatusInternalServerError
				}
			}

			// Use route path (template) as label to avoid cardinality explosion.
			path := c.Path()
			if path == "" {
				path = "unknown"
			}

			httpRequestsTotal.WithLabelValues(c.Request().Method, path, strconv.Itoa(status)).Inc()
			httpRequestDuration.WithLabelValues(c.Request().Method, path).Observe(duration)

			return err
		}
	}
}

// Handler returns the Prometheus metrics HTTP handler.
func Handler() echo.HandlerFunc {
	h := promhttp.Handler()
	return func(c echo.Context) error {
		h.ServeHTTP(c.Response(), c.Request())
		return nil
	}
}

// PollQueueDepth starts a background goroutine that updates the QueueDepth
// gauge every 30 seconds by querying the River job table.
func PollQueueDepth(ctx context.Context, pool *pgxpool.Pool) {
	go func() {
		tick := time.NewTicker(30 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				var n int
				if err := pool.QueryRow(ctx,
					`SELECT count(*) FROM river_job WHERE state = 'available'`,
				).Scan(&n); err == nil {
					QueueDepth.Set(float64(n))
				} else {
					slog.Debug("queue depth poll failed", "err", err)
				}
			}
		}
	}()
}
