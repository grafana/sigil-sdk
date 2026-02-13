package com.grafana.sigil.sdk.providers.openai;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.grafana.sigil.sdk.AuthMode;
import com.grafana.sigil.sdk.ExportGenerationResult;
import com.grafana.sigil.sdk.ExportGenerationsRequest;
import com.grafana.sigil.sdk.ExportGenerationsResponse;
import com.grafana.sigil.sdk.Generation;
import com.grafana.sigil.sdk.GenerationExportConfig;
import com.grafana.sigil.sdk.GenerationExporter;
import com.grafana.sigil.sdk.GenerationMode;
import com.grafana.sigil.sdk.MessageRole;
import com.grafana.sigil.sdk.SigilClient;
import com.grafana.sigil.sdk.SigilClientConfig;
import com.grafana.sigil.sdk.ToolDefinition;
import io.opentelemetry.api.GlobalOpenTelemetry;
import java.time.Duration;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.concurrent.CopyOnWriteArrayList;
import org.junit.jupiter.api.Test;

class OpenAiAdapterTest {
    @Test
    void syncWrapperSetsSyncModeAndRawArtifactsOffByDefault() throws Exception {
        CapturingExporter exporter = new CapturingExporter();
        try (SigilClient client = newClient(exporter)) {
            OpenAiAdapter.OpenAiChatResponse response = OpenAiAdapter.chatCompletion(
                    client,
                    requestFixture(),
                    _request -> responseFixture(),
                    new OpenAiAdapter.OpenAiOptions());
            assertThat(response.getOutputText()).isEqualTo("hello");
        }

        Generation generation = exporter.singleGeneration();
        assertThat(generation.getMode()).isEqualTo(GenerationMode.SYNC);
        assertThat(generation.getModel().getProvider()).isEqualTo("openai");
        assertThat(generation.getMaxTokens()).isEqualTo(512L);
        assertThat(generation.getTemperature()).isEqualTo(0.3);
        assertThat(generation.getTopP()).isEqualTo(0.8);
        assertThat(generation.getToolChoice()).isEqualTo("{\"function\":{\"name\":\"weather\"},\"type\":\"function\"}");
        assertThat(generation.getThinkingEnabled()).isTrue();
        assertThat(generation.getMetadata().get("sigil.gen_ai.request.thinking.budget_tokens")).isEqualTo(1024L);
        assertThat(generation.getArtifacts()).isEmpty();
    }

    @Test
    void streamWrapperSetsStreamModeAndRawArtifactsOnlyWithOptIn() throws Exception {
        CapturingExporter exporter = new CapturingExporter();
        try (SigilClient client = newClient(exporter)) {
            OpenAiAdapter.chatCompletionStream(
                    client,
                    requestFixture(),
                    _request -> new OpenAiAdapter.OpenAiStreamSummary()
                            .setOutputText("stream")
                            .setFinalResponse(responseFixture())
                            .setChunks(List.of("event")),
                    new OpenAiAdapter.OpenAiOptions().setRawArtifacts(true));
        }

        Generation generation = exporter.singleGeneration();
        assertThat(generation.getMode()).isEqualTo(GenerationMode.STREAM);
        assertThat(generation.getArtifacts()).hasSize(2);
        assertThat(generation.getArtifacts().get(1).getKind().name()).isEqualTo("PROVIDER_EVENT");
    }

    @Test
    void providerErrorsPopulateCallError() {
        CapturingExporter exporter = new CapturingExporter();
        assertThatThrownBy(() -> {
            try (SigilClient client = newClient(exporter)) {
                OpenAiAdapter.chatCompletion(
                        client,
                        requestFixture(),
                        _request -> {
                            throw new RuntimeException("provider blew up");
                        },
                        new OpenAiAdapter.OpenAiOptions());
            }
        }).isInstanceOf(RuntimeException.class).hasMessageContaining("provider blew up");

        Generation generation = exporter.singleGeneration();
        assertThat(generation.getCallError()).contains("provider blew up");
    }

    @Test
    void mapperFiltersSystemMessagesAndKeepsToolRole() {
        OpenAiAdapter.OpenAiChatRequest request = requestFixture();
        OpenAiAdapter.OpenAiChatResponse response = responseFixture();

        var mapped = OpenAiAdapter.fromRequestResponse(request, response, new OpenAiAdapter.OpenAiOptions());
        assertThat(mapped.getInput()).hasSize(2);
        assertThat(mapped.getInput().get(0).getRole()).isEqualTo(MessageRole.USER);
        assertThat(mapped.getInput().get(1).getRole()).isEqualTo(MessageRole.TOOL);
        assertThat(mapped.getMaxTokens()).isEqualTo(512L);
        assertThat(mapped.getTemperature()).isEqualTo(0.3);
        assertThat(mapped.getTopP()).isEqualTo(0.8);
        assertThat(mapped.getToolChoice()).isEqualTo("{\"function\":{\"name\":\"weather\"},\"type\":\"function\"}");
        assertThat(mapped.getThinkingEnabled()).isTrue();
        assertThat(mapped.getMetadata().get("sigil.gen_ai.request.thinking.budget_tokens")).isEqualTo(1024L);
    }

    @Test
    void mapperLeavesThinkingUnsetWithoutReasoningConfig() {
        OpenAiAdapter.OpenAiChatRequest request = requestFixture().setReasoning(null);
        OpenAiAdapter.OpenAiChatResponse response = responseFixture();

        var mapped = OpenAiAdapter.fromRequestResponse(request, response, new OpenAiAdapter.OpenAiOptions());
        assertThat(mapped.getThinkingEnabled()).isNull();
    }

    private static SigilClient newClient(CapturingExporter exporter) {
        return new SigilClient(new SigilClientConfig()
                .setTracer(GlobalOpenTelemetry.getTracer("test"))
                .setGenerationExporter(exporter)
                .setGenerationExport(new GenerationExportConfig()
                        .setBatchSize(1)
                        .setFlushInterval(Duration.ofMinutes(10))
                        .setMaxRetries(0)));
    }

    private static OpenAiAdapter.OpenAiChatRequest requestFixture() {
        return new OpenAiAdapter.OpenAiChatRequest()
                .setModel("gpt-5")
                .setSystemPrompt("be concise")
                .setMaxCompletionTokens(512L)
                .setMaxTokens(1024L)
                .setTemperature(0.3)
                .setTopP(0.8)
                .setToolChoice(new LinkedHashMap<>(java.util.Map.of(
                        "type", "function",
                        "function", java.util.Map.of("name", "weather"))))
                .setReasoning(java.util.Map.of("effort", "medium", "max_output_tokens", 1024))
                .setMessages(List.of(
                        new OpenAiAdapter.OpenAiMessage().setRole("system").setContent("system"),
                        new OpenAiAdapter.OpenAiMessage().setRole("user").setContent("hello"),
                        new OpenAiAdapter.OpenAiMessage().setRole("tool").setContent("tool-result")))
                .setTools(List.of(new ToolDefinition().setName("weather").setType("function")));
    }

    private static OpenAiAdapter.OpenAiChatResponse responseFixture() {
        return new OpenAiAdapter.OpenAiChatResponse()
                .setId("resp-1")
                .setModel("gpt-5-2026-02-01")
                .setOutputText("hello")
                .setStopReason("stop");
    }

    private static final class CapturingExporter implements GenerationExporter {
        private final List<Generation> generations = new CopyOnWriteArrayList<>();

        @Override
        public ExportGenerationsResponse exportGenerations(ExportGenerationsRequest request) {
            for (Generation generation : request.getGenerations()) {
                generations.add(generation.copy());
            }
            List<ExportGenerationResult> results = new ArrayList<>();
            for (Generation generation : request.getGenerations()) {
                results.add(new ExportGenerationResult().setGenerationId(generation.getId()).setAccepted(true));
            }
            return new ExportGenerationsResponse().setResults(results);
        }

        Generation singleGeneration() {
            assertThat(generations).hasSize(1);
            return generations.get(0);
        }
    }
}
