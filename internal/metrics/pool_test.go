package metrics

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRegisterPoolMetrics(t *testing.T) {
	// A zero-config pool (never connected) still exposes valid Stat() values.
	pool, err := pgxpool.New(context.Background(), "")
	if err != nil {
		// On systems without a running Postgres the pool constructor may still
		// succeed (connection is lazy), but if it fails we skip rather than
		// break CI.
		t.Skipf("unable to create pgxpool (no database): %v", err)
	}
	defer pool.Close()

	reg := prometheus.NewPedanticRegistry()
	RegisterPoolMetrics(reg, pool)

	maxConns := pool.Stat().MaxConns()

	expected := fmt.Sprintf(`
# HELP flagz_db_pool_acquired Number of currently acquired database connections.
# TYPE flagz_db_pool_acquired gauge
flagz_db_pool_acquired 0
# HELP flagz_db_pool_idle Number of idle database connections in the pool.
# TYPE flagz_db_pool_idle gauge
flagz_db_pool_idle 0
# HELP flagz_db_pool_max Maximum number of database connections allowed in the pool.
# TYPE flagz_db_pool_max gauge
flagz_db_pool_max %d
# HELP flagz_db_pool_total Total number of database connections in the pool.
# TYPE flagz_db_pool_total gauge
flagz_db_pool_total 0
`, maxConns)

	if err := testutil.GatherAndCompare(reg, strings.NewReader(expected),
		"flagz_db_pool_acquired",
		"flagz_db_pool_idle",
		"flagz_db_pool_total",
		"flagz_db_pool_max",
	); err != nil {
		t.Errorf("unexpected metrics output:\n%v", err)
	}
}

func TestRegisterPoolMetrics_DescribeCollect(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "")
	if err != nil {
		t.Skipf("unable to create pgxpool (no database): %v", err)
	}
	defer pool.Close()

	reg := prometheus.NewPedanticRegistry()
	RegisterPoolMetrics(reg, pool)

	// Gathering twice should not panic or return errors.
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}

	if len(mfs) != 4 {
		t.Errorf("expected 4 metric families, got %d", len(mfs))
	}
}
