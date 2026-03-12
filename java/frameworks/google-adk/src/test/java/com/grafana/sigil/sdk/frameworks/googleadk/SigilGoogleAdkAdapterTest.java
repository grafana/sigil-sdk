package com.grafana.sigil.sdk.frameworks.googleadk;

import static org.assertj.core.api.Assertions.assertThat;

import com.grafana.sigil.sdk.ExportGenerationResult;
import com.grafana.sigil.sdk.ExportGenerationsRequest;
import com.grafana.sigil.sdk.ExportGenerationsResponse;
import com.grafana.sigil.sdk.Generation;
import com.grafana.sigil.sdk.GenerationExporter;
import com.grafana.sigil.sdk.GenerationMode;
import com.grafana.sigil.sdk.GenerationRecorder;
import com.grafana.sigil.sdk.GenerationExportConfig;
import com.grafana.sigil.sdk.GenerationExportProtocol;
import com.grafana.sigil.sdk.GenerationStart;
import com.grafana.sigil.sdk.Message;
import com.grafana.sigil.sdk.MessagePart;
import com.grafana.sigil.sdk.MessageRole;
import com.grafana.sigil.sdk.ModelRef;
import com.grafana.sigil.sdk.SigilClient;
import com.grafana.sigil.sdk.SigilClientConfig;
import com.grafana.sigil.sdk.TokenUsage;
import com.grafana.sigil.sdk.ToolExecutionStart;
import io.opentelemetry.api.common.AttributeKey;
import io.opentelemetry.context.Scope;
import io.opentelemetry.sdk.metrics.SdkMeterProvider;
import io.opentelemetry.sdk.metrics.data.MetricData;
import io.opentelemetry.sdk.testing.exporter.InMemoryMetricReader;
import io.opentelemetry.sdk.testing.exporter.InMemorySpanExporter;
import io.opentelemetry.sdk.trace.SdkTracerProvider;
import io.opentelemetry.sdk.trace.data.SpanData;
import io.opentelemetry.sdk.trace.export.SimpleSpanProcessor;
import java.lang.reflect.Method;
import java.time.Duration;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.Test;

class SigilGoogleAdkAdapterTest {
    private static SigilClient newClient() {
        return new SigilClient(
                new SigilClientConfig()
                        .setGenerationExport(
                                new GenerationExportConfig()
                                        .setProtocol(GenerationExportProtocol.NONE)));
    }

    @Test
    void resolveConversationUsesFrameworkIdsFirst() {
        SigilGoogleAdkAdapter.ConversationContext conversation = SigilGoogleAdkAdapter.resolveConversation(
                new SigilGoogleAdkAdapter.RunStartEvent()
                        .setRunId("run-1")
                        .setConversationId("conversation-1")
                        .setSessionId("session-1")
                        .setGroupId("group-1")
                        .setThreadId("thread-1"));

        assertThat(conversation.conversationId).isEqualTo("conversation-1");
        assertThat(conversation.threadId).isEqualTo("thread-1");

        conversation = SigilGoogleAdkAdapter.resolveConversation(
                new SigilGoogleAdkAdapter.RunStartEvent().setRunId("run-2").setSessionId("session-2"));
        assertThat(conversation.conversationId).isEqualTo("session-2");

        conversation = SigilGoogleAdkAdapter.resolveConversation(
                new SigilGoogleAdkAdapter.RunStartEvent().setRunId("run-3"));
        assertThat(conversation.conversationId).isEqualTo("sigil:framework:google-adk:run-3");
    }

    @Test
    void normalizeProviderCoversKnownAndFallbackValues() {
        assertThat(SigilGoogleAdkAdapter.normalizeProvider("openai")).isEqualTo("openai");
        assertThat(SigilGoogleAdkAdapter.normalizeProvider("anthropic")).isEqualTo("anthropic");
        assertThat(SigilGoogleAdkAdapter.normalizeProvider("gemini")).isEqualTo("gemini");
        assertThat(SigilGoogleAdkAdapter.normalizeProvider("custom-provider")).isEqualTo("custom");

        assertThat(SigilGoogleAdkAdapter.inferProvider("gpt-5")).isEqualTo("openai");
        assertThat(SigilGoogleAdkAdapter.inferProvider("claude-sonnet-4-5")).isEqualTo("anthropic");
        assertThat(SigilGoogleAdkAdapter.inferProvider("gemini-2.5-pro")).isEqualTo("gemini");
        assertThat(SigilGoogleAdkAdapter.inferProvider("mistral-large")).isEqualTo("custom");
    }

    @Test
    void buildFrameworkMetadataIncludesCanonicalLineageKeys() {
        Map<String, Object> metadata = SigilGoogleAdkAdapter.buildFrameworkMetadata(new SigilGoogleAdkAdapter.MetadataInput()
                .setBase(Map.of("team", "infra"))
                .setEvent(Map.of("phase", "plan"))
                .setRunId("run-1")
                .setThreadId("thread-1")
                .setParentRunId("parent-1")
                .setComponentName("planner")
                .setRunType("chat")
                .setTags(List.of("prod", "framework"))
                .setRetryAttempt(3)
                .setEventId("event-1"));

        assertThat(metadata)
                .containsEntry(SigilGoogleAdkAdapter.META_RUN_ID, "run-1")
                .containsEntry(SigilGoogleAdkAdapter.META_RUN_TYPE, "chat")
                .containsEntry(SigilGoogleAdkAdapter.META_THREAD_ID, "thread-1")
                .containsEntry(SigilGoogleAdkAdapter.META_PARENT_RUN_ID, "parent-1")
                .containsEntry(SigilGoogleAdkAdapter.META_COMPONENT_NAME, "planner")
                .containsEntry(SigilGoogleAdkAdapter.META_RETRY_ATTEMPT, 3)
                .containsEntry(SigilGoogleAdkAdapter.META_EVENT_ID, "event-1")
                .containsEntry("team", "infra")
                .containsEntry("phase", "plan");
    }

    @Test
    void adapterRunAndToolLifecycleCompletesWithoutRecorderErrors() {
        SigilClient client = newClient();
        try {
            SigilGoogleAdkAdapter adapter = new SigilGoogleAdkAdapter(client, new SigilGoogleAdkAdapter.Options()
                    .setAgentName("adk-agent")
                    .setAgentVersion("1.0.0")
                    .setCaptureInputs(true)
                    .setCaptureOutputs(true));

            adapter.onRunStart(new SigilGoogleAdkAdapter.RunStartEvent()
                    .setRunId("run-sync")
                    .setSessionId("session-42")
                    .setParentRunId("parent-run")
                    .setEventId("event-42")
                    .setRunType("chat")
                    .setModelName("gpt-5")
                    .addPrompt("hello")
                    .putMetadata("team", "infra"));
            adapter.onRunEnd("run-sync", new SigilGoogleAdkAdapter.RunEndEvent().setResponseModel("gpt-5").setStopReason("stop"));

            adapter.onToolStart(new SigilGoogleAdkAdapter.ToolStartEvent()
                    .setRunId("tool-run")
                    .setSessionId("session-42")
                    .setToolName("lookup_customer")
                    .setArguments(Map.of("customer_id", "42")));
            adapter.onToolEnd("tool-run", new SigilGoogleAdkAdapter.ToolEndEvent().setResult(Map.of("status", "ok")));

            adapter.onRunStart(new SigilGoogleAdkAdapter.RunStartEvent()
                    .setRunId("run-stream")
                    .setModelName("claude-sonnet-4-5")
                    .setStream(true)
                    .addPrompt("stream me"));
            adapter.onRunToken("run-stream", "hello");
            adapter.onRunToken("run-stream", " world");
            adapter.onRunEnd("run-stream", new SigilGoogleAdkAdapter.RunEndEvent().setResponseModel("claude-sonnet-4-5"));
        } finally {
            client.shutdown();
        }
    }

    @Test
    void adapterUsesExplicitProviderWhenConfigured() {
        SigilClient client = newClient();
        try {
            SigilGoogleAdkAdapter adapter = new SigilGoogleAdkAdapter(client, new SigilGoogleAdkAdapter.Options()
                    .setProvider("gemini")
                    .setCaptureInputs(true)
                    .setCaptureOutputs(true));

            adapter.onRunStart(new SigilGoogleAdkAdapter.RunStartEvent()
                    .setRunId("run-provider")
                    .setModelName("gpt-5"));
            adapter.onRunEnd("run-provider", new SigilGoogleAdkAdapter.RunEndEvent());

            GenerationRecorder rec = client.startGeneration(new GenerationStart()
                    .setMode(GenerationMode.SYNC)
                    .setModel(new ModelRef().setProvider("openai").setName("gpt-5")));
            rec.end();

            client.startToolExecution(new ToolExecutionStart().setToolName("noop")).end();
        } finally {
            client.shutdown();
        }
    }

    @Test
    void syncRunExportsFrameworkPayloadTagsAndMetrics() {
        try (FrameworkConformanceEnv env = new FrameworkConformanceEnv()) {
            SigilGoogleAdkAdapter adapter = new SigilGoogleAdkAdapter(env.client, new SigilGoogleAdkAdapter.Options()
                    .setAgentName("adk-agent")
                    .setAgentVersion("1.0.0")
                    .setCaptureInputs(true)
                    .setCaptureOutputs(true)
                    .putExtraTag("team", "infra")
                    .putExtraMetadata("workspace", "sigil"));

            var parentSpan = env.tracerProvider.get("sigil-framework-test")
                    .spanBuilder("framework.request")
                    .setAttribute(AttributeKey.stringKey("sigil.framework.name"), "google-adk")
                    .setAttribute(AttributeKey.stringKey("sigil.framework.source"), "handler")
                    .setAttribute(AttributeKey.stringKey("sigil.framework.language"), "java")
                    .startSpan();
            try (Scope ignored = parentSpan.makeCurrent()) {
                adapter.onRunStart(new SigilGoogleAdkAdapter.RunStartEvent()
                        .setRunId("run-sync")
                        .setSessionId("session-42")
                        .setThreadId("thread-9")
                        .setParentRunId("framework-parent-run")
                        .setComponentName("planner")
                        .setRunType("chat")
                        .setRetryAttempt(2)
                        .setEventId("event-42")
                        .setModelName("gpt-5")
                        .addTag("prod")
                        .addTag("framework")
                        .addPrompt("hello")
                        .putMetadata("phase", "plan"));
                adapter.onRunEnd("run-sync", new SigilGoogleAdkAdapter.RunEndEvent()
                        .setResponseModel("gpt-5")
                        .setStopReason("stop")
                        .setUsage(new TokenUsage().setInputTokens(3).setOutputTokens(2).setTotalTokens(5))
                        .addOutputMessage(new Message()
                                .setRole(MessageRole.ASSISTANT)
                                .setParts(List.of(MessagePart.text("hi")))));
            } finally {
                parentSpan.end();
            }

            env.client.flush();

            Generation generation = env.exporter.singleGeneration();
            SpanData generationSpan = env.latestGenerationSpan();

            assertThat(generation.getMode()).isEqualTo(GenerationMode.SYNC);
            assertThat(generation.getOperationName()).isEqualTo("generateText");
            assertThat(generation.getConversationId()).isEqualTo("session-42");
            assertThat(generation.getResponseModel()).isEqualTo("gpt-5");
            assertThat(generation.getTraceId()).isEqualTo(generationSpan.getTraceId());
            assertThat(generation.getSpanId()).isEqualTo(generationSpan.getSpanId());
            assertThat(generation.getTags())
                    .containsEntry("sigil.framework.name", "google-adk")
                    .containsEntry("sigil.framework.source", "handler")
                    .containsEntry("sigil.framework.language", "java")
                    .containsEntry("team", "infra");
            assertThat(generation.getMetadata())
                    .containsEntry("workspace", "sigil")
                    .containsEntry("phase", "plan")
                    .containsEntry(SigilGoogleAdkAdapter.META_RUN_ID, "run-sync")
                    .containsEntry(SigilGoogleAdkAdapter.META_RUN_TYPE, "chat")
                    .containsEntry(SigilGoogleAdkAdapter.META_THREAD_ID, "thread-9")
                    .containsEntry(SigilGoogleAdkAdapter.META_PARENT_RUN_ID, "framework-parent-run")
                    .containsEntry(SigilGoogleAdkAdapter.META_COMPONENT_NAME, "planner")
                    .containsEntry(SigilGoogleAdkAdapter.META_RETRY_ATTEMPT, 2)
                    .containsEntry(SigilGoogleAdkAdapter.META_EVENT_ID, "event-42")
                    .containsEntry(SigilGoogleAdkAdapter.META_TAGS, List.of("prod", "framework"));
            assertThat(generation.getOutput()).hasSize(1);
            assertThat(generation.getOutput().get(0).getParts()).hasSize(1);
            assertThat(generation.getOutput().get(0).getParts().get(0).getText()).isEqualTo("hi");
            assertThat(generationSpan.getParentSpanId()).isEqualTo(parentSpan.getSpanContext().getSpanId());
            assertThat(env.metricNames())
                    .contains("gen_ai.client.operation.duration")
                    .doesNotContain("gen_ai.client.time_to_first_token");
        }
    }

    @Test
    void streamRunExportsStitchedOutputAndTtftMetric() {
        try (FrameworkConformanceEnv env = new FrameworkConformanceEnv()) {
            SigilGoogleAdkAdapter adapter = new SigilGoogleAdkAdapter(env.client, new SigilGoogleAdkAdapter.Options()
                    .setCaptureInputs(true)
                    .setCaptureOutputs(true));

            adapter.onRunStart(new SigilGoogleAdkAdapter.RunStartEvent()
                    .setRunId("run-stream-export")
                    .setSessionId("session-stream")
                    .setModelName("claude-sonnet-4-5")
                    .setStream(true)
                    .addPrompt("stream me"));
            adapter.onRunToken("run-stream-export", "hello");
            adapter.onRunToken("run-stream-export", " world");
            adapter.onRunEnd("run-stream-export", new SigilGoogleAdkAdapter.RunEndEvent()
                    .setResponseModel("claude-sonnet-4-5"));

            env.client.flush();

            Generation generation = env.exporter.singleGeneration();
            assertThat(generation.getMode()).isEqualTo(GenerationMode.STREAM);
            assertThat(generation.getOperationName()).isEqualTo("streamText");
            assertThat(generation.getResponseModel()).isEqualTo("claude-sonnet-4-5");
            assertThat(generation.getOutput()).hasSize(1);
            assertThat(generation.getOutput().get(0).getParts()).hasSize(1);
            assertThat(generation.getOutput().get(0).getParts().get(0).getText()).isEqualTo("hello world");
            assertThat(generation.getTags())
                    .containsEntry("sigil.framework.name", "google-adk")
                    .containsEntry("sigil.framework.source", "handler")
                    .containsEntry("sigil.framework.language", "java");
            assertThat(env.metricNames())
                    .contains("gen_ai.client.operation.duration", "gen_ai.client.time_to_first_token");
        }
    }

    @Test
    void generationSpanTracksActiveParentSpanAndPreservesExportLineage() {
        InMemorySpanExporter spanExporter = InMemorySpanExporter.create();
        SdkTracerProvider tracerProvider = SdkTracerProvider.builder()
                .addSpanProcessor(SimpleSpanProcessor.create(spanExporter))
                .build();
        SigilClient client = new SigilClient(
                new SigilClientConfig()
                        .setTracer(tracerProvider.get("sigil-framework-test"))
                        .setGenerationExport(
                                new GenerationExportConfig()
                                        .setProtocol(GenerationExportProtocol.NONE)));
        try {
            SigilGoogleAdkAdapter adapter = new SigilGoogleAdkAdapter(client, new SigilGoogleAdkAdapter.Options()
                    .setCaptureInputs(true)
                    .setCaptureOutputs(true));

            var parentSpan = tracerProvider.get("sigil-framework-test").spanBuilder("framework.request").startSpan();
            try (Scope ignored = parentSpan.makeCurrent()) {
                adapter.onRunStart(new SigilGoogleAdkAdapter.RunStartEvent()
                        .setRunId("run-lineage")
                        .setSessionId("session-lineage-42")
                        .setParentRunId("framework-parent-run")
                        .setRunType("chat")
                        .setModelName("gpt-5")
                        .addPrompt("hello"));
                adapter.onRunEnd("run-lineage", new SigilGoogleAdkAdapter.RunEndEvent()
                        .setResponseModel("gpt-5")
                        .setStopReason("stop"));
            } finally {
                parentSpan.end();
            }

            assertThat(client.debugSnapshot().getGenerations()).hasSize(1);
            var generation = client.debugSnapshot().getGenerations().get(0);
            var generationSpan = spanExporter.getFinishedSpanItems().stream()
                    .filter(span -> "generateText".equals(span.getAttributes().get(io.opentelemetry.api.common.AttributeKey.stringKey("gen_ai.operation.name"))))
                    .findFirst()
                    .orElseThrow();

            assertThat(generationSpan.getParentSpanId()).isEqualTo(parentSpan.getSpanContext().getSpanId());
            assertThat(generationSpan.getTraceId()).isEqualTo(parentSpan.getSpanContext().getTraceId());
            assertThat(generation.getTraceId()).isEqualTo(generationSpan.getTraceId());
            assertThat(generation.getSpanId()).isEqualTo(generationSpan.getSpanId());
        } finally {
            client.shutdown();
            tracerProvider.close();
        }
    }

    @Test
    void adapterExplicitlyHasNoEmbeddingLifecycle() {
        List<String> publicMethodNames = Arrays.stream(SigilGoogleAdkAdapter.class.getMethods())
                .map(Method::getName)
                .toList();

        assertThat(publicMethodNames)
                .doesNotContain("onEmbeddingStart")
                .doesNotContain("onEmbeddingEnd")
                .doesNotContain("onEmbeddingError");
    }

    @Test
    void onRunEndDropsOutputsWhenCaptureOutputsDisabled() {
        SigilClient client = newClient();
        try {
            SigilGoogleAdkAdapter adapter = new SigilGoogleAdkAdapter(client, new SigilGoogleAdkAdapter.Options()
                    .setCaptureInputs(true)
                    .setCaptureOutputs(false));

            adapter.onRunStart(new SigilGoogleAdkAdapter.RunStartEvent()
                    .setRunId("run-no-output")
                    .setSessionId("session-42")
                    .setModelName("gpt-5")
                    .addPrompt("hello"));
            adapter.onRunEnd("run-no-output", new SigilGoogleAdkAdapter.RunEndEvent()
                    .addOutputMessage(new Message()
                            .setRole(MessageRole.ASSISTANT)
                            .setParts(List.of(MessagePart.text("should-not-export")))));

            assertThat(client.debugSnapshot().getGenerations()).hasSize(1);
            assertThat(client.debugSnapshot().getGenerations().get(0).getOutput()).isEmpty();
        } finally {
            client.shutdown();
        }
    }

    @Test
    void onToolEndDropsArgumentsWhenCaptureInputsDisabled() {
        SigilClient client = newClient();
        try {
            SigilGoogleAdkAdapter adapter = new SigilGoogleAdkAdapter(client, new SigilGoogleAdkAdapter.Options()
                    .setCaptureInputs(false)
                    .setCaptureOutputs(true));

            adapter.onToolStart(new SigilGoogleAdkAdapter.ToolStartEvent()
                    .setRunId("tool-no-input")
                    .setSessionId("session-42")
                    .setToolName("lookup_customer")
                    .setArguments(Map.of("customer_id", "42")));
            adapter.onToolEnd("tool-no-input", new SigilGoogleAdkAdapter.ToolEndEvent()
                    .setResult(Map.of("status", "ok")));

            assertThat(client.debugSnapshot().getToolExecutions()).hasSize(1);
            assertThat(client.debugSnapshot().getToolExecutions().get(0).getArguments()).isNull();
            assertThat(client.debugSnapshot().getToolExecutions().get(0).getResult()).isEqualTo(Map.of("status", "ok"));
        } finally {
            client.shutdown();
        }
    }

    @Test
    void onRunTokenConcurrentCallbacksPreserveChunkedOutput() throws InterruptedException {
        SigilClient client = newClient();
        try {
            SigilGoogleAdkAdapter adapter = new SigilGoogleAdkAdapter(client, new SigilGoogleAdkAdapter.Options()
                    .setCaptureInputs(true)
                    .setCaptureOutputs(true));

            adapter.onRunStart(new SigilGoogleAdkAdapter.RunStartEvent()
                    .setRunId("run-token-concurrent")
                    .setSessionId("session-42")
                    .setModelName("gpt-5")
                    .setStream(true)
                    .addPrompt("hello"));

            int workers = 8;
            int tokensPerWorker = 250;
            CountDownLatch startGate = new CountDownLatch(1);
            CountDownLatch done = new CountDownLatch(workers);
            Runnable emitTokens = () -> {
                try {
                    startGate.await(5, TimeUnit.SECONDS);
                    for (int i = 0; i < tokensPerWorker; i++) {
                        adapter.onRunToken("run-token-concurrent", "x");
                    }
                } catch (InterruptedException exception) {
                    Thread.currentThread().interrupt();
                } finally {
                    done.countDown();
                }
            };

            for (int i = 0; i < workers; i++) {
                new Thread(emitTokens).start();
            }
            startGate.countDown();
            assertThat(done.await(5, TimeUnit.SECONDS)).isTrue();

            adapter.onRunEnd("run-token-concurrent", new SigilGoogleAdkAdapter.RunEndEvent());
            assertThat(client.debugSnapshot().getGenerations()).hasSize(1);
            assertThat(client.debugSnapshot().getGenerations().get(0).getOutput()).hasSize(1);
            String output = client.debugSnapshot().getGenerations().get(0).getOutput()
                    .get(0).getParts().get(0).getText();
            assertThat(output).hasSize(workers * tokensPerWorker);
        } finally {
            client.shutdown();
        }
    }

    @Test
    void onRunStartDeduplicatesConcurrentCallbacks() throws InterruptedException {
        SigilClient client = newClient();
        try {
            AtomicInteger starts = new AtomicInteger(0);
            SigilGoogleAdkAdapter adapter = new SigilGoogleAdkAdapter(
                    client,
                    new SigilGoogleAdkAdapter.Options(),
                    (start, stream) -> {
                        starts.incrementAndGet();
                        return stream ? client.startStreamingGeneration(start) : client.startGeneration(start);
                    },
                    client::startToolExecution);

            CountDownLatch startGate = new CountDownLatch(1);
            CountDownLatch done = new CountDownLatch(2);
            Runnable callback = () -> {
                try {
                    startGate.await(5, TimeUnit.SECONDS);
                    adapter.onRunStart(new SigilGoogleAdkAdapter.RunStartEvent()
                            .setRunId("run-concurrent")
                            .setModelName("gpt-5"));
                } catch (InterruptedException exception) {
                    Thread.currentThread().interrupt();
                } finally {
                    done.countDown();
                }
            };

            Thread first = new Thread(callback);
            Thread second = new Thread(callback);
            first.start();
            second.start();
            startGate.countDown();
            assertThat(done.await(5, TimeUnit.SECONDS)).isTrue();

            assertThat(starts.get()).isEqualTo(1);
            adapter.onRunEnd("run-concurrent", new SigilGoogleAdkAdapter.RunEndEvent());
        } finally {
            client.shutdown();
        }
    }

    @Test
    void onToolStartDeduplicatesConcurrentCallbacks() throws InterruptedException {
        SigilClient client = newClient();
        try {
            AtomicInteger starts = new AtomicInteger(0);
            SigilGoogleAdkAdapter adapter = new SigilGoogleAdkAdapter(
                    client,
                    new SigilGoogleAdkAdapter.Options(),
                    (start, stream) -> stream ? client.startStreamingGeneration(start) : client.startGeneration(start),
                    start -> {
                        starts.incrementAndGet();
                        return client.startToolExecution(start);
                    });

            CountDownLatch startGate = new CountDownLatch(1);
            CountDownLatch done = new CountDownLatch(2);
            Runnable callback = () -> {
                try {
                    startGate.await(5, TimeUnit.SECONDS);
                    adapter.onToolStart(new SigilGoogleAdkAdapter.ToolStartEvent()
                            .setRunId("tool-concurrent")
                            .setSessionId("session-42")
                            .setToolName("lookup_customer"));
                } catch (InterruptedException exception) {
                    Thread.currentThread().interrupt();
                } finally {
                    done.countDown();
                }
            };

            Thread first = new Thread(callback);
            Thread second = new Thread(callback);
            first.start();
            second.start();
            startGate.countDown();
            assertThat(done.await(5, TimeUnit.SECONDS)).isTrue();

            assertThat(starts.get()).isEqualTo(1);
            adapter.onToolEnd("tool-concurrent", new SigilGoogleAdkAdapter.ToolEndEvent());
        } finally {
            client.shutdown();
        }
    }

    @Test
    void createCallbacksProvidesOneTimeLifecycleWiring() {
        SigilClient client = newClient();
        try {
            SigilGoogleAdkAdapter.Callbacks callbacks = SigilGoogleAdkAdapter.createCallbacks(
                    client,
                    new SigilGoogleAdkAdapter.Options()
                            .setAgentName("adk-agent")
                            .setCaptureInputs(true)
                            .setCaptureOutputs(true));

            callbacks.onRunStart(new SigilGoogleAdkAdapter.RunStartEvent()
                    .setRunId("run-callbacks")
                    .setSessionId("session-callbacks")
                    .setModelName("gpt-5")
                    .addPrompt("hello"));
            callbacks.onRunToken("run-callbacks", "hi");
            callbacks.onRunEnd("run-callbacks", new SigilGoogleAdkAdapter.RunEndEvent()
                    .setResponseModel("gpt-5")
                    .setStopReason("stop"));

            callbacks.onToolStart(new SigilGoogleAdkAdapter.ToolStartEvent()
                    .setRunId("tool-callbacks")
                    .setSessionId("session-callbacks")
                    .setToolName("lookup_customer")
                    .setArguments(Map.of("id", "42")));
            callbacks.onToolEnd("tool-callbacks", new SigilGoogleAdkAdapter.ToolEndEvent()
                    .setResult(Map.of("status", "ok")));

            assertThat(client.debugSnapshot().getGenerations()).hasSize(1);
            assertThat(client.debugSnapshot().getToolExecutions()).hasSize(1);
        } finally {
            client.shutdown();
        }
    }

    private static final class FrameworkConformanceEnv implements AutoCloseable {
        private final CapturingExporter exporter = new CapturingExporter();
        private final InMemorySpanExporter spanExporter = InMemorySpanExporter.create();
        private final SdkTracerProvider tracerProvider = SdkTracerProvider.builder()
                .addSpanProcessor(SimpleSpanProcessor.create(spanExporter))
                .build();
        private final InMemoryMetricReader metricReader = InMemoryMetricReader.create();
        private final SdkMeterProvider meterProvider = SdkMeterProvider.builder()
                .registerMetricReader(metricReader)
                .build();
        private final SigilClient client = new SigilClient(new SigilClientConfig()
                .setTracer(tracerProvider.get("sigil-framework-test"))
                .setMeter(meterProvider.get("sigil-framework-test"))
                .setGenerationExporter(exporter)
                .setGenerationExport(new GenerationExportConfig()
                        .setBatchSize(1)
                        .setQueueSize(10)
                        .setFlushInterval(Duration.ofHours(1))
                        .setMaxRetries(0)));

        private List<String> metricNames() {
            return metricReader.collectAllMetrics().stream()
                    .map(MetricData::getName)
                    .toList();
        }

        private SpanData latestGenerationSpan() {
            return spanExporter.getFinishedSpanItems().stream()
                    .filter(span -> {
                        String operation = span.getAttributes().get(AttributeKey.stringKey("gen_ai.operation.name"));
                        return "generateText".equals(operation) || "streamText".equals(operation);
                    })
                    .reduce((first, second) -> second)
                    .orElseThrow();
        }

        @Override
        public void close() {
            client.shutdown();
            tracerProvider.close();
            meterProvider.close();
        }
    }

    private static final class CapturingExporter implements GenerationExporter {
        private final List<Generation> generations = new ArrayList<>();

        @Override
        public ExportGenerationsResponse exportGenerations(ExportGenerationsRequest request) {
            List<ExportGenerationResult> results = new ArrayList<>();
            for (Generation generation : request.getGenerations()) {
                generations.add(generation.copy());
                results.add(new ExportGenerationResult().setGenerationId(generation.getId()).setAccepted(true));
            }
            return new ExportGenerationsResponse().setResults(results);
        }

        private Generation singleGeneration() {
            assertThat(generations).hasSize(1);
            return generations.get(0);
        }
    }
}
