package com.grafana.sigil.sdk;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.function.Function;
import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * Reads canonical {@code SIGIL_*} environment variables and layers them under
 * caller-supplied {@link SigilClientConfig} values.
 *
 * <p>Resolution order (highest precedence first):
 *
 * <ol>
 *   <li>Caller-supplied {@code SigilClientConfig} field (when not at its
 *       default/unset state).</li>
 *   <li>Canonical {@code SIGIL_*} env var.</li>
 *   <li>SDK schema default (the field initializer on
 *       {@link SigilClientConfig} / {@link GenerationExportConfig} /
 *       {@link AuthConfig}).</li>
 * </ol>
 *
 * <p>Mirrors the Go reference implementation in {@code go/sigil/env.go}.
 * Invalid env values are skipped with a warning so a single typo does not
 * discard the rest of the env layer.</p>
 */
public final class SigilEnvConfig {
    public static final String ENV_ENDPOINT = "SIGIL_ENDPOINT";
    public static final String ENV_PROTOCOL = "SIGIL_PROTOCOL";
    public static final String ENV_INSECURE = "SIGIL_INSECURE";
    public static final String ENV_HEADERS = "SIGIL_HEADERS";
    public static final String ENV_AUTH_MODE = "SIGIL_AUTH_MODE";
    public static final String ENV_AUTH_TENANT_ID = "SIGIL_AUTH_TENANT_ID";
    public static final String ENV_AUTH_TOKEN = "SIGIL_AUTH_TOKEN";
    public static final String ENV_AGENT_NAME = "SIGIL_AGENT_NAME";
    public static final String ENV_AGENT_VERSION = "SIGIL_AGENT_VERSION";
    public static final String ENV_USER_ID = "SIGIL_USER_ID";
    public static final String ENV_TAGS = "SIGIL_TAGS";
    public static final String ENV_CONTENT_CAPTURE_MODE = "SIGIL_CONTENT_CAPTURE_MODE";
    public static final String ENV_DEBUG = "SIGIL_DEBUG";

    private SigilEnvConfig() {
    }

    /** Result of {@link #resolveFromEnv} — resolved config plus any non-fatal warnings. */
    public record EnvResolveResult(SigilClientConfig config, List<String> warnings) {
        public EnvResolveResult {
            warnings = warnings == null ? List.of() : List.copyOf(warnings);
        }
    }

    /**
     * Returns a config built from process env vars layered onto a fresh
     * {@link SigilClientConfig}. Convenience helper; most callers should let
     * {@link SigilClient} construction perform the same resolution internally.
     * Warnings for invalid env values are logged to {@code com.grafana.sigil.sdk}.
     */
    public static SigilClientConfig fromEnv() {
        EnvResolveResult result = resolveFromEnv(System::getenv, new SigilClientConfig());
        logWarnings(Logger.getLogger("com.grafana.sigil.sdk"), result.warnings());
        return result.config();
    }

    /**
     * Applies canonical {@code SIGIL_*} env values onto {@code base},
     * preserving caller-supplied fields. The returned config is a fresh copy;
     * {@code base} is not mutated.
     *
     * <p>Invalid {@code SIGIL_AUTH_MODE}, {@code SIGIL_PROTOCOL}, or
     * {@code SIGIL_CONTENT_CAPTURE_MODE} values are skipped — the base value
     * is kept and the warning is appended to the result so other valid env
     * vars still apply.</p>
     */
    public static EnvResolveResult resolveFromEnv(Function<String, String> lookup, SigilClientConfig base) {
        Function<String, String> source = lookup == null ? System::getenv : lookup;
        SigilClientConfig cfg = base == null ? new SigilClientConfig() : base.copy();
        List<String> warnings = new ArrayList<>();

        GenerationExportConfig export = cfg.getGenerationExport();

        String endpoint = envTrimmed(source, ENV_ENDPOINT);
        if (endpoint != null && export.getEndpoint().isEmpty()) {
            export.setEndpoint(endpoint);
        }

        String protocol = envTrimmed(source, ENV_PROTOCOL);
        if (protocol != null && export.getProtocol() == null) {
            GenerationExportProtocol parsed = parseProtocol(protocol);
            if (parsed != null) {
                export.setProtocol(parsed);
            } else {
                warnings.add("sigil: ignoring invalid " + ENV_PROTOCOL + " " + protocol);
            }
        }

        String insecureRaw = envTrimmed(source, ENV_INSECURE);
        if (insecureRaw != null && export.getInsecure() == null) {
            export.setInsecure(parseBool(insecureRaw));
        }

        String headersRaw = envTrimmed(source, ENV_HEADERS);
        if (headersRaw != null && export.getHeaders().isEmpty()) {
            export.setHeaders(parseCsvKv(headersRaw));
        }

        AuthConfig auth = export.getAuth();
        String authModeRaw = envTrimmed(source, ENV_AUTH_MODE);
        if (authModeRaw != null && auth.getMode() == null) {
            AuthMode parsed = parseAuthMode(authModeRaw);
            if (parsed != null) {
                auth.setMode(parsed);
            } else {
                warnings.add("sigil: ignoring invalid " + ENV_AUTH_MODE + " " + authModeRaw);
            }
        }

        String tenantId = envTrimmed(source, ENV_AUTH_TENANT_ID);
        if (tenantId != null && auth.getTenantId().isEmpty()) {
            auth.setTenantId(tenantId);
        }

        String token = envTrimmed(source, ENV_AUTH_TOKEN);
        if (token != null) {
            // Set both fields when empty; AuthHeaders.resolve uses only the one
            // matching the final mode. Lets env's token populate a caller-set
            // mode without env declaring SIGIL_AUTH_MODE.
            if (auth.getBearerToken().isEmpty()) {
                auth.setBearerToken(token);
            }
            if (auth.getBasicPassword().isEmpty()) {
                auth.setBasicPassword(token);
            }
        }
        if (auth.getMode() == AuthMode.BASIC && auth.getBasicUser().isEmpty() && !auth.getTenantId().isEmpty()) {
            auth.setBasicUser(auth.getTenantId());
        }

        // Finalize tri-state defaults after env layering: null/empty means
        // "no caller value and no env value", so apply the schema default.
        if (export.getProtocol() == null) {
            export.setProtocol(GenerationExportProtocol.HTTP);
        }
        if (export.getEndpoint().isEmpty()) {
            export.setEndpoint("http://localhost:8080/api/v1/generations:export");
        }
        if (auth.getMode() == null) {
            auth.setMode(AuthMode.NONE);
        }

        String agentName = envTrimmed(source, ENV_AGENT_NAME);
        if (agentName != null && cfg.getAgentName().isEmpty()) {
            cfg.setAgentName(agentName);
        }
        String agentVersion = envTrimmed(source, ENV_AGENT_VERSION);
        if (agentVersion != null && cfg.getAgentVersion().isEmpty()) {
            cfg.setAgentVersion(agentVersion);
        }
        String userId = envTrimmed(source, ENV_USER_ID);
        if (userId != null && cfg.getUserId().isEmpty()) {
            cfg.setUserId(userId);
        }

        String tagsRaw = envTrimmed(source, ENV_TAGS);
        if (tagsRaw != null) {
            Map<String, String> envTags = parseCsvKv(tagsRaw);
            // Env tags act as a base layer; caller tags win on collision.
            Map<String, String> merged = new LinkedHashMap<>(envTags);
            merged.putAll(cfg.getTags());
            cfg.setTags(merged);
        }

        String ccmRaw = envTrimmed(source, ENV_CONTENT_CAPTURE_MODE);
        if (ccmRaw != null && cfg.getContentCapture() == ContentCaptureMode.DEFAULT) {
            ContentCaptureMode parsed = parseContentCaptureMode(ccmRaw);
            if (parsed != null) {
                cfg.setContentCapture(parsed);
            } else {
                warnings.add("sigil: ignoring invalid " + ENV_CONTENT_CAPTURE_MODE + " " + ccmRaw);
            }
        }

        String debugRaw = envTrimmed(source, ENV_DEBUG);
        if (debugRaw != null && cfg.getDebug() == null) {
            cfg.setDebug(parseBool(debugRaw));
        }

        return new EnvResolveResult(cfg, warnings);
    }

    /** Logs each warning at WARNING level on {@code logger} (no-op when null). */
    public static void logWarnings(Logger logger, List<String> warnings) {
        if (logger == null || warnings == null) {
            return;
        }
        for (String w : warnings) {
            logger.log(Level.WARNING, w);
        }
    }

    private static String envTrimmed(Function<String, String> lookup, String key) {
        String raw;
        try {
            raw = lookup.apply(key);
        } catch (SecurityException ex) {
            return null;
        }
        if (raw == null) {
            return null;
        }
        String v = raw.trim();
        return v.isEmpty() ? null : v;
    }

    static boolean parseBool(String raw) {
        if (raw == null) {
            return false;
        }
        switch (raw.trim().toLowerCase(Locale.ROOT)) {
            case "1":
            case "true":
            case "yes":
            case "on":
                return true;
            default:
                return false;
        }
    }

    static Map<String, String> parseCsvKv(String raw) {
        Map<String, String> out = new LinkedHashMap<>();
        if (raw == null) {
            return out;
        }
        for (String part : raw.split(",", -1)) {
            String trimmed = part.trim();
            if (trimmed.isEmpty()) {
                continue;
            }
            int idx = trimmed.indexOf('=');
            if (idx <= 0) {
                continue;
            }
            String key = trimmed.substring(0, idx).trim();
            String value = trimmed.substring(idx + 1).trim();
            if (!key.isEmpty()) {
                out.put(key, value);
            }
        }
        return out;
    }

    static ContentCaptureMode parseContentCaptureMode(String raw) {
        if (raw == null) {
            return null;
        }
        switch (raw.trim().toLowerCase(Locale.ROOT)) {
            case "full":
                return ContentCaptureMode.FULL;
            case "no_tool_content":
                return ContentCaptureMode.NO_TOOL_CONTENT;
            case "metadata_only":
                return ContentCaptureMode.METADATA_ONLY;
            default:
                return null;
        }
    }

    static AuthMode parseAuthMode(String raw) {
        if (raw == null) {
            return null;
        }
        switch (raw.trim().toLowerCase(Locale.ROOT)) {
            case "none":
                return AuthMode.NONE;
            case "tenant":
                return AuthMode.TENANT;
            case "bearer":
                return AuthMode.BEARER;
            case "basic":
                return AuthMode.BASIC;
            default:
                return null;
        }
    }

    static GenerationExportProtocol parseProtocol(String raw) {
        if (raw == null) {
            return null;
        }
        switch (raw.trim().toLowerCase(Locale.ROOT)) {
            case "grpc":
                return GenerationExportProtocol.GRPC;
            case "http":
                return GenerationExportProtocol.HTTP;
            case "none":
                return GenerationExportProtocol.NONE;
            default:
                return null;
        }
    }

}
