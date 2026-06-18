package com.grafana.sigil.sdk;

import java.nio.charset.StandardCharsets;
import java.util.List;
import java.util.Locale;
import java.util.function.Function;
import java.util.logging.Level;
import java.util.logging.Logger;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/** Built-in local secret redaction for generation exports. */
public final class SecretRedaction {
    static final String ENV_REDACT_INPUT_MESSAGES = "SIGIL_REDACT_INPUT_MESSAGES";

    private static final List<SecretPattern> TIER1_PATTERNS = List.of(
            new SecretPattern("grafana-cloud-token", Pattern.compile("\\bglc_[A-Za-z0-9_-]{20,}")),
            new SecretPattern("grafana-service-account-token", Pattern.compile("\\bglsa_[A-Za-z0-9_-]{20,}")),
            new SecretPattern("aws-access-token", Pattern.compile("\\b(?:A3T[A-Z0-9]|AKIA|ASIA|ABIA|ACCA)[A-Z2-7]{16}\\b")),
            new SecretPattern("github-pat", Pattern.compile("\\bghp_[A-Za-z0-9_]{36,}")),
            new SecretPattern("github-oauth", Pattern.compile("\\bgho_[A-Za-z0-9_]{36,}")),
            new SecretPattern("github-app-token", Pattern.compile("\\bghs_[A-Za-z0-9_]{36,}")),
            new SecretPattern("github-fine-grained-pat", Pattern.compile("\\bgithub_pat_[A-Za-z0-9_]{82}")),
            new SecretPattern("anthropic-api-key", Pattern.compile("\\bsk-ant-api03-[a-zA-Z0-9_-]{93}AA")),
            new SecretPattern("anthropic-admin-key", Pattern.compile("\\bsk-ant-admin01-[a-zA-Z0-9_-]{93}AA")),
            new SecretPattern("openai-api-key", Pattern.compile("\\bsk-[a-zA-Z0-9]{20}T3BlbkFJ[a-zA-Z0-9]{20}")),
            new SecretPattern("openai-project-key", Pattern.compile("\\bsk-proj-[a-zA-Z0-9_-]{40,}")),
            new SecretPattern("openai-svcacct-key", Pattern.compile("\\bsk-svcacct-[a-zA-Z0-9_-]{40,}")),
            new SecretPattern("gcp-api-key", Pattern.compile("\\bAIza[A-Za-z0-9_-]{35}")),
            new SecretPattern("private-key", Pattern.compile("-----BEGIN[A-Z ]*PRIVATE KEY-----[\\s\\S]*?-----END[A-Z ]*PRIVATE KEY-----")),
            new SecretPattern("connection-string", Pattern.compile("(?:postgres|mysql|mongodb|redis|amqp)://[^\\s'\"]+@[^\\s'\"]+")),
            new SecretPattern("bearer-token", Pattern.compile("[Bb]earer\\s+[A-Za-z0-9_.\\-~+/]{20,}={0,3}")),
            new SecretPattern("slack-token", Pattern.compile("\\bxox[bporas]-[A-Za-z0-9-]{10,}")),
            new SecretPattern("stripe-key", Pattern.compile("\\b[sr]k_(?:live|test)_[A-Za-z0-9]{20,}")),
            new SecretPattern("sendgrid-api-key", Pattern.compile("\\bSG\\.[A-Za-z0-9_-]{22}\\.[A-Za-z0-9_-]{43}")),
            new SecretPattern("twilio-api-key", Pattern.compile("\\bSK[a-f0-9]{32}")),
            new SecretPattern("npm-token", Pattern.compile("\\bnpm_[A-Za-z0-9]{36}")),
            new SecretPattern("pypi-token", Pattern.compile("\\bpypi-[A-Za-z0-9_-]{50,}")));

    private static final SecretPattern EMAIL_PATTERN = new SecretPattern(
            "email",
            Pattern.compile("\\b[A-Z0-9._%+\\-]+@[A-Z0-9.\\-]+\\.[A-Z]{2,}\\b", Pattern.CASE_INSENSITIVE));
    private static final SecretPattern ENV_SECRET_PATTERN = new SecretPattern(
            "env-secret-value",
            Pattern.compile("((?:PASSWORD|SECRET|TOKEN|KEY|CREDENTIAL|API_KEY|PRIVATE_KEY|ACCESS_KEY)\\s*[=:]\\s*)([^\\s\"{}\\[\\],]+)", Pattern.CASE_INSENSITIVE));

    private SecretRedaction() {
    }

    /** Returns a sanitizer that redacts known secret formats before generation export. */
    public static GenerationSanitizer createSecretRedactionSanitizer() {
        return createSecretRedactionSanitizer(new SecretRedactionOptions());
    }

    /** Returns a sanitizer that redacts known secret formats before generation export. */
    public static GenerationSanitizer createSecretRedactionSanitizer(SecretRedactionOptions options) {
        return createSecretRedactionSanitizer(options, System::getenv, Logger.getLogger("com.grafana.sigil.sdk"));
    }

    static GenerationSanitizer createSecretRedactionSanitizer(
            SecretRedactionOptions options, Function<String, String> lookup, Logger logger) {
        SecretRedactionOptions opts = options == null ? new SecretRedactionOptions() : options;
        boolean includeEmail = opts.getRedactEmailAddresses() == null || opts.getRedactEmailAddresses();
        boolean redactInputMessages = resolveRedactInputMessages(opts.getRedactInputMessages(), lookup, logger);
        SecretRedactor redactor = new SecretRedactor(includeEmail);

        return generation -> {
            Generation sanitized = generation == null ? new Generation() : generation.copy();
            if (!sanitized.getSystemPrompt().isEmpty()) {
                sanitized.setSystemPrompt(redactor.redactFull(sanitized.getSystemPrompt()));
            }
            if (!sanitized.getConversationTitle().isEmpty()) {
                sanitized.setConversationTitle(redactor.redactLight(sanitized.getConversationTitle()));
            }
            if (!sanitized.getCallError().isEmpty()) {
                sanitized.setCallError(redactor.redactLight(sanitized.getCallError()));
            }

            for (Message message : sanitized.getInput()) {
                sanitizeMessage(message, inputTextMode(message.getRole(), redactInputMessages), redactor);
            }
            for (Message message : sanitized.getOutput()) {
                sanitizeMessage(message, outputTextMode(message.getRole()), redactor);
            }
            return sanitized;
        };
    }

    static boolean resolveRedactInputMessages(Boolean explicit, Function<String, String> lookup, Logger logger) {
        if (explicit != null) {
            return explicit;
        }
        Function<String, String> source = lookup == null ? System::getenv : lookup;
        String raw;
        try {
            raw = source.apply(ENV_REDACT_INPUT_MESSAGES);
        } catch (SecurityException ex) {
            return false;
        }
        if (raw == null || raw.trim().isEmpty()) {
            return false;
        }
        Boolean parsed = parseStrictBool(raw);
        if (parsed != null) {
            return parsed;
        }
        if (logger != null) {
            logger.log(Level.WARNING, "sigil: ignoring invalid " + ENV_REDACT_INPUT_MESSAGES + " " + raw.trim());
        }
        return false;
    }

    private static Boolean parseStrictBool(String raw) {
        String token = raw.trim().toLowerCase(Locale.ROOT);
        return switch (token) {
            case "1", "true", "yes", "on" -> true;
            case "0", "false", "no", "off" -> false;
            default -> null;
        };
    }

    private static TextMode inputTextMode(MessageRole role, boolean redactInputMessages) {
        return switch (role) {
            case USER -> redactInputMessages ? TextMode.FULL : TextMode.SKIP;
            case TOOL -> TextMode.FULL;
            case ASSISTANT -> TextMode.LIGHT;
            default -> TextMode.SKIP;
        };
    }

    private static TextMode outputTextMode(MessageRole role) {
        return switch (role) {
            case ASSISTANT -> TextMode.LIGHT;
            case TOOL -> TextMode.FULL;
            default -> TextMode.SKIP;
        };
    }

    private static void sanitizeMessage(Message message, TextMode mode, SecretRedactor redactor) {
        if (message == null || mode == TextMode.SKIP) {
            return;
        }
        for (MessagePart part : message.getParts()) {
            sanitizePart(part, mode, redactor);
        }
    }

    private static void sanitizePart(MessagePart part, TextMode mode, SecretRedactor redactor) {
        if (part == null) {
            return;
        }
        switch (part.getKind()) {
            case TEXT -> part.setText(redactString(part.getText(), mode, redactor));
            case THINKING -> part.setThinking(redactor.redactLight(part.getThinking()));
            case TOOL_CALL -> {
                ToolCall toolCall = part.getToolCall();
                if (toolCall != null && toolCall.getInputJson().length > 0) {
                    toolCall.setInputJson(redactor.redactFullBytes(toolCall.getInputJson()));
                }
            }
            case TOOL_RESULT -> {
                ToolResultPart toolResult = part.getToolResult();
                if (toolResult != null) {
                    if (!toolResult.getContent().isEmpty()) {
                        toolResult.setContent(redactor.redactFull(toolResult.getContent()));
                    }
                    if (toolResult.getContentJson().length > 0) {
                        toolResult.setContentJson(redactor.redactFullBytes(toolResult.getContentJson()));
                    }
                }
            }
        }
    }

    private static String redactString(String value, TextMode mode, SecretRedactor redactor) {
        return switch (mode) {
            case FULL -> redactor.redactFull(value);
            case LIGHT -> redactor.redactLight(value);
            case SKIP -> value;
        };
    }

    private record SecretPattern(String id, Pattern regex) {
    }

    private enum TextMode {
        SKIP,
        LIGHT,
        FULL
    }

    private static final class SecretRedactor {
        private final boolean includeEmail;

        private SecretRedactor(boolean includeEmail) {
            this.includeEmail = includeEmail;
        }

        String redactFull(String value) {
            String result = redactTier1(value);
            if (includeEmail) {
                result = applyPattern(result, EMAIL_PATTERN);
            }
            return applyEnvSecretPattern(result);
        }

        String redactLight(String value) {
            String result = redactTier1(value);
            if (includeEmail) {
                result = applyPattern(result, EMAIL_PATTERN);
            }
            return result;
        }

        byte[] redactFullBytes(byte[] value) {
            return redactFull(new String(value, StandardCharsets.UTF_8)).getBytes(StandardCharsets.UTF_8);
        }

        private String redactTier1(String value) {
            String result = value == null ? "" : value;
            for (SecretPattern pattern : TIER1_PATTERNS) {
                result = applyPattern(result, pattern);
            }
            return result;
        }

        private static String applyPattern(String value, SecretPattern pattern) {
            return pattern.regex().matcher(value).replaceAll(Matcher.quoteReplacement("[REDACTED:" + pattern.id() + "]"));
        }

        private static String applyEnvSecretPattern(String value) {
            return ENV_SECRET_PATTERN.regex()
                    .matcher(value)
                    .replaceAll("$1" + Matcher.quoteReplacement("[REDACTED:" + ENV_SECRET_PATTERN.id() + "]"));
        }
    }
}
