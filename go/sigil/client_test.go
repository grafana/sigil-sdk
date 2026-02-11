package sigil

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace"
)

func TestRecordGenerationExternalizesArtifacts(t *testing.T) {
	store := NewMemoryRecordStore()
	client := NewClient(Config{
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

	ref, err := client.RecordGeneration(context.Background(), Generation{
		ID:       "gen_test_externalize",
		ThreadID: "thread-1",
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
		Messages: []Message{
			{
				Role:  RoleUser,
				Parts: []Part{TextPart("hello")},
			},
			{
				Role:  RoleAssistant,
				Parts: []Part{TextPart("hi")},
			},
		},
		Artifacts: []Artifact{requestArtifact, responseArtifact},
	})
	if err != nil {
		t.Fatalf("record generation: %v", err)
	}

	if ref.GenerationID != "gen_test_externalize" {
		t.Fatalf("expected generation id gen_test_externalize, got %q", ref.GenerationID)
	}
	if len(ref.ArtifactRefs) != 2 {
		t.Fatalf("expected 2 artifact refs, got %d", len(ref.ArtifactRefs))
	}
	if store.Count() != 2 {
		t.Fatalf("expected 2 records in store, got %d", store.Count())
	}
	for _, artifactRef := range ref.ArtifactRefs {
		if artifactRef.RecordID == "" {
			t.Fatalf("expected artifact record id to be set")
		}
		if artifactRef.URI == "" {
			t.Fatalf("expected artifact URI to be set")
		}
	}
}

func TestRecordGenerationAndLifecycleParity(t *testing.T) {
	client := NewClient(Config{
		Now: func() time.Time {
			return time.Date(2026, 2, 11, 12, 0, 0, 0, time.UTC)
		},
	})

	generation := Generation{
		ID:       "gen_parity",
		ThreadID: "thread-2",
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
		Messages: []Message{
			{
				Role:  RoleUser,
				Parts: []Part{TextPart("question")},
			},
			{
				Role:  RoleAssistant,
				Parts: []Part{TextPart("answer")},
			},
		},
		Usage: TokenUsage{
			InputTokens:  10,
			OutputTokens: 4,
		},
	}

	oneShot, err := client.RecordGeneration(context.Background(), generation)
	if err != nil {
		t.Fatalf("one-shot record: %v", err)
	}

	handle, _, err := client.StartGeneration(context.Background(), GenerationStart{
		ID:       generation.ID,
		ThreadID: generation.ThreadID,
		Model:    generation.Model,
		Messages: generation.Messages[:1],
	})
	if err != nil {
		t.Fatalf("start generation: %v", err)
	}

	handle.SetGeneration(generation)
	lifecycle, err := handle.Finish(context.Background(), nil)
	if err != nil {
		t.Fatalf("finish generation: %v", err)
	}

	if oneShot.GenerationID != lifecycle.GenerationID {
		t.Fatalf("expected equal generation ids, got %q and %q", oneShot.GenerationID, lifecycle.GenerationID)
	}
	if len(oneShot.ArtifactRefs) != len(lifecycle.ArtifactRefs) {
		t.Fatalf("expected same artifact refs count, got %d and %d", len(oneShot.ArtifactRefs), len(lifecycle.ArtifactRefs))
	}
}

func TestFinishMarksCallError(t *testing.T) {
	client := NewClient(DefaultConfig())

	handle, _, err := client.StartGeneration(context.Background(), GenerationStart{
		ID:       "gen_with_error",
		ThreadID: "thread-3",
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
		Messages: []Message{
			{
				Role:  RoleUser,
				Parts: []Part{TextPart("hello")},
			},
		},
	})
	if err != nil {
		t.Fatalf("start generation: %v", err)
	}

	handle.SetGeneration(Generation{
		ID:       "gen_with_error",
		ThreadID: "thread-3",
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
		Messages: []Message{
			{
				Role:  RoleUser,
				Parts: []Part{TextPart("hello")},
			},
			{
				Role:  RoleAssistant,
				Parts: []Part{TextPart("world")},
			},
		},
	})

	if _, err := handle.Finish(context.Background(), errors.New("provider unavailable")); err != nil {
		t.Fatalf("finish generation: %v", err)
	}
}

func TestRecordGenerationAutoTraceSpanLink(t *testing.T) {
	client := NewClient(DefaultConfig())

	traceID, err := trace.TraceIDFromHex("00112233445566778899aabbccddeeff")
	if err != nil {
		t.Fatalf("trace id from hex: %v", err)
	}
	spanID, err := trace.SpanIDFromHex("0011223344556677")
	if err != nil {
		t.Fatalf("span id from hex: %v", err)
	}

	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), spanContext)

	ref, err := client.RecordGeneration(ctx, Generation{
		ID:       "gen_trace_link",
		ThreadID: "thread-4",
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
		Messages: []Message{
			{
				Role:  RoleUser,
				Parts: []Part{TextPart("hello")},
			},
		},
	})
	if err != nil {
		t.Fatalf("record generation: %v", err)
	}

	if ref.TraceID != traceID.String() {
		t.Fatalf("expected trace id %q, got %q", traceID.String(), ref.TraceID)
	}
	if ref.SpanID != spanID.String() {
		t.Fatalf("expected span id %q, got %q", spanID.String(), ref.SpanID)
	}
}

func TestRecordGenerationDoesNotOverrideTraceSpan(t *testing.T) {
	client := NewClient(DefaultConfig())

	traceID, _ := trace.TraceIDFromHex("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	spanID, _ := trace.SpanIDFromHex("bbbbbbbbbbbbbbbb")
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
	}))

	ref, err := client.RecordGeneration(ctx, Generation{
		ID:       "gen_manual_trace",
		ThreadID: "thread-5",
		TraceID:  "manual-trace-id",
		SpanID:   "manual-span-id",
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
		Messages: []Message{
			{
				Role:  RoleUser,
				Parts: []Part{TextPart("hello")},
			},
		},
	})
	if err != nil {
		t.Fatalf("record generation: %v", err)
	}

	if ref.TraceID != "manual-trace-id" {
		t.Fatalf("expected manual trace id, got %q", ref.TraceID)
	}
	if ref.SpanID != "manual-span-id" {
		t.Fatalf("expected manual span id, got %q", ref.SpanID)
	}
}

func TestLifecyclePreservesTraceSpanFromStartContext(t *testing.T) {
	client := NewClient(DefaultConfig())

	traceID, _ := trace.TraceIDFromHex("1234567890abcdef1234567890abcdef")
	spanID, _ := trace.SpanIDFromHex("0123456789abcdef")
	startCtx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
	}))

	handle, _, err := client.StartGeneration(startCtx, GenerationStart{
		ID:       "gen_lifecycle_trace",
		ThreadID: "thread-6",
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
		Messages: []Message{
			{
				Role:  RoleUser,
				Parts: []Part{TextPart("question")},
			},
		},
	})
	if err != nil {
		t.Fatalf("start generation: %v", err)
	}

	handle.SetGeneration(Generation{
		ID:       "gen_lifecycle_trace",
		ThreadID: "thread-6",
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
		Messages: []Message{
			{
				Role:  RoleUser,
				Parts: []Part{TextPart("question")},
			},
			{
				Role:  RoleAssistant,
				Parts: []Part{TextPart("answer")},
			},
		},
	})

	ref, err := handle.Finish(context.Background(), nil)
	if err != nil {
		t.Fatalf("finish generation: %v", err)
	}

	if ref.TraceID != traceID.String() {
		t.Fatalf("expected trace id %q, got %q", traceID.String(), ref.TraceID)
	}
	if ref.SpanID != spanID.String() {
		t.Fatalf("expected span id %q, got %q", spanID.String(), ref.SpanID)
	}
}
