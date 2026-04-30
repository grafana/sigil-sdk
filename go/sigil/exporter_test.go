package sigil

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace/noop"
)

func TestGenerationRecorderQueueFullReturnsEnqueueError(t *testing.T) {
	exporter := &capturingGenerationExporter{}
	client := NewClient(Config{
		GenerationExport: GenerationExportConfig{
			QueueSize: 1,
		},
		Tracer:                 noop.NewTracerProvider().Tracer("test"),
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
		Tracer:                 noop.NewTracerProvider().Tracer("test"),
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
		Tracer:                 noop.NewTracerProvider().Tracer("test"),
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
		Tracer:                 noop.NewTracerProvider().Tracer("test"),
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

func TestMergeGenerationExportConfigInsecure(t *testing.T) {
	testCases := []struct {
		name             string
		baseInsecure     *bool
		overrideInsecure *bool
		wantInsecure     *bool
	}{
		{
			name:             "override unset preserves base",
			baseInsecure:     BoolPtr(true),
			overrideInsecure: nil,
			wantInsecure:     BoolPtr(true),
		},
		{
			name:             "override false replaces base true",
			baseInsecure:     BoolPtr(true),
			overrideInsecure: BoolPtr(false),
			wantInsecure:     BoolPtr(false),
		},
		{
			name:             "override true replaces base false",
			baseInsecure:     BoolPtr(false),
			overrideInsecure: BoolPtr(true),
			wantInsecure:     BoolPtr(true),
		},
		{
			name:             "both nil remains nil",
			baseInsecure:     nil,
			overrideInsecure: nil,
			wantInsecure:     nil,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			base := GenerationExportConfig{Insecure: testCase.baseInsecure}
			override := GenerationExportConfig{Insecure: testCase.overrideInsecure}
			got := mergeGenerationExportConfig(base, override)
			if (got.Insecure == nil) != (testCase.wantInsecure == nil) {
				t.Fatalf("insecure=%v, want %v", got.Insecure, testCase.wantInsecure)
			}
			if got.Insecure != nil && *got.Insecure != *testCase.wantInsecure {
				t.Fatalf("insecure=%v, want %v", *got.Insecure, *testCase.wantInsecure)
			}
		})
	}
}

func TestMergeGenerationExportConfigGRPCMessageLimits(t *testing.T) {
	base := GenerationExportConfig{
		GRPCMaxSendMessageBytes:    2 << 20,
		GRPCMaxReceiveMessageBytes: 3 << 20,
	}
	override := GenerationExportConfig{
		GRPCMaxSendMessageBytes:    8 << 20,
		GRPCMaxReceiveMessageBytes: 9 << 20,
	}
	got := mergeGenerationExportConfig(base, override)

	if got.GRPCMaxSendMessageBytes != 8<<20 {
		t.Fatalf("expected grpc max send 8MiB, got %d", got.GRPCMaxSendMessageBytes)
	}
	if got.GRPCMaxReceiveMessageBytes != 9<<20 {
		t.Fatalf("expected grpc max receive 9MiB, got %d", got.GRPCMaxReceiveMessageBytes)
	}
}

func TestNewHTTPGenerationExporterUsesEndpointScheme(t *testing.T) {
	testCases := []struct {
		name     string
		endpoint string
		insecure bool
		wantURL  string
	}{
		{
			name:     "explicit http endpoint remains http",
			endpoint: "http://localhost:8080/api/v1/generations:export",
			insecure: false,
			wantURL:  "http://localhost:8080/api/v1/generations:export",
		},
		{
			name:     "host endpoint uses insecure flag when no scheme",
			endpoint: "localhost:8080/api/v1/generations:export",
			insecure: true,
			wantURL:  "http://localhost:8080/api/v1/generations:export",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			exporter, err := newHTTPGenerationExporter(GenerationExportConfig{
				Endpoint: testCase.endpoint,
				Insecure: BoolPtr(testCase.insecure),
			})
			if err != nil {
				t.Fatalf("newHTTPGenerationExporter failed: %v", err)
			}
			httpExporter, ok := exporter.(*httpGenerationExporter)
			if !ok {
				t.Fatalf("unexpected exporter type %T", exporter)
			}
			if httpExporter.endpoint != testCase.wantURL {
				t.Fatalf("endpoint=%q, want %q", httpExporter.endpoint, testCase.wantURL)
			}
		})
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
