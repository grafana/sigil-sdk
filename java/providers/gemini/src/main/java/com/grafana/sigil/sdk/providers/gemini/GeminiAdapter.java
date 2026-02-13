package com.grafana.sigil.sdk.providers.gemini;

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

/** Gemini adapter delegates to shared mapping logic with provider override. */
public final class GeminiAdapter {
    private static final String THINKING_BUDGET_METADATA_KEY = "sigil.gen_ai.request.thinking.budget_tokens";
    private static final ObjectMapper CANONICAL_JSON = new ObjectMapper()
            .configure(MapperFeature.SORT_PROPERTIES_ALPHABETICALLY, true)
            .configure(SerializationFeature.ORDER_MAP_ENTRIES_BY_KEYS, true);

    private GeminiAdapter() {
    }

    /** Executes a non-stream Gemini call and records a {@code SYNC} generation. */
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
                        .setModel(new ModelRef().setProvider("gemini").setName(request.getModel()))
                        .setSystemPrompt(request.getSystemPrompt())
                        .setTools(request.getTools())
                        .setMaxTokens(request.getMaxOutputTokens())
                        .setTemperature(request.getTemperature())
                        .setTopP(request.getTopP())
                        .setToolChoice(canonicalToolChoice(request.getFunctionCallingMode()))
                        .setThinkingEnabled(resolveGeminiThinkingEnabled(request.getThinkingConfig()))
                        .setMetadata(metadataWithThinkingBudget(resolved.getMetadata(), resolveGeminiThinkingBudget(request.getThinkingConfig())))
                        .setTags(new LinkedHashMap<>(resolved.getTags())),
                recorder -> {
                    ProviderAdapterSupport.OpenAiChatResponse response = providerCall.apply(request);
                    recorder.setResult(applyGeminiRequestControls(ProviderAdapterSupport.fromRequestResponse(request, response, resolved), request));
                    return response;
                });
    }

    /** Executes a stream Gemini call and records a {@code STREAM} generation. */
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
                        .setModel(new ModelRef().setProvider("gemini").setName(request.getModel()))
                        .setSystemPrompt(request.getSystemPrompt())
                        .setTools(request.getTools())
                        .setMaxTokens(request.getMaxOutputTokens())
                        .setTemperature(request.getTemperature())
                        .setTopP(request.getTopP())
                        .setToolChoice(canonicalToolChoice(request.getFunctionCallingMode()))
                        .setThinkingEnabled(resolveGeminiThinkingEnabled(request.getThinkingConfig()))
                        .setMetadata(metadataWithThinkingBudget(resolved.getMetadata(), resolveGeminiThinkingBudget(request.getThinkingConfig())))
                        .setTags(new LinkedHashMap<>(resolved.getTags())),
                recorder -> {
                    ProviderAdapterSupport.OpenAiStreamSummary summary = providerCall.apply(request);
                    recorder.setResult(applyGeminiRequestControls(ProviderAdapterSupport.fromStream(request, summary, resolved), request));
                    return summary;
                });
    }

    /** Maps non-stream Gemini payloads to a normalized Sigil generation result. */
    public static GenerationResult fromRequestResponse(
            ProviderAdapterSupport.OpenAiChatRequest request,
            ProviderAdapterSupport.OpenAiChatResponse response,
            ProviderAdapterSupport.OpenAiOptions options) {
        return applyGeminiRequestControls(ProviderAdapterSupport.fromRequestResponse(request, response, options), request);
    }

    /** Maps stream Gemini payloads to a normalized Sigil generation result. */
    public static GenerationResult fromStream(
            ProviderAdapterSupport.OpenAiChatRequest request,
            ProviderAdapterSupport.OpenAiStreamSummary summary,
            ProviderAdapterSupport.OpenAiOptions options) {
        return applyGeminiRequestControls(ProviderAdapterSupport.fromStream(request, summary, options), request);
    }

    private static GenerationResult applyGeminiRequestControls(
            GenerationResult result,
            ProviderAdapterSupport.OpenAiChatRequest request) {
        return result
                .setMaxTokens(request.getMaxOutputTokens())
                .setTemperature(request.getTemperature())
                .setTopP(request.getTopP())
                .setToolChoice(canonicalToolChoice(request.getFunctionCallingMode()))
                .setThinkingEnabled(resolveGeminiThinkingEnabled(request.getThinkingConfig()))
                .setMetadata(metadataWithThinkingBudget(result.getMetadata(), resolveGeminiThinkingBudget(request.getThinkingConfig())));
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

    private static Boolean resolveGeminiThinkingEnabled(Object value) {
        if (value == null) {
            return null;
        }
        if (value instanceof Map<?, ?> map) {
            Object includeThoughts = map.get("include_thoughts");
            if (!(includeThoughts instanceof Boolean)) {
                includeThoughts = map.get("includeThoughts");
            }
            if (includeThoughts instanceof Boolean bool) {
                return bool;
            }
            return null;
        }
        if (value instanceof Boolean bool) {
            return bool;
        }
        return null;
    }

    private static Long resolveGeminiThinkingBudget(Object value) {
        if (!(value instanceof Map<?, ?> map)) {
            return null;
        }
        Long snakeCase = coerceLong(map.get("thinking_budget"));
        if (snakeCase != null) {
            return snakeCase;
        }
        return coerceLong(map.get("thinkingBudget"));
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
