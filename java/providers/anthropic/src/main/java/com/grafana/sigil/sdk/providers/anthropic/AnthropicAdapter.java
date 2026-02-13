package com.grafana.sigil.sdk.providers.anthropic;

import com.fasterxml.jackson.databind.MapperFeature;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.SerializationFeature;
import com.grafana.sigil.sdk.GenerationResult;
import com.grafana.sigil.sdk.GenerationStart;
import com.grafana.sigil.sdk.ModelRef;
import com.grafana.sigil.sdk.SigilClient;
import com.grafana.sigil.sdk.ThrowingFunction;
import com.grafana.sigil.sdk.providers.openai.OpenAiAdapter;
import java.util.LinkedHashMap;
import java.util.Map;

/** Anthropic adapter delegates to shared mapping logic with provider override. */
public final class AnthropicAdapter {
    private static final String THINKING_BUDGET_METADATA_KEY = "sigil.gen_ai.request.thinking.budget_tokens";
    private static final ObjectMapper CANONICAL_JSON = new ObjectMapper()
            .configure(MapperFeature.SORT_PROPERTIES_ALPHABETICALLY, true)
            .configure(SerializationFeature.ORDER_MAP_ENTRIES_BY_KEYS, true);

    private AnthropicAdapter() {
    }

    /** Executes a non-stream Anthropic call and records a {@code SYNC} generation. */
    public static OpenAiAdapter.OpenAiChatResponse completion(
            SigilClient client,
            OpenAiAdapter.OpenAiChatRequest request,
            ThrowingFunction<OpenAiAdapter.OpenAiChatRequest, OpenAiAdapter.OpenAiChatResponse> providerCall,
            OpenAiAdapter.OpenAiOptions options) throws Exception {
        OpenAiAdapter.OpenAiOptions resolved = options == null ? new OpenAiAdapter.OpenAiOptions() : options;
        return client.withGeneration(new GenerationStart()
                        .setConversationId(resolved.getConversationId())
                        .setAgentName(resolved.getAgentName())
                        .setAgentVersion(resolved.getAgentVersion())
                        .setModel(new ModelRef().setProvider("anthropic").setName(request.getModel()))
                        .setSystemPrompt(request.getSystemPrompt())
                        .setTools(request.getTools())
                        .setMaxTokens(request.getMaxTokens())
                        .setTemperature(request.getTemperature())
                        .setTopP(request.getTopP())
                        .setToolChoice(canonicalToolChoice(request.getToolChoice()))
                        .setThinkingEnabled(resolveAnthropicThinkingEnabled(request.getThinking()))
                        .setMetadata(metadataWithThinkingBudget(resolved.getMetadata(), resolveAnthropicThinkingBudget(request.getThinking())))
                        .setTags(new LinkedHashMap<>(resolved.getTags())),
                recorder -> {
                    OpenAiAdapter.OpenAiChatResponse response = providerCall.apply(request);
                    recorder.setResult(applyAnthropicRequestControls(OpenAiAdapter.fromRequestResponse(request, response, resolved), request));
                    return response;
                });
    }

    /** Executes a stream Anthropic call and records a {@code STREAM} generation. */
    public static OpenAiAdapter.OpenAiStreamSummary completionStream(
            SigilClient client,
            OpenAiAdapter.OpenAiChatRequest request,
            ThrowingFunction<OpenAiAdapter.OpenAiChatRequest, OpenAiAdapter.OpenAiStreamSummary> providerCall,
            OpenAiAdapter.OpenAiOptions options) throws Exception {
        OpenAiAdapter.OpenAiOptions resolved = options == null ? new OpenAiAdapter.OpenAiOptions() : options;
        return client.withStreamingGeneration(new GenerationStart()
                        .setConversationId(resolved.getConversationId())
                        .setAgentName(resolved.getAgentName())
                        .setAgentVersion(resolved.getAgentVersion())
                        .setModel(new ModelRef().setProvider("anthropic").setName(request.getModel()))
                        .setSystemPrompt(request.getSystemPrompt())
                        .setTools(request.getTools())
                        .setMaxTokens(request.getMaxTokens())
                        .setTemperature(request.getTemperature())
                        .setTopP(request.getTopP())
                        .setToolChoice(canonicalToolChoice(request.getToolChoice()))
                        .setThinkingEnabled(resolveAnthropicThinkingEnabled(request.getThinking()))
                        .setMetadata(metadataWithThinkingBudget(resolved.getMetadata(), resolveAnthropicThinkingBudget(request.getThinking())))
                        .setTags(new LinkedHashMap<>(resolved.getTags())),
                recorder -> {
                    OpenAiAdapter.OpenAiStreamSummary summary = providerCall.apply(request);
                    recorder.setResult(applyAnthropicRequestControls(OpenAiAdapter.fromStream(request, summary, resolved), request));
                    return summary;
                });
    }

    /** Maps non-stream Anthropic payloads to a normalized Sigil generation result. */
    public static GenerationResult fromRequestResponse(
            OpenAiAdapter.OpenAiChatRequest request,
            OpenAiAdapter.OpenAiChatResponse response,
            OpenAiAdapter.OpenAiOptions options) {
        return applyAnthropicRequestControls(OpenAiAdapter.fromRequestResponse(request, response, options), request);
    }

    /** Maps stream Anthropic payloads to a normalized Sigil generation result. */
    public static GenerationResult fromStream(
            OpenAiAdapter.OpenAiChatRequest request,
            OpenAiAdapter.OpenAiStreamSummary summary,
            OpenAiAdapter.OpenAiOptions options) {
        return applyAnthropicRequestControls(OpenAiAdapter.fromStream(request, summary, options), request);
    }

    private static GenerationResult applyAnthropicRequestControls(
            GenerationResult result,
            OpenAiAdapter.OpenAiChatRequest request) {
        return result
                .setMaxTokens(request.getMaxTokens())
                .setTemperature(request.getTemperature())
                .setTopP(request.getTopP())
                .setToolChoice(canonicalToolChoice(request.getToolChoice()))
                .setThinkingEnabled(resolveAnthropicThinkingEnabled(request.getThinking()))
                .setMetadata(metadataWithThinkingBudget(result.getMetadata(), resolveAnthropicThinkingBudget(request.getThinking())));
    }

    private static String canonicalToolChoice(Object value) {
        if (value == null) {
            return null;
        }
        if (value instanceof String text) {
            String normalized = text.trim().toLowerCase();
            return normalized.isEmpty() ? null : normalized;
        }
        try {
            return CANONICAL_JSON.writeValueAsString(value);
        } catch (Exception ignored) {
            String fallback = String.valueOf(value).trim();
            return fallback.isEmpty() ? null : fallback;
        }
    }

    private static Boolean resolveAnthropicThinkingEnabled(Object value) {
        if (value == null) {
            return null;
        }
        if (value instanceof String text) {
            return thinkingTypeToBool(text);
        }
        if (value instanceof Map<?, ?> map) {
            Object type = map.get("type");
            if (type instanceof String text) {
                return thinkingTypeToBool(text);
            }
        }
        return null;
    }

    private static Long resolveAnthropicThinkingBudget(Object value) {
        if (!(value instanceof Map<?, ?> map)) {
            return null;
        }
        return coerceLong(map.get("budget_tokens"));
    }

    private static Boolean thinkingTypeToBool(String rawType) {
        String normalized = rawType == null ? "" : rawType.trim().toLowerCase();
        return switch (normalized) {
            case "enabled", "adaptive" -> Boolean.TRUE;
            case "disabled" -> Boolean.FALSE;
            default -> null;
        };
    }

    private static LinkedHashMap<String, Object> metadataWithThinkingBudget(Map<String, Object> metadata, Long thinkingBudget) {
        LinkedHashMap<String, Object> out = new LinkedHashMap<>(metadata);
        if (thinkingBudget != null) {
            out.put(THINKING_BUDGET_METADATA_KEY, thinkingBudget);
        }
        return out;
    }

    private static Long coerceLong(Object value) {
        if (value == null) {
            return null;
        }
        if (value instanceof Number number) {
            return number.longValue();
        }
        if (value instanceof String text) {
            try {
                return Long.parseLong(text.trim());
            } catch (NumberFormatException ignored) {
                return null;
            }
        }
        return null;
    }
}
