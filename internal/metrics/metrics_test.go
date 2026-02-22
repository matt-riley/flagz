package metrics

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNew(t *testing.T) {
	m := New()
	if m.Registry == nil {
		t.Fatal("expected non-nil Registry")
	}
	// Gathering should succeed and return registered metric families.
	fams, err := m.Registry.Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}
	// No samples yet, but families are registered on first use;
	// force a sample so we can verify at least one family appears.
	m.CacheLoadsTotal.Inc()
	fams, err = m.Registry.Gather()
	if err != nil {
		t.Fatalf("gather after inc failed: %v", err)
	}
	if len(fams) == 0 {
		t.Fatal("expected at least one metric family after increment")
	}
}

func TestRecordEvaluation(t *testing.T) {
	m := New()

	m.RecordEvaluation(true)
	m.RecordEvaluation(true)
	m.RecordEvaluation(false)

	trueCount := testutil.ToFloat64(m.EvaluationsTotal.WithLabelValues("true"))
	falseCount := testutil.ToFloat64(m.EvaluationsTotal.WithLabelValues("false"))

	if trueCount != 2 {
		t.Fatalf("expected true count 2, got %v", trueCount)
	}
	if falseCount != 1 {
		t.Fatalf("expected false count 1, got %v", falseCount)
	}
}

func TestSetCacheSize(t *testing.T) {
	m := New()

	m.SetCacheSize("proj1", 5)
	val := testutil.ToFloat64(m.CacheSize.WithLabelValues("proj1"))
	if val != 5 {
		t.Fatalf("expected cache size 5, got %v", val)
	}
}

func TestResetCacheSize(t *testing.T) {
	m := New()

	m.SetCacheSize("proj1", 10)
	m.SetCacheSize("proj2", 20)
	m.ResetCacheSize()

	// After reset, WithLabelValues creates a fresh gauge starting at 0.
	val := testutil.ToFloat64(m.CacheSize.WithLabelValues("proj1"))
	if val != 0 {
		t.Fatalf("expected cache size 0 after reset, got %v", val)
	}
}

func TestSetDBPoolStats(t *testing.T) {
	m := New()

	m.SetDBPoolStats(DBPoolStats{Acquired: 3, Idle: 7, Total: 10})

	if v := testutil.ToFloat64(m.DBPoolAcquired); v != 3 {
		t.Fatalf("expected acquired 3, got %v", v)
	}
	if v := testutil.ToFloat64(m.DBPoolIdle); v != 7 {
		t.Fatalf("expected idle 7, got %v", v)
	}
	if v := testutil.ToFloat64(m.DBPoolTotal); v != 10 {
		t.Fatalf("expected total 10, got %v", v)
	}
}

func TestHandler(t *testing.T) {
	m := New()
	m.CacheLoadsTotal.Inc()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	m.Handler().ServeHTTP(rec, req)

	body, _ := io.ReadAll(rec.Result().Body)
	if rec.Code != 200 {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if !strings.Contains(string(body), "flagz_cache_loads_total") {
		t.Fatal("expected response to contain flagz_cache_loads_total")
	}
}

func TestIncCacheLoads(t *testing.T) {
	m := New()

	m.IncCacheLoads()
	m.IncCacheLoads()

	if v := testutil.ToFloat64(m.CacheLoadsTotal); v != 2 {
		t.Fatalf("expected cache loads 2, got %v", v)
	}
}

func TestIncCacheInvalidations(t *testing.T) {
	m := New()

	m.IncCacheInvalidations()
	m.IncCacheInvalidations()
	m.IncCacheInvalidations()

	if v := testutil.ToFloat64(m.CacheInvalidations); v != 3 {
		t.Fatalf("expected cache invalidations 3, got %v", v)
	}
}
