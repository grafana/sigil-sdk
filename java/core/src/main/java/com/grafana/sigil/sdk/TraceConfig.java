package com.grafana.sigil.sdk;

import java.util.LinkedHashMap;
import java.util.Map;

/** OTLP trace export settings. */
public final class TraceConfig {
    private TraceProtocol protocol = TraceProtocol.OTLP_HTTP;
    private String endpoint = "http://localhost:4318/v1/traces";
    private final Map<String, String> headers = new LinkedHashMap<>();
    private AuthConfig auth = new AuthConfig();
    private boolean insecure = true;

    public TraceProtocol getProtocol() {
        return protocol;
    }

    public TraceConfig setProtocol(TraceProtocol protocol) {
        this.protocol = protocol == null ? TraceProtocol.OTLP_HTTP : protocol;
        return this;
    }

    public String getEndpoint() {
        return endpoint;
    }

    public TraceConfig setEndpoint(String endpoint) {
        this.endpoint = endpoint == null ? "" : endpoint;
        return this;
    }

    public Map<String, String> getHeaders() {
        return headers;
    }

    public TraceConfig setHeaders(Map<String, String> headers) {
        this.headers.clear();
        if (headers != null) {
            this.headers.putAll(headers);
        }
        return this;
    }

    public AuthConfig getAuth() {
        return auth;
    }

    public TraceConfig setAuth(AuthConfig auth) {
        this.auth = auth == null ? new AuthConfig() : auth;
        return this;
    }

    public boolean isInsecure() {
        return insecure;
    }

    public TraceConfig setInsecure(boolean insecure) {
        this.insecure = insecure;
        return this;
    }

    public TraceConfig copy() {
        return new TraceConfig()
                .setProtocol(protocol)
                .setEndpoint(endpoint)
                .setHeaders(headers)
                .setAuth(auth.copy())
                .setInsecure(insecure);
    }
}
