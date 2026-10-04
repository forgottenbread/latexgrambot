// Package telemetry wires the OpenTelemetry SDK to an OTLP collector
// (OTEL_EXPORTER_OTLP_ENDPOINT). With no endpoint configured the global
// tracer provider stays a no-op.
package telemetry

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Setup installs the global tracer provider backed by the OTLP collector.
// The returned shutdown flushes pending spans; call it on process exit.
func Setup(ctx context.Context) (shutdown func(context.Context) error, err error) {
	endpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	if endpoint == "" {
		otel.SetTracerProvider(sdktrace.NewTracerProvider())
		return func(context.Context) error { return nil }, nil
	}
	insecure, parseErr := strconv.ParseBool(getenv("OTEL_EXPORTER_OTLP_INSECURE", "true"))
	if parseErr != nil {
		return nil, fmt.Errorf("invalid OTEL_EXPORTER_OTLP_INSECURE: %w", parseErr)
	}
	options := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(endpoint)}
	if insecure {
		options = append(options, otlptracegrpc.WithInsecure())
	}
	exporter, err := otlptracegrpc.New(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("create OTLP exporter: %w", err)
	}
	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithAttributes(attribute.String("service.name", getenv("OTEL_SERVICE_NAME", "latexgrambot"))),
	)
	if err != nil {
		return nil, fmt.Errorf("create resource: %w", err)
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	return provider.Shutdown, nil
}

func getenv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
