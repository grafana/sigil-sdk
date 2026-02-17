package com.grafana.sigil.sdk.providers.openai;

import com.grafana.sigil.sdk.EmbeddingResult;
import com.grafana.sigil.sdk.SigilClient;
import com.grafana.sigil.sdk.ThrowingFunction;
import com.openai.models.embeddings.CreateEmbeddingResponse;
import com.openai.models.embeddings.EmbeddingCreateParams;

/** OpenAI embeddings wrappers and mappers using official OpenAI Java SDK types. */
public final class OpenAiEmbeddings {
    private OpenAiEmbeddings() {
    }

    public static CreateEmbeddingResponse create(
            SigilClient client,
            EmbeddingCreateParams request,
            ThrowingFunction<EmbeddingCreateParams, CreateEmbeddingResponse> providerCall,
            OpenAiOptions options) throws Exception {
        OpenAiOptions resolved = options == null ? new OpenAiOptions() : options;
        return client.withEmbedding(OpenAiGenerationMapper.embeddingsStart(request, resolved), recorder -> {
            CreateEmbeddingResponse response = providerCall.apply(request);
            recorder.setResult(fromRequestResponse(request, response));
            return response;
        });
    }

    public static EmbeddingResult fromRequestResponse(
            EmbeddingCreateParams request,
            CreateEmbeddingResponse response) {
        return OpenAiGenerationMapper.embeddingsFromRequestResponse(request, response);
    }
}
