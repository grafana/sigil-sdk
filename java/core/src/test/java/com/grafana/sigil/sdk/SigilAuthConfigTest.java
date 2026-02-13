package com.grafana.sigil.sdk;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.util.Map;
import org.junit.jupiter.api.Test;

class SigilAuthConfigTest {
    @Test
    void validatesAuthModeShape() {
        assertThatThrownBy(() -> AuthHeaders.resolve(Map.of(), new AuthConfig().setMode(AuthMode.NONE).setTenantId("x"), "trace"))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("mode 'none'");

        assertThatThrownBy(() -> AuthHeaders.resolve(Map.of(), new AuthConfig().setMode(AuthMode.TENANT), "generation export"))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("requires tenantId");

        assertThatThrownBy(() -> AuthHeaders.resolve(Map.of(), new AuthConfig().setMode(AuthMode.BEARER), "generation export"))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("requires bearerToken");
    }

    @Test
    void explicitHeadersOverrideInjectedAuthHeaders() {
        Map<String, String> trace = AuthHeaders.resolve(
                Map.of("Authorization", "Bearer override"),
                new AuthConfig().setMode(AuthMode.BEARER).setBearerToken("injected"),
                "trace");
        assertThat(trace.get("Authorization")).isEqualTo("Bearer override");

        Map<String, String> generation = AuthHeaders.resolve(
                Map.of("x-scope-orgid", "tenant-override"),
                new AuthConfig().setMode(AuthMode.TENANT).setTenantId("tenant-injected"),
                "generation export");
        assertThat(generation.get("x-scope-orgid")).isEqualTo("tenant-override");
    }
}
