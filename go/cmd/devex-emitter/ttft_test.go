package main

import (
	"context"
	"math/rand"
	"testing"

	"github.com/grafana/sigil/sdks/go/sigil"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestStreamEmittersRecordTTFTMetric(t *testing.T) {
	previousTracerProvider := otel.GetTracerProvider()
	previousMeterProvider := otel.GetMeterProvider()
	metricReader := sdkmetric.NewManualReader()
	traceExporter := tracetest.NewInMemoryExporter()
	tracerProvider, meterProvider := installTelemetryProviders(traceExporter, metricReader)
	t.Cleanup(func() {
		otel.SetTracerProvider(previousTracerProvider)
		otel.SetMeterProvider(previousMeterProvider)
		_ = tracerProvider.Shutdown(context.Background())
		_ = meterProvider.Shutdown(context.Background())
	})

	clientCfg := sigil.DefaultConfig()
	clientCfg.GenerationExport.Protocol = sigil.GenerationExportProtocolNone
	clientCfg.GenerationExport.Auth = sigil.AuthConfig{Mode: sigil.ExportAuthModeNone}
	client := sigil.NewClient(clientCfg)
	t.Cleanup(func() {
		_ = client.Shutdown(context.Background())
	})

	tags := map[string]string{"sigil.devex.test": "true"}
	metadata := map[string]any{"conversation_slot": 0}

	if err := emitOpenAIChatCompletionsStream(context.Background(), client, "conv-openai-chat", "Devex GO OpenAI 1", "agent-openai-chat", "v1", tags, metadata, 1); err != nil {
		t.Fatalf("emit openai chat stream: %v", err)
	}
	if err := emitOpenAIResponsesStream(context.Background(), client, "conv-openai-responses", "Devex GO OpenAI 1", "agent-openai-responses", "v1", tags, metadata, 2); err != nil {
		t.Fatalf("emit openai responses stream: %v", err)
	}
	if err := emitAnthropicStream(context.Background(), client, "conv-anthropic", "Devex GO Anthropic 1", "agent-anthropic", "v1", tags, metadata, 3); err != nil {
		t.Fatalf("emit anthropic stream: %v", err)
	}
	if err := emitGeminiStream(context.Background(), client, "conv-gemini", "Devex GO Gemini 1", "agent-gemini", "v1", tags, metadata, 4); err != nil {
		t.Fatalf("emit gemini stream: %v", err)
	}
	if err := emitCustomStream(
		context.Background(),
		client,
		"mistral",
		"conv-custom",
		"Devex GO Mistral 1",
		"agent-custom",
		"v1",
		tags,
		metadata,
		5,
		rand.New(rand.NewSource(42)),
	); err != nil {
		t.Fatalf("emit custom stream: %v", err)
	}

	if err := client.Flush(context.Background()); err != nil {
		t.Fatalf("flush client: %v", err)
	}

	var collected metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}

	ttftCount := histogramCount(collected, "gen_ai.client.time_to_first_token")
	if ttftCount < 5 {
		t.Fatalf("expected TTFT histogram count >= 5 from stream emitters, got %d", ttftCount)
	}
}

func histogramCount(collected metricdata.ResourceMetrics, metricName string) uint64 {
	var total uint64
	for _, scopeMetrics := range collected.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != metricName {
				continue
			}
			histogram, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				continue
			}
			for _, point := range histogram.DataPoints {
				total += point.Count
			}
		}
	}
	return total
}
