package com.grafana.sigil.sdk.frameworks.googleadk;

import static org.assertj.core.api.Assertions.assertThat;

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
import com.grafana.sigil.sdk.ToolExecutionStart;
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
}
