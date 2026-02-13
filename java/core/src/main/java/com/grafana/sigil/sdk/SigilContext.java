package com.grafana.sigil.sdk;

import io.opentelemetry.context.Context;
import io.opentelemetry.context.ContextKey;
import io.opentelemetry.context.Scope;

/** Context helpers for conversation and agent defaults. */
public final class SigilContext {
    private static final ContextKey<String> CONVERSATION_ID = ContextKey.named("sigil.conversation.id");
    private static final ContextKey<String> AGENT_NAME = ContextKey.named("sigil.agent.name");
    private static final ContextKey<String> AGENT_VERSION = ContextKey.named("sigil.agent.version");

    private SigilContext() {
    }

    /**
     * Sets the conversation id in the current OTel context.
     *
     * <p>Use the returned {@link Scope} in try-with-resources to restore context automatically.</p>
     */
    public static Scope withConversationId(String conversationId) {
        return Context.current().with(CONVERSATION_ID, emptyToBlank(conversationId)).makeCurrent();
    }

    /**
     * Sets the agent name in the current OTel context.
     *
     * <p>Use the returned {@link Scope} in try-with-resources to restore context automatically.</p>
     */
    public static Scope withAgentName(String agentName) {
        return Context.current().with(AGENT_NAME, emptyToBlank(agentName)).makeCurrent();
    }

    /**
     * Sets the agent version in the current OTel context.
     *
     * <p>Use the returned {@link Scope} in try-with-resources to restore context automatically.</p>
     */
    public static Scope withAgentVersion(String agentVersion) {
        return Context.current().with(AGENT_VERSION, emptyToBlank(agentVersion)).makeCurrent();
    }

    static String conversationIdFromContext() {
        String value = Context.current().get(CONVERSATION_ID);
        return value == null ? "" : value;
    }

    static String agentNameFromContext() {
        String value = Context.current().get(AGENT_NAME);
        return value == null ? "" : value;
    }

    static String agentVersionFromContext() {
        String value = Context.current().get(AGENT_VERSION);
        return value == null ? "" : value;
    }

    private static String emptyToBlank(String value) {
        return value == null ? "" : value;
    }
}
