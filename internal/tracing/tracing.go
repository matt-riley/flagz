// Package tracing provides opt-in OpenTelemetry tracing support for the flagz
// server. Tracing is enabled only when the OTEL_EXPORTER_OTLP_ENDPOINT
// environment variable is set; otherwise [Init] returns a no-op shutdown
// function.
package tracing

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

const defaultServiceName = "flagz"

// Init configures the global OpenTelemetry tracer provider with an OTLP HTTP
// exporter. If OTEL_EXPORTER_OTLP_ENDPOINT is not set, tracing is disabled and
// a no-op shutdown function is returned.
//
// The returned function should be called on server shutdown to flush pending
// spans.
func Init(ctx context.Context) (shutdown func(context.Context) error, err error) {
	endpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	if endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}
	if _, err := parseOTLPEndpoint(endpoint); err != nil {
		return nil, err
	}

	serviceName := serviceNameFromEnv()

	res, err := resource.Merge(
		resource.Default(),
		resource.NewSchemaless(
			semconv.ServiceName(serviceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create resource: %w", err)
	}

	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("create OTLP exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}

func serviceNameFromEnv() string {
	serviceName := strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME"))
	if serviceName == "" {
		return defaultServiceName
	}
	return serviceName
}

func parseOTLPEndpoint(endpoint string) (*url.URL, error) {
	parsedEndpoint, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid OTLP endpoint: %w", err)
	}
	if parsedEndpoint.Scheme == "" || parsedEndpoint.Host == "" {
		return nil, fmt.Errorf("invalid OTLP endpoint: %q must include scheme and host", endpoint)
	}
	return parsedEndpoint, nil
}
