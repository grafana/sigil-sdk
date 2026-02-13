package com.grafana.sigil.sdk.providers.anthropic;

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
import com.grafana.sigil.sdk.providers.openai.OpenAiAdapter;
import io.opentelemetry.api.GlobalOpenTelemetry;
import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.CopyOnWriteArrayList;
import org.junit.jupiter.api.Test;

class AnthropicAdapterTest {
    @Test
    void syncAndStreamWrappersSetAnthropicProviderAndModes() throws Exception {
        CapturingExporter exporter = new CapturingExporter();
        try (SigilClient client = new SigilClient(new SigilClientConfig()
                .setTracer(GlobalOpenTelemetry.getTracer("test"))
                .setGenerationExporter(exporter)
                .setGenerationExport(new GenerationExportConfig().setBatchSize(1).setFlushInterval(Duration.ofMinutes(10)).setMaxRetries(0)))) {

            AnthropicAdapter.completion(client, request(), _r -> response(), new OpenAiAdapter.OpenAiOptions());
            AnthropicAdapter.completionStream(
                    client,
                    request(),
                    _r -> new OpenAiAdapter.OpenAiStreamSummary().setOutputText("stream").setFinalResponse(response()),
                    new OpenAiAdapter.OpenAiOptions());
        }

        assertThat(exporter.generations).hasSize(2);
        assertThat(exporter.generations.get(0).getModel().getProvider()).isEqualTo("anthropic");
        assertThat(exporter.generations.get(0).getMode()).isEqualTo(GenerationMode.SYNC);
        assertThat(exporter.generations.get(1).getMode()).isEqualTo(GenerationMode.STREAM);
    }

    @Test
    void rawArtifactsAreOffByDefaultAndEnabledByOptIn() throws Exception {
        CapturingExporter exporter = new CapturingExporter();
        try (SigilClient client = new SigilClient(new SigilClientConfig()
                .setTracer(GlobalOpenTelemetry.getTracer("test"))
                .setGenerationExporter(exporter)
                .setGenerationExport(new GenerationExportConfig().setBatchSize(1).setFlushInterval(Duration.ofMinutes(10)).setMaxRetries(0)))) {

            AnthropicAdapter.completion(client, request(), _r -> response(), new OpenAiAdapter.OpenAiOptions());
            AnthropicAdapter.completionStream(
                    client,
                    request(),
                    _r -> new OpenAiAdapter.OpenAiStreamSummary().setOutputText("stream").setFinalResponse(response()),
                    new OpenAiAdapter.OpenAiOptions());
            AnthropicAdapter.completionStream(
                    client,
                    request(),
                    _r -> new OpenAiAdapter.OpenAiStreamSummary().setOutputText("stream").setFinalResponse(response()).setChunks(List.of("event")),
                    new OpenAiAdapter.OpenAiOptions().setRawArtifacts(true));
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
                AnthropicAdapter.completion(
                        client,
                        request(),
                        _r -> {
                            throw new RuntimeException("anthropic failed");
                        },
                        new OpenAiAdapter.OpenAiOptions());
            }
        }).isInstanceOf(RuntimeException.class).hasMessageContaining("anthropic failed");

        assertThat(exporter.generations).hasSize(1);
        assertThat(exporter.generations.get(0).getCallError()).contains("anthropic failed");
    }

    private static OpenAiAdapter.OpenAiChatRequest request() {
        return new OpenAiAdapter.OpenAiChatRequest().setModel("claude-3").setMessages(List.of(new OpenAiAdapter.OpenAiMessage().setRole("user").setContent("hi")));
    }

    private static OpenAiAdapter.OpenAiChatResponse response() {
        return new OpenAiAdapter.OpenAiChatResponse().setOutputText("ok");
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
