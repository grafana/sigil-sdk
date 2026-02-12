package sigil

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	sigilv1 "github.com/grafana/sigil/sdks/go/sigil/internal/gen/sigil/v1"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestStartGenerationEnqueuesArtifacts(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{
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

	_, generationRecorder := client.StartGeneration(context.Background(), GenerationStart{
		ID:             "gen_test_externalize",
		ConversationID: "conv-1",
		AgentName:      "agent-support",
		AgentVersion:   "v1.2.3",
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
	})

	generationRecorder.SetResult(Generation{
		Input: []Message{
			{Role: RoleUser, Parts: []Part{TextPart("hello")}},
		},
		Output: []Message{
			{Role: RoleAssistant, Parts: []Part{TextPart("hi")}},
		},
		Artifacts: []Artifact{requestArtifact, responseArtifact},
	}, nil)
	generationRecorder.End()

	if err := generationRecorder.Err(); err != nil {
		t.Fatalf("end generation: %v", err)
	}
	if len(generationRecorder.lastGeneration.Artifacts) != 2 {
		t.Fatalf("expected 2 artifacts on generation, got %d", len(generationRecorder.lastGeneration.Artifacts))
	}

	if generationRecorder.lastGeneration.ID != "gen_test_externalize" {
		t.Fatalf("expected generation id gen_test_externalize, got %q", generationRecorder.lastGeneration.ID)
	}
	if generationRecorder.lastGeneration.AgentName != "agent-support" {
		t.Fatalf("expected agent name agent-support, got %q", generationRecorder.lastGeneration.AgentName)
	}
	if generationRecorder.lastGeneration.AgentVersion != "v1.2.3" {
		t.Fatalf("expected agent version v1.2.3, got %q", generationRecorder.lastGeneration.AgentVersion)
	}

	span := onlyGenerationSpan(t, recorder.Ended())
	attrs := spanAttributeMap(span)
	if attrs[spanAttrGenerationID].AsString() != generationRecorder.lastGeneration.ID {
		t.Fatalf("expected sigil.generation.id=%q, got %q", generationRecorder.lastGeneration.ID, attrs[spanAttrGenerationID].AsString())
	}
	if attrs[spanAttrConversationID].AsString() != "conv-1" {
		t.Fatalf("expected gen_ai.conversation.id=conv-1")
	}
	if attrs[spanAttrAgentName].AsString() != "agent-support" {
		t.Fatalf("expected gen_ai.agent.name=agent-support")
	}
	if attrs[spanAttrAgentVersion].AsString() != "v1.2.3" {
		t.Fatalf("expected gen_ai.agent.version=v1.2.3")
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

	_, generationRecorder := client.StartGeneration(context.Background(), GenerationStart{
		ConversationID: "conv-2",
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
	})

	generationRecorder.End()
	if err := generationRecorder.Err(); err != nil {
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

	callCtx, generationRecorder := client.StartGeneration(parentCtx, GenerationStart{
		ConversationID: "conv-3",
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
	})

	callSC := trace.SpanContextFromContext(callCtx)
	if !callSC.IsValid() {
		t.Fatalf("expected call span context to be valid")
	}

	generationRecorder.End()
	if err := generationRecorder.Err(); err != nil {
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
	if attrs[spanAttrGenerationID].AsString() != generationRecorder.lastGeneration.ID {
		t.Fatalf("expected sigil.generation.id=%q, got %q", generationRecorder.lastGeneration.ID, attrs[spanAttrGenerationID].AsString())
	}
}

func TestStartGenerationSpanNameIncludesModelAndOperation(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{})

	_, syncRecorder := client.StartGeneration(context.Background(), GenerationStart{
		ConversationID: "conv-sync",
		OperationName:  "text_completion",
		Model: ModelRef{
			Provider: "openai",
			Name:     "gpt-5",
		},
	})
	syncRecorder.SetResult(Generation{
		Input:  []Message{{Role: RoleUser, Parts: []Part{TextPart("hello")}}},
		Output: []Message{{Role: RoleAssistant, Parts: []Part{TextPart("hi")}}},
	}, nil)
	syncRecorder.End()
	if err := syncRecorder.Err(); err != nil {
		t.Fatalf("end generation: %v", err)
	}

	_, streamRecorder := client.StartStreamingGeneration(context.Background(), GenerationStart{
		ConversationID: "conv-stream",
		OperationName:  "text_completion",
		Model: ModelRef{
			Provider: "openai",
			Name:     "gpt-5",
		},
	})
	streamRecorder.SetResult(Generation{
		Input:  []Message{{Role: RoleUser, Parts: []Part{TextPart("hello")}}},
		Output: []Message{{Role: RoleAssistant, Parts: []Part{TextPart("hi")}}},
	}, nil)
	streamRecorder.End()
	if err := streamRecorder.Err(); err != nil {
		t.Fatalf("end streaming generation: %v", err)
	}

	spans := recorder.Ended()
	generationSpans := make([]sdktrace.ReadOnlySpan, 0, 2)
	for _, span := range spans {
		if isGenerationSpan(span) {
			generationSpans = append(generationSpans, span)
		}
	}
	if len(generationSpans) != 2 {
		t.Fatalf("expected 2 generation spans, got %d", len(generationSpans))
	}

	for _, span := range generationSpans {
		if span.Name() != "text_completion gpt-5" {
			t.Fatalf("expected span name text_completion gpt-5, got %q", span.Name())
		}
		if _, ok := spanAttributeMap(span)["sigil.generation.mode"]; ok {
			t.Fatalf("did not expect sigil.generation.mode")
		}
	}
}

func TestStartGenerationUsesModeAwareDefaultOperationName(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{})

	_, syncRecorder := client.StartGeneration(context.Background(), GenerationStart{
		ConversationID: "conv-default-sync",
		Model: ModelRef{
			Provider: "openai",
			Name:     "gpt-5",
		},
	})
	syncRecorder.SetResult(Generation{
		Input:  []Message{{Role: RoleUser, Parts: []Part{TextPart("hello")}}},
		Output: []Message{{Role: RoleAssistant, Parts: []Part{TextPart("hi")}}},
	}, nil)
	syncRecorder.End()
	if err := syncRecorder.Err(); err != nil {
		t.Fatalf("end sync generation: %v", err)
	}
	if syncRecorder.lastGeneration.Mode != GenerationModeSync {
		t.Fatalf("expected sync mode %q, got %q", GenerationModeSync, syncRecorder.lastGeneration.Mode)
	}

	_, streamRecorder := client.StartStreamingGeneration(context.Background(), GenerationStart{
		ConversationID: "conv-default-stream",
		Model: ModelRef{
			Provider: "openai",
			Name:     "gpt-5",
		},
	})
	streamRecorder.SetResult(Generation{
		Input:  []Message{{Role: RoleUser, Parts: []Part{TextPart("hello")}}},
		Output: []Message{{Role: RoleAssistant, Parts: []Part{TextPart("hi")}}},
	}, nil)
	streamRecorder.End()
	if err := streamRecorder.Err(); err != nil {
		t.Fatalf("end stream generation: %v", err)
	}
	if streamRecorder.lastGeneration.Mode != GenerationModeStream {
		t.Fatalf("expected stream mode %q, got %q", GenerationModeStream, streamRecorder.lastGeneration.Mode)
	}

	spans := recorder.Ended()
	if got := countGenerationSpans(spans); got != 2 {
		t.Fatalf("expected 2 generation spans, got %d", got)
	}

	sawSync := false
	sawStream := false
	for _, span := range spans {
		if !isGenerationSpan(span) {
			continue
		}
		attrs := spanAttributeMap(span)
		switch attrs[spanAttrConversationID].AsString() {
		case "conv-default-sync":
			sawSync = true
			if attrs[spanAttrOperationName].AsString() != defaultOperationNameSync {
				t.Fatalf("expected sync operation %q, got %q", defaultOperationNameSync, attrs[spanAttrOperationName].AsString())
			}
			if span.Name() != defaultOperationNameSync+" gpt-5" {
				t.Fatalf("expected sync span name %q, got %q", defaultOperationNameSync+" gpt-5", span.Name())
			}
		case "conv-default-stream":
			sawStream = true
			if attrs[spanAttrOperationName].AsString() != defaultOperationNameStream {
				t.Fatalf("expected stream operation %q, got %q", defaultOperationNameStream, attrs[spanAttrOperationName].AsString())
			}
			if span.Name() != defaultOperationNameStream+" gpt-5" {
				t.Fatalf("expected stream span name %q, got %q", defaultOperationNameStream+" gpt-5", span.Name())
			}
		}
	}

	if !sawSync || !sawStream {
		t.Fatalf("expected both sync and stream default operation spans")
	}
}

func TestGenerationRecorderSetCallErrorMarksSpanError(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{})

	_, generationRecorder := client.StartGeneration(context.Background(), GenerationStart{
		ConversationID: "conv-4",
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
	})

	generationRecorder.SetCallError(errors.New("provider unavailable"))
	generationRecorder.End()

	err := generationRecorder.Err()
	if err != nil {
		t.Fatalf("expected nil recorder error for call failure, got %v", err)
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
	attrs := spanAttributeMap(span)
	if attrs[spanAttrErrorType].AsString() != "provider_call_error" {
		t.Fatalf("expected error.type=provider_call_error")
	}
}

func TestGenerationRecorderSetResultMappingErrorMarksSpanError(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{})

	_, generationRecorder := client.StartGeneration(context.Background(), GenerationStart{
		ConversationID: "conv-mapping",
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
	})

	generationRecorder.SetResult(Generation{}, errors.New("mapping failed"))
	generationRecorder.End()

	err := generationRecorder.Err()
	if err != nil {
		t.Fatalf("expected nil recorder error for mapping failure, got %v", err)
	}

	span := onlyGenerationSpan(t, recorder.Ended())
	if got := span.Status().Code; got != codes.Error {
		t.Fatalf("expected error span status, got %v", got)
	}
	attrs := spanAttributeMap(span)
	if attrs[spanAttrErrorType].AsString() != "mapping_error" {
		t.Fatalf("expected error.type=mapping_error, got %q", attrs[spanAttrErrorType].AsString())
	}
	// mapping error should NOT set call_error on generation
	if generationRecorder.lastGeneration.CallError != "" {
		t.Fatalf("expected no call_error on generation for mapping error, got %q", generationRecorder.lastGeneration.CallError)
	}
}

func TestGenerationRecorderEndReturnsEnqueueErrorAndMarksSpanError(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{
		GenerationExport: GenerationExportConfig{
			PayloadMaxBytes: 32,
		},
	})

	artifact, err := NewJSONArtifact(ArtifactKindRequest, "request", map[string]any{"payload": strings.Repeat("x", 256)})
	if err != nil {
		t.Fatalf("new artifact: %v", err)
	}

	_, generationRecorder := client.StartGeneration(context.Background(), GenerationStart{
		ConversationID: "conv-5",
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
	})

	generationRecorder.SetResult(Generation{
		Artifacts: []Artifact{artifact},
	}, nil)
	generationRecorder.End()

	enqueueErr := generationRecorder.Err()
	if enqueueErr == nil {
		t.Fatalf("expected enqueue error")
	}
	if !errors.Is(enqueueErr, ErrEnqueueFailed) {
		t.Fatalf("expected enqueue sentinel error, got %v", enqueueErr)
	}

	span := onlyGenerationSpan(t, recorder.Ended())
	if got := span.Status().Code; got != codes.Error {
		t.Fatalf("expected error span status, got %v", got)
	}
	attrs := spanAttributeMap(span)
	if attrs[spanAttrErrorType].AsString() != "enqueue_error" {
		t.Fatalf("expected error.type=enqueue_error")
	}
}

func TestGenerationRecorderEndReturnsValidationErrorAndMarksSpanError(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{})

	_, generationRecorder := client.StartGeneration(context.Background(), GenerationStart{
		ConversationID: "conv-validation",
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
	})

	generationRecorder.SetResult(Generation{
		Input: []Message{
			{Role: RoleUser},
		},
		Output: []Message{
			{Role: RoleAssistant, Parts: []Part{TextPart("ok")}},
		},
	}, nil)
	generationRecorder.End()

	validationErr := generationRecorder.Err()
	if validationErr == nil {
		t.Fatalf("expected validation error")
	}

	span := onlyGenerationSpan(t, recorder.Ended())
	if got := span.Status().Code; got != codes.Error {
		t.Fatalf("expected error span status, got %v", got)
	}
	attrs := spanAttributeMap(span)
	if attrs[spanAttrErrorType].AsString() != "validation_error" {
		t.Fatalf("expected error.type=validation_error")
	}
}

func TestGenerationRecorderEndSupportsStreamingPattern(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{})

	_, generationRecorder := client.StartStreamingGeneration(context.Background(), GenerationStart{
		ConversationID: "conv-6",
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
	})

	chunks := []string{"Hel", "lo", " ", "world"}
	var b strings.Builder
	for _, chunk := range chunks {
		b.WriteString(chunk)
	}

	generationRecorder.SetResult(Generation{
		Input: []Message{
			{Role: RoleUser, Parts: []Part{TextPart("Say hello")}},
		},
		Output: []Message{
			{Role: RoleAssistant, Parts: []Part{TextPart(b.String())}},
		},
	}, nil)
	generationRecorder.End()

	if err := generationRecorder.Err(); err != nil {
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
	if _, ok := attrs["sigil.generation.mode"]; ok {
		t.Fatalf("did not expect sigil.generation.mode")
	}
}

func TestGenerationRecorderEndSetsGenAIAttributes(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{})

	_, generationRecorder := client.StartGeneration(context.Background(), GenerationStart{
		ConversationID: "conv-7",
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
	})

	generationRecorder.SetResult(Generation{
		OperationName:  "text_completion",
		ConversationID: "conv-7",
		ResponseID:     "resp-7",
		ResponseModel:  "claude-sonnet-4-5-20260201",
		StopReason:     "end_turn",
		Usage: TokenUsage{
			InputTokens:           10,
			OutputTokens:          4,
			CacheReadInputTokens:  3,
			CacheWriteInputTokens: 2,
		},
		Input: []Message{
			{Role: RoleUser, Parts: []Part{TextPart("prompt")}},
		},
		Output: []Message{
			{Role: RoleAssistant, Parts: []Part{TextPart("answer")}},
		},
	}, nil)
	generationRecorder.End()

	if err := generationRecorder.Err(); err != nil {
		t.Fatalf("end generation: %v", err)
	}

	span := onlyGenerationSpan(t, recorder.Ended())
	if span.Name() != "text_completion claude-sonnet-4-5" {
		t.Fatalf("expected span name text_completion claude-sonnet-4-5, got %q", span.Name())
	}

	attrs := spanAttributeMap(span)
	if attrs[spanAttrOperationName].AsString() != "text_completion" {
		t.Fatalf("expected gen_ai.operation.name=text_completion")
	}
	if attrs[spanAttrProviderName].AsString() != "anthropic" {
		t.Fatalf("expected gen_ai.provider.name=anthropic")
	}
	if attrs[spanAttrRequestModel].AsString() != "claude-sonnet-4-5" {
		t.Fatalf("expected gen_ai.request.model=claude-sonnet-4-5")
	}
	if attrs[spanAttrConversationID].AsString() != "conv-7" {
		t.Fatalf("expected gen_ai.conversation.id=conv-7")
	}
	if attrs[spanAttrResponseID].AsString() != "resp-7" {
		t.Fatalf("expected gen_ai.response.id=resp-7")
	}
	if attrs[spanAttrResponseModel].AsString() != "claude-sonnet-4-5-20260201" {
		t.Fatalf("expected gen_ai.response.model to be set")
	}
	finishReasons, ok := attrs[spanAttrFinishReasons]
	if !ok {
		t.Fatalf("expected gen_ai.response.finish_reasons")
	}
	if got := finishReasons.AsStringSlice(); len(got) != 1 || got[0] != "end_turn" {
		t.Fatalf("expected finish reasons [end_turn], got %v", got)
	}
	if attrs[spanAttrInputTokens].AsInt64() != 10 {
		t.Fatalf("expected gen_ai.usage.input_tokens=10")
	}
	if attrs[spanAttrOutputTokens].AsInt64() != 4 {
		t.Fatalf("expected gen_ai.usage.output_tokens=4")
	}
	if attrs[spanAttrCacheReadTokens].AsInt64() != 3 {
		t.Fatalf("expected gen_ai.usage.cache_read_input_tokens=3")
	}
	if attrs[spanAttrCacheWriteTokens].AsInt64() != 2 {
		t.Fatalf("expected gen_ai.usage.cache_write_input_tokens=2")
	}
	if _, ok := attrs["gen_ai.response.finish_reason"]; ok {
		t.Fatalf("did not expect gen_ai.response.finish_reason")
	}
	if _, ok := attrs["gen_ai.usage.total_tokens"]; ok {
		t.Fatalf("did not expect gen_ai.usage.total_tokens")
	}
	if _, ok := attrs["gen_ai.usage.reasoning_tokens"]; ok {
		t.Fatalf("did not expect gen_ai.usage.reasoning_tokens")
	}
}

func TestGenerationRecorderEndIsIdempotent(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{})

	_, generationRecorder := client.StartGeneration(context.Background(), GenerationStart{
		ConversationID: "conv-8",
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
	})

	generationRecorder.End()
	if err := generationRecorder.Err(); err != nil {
		t.Fatalf("first end generation: %v", err)
	}

	// Second End is a no-op.
	generationRecorder.End()

	if got := countGenerationSpans(recorder.Ended()); got != 1 {
		t.Fatalf("expected 1 generation span, got %d", got)
	}
}

func TestNilClientReturnsNoOpRecorder(t *testing.T) {
	var client *Client
	ctx, rec := client.StartGeneration(context.Background(), GenerationStart{
		Model: ModelRef{Provider: "test", Name: "test"},
	})
	if ctx == nil {
		t.Fatalf("expected non-nil context")
	}
	// All methods should be safe to call.
	rec.SetCallError(errors.New("test"))
	rec.SetResult(Generation{}, nil)
	rec.End()
	if err := rec.Err(); err != nil {
		t.Fatalf("expected nil error from no-op recorder, got %v", err)
	}
}

func TestNilClientReturnsNoOpToolRecorder(t *testing.T) {
	var client *Client
	ctx, rec := client.StartToolExecution(context.Background(), ToolExecutionStart{
		ToolName: "test",
	})
	if ctx == nil {
		t.Fatalf("expected non-nil context")
	}
	rec.SetExecError(errors.New("test"))
	rec.SetResult(ToolExecutionEnd{})
	rec.End()
	if err := rec.Err(); err != nil {
		t.Fatalf("expected nil error from no-op recorder, got %v", err)
	}
}

func TestEmptyToolNameReturnsNoOpRecorder(t *testing.T) {
	client := NewClient(DefaultConfig())
	_, rec := client.StartToolExecution(context.Background(), ToolExecutionStart{})
	// Should not panic.
	rec.End()
	if err := rec.Err(); err != nil {
		t.Fatalf("expected nil error from no-op recorder, got %v", err)
	}
}

func TestStartToolExecutionSetsExecuteToolAttributes(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{})
	callCtx, toolRecorder := client.StartToolExecution(context.Background(), ToolExecutionStart{
		ToolName:        "weather",
		ToolCallID:      "call_weather",
		ToolType:        "function",
		ToolDescription: "Get weather",
		ConversationID:  "conv-tool",
		AgentName:       "agent-tools",
		AgentVersion:    "2026.02.12",
	})

	if !trace.SpanContextFromContext(callCtx).IsValid() {
		t.Fatalf("expected valid span context in callCtx")
	}

	toolRecorder.End()
	if err := toolRecorder.Err(); err != nil {
		t.Fatalf("end tool execution: %v", err)
	}

	span := onlyToolSpan(t, recorder.Ended())
	if span.Name() != "execute_tool weather" {
		t.Fatalf("unexpected tool span name: %q", span.Name())
	}
	if span.SpanKind() != trace.SpanKindInternal {
		t.Fatalf("expected internal span kind")
	}
	attrs := spanAttributeMap(span)
	if attrs[spanAttrOperationName].AsString() != "execute_tool" {
		t.Fatalf("expected gen_ai.operation.name=execute_tool")
	}
	if attrs[spanAttrToolName].AsString() != "weather" {
		t.Fatalf("expected gen_ai.tool.name=weather")
	}
	if attrs[spanAttrToolCallID].AsString() != "call_weather" {
		t.Fatalf("expected gen_ai.tool.call.id=call_weather")
	}
	if attrs[spanAttrToolType].AsString() != "function" {
		t.Fatalf("expected gen_ai.tool.type=function")
	}
	if attrs[spanAttrToolDescription].AsString() != "Get weather" {
		t.Fatalf("expected gen_ai.tool.description=Get weather")
	}
	if attrs[spanAttrConversationID].AsString() != "conv-tool" {
		t.Fatalf("expected gen_ai.conversation.id=conv-tool")
	}
	if attrs[spanAttrAgentName].AsString() != "agent-tools" {
		t.Fatalf("expected gen_ai.agent.name=agent-tools")
	}
	if attrs[spanAttrAgentVersion].AsString() != "2026.02.12" {
		t.Fatalf("expected gen_ai.agent.version=2026.02.12")
	}
}

func TestToolExecutionRecorderContentCapture(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{})

	_, withContent := client.StartToolExecution(context.Background(), ToolExecutionStart{
		ToolName:       "weather",
		IncludeContent: true,
	})
	withContent.SetResult(ToolExecutionEnd{
		Arguments: map[string]any{"city": "Paris"},
		Result:    map[string]any{"temp_c": 18},
	})
	withContent.End()
	if err := withContent.Err(); err != nil {
		t.Fatalf("end tool execution with content: %v", err)
	}

	_, withoutContent := client.StartToolExecution(context.Background(), ToolExecutionStart{
		ToolName: "weather",
	})
	withoutContent.SetResult(ToolExecutionEnd{
		Arguments: map[string]any{"city": "Paris"},
		Result:    map[string]any{"temp_c": 18},
	})
	withoutContent.End()
	if err := withoutContent.Err(); err != nil {
		t.Fatalf("end tool execution without content: %v", err)
	}

	toolSpans := make([]sdktrace.ReadOnlySpan, 0, 2)
	for _, span := range recorder.Ended() {
		if isToolSpan(span) {
			toolSpans = append(toolSpans, span)
		}
	}
	if len(toolSpans) != 2 {
		t.Fatalf("expected 2 tool spans, got %d", len(toolSpans))
	}

	var sawWithContent, sawWithoutContent bool
	for _, span := range toolSpans {
		attrs := spanAttributeMap(span)
		_, hasArgs := attrs[spanAttrToolCallArguments]
		_, hasResult := attrs[spanAttrToolCallResult]
		if hasArgs && hasResult {
			sawWithContent = true
		}
		if !hasArgs && !hasResult {
			sawWithoutContent = true
		}
	}

	if !sawWithContent || !sawWithoutContent {
		t.Fatalf("expected both content and non-content tool spans")
	}
}

func TestToolExecutionRecorderErrorSetsStatusAndType(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{})
	_, toolRecorder := client.StartToolExecution(context.Background(), ToolExecutionStart{
		ToolName: "weather",
	})

	toolRecorder.SetExecError(errors.New("tool failed"))
	toolRecorder.End()

	if err := toolRecorder.Err(); err == nil {
		t.Fatalf("expected tool error")
	}

	span := onlyToolSpan(t, recorder.Ended())
	if span.Status().Code != codes.Error {
		t.Fatalf("expected error status")
	}
	attrs := spanAttributeMap(span)
	if attrs[spanAttrErrorType].AsString() != "tool_execution_error" {
		t.Fatalf("expected error.type=tool_execution_error")
	}
}

func TestToolExecutionRecorderEndIsIdempotent(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{})
	_, toolRecorder := client.StartToolExecution(context.Background(), ToolExecutionStart{
		ToolName: "weather",
	})

	toolRecorder.End()
	if err := toolRecorder.Err(); err != nil {
		t.Fatalf("first end: %v", err)
	}

	// Second End is a no-op.
	toolRecorder.End()

	if got := countToolSpans(recorder.Ended()); got != 1 {
		t.Fatalf("expected 1 tool span, got %d", got)
	}
}

func TestConversationIDFromContext(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{})

	ctx := WithConversationID(context.Background(), "conv-from-ctx")
	_, generationRecorder := client.StartGeneration(ctx, GenerationStart{
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
	})
	generationRecorder.End()

	span := onlyGenerationSpan(t, recorder.Ended())
	attrs := spanAttributeMap(span)
	if attrs[spanAttrConversationID].AsString() != "conv-from-ctx" {
		t.Fatalf("expected gen_ai.conversation.id=conv-from-ctx, got %q", attrs[spanAttrConversationID].AsString())
	}
}

func TestAgentNameAndVersionFromContext(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{})

	ctx := WithAgentName(context.Background(), "agent-from-ctx")
	ctx = WithAgentVersion(ctx, "v-ctx")
	_, generationRecorder := client.StartGeneration(ctx, GenerationStart{
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
	})
	generationRecorder.End()

	span := onlyGenerationSpan(t, recorder.Ended())
	attrs := spanAttributeMap(span)
	if attrs[spanAttrAgentName].AsString() != "agent-from-ctx" {
		t.Fatalf("expected gen_ai.agent.name=agent-from-ctx, got %q", attrs[spanAttrAgentName].AsString())
	}
	if attrs[spanAttrAgentVersion].AsString() != "v-ctx" {
		t.Fatalf("expected gen_ai.agent.version=v-ctx, got %q", attrs[spanAttrAgentVersion].AsString())
	}
}

func TestExplicitConversationIDOverridesContext(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{})

	ctx := WithConversationID(context.Background(), "ctx-id")
	_, generationRecorder := client.StartGeneration(ctx, GenerationStart{
		ConversationID: "explicit-id",
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
	})
	generationRecorder.End()

	span := onlyGenerationSpan(t, recorder.Ended())
	attrs := spanAttributeMap(span)
	if attrs[spanAttrConversationID].AsString() != "explicit-id" {
		t.Fatalf("expected gen_ai.conversation.id=explicit-id, got %q", attrs[spanAttrConversationID].AsString())
	}
}

func TestExplicitAgentNameAndVersionOverrideContext(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{})

	ctx := WithAgentName(context.Background(), "ctx-agent")
	ctx = WithAgentVersion(ctx, "ctx-version")
	_, generationRecorder := client.StartGeneration(ctx, GenerationStart{
		AgentName:    "start-agent",
		AgentVersion: "start-version",
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
	})
	generationRecorder.End()

	span := onlyGenerationSpan(t, recorder.Ended())
	attrs := spanAttributeMap(span)
	if attrs[spanAttrAgentName].AsString() != "start-agent" {
		t.Fatalf("expected gen_ai.agent.name=start-agent, got %q", attrs[spanAttrAgentName].AsString())
	}
	if attrs[spanAttrAgentVersion].AsString() != "start-version" {
		t.Fatalf("expected gen_ai.agent.version=start-version, got %q", attrs[spanAttrAgentVersion].AsString())
	}
}

func TestToolExecutionConversationIDFromContext(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{})

	ctx := WithConversationID(context.Background(), "conv-tool-ctx")
	_, toolRecorder := client.StartToolExecution(ctx, ToolExecutionStart{
		ToolName: "weather",
	})
	toolRecorder.End()

	span := onlyToolSpan(t, recorder.Ended())
	attrs := spanAttributeMap(span)
	if attrs[spanAttrConversationID].AsString() != "conv-tool-ctx" {
		t.Fatalf("expected gen_ai.conversation.id=conv-tool-ctx, got %q", attrs[spanAttrConversationID].AsString())
	}
}

func TestToolExecutionAgentNameAndVersionFromContext(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{})

	ctx := WithAgentName(context.Background(), "tool-agent-ctx")
	ctx = WithAgentVersion(ctx, "tool-v-ctx")
	_, toolRecorder := client.StartToolExecution(ctx, ToolExecutionStart{
		ToolName: "weather",
	})
	toolRecorder.End()

	span := onlyToolSpan(t, recorder.Ended())
	attrs := spanAttributeMap(span)
	if attrs[spanAttrAgentName].AsString() != "tool-agent-ctx" {
		t.Fatalf("expected gen_ai.agent.name=tool-agent-ctx, got %q", attrs[spanAttrAgentName].AsString())
	}
	if attrs[spanAttrAgentVersion].AsString() != "tool-v-ctx" {
		t.Fatalf("expected gen_ai.agent.version=tool-v-ctx, got %q", attrs[spanAttrAgentVersion].AsString())
	}
}

func TestGenerationResultAgentFieldsOverrideSeed(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{})

	ctx := WithAgentName(context.Background(), "ctx-agent")
	ctx = WithAgentVersion(ctx, "ctx-version")
	_, rec := client.StartGeneration(ctx, GenerationStart{
		AgentName:    "start-agent",
		AgentVersion: "start-version",
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
	})
	rec.SetResult(Generation{
		AgentName:    "result-agent",
		AgentVersion: "result-version",
		Input:        []Message{{Role: RoleUser, Parts: []Part{TextPart("hello")}}},
		Output:       []Message{{Role: RoleAssistant, Parts: []Part{TextPart("hi")}}},
	}, nil)
	rec.End()

	if rec.lastGeneration.AgentName != "result-agent" {
		t.Fatalf("expected last generation agent name result-agent, got %q", rec.lastGeneration.AgentName)
	}
	if rec.lastGeneration.AgentVersion != "result-version" {
		t.Fatalf("expected last generation agent version result-version, got %q", rec.lastGeneration.AgentVersion)
	}

	span := onlyGenerationSpan(t, recorder.Ended())
	attrs := spanAttributeMap(span)
	if attrs[spanAttrAgentName].AsString() != "result-agent" {
		t.Fatalf("expected gen_ai.agent.name=result-agent, got %q", attrs[spanAttrAgentName].AsString())
	}
	if attrs[spanAttrAgentVersion].AsString() != "result-version" {
		t.Fatalf("expected gen_ai.agent.version=result-version, got %q", attrs[spanAttrAgentVersion].AsString())
	}
}

func TestEmptyAgentFieldsAreNotEmitted(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{})

	_, rec := client.StartGeneration(context.Background(), GenerationStart{
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
	})
	rec.End()

	span := onlyGenerationSpan(t, recorder.Ended())
	attrs := spanAttributeMap(span)
	if _, ok := attrs[spanAttrAgentName]; ok {
		t.Fatalf("did not expect %s attribute", spanAttrAgentName)
	}
	if _, ok := attrs[spanAttrAgentVersion]; ok {
		t.Fatalf("did not expect %s attribute", spanAttrAgentVersion)
	}
}

func TestSentinelErrorsAreMatchable(t *testing.T) {
	client, _, _ := newTestClient(t, Config{
		GenerationExport: GenerationExportConfig{
			PayloadMaxBytes: 32,
		},
	})

	artifact, err := NewJSONArtifact(ArtifactKindRequest, "request", map[string]any{"payload": strings.Repeat("x", 256)})
	if err != nil {
		t.Fatalf("new artifact: %v", err)
	}

	_, rec := client.StartGeneration(context.Background(), GenerationStart{
		Model: ModelRef{Provider: "test", Name: "test"},
	})
	rec.SetResult(Generation{Artifacts: []Artifact{artifact}}, nil)
	rec.End()

	if !errors.Is(rec.Err(), ErrEnqueueFailed) {
		t.Fatalf("expected errors.Is(err, ErrEnqueueFailed), got %v", rec.Err())
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

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
	if cfg.testGenerationExporter == nil {
		cfg.testGenerationExporter = &capturingGenerationExporter{}
	}

	client := NewClient(cfg)
	t.Cleanup(func() {
		_ = client.Shutdown(context.Background())
	})
	return client, recorder, tp
}

type capturingGenerationExporter struct {
	mu       sync.Mutex
	requests []*sigilv1.ExportGenerationsRequest
	err      error
}

func (e *capturingGenerationExporter) Export(_ context.Context, req *sigilv1.ExportGenerationsRequest) (*sigilv1.ExportGenerationsResponse, error) {
	if e.err != nil {
		return nil, e.err
	}

	e.mu.Lock()
	e.requests = append(e.requests, req)
	e.mu.Unlock()

	results := make([]*sigilv1.ExportGenerationResult, len(req.Generations))
	for i := range req.Generations {
		results[i] = &sigilv1.ExportGenerationResult{
			GenerationId: req.Generations[i].Id,
			Accepted:     true,
		}
	}
	return &sigilv1.ExportGenerationsResponse{Results: results}, nil
}

func (e *capturingGenerationExporter) Shutdown(_ context.Context) error {
	return nil
}

func countGenerationSpans(spans []sdktrace.ReadOnlySpan) int {
	count := 0
	for _, span := range spans {
		if isGenerationSpan(span) {
			count++
		}
	}
	return count
}

func countToolSpans(spans []sdktrace.ReadOnlySpan) int {
	count := 0
	for _, span := range spans {
		if isToolSpan(span) {
			count++
		}
	}
	return count
}

func onlyGenerationSpan(t *testing.T, spans []sdktrace.ReadOnlySpan) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, span := range spans {
		if isGenerationSpan(span) {
			return span
		}
	}
	t.Fatalf("no generation span found")
	return nil
}

func onlyToolSpan(t *testing.T, spans []sdktrace.ReadOnlySpan) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, span := range spans {
		if isToolSpan(span) {
			return span
		}
	}
	t.Fatalf("no tool span found")
	return nil
}

func isGenerationSpan(span sdktrace.ReadOnlySpan) bool {
	attrs := spanAttributeMap(span)
	op, ok := attrs[spanAttrOperationName]
	return ok && op.AsString() != "execute_tool"
}

func isToolSpan(span sdktrace.ReadOnlySpan) bool {
	attrs := spanAttributeMap(span)
	op, ok := attrs[spanAttrOperationName]
	return ok && op.AsString() == "execute_tool"
}

func spanAttributeMap(span sdktrace.ReadOnlySpan) map[string]attribute.Value {
	out := make(map[string]attribute.Value, len(span.Attributes()))
	for _, attr := range span.Attributes() {
		out[string(attr.Key)] = attr.Value
	}
	return out
}
