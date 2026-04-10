package com.grafana.sigil.sdk;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.opentelemetry.api.common.AttributeKey;
import io.opentelemetry.api.trace.StatusCode;
import io.opentelemetry.context.Scope;
import io.opentelemetry.sdk.testing.exporter.InMemorySpanExporter;
import io.opentelemetry.sdk.trace.SdkTracerProvider;
import io.opentelemetry.sdk.trace.data.SpanData;
import io.opentelemetry.sdk.trace.export.SimpleSpanProcessor;
import java.time.Duration;
import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.Test;

class ContentCaptureModeTest {

    // --- Enum tests ---

    @Test
    void toMetadataValue() {
        assertThat(ContentCaptureMode.FULL.toMetadataValue()).isEqualTo("full");
        assertThat(ContentCaptureMode.NO_TOOL_CONTENT.toMetadataValue()).isEqualTo("no_tool_content");
        assertThat(ContentCaptureMode.METADATA_ONLY.toMetadataValue()).isEqualTo("metadata_only");
    }

    @Test
    void toMetadataValue_defaultThrows() {
        assertThatThrownBy(ContentCaptureMode.DEFAULT::toMetadataValue)
                .isInstanceOf(IllegalStateException.class);
    }

    // --- Resolution tests ---

    @Test
    void resolveContentCaptureMode_overrideWins() {
        assertThat(SigilClient.resolveContentCaptureMode(ContentCaptureMode.METADATA_ONLY, ContentCaptureMode.FULL))
                .isEqualTo(ContentCaptureMode.METADATA_ONLY);
    }

    @Test
    void resolveContentCaptureMode_defaultFallsThrough() {
        assertThat(SigilClient.resolveContentCaptureMode(ContentCaptureMode.DEFAULT, ContentCaptureMode.FULL))
                .isEqualTo(ContentCaptureMode.FULL);
    }

    @Test
    void resolveClientContentCaptureMode_defaultBecomesNoToolContent() {
        assertThat(SigilClient.resolveClientContentCaptureMode(ContentCaptureMode.DEFAULT))
                .isEqualTo(ContentCaptureMode.NO_TOOL_CONTENT);
    }

    @Test
    void resolveClientContentCaptureMode_nonDefaultPassesThrough() {
        assertThat(SigilClient.resolveClientContentCaptureMode(ContentCaptureMode.FULL))
                .isEqualTo(ContentCaptureMode.FULL);
        assertThat(SigilClient.resolveClientContentCaptureMode(ContentCaptureMode.METADATA_ONLY))
                .isEqualTo(ContentCaptureMode.METADATA_ONLY);
    }

    // --- Resolver tests ---

    @Test
    void callContentCaptureResolver_nullResolverReturnsDefault() {
        assertThat(SigilClient.callContentCaptureResolver(null, null, null))
                .isEqualTo(ContentCaptureMode.DEFAULT);
    }

    @Test
    void callContentCaptureResolver_returnsResolverResult() {
        ContentCaptureResolver resolver = meta -> ContentCaptureMode.METADATA_ONLY;
        assertThat(SigilClient.callContentCaptureResolver(resolver, null, null))
                .isEqualTo(ContentCaptureMode.METADATA_ONLY);
    }

    @Test
    void callContentCaptureResolver_readsMetadata() {
        ContentCaptureResolver resolver = meta -> {
            if (meta != null && "opted-out".equals(meta.get("tenant_id"))) {
                return ContentCaptureMode.METADATA_ONLY;
            }
            return ContentCaptureMode.FULL;
        };
        assertThat(SigilClient.callContentCaptureResolver(resolver, Map.of("tenant_id", "opted-out"), null))
                .isEqualTo(ContentCaptureMode.METADATA_ONLY);
        assertThat(SigilClient.callContentCaptureResolver(resolver, Map.of("tenant_id", "normal"), null))
                .isEqualTo(ContentCaptureMode.FULL);
    }

    @Test
    void callContentCaptureResolver_exceptionFailsClosed() {
        ContentCaptureResolver resolver = meta -> {
            throw new RuntimeException("resolver bug");
        };
        assertThat(SigilClient.callContentCaptureResolver(resolver, null, null))
                .isEqualTo(ContentCaptureMode.METADATA_ONLY);
    }

    @Test
    void callContentCaptureResolver_nullReturnBecomesDefault() {
        ContentCaptureResolver resolver = meta -> null;
        assertThat(SigilClient.callContentCaptureResolver(resolver, null, null))
                .isEqualTo(ContentCaptureMode.DEFAULT);
    }

    // --- stripContent tests ---

    @Test
    void stripContent_stripsSensitiveFields() {
        Generation gen = makeGeneration();
        SigilClient.stripContent(gen, "rate_limit");

        assertThat(gen.getSystemPrompt()).isEmpty();
        assertThat(gen.getArtifacts()).isEmpty();
        assertThat(gen.getCallError()).isEqualTo("rate_limit");
        assertThat(gen.getMetadata()).doesNotContainKey("call_error");
        assertThat(gen.getConversationTitle()).isEmpty();
        assertThat(gen.getMetadata()).doesNotContainKey("sigil.conversation.title");

        // Input text stripped
        assertThat(gen.getInput().get(0).getParts().get(0).getText()).isEmpty();
        // Tool result content stripped
        assertThat(gen.getInput().get(1).getParts().get(0).getToolResult().getContent()).isEmpty();
        assertThat(gen.getInput().get(1).getParts().get(0).getToolResult().getContentJson()).isEmpty();

        // Output thinking stripped
        assertThat(gen.getOutput().get(0).getParts().get(0).getThinking()).isEmpty();
        // Tool call inputJson stripped
        assertThat(gen.getOutput().get(0).getParts().get(1).getToolCall().getInputJson()).isEmpty();
        // Output text stripped
        assertThat(gen.getOutput().get(0).getParts().get(2).getText()).isEmpty();

        // Tool definitions stripped
        assertThat(gen.getTools().get(0).getDescription()).isEmpty();
        assertThat(gen.getTools().get(0).getInputSchemaJson()).isEmpty();
    }

    @Test
    void stripContent_preservesStructure() {
        Generation gen = makeGeneration();
        SigilClient.stripContent(gen, "rate_limit");

        assertThat(gen.getInput()).hasSize(2);
        assertThat(gen.getOutput()).hasSize(1);
        assertThat(gen.getOutput().get(0).getParts()).hasSize(3);

        assertThat(gen.getInput().get(0).getRole()).isEqualTo(MessageRole.USER);
        assertThat(gen.getOutput().get(0).getParts().get(0).getKind()).isEqualTo(MessagePartKind.THINKING);
        assertThat(gen.getOutput().get(0).getParts().get(1).getToolCall().getName()).isEqualTo("weather");
        assertThat(gen.getOutput().get(0).getParts().get(1).getToolCall().getId()).isEqualTo("call_1");
        assertThat(gen.getInput().get(1).getParts().get(0).getToolResult().getToolCallId()).isEqualTo("call_1");
        assertThat(gen.getInput().get(1).getParts().get(0).getToolResult().getName()).isEqualTo("weather");
    }

    @Test
    void stripContent_preservesOperationalMetadata() {
        Generation gen = makeGeneration();
        SigilClient.stripContent(gen, "rate_limit");

        assertThat(gen.getTools().get(0).getName()).isEqualTo("weather");
        assertThat(gen.getUsage().getInputTokens()).isEqualTo(120);
        assertThat(gen.getUsage().getOutputTokens()).isEqualTo(42);
        assertThat(gen.getStopReason()).isEqualTo("end_turn");
        assertThat(gen.getModel().getName()).isEqualTo("claude-sonnet-4-5");
        assertThat(gen.getMetadata().get("sigil.sdk.name")).isEqualTo("sdk-java");
    }

    @Test
    void stripContent_noCategory_fallsBackToSdkError() {
        Generation gen = makeGeneration();
        SigilClient.stripContent(gen, "");

        assertThat(gen.getCallError()).isEqualTo("sdk_error");
    }

    @Test
    void stripContent_noCallError_remainsEmpty() {
        Generation gen = makeGeneration();
        gen.setCallError("");
        gen.getMetadata().remove("call_error");
        SigilClient.stripContent(gen, "rate_limit");

        assertThat(gen.getCallError()).isEmpty();
    }

    // --- Integration: generation content capture mode stamping and stripping ---

    @Test
    void generation_defaultConfig_stampsNoToolContent() {
        try (SigilClient client = newTestClient(ContentCaptureMode.DEFAULT, null)) {
            GenerationRecorder recorder = client.startGeneration(new GenerationStart()
                    .setModel(new ModelRef().setProvider("anthropic").setName("claude-sonnet-4-5")));
            recorder.setResult(minimalResult());
            recorder.end();

            Generation gen = recorder.lastGeneration().orElseThrow();
            assertThat(gen.getMetadata().get(SigilClient.METADATA_KEY_CONTENT_CAPTURE_MODE))
                    .isEqualTo("no_tool_content");
            assertThat(gen.getInput().get(0).getParts().get(0).getText()).isEqualTo("Hello");
        }
    }

    @Test
    void generation_clientMetadataOnly_stripsContent() {
        try (SigilClient client = newTestClient(ContentCaptureMode.METADATA_ONLY, null)) {
            GenerationRecorder recorder = client.startGeneration(new GenerationStart()
                    .setModel(new ModelRef().setProvider("anthropic").setName("claude-sonnet-4-5")));
            recorder.setResult(minimalResult());
            recorder.end();

            Generation gen = recorder.lastGeneration().orElseThrow();
            assertThat(gen.getMetadata().get(SigilClient.METADATA_KEY_CONTENT_CAPTURE_MODE))
                    .isEqualTo("metadata_only");
            assertThat(gen.getInput().get(0).getParts().get(0).getText()).isEmpty();
            assertThat(gen.getInput().get(0).getRole()).isEqualTo(MessageRole.USER);
            assertThat(gen.getUsage().getInputTokens()).isEqualTo(10);
        }
    }

    @Test
    void generation_perGenOverride_overridesClient() {
        try (SigilClient client = newTestClient(ContentCaptureMode.METADATA_ONLY, null)) {
            GenerationRecorder recorder = client.startGeneration(new GenerationStart()
                    .setModel(new ModelRef().setProvider("anthropic").setName("claude-sonnet-4-5"))
                    .setContentCapture(ContentCaptureMode.FULL));
            recorder.setResult(minimalResult());
            recorder.end();

            Generation gen = recorder.lastGeneration().orElseThrow();
            assertThat(gen.getMetadata().get(SigilClient.METADATA_KEY_CONTENT_CAPTURE_MODE))
                    .isEqualTo("full");
            assertThat(gen.getInput().get(0).getParts().get(0).getText()).isEqualTo("Hello");
        }
    }

    @Test
    void generation_clientFull_genMetadataOnly_stripsContent() {
        try (SigilClient client = newTestClient(ContentCaptureMode.FULL, null)) {
            GenerationRecorder recorder = client.startGeneration(new GenerationStart()
                    .setModel(new ModelRef().setProvider("anthropic").setName("claude-sonnet-4-5"))
                    .setContentCapture(ContentCaptureMode.METADATA_ONLY));
            recorder.setResult(minimalResult());
            recorder.end();

            Generation gen = recorder.lastGeneration().orElseThrow();
            assertThat(gen.getMetadata().get(SigilClient.METADATA_KEY_CONTENT_CAPTURE_MODE))
                    .isEqualTo("metadata_only");
            assertThat(gen.getInput().get(0).getParts().get(0).getText()).isEmpty();
        }
    }

    @Test
    void generation_clientFull_genDefault_preservesContent() {
        try (SigilClient client = newTestClient(ContentCaptureMode.FULL, null)) {
            GenerationRecorder recorder = client.startGeneration(new GenerationStart()
                    .setModel(new ModelRef().setProvider("anthropic").setName("claude-sonnet-4-5")));
            recorder.setResult(minimalResult());
            recorder.end();

            Generation gen = recorder.lastGeneration().orElseThrow();
            assertThat(gen.getMetadata().get(SigilClient.METADATA_KEY_CONTENT_CAPTURE_MODE))
                    .isEqualTo("full");
            assertThat(gen.getInput().get(0).getParts().get(0).getText()).isEqualTo("Hello");
        }
    }

    // --- Integration: resolver ---

    @Test
    void resolver_metadataOnlyOverridesClientFull() {
        ContentCaptureResolver resolver = meta -> ContentCaptureMode.METADATA_ONLY;
        try (SigilClient client = newTestClient(ContentCaptureMode.FULL, resolver)) {
            GenerationRecorder recorder = client.startGeneration(new GenerationStart()
                    .setModel(new ModelRef().setProvider("test").setName("test-model")));
            recorder.setResult(minimalResult());
            recorder.end();

            Generation gen = recorder.lastGeneration().orElseThrow();
            assertThat(gen.getMetadata().get(SigilClient.METADATA_KEY_CONTENT_CAPTURE_MODE))
                    .isEqualTo("metadata_only");
            assertThat(gen.getInput().get(0).getParts().get(0).getText()).isEmpty();
        }
    }

    @Test
    void resolver_perGenFullOverridesResolverMetadataOnly() {
        ContentCaptureResolver resolver = meta -> ContentCaptureMode.METADATA_ONLY;
        try (SigilClient client = newTestClient(ContentCaptureMode.DEFAULT, resolver)) {
            GenerationRecorder recorder = client.startGeneration(new GenerationStart()
                    .setModel(new ModelRef().setProvider("test").setName("test-model"))
                    .setContentCapture(ContentCaptureMode.FULL));
            recorder.setResult(minimalResult());
            recorder.end();

            Generation gen = recorder.lastGeneration().orElseThrow();
            assertThat(gen.getMetadata().get(SigilClient.METADATA_KEY_CONTENT_CAPTURE_MODE))
                    .isEqualTo("full");
            assertThat(gen.getInput().get(0).getParts().get(0).getText()).isEqualTo("Hello");
        }
    }

    @Test
    void resolver_defaultDefersToClient() {
        ContentCaptureResolver resolver = meta -> ContentCaptureMode.DEFAULT;
        try (SigilClient client = newTestClient(ContentCaptureMode.METADATA_ONLY, resolver)) {
            GenerationRecorder recorder = client.startGeneration(new GenerationStart()
                    .setModel(new ModelRef().setProvider("test").setName("test-model")));
            recorder.setResult(minimalResult());
            recorder.end();

            Generation gen = recorder.lastGeneration().orElseThrow();
            assertThat(gen.getMetadata().get(SigilClient.METADATA_KEY_CONTENT_CAPTURE_MODE))
                    .isEqualTo("metadata_only");
        }
    }

    @Test
    void resolver_panicFailsClosedToMetadataOnly() {
        ContentCaptureResolver resolver = meta -> {
            throw new RuntimeException("oops");
        };
        try (SigilClient client = newTestClient(ContentCaptureMode.FULL, resolver)) {
            GenerationRecorder recorder = client.startGeneration(new GenerationStart()
                    .setModel(new ModelRef().setProvider("test").setName("test-model")));
            recorder.setResult(minimalResult());
            recorder.end();

            Generation gen = recorder.lastGeneration().orElseThrow();
            assertThat(gen.getMetadata().get(SigilClient.METADATA_KEY_CONTENT_CAPTURE_MODE))
                    .isEqualTo("metadata_only");
            assertThat(gen.getInput().get(0).getParts().get(0).getText()).isEmpty();
        }
    }

    // --- Integration: tool content capture inheritance ---

    @Test
    void tool_parentMetadataOnly_inherits_suppressed() {
        InMemorySpanExporter spanExporter = InMemorySpanExporter.create();
        try (SigilClient client = newSpanTestClient(ContentCaptureMode.METADATA_ONLY, null, spanExporter)) {
            GenerationRecorder genRec = client.startGeneration(new GenerationStart()
                    .setModel(new ModelRef().setProvider("anthropic").setName("claude-sonnet-4-5")));

            @SuppressWarnings("deprecation")
            ToolExecutionRecorder toolRec = client.startToolExecution(new ToolExecutionStart()
                    .setToolName("test_tool")
                    .setIncludeContent(true));
            toolRec.setResult(new ToolExecutionResult().setArguments("args").setResult("result"));
            toolRec.end();

            genRec.setResult(minimalResult());
            genRec.end();
        }

        SpanData toolSpan = findToolSpan(spanExporter.getFinishedSpanItems());
        assertThat(toolSpan).isNotNull();
        assertThat(toolSpan.getAttributes().get(AttributeKey.stringKey(SigilClient.SPAN_ATTR_TOOL_CALL_ARGUMENTS))).isNull();
        assertThat(toolSpan.getAttributes().get(AttributeKey.stringKey(SigilClient.SPAN_ATTR_TOOL_NAME))).isEqualTo("test_tool");
    }

    @Test
    void tool_parentMetadataOnly_explicitFull_included() {
        InMemorySpanExporter spanExporter = InMemorySpanExporter.create();
        try (SigilClient client = newSpanTestClient(ContentCaptureMode.METADATA_ONLY, null, spanExporter)) {
            GenerationRecorder genRec = client.startGeneration(new GenerationStart()
                    .setModel(new ModelRef().setProvider("anthropic").setName("claude-sonnet-4-5")));

            ToolExecutionRecorder toolRec = client.startToolExecution(new ToolExecutionStart()
                    .setToolName("test_tool")
                    .setContentCapture(ContentCaptureMode.FULL));
            toolRec.setResult(new ToolExecutionResult().setArguments("args").setResult("result"));
            toolRec.end();

            genRec.setResult(minimalResult());
            genRec.end();
        }

        SpanData toolSpan = findToolSpan(spanExporter.getFinishedSpanItems());
        assertThat(toolSpan.getAttributes().get(AttributeKey.stringKey(SigilClient.SPAN_ATTR_TOOL_CALL_ARGUMENTS)))
                .isEqualTo("\"args\"");
    }

    @Test
    void tool_parentFull_overridesClientMetadataOnly_included() {
        InMemorySpanExporter spanExporter = InMemorySpanExporter.create();
        try (SigilClient client = newSpanTestClient(ContentCaptureMode.METADATA_ONLY, null, spanExporter)) {
            GenerationRecorder genRec = client.startGeneration(new GenerationStart()
                    .setModel(new ModelRef().setProvider("anthropic").setName("claude-sonnet-4-5"))
                    .setContentCapture(ContentCaptureMode.FULL));

            @SuppressWarnings("deprecation")
            ToolExecutionRecorder toolRec = client.startToolExecution(new ToolExecutionStart()
                    .setToolName("test_tool")
                    .setIncludeContent(true));
            toolRec.setResult(new ToolExecutionResult().setArguments("args").setResult("result"));
            toolRec.end();

            genRec.setResult(minimalResult());
            genRec.end();
        }

        SpanData toolSpan = findToolSpan(spanExporter.getFinishedSpanItems());
        assertThat(toolSpan.getAttributes().get(AttributeKey.stringKey(SigilClient.SPAN_ATTR_TOOL_CALL_ARGUMENTS)))
                .isEqualTo("\"args\"");
    }

    @Test
    void tool_noParentGen_clientMetadataOnly_suppressed() {
        InMemorySpanExporter spanExporter = InMemorySpanExporter.create();
        try (SigilClient client = newSpanTestClient(ContentCaptureMode.METADATA_ONLY, null, spanExporter)) {
            @SuppressWarnings("deprecation")
            ToolExecutionRecorder toolRec = client.startToolExecution(new ToolExecutionStart()
                    .setToolName("test_tool")
                    .setIncludeContent(true));
            toolRec.setResult(new ToolExecutionResult().setArguments("args").setResult("result"));
            toolRec.end();
        }

        SpanData toolSpan = findToolSpan(spanExporter.getFinishedSpanItems());
        assertThat(toolSpan.getAttributes().get(AttributeKey.stringKey(SigilClient.SPAN_ATTR_TOOL_CALL_ARGUMENTS))).isNull();
    }

    @Test
    void tool_noParentGen_clientFull_included() {
        InMemorySpanExporter spanExporter = InMemorySpanExporter.create();
        try (SigilClient client = newSpanTestClient(ContentCaptureMode.FULL, null, spanExporter)) {
            ToolExecutionRecorder toolRec = client.startToolExecution(new ToolExecutionStart()
                    .setToolName("test_tool"));
            toolRec.setResult(new ToolExecutionResult().setArguments("args").setResult("result"));
            toolRec.end();
        }

        SpanData toolSpan = findToolSpan(spanExporter.getFinishedSpanItems());
        assertThat(toolSpan.getAttributes().get(AttributeKey.stringKey(SigilClient.SPAN_ATTR_TOOL_CALL_ARGUMENTS)))
                .isEqualTo("\"args\"");
    }

    @Test
    void tool_backwardCompat_clientDefault_legacyFalse_suppressed() {
        InMemorySpanExporter spanExporter = InMemorySpanExporter.create();
        try (SigilClient client = newSpanTestClient(ContentCaptureMode.DEFAULT, null, spanExporter)) {
            @SuppressWarnings("deprecation")
            ToolExecutionRecorder toolRec = client.startToolExecution(new ToolExecutionStart()
                    .setToolName("test_tool")
                    .setIncludeContent(false));
            toolRec.setResult(new ToolExecutionResult().setArguments("args").setResult("result"));
            toolRec.end();
        }

        SpanData toolSpan = findToolSpan(spanExporter.getFinishedSpanItems());
        assertThat(toolSpan.getAttributes().get(AttributeKey.stringKey(SigilClient.SPAN_ATTR_TOOL_CALL_ARGUMENTS))).isNull();
    }

    @Test
    void tool_backwardCompat_clientDefault_legacyTrue_included() {
        InMemorySpanExporter spanExporter = InMemorySpanExporter.create();
        try (SigilClient client = newSpanTestClient(ContentCaptureMode.DEFAULT, null, spanExporter)) {
            @SuppressWarnings("deprecation")
            ToolExecutionRecorder toolRec = client.startToolExecution(new ToolExecutionStart()
                    .setToolName("test_tool")
                    .setIncludeContent(true));
            toolRec.setResult(new ToolExecutionResult().setArguments("args").setResult("result"));
            toolRec.end();
        }

        SpanData toolSpan = findToolSpan(spanExporter.getFinishedSpanItems());
        assertThat(toolSpan.getAttributes().get(AttributeKey.stringKey(SigilClient.SPAN_ATTR_TOOL_CALL_ARGUMENTS)))
                .isEqualTo("\"args\"");
    }

    @Test
    void tool_parentFull_explicitMetadataOnly_suppressed() {
        InMemorySpanExporter spanExporter = InMemorySpanExporter.create();
        try (SigilClient client = newSpanTestClient(ContentCaptureMode.FULL, null, spanExporter)) {
            GenerationRecorder genRec = client.startGeneration(new GenerationStart()
                    .setModel(new ModelRef().setProvider("anthropic").setName("claude-sonnet-4-5")));

            @SuppressWarnings("deprecation")
            ToolExecutionRecorder toolRec = client.startToolExecution(new ToolExecutionStart()
                    .setToolName("test_tool")
                    .setContentCapture(ContentCaptureMode.METADATA_ONLY)
                    .setIncludeContent(true));
            toolRec.setResult(new ToolExecutionResult().setArguments("args").setResult("result"));
            toolRec.end();

            genRec.setResult(minimalResult());
            genRec.end();
        }

        SpanData toolSpan = findToolSpan(spanExporter.getFinishedSpanItems());
        assertThat(toolSpan.getAttributes().get(AttributeKey.stringKey(SigilClient.SPAN_ATTR_TOOL_CALL_ARGUMENTS))).isNull();
    }

    // --- Validation accepts stripped payloads ---

    @Test
    void validation_acceptsStrippedGeneration() {
        try (SigilClient client = newTestClient(ContentCaptureMode.METADATA_ONLY, null)) {
            GenerationRecorder recorder = client.startGeneration(new GenerationStart()
                    .setModel(new ModelRef().setProvider("anthropic").setName("claude-sonnet-4-5")));

            GenerationResult result = new GenerationResult()
                    .setUsage(new TokenUsage().setInputTokens(10).setOutputTokens(5));
            result.getInput().add(new Message()
                    .setRole(MessageRole.USER)
                    .setParts(List.of(MessagePart.text("Hello"))));
            result.getOutput().add(new Message()
                    .setRole(MessageRole.ASSISTANT)
                    .setParts(List.of(
                            MessagePart.thinking("thinking..."),
                            MessagePart.text("World"))));
            recorder.setResult(result);
            recorder.end();

            assertThat(recorder.error()).isEmpty();
            Generation gen = recorder.lastGeneration().orElseThrow();
            assertThat(gen.getInput().get(0).getParts().get(0).getText()).isEmpty();
            assertThat(gen.getOutput().get(0).getParts().get(0).getThinking()).isEmpty();
            assertThat(gen.getOutput().get(0).getParts().get(1).getText()).isEmpty();
        }
    }

    // --- Rating comment stripping ---

    @Test
    void rating_metadataOnly_stripsComment() throws Exception {
        java.util.concurrent.atomic.AtomicReference<com.fasterxml.jackson.databind.JsonNode> payload =
                new java.util.concurrent.atomic.AtomicReference<>();

        com.sun.net.httpserver.HttpServer server = com.sun.net.httpserver.HttpServer.create(
                new java.net.InetSocketAddress("127.0.0.1", 0), 0);
        server.createContext("/api/v1/conversations/conv-1/ratings", exchange -> {
            byte[] body = exchange.getRequestBody().readAllBytes();
            payload.set(Json.MAPPER.readTree(body));

            byte[] response = """
                    {
                      "rating":{"rating_id":"rat-1","conversation_id":"conv-1","rating":"CONVERSATION_RATING_VALUE_BAD","created_at":"2026-02-13T12:00:00Z"},
                      "summary":{"total_count":1,"good_count":0,"bad_count":1,"latest_rating":"CONVERSATION_RATING_VALUE_BAD","latest_rated_at":"2026-02-13T12:00:00Z","has_bad_rating":true}
                    }
                    """.getBytes();
            exchange.getResponseHeaders().add("Content-Type", "application/json");
            exchange.sendResponseHeaders(200, response.length);
            try (java.io.OutputStream os = exchange.getResponseBody()) {
                os.write(response);
            }
        });
        server.start();

        SigilClientConfig config = new SigilClientConfig()
                .setGenerationExporter(new TestFixtures.CapturingExporter())
                .setContentCapture(ContentCaptureMode.METADATA_ONLY)
                .setApi(new ApiConfig().setEndpoint("http://127.0.0.1:" + server.getAddress().getPort()))
                .setGenerationExport(new GenerationExportConfig()
                        .setProtocol(GenerationExportProtocol.HTTP)
                        .setEndpoint("http://127.0.0.1:" + server.getAddress().getPort() + "/api/v1/generations:export")
                        .setBatchSize(1)
                        .setFlushInterval(java.time.Duration.ofMinutes(10))
                        .setMaxRetries(0));

        try (SigilClient client = new SigilClient(config)) {
            client.submitConversationRating("conv-1",
                    new SubmitConversationRatingRequest()
                            .setRatingId("rat-1")
                            .setRating(ConversationRatingValue.BAD)
                            .setComment("this answer was terrible"));
        } finally {
            server.stop(0);
        }

        assertThat(payload.get().has("comment")).isFalse();
    }

    @Test
    void rating_full_preservesComment() throws Exception {
        java.util.concurrent.atomic.AtomicReference<com.fasterxml.jackson.databind.JsonNode> payload =
                new java.util.concurrent.atomic.AtomicReference<>();

        com.sun.net.httpserver.HttpServer server = com.sun.net.httpserver.HttpServer.create(
                new java.net.InetSocketAddress("127.0.0.1", 0), 0);
        server.createContext("/api/v1/conversations/conv-1/ratings", exchange -> {
            byte[] body = exchange.getRequestBody().readAllBytes();
            payload.set(Json.MAPPER.readTree(body));

            byte[] response = """
                    {
                      "rating":{"rating_id":"rat-1","conversation_id":"conv-1","rating":"CONVERSATION_RATING_VALUE_BAD","created_at":"2026-02-13T12:00:00Z"},
                      "summary":{"total_count":1,"good_count":0,"bad_count":1,"latest_rating":"CONVERSATION_RATING_VALUE_BAD","latest_rated_at":"2026-02-13T12:00:00Z","has_bad_rating":true}
                    }
                    """.getBytes();
            exchange.getResponseHeaders().add("Content-Type", "application/json");
            exchange.sendResponseHeaders(200, response.length);
            try (java.io.OutputStream os = exchange.getResponseBody()) {
                os.write(response);
            }
        });
        server.start();

        SigilClientConfig config = new SigilClientConfig()
                .setGenerationExporter(new TestFixtures.CapturingExporter())
                .setContentCapture(ContentCaptureMode.FULL)
                .setApi(new ApiConfig().setEndpoint("http://127.0.0.1:" + server.getAddress().getPort()))
                .setGenerationExport(new GenerationExportConfig()
                        .setProtocol(GenerationExportProtocol.HTTP)
                        .setEndpoint("http://127.0.0.1:" + server.getAddress().getPort() + "/api/v1/generations:export")
                        .setBatchSize(1)
                        .setFlushInterval(java.time.Duration.ofMinutes(10))
                        .setMaxRetries(0));

        try (SigilClient client = new SigilClient(config)) {
            client.submitConversationRating("conv-1",
                    new SubmitConversationRatingRequest()
                            .setRatingId("rat-1")
                            .setRating(ConversationRatingValue.BAD)
                            .setComment("this answer was terrible"));
        } finally {
            server.stop(0);
        }

        assertThat(payload.get().path("comment").asText()).isEqualTo("this answer was terrible");
    }

    // --- Context propagation ---

    @Test
    void contextPropagation_setAndGet() {
        try (Scope scope = SigilContext.withContentCaptureMode(ContentCaptureMode.METADATA_ONLY)) {
            assertThat(SigilContext.contentCaptureModeFromContext())
                    .isEqualTo(ContentCaptureMode.METADATA_ONLY);
        }
        assertThat(SigilContext.contentCaptureModeFromContext()).isNull();
    }

    // --- Conversation title stripping from spans ---

    @Test
    void generation_metadataOnly_stripsConversationTitleFromSpan() {
        InMemorySpanExporter spanExporter = InMemorySpanExporter.create();
        try (SigilClient client = newSpanTestClient(ContentCaptureMode.METADATA_ONLY, null, spanExporter)) {
            GenerationRecorder recorder = client.startGeneration(new GenerationStart()
                    .setModel(new ModelRef().setProvider("anthropic").setName("claude-sonnet-4-5"))
                    .setConversationTitle("My secret conversation"));
            recorder.setResult(minimalResult());
            recorder.end();
        }

        SpanData genSpan = findGenerationSpan(spanExporter.getFinishedSpanItems());
        assertThat(genSpan).isNotNull();
        assertThat(genSpan.getAttributes().get(AttributeKey.stringKey(SigilClient.SPAN_ATTR_CONVERSATION_TITLE))).isNull();
    }

    @Test
    void generation_full_preservesConversationTitleOnSpan() {
        InMemorySpanExporter spanExporter = InMemorySpanExporter.create();
        try (SigilClient client = newSpanTestClient(ContentCaptureMode.FULL, null, spanExporter)) {
            GenerationRecorder recorder = client.startGeneration(new GenerationStart()
                    .setModel(new ModelRef().setProvider("anthropic").setName("claude-sonnet-4-5"))
                    .setConversationTitle("My conversation"));
            recorder.setResult(minimalResult());
            recorder.end();
        }

        SpanData genSpan = findGenerationSpan(spanExporter.getFinishedSpanItems());
        assertThat(genSpan).isNotNull();
        assertThat(genSpan.getAttributes().get(AttributeKey.stringKey(SigilClient.SPAN_ATTR_CONVERSATION_TITLE)))
                .isEqualTo("My conversation");
    }

    @Test
    void tool_metadataOnly_stripsConversationTitleFromSpan() {
        InMemorySpanExporter spanExporter = InMemorySpanExporter.create();
        try (SigilClient client = newSpanTestClient(ContentCaptureMode.METADATA_ONLY, null, spanExporter)) {
            GenerationRecorder genRec = client.startGeneration(new GenerationStart()
                    .setModel(new ModelRef().setProvider("anthropic").setName("claude-sonnet-4-5")));

            ToolExecutionRecorder toolRec = client.startToolExecution(new ToolExecutionStart()
                    .setToolName("test_tool")
                    .setConversationTitle("My secret conversation"));
            toolRec.end();

            genRec.setResult(minimalResult());
            genRec.end();
        }

        SpanData toolSpan = findToolSpan(spanExporter.getFinishedSpanItems());
        assertThat(toolSpan).isNotNull();
        assertThat(toolSpan.getAttributes().get(AttributeKey.stringKey(SigilClient.SPAN_ATTR_CONVERSATION_TITLE))).isNull();
    }

    @Test
    void tool_full_preservesConversationTitleOnSpan() {
        InMemorySpanExporter spanExporter = InMemorySpanExporter.create();
        try (SigilClient client = newSpanTestClient(ContentCaptureMode.FULL, null, spanExporter)) {
            ToolExecutionRecorder toolRec = client.startToolExecution(new ToolExecutionStart()
                    .setToolName("test_tool")
                    .setConversationTitle("My conversation"));
            toolRec.end();
        }

        SpanData toolSpan = findToolSpan(spanExporter.getFinishedSpanItems());
        assertThat(toolSpan).isNotNull();
        assertThat(toolSpan.getAttributes().get(AttributeKey.stringKey(SigilClient.SPAN_ATTR_CONVERSATION_TITLE)))
                .isEqualTo("My conversation");
    }

    @Test
    void tool_metadataOnly_stripsToolDescriptionFromSpan() {
        InMemorySpanExporter spanExporter = InMemorySpanExporter.create();
        try (SigilClient client = newSpanTestClient(ContentCaptureMode.METADATA_ONLY, null, spanExporter)) {
            ToolExecutionRecorder toolRec = client.startToolExecution(new ToolExecutionStart()
                    .setToolName("test_tool")
                    .setToolDescription("Internal usage hint with example payloads"));
            toolRec.end();
        }

        SpanData toolSpan = findToolSpan(spanExporter.getFinishedSpanItems());
        assertThat(toolSpan).isNotNull();
        assertThat(toolSpan.getAttributes().get(AttributeKey.stringKey(SigilClient.SPAN_ATTR_TOOL_DESCRIPTION))).isNull();
    }

    @Test
    void tool_full_preservesToolDescriptionOnSpan() {
        InMemorySpanExporter spanExporter = InMemorySpanExporter.create();
        try (SigilClient client = newSpanTestClient(ContentCaptureMode.FULL, null, spanExporter)) {
            ToolExecutionRecorder toolRec = client.startToolExecution(new ToolExecutionStart()
                    .setToolName("test_tool")
                    .setToolDescription("Public tool description"));
            toolRec.end();
        }

        SpanData toolSpan = findToolSpan(spanExporter.getFinishedSpanItems());
        assertThat(toolSpan).isNotNull();
        assertThat(toolSpan.getAttributes().get(AttributeKey.stringKey(SigilClient.SPAN_ATTR_TOOL_DESCRIPTION)))
                .isEqualTo("Public tool description");
    }

    // --- Span error sanitization ---

    @Test
    void generation_metadataOnly_sanitizesSpanErrors() {
        InMemorySpanExporter spanExporter = InMemorySpanExporter.create();
        try (SigilClient client = newSpanTestClient(ContentCaptureMode.METADATA_ONLY, null, spanExporter)) {
            GenerationRecorder recorder = client.startGeneration(new GenerationStart()
                    .setModel(new ModelRef().setProvider("anthropic").setName("claude-sonnet-4-5")));
            recorder.setCallError(new RuntimeException("sensitive prompt text leaked in error"));
            recorder.setResult(minimalResult());
            recorder.end();
        }

        SpanData genSpan = findGenerationSpan(spanExporter.getFinishedSpanItems());
        assertThat(genSpan).isNotNull();
        assertThat(genSpan.getStatus().getStatusCode()).isEqualTo(StatusCode.ERROR);
        assertThat(genSpan.getStatus().getDescription()).doesNotContain("sensitive prompt text");
        assertThat(genSpan.getEvents()).isEmpty();
    }

    @Test
    void generation_full_preservesRawSpanErrors() {
        InMemorySpanExporter spanExporter = InMemorySpanExporter.create();
        try (SigilClient client = newSpanTestClient(ContentCaptureMode.FULL, null, spanExporter)) {
            GenerationRecorder recorder = client.startGeneration(new GenerationStart()
                    .setModel(new ModelRef().setProvider("anthropic").setName("claude-sonnet-4-5")));
            recorder.setCallError(new RuntimeException("detailed error message"));
            recorder.setResult(minimalResult());
            recorder.end();
        }

        SpanData genSpan = findGenerationSpan(spanExporter.getFinishedSpanItems());
        assertThat(genSpan).isNotNull();
        assertThat(genSpan.getStatus().getStatusCode()).isEqualTo(StatusCode.ERROR);
        assertThat(genSpan.getStatus().getDescription()).contains("detailed error message");
        assertThat(genSpan.getEvents()).isNotEmpty();
    }

    @Test
    void tool_metadataOnly_sanitizesSpanErrors() {
        InMemorySpanExporter spanExporter = InMemorySpanExporter.create();
        try (SigilClient client = newSpanTestClient(ContentCaptureMode.METADATA_ONLY, null, spanExporter)) {
            GenerationRecorder genRec = client.startGeneration(new GenerationStart()
                    .setModel(new ModelRef().setProvider("anthropic").setName("claude-sonnet-4-5")));

            ToolExecutionRecorder toolRec = client.startToolExecution(new ToolExecutionStart()
                    .setToolName("test_tool"));
            toolRec.setCallError(new RuntimeException("sensitive tool error with user data"));
            toolRec.end();

            genRec.setResult(minimalResult());
            genRec.end();
        }

        SpanData toolSpan = findToolSpan(spanExporter.getFinishedSpanItems());
        assertThat(toolSpan).isNotNull();
        assertThat(toolSpan.getStatus().getStatusCode()).isEqualTo(StatusCode.ERROR);
        assertThat(toolSpan.getStatus().getDescription()).doesNotContain("sensitive tool error");
        assertThat(toolSpan.getEvents()).isEmpty();
    }

    @Test
    void tool_full_preservesRawSpanErrors() {
        InMemorySpanExporter spanExporter = InMemorySpanExporter.create();
        try (SigilClient client = newSpanTestClient(ContentCaptureMode.FULL, null, spanExporter)) {
            ToolExecutionRecorder toolRec = client.startToolExecution(new ToolExecutionStart()
                    .setToolName("test_tool"));
            toolRec.setCallError(new RuntimeException("detailed tool error"));
            toolRec.end();
        }

        SpanData toolSpan = findToolSpan(spanExporter.getFinishedSpanItems());
        assertThat(toolSpan).isNotNull();
        assertThat(toolSpan.getStatus().getStatusCode()).isEqualTo(StatusCode.ERROR);
        assertThat(toolSpan.getStatus().getDescription()).contains("detailed tool error");
        assertThat(toolSpan.getEvents()).isNotEmpty();
    }

    // --- Helpers ---

    private static SigilClient newTestClient(ContentCaptureMode mode,
                                             ContentCaptureResolver resolver) {
        TestFixtures.CapturingExporter exporter = new TestFixtures.CapturingExporter();
        SigilClientConfig config = new SigilClientConfig()
                .setGenerationExporter(exporter)
                .setContentCapture(mode)
                .setContentCaptureResolver(resolver)
                .setGenerationExport(new GenerationExportConfig()
                        .setBatchSize(1)
                        .setFlushInterval(Duration.ofMinutes(10))
                        .setMaxRetries(0));
        return new SigilClient(config);
    }

    private static SigilClient newSpanTestClient(ContentCaptureMode mode,
                                                 ContentCaptureResolver resolver,
                                                 InMemorySpanExporter spanExporter) {
        SdkTracerProvider provider = SdkTracerProvider.builder()
                .addSpanProcessor(SimpleSpanProcessor.create(spanExporter))
                .build();
        TestFixtures.CapturingExporter exporter = new TestFixtures.CapturingExporter();
        SigilClientConfig config = new SigilClientConfig()
                .setTracer(provider.get("test"))
                .setGenerationExporter(exporter)
                .setContentCapture(mode)
                .setContentCaptureResolver(resolver)
                .setGenerationExport(new GenerationExportConfig()
                        .setBatchSize(1)
                        .setFlushInterval(Duration.ofMinutes(10))
                        .setMaxRetries(0));
        return new SigilClient(config);
    }

    private static GenerationResult minimalResult() {
        GenerationResult result = new GenerationResult()
                .setUsage(new TokenUsage().setInputTokens(10).setOutputTokens(5));
        result.getInput().add(new Message()
                .setRole(MessageRole.USER)
                .setParts(List.of(MessagePart.text("Hello"))));
        result.getOutput().add(new Message()
                .setRole(MessageRole.ASSISTANT)
                .setParts(List.of(MessagePart.text("Hi there"))));
        return result;
    }

    private static Generation makeGeneration() {
        Generation gen = new Generation()
                .setMode(GenerationMode.SYNC)
                .setModel(new ModelRef().setProvider("anthropic").setName("claude-sonnet-4-5"))
                .setSystemPrompt("You are helpful.")
                .setConversationTitle("My secret conversation");
        gen.setUsage(new TokenUsage().setInputTokens(120).setOutputTokens(42));
        gen.setStopReason("end_turn");
        gen.setCallError("rate limit exceeded: prompt too long for model");
        gen.getMetadata().put("sigil.sdk.name", "sdk-java");
        gen.getMetadata().put("call_error", "rate limit exceeded: prompt too long for model");
        gen.getMetadata().put("sigil.conversation.title", "My secret conversation");

        gen.getInput().add(new Message()
                .setRole(MessageRole.USER)
                .setParts(List.of(MessagePart.text("What is the weather?"))));
        gen.getInput().add(new Message()
                .setRole(MessageRole.TOOL)
                .setParts(List.of(MessagePart.toolResult(new ToolResultPart()
                        .setToolCallId("call_1")
                        .setName("weather")
                        .setContent("sunny 18C")
                        .setContentJson("{\"temp\":18}".getBytes())))));

        gen.getOutput().add(new Message()
                .setRole(MessageRole.ASSISTANT)
                .setParts(List.of(
                        MessagePart.thinking("let me think about weather"),
                        MessagePart.toolCall(new ToolCall()
                                .setId("call_1")
                                .setName("weather")
                                .setInputJson("{\"city\":\"Paris\"}".getBytes())),
                        MessagePart.text("It's 18C and sunny in Paris."))));

        gen.getTools().add(new ToolDefinition()
                .setName("weather")
                .setDescription("Get weather info")
                .setType("function")
                .setInputSchemaJson("{\"type\":\"object\"}".getBytes()));

        gen.getArtifacts().add(new Artifact()
                .setKind(ArtifactKind.REQUEST)
                .setPayload("raw".getBytes()));

        return gen;
    }

    private static SpanData findGenerationSpan(List<SpanData> spans) {
        for (SpanData span : spans) {
            if (!span.getName().startsWith("execute_tool")) {
                return span;
            }
        }
        return null;
    }

    private static SpanData findToolSpan(List<SpanData> spans) {
        for (SpanData span : spans) {
            if (span.getName().startsWith("execute_tool")) {
                return span;
            }
        }
        return null;
    }
}
