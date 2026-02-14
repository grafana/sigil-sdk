using System.Text.Json;
using Anthropic;
using Anthropic.Models.Messages;
using AnthropicMessage = Anthropic.Models.Messages.Message;

namespace Grafana.Sigil.Anthropic;

public static class AnthropicRecorder
{
    public static async Task<AnthropicMessage> MessageAsync(
        SigilClient client,
        IAnthropicClient provider,
        MessageCreateParams request,
        AnthropicSigilOptions? options = null,
        CancellationToken cancellationToken = default
    )
    {
        if (provider == null)
        {
            throw new ArgumentNullException(nameof(provider));
        }

        return await MessageAsync(
            client,
            request,
            (messageRequest, ct) => provider.Messages.Create(messageRequest, ct),
            options,
            cancellationToken
        ).ConfigureAwait(false);
    }

    public static async Task<AnthropicMessage> MessageAsync(
        SigilClient client,
        MessageCreateParams request,
        Func<MessageCreateParams, CancellationToken, Task<AnthropicMessage>> invoke,
        AnthropicSigilOptions? options = null,
        CancellationToken cancellationToken = default
    )
    {
        if (client == null)
        {
            throw new ArgumentNullException(nameof(client));
        }

        if (request == null)
        {
            throw new ArgumentNullException(nameof(request));
        }

        if (invoke == null)
        {
            throw new ArgumentNullException(nameof(invoke));
        }

        var effective = options ?? new AnthropicSigilOptions();
        var modelName = ResolveModelName(request, effective);

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
            var response = await invoke(request, cancellationToken).ConfigureAwait(false);
            Exception? mappingError = null;
            Generation generation;

            try
            {
                generation = AnthropicGenerationMapper.FromRequestResponse(request, response, effective with { ModelName = modelName });
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

    public static async Task<AnthropicStreamSummary> MessageStreamAsync(
        SigilClient client,
        IAnthropicClient provider,
        MessageCreateParams request,
        AnthropicSigilOptions? options = null,
        CancellationToken cancellationToken = default
    )
    {
        if (provider == null)
        {
            throw new ArgumentNullException(nameof(provider));
        }

        return await MessageStreamAsync(
            client,
            request,
            (messageRequest, ct) => provider.Messages.CreateStreaming(messageRequest, ct),
            options,
            cancellationToken
        ).ConfigureAwait(false);
    }

    public static async Task<AnthropicStreamSummary> MessageStreamAsync(
        SigilClient client,
        MessageCreateParams request,
        Func<MessageCreateParams, CancellationToken, IAsyncEnumerable<RawMessageStreamEvent>> invoke,
        AnthropicSigilOptions? options = null,
        CancellationToken cancellationToken = default
    )
    {
        if (client == null)
        {
            throw new ArgumentNullException(nameof(client));
        }

        if (request == null)
        {
            throw new ArgumentNullException(nameof(request));
        }

        if (invoke == null)
        {
            throw new ArgumentNullException(nameof(invoke));
        }

        var effective = options ?? new AnthropicSigilOptions();
        var modelName = ResolveModelName(request, effective);

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
            var summary = new AnthropicStreamSummary();
            await foreach (var streamEvent in invoke(request, cancellationToken).WithCancellation(cancellationToken))
            {
                if (!summary.FirstChunkAt.HasValue)
                {
                    var firstChunkAt = DateTimeOffset.UtcNow;
                    summary.FirstChunkAt = firstChunkAt;
                    recorder.SetFirstTokenAt(firstChunkAt);
                }
                summary.Events.Add(streamEvent);
            }

            Exception? mappingError = null;
            Generation generation;
            try
            {
                generation = AnthropicGenerationMapper.FromStream(request, summary, effective with { ModelName = modelName });
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

    private static string ResolveModelName(MessageCreateParams request, AnthropicSigilOptions options)
    {
        if (!string.IsNullOrWhiteSpace(options.ModelName))
        {
            return options.ModelName;
        }

        var json = JsonSerializer.SerializeToElement(request);
        if (json.ValueKind == JsonValueKind.Object
            && json.TryGetProperty("model", out var model)
            && model.ValueKind == JsonValueKind.String
            && !string.IsNullOrWhiteSpace(model.GetString()))
        {
            return model.GetString()!;
        }

        return "unknown";
    }
}
