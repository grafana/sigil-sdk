package com.grafana.sigil.sdk;

/** Options for {@link SecretRedaction#createSecretRedactionSanitizer(SecretRedactionOptions)}. */
public final class SecretRedactionOptions {
    private Boolean redactInputMessages;
    private Boolean redactEmailAddresses;

    /**
     * Whether user-role input messages should be redacted.
     *
     * <p>{@code null} falls back to {@code SIGIL_REDACT_INPUT_MESSAGES} and
     * then to {@code false}. Assistant and tool messages are redacted
     * regardless because they share the same secret surface as outputs.</p>
     */
    public Boolean getRedactInputMessages() {
        return redactInputMessages;
    }

    public SecretRedactionOptions setRedactInputMessages(Boolean redactInputMessages) {
        this.redactInputMessages = redactInputMessages;
        return this;
    }

    /**
     * Whether generic email addresses should be redacted.
     *
     * <p>{@code null} defaults to {@code true}.</p>
     */
    public Boolean getRedactEmailAddresses() {
        return redactEmailAddresses;
    }

    public SecretRedactionOptions setRedactEmailAddresses(Boolean redactEmailAddresses) {
        this.redactEmailAddresses = redactEmailAddresses;
        return this;
    }
}
