package com.grafana.sigil.sdk;

import static org.assertj.core.api.Assertions.assertThat;

import com.grafana.sigil.sdk.SigilEnvConfig.EnvResolveResult;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.function.Function;
import org.junit.jupiter.api.Test;

class SigilEnvConfigTest {

    private static Function<String, String> mapLookup(Map<String, String> env) {
        return env::get;
    }

    private static EnvResolveResult resolve(Map<String, String> env) {
        return SigilEnvConfig.resolveFromEnv(mapLookup(env), new SigilClientConfig());
    }

    @Test
    void noEnvKeepsBaseDefaults() {
        EnvResolveResult result = resolve(Map.of());
        SigilClientConfig cfg = result.config();
        assertThat(cfg.getAgentName()).isEmpty();
        assertThat(cfg.getAgentVersion()).isEmpty();
        assertThat(cfg.getUserId()).isEmpty();
        assertThat(cfg.getTags()).isEmpty();
        assertThat(cfg.getDebug()).isNull();
        assertThat(cfg.getGenerationExport().getInsecure()).isNull();
        assertThat(cfg.getGenerationExport().getEndpoint())
                .isEqualTo("http://localhost:8080");
        assertThat(result.warnings()).isEmpty();
    }

    @Test
    void transportFromEnv() {
        Map<String, String> env = Map.of(
                "SIGIL_ENDPOINT", "https://env:4318",
                "SIGIL_PROTOCOL", "http",
                "SIGIL_INSECURE", "true",
                "SIGIL_HEADERS", "X-A=1,X-B=two");
        SigilClientConfig cfg = resolve(env).config();
        assertThat(cfg.getGenerationExport().getEndpoint()).isEqualTo("https://env:4318");
        assertThat(cfg.getGenerationExport().getProtocol()).isEqualTo(GenerationExportProtocol.HTTP);
        assertThat(cfg.getGenerationExport().getInsecure()).isTrue();
        assertThat(cfg.getGenerationExport().getHeaders())
                .containsEntry("X-A", "1")
                .containsEntry("X-B", "two");
    }

    @Test
    void grpcProtocolFromEnv() {
        SigilClientConfig cfg = resolve(Map.of("SIGIL_PROTOCOL", "grpc")).config();
        assertThat(cfg.getGenerationExport().getProtocol()).isEqualTo(GenerationExportProtocol.GRPC);
    }

    @Test
    void basicAuthFromEnv() {
        Map<String, String> env = Map.of(
                "SIGIL_AUTH_MODE", "basic",
                "SIGIL_AUTH_TENANT_ID", "42",
                "SIGIL_AUTH_TOKEN", "glc_xxx");
        AuthConfig auth = resolve(env).config().getGenerationExport().getAuth();
        assertThat(auth.getMode()).isEqualTo(AuthMode.BASIC);
        assertThat(auth.getTenantId()).isEqualTo("42");
        assertThat(auth.getBasicPassword()).isEqualTo("glc_xxx");
        assertThat(auth.getBasicUser()).isEqualTo("42");
    }

    @Test
    void bearerAuthFromEnv() {
        Map<String, String> env = Map.of(
                "SIGIL_AUTH_MODE", "bearer",
                "SIGIL_AUTH_TOKEN", "tok");
        AuthConfig auth = resolve(env).config().getGenerationExport().getAuth();
        assertThat(auth.getMode()).isEqualTo(AuthMode.BEARER);
        assertThat(auth.getBearerToken()).isEqualTo("tok");
    }

    @Test
    void invalidAuthModeWarnsAndPreservesOtherEnv() {
        Map<String, String> env = new HashMap<>();
        env.put("SIGIL_AUTH_MODE", "Bearrer");
        env.put("SIGIL_ENDPOINT", "valid.example:4318");
        env.put("SIGIL_AGENT_NAME", "valid-agent");
        env.put("SIGIL_USER_ID", "alice");
        EnvResolveResult result = SigilEnvConfig.resolveFromEnv(env::get, new SigilClientConfig());

        SigilClientConfig cfg = result.config();
        assertThat(cfg.getGenerationExport().getEndpoint()).isEqualTo("valid.example:4318");
        assertThat(cfg.getAgentName()).isEqualTo("valid-agent");
        assertThat(cfg.getUserId()).isEqualTo("alice");
        assertThat(cfg.getGenerationExport().getAuth().getMode()).isEqualTo(AuthMode.NONE);
        assertThat(result.warnings()).anySatisfy(w -> assertThat(w).contains("SIGIL_AUTH_MODE"));
    }

    @Test
    void invalidContentCaptureModeWarnsAndPreservesOtherEnv() {
        Map<String, String> env = new HashMap<>();
        env.put("SIGIL_CONTENT_CAPTURE_MODE", "bogus");
        env.put("SIGIL_ENDPOINT", "valid.example:4318");
        EnvResolveResult result = SigilEnvConfig.resolveFromEnv(env::get, new SigilClientConfig());

        SigilClientConfig cfg = result.config();
        assertThat(cfg.getContentCapture()).isEqualTo(ContentCaptureMode.DEFAULT);
        assertThat(cfg.getGenerationExport().getEndpoint()).isEqualTo("valid.example:4318");
        assertThat(result.warnings()).anySatisfy(w -> assertThat(w).contains("SIGIL_CONTENT_CAPTURE_MODE"));
    }

    @Test
    void invalidContentCaptureModeKeepsCallerBaseValue() {
        SigilClientConfig base = new SigilClientConfig().setContentCapture(ContentCaptureMode.METADATA_ONLY);
        EnvResolveResult result = SigilEnvConfig.resolveFromEnv(
                Map.of("SIGIL_CONTENT_CAPTURE_MODE", "bogus")::get, base);
        // Base value should remain because it isn't DEFAULT (caller-set).
        assertThat(result.config().getContentCapture()).isEqualTo(ContentCaptureMode.METADATA_ONLY);
    }

    @Test
    void contentCaptureModeFromEnv() {
        SigilClientConfig cfg = resolve(Map.of("SIGIL_CONTENT_CAPTURE_MODE", "metadata_only")).config();
        assertThat(cfg.getContentCapture()).isEqualTo(ContentCaptureMode.METADATA_ONLY);
    }

    @Test
    void agentUserTagsDebugFromEnv() {
        Map<String, String> env = Map.of(
                "SIGIL_AGENT_NAME", "planner",
                "SIGIL_AGENT_VERSION", "1.2.3",
                "SIGIL_USER_ID", "alice@example.com",
                "SIGIL_TAGS", "service=orchestrator,env=prod",
                "SIGIL_DEBUG", "true");
        SigilClientConfig cfg = resolve(env).config();
        assertThat(cfg.getAgentName()).isEqualTo("planner");
        assertThat(cfg.getAgentVersion()).isEqualTo("1.2.3");
        assertThat(cfg.getUserId()).isEqualTo("alice@example.com");
        assertThat(cfg.getTags()).containsEntry("service", "orchestrator").containsEntry("env", "prod");
        assertThat(cfg.getDebug()).isTrue();
    }

    @Test
    void whitespaceOnlyValuesAreIgnored() {
        Map<String, String> env = new HashMap<>();
        env.put("SIGIL_TAGS", "   ");
        env.put("SIGIL_AGENT_NAME", "");
        env.put("SIGIL_USER_ID", "\t \n");
        SigilClientConfig cfg = SigilEnvConfig.resolveFromEnv(env::get, new SigilClientConfig()).config();
        assertThat(cfg.getTags()).isEmpty();
        assertThat(cfg.getAgentName()).isEmpty();
        assertThat(cfg.getUserId()).isEmpty();
    }

    @Test
    void callerEndpointWinsOverEnv() {
        SigilClientConfig base = new SigilClientConfig();
        base.getGenerationExport().setEndpoint("https://caller-host");
        SigilClientConfig cfg = SigilEnvConfig.resolveFromEnv(
                Map.of("SIGIL_ENDPOINT", "https://env-host")::get, base).config();
        assertThat(cfg.getGenerationExport().getEndpoint()).isEqualTo("https://caller-host");
    }

    @Test
    void callerInsecureFalseBeatsEnvTrue() {
        SigilClientConfig base = new SigilClientConfig();
        base.getGenerationExport().setInsecure(Boolean.FALSE);
        SigilClientConfig cfg = SigilEnvConfig.resolveFromEnv(
                Map.of("SIGIL_INSECURE", "true")::get, base).config();
        assertThat(cfg.getGenerationExport().getInsecure()).isFalse();
        assertThat(cfg.getGenerationExport().isInsecureResolved()).isFalse();
    }

    @Test
    void envInsecureTrueLayersUnderUnsetCaller() {
        SigilClientConfig cfg = SigilEnvConfig.resolveFromEnv(
                Map.of("SIGIL_INSECURE", "true")::get, new SigilClientConfig()).config();
        assertThat(cfg.getGenerationExport().getInsecure()).isTrue();
        assertThat(cfg.getGenerationExport().isInsecureResolved()).isTrue();
    }

    @Test
    void unsetInsecureResolvesToFalse() {
        // SIGIL_INSECURE unset and caller didn't set → resolved boolean is false (TLS on).
        SigilClientConfig cfg = SigilEnvConfig.resolveFromEnv(Map.<String, String>of()::get, new SigilClientConfig())
                .config();
        assertThat(cfg.getGenerationExport().getInsecure()).isNull();
        assertThat(cfg.getGenerationExport().isInsecureResolved()).isFalse();
    }

    @Test
    void authTokenFillsBothBearerAndBasicWhenEmpty() {
        SigilClientConfig cfg = SigilEnvConfig.resolveFromEnv(
                Map.of("SIGIL_AUTH_TOKEN", "secret")::get, new SigilClientConfig()).config();
        AuthConfig auth = cfg.getGenerationExport().getAuth();
        assertThat(auth.getBearerToken()).isEqualTo("secret");
        assertThat(auth.getBasicPassword()).isEqualTo("secret");
    }

    @Test
    void authTokenSkipsAlreadyFilledFields() {
        SigilClientConfig base = new SigilClientConfig();
        base.getGenerationExport().getAuth().setBearerToken("caller-bearer");
        SigilClientConfig cfg = SigilEnvConfig.resolveFromEnv(
                Map.of("SIGIL_AUTH_TOKEN", "env-token")::get, base).config();
        AuthConfig auth = cfg.getGenerationExport().getAuth();
        assertThat(auth.getBearerToken()).isEqualTo("caller-bearer");
        assertThat(auth.getBasicPassword()).isEqualTo("env-token");
    }

    @Test
    void basicModeUsesTenantAsBasicUserFallback() {
        Map<String, String> env = Map.of(
                "SIGIL_AUTH_MODE", "basic",
                "SIGIL_AUTH_TENANT_ID", "tenant-a",
                "SIGIL_AUTH_TOKEN", "secret");
        AuthConfig auth = resolve(env).config().getGenerationExport().getAuth();
        assertThat(auth.getMode()).isEqualTo(AuthMode.BASIC);
        assertThat(auth.getBasicUser()).isEqualTo("tenant-a");
        assertThat(auth.getBasicPassword()).isEqualTo("secret");
    }

    @Test
    void strayTenantIdKeepsModeNone() {
        AuthConfig auth = resolve(Map.of("SIGIL_AUTH_TENANT_ID", "42"))
                .config().getGenerationExport().getAuth();
        assertThat(auth.getMode()).isEqualTo(AuthMode.NONE);
        assertThat(auth.getTenantId()).isEqualTo("42");
    }

    @Test
    void parseCsvKvHandlesEdgeCases() {
        Map<String, String> result = SigilEnvConfig.parseCsvKv("a=1, b = two ,, =skip,c=");
        assertThat(result).containsExactlyInAnyOrderEntriesOf(Map.of(
                "a", "1",
                "b", "two",
                "c", ""));
    }

    @Test
    void envTagsMergeUnderCallerTags() {
        SigilClientConfig base = new SigilClientConfig();
        base.setTags(Map.of("team", "ai", "env", "staging"));
        SigilClientConfig cfg = SigilEnvConfig.resolveFromEnv(
                Map.of("SIGIL_TAGS", "service=orch,env=prod")::get, base).config();
        Map<String, String> tags = cfg.getTags();
        assertThat(tags).containsEntry("service", "orch");      // env-only fills
        assertThat(tags).containsEntry("team", "ai");           // caller-only preserved
        assertThat(tags).containsEntry("env", "staging");       // caller wins on collision
    }

    @Test
    void parseBoolAcceptsCanonicalTrue() {
        assertThat(SigilEnvConfig.parseBool("1")).isTrue();
        assertThat(SigilEnvConfig.parseBool("true")).isTrue();
        assertThat(SigilEnvConfig.parseBool("YES")).isTrue();
        assertThat(SigilEnvConfig.parseBool("On")).isTrue();
        assertThat(SigilEnvConfig.parseBool("0")).isFalse();
        assertThat(SigilEnvConfig.parseBool("false")).isFalse();
        assertThat(SigilEnvConfig.parseBool("garbage")).isFalse();
    }

    @Test
    void fromEnvUsesProcessEnv() {
        // Smoke: just verify it doesn't throw and returns a config.
        SigilClientConfig cfg = SigilEnvConfig.fromEnv();
        assertThat(cfg).isNotNull();
    }

    @Test
    void resolveDoesNotMutateBase() {
        SigilClientConfig base = new SigilClientConfig().setAgentName("base-agent");
        SigilClientConfig resolved = SigilEnvConfig.resolveFromEnv(
                Map.of("SIGIL_USER_ID", "alice")::get, base).config();
        assertThat(base.getUserId()).isEmpty();
        assertThat(resolved.getUserId()).isEqualTo("alice");
        assertThat(resolved.getAgentName()).isEqualTo("base-agent");
    }

    @Test
    void parseAuthModeNormalizesCase() {
        assertThat(SigilEnvConfig.parseAuthMode("BASIC")).isEqualTo(AuthMode.BASIC);
        assertThat(SigilEnvConfig.parseAuthMode("Bearer")).isEqualTo(AuthMode.BEARER);
        assertThat(SigilEnvConfig.parseAuthMode("none")).isEqualTo(AuthMode.NONE);
        assertThat(SigilEnvConfig.parseAuthMode("garbage")).isNull();
    }

    @Test
    void fluentSetTagsCopiesInput() {
        Map<String, String> source = new LinkedHashMap<>();
        source.put("k", "v");
        SigilClientConfig cfg = new SigilClientConfig().setTags(source);
        source.put("late", "edit");
        assertThat(cfg.getTags()).containsExactly(Map.entry("k", "v"));
    }
}
