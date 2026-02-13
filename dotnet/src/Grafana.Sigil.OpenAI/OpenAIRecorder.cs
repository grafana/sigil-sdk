using OpenAI.Chat;

namespace Grafana.Sigil.OpenAI;

public static class OpenAIRecorder
{
    public static async Task<ChatCompletion> ChatCompletionAsync(
        SigilClient client,
        ChatClient provider,
        IEnumerable<ChatMessage> messages,
        ChatCompletionOptions? requestOptions = null,
        OpenAISigilOptions? options = null,
        CancellationToken cancellationToken = default
    )
    {
        if (provider == null)
        {
            throw new ArgumentNullException(nameof(provider));
        }

        var effective = options ?? new OpenAISigilOptions();
        var modelName = ResolveInitialModelName(effective, provider.GetType().GetProperty("Model")?.GetValue(provider) as string);

        return await ChatCompletionAsync(
            client,
            messages,
            async (requestMessages, opts, ct) =>
            {
                var result = await provider.CompleteChatAsync(requestMessages, opts, ct).ConfigureAwait(false);
                return result.Value;
            },
            requestOptions,
            effective with { ModelName = modelName },
            cancellationToken
        ).ConfigureAwait(false);
    }

    public static async Task<ChatCompletion> ChatCompletionAsync(
        SigilClient client,
        IEnumerable<ChatMessage> messages,
        Func<IEnumerable<ChatMessage>, ChatCompletionOptions?, CancellationToken, Task<ChatCompletion>> invoke,
        ChatCompletionOptions? requestOptions = null,
        OpenAISigilOptions? options = null,
        CancellationToken cancellationToken = default
    )
    {
        if (client == null)
        {
            throw new ArgumentNullException(nameof(client));
        }

        if (invoke == null)
        {
            throw new ArgumentNullException(nameof(invoke));
        }

        var effective = options ?? new OpenAISigilOptions();
        var messageList = messages?.ToList() ?? throw new ArgumentNullException(nameof(messages));
        var modelName = ResolveInitialModelName(effective, fallback: null);

        var recorder = client.StartGeneration(new GenerationStart
        {
            ConversationId = effective.ConversationId,
            AgentName = effective.AgentName,
            AgentVersion = effective.AgentVersion,
            Model = new ModelRef
            {
                Provider = effective.ProviderName,
                Name = modelName,
            },
            Mode = GenerationMode.Sync,
        });

        try
        {
            var response = await invoke(messageList, requestOptions, cancellationToken).ConfigureAwait(false);
            Exception? mappingError = null;
            Generation generation;

            try
            {
                var responseModel = string.IsNullOrWhiteSpace(response?.Model) ? modelName : response.Model;
                generation = OpenAIGenerationMapper.FromRequestResponse(
                    responseModel,
                    messageList,
                    requestOptions,
                    response!,
                    effective with { ModelName = responseModel }
                );
            }
            catch (Exception ex)
            {
                mappingError = ex;
                generation = new Generation();
            }

            recorder.SetResult(generation, mappingError);
            return response;
        }
        catch (Exception ex)
        {
            recorder.SetCallError(ex);
            throw;
        }
        finally
        {
            recorder.End();
        }
    }

    public static async Task<OpenAIStreamSummary> ChatCompletionStreamAsync(
        SigilClient client,
        ChatClient provider,
        IEnumerable<ChatMessage> messages,
        ChatCompletionOptions? requestOptions = null,
        OpenAISigilOptions? options = null,
        CancellationToken cancellationToken = default
    )
    {
        if (provider == null)
        {
            throw new ArgumentNullException(nameof(provider));
        }

        var effective = options ?? new OpenAISigilOptions();
        var modelName = ResolveInitialModelName(effective, provider.GetType().GetProperty("Model")?.GetValue(provider) as string);

        return await ChatCompletionStreamAsync(
            client,
            messages,
            (requestMessages, opts, ct) => provider.CompleteChatStreamingAsync(requestMessages, opts, ct),
            requestOptions,
            effective with { ModelName = modelName },
            cancellationToken
        ).ConfigureAwait(false);
    }

    public static async Task<OpenAIStreamSummary> ChatCompletionStreamAsync(
        SigilClient client,
        IEnumerable<ChatMessage> messages,
        Func<IEnumerable<ChatMessage>, ChatCompletionOptions?, CancellationToken, IAsyncEnumerable<StreamingChatCompletionUpdate>> invoke,
        ChatCompletionOptions? requestOptions = null,
        OpenAISigilOptions? options = null,
        CancellationToken cancellationToken = default
    )
    {
        if (client == null)
        {
            throw new ArgumentNullException(nameof(client));
        }

        if (invoke == null)
        {
            throw new ArgumentNullException(nameof(invoke));
        }

        var effective = options ?? new OpenAISigilOptions();
        var messageList = messages?.ToList() ?? throw new ArgumentNullException(nameof(messages));
        var modelName = ResolveInitialModelName(effective, fallback: null);

        var recorder = client.StartStreamingGeneration(new GenerationStart
        {
            ConversationId = effective.ConversationId,
            AgentName = effective.AgentName,
            AgentVersion = effective.AgentVersion,
            Model = new ModelRef
            {
                Provider = effective.ProviderName,
                Name = modelName,
            },
            Mode = GenerationMode.Stream,
        });

        try
        {
            var summary = new OpenAIStreamSummary();
            await foreach (var update in invoke(messageList, requestOptions, cancellationToken).WithCancellation(cancellationToken))
            {
                summary.Updates.Add(update);
            }

            Exception? mappingError = null;
            Generation generation;
            try
            {
                generation = OpenAIGenerationMapper.FromStream(modelName, messageList, requestOptions, summary, effective);
            }
            catch (Exception ex)
            {
                mappingError = ex;
                generation = new Generation();
            }

            recorder.SetResult(generation, mappingError);
            return summary;
        }
        catch (Exception ex)
        {
            recorder.SetCallError(ex);
            throw;
        }
        finally
        {
            recorder.End();
        }
    }

    private static string ResolveInitialModelName(OpenAISigilOptions options, string? fallback)
    {
        if (!string.IsNullOrWhiteSpace(options.ModelName))
        {
            return options.ModelName;
        }

        if (!string.IsNullOrWhiteSpace(fallback))
        {
            return fallback;
        }

        return "unknown";
    }
}
