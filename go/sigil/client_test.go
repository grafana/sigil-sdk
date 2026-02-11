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
		ID:             "gen_test_externalize",
		ConversationID: "conv-1",
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
	if attrs[spanAttrGenerationID].AsString() != generationRecorder.lastGeneration.ID {
		t.Fatalf("expected sigil.generation.id=%q, got %q", generationRecorder.lastGeneration.ID, attrs[spanAttrGenerationID].AsString())
	}
	if attrs[spanAttrConversationID].AsString() != "conv-1" {
		t.Fatalf("expected gen_ai.conversation.id=conv-1")
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
		ConversationID: "conv-2",
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
		ConversationID: "conv-3",
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
	if attrs[spanAttrGenerationID].AsString() != generationRecorder.lastGeneration.ID {
		t.Fatalf("expected sigil.generation.id=%q, got %q", generationRecorder.lastGeneration.ID, attrs[spanAttrGenerationID].AsString())
	}
}

func TestStartGenerationAndStreamingUseSameOperationSpanName(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{})

	_, syncRecorder, err := client.StartGeneration(context.Background(), GenerationStart{
		ConversationID: "conv-sync",
		OperationName:  "text_completion",
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
		ConversationID: "conv-stream",
		OperationName:  "text_completion",
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

func TestStartStreamingGenerationCreatesChildSpan(t *testing.T) {
	client, recorder, tp := newTestClient(t, Config{})
	parentCtx, parent := tp.Tracer("parent").Start(context.Background(), "parent")
	parentSC := parent.SpanContext()

	callCtx, generationRecorder, err := client.StartStreamingGeneration(parentCtx, GenerationStart{
		ConversationID: "conv-stream-child",
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
		ConversationID: "conv-4",
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
	attrs := spanAttributeMap(span)
	if attrs[spanAttrErrorType].AsString() != "provider_call_error" {
		t.Fatalf("expected error.type=provider_call_error")
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
		ConversationID: "conv-5",
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
	attrs := spanAttributeMap(span)
	if attrs[spanAttrErrorType].AsString() != "record_store_error" {
		t.Fatalf("expected error.type=record_store_error")
	}
}

func TestGenerationRecorderEndReturnsValidationErrorAndMarksSpanError(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{})

	_, generationRecorder, err := client.StartGeneration(context.Background(), GenerationStart{
		ConversationID: "conv-validation",
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
			{Role: RoleUser},
		},
		Output: []Message{
			{Role: RoleAssistant, Parts: []Part{TextPart("ok")}},
		},
	}, nil)
	if err == nil {
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

	_, generationRecorder, err := client.StartStreamingGeneration(context.Background(), GenerationStart{
		ConversationID: "conv-6",
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
	if _, ok := attrs["sigil.generation.mode"]; ok {
		t.Fatalf("did not expect sigil.generation.mode")
	}
}

func TestGenerationRecorderEndSetsGenAIAttributes(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{})

	_, generationRecorder, err := client.StartGeneration(context.Background(), GenerationStart{
		ConversationID: "conv-7",
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
	})
	if err != nil {
		t.Fatalf("start generation: %v", err)
	}

	err = generationRecorder.End(Generation{
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
	if err != nil {
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

func TestGenerationRecorderEndIsSingleUse(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{})

	_, generationRecorder, err := client.StartGeneration(context.Background(), GenerationStart{
		ConversationID: "conv-8",
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

func TestStartToolExecutionRequiresToolName(t *testing.T) {
	client := NewClient(DefaultConfig())
	_, _, err := client.StartToolExecution(context.Background(), ToolExecutionStart{})
	if err == nil {
		t.Fatalf("expected tool name error")
	}
}

func TestToolExecutionRecorderEndSetsExecuteToolAttributes(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{})
	callCtx, toolRecorder, err := client.StartToolExecution(context.Background(), ToolExecutionStart{
		ToolName:        "weather",
		ToolCallID:      "call_weather",
		ToolType:        "function",
		ToolDescription: "Get weather",
		ConversationID:  "conv-tool",
	})
	if err != nil {
		t.Fatalf("start tool execution: %v", err)
	}
	if !trace.SpanContextFromContext(callCtx).IsValid() {
		t.Fatalf("expected valid span context in callCtx")
	}

	if err := toolRecorder.End(ToolExecutionEnd{}, nil); err != nil {
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
}

func TestToolExecutionRecorderContentCapture(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{})

	_, withContent, err := client.StartToolExecution(context.Background(), ToolExecutionStart{
		ToolName:       "weather",
		IncludeContent: true,
	})
	if err != nil {
		t.Fatalf("start tool execution with content: %v", err)
	}
	if err := withContent.End(ToolExecutionEnd{
		Arguments: map[string]any{"city": "Paris"},
		Result:    map[string]any{"temp_c": 18},
	}, nil); err != nil {
		t.Fatalf("end tool execution with content: %v", err)
	}

	_, withoutContent, err := client.StartToolExecution(context.Background(), ToolExecutionStart{
		ToolName: "weather",
	})
	if err != nil {
		t.Fatalf("start tool execution without content: %v", err)
	}
	if err := withoutContent.End(ToolExecutionEnd{
		Arguments: map[string]any{"city": "Paris"},
		Result:    map[string]any{"temp_c": 18},
	}, nil); err != nil {
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
	_, toolRecorder, err := client.StartToolExecution(context.Background(), ToolExecutionStart{
		ToolName: "weather",
	})
	if err != nil {
		t.Fatalf("start tool execution: %v", err)
	}

	execErr := errors.New("tool failed")
	if err := toolRecorder.End(ToolExecutionEnd{}, execErr); err == nil {
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

func TestToolExecutionRecorderEndIsSingleUse(t *testing.T) {
	client, recorder, _ := newTestClient(t, Config{})
	_, toolRecorder, err := client.StartToolExecution(context.Background(), ToolExecutionStart{
		ToolName: "weather",
	})
	if err != nil {
		t.Fatalf("start tool execution: %v", err)
	}

	if err := toolRecorder.End(ToolExecutionEnd{}, nil); err != nil {
		t.Fatalf("first end: %v", err)
	}
	err = toolRecorder.End(ToolExecutionEnd{}, nil)
	if err == nil {
		t.Fatalf("expected second End to fail")
	}
	if err.Error() != "tool execution recorder already ended" {
		t.Fatalf("expected deterministic error, got %q", err.Error())
	}

	if got := countToolSpans(recorder.Ended()); got != 1 {
		t.Fatalf("expected 1 tool span, got %d", got)
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
