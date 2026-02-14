using OpenAI.Chat;
using OpenAI.Responses;

namespace Grafana.Sigil.OpenAI;

public static class OpenAIRecorder
{
    public static async Task<ChatCompletion> CompleteChatAsync(
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

        return await CompleteChatAsync(
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

    public static async Task<ChatCompletion> CompleteChatAsync(
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
                generation = OpenAIGenerationMapper.ChatCompletionsFromRequestResponse(
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

    public static async Task<OpenAIChatCompletionsStreamSummary> CompleteChatStreamingAsync(
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

        return await CompleteChatStreamingAsync(
            client,
            messages,
            (requestMessages, opts, ct) => provider.CompleteChatStreamingAsync(requestMessages, opts, ct),
            requestOptions,
            effective with { ModelName = modelName },
            cancellationToken
        ).ConfigureAwait(false);
    }

    public static async Task<OpenAIChatCompletionsStreamSummary> CompleteChatStreamingAsync(
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
            var summary = new OpenAIChatCompletionsStreamSummary();
            await foreach (var update in invoke(messageList, requestOptions, cancellationToken).WithCancellation(cancellationToken))
            {
                if (!summary.FirstChunkAt.HasValue)
                {
                    var firstChunkAt = DateTimeOffset.UtcNow;
                    summary.FirstChunkAt = firstChunkAt;
                    recorder.SetFirstTokenAt(firstChunkAt);
                }
                summary.Updates.Add(update);
            }

            Exception? mappingError = null;
            Generation generation;
            try
            {
                generation = OpenAIGenerationMapper.ChatCompletionsFromStream(modelName, messageList, requestOptions, summary, effective);
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

    public static async Task<OpenAIResponse> CreateResponseAsync(
        SigilClient client,
        OpenAIResponseClient provider,
        IEnumerable<ResponseItem> inputItems,
        ResponseCreationOptions? requestOptions = null,
        OpenAISigilOptions? options = null,
        CancellationToken cancellationToken = default
    )
    {
        if (provider == null)
        {
            throw new ArgumentNullException(nameof(provider));
        }

        var effective = options ?? new OpenAISigilOptions();
        var modelName = ResolveInitialModelName(effective, provider.Model);

        return await CreateResponseAsync(
            client,
            inputItems,
            async (items, opts, ct) =>
            {
                var result = await provider.CreateResponseAsync(items, opts, ct).ConfigureAwait(false);
                return result.Value;
            },
            requestOptions,
            effective with { ModelName = modelName },
            cancellationToken
        ).ConfigureAwait(false);
    }

    public static async Task<OpenAIResponse> CreateResponseAsync(
        SigilClient client,
        IEnumerable<ResponseItem> inputItems,
        Func<IEnumerable<ResponseItem>, ResponseCreationOptions?, CancellationToken, Task<OpenAIResponse>> invoke,
        ResponseCreationOptions? requestOptions = null,
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
        var itemList = inputItems?.ToList() ?? throw new ArgumentNullException(nameof(inputItems));
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
            var response = await invoke(itemList, requestOptions, cancellationToken).ConfigureAwait(false);
            Exception? mappingError = null;
            Generation generation;

            try
            {
                var responseModel = string.IsNullOrWhiteSpace(response?.Model) ? modelName : response.Model;
                generation = OpenAIGenerationMapper.ResponsesFromRequestResponse(
                    responseModel,
                    itemList,
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

    public static async Task<OpenAIResponsesStreamSummary> CreateResponseStreamingAsync(
        SigilClient client,
        OpenAIResponseClient provider,
        IEnumerable<ResponseItem> inputItems,
        ResponseCreationOptions? requestOptions = null,
        OpenAISigilOptions? options = null,
        CancellationToken cancellationToken = default
    )
    {
        if (provider == null)
        {
            throw new ArgumentNullException(nameof(provider));
        }

        var effective = options ?? new OpenAISigilOptions();
        var modelName = ResolveInitialModelName(effective, provider.Model);

        return await CreateResponseStreamingAsync(
            client,
            inputItems,
            (items, opts, ct) => provider.CreateResponseStreamingAsync(items, opts, ct),
            requestOptions,
            effective with { ModelName = modelName },
            cancellationToken
        ).ConfigureAwait(false);
    }

    public static async Task<OpenAIResponsesStreamSummary> CreateResponseStreamingAsync(
        SigilClient client,
        IEnumerable<ResponseItem> inputItems,
        Func<IEnumerable<ResponseItem>, ResponseCreationOptions?, CancellationToken, IAsyncEnumerable<StreamingResponseUpdate>> invoke,
        ResponseCreationOptions? requestOptions = null,
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
        var itemList = inputItems?.ToList() ?? throw new ArgumentNullException(nameof(inputItems));
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
            var summary = new OpenAIResponsesStreamSummary();
            await foreach (var streamEvent in invoke(itemList, requestOptions, cancellationToken).WithCancellation(cancellationToken))
            {
                if (!summary.FirstChunkAt.HasValue)
                {
                    var firstChunkAt = DateTimeOffset.UtcNow;
                    summary.FirstChunkAt = firstChunkAt;
                    recorder.SetFirstTokenAt(firstChunkAt);
                }
                summary.Events.Add(streamEvent);
                if (streamEvent is StreamingResponseCompletedUpdate completed && completed.Response != null)
                {
                    summary.FinalResponse = completed.Response;
                }
            }

            Exception? mappingError = null;
            Generation generation;
            try
            {
                generation = OpenAIGenerationMapper.ResponsesFromStream(modelName, itemList, requestOptions, summary, effective);
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
