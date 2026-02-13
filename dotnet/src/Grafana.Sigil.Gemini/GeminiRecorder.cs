using Google.GenAI;
using Google.GenAI.Types;

namespace Grafana.Sigil.Gemini;

public static class GeminiRecorder
{
    public static async Task<GenerateContentResponse> GenerateContentAsync(
        SigilClient client,
        Client provider,
        GenerateContentRequest request,
        GeminiSigilOptions? options = null,
        CancellationToken cancellationToken = default
    )
    {
        if (provider == null)
        {
            throw new ArgumentNullException(nameof(provider));
        }

        return await GenerateContentAsync(
            client,
            request,
            (payload, ct) => provider.Models.GenerateContentAsync(payload.Model, payload.Contents, payload.Config, ct),
            options,
            cancellationToken
        ).ConfigureAwait(false);
    }

    public static async Task<GenerateContentResponse> GenerateContentAsync(
        SigilClient client,
        GenerateContentRequest request,
        Func<GenerateContentRequest, CancellationToken, Task<GenerateContentResponse>> invoke,
        GeminiSigilOptions? options = null,
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

        var effective = options ?? new GeminiSigilOptions();
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
                generation = GeminiGenerationMapper.FromRequestResponse(request, response, effective with { ModelName = modelName });
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

    public static async Task<GeminiStreamSummary> GenerateContentStreamAsync(
        SigilClient client,
        Client provider,
        GenerateContentRequest request,
        GeminiSigilOptions? options = null,
        CancellationToken cancellationToken = default
    )
    {
        if (provider == null)
        {
            throw new ArgumentNullException(nameof(provider));
        }

        return await GenerateContentStreamAsync(
            client,
            request,
            (payload, ct) => provider.Models.GenerateContentStreamAsync(payload.Model, payload.Contents, payload.Config, ct),
            options,
            cancellationToken
        ).ConfigureAwait(false);
    }

    public static async Task<GeminiStreamSummary> GenerateContentStreamAsync(
        SigilClient client,
        GenerateContentRequest request,
        Func<GenerateContentRequest, CancellationToken, IAsyncEnumerable<GenerateContentResponse>> invoke,
        GeminiSigilOptions? options = null,
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

        var effective = options ?? new GeminiSigilOptions();
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
            var summary = new GeminiStreamSummary();
            await foreach (var response in invoke(request, cancellationToken).WithCancellation(cancellationToken))
            {
                if (response != null)
                {
                    summary.Responses.Add(response);
                }
            }

            Exception? mappingError = null;
            Generation generation;
            try
            {
                generation = GeminiGenerationMapper.FromStream(request, summary, effective with { ModelName = modelName });
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

    private static string ResolveModelName(GenerateContentRequest request, GeminiSigilOptions options)
    {
        if (!string.IsNullOrWhiteSpace(options.ModelName))
        {
            return options.ModelName;
        }

        if (!string.IsNullOrWhiteSpace(request.Model))
        {
            return request.Model;
        }

        return "unknown";
    }
}
