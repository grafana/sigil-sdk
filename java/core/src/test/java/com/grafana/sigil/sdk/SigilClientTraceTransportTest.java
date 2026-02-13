package com.grafana.sigil.sdk;

import static org.assertj.core.api.Assertions.assertThat;

import io.grpc.Metadata;
import io.grpc.Server;
import io.grpc.ServerBuilder;
import io.grpc.ServerCall;
import io.grpc.ServerCallHandler;
import io.grpc.ServerInterceptor;
import io.grpc.ServerInterceptors;
import io.grpc.stub.StreamObserver;
import io.opentelemetry.proto.collector.trace.v1.ExportTraceServiceRequest;
import io.opentelemetry.proto.collector.trace.v1.ExportTraceServiceResponse;
import io.opentelemetry.proto.collector.trace.v1.TraceServiceGrpc;
import io.opentelemetry.proto.common.v1.AnyValue;
import io.opentelemetry.proto.common.v1.KeyValue;
import io.opentelemetry.proto.trace.v1.ResourceSpans;
import io.opentelemetry.proto.trace.v1.ScopeSpans;
import io.opentelemetry.proto.trace.v1.Span;
import java.io.OutputStream;
import java.net.InetSocketAddress;
import java.time.Duration;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.concurrent.atomic.AtomicReference;
import org.junit.jupiter.api.Test;

class SigilClientTraceTransportTest {
    @Test
    void exportsTraceOverOtlpHttpAndAppliesTenantAuthHeader() throws Exception {
        AtomicReference<Map<String, String>> headers = new AtomicReference<>(new LinkedHashMap<>());
        AtomicReference<ExportTraceServiceRequest> request = new AtomicReference<>();
        AtomicReference<Generation> lastGeneration = new AtomicReference<>();

        com.sun.net.httpserver.HttpServer server = com.sun.net.httpserver.HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        server.createContext("/v1/traces", exchange -> {
            Map<String, String> requestHeaders = new LinkedHashMap<>();
            exchange.getRequestHeaders().forEach((k, v) -> requestHeaders.put(k.toLowerCase(), String.join(",", v)));
            headers.set(requestHeaders);
            request.set(ExportTraceServiceRequest.parseFrom(exchange.getRequestBody().readAllBytes()));

            byte[] response = ExportTraceServiceResponse.newBuilder().build().toByteArray();
            exchange.getResponseHeaders().add("Content-Type", "application/x-protobuf");
            exchange.sendResponseHeaders(200, response.length);
            try (OutputStream outputStream = exchange.getResponseBody()) {
                outputStream.write(response);
            }
        });
        server.start();

        SigilClientConfig config = new SigilClientConfig()
                .setGenerationExporter(new TestFixtures.CapturingExporter())
                .setGenerationExport(new GenerationExportConfig()
                        .setBatchSize(1)
                        .setQueueSize(100)
                        .setFlushInterval(Duration.ofMinutes(10))
                        .setMaxRetries(0))
                .setTrace(new TraceConfig()
                        .setProtocol(TraceProtocol.OTLP_HTTP)
                        .setEndpoint("http://127.0.0.1:" + server.getAddress().getPort() + "/v1/traces")
                        .setAuth(new AuthConfig().setMode(AuthMode.TENANT).setTenantId("tenant-trace-http")));

        try (SigilClient client = new SigilClient(config)) {
            GenerationRecorder recorder = client.startGeneration(TestFixtures.startFixture());
            recorder.setResult(TestFixtures.resultFixture());
            recorder.end();
            lastGeneration.set(recorder.lastGeneration().orElseThrow());
            client.shutdown();
        } finally {
            server.stop(0);
        }

        assertThat(headers.get().get("x-scope-orgid")).isEqualTo("tenant-trace-http");
        assertTraceRequestMatchesGeneration(request.get(), lastGeneration.get());
    }

    @Test
    void exportsTraceOverOtlpGrpcAndHonorsAuthorizationHeaderOverride() throws Exception {
        AtomicReference<ExportTraceServiceRequest> request = new AtomicReference<>();
        AtomicReference<Map<String, String>> metadata = new AtomicReference<>(new LinkedHashMap<>());
        AtomicReference<Generation> lastGeneration = new AtomicReference<>();

        TraceServiceGrpc.TraceServiceImplBase service = new TraceServiceGrpc.TraceServiceImplBase() {
            @Override
            public void export(
                    ExportTraceServiceRequest value,
                    StreamObserver<ExportTraceServiceResponse> responseObserver) {
                request.set(value);
                responseObserver.onNext(ExportTraceServiceResponse.newBuilder().build());
                responseObserver.onCompleted();
            }
        };

        ServerInterceptor interceptor = new ServerInterceptor() {
            @Override
            public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
                    ServerCall<ReqT, RespT> call,
                    Metadata headers,
                    ServerCallHandler<ReqT, RespT> next) {
                Map<String, String> seen = new LinkedHashMap<>();
                for (String key : headers.keys()) {
                    if (key.endsWith(Metadata.BINARY_HEADER_SUFFIX)) {
                        continue;
                    }
                    Metadata.Key<String> metaKey = Metadata.Key.of(key, Metadata.ASCII_STRING_MARSHALLER);
                    seen.put(key, headers.get(metaKey));
                }
                metadata.set(seen);
                return next.startCall(call, headers);
            }
        };

        Server server = ServerBuilder.forPort(0)
                .addService(ServerInterceptors.intercept(service, interceptor))
                .build()
                .start();

        SigilClientConfig config = new SigilClientConfig()
                .setGenerationExporter(new TestFixtures.CapturingExporter())
                .setGenerationExport(new GenerationExportConfig()
                        .setBatchSize(1)
                        .setQueueSize(100)
                        .setFlushInterval(Duration.ofMinutes(10))
                        .setMaxRetries(0))
                .setTrace(new TraceConfig()
                        .setProtocol(TraceProtocol.OTLP_GRPC)
                        .setEndpoint("http://127.0.0.1:" + server.getPort())
                        .setHeaders(Map.of("authorization", "Bearer override-trace-token"))
                        .setAuth(new AuthConfig().setMode(AuthMode.BEARER).setBearerToken("ignored")));

        try (SigilClient client = new SigilClient(config)) {
            GenerationRecorder recorder = client.startGeneration(TestFixtures.startFixture());
            recorder.setResult(TestFixtures.resultFixture());
            recorder.end();
            lastGeneration.set(recorder.lastGeneration().orElseThrow());
            client.shutdown();
        } finally {
            server.shutdownNow();
        }

        assertThat(metadata.get().get("authorization")).isEqualTo("Bearer override-trace-token");
        assertTraceRequestMatchesGeneration(request.get(), lastGeneration.get());
    }

    private static void assertTraceRequestMatchesGeneration(ExportTraceServiceRequest request, Generation generation) {
        assertThat(request).isNotNull();
        Span span = findSpanByName(request, SigilClient.generationSpanName(generation.getOperationName(), generation.getModel().getName()));
        assertThat(span).isNotNull();

        Map<String, Object> attrs = valueAttributes(span);
        assertThat(attrs.get(SigilClient.SPAN_ATTR_GENERATION_ID)).isEqualTo(generation.getId());
        assertThat(attrs.get(SigilClient.SPAN_ATTR_CONVERSATION_ID)).isEqualTo(generation.getConversationId());
        assertThat(attrs.get(SigilClient.SPAN_ATTR_AGENT_NAME)).isEqualTo(generation.getAgentName());
        assertThat(attrs.get(SigilClient.SPAN_ATTR_AGENT_VERSION)).isEqualTo(generation.getAgentVersion());
        assertThat(attrs.get(SigilClient.SPAN_ATTR_PROVIDER_NAME)).isEqualTo(generation.getModel().getProvider());
        assertThat(attrs.get(SigilClient.SPAN_ATTR_REQUEST_MODEL)).isEqualTo(generation.getModel().getName());
        assertThat(attrs.get(SigilClient.SPAN_ATTR_OPERATION_NAME)).isEqualTo(generation.getOperationName());
        assertThat(attrs.get(SigilClient.SPAN_ATTR_REQUEST_MAX_TOKENS)).isEqualTo(generation.getMaxTokens());
        assertThat(attrs.get(SigilClient.SPAN_ATTR_REQUEST_TEMPERATURE)).isEqualTo(generation.getTemperature());
        assertThat(attrs.get(SigilClient.SPAN_ATTR_REQUEST_TOP_P)).isEqualTo(generation.getTopP());
        assertThat(attrs.get(SigilClient.SPAN_ATTR_REQUEST_TOOL_CHOICE)).isEqualTo(generation.getToolChoice());
        assertThat(attrs.get(SigilClient.SPAN_ATTR_REQUEST_THINKING_ENABLED)).isEqualTo(generation.getThinkingEnabled());
        assertThat(attrs.get(SigilClient.SPAN_ATTR_REQUEST_THINKING_BUDGET))
                .isEqualTo(((Number) generation.getMetadata().get("sigil.gen_ai.request.thinking.budget_tokens")).longValue());
        assertThat(attrs.get(SigilClient.SPAN_ATTR_FINISH_REASONS)).isEqualTo(java.util.List.of(generation.getStopReason()));
    }

    private static Span findSpanByName(ExportTraceServiceRequest request, String name) {
        for (ResourceSpans resourceSpans : request.getResourceSpansList()) {
            for (ScopeSpans scopeSpans : resourceSpans.getScopeSpansList()) {
                for (Span span : scopeSpans.getSpansList()) {
                    if (name.equals(span.getName())) {
                        return span;
                    }
                }
            }
        }
        return null;
    }

    private static Map<String, Object> valueAttributes(Span span) {
        Map<String, Object> out = new LinkedHashMap<>();
        for (KeyValue kv : span.getAttributesList()) {
            out.put(kv.getKey(), decodeAnyValue(kv.getValue()));
        }
        return out;
    }

    private static Object decodeAnyValue(AnyValue value) {
        if (value.hasStringValue()) {
            return value.getStringValue();
        }
        if (value.hasIntValue()) {
            return value.getIntValue();
        }
        if (value.hasDoubleValue()) {
            return value.getDoubleValue();
        }
        if (value.hasBoolValue()) {
            return value.getBoolValue();
        }
        if (value.hasArrayValue()) {
            java.util.List<Object> out = new java.util.ArrayList<>();
            for (AnyValue item : value.getArrayValue().getValuesList()) {
                out.add(decodeAnyValue(item));
            }
            return out;
        }
        return null;
    }
}
