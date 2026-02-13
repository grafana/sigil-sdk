package com.grafana.sigil.sdk;

import java.time.Instant;

/** Seed fields for a tool execution span. */
public final class ToolExecutionStart {
    private String toolName = "";
    private String toolCallId = "";
    private String toolType = "";
    private String toolDescription = "";
    private String conversationId = "";
    private String agentName = "";
    private String agentVersion = "";
    private boolean includeContent;
    private Instant startedAt;

    public String getToolName() {
        return toolName;
    }

    public ToolExecutionStart setToolName(String toolName) {
        this.toolName = toolName == null ? "" : toolName;
        return this;
    }

    public String getToolCallId() {
        return toolCallId;
    }

    public ToolExecutionStart setToolCallId(String toolCallId) {
        this.toolCallId = toolCallId == null ? "" : toolCallId;
        return this;
    }

    public String getToolType() {
        return toolType;
    }

    public ToolExecutionStart setToolType(String toolType) {
        this.toolType = toolType == null ? "" : toolType;
        return this;
    }

    public String getToolDescription() {
        return toolDescription;
    }

    public ToolExecutionStart setToolDescription(String toolDescription) {
        this.toolDescription = toolDescription == null ? "" : toolDescription;
        return this;
    }

    public String getConversationId() {
        return conversationId;
    }

    public ToolExecutionStart setConversationId(String conversationId) {
        this.conversationId = conversationId == null ? "" : conversationId;
        return this;
    }

    public String getAgentName() {
        return agentName;
    }

    public ToolExecutionStart setAgentName(String agentName) {
        this.agentName = agentName == null ? "" : agentName;
        return this;
    }

    public String getAgentVersion() {
        return agentVersion;
    }

    public ToolExecutionStart setAgentVersion(String agentVersion) {
        this.agentVersion = agentVersion == null ? "" : agentVersion;
        return this;
    }

    public boolean isIncludeContent() {
        return includeContent;
    }

    public ToolExecutionStart setIncludeContent(boolean includeContent) {
        this.includeContent = includeContent;
        return this;
    }

    public Instant getStartedAt() {
        return startedAt;
    }

    public ToolExecutionStart setStartedAt(Instant startedAt) {
        this.startedAt = startedAt;
        return this;
    }

    public ToolExecutionStart copy() {
        return new ToolExecutionStart()
                .setToolName(toolName)
                .setToolCallId(toolCallId)
                .setToolType(toolType)
                .setToolDescription(toolDescription)
                .setConversationId(conversationId)
                .setAgentName(agentName)
                .setAgentVersion(agentVersion)
                .setIncludeContent(includeContent)
                .setStartedAt(startedAt);
    }
}
