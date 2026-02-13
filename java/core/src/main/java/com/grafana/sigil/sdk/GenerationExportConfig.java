package com.grafana.sigil.sdk;

import java.time.Duration;
import java.util.LinkedHashMap;
import java.util.Map;

/** Generation ingest export settings. */
public final class GenerationExportConfig {
    private GenerationExportProtocol protocol = GenerationExportProtocol.HTTP;
    private String endpoint = "http://localhost:8080/api/v1/generations:export";
    private final Map<String, String> headers = new LinkedHashMap<>();
    private AuthConfig auth = new AuthConfig();
    private boolean insecure = true;

    private int batchSize = 100;
    private Duration flushInterval = Duration.ofSeconds(1);
    private int queueSize = 2000;
    private int maxRetries = 5;
    private Duration initialBackoff = Duration.ofMillis(100);
    private Duration maxBackoff = Duration.ofSeconds(5);
    private int payloadMaxBytes = 4 << 20;

    public GenerationExportProtocol getProtocol() {
        return protocol;
    }

    public GenerationExportConfig setProtocol(GenerationExportProtocol protocol) {
        this.protocol = protocol == null ? GenerationExportProtocol.HTTP : protocol;
        return this;
    }

    public String getEndpoint() {
        return endpoint;
    }

    public GenerationExportConfig setEndpoint(String endpoint) {
        this.endpoint = endpoint == null ? "" : endpoint;
        return this;
    }

    public Map<String, String> getHeaders() {
        return headers;
    }

    public GenerationExportConfig setHeaders(Map<String, String> headers) {
        this.headers.clear();
        if (headers != null) {
            this.headers.putAll(headers);
        }
        return this;
    }

    public AuthConfig getAuth() {
        return auth;
    }

    public GenerationExportConfig setAuth(AuthConfig auth) {
        this.auth = auth == null ? new AuthConfig() : auth;
        return this;
    }

    public boolean isInsecure() {
        return insecure;
    }

    public GenerationExportConfig setInsecure(boolean insecure) {
        this.insecure = insecure;
        return this;
    }

    public int getBatchSize() {
        return batchSize;
    }

    public GenerationExportConfig setBatchSize(int batchSize) {
        this.batchSize = batchSize;
        return this;
    }

    public Duration getFlushInterval() {
        return flushInterval;
    }

    public GenerationExportConfig setFlushInterval(Duration flushInterval) {
        this.flushInterval = flushInterval == null ? Duration.ZERO : flushInterval;
        return this;
    }

    public int getQueueSize() {
        return queueSize;
    }

    public GenerationExportConfig setQueueSize(int queueSize) {
        this.queueSize = queueSize;
        return this;
    }

    public int getMaxRetries() {
        return maxRetries;
    }

    public GenerationExportConfig setMaxRetries(int maxRetries) {
        this.maxRetries = maxRetries;
        return this;
    }

    public Duration getInitialBackoff() {
        return initialBackoff;
    }

    public GenerationExportConfig setInitialBackoff(Duration initialBackoff) {
        this.initialBackoff = initialBackoff == null ? Duration.ZERO : initialBackoff;
        return this;
    }

    public Duration getMaxBackoff() {
        return maxBackoff;
    }

    public GenerationExportConfig setMaxBackoff(Duration maxBackoff) {
        this.maxBackoff = maxBackoff == null ? Duration.ZERO : maxBackoff;
        return this;
    }

    public int getPayloadMaxBytes() {
        return payloadMaxBytes;
    }

    public GenerationExportConfig setPayloadMaxBytes(int payloadMaxBytes) {
        this.payloadMaxBytes = payloadMaxBytes;
        return this;
    }

    public GenerationExportConfig copy() {
        return new GenerationExportConfig()
                .setProtocol(protocol)
                .setEndpoint(endpoint)
                .setHeaders(headers)
                .setAuth(auth.copy())
                .setInsecure(insecure)
                .setBatchSize(batchSize)
                .setFlushInterval(flushInterval)
                .setQueueSize(queueSize)
                .setMaxRetries(maxRetries)
                .setInitialBackoff(initialBackoff)
                .setMaxBackoff(maxBackoff)
                .setPayloadMaxBytes(payloadMaxBytes);
    }
}
