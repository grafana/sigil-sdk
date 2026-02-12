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
		ID:             "gen-trace-http",
		ConversationID: "conv-trace-http",
		AgentName:      "trace-agent-http",
		AgentVersion:   "trace-v-http",
		Model:          ModelRef{Provider: "openai", Name: "gpt-5"},
	})
	rec.SetResult(Generation{
		Input:  []Message{UserTextMessage("hello")},
		Output: []Message{AssistantTextMessage("hi")},
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
		ID:             "gen-trace-grpc",
		ConversationID: "conv-trace-grpc",
		AgentName:      "trace-agent-grpc",
		AgentVersion:   "trace-v-grpc",
		Model:          ModelRef{Provider: "anthropic", Name: "claude-sonnet-4-5"},
	})
	rec.SetResult(Generation{
		Input:  []Message{UserTextMessage("hello")},
		Output: []Message{AssistantTextMessage("hi")},
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

	attrs := attrStringMap(span.Attributes)
	if attrs[spanAttrGenerationID] != generation.ID {
		t.Fatalf("expected %s=%q, got %q", spanAttrGenerationID, generation.ID, attrs[spanAttrGenerationID])
	}
	if attrs[spanAttrConversationID] != generation.ConversationID {
		t.Fatalf("expected %s=%q, got %q", spanAttrConversationID, generation.ConversationID, attrs[spanAttrConversationID])
	}
	if attrs[spanAttrAgentName] != generation.AgentName {
		t.Fatalf("expected %s=%q, got %q", spanAttrAgentName, generation.AgentName, attrs[spanAttrAgentName])
	}
	if attrs[spanAttrAgentVersion] != generation.AgentVersion {
		t.Fatalf("expected %s=%q, got %q", spanAttrAgentVersion, generation.AgentVersion, attrs[spanAttrAgentVersion])
	}
	if attrs[spanAttrProviderName] != generation.Model.Provider {
		t.Fatalf("expected %s=%q, got %q", spanAttrProviderName, generation.Model.Provider, attrs[spanAttrProviderName])
	}
	if attrs[spanAttrRequestModel] != generation.Model.Name {
		t.Fatalf("expected %s=%q, got %q", spanAttrRequestModel, generation.Model.Name, attrs[spanAttrRequestModel])
	}
	if attrs[spanAttrOperationName] != operationName(generation) {
		t.Fatalf("expected %s=%q, got %q", spanAttrOperationName, operationName(generation), attrs[spanAttrOperationName])
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

func attrStringMap(attrs []*commonv1.KeyValue) map[string]string {
	out := make(map[string]string, len(attrs))
	for _, kv := range attrs {
		if kv == nil || kv.Value == nil {
			continue
		}

		switch value := kv.Value.Value.(type) {
		case *commonv1.AnyValue_StringValue:
			out[kv.Key] = value.StringValue
		}
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
