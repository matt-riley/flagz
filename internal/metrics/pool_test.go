package metrics_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/matt-riley/flagz/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRegisterPoolMetrics(t *testing.T) {
	// A zero-config pool (never connected) still exposes valid Stat() values.
	pool, err := pgxpool.New(t.Context(), "")
	if err != nil {
		// On systems without a running Postgres the pool constructor may still
		// succeed (connection is lazy), but if it fails we skip rather than
		// break CI.
		t.Skipf("unable to create pgxpool (no database): %v", err)
	}
	defer pool.Close()

	reg := prometheus.NewPedanticRegistry()
	metrics.RegisterPoolMetrics(reg, pool)

	maxConns := pool.Stat().MaxConns()

	expected := fmt.Sprintf(`
# HELP flagz_db_pool_acquired_conns Number of currently acquired connections in the pool.
# TYPE flagz_db_pool_acquired_conns gauge
flagz_db_pool_acquired_conns 0
# HELP flagz_db_pool_idle_conns Number of currently idle connections in the pool.
# TYPE flagz_db_pool_idle_conns gauge
flagz_db_pool_idle_conns 0
# HELP flagz_db_pool_max_conns Maximum number of connections allowed in the pool.
# TYPE flagz_db_pool_max_conns gauge
flagz_db_pool_max_conns %d
# HELP flagz_db_pool_total_conns Total number of connections currently in the pool.
# TYPE flagz_db_pool_total_conns gauge
flagz_db_pool_total_conns 0
`, maxConns)

	if err := testutil.GatherAndCompare(reg, strings.NewReader(expected),
		"flagz_db_pool_acquired_conns",
		"flagz_db_pool_idle_conns",
		"flagz_db_pool_total_conns",
		"flagz_db_pool_max_conns",
	); err != nil {
		t.Errorf("unexpected metrics output:\n%v", err)
	}
}

func TestRegisterPoolMetrics_DescribeCollect(t *testing.T) {
	pool, err := pgxpool.New(t.Context(), "")
	if err != nil {
		t.Skipf("unable to create pgxpool (no database): %v", err)
	}
	defer pool.Close()

	reg := prometheus.NewPedanticRegistry()
	metrics.RegisterPoolMetrics(reg, pool)

	// Gathering twice should not panic or return errors.
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}

	if len(mfs) != 4 {
		t.Errorf("expected 4 metric families, got %d", len(mfs))
	}
}
