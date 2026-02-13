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
import com.grafana.sigil.sdk.providers.openai.ProviderAdapterSupport;
import io.opentelemetry.api.GlobalOpenTelemetry;
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
    private ProviderAdapterSupport.OpenAiChatRequest request;
    private ProviderAdapterSupport.OpenAiChatResponse response;
    private SigilClient client;

    @Setup(Level.Trial)
    public void setup() {
        request = new ProviderAdapterSupport.OpenAiChatRequest()
                .setModel("gpt-5")
                .setMessages(List.of(new ProviderAdapterSupport.OpenAiMessage().setRole("user").setContent("hello")));
        response = new ProviderAdapterSupport.OpenAiChatResponse().setOutputText("hello");

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
        return ProviderAdapterSupport.fromRequestResponse(request, response, new ProviderAdapterSupport.OpenAiOptions());
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
