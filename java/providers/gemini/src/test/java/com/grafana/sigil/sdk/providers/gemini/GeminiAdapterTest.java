package com.grafana.sigil.sdk.providers.gemini;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.google.genai.types.Content;
import com.google.genai.types.GenerateContentConfig;
import com.google.genai.types.GenerateContentResponse;
import com.google.genai.types.Part;
import com.grafana.sigil.sdk.ExportGenerationResult;
import com.grafana.sigil.sdk.ExportGenerationsRequest;
import com.grafana.sigil.sdk.ExportGenerationsResponse;
import com.grafana.sigil.sdk.Generation;
import com.grafana.sigil.sdk.GenerationExportConfig;
import com.grafana.sigil.sdk.GenerationExporter;
import com.grafana.sigil.sdk.GenerationMode;
import com.grafana.sigil.sdk.SigilClient;
import com.grafana.sigil.sdk.SigilClientConfig;
import io.opentelemetry.api.GlobalOpenTelemetry;
import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
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

            GeminiAdapter.completion(client, model(), contents(), config(), (_m, _c, _cfg) -> response(), new GeminiOptions());
            GeminiAdapter.completionStream(client, model(), contents(), config(), (_m, _c, _cfg) -> List.of(response()), new GeminiOptions());
        }

        assertThat(exporter.generations).hasSize(2);
        assertThat(exporter.generations.get(0).getModel().getProvider()).isEqualTo("gemini");
        assertThat(exporter.generations.get(0).getMode()).isEqualTo(GenerationMode.SYNC);
        assertThat(exporter.generations.get(0).getMaxTokens()).isEqualTo(512L);
        assertThat(exporter.generations.get(0).getTemperature()).isEqualTo(0.2);
        assertThat(exporter.generations.get(0).getTopP()).isEqualTo(0.75);
        assertThat(exporter.generations.get(0).getToolChoice()).isEqualTo("auto");
        assertThat(exporter.generations.get(0).getThinkingEnabled()).isTrue();
        assertThat(exporter.generations.get(0).getMetadata().get("sigil.gen_ai.request.thinking.budget_tokens")).isEqualTo(1536L);
        Object thinkingLevel = exporter.generations.get(0).getMetadata().get("sigil.gen_ai.request.thinking.level");
        if (thinkingLevel != null) {
            assertThat(thinkingLevel).isEqualTo("high");
        }
        assertThat(exporter.generations.get(0).getMetadata().get("sigil.gen_ai.usage.tool_use_prompt_tokens")).isEqualTo(5L);
        assertThat(exporter.generations.get(1).getMode()).isEqualTo(GenerationMode.STREAM);
    }

    @Test
    void rawArtifactsAreOffByDefaultAndEnabledByOptIn() throws Exception {
        CapturingExporter exporter = new CapturingExporter();
        try (SigilClient client = new SigilClient(new SigilClientConfig()
                .setTracer(GlobalOpenTelemetry.getTracer("test"))
                .setGenerationExporter(exporter)
                .setGenerationExport(new GenerationExportConfig().setBatchSize(1).setFlushInterval(Duration.ofMinutes(10)).setMaxRetries(0)))) {

            GeminiAdapter.completion(client, model(), contents(), config(), (_m, _c, _cfg) -> response(), new GeminiOptions());
            GeminiAdapter.completionStream(client, model(), contents(), config(), (_m, _c, _cfg) -> List.of(response()), new GeminiOptions());
            GeminiAdapter.completionStream(
                    client,
                    model(),
                    contents(),
                    config(),
                    (_m, _c, _cfg) -> List.of(response()),
                    new GeminiOptions().setRawArtifacts(true));
        }

        assertThat(exporter.generations).hasSize(3);
        assertThat(exporter.generations.get(0).getArtifacts()).isEmpty();
        assertThat(exporter.generations.get(1).getArtifacts()).isEmpty();
        assertThat(exporter.generations.get(2).getArtifacts()).hasSizeGreaterThanOrEqualTo(2);
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
                        model(),
                        contents(),
                        config(),
                        (_m, _c, _cfg) -> {
                            throw new RuntimeException("gemini failed");
                        },
                        new GeminiOptions());
            }
        }).isInstanceOf(RuntimeException.class).hasMessageContaining("gemini failed");

        assertThat(exporter.generations).hasSize(1);
        assertThat(exporter.generations.get(0).getCallError()).contains("gemini failed");
    }

    @Test
    void mapperSetsThinkingFalseWhenIncludeThoughtsFalse() {
        var mapped = GeminiAdapter.fromRequestResponse(
                model(),
                contents(),
                GenerateContentConfig.fromJson("""
                        {
                          "maxOutputTokens": 512,
                          "temperature": 0.2,
                          "topP": 0.75,
                          "thinkingConfig": {
                            "includeThoughts": false,
                            "thinkingBudget": 1536
                          }
                        }
                        """),
                response(),
                new GeminiOptions());
        assertThat(mapped.getThinkingEnabled()).isFalse();
    }

    private static String model() {
        return "gemini-2.5-pro";
    }

    private static List<Content> contents() {
        return List.of(Content.builder()
                .role("user")
                .parts(Part.fromText("hi"))
                .build());
    }

    private static GenerateContentConfig config() {
        return GenerateContentConfig.fromJson("""
                {
                  "maxOutputTokens": 512,
                  "temperature": 0.2,
                  "topP": 0.75,
                  "toolConfig": {
                    "functionCallingConfig": {
                      "mode": "AUTO"
                    }
                  },
                  "thinkingConfig": {
                    "includeThoughts": true,
                    "thinkingBudget": 1536,
                    "thinkingLevel": "HIGH"
                  }
                }
                """);
    }

    private static GenerateContentResponse response() {
        return GenerateContentResponse.fromJson("""
                {
                  "responseId": "resp_1",
                  "modelVersion": "gemini-2.5-pro-001",
                  "candidates": [
                    {
                      "finishReason": "STOP",
                      "content": {
                        "role": "model",
                        "parts": [
                          {"text": "ok"}
                        ]
                      }
                    }
                  ],
                  "usageMetadata": {
                    "promptTokenCount": 10,
                    "candidatesTokenCount": 5,
                    "totalTokenCount": 15,
                    "cachedContentTokenCount": 2,
                    "thoughtsTokenCount": 3,
                    "toolUsePromptTokenCount": 5
                  }
                }
                """);
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
