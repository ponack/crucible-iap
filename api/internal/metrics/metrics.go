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

	ActiveRuns = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "crucible_active_runs",
		Help: "Number of runs currently in progress (preparing, planning, or applying).",
	})

	StacksTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "crucible_stacks_total",
		Help: "Total number of stacks.",
	})

	BuildInfo = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "crucible_build_info",
		Help: "Build metadata.",
	}, []string{"version"})

	// Per-pool gauges back operator auto-scaling: scrape these and wire
	// HPA / Docker Compose scale / CloudWatch alarms against them. Crucible
	// can't spawn agents on the operator's infra, so these surface demand
	// signals; the operator's platform reacts.
	WorkerPoolQueueDepth = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "crucible_worker_pool_queue_depth",
		Help: "Runs in 'queued' status waiting for a worker pool agent.",
	}, []string{"pool_id", "pool_name"})

	WorkerPoolRunningRuns = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "crucible_worker_pool_running_runs",
		Help: "Runs actively executing on a worker pool (preparing, planning, or applying).",
	}, []string{"pool_id", "pool_name"})

	WorkerPoolSeen = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "crucible_worker_pool_seen",
		Help: "1 if at least one agent in the pool checked in within the last 60s, else 0.",
	}, []string{"pool_id", "pool_name"})
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

// PollQueueDepth starts a background goroutine that updates operational gauges
// every 30 seconds by querying the database.
func PollQueueDepth(ctx context.Context, pool *pgxpool.Pool) {
	go func() {
		tick := time.NewTicker(30 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				pollGauges(ctx, pool)
			}
		}
	}()
}

func pollGauges(ctx context.Context, pool *pgxpool.Pool) {
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM river_job WHERE state = 'available'`,
	).Scan(&n); err == nil {
		QueueDepth.Set(float64(n))
	} else {
		slog.Debug("queue depth poll failed", "err", err)
	}

	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM runs WHERE status IN ('preparing','planning','applying')`,
	).Scan(&n); err == nil {
		ActiveRuns.Set(float64(n))
	} else {
		slog.Debug("active runs poll failed", "err", err)
	}

	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM stacks`,
	).Scan(&n); err == nil {
		StacksTotal.Set(float64(n))
	} else {
		slog.Debug("stacks total poll failed", "err", err)
	}

	pollWorkerPoolGauges(ctx, pool)
}

// pollWorkerPoolGauges resets the per-pool gauge label set then re-populates
// it from the current state of worker_pools + runs. Resetting first prevents
// stale labels lingering after a pool is deleted.
func pollWorkerPoolGauges(ctx context.Context, pool *pgxpool.Pool) {
	WorkerPoolQueueDepth.Reset()
	WorkerPoolRunningRuns.Reset()
	WorkerPoolSeen.Reset()

	rows, err := pool.Query(ctx, `
		SELECT
			wp.id::text,
			wp.name,
			COALESCE(SUM(CASE WHEN r.status = 'queued' THEN 1 ELSE 0 END), 0) AS queued,
			COALESCE(SUM(CASE WHEN r.status IN ('preparing','planning','applying') THEN 1 ELSE 0 END), 0) AS running,
			CASE WHEN wp.last_seen_at IS NOT NULL AND wp.last_seen_at > now() - interval '60 seconds' THEN 1 ELSE 0 END AS seen
		FROM worker_pools wp
		LEFT JOIN runs r ON r.worker_pool_id = wp.id
		WHERE wp.is_disabled = false
		GROUP BY wp.id, wp.name, wp.last_seen_at
	`)
	if err != nil {
		slog.Debug("worker pool gauges poll failed", "err", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id, name string
		var queued, running, seen int
		if err := rows.Scan(&id, &name, &queued, &running, &seen); err != nil {
			slog.Debug("worker pool gauge row scan failed", "err", err)
			continue
		}
		WorkerPoolQueueDepth.WithLabelValues(id, name).Set(float64(queued))
		WorkerPoolRunningRuns.WithLabelValues(id, name).Set(float64(running))
		WorkerPoolSeen.WithLabelValues(id, name).Set(float64(seen))
	}
}
