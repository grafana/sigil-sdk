package com.grafana.sigil.sdk.benchmarks;

import com.grafana.sigil.sdk.ExportGenerationResult;
import com.grafana.sigil.sdk.ExportGenerationsRequest;
import com.grafana.sigil.sdk.ExportGenerationsResponse;
import com.grafana.sigil.sdk.GenerationExportConfig;
import com.grafana.sigil.sdk.GenerationExporter;
import com.grafana.sigil.sdk.GenerationStart;
import com.grafana.sigil.sdk.Message;
import com.grafana.sigil.sdk.MessagePart;
import com.grafana.sigil.sdk.MessageRole;
import com.grafana.sigil.sdk.ModelRef;
import com.grafana.sigil.sdk.SigilClient;
import com.grafana.sigil.sdk.SigilClientConfig;
import com.grafana.sigil.sdk.providers.openai.OpenAiChatCompletions;
import com.grafana.sigil.sdk.providers.openai.OpenAiOptions;
import com.openai.core.ObjectMappers;
import com.openai.models.chat.completions.ChatCompletion;
import com.openai.models.chat.completions.ChatCompletionCreateParams;
import io.opentelemetry.api.GlobalOpenTelemetry;
import java.io.IOException;
import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.TimeUnit;
import org.openjdk.jmh.annotations.Benchmark;
import org.openjdk.jmh.annotations.BenchmarkMode;
import org.openjdk.jmh.annotations.Level;
import org.openjdk.jmh.annotations.Mode;
import org.openjdk.jmh.annotations.OutputTimeUnit;
import org.openjdk.jmh.annotations.Scope;
import org.openjdk.jmh.annotations.Setup;
import org.openjdk.jmh.annotations.State;

@State(Scope.Benchmark)
public class SdkBenchmarks {
    private ChatCompletionCreateParams request;
    private ChatCompletion response;
    private SigilClient client;

    @Setup(Level.Trial)
    public void setup() throws IOException {
        request = ChatCompletionCreateParams.builder()
                .model("gpt-5")
                .addUserMessage("hello")
                .maxCompletionTokens(128L)
                .build();
        response = ObjectMappers.jsonMapper().readValue(
                """
                {
                  "id": "chatcmpl_1",
                  "choices": [
                    {
                      "finish_reason": "stop",
                      "index": 0,
                      "message": {
                        "role": "assistant",
                        "content": "hello"
                      }
                    }
                  ],
                  "created": 1,
                  "model": "gpt-5"
                }
                """,
                ChatCompletion.class);

        client = new SigilClient(new SigilClientConfig()
                .setTracer(GlobalOpenTelemetry.getTracer("bench"))
                .setGenerationExporter(new NoopExporter())
                .setGenerationExport(new GenerationExportConfig()
                        .setBatchSize(1)
                        .setQueueSize(10000)
                        .setFlushInterval(Duration.ofMinutes(10))
                        .setMaxRetries(0)));
    }

    @Benchmark
    @BenchmarkMode(Mode.Throughput)
    @OutputTimeUnit(TimeUnit.SECONDS)
    public Object mapOpenAiSync() {
        return OpenAiChatCompletions.fromRequestResponse(request, response, new OpenAiOptions());
    }

    @Benchmark
    @BenchmarkMode(Mode.Throughput)
    @OutputTimeUnit(TimeUnit.SECONDS)
    public void recordGenerationHotPath() {
        GenerationStart start = new GenerationStart()
                .setModel(new ModelRef().setProvider("openai").setName("gpt-5"))
                .setOperationName("generateText");
        var recorder = client.startGeneration(start);
        recorder.setResult(new com.grafana.sigil.sdk.GenerationResult().setOutput(List.of(
                new Message().setRole(MessageRole.ASSISTANT).setParts(List.of(MessagePart.text("hello"))))));
        recorder.end();
    }

    private static final class NoopExporter implements GenerationExporter {
        @Override
        public ExportGenerationsResponse exportGenerations(ExportGenerationsRequest request) {
            List<ExportGenerationResult> results = new ArrayList<>();
            request.getGenerations().forEach(g -> results.add(new ExportGenerationResult().setGenerationId(g.getId()).setAccepted(true)));
            return new ExportGenerationsResponse().setResults(results);
        }
    }
}
