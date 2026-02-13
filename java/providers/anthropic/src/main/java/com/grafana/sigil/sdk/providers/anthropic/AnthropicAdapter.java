package com.grafana.sigil.sdk.providers.anthropic;

import com.grafana.sigil.sdk.GenerationResult;
import com.grafana.sigil.sdk.GenerationStart;
import com.grafana.sigil.sdk.ModelRef;
import com.grafana.sigil.sdk.SigilClient;
import com.grafana.sigil.sdk.ThrowingFunction;
import com.grafana.sigil.sdk.providers.openai.OpenAiAdapter;
import java.util.LinkedHashMap;

/** Anthropic adapter delegates to shared mapping logic with provider override. */
public final class AnthropicAdapter {
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
                        .setMetadata(new LinkedHashMap<>(resolved.getMetadata()))
                        .setTags(new LinkedHashMap<>(resolved.getTags())),
                recorder -> {
                    OpenAiAdapter.OpenAiChatResponse response = providerCall.apply(request);
                    recorder.setResult(OpenAiAdapter.fromRequestResponse(request, response, resolved));
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
                        .setMetadata(new LinkedHashMap<>(resolved.getMetadata()))
                        .setTags(new LinkedHashMap<>(resolved.getTags())),
                recorder -> {
                    OpenAiAdapter.OpenAiStreamSummary summary = providerCall.apply(request);
                    recorder.setResult(OpenAiAdapter.fromStream(request, summary, resolved));
                    return summary;
                });
    }

    /** Maps non-stream Anthropic payloads to a normalized Sigil generation result. */
    public static GenerationResult fromRequestResponse(
            OpenAiAdapter.OpenAiChatRequest request,
            OpenAiAdapter.OpenAiChatResponse response,
            OpenAiAdapter.OpenAiOptions options) {
        return OpenAiAdapter.fromRequestResponse(request, response, options);
    }

    /** Maps stream Anthropic payloads to a normalized Sigil generation result. */
    public static GenerationResult fromStream(
            OpenAiAdapter.OpenAiChatRequest request,
            OpenAiAdapter.OpenAiStreamSummary summary,
            OpenAiAdapter.OpenAiOptions options) {
        return OpenAiAdapter.fromStream(request, summary, options);
    }
}
