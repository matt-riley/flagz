// Package metrics provides Prometheus instrumentation for the flagz server.
//
// All metrics are registered in a custom [prometheus.Registry] (not the global
// default) so that only flagz metrics appear on the /metrics endpoint.
package metrics

import (
	"context"
	"net/http"
	"path"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
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

// UnaryServerInterceptor returns a gRPC unary interceptor that records
// request count and latency for each method.
func (m *Metrics) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		method := path.Base(info.FullMethod)
		st, _ := status.FromError(err)
		code := st.Code().String()
		m.GRPCRequestsTotal.WithLabelValues(method, code).Inc()
		m.GRPCRequestDuration.WithLabelValues(method, code).Observe(time.Since(start).Seconds())
		return resp, err
	}
}

// StreamServerInterceptor returns a gRPC stream interceptor that records
// request count, latency, and active stream gauge.
func (m *Metrics) StreamServerInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		m.ActiveStreams.WithLabelValues("grpc").Inc()
		defer m.ActiveStreams.WithLabelValues("grpc").Dec()
		start := time.Now()
		err := handler(srv, ss)
		method := path.Base(info.FullMethod)
		st, _ := status.FromError(err)
		code := st.Code().String()
		m.GRPCRequestsTotal.WithLabelValues(method, code).Inc()
		m.GRPCRequestDuration.WithLabelValues(method, code).Observe(time.Since(start).Seconds())
		return err
	}
}

// RecordEvaluation increments the evaluation counter with the given result.
func (m *Metrics) RecordEvaluation(result bool) {
	m.EvaluationsTotal.WithLabelValues(strconv.FormatBool(result)).Inc()
}

// SetCacheSize updates the cache size gauge for the given project.
func (m *Metrics) SetCacheSize(projectID string, size float64) {
	m.CacheSize.WithLabelValues(projectID).Set(size)
}

// IncCacheLoads increments the cache load counter.
func (m *Metrics) IncCacheLoads() {
	m.CacheLoadsTotal.Inc()
}

// IncCacheInvalidations increments the cache invalidation counter.
func (m *Metrics) IncCacheInvalidations() {
	m.CacheInvalidations.Inc()
}

// DBPoolStats holds connection pool statistics for metric updates.
type DBPoolStats struct {
	Acquired float64
	Idle     float64
	Total    float64
}

// SetDBPoolStats updates the DB pool gauges.
func (m *Metrics) SetDBPoolStats(stats DBPoolStats) {
	m.DBPoolAcquired.Set(stats.Acquired)
	m.DBPoolIdle.Set(stats.Idle)
	m.DBPoolTotal.Set(stats.Total)
}
