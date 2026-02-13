package com.grafana.sigil.sdk.providers.gemini;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.grafana.sigil.sdk.ExportGenerationResult;
import com.grafana.sigil.sdk.ExportGenerationsRequest;
import com.grafana.sigil.sdk.ExportGenerationsResponse;
import com.grafana.sigil.sdk.Generation;
import com.grafana.sigil.sdk.GenerationExportConfig;
import com.grafana.sigil.sdk.GenerationExporter;
import com.grafana.sigil.sdk.GenerationMode;
import com.grafana.sigil.sdk.SigilClient;
import com.grafana.sigil.sdk.SigilClientConfig;
import com.grafana.sigil.sdk.providers.openai.ProviderAdapterSupport;
import io.opentelemetry.api.GlobalOpenTelemetry;
import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CopyOnWriteArrayList;
import org.junit.jupiter.api.Test;

class GeminiAdapterTest {
    @Test
    void syncAndStreamWrappersSetGeminiProviderAndModes() throws Exception {
        CapturingExporter exporter = new CapturingExporter();
        try (SigilClient client = new SigilClient(new SigilClientConfig()
                .setTracer(GlobalOpenTelemetry.getTracer("test"))
                .setGenerationExporter(exporter)
                .setGenerationExport(new GenerationExportConfig().setBatchSize(1).setFlushInterval(Duration.ofMinutes(10)).setMaxRetries(0)))) {

            GeminiAdapter.completion(client, request(), _r -> response(), new ProviderAdapterSupport.OpenAiOptions());
            GeminiAdapter.completionStream(
                    client,
                    request(),
                    _r -> new ProviderAdapterSupport.OpenAiStreamSummary().setOutputText("stream").setFinalResponse(response()),
                    new ProviderAdapterSupport.OpenAiOptions());
        }

        assertThat(exporter.generations).hasSize(2);
        assertThat(exporter.generations.get(0).getModel().getProvider()).isEqualTo("gemini");
        assertThat(exporter.generations.get(0).getMode()).isEqualTo(GenerationMode.SYNC);
        assertThat(exporter.generations.get(0).getMaxTokens()).isEqualTo(512L);
        assertThat(exporter.generations.get(0).getTemperature()).isEqualTo(0.2);
        assertThat(exporter.generations.get(0).getTopP()).isEqualTo(0.75);
        assertThat(exporter.generations.get(0).getToolChoice()).isEqualTo("{\"mode\":\"auto\"}");
        assertThat(exporter.generations.get(0).getThinkingEnabled()).isTrue();
        assertThat(exporter.generations.get(0).getMetadata().get("sigil.gen_ai.request.thinking.budget_tokens")).isEqualTo(1536L);
        assertThat(exporter.generations.get(1).getMode()).isEqualTo(GenerationMode.STREAM);
    }

    @Test
    void rawArtifactsAreOffByDefaultAndEnabledByOptIn() throws Exception {
        CapturingExporter exporter = new CapturingExporter();
        try (SigilClient client = new SigilClient(new SigilClientConfig()
                .setTracer(GlobalOpenTelemetry.getTracer("test"))
                .setGenerationExporter(exporter)
                .setGenerationExport(new GenerationExportConfig().setBatchSize(1).setFlushInterval(Duration.ofMinutes(10)).setMaxRetries(0)))) {

            GeminiAdapter.completion(client, request(), _r -> response(), new ProviderAdapterSupport.OpenAiOptions());
            GeminiAdapter.completionStream(
                    client,
                    request(),
                    _r -> new ProviderAdapterSupport.OpenAiStreamSummary().setOutputText("stream").setFinalResponse(response()),
                    new ProviderAdapterSupport.OpenAiOptions());
            GeminiAdapter.completionStream(
                    client,
                    request(),
                    _r -> new ProviderAdapterSupport.OpenAiStreamSummary().setOutputText("stream").setFinalResponse(response()).setChunks(List.of("event")),
                    new ProviderAdapterSupport.OpenAiOptions().setRawArtifacts(true));
        }

        assertThat(exporter.generations).hasSize(3);
        assertThat(exporter.generations.get(0).getArtifacts()).isEmpty();
        assertThat(exporter.generations.get(1).getArtifacts()).isEmpty();
        assertThat(exporter.generations.get(2).getArtifacts()).hasSize(2);
    }

    @Test
    void providerErrorsPopulateCallError() {
        CapturingExporter exporter = new CapturingExporter();
        assertThatThrownBy(() -> {
            try (SigilClient client = new SigilClient(new SigilClientConfig()
                    .setTracer(GlobalOpenTelemetry.getTracer("test"))
                    .setGenerationExporter(exporter)
                    .setGenerationExport(new GenerationExportConfig().setBatchSize(1).setFlushInterval(Duration.ofMinutes(10)).setMaxRetries(0)))) {
                GeminiAdapter.completion(
                        client,
                        request(),
                        _r -> {
                            throw new RuntimeException("gemini failed");
                        },
                        new ProviderAdapterSupport.OpenAiOptions());
            }
        }).isInstanceOf(RuntimeException.class).hasMessageContaining("gemini failed");

        assertThat(exporter.generations).hasSize(1);
        assertThat(exporter.generations.get(0).getCallError()).contains("gemini failed");
    }

    @Test
    void mapperSetsThinkingFalseWhenIncludeThoughtsFalse() {
        var mapped = GeminiAdapter.fromRequestResponse(
                request().setThinkingConfig(Map.of("include_thoughts", false)),
                response(),
                new ProviderAdapterSupport.OpenAiOptions());
        assertThat(mapped.getThinkingEnabled()).isFalse();
    }

    private static ProviderAdapterSupport.OpenAiChatRequest request() {
        return new ProviderAdapterSupport.OpenAiChatRequest()
                .setModel("gemini-2.5")
                .setMaxOutputTokens(512L)
                .setTemperature(0.2)
                .setTopP(0.75)
                .setFunctionCallingMode(Map.of("mode", "auto"))
                .setThinkingConfig(Map.of("include_thoughts", true, "thinking_budget", 1536))
                .setMessages(List.of(new ProviderAdapterSupport.OpenAiMessage().setRole("user").setContent("hi")));
    }

    private static ProviderAdapterSupport.OpenAiChatResponse response() {
        return new ProviderAdapterSupport.OpenAiChatResponse().setOutputText("ok");
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
    }
}
