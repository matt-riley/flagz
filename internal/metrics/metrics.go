// Package metrics provides Prometheus instrumentation for the flagz server.
//
// All metrics are registered in a custom [prometheus.Registry] (not the global
// default) so that only flagz metrics appear on the /metrics endpoint.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds all Prometheus collectors used by the flagz server.
type Metrics struct {
	Registry *prometheus.Registry

	HTTPRequestsTotal    *prometheus.CounterVec
	HTTPRequestDuration  *prometheus.HistogramVec
	GRPCRequestsTotal    *prometheus.CounterVec
	GRPCRequestDuration  *prometheus.HistogramVec
	CacheSize            *prometheus.GaugeVec
	CacheLoadsTotal      prometheus.Counter
	CacheInvalidations   prometheus.Counter
	EvaluationsTotal     *prometheus.CounterVec
	AuthFailuresTotal    prometheus.Counter
	ActiveStreams         *prometheus.GaugeVec
	DBPoolAcquired       prometheus.Gauge
	DBPoolIdle           prometheus.Gauge
	DBPoolTotal          prometheus.Gauge
}

// Metrics are defined upfront as part of the custom registry. Some metrics
// (e.g., gRPC, cache, evaluation, auth, DB pool) are instrumented by
// subsequent phases that add the corresponding middleware and hooks.
// Defining them here ensures the registry is complete and /metrics always
// exposes a consistent set of metric names.

// New creates and registers all flagz metrics in a fresh registry.
func New() *Metrics {
	reg := prometheus.NewRegistry()

	m := &Metrics{
		Registry: reg,

		HTTPRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "flagz_http_requests_total",
			Help: "Total number of HTTP requests.",
		}, []string{"method", "route", "status"}),

		HTTPRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "flagz_http_request_duration_seconds",
			Help:    "HTTP request latency in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route", "status"}),

		GRPCRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "flagz_grpc_requests_total",
			Help: "Total number of gRPC requests.",
		}, []string{"method", "status"}),

		GRPCRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "flagz_grpc_request_duration_seconds",
			Help:    "gRPC request latency in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "status"}),

		CacheSize: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "flagz_cache_size",
			Help: "Number of flags in the in-memory cache.",
		}, []string{"project_id"}),

		CacheLoadsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "flagz_cache_loads_total",
			Help: "Total number of full cache reloads from the database.",
		}),

		CacheInvalidations: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "flagz_cache_invalidations_total",
			Help: "Total number of NOTIFY-triggered cache invalidations.",
		}),

		EvaluationsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "flagz_flag_evaluations_total",
			Help: "Total number of flag evaluations.",
		}, []string{"result"}),

		AuthFailuresTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "flagz_auth_failures_total",
			Help: "Total number of failed authentication attempts.",
		}),

		ActiveStreams: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "flagz_active_streams",
			Help: "Number of active streaming connections.",
		}, []string{"transport"}),

		DBPoolAcquired: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "flagz_db_pool_acquired",
			Help: "Number of currently acquired database connections.",
		}),

		DBPoolIdle: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "flagz_db_pool_idle",
			Help: "Number of idle database connections in the pool.",
		}),

		DBPoolTotal: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "flagz_db_pool_total",
			Help: "Total number of database connections in the pool.",
		}),
	}

	reg.MustRegister(
		m.HTTPRequestsTotal,
		m.HTTPRequestDuration,
		m.GRPCRequestsTotal,
		m.GRPCRequestDuration,
		m.CacheSize,
		m.CacheLoadsTotal,
		m.CacheInvalidations,
		m.EvaluationsTotal,
		m.AuthFailuresTotal,
		m.ActiveStreams,
		m.DBPoolAcquired,
		m.DBPoolIdle,
		m.DBPoolTotal,
	)

	return m
}

// Handler returns an [http.Handler] that serves Prometheus metrics.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{})
}
