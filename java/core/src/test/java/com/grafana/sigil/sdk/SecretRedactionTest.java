package com.grafana.sigil.sdk;

import static org.assertj.core.api.Assertions.assertThat;

import io.opentelemetry.api.GlobalOpenTelemetry;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.List;
import java.util.Map;
import java.util.logging.Logger;
import org.junit.jupiter.api.Test;

class SecretRedactionTest {
    @Test
    void secretRedactionSanitizerRedactsExportedGeneration() throws Exception {
        TestFixtures.CapturingExporter exporter = new TestFixtures.CapturingExporter();
        SigilClientConfig config = new SigilClientConfig()
                .setTracer(GlobalOpenTelemetry.getTracer("test"))
                .setGenerationExporter(exporter)
                .setGenerationSanitizer(SecretRedaction.createSecretRedactionSanitizer())
                .setGenerationExport(new GenerationExportConfig()
                        .setBatchSize(1)
                        .setQueueSize(100)
                        .setFlushInterval(Duration.ofMinutes(10))
                        .setMaxRetries(0));

        try (SigilClient client = new SigilClient(config)) {
            GenerationStart start = TestFixtures.startFixture()
                    .setSystemPrompt("Use TOKEN=system-secret")
                    .setConversationTitle("Ask support@example.com");

            GenerationResult result = TestFixtures.resultFixture()
                    .setSystemPrompt("Use TOKEN=system-secret")
                    .setInput(List.of(new Message()
                            .setRole(MessageRole.USER)
                            .setParts(List.of(MessagePart.text("raw input AKIAIOSFODNN7EXAMPLE")))))
                    .setOutput(List.of(
                            new Message()
                                    .setRole(MessageRole.ASSISTANT)
                                    .setParts(List.of(
                                            MessagePart.text("email support@example.com "
                                                    + "key sk-proj-" + "a".repeat(40)),
                                            MessagePart.thinking("seen " + githubPat()),
                                            MessagePart.toolCall(new ToolCall()
                                                    .setId("tool-call-1")
                                                    .setName("search")
                                                    .setInputJson(("{\"auth\":\"Bearer " + "a".repeat(24) + "\"}")
                                                            .getBytes(StandardCharsets.UTF_8))))),
                            new Message()
                                    .setRole(MessageRole.TOOL)
                                    .setParts(List.of(MessagePart.toolResult(new ToolResultPart()
                                            .setToolCallId("tool-call-1")
                                            .setName("search")
                                            .setContent("PASSWORD=tool-secret")
                                            .setContentJson("{\"token\":\"ghp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"}"
                                                    .getBytes(StandardCharsets.UTF_8)))))));

            GenerationRecorder recorder = client.startGeneration(start);
            recorder.setResult(result);
            recorder.end();
        }

        TestFixtures.waitFor(() -> !exporter.getRequests().isEmpty(), Duration.ofSeconds(2));
        Generation generation = exporter.getRequests().get(0).get(0);

        assertThat(generation.getSystemPrompt()).contains("TOKEN=[REDACTED:env-secret-value]");
        assertThat(generation.getConversationTitle()).isEqualTo("Ask [REDACTED:email]");
        assertThat(generation.getMetadata()).containsEntry(SigilClient.SPAN_ATTR_CONVERSATION_TITLE, "Ask [REDACTED:email]");
        assertThat(generation.getInput().get(0).getParts().get(0).getText()).contains("AKIAIOSFODNN7EXAMPLE");
        assertThat(generation.getOutput().get(0).getParts().get(0).getText())
                .contains("[REDACTED:email]")
                .contains("[REDACTED:openai-project-key]");
        assertThat(generation.getOutput().get(0).getParts().get(1).getThinking())
                .contains("[REDACTED:github-pat]");
        assertThat(new String(
                generation.getOutput().get(0).getParts().get(2).getToolCall().getInputJson(),
                StandardCharsets.UTF_8)).contains("[REDACTED:bearer-token]");
        assertThat(generation.getOutput().get(1).getParts().get(0).getToolResult().getContent())
                .contains("PASSWORD=[REDACTED:env-secret-value]");
        assertThat(new String(
                generation.getOutput().get(1).getParts().get(0).getToolResult().getContentJson(),
                StandardCharsets.UTF_8)).contains("[REDACTED:github-pat]");
    }

    @Test
    void secretRedactionSanitizerCanRedactUserInputMessages() {
        GenerationSanitizer sanitizer = SecretRedaction.createSecretRedactionSanitizer(
                new SecretRedactionOptions().setRedactInputMessages(true));

        Generation generation = new Generation();
        generation.setInput(List.of(new Message()
                .setRole(MessageRole.USER)
                .setParts(List.of(MessagePart.text("aws AKIAIOSFODNN7EXAMPLE")))));

        Generation sanitized = sanitizer.sanitize(generation);

        assertThat(sanitized.getInput().get(0).getParts().get(0).getText())
                .contains("[REDACTED:aws-access-token]");
        assertThat(generation.getInput().get(0).getParts().get(0).getText())
                .contains("AKIAIOSFODNN7EXAMPLE");
    }

    @Test
    void secretRedactionSanitizerSupportsGitleaksPatternSet() {
        GenerationSanitizer sanitizer = SecretRedaction.createSecretRedactionSanitizer(
                new SecretRedactionOptions().setRedactInputMessages(true));

        for (Map.Entry<String, String> fixture : fixtures().entrySet()) {
            Generation generation = new Generation();
            generation.setOutput(List.of(new Message()
                    .setRole(MessageRole.ASSISTANT)
                    .setParts(List.of(MessagePart.text(fixture.getValue())))));
            Generation sanitized = sanitizer.sanitize(generation);

            assertThat(sanitized.getOutput().get(0).getParts().get(0).getText())
                    .as(fixture.getKey())
                    .contains("[REDACTED:" + fixture.getKey() + "]");
        }
    }

    @Test
    void redactInputMessagesResolvesExplicitThenEnvThenFalse() {
        Logger logger = Logger.getLogger("com.grafana.sigil.sdk.test");

        assertThat(SecretRedaction.resolveRedactInputMessages(true, key -> "false", logger)).isTrue();
        assertThat(SecretRedaction.resolveRedactInputMessages(null, key -> "yes", logger)).isTrue();
        assertThat(SecretRedaction.resolveRedactInputMessages(null, key -> "0", logger)).isFalse();
        assertThat(SecretRedaction.resolveRedactInputMessages(null, key -> "maybe", logger)).isFalse();
        assertThat(SecretRedaction.resolveRedactInputMessages(null, key -> null, logger)).isFalse();
    }

    private static Map<String, String> fixtures() {
        return Map.ofEntries(
                Map.entry("grafana-cloud-token", "glc_" + "a".repeat(20)),
                Map.entry("grafana-service-account-token", "glsa_" + "b".repeat(20)),
                Map.entry("aws-access-token", "AKIAIOSFODNN7EXAMPLE"),
                Map.entry("github-pat", githubPat()),
                Map.entry("github-oauth", "gho_" + "c".repeat(36)),
                Map.entry("github-app-token", "ghs_" + "d".repeat(36)),
                Map.entry("github-fine-grained-pat", "github_pat_" + "e".repeat(82)),
                Map.entry("anthropic-api-key", "sk-ant-api03-" + "f".repeat(93) + "AA"),
                Map.entry("anthropic-admin-key", "sk-ant-admin01-" + "g".repeat(93) + "AA"),
                Map.entry("openai-api-key", "sk-" + "h".repeat(20) + "T3BlbkFJ" + "i".repeat(20)),
                Map.entry("openai-project-key", "sk-proj-" + "j".repeat(40)),
                Map.entry("openai-svcacct-key", "sk-svcacct-" + "k".repeat(40)),
                Map.entry("gcp-api-key", "AIza" + "l".repeat(35)),
                Map.entry("private-key", "-----BEGIN PRIVATE KEY-----\nsecret\n-----END PRIVATE KEY-----"),
                Map.entry("connection-string", "postgres://user:pass@example.com/db"),
                Map.entry("bearer-token", "Bearer " + "m".repeat(24)),
                Map.entry("slack-token", "xoxb-" + "n".repeat(10)),
                Map.entry("stripe-key", "sk_live_" + "o".repeat(20)),
                Map.entry("sendgrid-api-key", "SG." + "p".repeat(22) + "." + "q".repeat(43)),
                Map.entry("twilio-api-key", "SK" + "a".repeat(32)),
                Map.entry("npm-token", "npm_" + "r".repeat(36)),
                Map.entry("pypi-token", "pypi-" + "s".repeat(50)));
    }

    private static String githubPat() {
        return "ghp_" + "a".repeat(36);
    }
}
