package main

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestInstallTelemetryProvidersSetsTracerAndMeterProviders(t *testing.T) {
	previousTracerProvider := otel.GetTracerProvider()
	previousMeterProvider := otel.GetMeterProvider()
	defer func() {
		otel.SetTracerProvider(previousTracerProvider)
		otel.SetMeterProvider(previousMeterProvider)
	}()

	traceExporter := tracetest.NewInMemoryExporter()
	metricReader := sdkmetric.NewManualReader()

	tracerProvider, meterProvider := installTelemetryProviders(traceExporter, metricReader)
	t.Cleanup(func() {
		_ = tracerProvider.Shutdown(context.Background())
		_ = meterProvider.Shutdown(context.Background())
	})

	_, span := otel.Tracer("sigil/devex-telemetry-test").Start(context.Background(), "test-span")
	span.End()
	if err := tracerProvider.ForceFlush(context.Background()); err != nil {
		t.Fatalf("force flush tracer provider: %v", err)
	}

	if got := len(traceExporter.GetSpans()); got == 0 {
		t.Fatalf("expected at least one span to be exported, got %d", got)
	}

	counter, err := otel.Meter("sigil/devex-telemetry-test").Int64Counter("devex_telemetry_counter")
	if err != nil {
		t.Fatalf("create counter: %v", err)
	}
	counter.Add(context.Background(), 1, metric.WithAttributes(attribute.String("source", "test")))

	var collected metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}

	foundCounter := false
	for _, scopeMetrics := range collected.ScopeMetrics {
		for _, metric := range scopeMetrics.Metrics {
			if metric.Name == "devex_telemetry_counter" {
				foundCounter = true
				break
			}
		}
	}
	if !foundCounter {
		t.Fatal("expected devex_telemetry_counter metric to be collected")
	}
}

var _ sdktrace.SpanExporter = (*tracetest.InMemoryExporter)(nil)
