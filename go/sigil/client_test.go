package sigil

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestStartGenerationExternalizesArtifacts(t *testing.T) {
	store := NewMemoryRecordStore()
	client, recorder, _ := newTestClient(t, Config{
		RecordStore: store,
		Now: func() time.Time {
			return time.Date(2026, 2, 11, 12, 0, 0, 0, time.UTC)
		},
	})

	requestArtifact, err := NewJSONArtifact(ArtifactKindRequest, "request", map[string]any{
		"model": "claude-sonnet-4-5",
	})
	if err != nil {
		t.Fatalf("new request artifact: %v", err)
	}

	responseArtifact, err := NewJSONArtifact(ArtifactKindResponse, "response", map[string]any{
		"stop_reason": "end_turn",
	})
	if err != nil {
		t.Fatalf("new response artifact: %v", err)
	}

	_, generationRecorder, err := client.StartGeneration(context.Background(), GenerationStart{
		ID:       "gen_test_externalize",
		ThreadID: "thread-1",
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
	})
	if err != nil {
		t.Fatalf("start generation: %v", err)
	}

	err = generationRecorder.End(Generation{
		Input: []Message{
			{Role: RoleUser, Parts: []Part{TextPart("hello")}},
		},
		Output: []Message{
			{Role: RoleAssistant, Parts: []Part{TextPart("hi")}},
		},
		Artifacts: []Artifact{requestArtifact, responseArtifact},
	}, nil)
	if err != nil {
		t.Fatalf("end generation: %v", err)
	}

	if store.Count() != 2 {
		t.Fatalf("expected 2 records in store, got %d", store.Count())
	}

	if generationRecorder.lastGeneration.ID != "gen_test_externalize" {
		t.Fatalf("expected generation id gen_test_externalize, got %q", generationRecorder.lastGeneration.ID)
	}

	span := onlyGenerationSpan(t, recorder.Ended())
	attrs := spanAttributeMap(span)
	if attrs["sigil.generation.id"].AsString() != generationRecorder.lastGeneration.ID {
		t.Fatalf("expected sigil.generation.id=%q, got %q", generationRecorder.lastGeneration.ID, attrs["sigil.generation.id"].AsString())
	}
}

func TestStartGenerationUsesLifecycleTimingWhenMissingOnGeneration(t *testing.T) {
	t0 := time.Date(2026, 2, 11, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(2 * time.Second)
	times := []time.Time{t0, t1}
	idx := 0

	client, recorder, _ := newTestClient(t, Config{
		Now: func() time.Time {
			if idx >= len(times) {
				return times[len(times)-1]
			}
			now := times[idx]
			idx++
			return now
		},
	})

	_, generationRecorder, err := client.StartGeneration(context.Background(), GenerationStart{
		ThreadID: "thread-2",
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
	})
	if err != nil {
		t.Fatalf("start generation: %v", err)
	}

	if err := generationRecorder.End(Generation{}, nil); err != nil {
		t.Fatalf("end generation: %v", err)
	}

	if !generationRecorder.lastGeneration.StartedAt.Equal(t0) {
		t.Fatalf("expected startedAt %s, got %s", t0, generationRecorder.lastGeneration.StartedAt)
	}
	if !generationRecorder.lastGeneration.CompletedAt.Equal(t1) {
		t.Fatalf("expected completedAt %s, got %s", t1, generationRecorder.lastGeneration.CompletedAt)
	}

	span := onlyGenerationSpan(t, recorder.Ended())
	if !span.StartTime().Equal(t0) {
		t.Fatalf("expected span start %s, got %s", t0, span.StartTime())
	}
	if !span.EndTime().Equal(t1) {
		t.Fatalf("expected span end %s, got %s", t1, span.EndTime())
	}
}

func TestStartGenerationCreatesChildSpanAndLinksGenerationToSpan(t *testing.T) {
	client, recorder, tp := newTestClient(t, Config{})
	parentCtx, parent := tp.Tracer("parent").Start(context.Background(), "parent")
	parentSC := parent.SpanContext()

	callCtx, generationRecorder, err := client.StartGeneration(parentCtx, GenerationStart{
		ThreadID: "thread-3",
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
	})
	if err != nil {
		t.Fatalf("start generation: %v", err)
	}
	callSC := trace.SpanContextFromContext(callCtx)
	if !callSC.IsValid() {
		t.Fatalf("expected call span context to be valid")
	}

	if err := generationRecorder.End(Generation{}, nil); err != nil {
		t.Fatalf("end generation: %v", err)
	}
	parent.End()

	if callSC.TraceID() != parentSC.TraceID() {
		t.Fatalf("expected call trace id %q, got %q", parentSC.TraceID().String(), callSC.TraceID().String())
	}
	if generationRecorder.lastGeneration.TraceID != callSC.TraceID().String() {
		t.Fatalf("expected generation trace id %q, got %q", callSC.TraceID().String(), generationRecorder.lastGeneration.TraceID)
	}
	if generationRecorder.lastGeneration.SpanID != callSC.SpanID().String() {
		t.Fatalf("expected generation span id %q, got %q", callSC.SpanID().String(), generationRecorder.lastGeneration.SpanID)
	}

	span := onlyGenerationSpan(t, recorder.Ended())
	if span.Parent().SpanID() != parentSC.SpanID() {
		t.Fatalf("expected parent span id %q, got %q", parentSC.SpanID().String(), span.Parent().SpanID().String())
	}

	attrs := spanAttributeMap(span)
	if attrs["sigil.generation.id"].AsString() != generationRecorder.lastGeneration.ID {
		t.Fatalf("expected sigil.generation.id=%q, got %q", generationRecorder.lastGeneration.ID, attrs["sigil.generation.id"].AsString())
	}
	if attrs["sigil.generation.mode"].AsString() != "generation" {
		t.Fatalf("expected sigil.generation.mode=generation")
	}
}

func TestStartGenerationAndStreamingUseSameOperationSpanNameWithDifferentMode(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{})

	_, syncRecorder, err := client.StartGeneration(context.Background(), GenerationStart{
		ThreadID:      "thread-sync",
		OperationName: "text_completion",
		Model: ModelRef{
			Provider: "openai",
			Name:     "gpt-5",
		},
	})
	if err != nil {
		t.Fatalf("start generation: %v", err)
	}
	if err := syncRecorder.End(Generation{
		Input:  []Message{{Role: RoleUser, Parts: []Part{TextPart("hello")}}},
		Output: []Message{{Role: RoleAssistant, Parts: []Part{TextPart("hi")}}},
	}, nil); err != nil {
		t.Fatalf("end generation: %v", err)
	}

	_, streamRecorder, err := client.StartStreamingGeneration(context.Background(), GenerationStart{
		ThreadID:      "thread-stream",
		OperationName: "text_completion",
		Model: ModelRef{
			Provider: "openai",
			Name:     "gpt-5",
		},
	})
	if err != nil {
		t.Fatalf("start streaming generation: %v", err)
	}
	if err := streamRecorder.End(Generation{
		Input:  []Message{{Role: RoleUser, Parts: []Part{TextPart("hello")}}},
		Output: []Message{{Role: RoleAssistant, Parts: []Part{TextPart("hi")}}},
	}, nil); err != nil {
		t.Fatalf("end streaming generation: %v", err)
	}

	spans := recorder.Ended()
	generationSpans := make([]sdktrace.ReadOnlySpan, 0, 2)
	for _, span := range spans {
		if strings.HasPrefix(span.Name(), "gen_ai.") {
			generationSpans = append(generationSpans, span)
		}
	}
	if len(generationSpans) != 2 {
		t.Fatalf("expected 2 generation spans, got %d", len(generationSpans))
	}

	sawSyncMode := false
	sawStreamMode := false
	for _, span := range generationSpans {
		if span.Name() != "gen_ai.text_completion" {
			t.Fatalf("expected span name gen_ai.text_completion, got %q", span.Name())
		}
		mode := spanAttributeMap(span)["sigil.generation.mode"].AsString()
		if mode == "generation" {
			sawSyncMode = true
		}
		if mode == "streaming_generation" {
			sawStreamMode = true
		}
	}
	if !sawSyncMode || !sawStreamMode {
		t.Fatalf("expected both generation and streaming_generation modes, got sync=%v stream=%v", sawSyncMode, sawStreamMode)
	}
}

func TestStartStreamingGenerationCreatesChildSpan(t *testing.T) {
	client, recorder, tp := newTestClient(t, Config{})
	parentCtx, parent := tp.Tracer("parent").Start(context.Background(), "parent")
	parentSC := parent.SpanContext()

	callCtx, generationRecorder, err := client.StartStreamingGeneration(parentCtx, GenerationStart{
		ThreadID: "thread-stream-child",
		Model: ModelRef{
			Provider: "openai",
			Name:     "gpt-5",
		},
	})
	if err != nil {
		t.Fatalf("start streaming generation: %v", err)
	}

	callSC := trace.SpanContextFromContext(callCtx)
	if !callSC.IsValid() {
		t.Fatalf("expected call span context to be valid")
	}
	if callSC.TraceID() != parentSC.TraceID() {
		t.Fatalf("expected call trace id %q, got %q", parentSC.TraceID().String(), callSC.TraceID().String())
	}

	if err := generationRecorder.End(Generation{
		Input:  []Message{{Role: RoleUser, Parts: []Part{TextPart("hello")}}},
		Output: []Message{{Role: RoleAssistant, Parts: []Part{TextPart("hi")}}},
	}, nil); err != nil {
		t.Fatalf("end streaming generation: %v", err)
	}
	parent.End()

	span := onlyGenerationSpan(t, recorder.Ended())
	if span.Parent().SpanID() != parentSC.SpanID() {
		t.Fatalf("expected parent span id %q, got %q", parentSC.SpanID().String(), span.Parent().SpanID().String())
	}
}

func TestGenerationRecorderEndReturnsCallErrorAndMarksSpanError(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{})

	_, generationRecorder, err := client.StartGeneration(context.Background(), GenerationStart{
		ThreadID: "thread-4",
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
	})
	if err != nil {
		t.Fatalf("start generation: %v", err)
	}

	err = generationRecorder.End(Generation{}, errors.New("provider unavailable"))
	if err == nil {
		t.Fatalf("expected call error")
	}
	if !strings.Contains(err.Error(), "provider unavailable") {
		t.Fatalf("expected provider unavailable error, got %v", err)
	}
	if generationRecorder.lastGeneration.CallError != "provider unavailable" {
		t.Fatalf("expected call error on generation, got %q", generationRecorder.lastGeneration.CallError)
	}
	if generationRecorder.lastGeneration.Metadata["call_error"] != "provider unavailable" {
		t.Fatalf("expected metadata call_error to be set")
	}

	span := onlyGenerationSpan(t, recorder.Ended())
	if got := span.Status().Code; got != codes.Error {
		t.Fatalf("expected error span status, got %v", got)
	}
}

func TestGenerationRecorderEndReturnsRecordErrorAndMarksSpanError(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{
		RecordStore: &failingRecordStore{err: errors.New("store unavailable")},
	})

	artifact, err := NewJSONArtifact(ArtifactKindRequest, "request", map[string]any{"ok": true})
	if err != nil {
		t.Fatalf("new artifact: %v", err)
	}

	_, generationRecorder, err := client.StartGeneration(context.Background(), GenerationStart{
		ThreadID: "thread-5",
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
	})
	if err != nil {
		t.Fatalf("start generation: %v", err)
	}

	err = generationRecorder.End(Generation{
		Artifacts: []Artifact{artifact},
	}, nil)
	if err == nil {
		t.Fatalf("expected record error")
	}
	if !strings.Contains(err.Error(), "store artifact") {
		t.Fatalf("expected store artifact error, got %v", err)
	}

	span := onlyGenerationSpan(t, recorder.Ended())
	if got := span.Status().Code; got != codes.Error {
		t.Fatalf("expected error span status, got %v", got)
	}
}

func TestGenerationRecorderEndSupportsStreamingPattern(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{})

	_, generationRecorder, err := client.StartStreamingGeneration(context.Background(), GenerationStart{
		ThreadID: "thread-6",
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
	})
	if err != nil {
		t.Fatalf("start generation: %v", err)
	}

	chunks := []string{"Hel", "lo", " ", "world"}
	var b strings.Builder
	for _, chunk := range chunks {
		b.WriteString(chunk)
	}

	err = generationRecorder.End(Generation{
		Input: []Message{
			{Role: RoleUser, Parts: []Part{TextPart("Say hello")}},
		},
		Output: []Message{
			{Role: RoleAssistant, Parts: []Part{TextPart(b.String())}},
		},
	}, nil)
	if err != nil {
		t.Fatalf("end generation: %v", err)
	}

	if len(generationRecorder.lastGeneration.Output) != 1 {
		t.Fatalf("expected 1 output message, got %d", len(generationRecorder.lastGeneration.Output))
	}
	if got := generationRecorder.lastGeneration.Output[0].Parts[0].Text; got != "Hello world" {
		t.Fatalf("expected streamed assistant text %q, got %q", "Hello world", got)
	}

	span := onlyGenerationSpan(t, recorder.Ended())
	if got := span.Status().Code; got != codes.Ok {
		t.Fatalf("expected ok span status, got %v", got)
	}
	attrs := spanAttributeMap(span)
	if got := attrs["sigil.generation.mode"].AsString(); got != "streaming_generation" {
		t.Fatalf("expected sigil.generation.mode=streaming_generation, got %q", got)
	}
}

func TestGenerationRecorderEndSetsGenAIAttributes(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{})

	_, generationRecorder, err := client.StartGeneration(context.Background(), GenerationStart{
		ThreadID: "thread-7",
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
	})
	if err != nil {
		t.Fatalf("start generation: %v", err)
	}

	err = generationRecorder.End(Generation{
		OperationName: "text_completion",
		StopReason:    "end_turn",
		Usage: TokenUsage{
			InputTokens:  10,
			OutputTokens: 4,
		},
		Input: []Message{
			{Role: RoleUser, Parts: []Part{TextPart("prompt")}},
		},
		Output: []Message{
			{Role: RoleAssistant, Parts: []Part{TextPart("answer")}},
		},
	}, nil)
	if err != nil {
		t.Fatalf("end generation: %v", err)
	}

	span := onlyGenerationSpan(t, recorder.Ended())
	if span.Name() != "gen_ai.text_completion" {
		t.Fatalf("expected span name gen_ai.text_completion, got %q", span.Name())
	}

	attrs := spanAttributeMap(span)
	if attrs["gen_ai.operation.name"].AsString() != "text_completion" {
		t.Fatalf("expected gen_ai.operation.name=text_completion")
	}
	if attrs["gen_ai.provider.name"].AsString() != "anthropic" {
		t.Fatalf("expected gen_ai.provider.name=anthropic")
	}
	if attrs["gen_ai.request.model"].AsString() != "claude-sonnet-4-5" {
		t.Fatalf("expected gen_ai.request.model=claude-sonnet-4-5")
	}
	if attrs["gen_ai.response.finish_reason"].AsString() != "end_turn" {
		t.Fatalf("expected gen_ai.response.finish_reason=end_turn")
	}
	if attrs["gen_ai.usage.input_tokens"].AsInt64() != 10 {
		t.Fatalf("expected gen_ai.usage.input_tokens=10")
	}
	if attrs["gen_ai.usage.output_tokens"].AsInt64() != 4 {
		t.Fatalf("expected gen_ai.usage.output_tokens=4")
	}
	if attrs["gen_ai.usage.total_tokens"].AsInt64() != 14 {
		t.Fatalf("expected gen_ai.usage.total_tokens=14")
	}
	if attrs["sigil.generation.mode"].AsString() != "generation" {
		t.Fatalf("expected sigil.generation.mode=generation")
	}
}

func TestGenerationRecorderEndIsSingleUse(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{})

	_, generationRecorder, err := client.StartGeneration(context.Background(), GenerationStart{
		ThreadID: "thread-8",
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
	})
	if err != nil {
		t.Fatalf("start generation: %v", err)
	}

	if err := generationRecorder.End(Generation{}, nil); err != nil {
		t.Fatalf("first end generation: %v", err)
	}

	err = generationRecorder.End(Generation{}, nil)
	if err == nil {
		t.Fatalf("expected second End to fail")
	}
	if err.Error() != "generation recorder already ended" {
		t.Fatalf("expected deterministic error, got %q", err.Error())
	}

	if got := countGenerationSpans(recorder.Ended()); got != 1 {
		t.Fatalf("expected 1 generation span, got %d", got)
	}
}

type failingRecordStore struct {
	err error
}

func (s *failingRecordStore) Put(_ context.Context, _ Record) (string, error) {
	return "", s.err
}

func newTestClient(t *testing.T, config Config) (*Client, *tracetest.SpanRecorder, *sdktrace.TracerProvider) {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})

	cfg := config
	cfg.Tracer = tp.Tracer("sigil-test")
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.RecordStore == nil {
		cfg.RecordStore = NewMemoryRecordStore()
	}

	return NewClient(cfg), recorder, tp
}

func countGenerationSpans(spans []sdktrace.ReadOnlySpan) int {
	count := 0
	for _, span := range spans {
		if strings.HasPrefix(span.Name(), "gen_ai.") {
			count++
		}
	}
	return count
}

func onlyGenerationSpan(t *testing.T, spans []sdktrace.ReadOnlySpan) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, span := range spans {
		if strings.HasPrefix(span.Name(), "gen_ai.") {
			return span
		}
	}
	t.Fatalf("no generation span found")
	return nil
}

func spanAttributeMap(span sdktrace.ReadOnlySpan) map[string]attribute.Value {
	out := make(map[string]attribute.Value, len(span.Attributes()))
	for _, attr := range span.Attributes() {
		out[string(attr.Key)] = attr.Value
	}
	return out
}
