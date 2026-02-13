package com.grafana.sigil.sdk;

import com.fasterxml.jackson.databind.JsonNode;
import com.google.protobuf.util.JsonFormat;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;

/** HTTP exporter for generation ingest parity endpoint. */
public final class HttpGenerationExporter implements GenerationExporter {
    private final URI endpoint;
    private final Map<String, String> headers;
    private final HttpClient client;

    public HttpGenerationExporter(String endpoint, Map<String, String> headers) {
        this.endpoint = URI.create(normalizeEndpoint(endpoint));
        this.headers = headers;
        this.client = HttpClient.newBuilder().connectTimeout(Duration.ofSeconds(10)).build();
    }

    @Override
    public ExportGenerationsResponse exportGenerations(ExportGenerationsRequest request) throws Exception {
        String body = JsonFormat.printer()
                .preservingProtoFieldNames()
                .print(ProtoMapper.toProtoRequest(request));

        HttpRequest.Builder requestBuilder = HttpRequest.newBuilder()
                .uri(endpoint)
                .timeout(Duration.ofSeconds(10))
                .header("Content-Type", "application/json")
                .POST(HttpRequest.BodyPublishers.ofString(body));
        for (Map.Entry<String, String> entry : headers.entrySet()) {
            requestBuilder.header(entry.getKey(), entry.getValue());
        }

        HttpResponse<String> response = client.send(requestBuilder.build(), HttpResponse.BodyHandlers.ofString());
        if (response.statusCode() < 200 || response.statusCode() >= 300) {
            throw new RuntimeException("http generation export status " + response.statusCode() + ": " + response.body().trim());
        }

        JsonNode payload = Json.MAPPER.readTree(response.body());
        JsonNode resultsNode = payload.path("results");
        if (!resultsNode.isArray()) {
            throw new RuntimeException("invalid generation export response payload");
        }

        List<ExportGenerationResult> results = new ArrayList<>();
        for (int i = 0; i < resultsNode.size(); i++) {
            JsonNode item = resultsNode.get(i);
            String generationId = firstNonBlank(
                    item.path("generation_id").asText(""),
                    item.path("generationId").asText(""),
                    i < request.getGenerations().size() ? request.getGenerations().get(i).getId() : "");
            results.add(new ExportGenerationResult()
                    .setGenerationId(generationId)
                    .setAccepted(item.path("accepted").asBoolean(false))
                    .setError(item.path("error").asText("")));
        }

        return new ExportGenerationsResponse().setResults(results);
    }

    private static String normalizeEndpoint(String endpoint) {
        String trimmed = endpoint == null ? "" : endpoint.trim();
        if (trimmed.isEmpty()) {
            throw new IllegalArgumentException("generation export endpoint is required");
        }
        if (trimmed.startsWith("http://") || trimmed.startsWith("https://")) {
            return trimmed;
        }
        return "http://" + trimmed;
    }

    private static String firstNonBlank(String... values) {
        for (String value : values) {
            if (value != null && !value.isBlank()) {
                return value;
            }
        }
        return "";
    }
}
