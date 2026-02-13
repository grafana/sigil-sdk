package com.grafana.sigil.sdk;

/** Per-export authentication settings. */
public final class AuthConfig {
    private AuthMode mode = AuthMode.NONE;
    private String tenantId = "";
    private String bearerToken = "";

    public AuthMode getMode() {
        return mode;
    }

    public AuthConfig setMode(AuthMode mode) {
        this.mode = mode == null ? AuthMode.NONE : mode;
        return this;
    }

    public String getTenantId() {
        return tenantId;
    }

    public AuthConfig setTenantId(String tenantId) {
        this.tenantId = tenantId == null ? "" : tenantId;
        return this;
    }

    public String getBearerToken() {
        return bearerToken;
    }

    public AuthConfig setBearerToken(String bearerToken) {
        this.bearerToken = bearerToken == null ? "" : bearerToken;
        return this;
    }

    public AuthConfig copy() {
        return new AuthConfig().setMode(mode).setTenantId(tenantId).setBearerToken(bearerToken);
    }
}
