package main

import (
	"context"
	"math/rand"
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
	if envelope.conversationTitle != "Devex GO OpenAI 2" {
		t.Fatalf("expected conversation title %q, got %q", "Devex GO OpenAI 2", envelope.conversationTitle)
	}
}

func TestConversationTitleForUsesStableProviderAndSlot(t *testing.T) {
	if got := conversationTitleFor(sourceGemini, 0); got != "Devex GO Gemini 1" {
		t.Fatalf("expected Gemini title, got %q", got)
	}
	if got := conversationTitleFor(sourceCustom, 2); got != "Devex GO Mistral 3" {
		t.Fatalf("expected Mistral title, got %q", got)
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

func TestEmitSyntheticLifecycleSpansProducesTraceRichSpanCount(t *testing.T) {
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

	ctx, root := otel.Tracer("devex-emitter-test").Start(context.Background(), "root")
	traceEnd := time.Now()
	traceStart := traceEnd.Add(-3 * time.Second)
	syntheticCount := emitSyntheticLifecycleSpans(ctx, rand.New(rand.NewSource(42)), traceStart, traceEnd)
	root.End()
	if err := tracerProvider.ForceFlush(context.Background()); err != nil {
		t.Fatalf("force flush: %v", err)
	}

	if syntheticCount < minSyntheticSpans || syntheticCount > maxSyntheticSpans {
		t.Fatalf("expected synthetic count in [%d,%d], got %d", minSyntheticSpans, maxSyntheticSpans, syntheticCount)
	}

	spans := exporter.GetSpans()
	if len(spans) != syntheticCount+1 {
		t.Fatalf("expected root + synthetic spans (%d), got %d", syntheticCount+1, len(spans))
	}

	syntheticSeen := 0
	for _, span := range spans {
		if span.Name == "root" {
			continue
		}
		syntheticSeen++
		foundCategory := false
		var simulatedDurationMs int64 = -1
		for _, kv := range span.Attributes {
			if string(kv.Key) == "sigil.synthetic.category" && kv.Value.AsString() != "" {
				foundCategory = true
			}
			if string(kv.Key) == "sigil.synthetic.simulated_duration_ms" {
				simulatedDurationMs = kv.Value.AsInt64()
			}
		}
		if !span.EndTime.After(span.StartTime) {
			t.Fatalf("expected synthetic span %q to have end time after start time", span.Name)
		}
		if !foundCategory {
			t.Fatalf("expected synthetic span %q to include sigil.synthetic.category attribute", span.Name)
		}
		if simulatedDurationMs <= 0 {
			t.Fatalf("expected synthetic span %q to include positive simulated duration attribute", span.Name)
		}
		actualDurationMs := span.EndTime.Sub(span.StartTime).Milliseconds()
		if actualDurationMs != simulatedDurationMs {
			t.Fatalf("expected synthetic span %q duration to match simulated duration: actual=%dms simulated=%dms", span.Name, actualDurationMs, simulatedDurationMs)
		}
		if span.StartTime.Before(traceStart) {
			t.Fatalf("expected synthetic span %q start time (%s) to be inside trace window start (%s)", span.Name, span.StartTime, traceStart)
		}
		if span.EndTime.After(traceEnd) {
			t.Fatalf("expected synthetic span %q end time (%s) to be inside trace window end (%s)", span.Name, span.EndTime, traceEnd)
		}
	}
	if syntheticSeen != syntheticCount {
		t.Fatalf("expected %d synthetic spans, saw %d", syntheticCount, syntheticSeen)
	}
}
