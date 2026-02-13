package com.grafana.sigil.sdk.providers.openai;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.grafana.sigil.sdk.Artifact;
import com.grafana.sigil.sdk.ArtifactKind;
import com.grafana.sigil.sdk.GenerationResult;
import com.grafana.sigil.sdk.GenerationStart;
import com.grafana.sigil.sdk.Message;
import com.grafana.sigil.sdk.MessagePart;
import com.grafana.sigil.sdk.MessageRole;
import com.grafana.sigil.sdk.ModelRef;
import com.grafana.sigil.sdk.SigilClient;
import com.grafana.sigil.sdk.TokenUsage;
import com.grafana.sigil.sdk.ToolDefinition;
import com.grafana.sigil.sdk.ThrowingFunction;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/** OpenAI provider adapter helpers for sync and streaming calls. */
public final class OpenAiAdapter {
    private static final ObjectMapper JSON = new ObjectMapper();

    private OpenAiAdapter() {
    }

    /**
     * Executes a non-stream provider call and records a {@code SYNC} generation lifecycle.
     *
     * <p>{@code providerCall} should invoke the official OpenAI SDK.</p>
     */
    public static OpenAiChatResponse chatCompletion(
            SigilClient client,
            OpenAiChatRequest request,
            ThrowingFunction<OpenAiChatRequest, OpenAiChatResponse> providerCall,
            OpenAiOptions options) throws Exception {
        OpenAiOptions resolved = options == null ? new OpenAiOptions() : options;
        return client.withGeneration(startFromRequest(request, resolved, "openai"), recorder -> {
            OpenAiChatResponse response = providerCall.apply(request);
            recorder.setResult(fromRequestResponse(request, response, resolved));
            return response;
        });
    }

    /**
     * Executes a stream provider call and records a {@code STREAM} generation lifecycle.
     *
     * <p>{@code providerCall} should return a stitched stream summary.</p>
     */
    public static OpenAiStreamSummary chatCompletionStream(
            SigilClient client,
            OpenAiChatRequest request,
            ThrowingFunction<OpenAiChatRequest, OpenAiStreamSummary> providerCall,
            OpenAiOptions options) throws Exception {
        OpenAiOptions resolved = options == null ? new OpenAiOptions() : options;
        return client.withStreamingGeneration(startFromRequest(request, resolved, "openai"), recorder -> {
            OpenAiStreamSummary summary = providerCall.apply(request);
            recorder.setResult(fromStream(request, summary, resolved));
            return summary;
        });
    }

    /** Maps non-stream OpenAI request/response data to a normalized Sigil result. */
    public static GenerationResult fromRequestResponse(OpenAiChatRequest request, OpenAiChatResponse response, OpenAiOptions options) {
        GenerationResult result = new GenerationResult()
                .setResponseId(response.getId())
                .setResponseModel(firstNonBlank(response.getModel(), request.getModel()))
                .setStopReason(response.getStopReason())
                .setUsage(response.getUsage() == null ? new TokenUsage() : response.getUsage().copy());

        for (OpenAiMessage inputMessage : request.getMessages()) {
            if ("system".equalsIgnoreCase(inputMessage.getRole())) {
                continue;
            }
            result.getInput().add(new Message()
                    .setRole(normalizeRole(inputMessage.getRole()))
                    .setName(inputMessage.getName())
                    .setParts(List.of(MessagePart.text(inputMessage.getContent()))));
        }

        result.getOutput().add(new Message()
                .setRole(MessageRole.ASSISTANT)
                .setParts(List.of(MessagePart.text(response.getOutputText()))));

        for (ToolDefinition tool : request.getTools()) {
            result.getTools().add(tool == null ? new ToolDefinition() : tool.copy());
        }
        result.getMetadata().putAll(options.getMetadata());
        result.getTags().putAll(options.getTags());

        if (options.isRawArtifacts()) {
            result.getArtifacts().add(toArtifact(ArtifactKind.REQUEST, "request", request));
            result.getArtifacts().add(toArtifact(ArtifactKind.RESPONSE, "response", response.getRaw() == null ? response : response.getRaw()));
        }

        return result;
    }

    /** Maps stream summary data to a normalized Sigil result. */
    public static GenerationResult fromStream(OpenAiChatRequest request, OpenAiStreamSummary summary, OpenAiOptions options) {
        OpenAiChatResponse finalResponse = summary.getFinalResponse();
        GenerationResult result = new GenerationResult()
                .setResponseId(finalResponse == null ? "" : finalResponse.getId())
                .setResponseModel(finalResponse == null ? request.getModel() : firstNonBlank(finalResponse.getModel(), request.getModel()))
                .setStopReason(finalResponse == null ? "" : finalResponse.getStopReason())
                .setUsage(finalResponse == null || finalResponse.getUsage() == null ? new TokenUsage() : finalResponse.getUsage().copy());

        for (OpenAiMessage inputMessage : request.getMessages()) {
            if ("system".equalsIgnoreCase(inputMessage.getRole())) {
                continue;
            }
            result.getInput().add(new Message()
                    .setRole(normalizeRole(inputMessage.getRole()))
                    .setName(inputMessage.getName())
                    .setParts(List.of(MessagePart.text(inputMessage.getContent()))));
        }

        result.getOutput().add(new Message()
                .setRole(MessageRole.ASSISTANT)
                .setParts(List.of(MessagePart.text(summary.getOutputText()))));

        for (ToolDefinition tool : request.getTools()) {
            result.getTools().add(tool == null ? new ToolDefinition() : tool.copy());
        }
        result.getMetadata().putAll(options.getMetadata());
        result.getTags().putAll(options.getTags());

        if (options.isRawArtifacts()) {
            result.getArtifacts().add(toArtifact(ArtifactKind.REQUEST, "request", request));
            result.getArtifacts().add(toArtifact(ArtifactKind.PROVIDER_EVENT, "provider_event", summary.getChunks()));
        }

        return result;
    }

    private static GenerationStart startFromRequest(OpenAiChatRequest request, OpenAiOptions options, String providerName) {
        return new GenerationStart()
                .setConversationId(options.getConversationId())
                .setAgentName(options.getAgentName())
                .setAgentVersion(options.getAgentVersion())
                .setModel(new ModelRef().setProvider(providerName).setName(request.getModel()))
                .setSystemPrompt(request.getSystemPrompt())
                .setTools(request.getTools())
                .setMetadata(new LinkedHashMap<>(options.getMetadata()))
                .setTags(new LinkedHashMap<>(options.getTags()));
    }

    private static Artifact toArtifact(ArtifactKind kind, String name, Object payload) {
        try {
            return new Artifact()
                    .setKind(kind)
                    .setName(name)
                    .setContentType("application/json")
                    .setPayload(JSON.writeValueAsBytes(payload));
        } catch (Exception ignored) {
            return new Artifact()
                    .setKind(kind)
                    .setName(name)
                    .setContentType("application/json")
                    .setPayload(String.valueOf(payload).getBytes(StandardCharsets.UTF_8));
        }
    }

    private static MessageRole normalizeRole(String role) {
        if (role != null && role.equalsIgnoreCase("assistant")) {
            return MessageRole.ASSISTANT;
        }
        if (role != null && role.equalsIgnoreCase("tool")) {
            return MessageRole.TOOL;
        }
        return MessageRole.USER;
    }

    private static String firstNonBlank(String... values) {
        for (String value : values) {
            if (value != null && !value.isBlank()) {
                return value;
            }
        }
        return "";
    }

    public static final class OpenAiMessage {
        private String role = "user";
        private String content = "";
        private String name = "";

        public String getRole() {
            return role;
        }

        public OpenAiMessage setRole(String role) {
            this.role = role == null ? "user" : role;
            return this;
        }

        public String getContent() {
            return content;
        }

        public OpenAiMessage setContent(String content) {
            this.content = content == null ? "" : content;
            return this;
        }

        public String getName() {
            return name;
        }

        public OpenAiMessage setName(String name) {
            this.name = name == null ? "" : name;
            return this;
        }
    }

    public static final class OpenAiChatRequest {
        private String model = "";
        private String systemPrompt = "";
        private final List<OpenAiMessage> messages = new ArrayList<>();
        private final List<ToolDefinition> tools = new ArrayList<>();

        public String getModel() {
            return model;
        }

        public OpenAiChatRequest setModel(String model) {
            this.model = model == null ? "" : model;
            return this;
        }

        public String getSystemPrompt() {
            return systemPrompt;
        }

        public OpenAiChatRequest setSystemPrompt(String systemPrompt) {
            this.systemPrompt = systemPrompt == null ? "" : systemPrompt;
            return this;
        }

        public List<OpenAiMessage> getMessages() {
            return messages;
        }

        public OpenAiChatRequest setMessages(List<OpenAiMessage> messages) {
            this.messages.clear();
            if (messages != null) {
                this.messages.addAll(messages);
            }
            return this;
        }

        public List<ToolDefinition> getTools() {
            return tools;
        }

        public OpenAiChatRequest setTools(List<ToolDefinition> tools) {
            this.tools.clear();
            if (tools != null) {
                this.tools.addAll(tools);
            }
            return this;
        }
    }

    public static final class OpenAiChatResponse {
        private String id = "";
        private String model = "";
        private String outputText = "";
        private TokenUsage usage = new TokenUsage();
        private String stopReason = "";
        private Object raw;

        public String getId() {
            return id;
        }

        public OpenAiChatResponse setId(String id) {
            this.id = id == null ? "" : id;
            return this;
        }

        public String getModel() {
            return model;
        }

        public OpenAiChatResponse setModel(String model) {
            this.model = model == null ? "" : model;
            return this;
        }

        public String getOutputText() {
            return outputText;
        }

        public OpenAiChatResponse setOutputText(String outputText) {
            this.outputText = outputText == null ? "" : outputText;
            return this;
        }

        public TokenUsage getUsage() {
            return usage;
        }

        public OpenAiChatResponse setUsage(TokenUsage usage) {
            this.usage = usage == null ? new TokenUsage() : usage;
            return this;
        }

        public String getStopReason() {
            return stopReason;
        }

        public OpenAiChatResponse setStopReason(String stopReason) {
            this.stopReason = stopReason == null ? "" : stopReason;
            return this;
        }

        public Object getRaw() {
            return raw;
        }

        public OpenAiChatResponse setRaw(Object raw) {
            this.raw = raw;
            return this;
        }
    }

    public static final class OpenAiStreamSummary {
        private String outputText = "";
        private OpenAiChatResponse finalResponse;
        private final List<Object> chunks = new ArrayList<>();

        public String getOutputText() {
            return outputText;
        }

        public OpenAiStreamSummary setOutputText(String outputText) {
            this.outputText = outputText == null ? "" : outputText;
            return this;
        }

        public OpenAiChatResponse getFinalResponse() {
            return finalResponse;
        }

        public OpenAiStreamSummary setFinalResponse(OpenAiChatResponse finalResponse) {
            this.finalResponse = finalResponse;
            return this;
        }

        public List<Object> getChunks() {
            return chunks;
        }

        public OpenAiStreamSummary setChunks(List<Object> chunks) {
            this.chunks.clear();
            if (chunks != null) {
                this.chunks.addAll(chunks);
            }
            return this;
        }
    }

    public static final class OpenAiOptions {
        private String conversationId = "";
        private String agentName = "";
        private String agentVersion = "";
        private final Map<String, String> tags = new LinkedHashMap<>();
        private final Map<String, Object> metadata = new LinkedHashMap<>();
        private boolean rawArtifacts;

        public String getConversationId() {
            return conversationId;
        }

        public OpenAiOptions setConversationId(String conversationId) {
            this.conversationId = conversationId == null ? "" : conversationId;
            return this;
        }

        public String getAgentName() {
            return agentName;
        }

        public OpenAiOptions setAgentName(String agentName) {
            this.agentName = agentName == null ? "" : agentName;
            return this;
        }

        public String getAgentVersion() {
            return agentVersion;
        }

        public OpenAiOptions setAgentVersion(String agentVersion) {
            this.agentVersion = agentVersion == null ? "" : agentVersion;
            return this;
        }

        public Map<String, String> getTags() {
            return tags;
        }

        public OpenAiOptions setTags(Map<String, String> tags) {
            this.tags.clear();
            if (tags != null) {
                this.tags.putAll(tags);
            }
            return this;
        }

        public Map<String, Object> getMetadata() {
            return metadata;
        }

        public OpenAiOptions setMetadata(Map<String, Object> metadata) {
            this.metadata.clear();
            if (metadata != null) {
                this.metadata.putAll(metadata);
            }
            return this;
        }

        public boolean isRawArtifacts() {
            return rawArtifacts;
        }

        public OpenAiOptions setRawArtifacts(boolean rawArtifacts) {
            this.rawArtifacts = rawArtifacts;
            return this;
        }
    }
}
