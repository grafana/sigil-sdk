package com.grafana.sigil.sdk;

import java.util.LinkedHashMap;
import java.util.Map;

final class AuthHeaders {
    static final String TENANT_HEADER = "X-Scope-OrgID";
    static final String AUTHORIZATION_HEADER = "Authorization";

    private AuthHeaders() {
    }

    static Map<String, String> resolve(Map<String, String> headers, AuthConfig auth, String label) {
        Map<String, String> out = new LinkedHashMap<>();
        if (headers != null) {
            out.putAll(headers);
        }

        AuthMode mode = auth == null || auth.getMode() == null ? AuthMode.NONE : auth.getMode();
        String tenantId = auth == null ? "" : auth.getTenantId().trim();
        String bearer = auth == null ? "" : auth.getBearerToken().trim();

        if (mode == AuthMode.NONE) {
            if (!tenantId.isEmpty() || !bearer.isEmpty()) {
                throw new IllegalArgumentException(label + " auth mode 'none' does not allow tenantId or bearerToken");
            }
            return out;
        }

        if (mode == AuthMode.TENANT) {
            if (tenantId.isEmpty()) {
                throw new IllegalArgumentException(label + " auth mode 'tenant' requires tenantId");
            }
            if (!bearer.isEmpty()) {
                throw new IllegalArgumentException(label + " auth mode 'tenant' does not allow bearerToken");
            }
            if (!hasHeader(out, TENANT_HEADER)) {
                out.put(TENANT_HEADER, tenantId);
            }
            return out;
        }

        if (mode == AuthMode.BEARER) {
            if (bearer.isEmpty()) {
                throw new IllegalArgumentException(label + " auth mode 'bearer' requires bearerToken");
            }
            if (!tenantId.isEmpty()) {
                throw new IllegalArgumentException(label + " auth mode 'bearer' does not allow tenantId");
            }
            if (!hasHeader(out, AUTHORIZATION_HEADER)) {
                out.put(AUTHORIZATION_HEADER, formatBearer(bearer));
            }
            return out;
        }

        throw new IllegalArgumentException("unsupported " + label + " auth mode " + mode);
    }

    private static boolean hasHeader(Map<String, String> headers, String key) {
        String target = key.toLowerCase();
        return headers.keySet().stream().anyMatch(existing -> existing != null && existing.toLowerCase().equals(target));
    }

    private static String formatBearer(String token) {
        String value = token.trim();
        if (value.regionMatches(true, 0, "Bearer ", 0, 7)) {
            value = value.substring(7).trim();
        }
        return "Bearer " + value;
    }
}
