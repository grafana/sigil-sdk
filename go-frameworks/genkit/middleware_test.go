package genkit_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	sigilgenkit "github.com/grafana/sigil-sdk/go-frameworks/genkit"

	"github.com/firebase/genkit/go/ai"
	"github.com/grafana/sigil-sdk/go/sigil"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestSyncGeneration(t *testing.T) {
	env := newTestEnv(t, sigilgenkit.Options{
		AgentName:    "test-agent",
		AgentVersion: "1.0.0",
	})

	model := &fakeModelArg{name: "anthropic/claude-3.5-sonnet"}
	mw := env.Plugin.Middleware(model)

	fakeModel := func(_ context.Context, req *ai.ModelRequest, cb ai.ModelStreamCallback) (*ai.ModelResponse, error) {
		return &ai.ModelResponse{
			Message: &ai.Message{
				Role:    ai.RoleModel,
				Content: []*ai.Part{ai.NewTextPart("Hello!")},
			},
			FinishReason: ai.FinishReasonStop,
			Usage: &ai.GenerationUsage{
				InputTokens:  10,
				OutputTokens: 5,
				TotalTokens:  15,
			},
		}, nil
	}

	wrapped := mw(fakeModel)
	req := &ai.ModelRequest{
		Messages: []*ai.Message{
			{Role: ai.RoleSystem, Content: []*ai.Part{ai.NewTextPart("You are helpful.")}},
			{Role: ai.RoleUser, Content: []*ai.Part{ai.NewTextPart("Hi")}},
		},
		Config: &ai.GenerationCommonConfig{
			MaxOutputTokens: 1024,
			Temperature:     0.7,
		},
	}

	resp, err := wrapped(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message == nil || len(resp.Message.Content) == 0 {
		t.Fatal("expected response message")
	}

	env.Shutdown(t)

	gen := env.Export.SingleGeneration(t)
	if got := stringValue(t, gen, "operation_name"); got != "generateText" {
		t.Fatalf("operation_name: got %q, want %q", got, "generateText")
	}

	genModel := objectValue(t, gen, "model")
	requireStringField(t, genModel, "provider", "anthropic")
	requireStringField(t, genModel, "name", "claude-3.5-sonnet")

	if got := stringValue(t, gen, "system_prompt"); got != "You are helpful." {
		t.Fatalf("system_prompt: got %q, want %q", got, "You are helpful.")
	}

	input := arrayValue(t, gen, "input")
	if len(input) != 1 {
		t.Fatalf("expected 1 input message (system extracted), got %d", len(input))
	}
	inputMsg := asObject(t, input[0], "input[0]")
	requireStringField(t, inputMsg, "role", "MESSAGE_ROLE_USER")

	output := arrayValue(t, gen, "output")
	if len(output) != 1 {
		t.Fatalf("expected 1 output message, got %d", len(output))
	}
	outputMsg := asObject(t, output[0], "output[0]")
	requireStringField(t, outputMsg, "role", "MESSAGE_ROLE_ASSISTANT")

	usage := objectValue(t, gen, "usage")
	requireStringField(t, usage, "input_tokens", "10")
	requireStringField(t, usage, "output_tokens", "5")
	requireStringField(t, usage, "total_tokens", "15")

	if got := stringValue(t, gen, "stop_reason"); got != "stop" {
		t.Fatalf("stop_reason: got %q, want %q", got, "stop")
	}

	tags := objectValue(t, gen, "tags")
	requireStringField(t, tags, "sigil.framework.name", "genkit")
	requireStringField(t, tags, "sigil.framework.source", "middleware")
	requireStringField(t, tags, "sigil.framework.language", "go")
}

func TestStreamingGeneration(t *testing.T) {
	env := newTestEnv(t, sigilgenkit.Options{
		AgentName: "stream-agent",
	})

	model := &fakeModelArg{name: "openai/gpt-5"}
	mw := env.Plugin.Middleware(model)

	fakeModel := func(_ context.Context, req *ai.ModelRequest, cb ai.ModelStreamCallback) (*ai.ModelResponse, error) {
		if cb != nil {
			_ = cb(context.Background(), &ai.ModelResponseChunk{
				Content: []*ai.Part{ai.NewTextPart("Hello")},
			})
			_ = cb(context.Background(), &ai.ModelResponseChunk{
				Content: []*ai.Part{ai.NewTextPart(" world")},
			})
		}
		return &ai.ModelResponse{
			Message: &ai.Message{
				Role:    ai.RoleModel,
				Content: []*ai.Part{ai.NewTextPart("Hello world")},
			},
			FinishReason: ai.FinishReasonStop,
			Usage: &ai.GenerationUsage{
				InputTokens:  5,
				OutputTokens: 2,
			},
		}, nil
	}

	streamCb := func(_ context.Context, chunk *ai.ModelResponseChunk) error {
		return nil
	}

	wrapped := mw(fakeModel)
	req := &ai.ModelRequest{
		Messages: []*ai.Message{
			{Role: ai.RoleUser, Content: []*ai.Part{ai.NewTextPart("Say hello")}},
		},
	}

	resp, err := wrapped(context.Background(), req, streamCb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}

	env.Shutdown(t)

	gen := env.Export.SingleGeneration(t)
	if got := stringValue(t, gen, "operation_name"); got != "streamText" {
		t.Fatalf("operation_name: got %q, want %q", got, "streamText")
	}

	genModel := objectValue(t, gen, "model")
	requireStringField(t, genModel, "provider", "openai")
	requireStringField(t, genModel, "name", "gpt-5")
}

func TestCallError(t *testing.T) {
	env := newTestEnv(t, sigilgenkit.Options{
		AgentName: "error-agent",
	})

	model := &fakeModelArg{name: "anthropic/claude-3.5-sonnet"}
	mw := env.Plugin.Middleware(model)

	fakeModel := func(_ context.Context, req *ai.ModelRequest, cb ai.ModelStreamCallback) (*ai.ModelResponse, error) {
		return nil, errors.New("provider error")
	}

	wrapped := mw(fakeModel)
	req := &ai.ModelRequest{
		Messages: []*ai.Message{
			{Role: ai.RoleUser, Content: []*ai.Part{ai.NewTextPart("Hi")}},
		},
	}

	_, err := wrapped(context.Background(), req, nil)
	if err == nil {
		t.Fatal("expected error")
	}

	env.Shutdown(t)

	gen := env.Export.SingleGeneration(t)
	if got := stringValue(t, gen, "call_error"); got != "provider error" {
		t.Fatalf("call_error: got %q, want %q", got, "provider error")
	}

	input := arrayValue(t, gen, "input")
	if len(input) != 1 {
		t.Fatalf("expected 1 input message, got %d", len(input))
	}
	inputMsg := asObject(t, input[0], "input[0]")
	requireStringField(t, inputMsg, "role", "MESSAGE_ROLE_USER")
}

func TestContentCaptureMetadataOnly(t *testing.T) {
	env := newTestEnv(t, sigilgenkit.Options{
		AgentName:      "nocapture-agent",
		ContentCapture: sigil.ContentCaptureModeMetadataOnly,
	})

	model := &fakeModelArg{name: "anthropic/claude-3.5-sonnet"}
	mw := env.Plugin.Middleware(model)

	fakeModel := func(_ context.Context, req *ai.ModelRequest, cb ai.ModelStreamCallback) (*ai.ModelResponse, error) {
		return &ai.ModelResponse{
			Message: &ai.Message{
				Role:    ai.RoleModel,
				Content: []*ai.Part{ai.NewTextPart("Secret response")},
			},
			FinishReason: ai.FinishReasonStop,
			Usage: &ai.GenerationUsage{
				InputTokens:  10,
				OutputTokens: 5,
			},
		}, nil
	}

	wrapped := mw(fakeModel)
	req := &ai.ModelRequest{
		Messages: []*ai.Message{
			{Role: ai.RoleSystem, Content: []*ai.Part{ai.NewTextPart("Secret system")}},
			{Role: ai.RoleUser, Content: []*ai.Part{ai.NewTextPart("Secret input")}},
		},
	}

	_, err := wrapped(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	env.Shutdown(t)

	gen := env.Export.SingleGeneration(t)

	// Content capture mode should be stamped in metadata
	metadata := objectValue(t, gen, "metadata")
	requireStringField(t, metadata, "sigil.sdk.content_capture_mode", "metadata_only")

	// System prompt should be stripped
	if got, ok := gen["system_prompt"]; ok {
		if s, ok := got.(string); ok && s != "" {
			t.Fatalf("expected empty system_prompt in metadata-only mode, got %q", s)
		}
	}

	// Messages should still exist (structure preserved) but content stripped
	input := arrayValue(t, gen, "input")
	if len(input) != 1 {
		t.Fatalf("expected 1 input message (structure preserved), got %d", len(input))
	}
	inputMsg := asObject(t, input[0], "input[0]")
	requireStringField(t, inputMsg, "role", "MESSAGE_ROLE_USER")

	output := arrayValue(t, gen, "output")
	if len(output) != 1 {
		t.Fatalf("expected 1 output message (structure preserved), got %d", len(output))
	}
	outputMsg := asObject(t, output[0], "output[0]")
	requireStringField(t, outputMsg, "role", "MESSAGE_ROLE_ASSISTANT")

	// Usage should still be present
	usage := objectValue(t, gen, "usage")
	requireStringField(t, usage, "input_tokens", "10")
	requireStringField(t, usage, "output_tokens", "5")
}

func TestFrameworkTags(t *testing.T) {
	env := newTestEnv(t, sigilgenkit.Options{
		AgentName: "tags-agent",
		ExtraTags: map[string]string{
			"deployment.environment": "test",
		},
		ExtraMetadata: map[string]any{
			"team": "infra",
		},
	})

	model := &fakeModelArg{name: "openai/gpt-5"}
	mw := env.Plugin.Middleware(model)

	fakeModel := func(_ context.Context, req *ai.ModelRequest, cb ai.ModelStreamCallback) (*ai.ModelResponse, error) {
		return &ai.ModelResponse{
			Message: &ai.Message{
				Role:    ai.RoleModel,
				Content: []*ai.Part{ai.NewTextPart("ok")},
			},
			FinishReason: ai.FinishReasonStop,
		}, nil
	}

	wrapped := mw(fakeModel)
	req := &ai.ModelRequest{
		Messages: []*ai.Message{
			{Role: ai.RoleUser, Content: []*ai.Part{ai.NewTextPart("test")}},
		},
	}

	_, err := wrapped(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	env.Shutdown(t)

	gen := env.Export.SingleGeneration(t)
	tags := objectValue(t, gen, "tags")
	requireStringField(t, tags, "sigil.framework.name", "genkit")
	requireStringField(t, tags, "sigil.framework.source", "middleware")
	requireStringField(t, tags, "sigil.framework.language", "go")
	requireStringField(t, tags, "deployment.environment", "test")

	metadata := objectValue(t, gen, "metadata")
	requireStringField(t, metadata, "team", "infra")
}

func TestToolMapping(t *testing.T) {
	env := newTestEnv(t, sigilgenkit.Options{
		AgentName: "tool-agent",
	})

	model := &fakeModelArg{name: "anthropic/claude-3.5-sonnet"}
	mw := env.Plugin.Middleware(model)

	fakeModel := func(_ context.Context, req *ai.ModelRequest, cb ai.ModelStreamCallback) (*ai.ModelResponse, error) {
		return &ai.ModelResponse{
			Message: &ai.Message{
				Role: ai.RoleModel,
				Content: []*ai.Part{
					ai.NewTextPart("Let me check the weather."),
					ai.NewToolRequestPart(&ai.ToolRequest{
						Name:  "weather",
						Ref:   "call-1",
						Input: map[string]any{"city": "Paris"},
					}),
				},
			},
			FinishReason: ai.FinishReasonStop,
			Usage: &ai.GenerationUsage{
				InputTokens:  15,
				OutputTokens: 8,
			},
		}, nil
	}

	wrapped := mw(fakeModel)
	req := &ai.ModelRequest{
		Messages: []*ai.Message{
			{Role: ai.RoleUser, Content: []*ai.Part{ai.NewTextPart("What's the weather?")}},
		},
		Tools: []*ai.ToolDefinition{
			{
				Name:        "weather",
				Description: "Get weather",
				InputSchema: map[string]any{"type": "object"},
			},
		},
		ToolChoice: ai.ToolChoiceAuto,
	}

	_, err := wrapped(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	env.Shutdown(t)

	gen := env.Export.SingleGeneration(t)

	tools := arrayValue(t, gen, "tools")
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	tool := asObject(t, tools[0], "tools[0]")
	requireStringField(t, tool, "name", "weather")

	output := arrayValue(t, gen, "output")
	if len(output) != 1 {
		t.Fatalf("expected 1 output message, got %d", len(output))
	}
	msg := asObject(t, output[0], "output[0]")
	parts := arrayValue(t, msg, "parts")
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts (text + tool_call), got %d", len(parts))
	}
}

func TestNilModelResponse(t *testing.T) {
	env := newTestEnv(t, sigilgenkit.Options{
		AgentName: "nil-resp-agent",
	})

	model := &fakeModelArg{name: "custom/model"}
	mw := env.Plugin.Middleware(model)

	fakeModel := func(_ context.Context, req *ai.ModelRequest, cb ai.ModelStreamCallback) (*ai.ModelResponse, error) {
		return nil, nil
	}

	wrapped := mw(fakeModel)
	req := &ai.ModelRequest{
		Messages: []*ai.Message{
			{Role: ai.RoleUser, Content: []*ai.Part{ai.NewTextPart("test")}},
		},
	}

	resp, err := wrapped(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response even when model returns nil")
	}

	env.Shutdown(t)

	gen := env.Export.SingleGeneration(t)
	if got := stringValue(t, gen, "operation_name"); got != "generateText" {
		t.Fatalf("operation_name: got %q, want %q", got, "generateText")
	}
}

func TestConversationIDAndAgentFields(t *testing.T) {
	env := newTestEnv(t, sigilgenkit.Options{
		AgentName:      "my-agent",
		AgentVersion:   "2.0.0",
		ConversationID: "conv-123",
	})

	model := &fakeModelArg{name: "openai/gpt-5"}
	mw := env.Plugin.Middleware(model)

	fakeModel := func(_ context.Context, req *ai.ModelRequest, cb ai.ModelStreamCallback) (*ai.ModelResponse, error) {
		return &ai.ModelResponse{
			Message: &ai.Message{
				Role:    ai.RoleModel,
				Content: []*ai.Part{ai.NewTextPart("ok")},
			},
			FinishReason: ai.FinishReasonStop,
		}, nil
	}

	wrapped := mw(fakeModel)
	req := &ai.ModelRequest{
		Messages: []*ai.Message{
			{Role: ai.RoleUser, Content: []*ai.Part{ai.NewTextPart("test")}},
		},
	}

	_, err := wrapped(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	env.Shutdown(t)

	gen := env.Export.SingleGeneration(t)
	requireStringField(t, gen, "conversation_id", "conv-123")
	requireStringField(t, gen, "agent_name", "my-agent")
	requireStringField(t, gen, "agent_version", "2.0.0")
}

func TestModelConfigExported(t *testing.T) {
	env := newTestEnv(t, sigilgenkit.Options{
		AgentName: "config-agent",
	})

	model := &fakeModelArg{name: "anthropic/claude-3.5-sonnet"}
	mw := env.Plugin.Middleware(model)

	fakeModel := func(_ context.Context, req *ai.ModelRequest, cb ai.ModelStreamCallback) (*ai.ModelResponse, error) {
		return &ai.ModelResponse{
			Message: &ai.Message{
				Role:    ai.RoleModel,
				Content: []*ai.Part{ai.NewTextPart("ok")},
			},
			FinishReason: ai.FinishReasonStop,
		}, nil
	}

	wrapped := mw(fakeModel)
	req := &ai.ModelRequest{
		Messages: []*ai.Message{
			{Role: ai.RoleUser, Content: []*ai.Part{ai.NewTextPart("test")}},
		},
		Config: &ai.GenerationCommonConfig{
			MaxOutputTokens: 2048,
			Temperature:     0.7,
			TopP:            0.9,
		},
	}

	_, err := wrapped(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	env.Shutdown(t)

	gen := env.Export.SingleGeneration(t)

	// int64 fields are encoded as strings in protobuf JSON
	requireStringField(t, gen, "max_tokens", "2048")

	// double fields are encoded as float64 in protobuf JSON
	if v, ok := gen["temperature"]; !ok {
		t.Fatal("missing temperature")
	} else if n, ok := v.(float64); !ok || n != 0.7 {
		t.Fatalf("temperature: got %v (%T), want 0.7", v, v)
	}
	if v, ok := gen["top_p"]; !ok {
		t.Fatal("missing top_p")
	} else if n, ok := v.(float64); !ok || n != 0.9 {
		t.Fatalf("top_p: got %v (%T), want 0.9", v, v)
	}
}

func TestStreamingCallbackForwarded(t *testing.T) {
	env := newTestEnv(t, sigilgenkit.Options{
		AgentName: "stream-cb-agent",
	})

	model := &fakeModelArg{name: "openai/gpt-5"}
	mw := env.Plugin.Middleware(model)

	fakeModel := func(_ context.Context, req *ai.ModelRequest, cb ai.ModelStreamCallback) (*ai.ModelResponse, error) {
		if cb != nil {
			_ = cb(context.Background(), &ai.ModelResponseChunk{
				Content: []*ai.Part{ai.NewTextPart("chunk1")},
			})
			_ = cb(context.Background(), &ai.ModelResponseChunk{
				Content: []*ai.Part{ai.NewTextPart("chunk2")},
			})
		}
		return &ai.ModelResponse{
			Message: &ai.Message{
				Role:    ai.RoleModel,
				Content: []*ai.Part{ai.NewTextPart("chunk1chunk2")},
			},
			FinishReason: ai.FinishReasonStop,
			Usage:        &ai.GenerationUsage{InputTokens: 5, OutputTokens: 2},
		}, nil
	}

	var chunks []string
	streamCb := func(_ context.Context, chunk *ai.ModelResponseChunk) error {
		for _, p := range chunk.Content {
			if p.Kind == ai.PartText {
				chunks = append(chunks, p.Text)
			}
		}
		return nil
	}

	wrapped := mw(fakeModel)
	req := &ai.ModelRequest{
		Messages: []*ai.Message{
			{Role: ai.RoleUser, Content: []*ai.Part{ai.NewTextPart("test")}},
		},
	}

	_, err := wrapped(context.Background(), req, streamCb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(chunks) != 2 || chunks[0] != "chunk1" || chunks[1] != "chunk2" {
		t.Fatalf("expected chunks [chunk1, chunk2], got %v", chunks)
	}

	env.Shutdown(t)

	gen := env.Export.SingleGeneration(t)
	if got := stringValue(t, gen, "operation_name"); got != "streamText" {
		t.Fatalf("operation_name: got %q, want %q", got, "streamText")
	}
}

func TestEmptyOutputRoleDefaultsToAssistant(t *testing.T) {
	env := newTestEnv(t, sigilgenkit.Options{
		AgentName: "role-agent",
	})

	model := &fakeModelArg{name: "custom/model"}
	mw := env.Plugin.Middleware(model)

	fakeModel := func(_ context.Context, req *ai.ModelRequest, cb ai.ModelStreamCallback) (*ai.ModelResponse, error) {
		return &ai.ModelResponse{
			Message: &ai.Message{
				Content: []*ai.Part{ai.NewTextPart("response without role")},
			},
			FinishReason: ai.FinishReasonStop,
			Usage:        &ai.GenerationUsage{InputTokens: 5, OutputTokens: 3},
		}, nil
	}

	wrapped := mw(fakeModel)
	req := &ai.ModelRequest{
		Messages: []*ai.Message{
			{Role: ai.RoleUser, Content: []*ai.Part{ai.NewTextPart("test")}},
		},
	}

	_, err := wrapped(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	env.Shutdown(t)

	gen := env.Export.SingleGeneration(t)
	output := arrayValue(t, gen, "output")
	if len(output) != 1 {
		t.Fatalf("expected 1 output message, got %d", len(output))
	}
	outputMsg := asObject(t, output[0], "output[0]")
	requireStringField(t, outputMsg, "role", "MESSAGE_ROLE_ASSISTANT")
}

// --- test infrastructure ---

type fakeModelArg struct{ name string }

func (f *fakeModelArg) Name() string { return f.name }

type testEnv struct {
	Client         *sigil.Client
	Plugin         *sigilgenkit.Plugin
	Export         *generationCaptureServer
	Spans          *tracetest.SpanRecorder
	tracerProvider *sdktrace.TracerProvider
}

func newTestEnv(t *testing.T, opts sigilgenkit.Options) *testEnv {
	t.Helper()

	export := newGenerationCaptureServer(t)
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))

	cfg := sigil.DefaultConfig()
	cfg.Tracer = tracerProvider.Tracer("genkit-test")
	cfg.GenerationExport.Protocol = sigil.GenerationExportProtocolHTTP
	cfg.GenerationExport.Endpoint = export.server.URL + "/api/v1/generations:export"
	cfg.GenerationExport.BatchSize = 1
	cfg.GenerationExport.QueueSize = 8
	cfg.GenerationExport.FlushInterval = time.Hour
	cfg.GenerationExport.MaxRetries = 1
	cfg.GenerationExport.InitialBackoff = time.Millisecond
	cfg.GenerationExport.MaxBackoff = 5 * time.Millisecond

	client := sigil.NewClient(cfg)
	plugin := sigilgenkit.New(client, opts)

	env := &testEnv{
		Client:         client,
		Plugin:         plugin,
		Export:         export,
		Spans:          spanRecorder,
		tracerProvider: tracerProvider,
	}
	t.Cleanup(func() {
		_ = env.close()
	})
	return env
}

func (e *testEnv) Shutdown(t *testing.T) {
	t.Helper()
	if e == nil || e.Client == nil {
		return
	}
	client := e.Client
	e.Client = nil
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown client: %v", err)
	}
}

func (e *testEnv) close() error {
	if e == nil {
		return nil
	}
	var closeErr error
	if e.Client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := e.Client.Shutdown(ctx); err != nil {
			closeErr = err
		}
		e.Client = nil
	}
	if e.tracerProvider != nil {
		if err := e.tracerProvider.Shutdown(context.Background()); err != nil && closeErr == nil {
			closeErr = err
		}
		e.tracerProvider = nil
	}
	if e.Export != nil && e.Export.server != nil {
		e.Export.server.Close()
		e.Export = nil
	}
	return closeErr
}

type generationCaptureServer struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests []map[string]any
}

func newGenerationCaptureServer(t *testing.T) *generationCaptureServer {
	t.Helper()
	capture := &generationCaptureServer{}
	capture.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		request := map[string]any{}
		if err := json.Unmarshal(body, &request); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		capture.mu.Lock()
		capture.requests = append(capture.requests, request)
		capture.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	return capture
}

func (c *generationCaptureServer) SingleGeneration(t *testing.T) map[string]any {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.requests) != 1 {
		t.Fatalf("expected exactly one export request, got %d", len(c.requests))
	}
	generations := arrayValue(t, c.requests[0], "generations")
	if len(generations) != 1 {
		t.Fatalf("expected exactly one exported generation, got %d", len(generations))
	}
	return asObject(t, generations[0], "generations[0]")
}

func stringValue(t *testing.T, object map[string]any, key string) string {
	t.Helper()
	value, ok := object[key]
	if !ok {
		t.Fatalf("missing %q", key)
	}
	text, ok := value.(string)
	if !ok {
		t.Fatalf("expected %q to be string, got %T", key, value)
	}
	return text
}

func objectValue(t *testing.T, object map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := object[key]
	if !ok {
		t.Fatalf("missing %q", key)
	}
	return asObject(t, value, key)
}

func arrayValue(t *testing.T, object map[string]any, key string) []any {
	t.Helper()
	value, ok := object[key]
	if !ok {
		t.Fatalf("missing %q", key)
	}
	items, ok := value.([]any)
	if !ok {
		t.Fatalf("expected %q to be array, got %T", key, value)
	}
	return items
}

func asObject(t *testing.T, value any, label string) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected %s to be object, got %T", label, value)
	}
	return object
}

func requireStringField(t *testing.T, object map[string]any, key string, want string) {
	t.Helper()
	if got := stringValue(t, object, key); got != want {
		t.Fatalf("unexpected %q: got %q want %q", key, got, want)
	}
}
