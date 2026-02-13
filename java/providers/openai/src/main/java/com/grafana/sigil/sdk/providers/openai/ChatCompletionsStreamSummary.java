package com.grafana.sigil.sdk.providers.openai;

import com.openai.models.chat.completions.ChatCompletion;
import com.openai.models.chat.completions.ChatCompletionChunk;
import java.util.ArrayList;
import java.util.List;

/** Captures OpenAI chat-completions stream chunks and optional stitched final response. */
public final class ChatCompletionsStreamSummary {
    private final List<ChatCompletionChunk> chunks = new ArrayList<>();
    private ChatCompletion finalResponse;

    public List<ChatCompletionChunk> getChunks() {
        return chunks;
    }

    public ChatCompletionsStreamSummary setChunks(List<ChatCompletionChunk> chunks) {
        this.chunks.clear();
        if (chunks != null) {
            this.chunks.addAll(chunks);
        }
        return this;
    }

    public ChatCompletion getFinalResponse() {
        return finalResponse;
    }

    public ChatCompletionsStreamSummary setFinalResponse(ChatCompletion finalResponse) {
        this.finalResponse = finalResponse;
        return this;
    }
}
