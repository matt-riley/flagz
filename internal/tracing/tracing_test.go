package tracing

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestInit_NoEndpointReturnsNoop(t *testing.T) {
	restoreOpenTelemetryGlobals(t)
	sentinelProvider := noop.NewTracerProvider()
	otel.SetTracerProvider(sentinelProvider)

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "   ")

	shutdown, err := Init(context.Background())
	if err != nil {
		t.Fatalf("Init() error = %v, want nil", err)
	}
	if shutdown == nil {
		t.Fatal("Init() shutdown = nil, want non-nil")
	}
	if got := otel.GetTracerProvider(); got != sentinelProvider {
		t.Fatal("Init() changed global tracer provider when tracing endpoint is unset")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown() error = %v, want nil", err)
	}
}

func TestInit_WithEndpointInitializesTracerProvider(t *testing.T) {
	restoreOpenTelemetryGlobals(t)
	sentinelProvider := noop.NewTracerProvider()
	otel.SetTracerProvider(sentinelProvider)

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4318")
	t.Setenv("OTEL_SERVICE_NAME", "flagz-test")

	shutdown, err := Init(context.Background())
	if err != nil {
		t.Fatalf("Init() error = %v, want nil", err)
	}
	if shutdown == nil {
		t.Fatal("Init() shutdown = nil, want non-nil")
	}

	got := otel.GetTracerProvider()
	if got == sentinelProvider {
		t.Fatal("Init() did not replace global tracer provider")
	}
	if _, ok := got.(*sdktrace.TracerProvider); !ok {
		t.Fatalf("Init() tracer provider type = %T, want *sdktrace.TracerProvider", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		t.Fatalf("shutdown() error = %v, want nil", err)
	}
}

func TestServiceNameFromEnv(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "  ")
	if got := serviceNameFromEnv(); got != defaultServiceName {
		t.Fatalf("serviceNameFromEnv() = %q, want %q", got, defaultServiceName)
	}

	t.Setenv("OTEL_SERVICE_NAME", " custom-service ")
	if got := serviceNameFromEnv(); got != "custom-service" {
		t.Fatalf("serviceNameFromEnv() = %q, want %q", got, "custom-service")
	}
}

func TestInit_InvalidExporterConfigReturnsError(t *testing.T) {
	restoreOpenTelemetryGlobals(t)
	sentinelProvider := noop.NewTracerProvider()
	otel.SetTracerProvider(sentinelProvider)

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://[::1")

	shutdown, err := Init(context.Background())
	if err == nil {
		t.Fatal("Init() error = nil, want non-nil")
	}
	if shutdown != nil {
		t.Fatal("Init() shutdown should be nil when initialization fails")
	}
	if !strings.Contains(err.Error(), "invalid OTLP endpoint") {
		t.Fatalf("Init() error = %q, want prefix containing %q", err.Error(), "invalid OTLP endpoint")
	}
	if got := otel.GetTracerProvider(); got != sentinelProvider {
		t.Fatal("Init() changed global tracer provider on exporter initialization error")
	}
}

func restoreOpenTelemetryGlobals(t *testing.T) {
	t.Helper()
	originalProvider := otel.GetTracerProvider()
	originalPropagator := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(originalProvider)
		otel.SetTextMapPropagator(originalPropagator)
	})
}
