package com.grafana.sigil.sdk.providers.anthropic;

import com.fasterxml.jackson.databind.MapperFeature;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.SerializationFeature;
import com.grafana.sigil.sdk.GenerationResult;
import com.grafana.sigil.sdk.GenerationStart;
import com.grafana.sigil.sdk.ModelRef;
import com.grafana.sigil.sdk.SigilClient;
import com.grafana.sigil.sdk.ThrowingFunction;
import com.grafana.sigil.sdk.providers.openai.ProviderAdapterSupport;
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
    public static ProviderAdapterSupport.OpenAiChatResponse completion(
            SigilClient client,
            ProviderAdapterSupport.OpenAiChatRequest request,
            ThrowingFunction<ProviderAdapterSupport.OpenAiChatRequest, ProviderAdapterSupport.OpenAiChatResponse> providerCall,
            ProviderAdapterSupport.OpenAiOptions options) throws Exception {
        ProviderAdapterSupport.OpenAiOptions resolved = options == null ? new ProviderAdapterSupport.OpenAiOptions() : options;
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
                    ProviderAdapterSupport.OpenAiChatResponse response = providerCall.apply(request);
                    recorder.setResult(applyAnthropicRequestControls(ProviderAdapterSupport.fromRequestResponse(request, response, resolved), request));
                    return response;
                });
    }

    /** Executes a stream Anthropic call and records a {@code STREAM} generation. */
    public static ProviderAdapterSupport.OpenAiStreamSummary completionStream(
            SigilClient client,
            ProviderAdapterSupport.OpenAiChatRequest request,
            ThrowingFunction<ProviderAdapterSupport.OpenAiChatRequest, ProviderAdapterSupport.OpenAiStreamSummary> providerCall,
            ProviderAdapterSupport.OpenAiOptions options) throws Exception {
        ProviderAdapterSupport.OpenAiOptions resolved = options == null ? new ProviderAdapterSupport.OpenAiOptions() : options;
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
                    ProviderAdapterSupport.OpenAiStreamSummary summary = providerCall.apply(request);
                    recorder.setResult(applyAnthropicRequestControls(ProviderAdapterSupport.fromStream(request, summary, resolved), request));
                    return summary;
                });
    }

    /** Maps non-stream Anthropic payloads to a normalized Sigil generation result. */
    public static GenerationResult fromRequestResponse(
            ProviderAdapterSupport.OpenAiChatRequest request,
            ProviderAdapterSupport.OpenAiChatResponse response,
            ProviderAdapterSupport.OpenAiOptions options) {
        return applyAnthropicRequestControls(ProviderAdapterSupport.fromRequestResponse(request, response, options), request);
    }

    /** Maps stream Anthropic payloads to a normalized Sigil generation result. */
    public static GenerationResult fromStream(
            ProviderAdapterSupport.OpenAiChatRequest request,
            ProviderAdapterSupport.OpenAiStreamSummary summary,
            ProviderAdapterSupport.OpenAiOptions options) {
        return applyAnthropicRequestControls(ProviderAdapterSupport.fromStream(request, summary, options), request);
    }

    private static GenerationResult applyAnthropicRequestControls(
            GenerationResult result,
            ProviderAdapterSupport.OpenAiChatRequest request) {
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
