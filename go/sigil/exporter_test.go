package sigil

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace"
)

func TestGenerationRecorderQueueFullReturnsEnqueueError(t *testing.T) {
	exporter := &capturingGenerationExporter{}
	client := NewClient(Config{
		GenerationExport: GenerationExportConfig{
			QueueSize: 1,
		},
		Tracer:                 trace.NewNoopTracerProvider().Tracer("test"),
		Now:                    time.Now,
		testDisableWorker:      true,
		testGenerationExporter: exporter,
	})
	t.Cleanup(func() {
		_ = client.Shutdown(context.Background())
	})

	_, rec1 := client.StartGeneration(context.Background(), GenerationStart{Model: ModelRef{Provider: "openai", Name: "gpt-5"}})
	rec1.SetResult(Generation{
		Input:  []Message{UserTextMessage("hello")},
		Output: []Message{AssistantTextMessage("hi")},
	}, nil)
	rec1.End()
	if err := rec1.Err(); err != nil {
		t.Fatalf("unexpected error on first enqueue: %v", err)
	}

	_, rec2 := client.StartGeneration(context.Background(), GenerationStart{Model: ModelRef{Provider: "openai", Name: "gpt-5"}})
	rec2.SetResult(Generation{
		Input:  []Message{UserTextMessage("hello")},
		Output: []Message{AssistantTextMessage("hi")},
	}, nil)
	rec2.End()

	if !errors.Is(rec2.Err(), ErrEnqueueFailed) {
		t.Fatalf("expected enqueue failure sentinel, got %v", rec2.Err())
	}
	if !errors.Is(rec2.Err(), ErrQueueFull) {
		t.Fatalf("expected queue full sentinel, got %v", rec2.Err())
	}
}

func TestGenerationExporterFlushesByBatchSize(t *testing.T) {
	exporter := &capturingGenerationExporter{}
	client := NewClient(Config{
		GenerationExport: GenerationExportConfig{
			QueueSize:      10,
			BatchSize:      2,
			FlushInterval:  time.Hour,
			MaxRetries:     1,
			InitialBackoff: time.Millisecond,
			MaxBackoff:     time.Millisecond,
		},
		Tracer:                 trace.NewNoopTracerProvider().Tracer("test"),
		Now:                    time.Now,
		testGenerationExporter: exporter,
	})
	t.Cleanup(func() {
		_ = client.Shutdown(context.Background())
	})

	for i := 0; i < 2; i++ {
		_, rec := client.StartGeneration(context.Background(), GenerationStart{Model: ModelRef{Provider: "openai", Name: "gpt-5"}})
		rec.SetResult(Generation{
			Input:  []Message{UserTextMessage("hello")},
			Output: []Message{AssistantTextMessage("hi")},
		}, nil)
		rec.End()
		if err := rec.Err(); err != nil {
			t.Fatalf("unexpected enqueue error: %v", err)
		}
	}

	if err := waitForCondition(300*time.Millisecond, func() bool {
		exporter.mu.Lock()
		defer exporter.mu.Unlock()
		return len(exporter.requests) == 1 && len(exporter.requests[0].Generations) == 2
	}); err != nil {
		t.Fatalf("batch size flush not observed: %v", err)
	}
}

func TestGenerationExporterFlushesByInterval(t *testing.T) {
	exporter := &capturingGenerationExporter{}
	client := NewClient(Config{
		GenerationExport: GenerationExportConfig{
			QueueSize:      10,
			BatchSize:      10,
			FlushInterval:  15 * time.Millisecond,
			MaxRetries:     1,
			InitialBackoff: time.Millisecond,
			MaxBackoff:     time.Millisecond,
		},
		Tracer:                 trace.NewNoopTracerProvider().Tracer("test"),
		Now:                    time.Now,
		testGenerationExporter: exporter,
	})
	t.Cleanup(func() {
		_ = client.Shutdown(context.Background())
	})

	_, rec := client.StartGeneration(context.Background(), GenerationStart{Model: ModelRef{Provider: "openai", Name: "gpt-5"}})
	rec.SetResult(Generation{
		Input:  []Message{UserTextMessage("hello")},
		Output: []Message{AssistantTextMessage("hi")},
	}, nil)
	rec.End()
	if err := rec.Err(); err != nil {
		t.Fatalf("unexpected enqueue error: %v", err)
	}

	if err := waitForCondition(500*time.Millisecond, func() bool {
		exporter.mu.Lock()
		defer exporter.mu.Unlock()
		return len(exporter.requests) >= 1 && len(exporter.requests[0].Generations) == 1
	}); err != nil {
		t.Fatalf("interval flush not observed: %v", err)
	}
}

func TestShutdownFlushesPendingGenerations(t *testing.T) {
	exporter := &capturingGenerationExporter{}
	client := NewClient(Config{
		GenerationExport: GenerationExportConfig{
			QueueSize:      10,
			BatchSize:      10,
			FlushInterval:  time.Hour,
			MaxRetries:     1,
			InitialBackoff: time.Millisecond,
			MaxBackoff:     time.Millisecond,
		},
		Tracer:                 trace.NewNoopTracerProvider().Tracer("test"),
		Now:                    time.Now,
		testGenerationExporter: exporter,
	})

	_, rec := client.StartGeneration(context.Background(), GenerationStart{Model: ModelRef{Provider: "openai", Name: "gpt-5"}})
	rec.SetResult(Generation{
		Input:  []Message{UserTextMessage("hello")},
		Output: []Message{AssistantTextMessage("hi")},
	}, nil)
	rec.End()
	if err := rec.Err(); err != nil {
		t.Fatalf("unexpected enqueue error: %v", err)
	}

	if err := client.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	exporter.mu.Lock()
	defer exporter.mu.Unlock()
	if len(exporter.requests) != 1 {
		t.Fatalf("expected one flush on shutdown, got %d", len(exporter.requests))
	}
	if len(exporter.requests[0].Generations) != 1 {
		t.Fatalf("expected one generation in shutdown flush, got %d", len(exporter.requests[0].Generations))
	}
}

func waitForCondition(timeout time.Duration, condition func() bool) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return errors.New("condition timed out")
}
