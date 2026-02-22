package metrics

import (
"github.com/jackc/pgx/v5/pgxpool"
"github.com/prometheus/client_golang/prometheus"
)

type poolCollector struct {
pool *pgxpool.Pool

acquiredConns *prometheus.Desc
idleConns     *prometheus.Desc
totalConns    *prometheus.Desc
maxConns      *prometheus.Desc
}

// RegisterPoolMetrics registers Prometheus gauges that report live pgxpool
// connection statistics on every scrape.
func RegisterPoolMetrics(reg prometheus.Registerer, pool *pgxpool.Pool) {
reg.MustRegister(&poolCollector{
pool: pool,
acquiredConns: prometheus.NewDesc(
"flagz_db_pool_acquired_conns",
"Number of currently acquired connections in the pool.",
nil, nil,
),
idleConns: prometheus.NewDesc(
"flagz_db_pool_idle_conns",
"Number of currently idle connections in the pool.",
nil, nil,
),
totalConns: prometheus.NewDesc(
"flagz_db_pool_total_conns",
"Total number of connections currently in the pool.",
nil, nil,
),
maxConns: prometheus.NewDesc(
"flagz_db_pool_max_conns",
"Maximum number of connections allowed in the pool.",
nil, nil,
),
})
}

func (c *poolCollector) Describe(ch chan<- *prometheus.Desc) {
ch <- c.acquiredConns
ch <- c.idleConns
ch <- c.totalConns
ch <- c.maxConns
}

func (c *poolCollector) Collect(ch chan<- prometheus.Metric) {
stat := c.pool.Stat()

ch <- prometheus.MustNewConstMetric(c.acquiredConns, prometheus.GaugeValue, float64(stat.AcquiredConns()))
ch <- prometheus.MustNewConstMetric(c.idleConns, prometheus.GaugeValue, float64(stat.IdleConns()))
ch <- prometheus.MustNewConstMetric(c.totalConns, prometheus.GaugeValue, float64(stat.TotalConns()))
ch <- prometheus.MustNewConstMetric(c.maxConns, prometheus.GaugeValue, float64(stat.MaxConns()))
}
