package main

import (
	"context"
	"testing"
	"time"

	"github.com/grafana/sigil/sdks/go/sigil"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestBuildTagEnvelopeIncludesRequiredContractFields(t *testing.T) {
	envelope := buildTagEnvelope(sourceOpenAI, sigil.GenerationModeSync, 2, 1)

	if envelope.tags["sigil.devex.language"] != languageName {
		t.Fatalf("expected language tag %q, got %q", languageName, envelope.tags["sigil.devex.language"])
	}
	if envelope.tags["sigil.devex.provider"] != "openai" {
		t.Fatalf("expected provider tag openai, got %q", envelope.tags["sigil.devex.provider"])
	}
	if envelope.tags["sigil.devex.source"] != "provider_wrapper" {
		t.Fatalf("expected source tag provider_wrapper, got %q", envelope.tags["sigil.devex.source"])
	}
	if envelope.tags["sigil.devex.mode"] != "SYNC" {
		t.Fatalf("expected mode tag SYNC, got %q", envelope.tags["sigil.devex.mode"])
	}

	if envelope.metadata["turn_index"] != 2 {
		t.Fatalf("expected turn_index metadata 2, got %#v", envelope.metadata["turn_index"])
	}
	if envelope.metadata["conversation_slot"] != 1 {
		t.Fatalf("expected conversation_slot metadata 1, got %#v", envelope.metadata["conversation_slot"])
	}
	if envelope.metadata["emitter"] != "sdk-traffic" {
		t.Fatalf("expected emitter metadata sdk-traffic, got %#v", envelope.metadata["emitter"])
	}
	if envelope.metadata["provider_shape"] != "openai_chat_completions" {
		t.Fatalf("expected provider_shape openai_chat_completions, got %#v", envelope.metadata["provider_shape"])
	}
	if envelope.agentPersona == "" {
		t.Fatalf("expected non-empty agent persona")
	}
}

func TestBuildTagEnvelopeOpenAIAlternatesProviderShape(t *testing.T) {
	chatEnvelope := buildTagEnvelope(sourceOpenAI, sigil.GenerationModeSync, 0, 0)
	if chatEnvelope.metadata["provider_shape"] != "openai_chat_completions" {
		t.Fatalf("expected openai_chat_completions, got %#v", chatEnvelope.metadata["provider_shape"])
	}

	responsesEnvelope := buildTagEnvelope(sourceOpenAI, sigil.GenerationModeSync, 1, 0)
	if responsesEnvelope.metadata["provider_shape"] != "openai_responses" {
		t.Fatalf("expected openai_responses, got %#v", responsesEnvelope.metadata["provider_shape"])
	}
}

func TestSourceTagForCustomProvider(t *testing.T) {
	if got := sourceTagFor(sourceCustom); got != "core_custom" {
		t.Fatalf("expected core_custom, got %q", got)
	}
	if got := sourceTagFor(sourceGemini); got != "provider_wrapper" {
		t.Fatalf("expected provider_wrapper, got %q", got)
	}
}

func TestChooseModeUsesThreshold(t *testing.T) {
	if got := chooseMode(10, 30); got != sigil.GenerationModeStream {
		t.Fatalf("expected STREAM, got %s", got)
	}
	if got := chooseMode(30, 30); got != sigil.GenerationModeSync {
		t.Fatalf("expected SYNC, got %s", got)
	}
}

func TestEnsureThreadRotatesConversationAtThreshold(t *testing.T) {
	thread := &threadState{}
	ensureThread(thread, 3, sourceOpenAI, 0)
	if thread.turn != 0 {
		t.Fatalf("expected initial turn 0, got %d", thread.turn)
	}
	if thread.conversationID == "" {
		t.Fatalf("expected conversation id to be set")
	}

	firstID := thread.conversationID
	thread.turn = 3
	// newConversationID uses Unix millis, so ensure the timestamp can advance.
	time.Sleep(2 * time.Millisecond)
	ensureThread(thread, 3, sourceOpenAI, 0)
	if thread.turn != 0 {
		t.Fatalf("expected rotated turn 0, got %d", thread.turn)
	}
	if thread.conversationID == firstID {
		t.Fatalf("expected rotated conversation id to change")
	}
}

func TestLoadConfigReadsTraceGRPCEndpoint(t *testing.T) {
	t.Setenv("SIGIL_TRAFFIC_TRACE_GRPC_ENDPOINT", "collector:14317")

	cfg := loadConfig()

	if cfg.traceGRPC != "collector:14317" {
		t.Fatalf("expected trace GRPC endpoint collector:14317, got %q", cfg.traceGRPC)
	}
}

func TestInstallTelemetryProvidersExportsSpans(t *testing.T) {
	previousProvider := otel.GetTracerProvider()
	previousMeterProvider := otel.GetMeterProvider()
	exporter := tracetest.NewInMemoryExporter()
	tracerProvider, meterProvider := installTelemetryProviders(exporter, sdkmetric.NewManualReader())
	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		otel.SetMeterProvider(previousMeterProvider)
		_ = tracerProvider.Shutdown(context.Background())
		_ = meterProvider.Shutdown(context.Background())
	})

	_, span := otel.Tracer("devex-emitter-test").Start(context.Background(), "synthetic-span")
	span.End()
	if err := tracerProvider.ForceFlush(context.Background()); err != nil {
		t.Fatalf("force flush: %v", err)
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 exported span, got %d", len(spans))
	}

	serviceName := ""
	for _, kv := range spans[0].Resource.Attributes() {
		if string(kv.Key) == "service.name" {
			serviceName = kv.Value.AsString()
			break
		}
	}
	if serviceName == "" {
		t.Fatalf("expected service.name resource attribute")
	}
	if serviceName != traceServiceName {
		t.Fatalf("expected service.name %q, got %q", traceServiceName, serviceName)
	}
}
