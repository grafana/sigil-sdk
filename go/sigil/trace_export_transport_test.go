package sigil

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	collecttracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

func TestSDKTraceExportOverHTTP(t *testing.T) {
	collector := newHTTPTraceCollector(t)

	client := NewClient(Config{
		Trace: TraceConfig{
			Protocol: TraceProtocolHTTP,
			Endpoint: collector.server.URL + "/v1/traces",
			Insecure: true,
		},
		testGenerationExporter: &capturingGenerationExporter{},
		testDisableWorker:      true,
	})

	_, rec := client.StartGeneration(context.Background(), GenerationStart{
		ID:              "gen-trace-http",
		ConversationID:  "conv-trace-http",
		AgentName:       "trace-agent-http",
		AgentVersion:    "trace-v-http",
		Model:           ModelRef{Provider: "openai", Name: "gpt-5"},
		MaxTokens:       int64Ptr(512),
		Temperature:     float64Ptr(0.2),
		TopP:            float64Ptr(0.95),
		ToolChoice:      stringPtr("required"),
		ThinkingEnabled: boolPtr(false),
	})
	rec.SetResult(Generation{
		Input:      []Message{UserTextMessage("hello")},
		Output:     []Message{AssistantTextMessage("hi")},
		StopReason: "end_turn",
		Metadata: map[string]any{
			"sigil.gen_ai.request.thinking.budget_tokens": int64(2048),
		},
	}, nil)
	rec.End()
	if err := rec.Err(); err != nil {
		t.Fatalf("recorder error: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown client: %v", err)
	}

	request := collector.waitSingleRequest(t)
	assertTraceRequestForGeneration(t, request, rec.lastGeneration)
}

func TestSDKTraceExportOverGRPC(t *testing.T) {
	collector := newGRPCTraceCollector(t)

	client := NewClient(Config{
		Trace: TraceConfig{
			Protocol: TraceProtocolGRPC,
			Endpoint: collector.listener.Addr().String(),
			Insecure: true,
		},
		testGenerationExporter: &capturingGenerationExporter{},
		testDisableWorker:      true,
	})

	_, rec := client.StartStreamingGeneration(context.Background(), GenerationStart{
		ID:              "gen-trace-grpc",
		ConversationID:  "conv-trace-grpc",
		AgentName:       "trace-agent-grpc",
		AgentVersion:    "trace-v-grpc",
		Model:           ModelRef{Provider: "anthropic", Name: "claude-sonnet-4-5"},
		MaxTokens:       int64Ptr(1024),
		Temperature:     float64Ptr(0.1),
		TopP:            float64Ptr(0.9),
		ToolChoice:      stringPtr("auto"),
		ThinkingEnabled: boolPtr(true),
	})
	rec.SetResult(Generation{
		Input:      []Message{UserTextMessage("hello")},
		Output:     []Message{AssistantTextMessage("hi")},
		StopReason: "stop",
		Metadata: map[string]any{
			"sigil.gen_ai.request.thinking.budget_tokens": int64(1024),
		},
	}, nil)
	rec.End()
	if err := rec.Err(); err != nil {
		t.Fatalf("recorder error: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown client: %v", err)
	}

	request := collector.waitSingleRequest(t)
	assertTraceRequestForGeneration(t, request, rec.lastGeneration)
}

func assertTraceRequestForGeneration(t *testing.T, request *collecttracev1.ExportTraceServiceRequest, generation Generation) {
	t.Helper()

	if request == nil {
		t.Fatalf("expected non-nil trace request")
	}

	span, ok := findSpanByName(request, generationSpanName(generation))
	if !ok {
		t.Fatalf("expected span %q in exported traces", generationSpanName(generation))
	}

	attrs := attrMap(span.Attributes)
	if attrs[spanAttrGenerationID].GetStringValue() != generation.ID {
		t.Fatalf("expected %s=%q, got %q", spanAttrGenerationID, generation.ID, attrs[spanAttrGenerationID].GetStringValue())
	}
	if attrs[spanAttrConversationID].GetStringValue() != generation.ConversationID {
		t.Fatalf("expected %s=%q, got %q", spanAttrConversationID, generation.ConversationID, attrs[spanAttrConversationID].GetStringValue())
	}
	if attrs[spanAttrAgentName].GetStringValue() != generation.AgentName {
		t.Fatalf("expected %s=%q, got %q", spanAttrAgentName, generation.AgentName, attrs[spanAttrAgentName].GetStringValue())
	}
	if attrs[spanAttrAgentVersion].GetStringValue() != generation.AgentVersion {
		t.Fatalf("expected %s=%q, got %q", spanAttrAgentVersion, generation.AgentVersion, attrs[spanAttrAgentVersion].GetStringValue())
	}
	if attrs[spanAttrProviderName].GetStringValue() != generation.Model.Provider {
		t.Fatalf("expected %s=%q, got %q", spanAttrProviderName, generation.Model.Provider, attrs[spanAttrProviderName].GetStringValue())
	}
	if attrs[spanAttrRequestModel].GetStringValue() != generation.Model.Name {
		t.Fatalf("expected %s=%q, got %q", spanAttrRequestModel, generation.Model.Name, attrs[spanAttrRequestModel].GetStringValue())
	}
	if attrs[spanAttrOperationName].GetStringValue() != operationName(generation) {
		t.Fatalf("expected %s=%q, got %q", spanAttrOperationName, operationName(generation), attrs[spanAttrOperationName].GetStringValue())
	}
	if generation.MaxTokens != nil && attrs[spanAttrRequestMaxTokens].GetIntValue() != *generation.MaxTokens {
		t.Fatalf("expected %s=%d, got %d", spanAttrRequestMaxTokens, *generation.MaxTokens, attrs[spanAttrRequestMaxTokens].GetIntValue())
	}
	if generation.Temperature != nil && attrs[spanAttrRequestTemperature].GetDoubleValue() != *generation.Temperature {
		t.Fatalf("expected %s=%v, got %v", spanAttrRequestTemperature, *generation.Temperature, attrs[spanAttrRequestTemperature].GetDoubleValue())
	}
	if generation.TopP != nil && attrs[spanAttrRequestTopP].GetDoubleValue() != *generation.TopP {
		t.Fatalf("expected %s=%v, got %v", spanAttrRequestTopP, *generation.TopP, attrs[spanAttrRequestTopP].GetDoubleValue())
	}
	if generation.ToolChoice != nil && attrs[spanAttrRequestToolChoice].GetStringValue() != *generation.ToolChoice {
		t.Fatalf("expected %s=%q, got %q", spanAttrRequestToolChoice, *generation.ToolChoice, attrs[spanAttrRequestToolChoice].GetStringValue())
	}
	if generation.ThinkingEnabled != nil && attrs[spanAttrRequestThinkingEnabled].GetBoolValue() != *generation.ThinkingEnabled {
		t.Fatalf("expected %s=%v, got %v", spanAttrRequestThinkingEnabled, *generation.ThinkingEnabled, attrs[spanAttrRequestThinkingEnabled].GetBoolValue())
	}
	if budget, ok := generation.Metadata[spanAttrRequestThinkingBudget].(int64); ok {
		if attrs[spanAttrRequestThinkingBudget].GetIntValue() != budget {
			t.Fatalf("expected %s=%d, got %d", spanAttrRequestThinkingBudget, budget, attrs[spanAttrRequestThinkingBudget].GetIntValue())
		}
	}
	if generation.StopReason != "" {
		reasons := attrs[spanAttrFinishReasons].GetArrayValue().GetValues()
		if len(reasons) != 1 || reasons[0].GetStringValue() != generation.StopReason {
			t.Fatalf("expected %s=[%q], got %v", spanAttrFinishReasons, generation.StopReason, reasons)
		}
	}

	traceID := hex.EncodeToString(span.TraceId)
	if generation.TraceID != "" && generation.TraceID != traceID {
		t.Fatalf("expected exported trace id %q, got %q", generation.TraceID, traceID)
	}
	spanID := hex.EncodeToString(span.SpanId)
	if generation.SpanID != "" && generation.SpanID != spanID {
		t.Fatalf("expected exported span id %q, got %q", generation.SpanID, spanID)
	}
}

func findSpanByName(request *collecttracev1.ExportTraceServiceRequest, name string) (*tracev1.Span, bool) {
	for _, resourceSpans := range request.GetResourceSpans() {
		for _, scopeSpans := range resourceSpans.GetScopeSpans() {
			for _, span := range scopeSpans.GetSpans() {
				if span.GetName() == name {
					return span, true
				}
			}
		}
	}
	return nil, false
}

func attrMap(attrs []*commonv1.KeyValue) map[string]*commonv1.AnyValue {
	out := make(map[string]*commonv1.AnyValue, len(attrs))
	for _, kv := range attrs {
		if kv == nil || kv.Value == nil {
			continue
		}
		out[kv.Key] = kv.Value
	}
	return out
}

type traceSink struct {
	mu       sync.Mutex
	requests []*collecttracev1.ExportTraceServiceRequest
}

func (s *traceSink) add(request *collecttracev1.ExportTraceServiceRequest) {
	if request == nil {
		return
	}

	clone := proto.Clone(request)
	typed, ok := clone.(*collecttracev1.ExportTraceServiceRequest)
	if !ok {
		return
	}

	s.mu.Lock()
	s.requests = append(s.requests, typed)
	s.mu.Unlock()
}

func (s *traceSink) waitSingleRequest(t *testing.T) *collecttracev1.ExportTraceServiceRequest {
	t.Helper()

	if err := waitForCondition(2*time.Second, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.requests) >= 1
	}); err != nil {
		t.Fatalf("did not receive trace export request: %v", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.requests) != 1 {
		t.Fatalf("expected exactly one trace export request, got %d", len(s.requests))
	}
	return s.requests[0]
}

type httpTraceCollector struct {
	sink   *traceSink
	server *httptest.Server
}

func newHTTPTraceCollector(t *testing.T) *httpTraceCollector {
	t.Helper()

	sink := &traceSink{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}

		request := &collecttracev1.ExportTraceServiceRequest{}
		if err := proto.Unmarshal(body, request); err != nil {
			http.Error(w, fmt.Sprintf("unmarshal body: %v", err), http.StatusBadRequest)
			return
		}
		sink.add(request)

		response := &collecttracev1.ExportTraceServiceResponse{}
		payload, err := proto.Marshal(response)
		if err != nil {
			http.Error(w, "marshal response", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	t.Cleanup(server.Close)

	return &httpTraceCollector{sink: sink, server: server}
}

func (c *httpTraceCollector) waitSingleRequest(t *testing.T) *collecttracev1.ExportTraceServiceRequest {
	t.Helper()
	return c.sink.waitSingleRequest(t)
}

type grpcTraceCollector struct {
	sink     *traceSink
	server   *grpc.Server
	listener net.Listener
}

func newGRPCTraceCollector(t *testing.T) *grpcTraceCollector {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen grpc: %v", err)
	}

	sink := &traceSink{}
	server := grpc.NewServer()
	collecttracev1.RegisterTraceServiceServer(server, &traceServiceServer{sink: sink})

	go func() {
		_ = server.Serve(listener)
	}()

	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	return &grpcTraceCollector{sink: sink, server: server, listener: listener}
}

func (c *grpcTraceCollector) waitSingleRequest(t *testing.T) *collecttracev1.ExportTraceServiceRequest {
	t.Helper()
	return c.sink.waitSingleRequest(t)
}

type traceServiceServer struct {
	collecttracev1.UnimplementedTraceServiceServer
	sink *traceSink
}

func (s *traceServiceServer) Export(_ context.Context, request *collecttracev1.ExportTraceServiceRequest) (*collecttracev1.ExportTraceServiceResponse, error) {
	s.sink.add(request)
	return &collecttracev1.ExportTraceServiceResponse{}, nil
}
