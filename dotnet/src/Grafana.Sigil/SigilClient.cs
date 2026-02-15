using System.Diagnostics;
using System.Diagnostics.Metrics;
using System.Globalization;
using System.Reflection;
using System.Net;
using System.Text;
using System.Text.Json;
using System.Text.RegularExpressions;
using System.Threading;

namespace Grafana.Sigil;

public sealed class SigilClient : IAsyncDisposable
{
    internal const string InstrumentationName = "github.com/grafana/sigil/sdks/dotnet";
    internal const string DefaultOperationNameSync = "generateText";
    internal const string DefaultOperationNameStream = "streamText";

    internal const string SpanAttrGenerationId = "sigil.generation.id";
    internal const string SpanAttrConversationId = "gen_ai.conversation.id";
    internal const string SpanAttrAgentName = "gen_ai.agent.name";
    internal const string SpanAttrAgentVersion = "gen_ai.agent.version";
    internal const string SpanAttrErrorType = "error.type";
    internal const string SpanAttrErrorCategory = "error.category";
    internal const string SpanAttrOperationName = "gen_ai.operation.name";
    internal const string SpanAttrProviderName = "gen_ai.provider.name";
    internal const string SpanAttrRequestModel = "gen_ai.request.model";
    internal const string SpanAttrRequestMaxTokens = "gen_ai.request.max_tokens";
    internal const string SpanAttrRequestTemperature = "gen_ai.request.temperature";
    internal const string SpanAttrRequestTopP = "gen_ai.request.top_p";
    internal const string SpanAttrRequestToolChoice = "sigil.gen_ai.request.tool_choice";
    internal const string SpanAttrRequestThinkingEnabled = "sigil.gen_ai.request.thinking.enabled";
    internal const string SpanAttrRequestThinkingBudget = "sigil.gen_ai.request.thinking.budget_tokens";
    internal const string SpanAttrResponseId = "gen_ai.response.id";
    internal const string SpanAttrResponseModel = "gen_ai.response.model";
    internal const string SpanAttrFinishReasons = "gen_ai.response.finish_reasons";
    internal const string SpanAttrInputTokens = "gen_ai.usage.input_tokens";
    internal const string SpanAttrOutputTokens = "gen_ai.usage.output_tokens";
    internal const string SpanAttrCacheReadTokens = "gen_ai.usage.cache_read_input_tokens";
    internal const string SpanAttrCacheWriteTokens = "gen_ai.usage.cache_write_input_tokens";
    internal const string SpanAttrCacheCreationTokens = "gen_ai.usage.cache_creation_input_tokens";
    internal const string SpanAttrReasoningTokens = "gen_ai.usage.reasoning_tokens";
    internal const string SpanAttrToolName = "gen_ai.tool.name";
    internal const string SpanAttrToolCallId = "gen_ai.tool.call.id";
    internal const string SpanAttrToolType = "gen_ai.tool.type";
    internal const string SpanAttrToolDescription = "gen_ai.tool.description";
    internal const string SpanAttrToolCallArguments = "gen_ai.tool.call.arguments";
    internal const string SpanAttrToolCallResult = "gen_ai.tool.call.result";
    private const int MaxRatingConversationIdLen = 255;
    private const int MaxRatingIdLen = 128;
    private const int MaxRatingGenerationIdLen = 255;
    private const int MaxRatingActorIdLen = 255;
    private const int MaxRatingSourceLen = 64;
    private const int MaxRatingCommentBytes = 4096;
    private const int MaxRatingMetadataBytes = 16 * 1024;

    internal const string MetricOperationDuration = "gen_ai.client.operation.duration";
    internal const string MetricTokenUsage = "gen_ai.client.token.usage";
    internal const string MetricTimeToFirstToken = "gen_ai.client.time_to_first_token";
    internal const string MetricToolCallsPerOperation = "gen_ai.client.tool_calls_per_operation";
    internal const string MetricAttrTokenType = "gen_ai.token.type";
    internal const string MetricTokenTypeInput = "input";
    internal const string MetricTokenTypeOutput = "output";
    internal const string MetricTokenTypeCacheRead = "cache_read";
    internal const string MetricTokenTypeCacheWrite = "cache_write";
    internal const string MetricTokenTypeCacheCreation = "cache_creation";
    internal const string MetricTokenTypeReasoning = "reasoning";

    private static readonly Regex StatusCodeRegex = new(@"\b([1-5][0-9][0-9])\b", RegexOptions.Compiled);

    internal readonly SigilClientConfig _config;
    private readonly IGenerationExporter _generationExporter;
    private readonly ActivitySource _activitySource;
    private readonly Meter _meter;
    private readonly Histogram<double> _operationDurationHistogram;
    private readonly Histogram<double> _tokenUsageHistogram;
    private readonly Histogram<double> _ttftHistogram;
    private readonly Histogram<double> _toolCallsHistogram;
    private readonly Action<string> _log;
    private readonly HttpClient _ratingHttpClient = new(new HttpClientHandler
    {
        UseCookies = false,
    })
    {
        Timeout = TimeSpan.FromSeconds(10),
    };

    private readonly object _pendingLock = new();
    private readonly List<Generation> _pending = new();
    private readonly SemaphoreSlim _flushSemaphore = new(1, 1);
    private readonly object _flushBackgroundLock = new();
    private Task? _backgroundFlushTask;

    private readonly CancellationTokenSource _timerCts = new();
    private readonly Task _timerTask;

    private readonly object _stateLock = new();
    private bool _shutdown;

    public SigilClient(SigilClientConfig? config = null)
    {
        _config = ConfigResolver.Resolve(config);
        _log = _config.Logger!;

        _generationExporter = _config.GenerationExporter
            ?? _config.GenerationExport.Protocol switch
            {
                GenerationExportProtocol.Http => new HttpGenerationExporter(
                    _config.GenerationExport.Endpoint,
                    _config.GenerationExport.Headers
                ),
                GenerationExportProtocol.Grpc => new GrpcGenerationExporter(
                    _config.GenerationExport.Endpoint,
                    _config.GenerationExport.Insecure,
                    _config.GenerationExport.Headers
                ),
                GenerationExportProtocol.None => new NoopGenerationExporter(),
                _ => throw new InvalidOperationException(
                    $"unsupported generation export protocol {_config.GenerationExport.Protocol}"
                ),
            };

        _activitySource = new ActivitySource(InstrumentationName);
        _meter = new Meter(InstrumentationName);
        _operationDurationHistogram = _meter.CreateHistogram<double>(MetricOperationDuration, "s");
        _tokenUsageHistogram = _meter.CreateHistogram<double>(MetricTokenUsage, "token");
        _ttftHistogram = _meter.CreateHistogram<double>(MetricTimeToFirstToken, "s");
        _toolCallsHistogram = _meter.CreateHistogram<double>(MetricToolCallsPerOperation, "count");

        _timerTask = Task.Run(RunFlushTimerAsync);
    }

    public GenerationRecorder StartGeneration(GenerationStart start)
    {
        return StartGenerationInternal(start, GenerationMode.Sync);
    }

    public GenerationRecorder StartStreamingGeneration(GenerationStart start)
    {
        return StartGenerationInternal(start, GenerationMode.Stream);
    }

    public ToolExecutionRecorder StartToolExecution(ToolExecutionStart start)
    {
        EnsureNotShutdown();

        var seed = InternalUtils.DeepClone(start);
        seed.ToolName = (seed.ToolName ?? string.Empty).Trim();
        if (seed.ToolName.Length == 0)
        {
            return ToolExecutionRecorder.Noop;
        }

        if (string.IsNullOrWhiteSpace(seed.ConversationId))
        {
            seed.ConversationId = SigilContext.ConversationIdFromContext() ?? string.Empty;
        }

        if (string.IsNullOrWhiteSpace(seed.AgentName))
        {
            seed.AgentName = SigilContext.AgentNameFromContext() ?? string.Empty;
        }

        if (string.IsNullOrWhiteSpace(seed.AgentVersion))
        {
            seed.AgentVersion = SigilContext.AgentVersionFromContext() ?? string.Empty;
        }

        seed.StartedAt = seed.StartedAt.HasValue
            ? InternalUtils.Utc(seed.StartedAt.Value)
            : _config.UtcNow!();

        var activity = _activitySource.StartActivity(
            ToolSpanName(seed.ToolName),
            ActivityKind.Internal,
            default(ActivityContext),
            tags: null,
            links: null,
            seed.StartedAt!.Value
        );

        if (activity != null)
        {
            ApplyToolSpanAttributes(activity, seed);
        }

        return new ToolExecutionRecorder(this, seed, seed.StartedAt!.Value, seed.IncludeContent, activity);
    }

    public async Task<SubmitConversationRatingResponse> SubmitConversationRatingAsync(
        string conversationId,
        SubmitConversationRatingRequest request,
        CancellationToken cancellationToken = default
    )
    {
        EnsureNotShutdown();

        var normalizedConversationId = (conversationId ?? string.Empty).Trim();
        if (normalizedConversationId.Length == 0)
        {
            throw new ValidationException("sigil conversation rating validation failed: conversationId is required");
        }

        if (normalizedConversationId.Length > MaxRatingConversationIdLen)
        {
            throw new ValidationException("sigil conversation rating validation failed: conversationId is too long");
        }

        var normalizedRequest = NormalizeConversationRatingRequest(request);
        var endpoint = BuildConversationRatingEndpoint(
            _config.Api.Endpoint,
            _config.GenerationExport.Insecure,
            normalizedConversationId
        );

        var payload = new Dictionary<string, object?>(StringComparer.Ordinal)
        {
            ["rating_id"] = normalizedRequest.RatingId,
            ["rating"] = ToWireConversationRatingValue(normalizedRequest.Rating),
        };
        if (!string.IsNullOrWhiteSpace(normalizedRequest.Comment))
        {
            payload["comment"] = normalizedRequest.Comment;
        }
        if (normalizedRequest.Metadata.Count > 0)
        {
            payload["metadata"] = normalizedRequest.Metadata;
        }
        if (!string.IsNullOrWhiteSpace(normalizedRequest.GenerationId))
        {
            payload["generation_id"] = normalizedRequest.GenerationId;
        }
        if (!string.IsNullOrWhiteSpace(normalizedRequest.RaterId))
        {
            payload["rater_id"] = normalizedRequest.RaterId;
        }
        if (!string.IsNullOrWhiteSpace(normalizedRequest.Source))
        {
            payload["source"] = normalizedRequest.Source;
        }

        var body = JsonSerializer.Serialize(payload);
        using var httpRequest = new HttpRequestMessage(HttpMethod.Post, endpoint)
        {
            Content = new StringContent(body, Encoding.UTF8, "application/json"),
        };
        foreach (var header in _config.GenerationExport.Headers)
        {
            httpRequest.Headers.TryAddWithoutValidation(header.Key, header.Value);
        }

        HttpResponseMessage response;
        try
        {
            response = await _ratingHttpClient.SendAsync(httpRequest, cancellationToken).ConfigureAwait(false);
        }
        catch (Exception ex)
        {
            throw new RatingTransportException("sigil conversation rating transport failed", ex);
        }
        using (response)
        {
            var responseBody = (await ReadResponseBodyAsync(response.Content, cancellationToken).ConfigureAwait(false)).Trim();
            if (response.StatusCode == HttpStatusCode.BadRequest)
            {
                throw new ValidationException(
                    $"sigil conversation rating validation failed: {RatingErrorText(responseBody, (int)response.StatusCode)}"
                );
            }
            if (response.StatusCode == HttpStatusCode.Conflict)
            {
                throw new RatingConflictException(
                    $"sigil conversation rating conflict: {RatingErrorText(responseBody, (int)response.StatusCode)}"
                );
            }
            if (!response.IsSuccessStatusCode)
            {
                throw new RatingTransportException(
                    $"sigil conversation rating transport failed: status {(int)response.StatusCode}: {RatingErrorText(responseBody, (int)response.StatusCode)}"
                );
            }
            if (string.IsNullOrWhiteSpace(responseBody))
            {
                throw new RatingTransportException("sigil conversation rating transport failed: empty response payload");
            }

            return ParseSubmitConversationRatingResponse(responseBody);
        }
    }

    public async Task FlushAsync(CancellationToken cancellationToken = default)
    {
        EnsureNotShutdown();
        await FlushInternalAsync(cancellationToken).ConfigureAwait(false);
    }

    public async Task ShutdownAsync(CancellationToken cancellationToken = default)
    {
        lock (_stateLock)
        {
            if (_shutdown)
            {
                return;
            }

            _shutdown = true;
        }

        _timerCts.Cancel();

        try
        {
            await _timerTask.ConfigureAwait(false);
        }
        catch (OperationCanceledException)
        {
            // Ignore.
        }

        try
        {
            await FlushInternalAsync(cancellationToken).ConfigureAwait(false);
        }
        catch (Exception ex)
        {
            _log($"sigil generation export flush on shutdown failed: {ex}");
        }

        try
        {
            await _generationExporter.ShutdownAsync(cancellationToken).ConfigureAwait(false);
        }
        catch (Exception ex)
        {
            _log($"sigil generation exporter shutdown failed: {ex}");
        }

        _activitySource.Dispose();
        _meter.Dispose();
        _ratingHttpClient.Dispose();
    }

    public async ValueTask DisposeAsync()
    {
        await ShutdownAsync().ConfigureAwait(false);
    }

    private GenerationRecorder StartGenerationInternal(GenerationStart start, GenerationMode defaultMode)
    {
        EnsureNotShutdown();

        var seed = InternalUtils.DeepClone(start);

        if (seed.Mode == null)
        {
            seed.Mode = defaultMode;
        }

        if (string.IsNullOrWhiteSpace(seed.OperationName))
        {
            seed.OperationName = DefaultOperationNameForMode(seed.Mode!.Value);
        }

        if (string.IsNullOrWhiteSpace(seed.ConversationId))
        {
            seed.ConversationId = SigilContext.ConversationIdFromContext() ?? string.Empty;
        }

        if (string.IsNullOrWhiteSpace(seed.AgentName))
        {
            seed.AgentName = SigilContext.AgentNameFromContext() ?? string.Empty;
        }

        if (string.IsNullOrWhiteSpace(seed.AgentVersion))
        {
            seed.AgentVersion = SigilContext.AgentVersionFromContext() ?? string.Empty;
        }

        seed.StartedAt = seed.StartedAt.HasValue
            ? InternalUtils.Utc(seed.StartedAt.Value)
            : _config.UtcNow!();

        var activity = _activitySource.StartActivity(
            GenerationSpanName(seed.OperationName, seed.Model.Name),
            ActivityKind.Client,
            default(ActivityContext),
            tags: null,
            links: null,
            seed.StartedAt!.Value
        );

        if (activity != null)
        {
            ApplyGenerationSpanAttributes(activity, new Generation
            {
                Id = seed.Id,
                ConversationId = seed.ConversationId,
                AgentName = seed.AgentName,
                AgentVersion = seed.AgentVersion,
                Mode = seed.Mode,
                OperationName = seed.OperationName,
                Model = InternalUtils.DeepClone(seed.Model),
                MaxTokens = seed.MaxTokens,
                Temperature = seed.Temperature,
                TopP = seed.TopP,
                ToolChoice = seed.ToolChoice,
                ThinkingEnabled = seed.ThinkingEnabled,
            });
        }

        return new GenerationRecorder(this, seed, seed.StartedAt!.Value, activity);
    }

    internal void PersistGeneration(Generation generation)
    {
        try
        {
            GenerationValidator.Validate(generation);
        }
        catch (Exception ex)
        {
            throw new ValidationException($"sigil: generation validation failed: {ex.Message}");
        }

        var proto = ProtoMapping.ToProto(generation);
        if (_config.GenerationExport.PayloadMaxBytes > 0)
        {
            var payloadSize = proto.CalculateSize();
            if (payloadSize > _config.GenerationExport.PayloadMaxBytes)
            {
                throw new EnqueueException(
                    $"generation payload exceeds max bytes ({payloadSize} > {_config.GenerationExport.PayloadMaxBytes})"
                );
            }
        }

        lock (_stateLock)
        {
            if (_shutdown)
            {
                throw new ClientShutdownException("sigil: client is shutting down");
            }
        }

        var shouldTriggerFlush = false;
        lock (_pendingLock)
        {
            if (_pending.Count >= _config.GenerationExport.QueueSize)
            {
                throw new QueueFullException("sigil: generation queue is full");
            }

            _pending.Add(InternalUtils.DeepClone(generation));
            shouldTriggerFlush = _pending.Count >= _config.GenerationExport.BatchSize;
        }

        if (shouldTriggerFlush)
        {
            TriggerBackgroundFlush();
        }
    }

    private void TriggerBackgroundFlush()
    {
        lock (_flushBackgroundLock)
        {
            if (_backgroundFlushTask is { IsCompleted: false })
            {
                return;
            }

            _backgroundFlushTask = Task.Run(async () =>
            {
                try
                {
                    await FlushInternalAsync(CancellationToken.None).ConfigureAwait(false);
                }
                catch (Exception ex)
                {
                    _log($"sigil generation export failed: {ex}");
                }
            });
        }
    }

    private async Task RunFlushTimerAsync()
    {
        while (!_timerCts.IsCancellationRequested)
        {
            try
            {
                await Task.Delay(_config.GenerationExport.FlushInterval, _timerCts.Token).ConfigureAwait(false);
            }
            catch (OperationCanceledException)
            {
                break;
            }

            TriggerBackgroundFlush();
        }
    }

    private async Task FlushInternalAsync(CancellationToken cancellationToken)
    {
        await _flushSemaphore.WaitAsync(cancellationToken).ConfigureAwait(false);
        try
        {
            while (true)
            {
                List<Generation> batch;
                lock (_pendingLock)
                {
                    if (_pending.Count == 0)
                    {
                        return;
                    }

                    var count = Math.Min(_pending.Count, _config.GenerationExport.BatchSize);
                    batch = _pending.Take(count).Select(InternalUtils.DeepClone).ToList();
                    _pending.RemoveRange(0, count);
                }

                var response = await ExportWithRetryAsync(new ExportGenerationsRequest { Generations = batch }, cancellationToken)
                    .ConfigureAwait(false);

                foreach (var result in response.Results)
                {
                    if (!result.Accepted)
                    {
                        _log($"sigil generation rejected id={result.GenerationId} error={result.Error}");
                    }
                }
            }
        }
        finally
        {
            _flushSemaphore.Release();
        }
    }

    private async Task<ExportGenerationsResponse> ExportWithRetryAsync(
        ExportGenerationsRequest request,
        CancellationToken cancellationToken
    )
    {
        var attempts = _config.GenerationExport.MaxRetries + 1;
        var backoff = _config.GenerationExport.InitialBackoff;
        var maxBackoff = _config.GenerationExport.MaxBackoff;
        if (backoff <= TimeSpan.Zero)
        {
            backoff = TimeSpan.FromMilliseconds(100);
        }

        Exception? lastError = null;
        for (var attempt = 0; attempt < attempts; attempt++)
        {
            try
            {
                return await _generationExporter.ExportGenerationsAsync(request, cancellationToken).ConfigureAwait(false);
            }
            catch (Exception ex)
            {
                lastError = ex;
                if (attempt == attempts - 1)
                {
                    break;
                }

                await _config.SleepAsync!(backoff, cancellationToken).ConfigureAwait(false);
                var next = backoff + backoff;
                backoff = next > maxBackoff ? maxBackoff : next;
            }
        }

        throw lastError ?? new InvalidOperationException("generation export failed");
    }

    private void EnsureNotShutdown()
    {
        lock (_stateLock)
        {
            if (_shutdown)
            {
                throw new ClientShutdownException("sigil: client is shutting down");
            }
        }
    }

    private static SubmitConversationRatingRequest NormalizeConversationRatingRequest(SubmitConversationRatingRequest request)
    {
        var input = request ?? new SubmitConversationRatingRequest();
        var normalized = new SubmitConversationRatingRequest
        {
            RatingId = (input.RatingId ?? string.Empty).Trim(),
            Rating = input.Rating,
            Comment = (input.Comment ?? string.Empty).Trim(),
            Metadata = input.Metadata != null
                ? new Dictionary<string, object?>(input.Metadata, StringComparer.Ordinal)
                : new Dictionary<string, object?>(StringComparer.Ordinal),
            GenerationId = (input.GenerationId ?? string.Empty).Trim(),
            RaterId = (input.RaterId ?? string.Empty).Trim(),
            Source = (input.Source ?? string.Empty).Trim(),
        };

        if (normalized.RatingId.Length == 0)
        {
            throw new ValidationException("sigil conversation rating validation failed: ratingId is required");
        }
        if (normalized.RatingId.Length > MaxRatingIdLen)
        {
            throw new ValidationException("sigil conversation rating validation failed: ratingId is too long");
        }
        if (normalized.Rating != ConversationRatingValue.Good && normalized.Rating != ConversationRatingValue.Bad)
        {
            throw new ValidationException(
                "sigil conversation rating validation failed: rating must be CONVERSATION_RATING_VALUE_GOOD or CONVERSATION_RATING_VALUE_BAD"
            );
        }
        if (Encoding.UTF8.GetByteCount(normalized.Comment) > MaxRatingCommentBytes)
        {
            throw new ValidationException("sigil conversation rating validation failed: comment is too long");
        }
        if (normalized.GenerationId.Length > MaxRatingGenerationIdLen)
        {
            throw new ValidationException("sigil conversation rating validation failed: generationId is too long");
        }
        if (normalized.RaterId.Length > MaxRatingActorIdLen)
        {
            throw new ValidationException("sigil conversation rating validation failed: raterId is too long");
        }
        if (normalized.Source.Length > MaxRatingSourceLen)
        {
            throw new ValidationException("sigil conversation rating validation failed: source is too long");
        }

        if (normalized.Metadata.Count > 0)
        {
            byte[] metadataBytes;
            try
            {
                metadataBytes = JsonSerializer.SerializeToUtf8Bytes(normalized.Metadata);
            }
            catch (Exception ex)
            {
                throw new ValidationException($"sigil conversation rating validation failed: metadata must be valid JSON ({ex.Message})");
            }

            if (metadataBytes.Length > MaxRatingMetadataBytes)
            {
                throw new ValidationException("sigil conversation rating validation failed: metadata is too large");
            }
        }

        return normalized;
    }

    private static string BuildConversationRatingEndpoint(string apiEndpoint, bool insecure, string conversationId)
    {
        var baseUrl = BuildRatingBaseUrl(apiEndpoint, insecure);
        return $"{baseUrl}/api/v1/conversations/{Uri.EscapeDataString(conversationId)}/ratings";
    }

    private static string BuildRatingBaseUrl(string apiEndpoint, bool insecure)
    {
        var trimmedEndpoint = (apiEndpoint ?? string.Empty).Trim();
        if (trimmedEndpoint.Length == 0)
        {
            throw new RatingTransportException("sigil conversation rating transport failed: api endpoint is required");
        }

        if (trimmedEndpoint.StartsWith("http://", StringComparison.OrdinalIgnoreCase)
            || trimmedEndpoint.StartsWith("https://", StringComparison.OrdinalIgnoreCase))
        {
            if (!Uri.TryCreate(trimmedEndpoint, UriKind.Absolute, out var parsed) || string.IsNullOrWhiteSpace(parsed.Host))
            {
                throw new RatingTransportException(
                    "sigil conversation rating transport failed: api endpoint host is required"
                );
            }

            return $"{parsed.Scheme}://{parsed.Authority}";
        }

        var host = trimmedEndpoint;
        if (host.StartsWith("grpc://", StringComparison.OrdinalIgnoreCase))
        {
            host = host.Substring("grpc://".Length);
        }
        var slashIndex = host.IndexOf('/');
        if (slashIndex >= 0)
        {
            host = host.Substring(0, slashIndex);
        }
        host = host.Trim();
        if (host.Length == 0)
        {
            throw new RatingTransportException(
                "sigil conversation rating transport failed: api endpoint host is required"
            );
        }

        var scheme = insecure ? "http" : "https";
        return $"{scheme}://{host}";
    }

    private static SubmitConversationRatingResponse ParseSubmitConversationRatingResponse(string payload)
    {
        try
        {
            using var document = JsonDocument.Parse(payload);
            if (document.RootElement.ValueKind != JsonValueKind.Object)
            {
                throw new RatingTransportException("sigil conversation rating transport failed: invalid response payload");
            }

            var ratingElement = GetRequiredProperty(document.RootElement, "rating");
            var summaryElement = GetRequiredProperty(document.RootElement, "summary");

            return new SubmitConversationRatingResponse
            {
                Rating = ParseConversationRating(ratingElement),
                Summary = ParseConversationRatingSummary(summaryElement),
            };
        }
        catch (RatingTransportException)
        {
            throw;
        }
        catch (JsonException ex)
        {
            throw new RatingTransportException("sigil conversation rating transport failed: invalid JSON response", ex);
        }
    }

    private static ConversationRating ParseConversationRating(JsonElement element)
    {
        if (element.ValueKind != JsonValueKind.Object)
        {
            throw new RatingTransportException("sigil conversation rating transport failed: invalid rating payload");
        }

        var rating = new ConversationRating
        {
            RatingId = GetRequiredString(element, "rating_id"),
            ConversationId = GetRequiredString(element, "conversation_id"),
            Rating = ParseWireConversationRatingValue(GetRequiredString(element, "rating")),
            CreatedAt = ParseRequiredTimestamp(element, "created_at"),
        };

        if (TryGetOptionalString(element, "comment", out var comment))
        {
            rating.Comment = comment;
        }
        if (TryGetProperty(element, "metadata", out var metadataElement))
        {
            rating.Metadata = metadataElement.ValueKind switch
            {
                JsonValueKind.Object => ParseMetadataObject(metadataElement),
                JsonValueKind.Null => new Dictionary<string, object?>(StringComparer.Ordinal),
                _ => throw new RatingTransportException("sigil conversation rating transport failed: invalid rating payload"),
            };
        }
        if (TryGetOptionalString(element, "generation_id", out var generationId))
        {
            rating.GenerationId = generationId;
        }
        if (TryGetOptionalString(element, "rater_id", out var raterId))
        {
            rating.RaterId = raterId;
        }
        if (TryGetOptionalString(element, "source", out var source))
        {
            rating.Source = source;
        }

        return rating;
    }

    private static ConversationRatingSummary ParseConversationRatingSummary(JsonElement element)
    {
        if (element.ValueKind != JsonValueKind.Object)
        {
            throw new RatingTransportException("sigil conversation rating transport failed: invalid rating summary payload");
        }

        ConversationRatingValue? latestRating = null;
        if (TryGetProperty(element, "latest_rating", out var latestRatingElement))
        {
            latestRating = latestRatingElement.ValueKind switch
            {
                JsonValueKind.String => ParseWireConversationRatingValue(latestRatingElement.GetString() ?? string.Empty),
                JsonValueKind.Null => null,
                _ => throw new RatingTransportException(
                    "sigil conversation rating transport failed: invalid rating summary payload"
                ),
            };
        }

        DateTimeOffset? latestBadAt = null;
        if (TryGetProperty(element, "latest_bad_at", out var latestBadAtElement))
        {
            latestBadAt = latestBadAtElement.ValueKind switch
            {
                JsonValueKind.String => ParseTimestamp(latestBadAtElement.GetString() ?? string.Empty),
                JsonValueKind.Null => null,
                _ => throw new RatingTransportException(
                    "sigil conversation rating transport failed: invalid rating summary payload"
                ),
            };
        }

        return new ConversationRatingSummary
        {
            TotalCount = GetRequiredInt(element, "total_count"),
            GoodCount = GetRequiredInt(element, "good_count"),
            BadCount = GetRequiredInt(element, "bad_count"),
            LatestRating = latestRating,
            LatestRatedAt = ParseRequiredTimestamp(element, "latest_rated_at"),
            LatestBadAt = latestBadAt,
            HasBadRating = GetRequiredBool(element, "has_bad_rating"),
        };
    }

    private static ConversationRatingValue ParseWireConversationRatingValue(string value)
    {
        return value switch
        {
            "CONVERSATION_RATING_VALUE_GOOD" => ConversationRatingValue.Good,
            "CONVERSATION_RATING_VALUE_BAD" => ConversationRatingValue.Bad,
            _ => throw new RatingTransportException("sigil conversation rating transport failed: invalid rating payload"),
        };
    }

    private static string ToWireConversationRatingValue(ConversationRatingValue value)
    {
        return value switch
        {
            ConversationRatingValue.Good => "CONVERSATION_RATING_VALUE_GOOD",
            ConversationRatingValue.Bad => "CONVERSATION_RATING_VALUE_BAD",
            _ => throw new ValidationException(
                "sigil conversation rating validation failed: rating must be CONVERSATION_RATING_VALUE_GOOD or CONVERSATION_RATING_VALUE_BAD"
            ),
        };
    }

    private static string RatingErrorText(string body, int statusCode)
    {
        var trimmed = (body ?? string.Empty).Trim();
        if (trimmed.Length > 0)
        {
            return trimmed;
        }

        if (Enum.IsDefined(typeof(HttpStatusCode), statusCode))
        {
            return ((HttpStatusCode)statusCode).ToString();
        }

        return "status " + statusCode.ToString(CultureInfo.InvariantCulture);
    }

    private static JsonElement GetRequiredProperty(JsonElement element, string name)
    {
        if (!TryGetProperty(element, name, out var value))
        {
            throw new RatingTransportException("sigil conversation rating transport failed: invalid response payload");
        }

        return value;
    }

    private static bool TryGetProperty(JsonElement element, string name, out JsonElement value)
    {
        foreach (var property in element.EnumerateObject())
        {
            if (string.Equals(property.Name, name, StringComparison.Ordinal))
            {
                value = property.Value;
                return true;
            }
        }

        value = default;
        return false;
    }

    private static string GetRequiredString(JsonElement element, string name)
    {
        if (!TryGetProperty(element, name, out var value) || value.ValueKind != JsonValueKind.String)
        {
            throw new RatingTransportException("sigil conversation rating transport failed: invalid response payload");
        }

        var text = (value.GetString() ?? string.Empty).Trim();
        if (text.Length == 0)
        {
            throw new RatingTransportException("sigil conversation rating transport failed: invalid response payload");
        }

        return text;
    }

    private static bool TryGetOptionalString(JsonElement element, string name, out string value)
    {
        value = string.Empty;
        if (!TryGetProperty(element, name, out var raw))
        {
            return false;
        }

        if (raw.ValueKind == JsonValueKind.Null)
        {
            return false;
        }
        if (raw.ValueKind != JsonValueKind.String)
        {
            throw new RatingTransportException("sigil conversation rating transport failed: invalid response payload");
        }

        value = (raw.GetString() ?? string.Empty).Trim();
        return true;
    }

    private static int GetRequiredInt(JsonElement element, string name)
    {
        if (!TryGetProperty(element, name, out var value) || value.ValueKind != JsonValueKind.Number || !value.TryGetInt32(out var parsed))
        {
            throw new RatingTransportException("sigil conversation rating transport failed: invalid response payload");
        }

        return parsed;
    }

    private static bool GetRequiredBool(JsonElement element, string name)
    {
        if (!TryGetProperty(element, name, out var value)
            || (value.ValueKind != JsonValueKind.True && value.ValueKind != JsonValueKind.False))
        {
            throw new RatingTransportException("sigil conversation rating transport failed: invalid response payload");
        }

        return value.GetBoolean();
    }

    private static DateTimeOffset ParseRequiredTimestamp(JsonElement element, string name)
    {
        if (!TryGetProperty(element, name, out var value) || value.ValueKind != JsonValueKind.String)
        {
            throw new RatingTransportException("sigil conversation rating transport failed: invalid response payload");
        }

        return ParseTimestamp(value.GetString() ?? string.Empty);
    }

    private static DateTimeOffset ParseTimestamp(string raw)
    {
        if (!DateTimeOffset.TryParse(raw, CultureInfo.InvariantCulture, DateTimeStyles.RoundtripKind, out var timestamp))
        {
            throw new RatingTransportException("sigil conversation rating transport failed: invalid timestamp in response payload");
        }

        return timestamp;
    }

    private static Dictionary<string, object?> ParseMetadataObject(JsonElement value)
    {
        try
        {
            var metadata = JsonSerializer.Deserialize<Dictionary<string, object?>>(value.GetRawText());
            return metadata != null
                ? new Dictionary<string, object?>(metadata, StringComparer.Ordinal)
                : new Dictionary<string, object?>(StringComparer.Ordinal);
        }
        catch (Exception ex)
        {
            throw new RatingTransportException("sigil conversation rating transport failed: invalid rating payload", ex);
        }
    }

    internal static string DefaultOperationNameForMode(GenerationMode mode)
    {
        return mode == GenerationMode.Stream ? DefaultOperationNameStream : DefaultOperationNameSync;
    }

    internal static string GenerationSpanName(string operationName, string modelName)
    {
        var operation = string.IsNullOrWhiteSpace(operationName) ? DefaultOperationNameSync : operationName.Trim();
        var model = modelName?.Trim() ?? string.Empty;
        return model.Length == 0 ? operation : operation + " " + model;
    }

    internal static string ToolSpanName(string toolName)
    {
        return "execute_tool " + toolName;
    }

    internal static void ApplyGenerationSpanAttributes(Activity activity, Generation generation)
    {
        activity.SetTag(SpanAttrOperationName, OperationName(generation));

        if (!string.IsNullOrWhiteSpace(generation.Id))
        {
            activity.SetTag(SpanAttrGenerationId, generation.Id);
        }

        if (!string.IsNullOrWhiteSpace(generation.ConversationId))
        {
            activity.SetTag(SpanAttrConversationId, generation.ConversationId);
        }

        if (!string.IsNullOrWhiteSpace(generation.AgentName))
        {
            activity.SetTag(SpanAttrAgentName, generation.AgentName);
        }

        if (!string.IsNullOrWhiteSpace(generation.AgentVersion))
        {
            activity.SetTag(SpanAttrAgentVersion, generation.AgentVersion);
        }

        if (!string.IsNullOrWhiteSpace(generation.Model.Provider))
        {
            activity.SetTag(SpanAttrProviderName, generation.Model.Provider);
        }

        if (!string.IsNullOrWhiteSpace(generation.Model.Name))
        {
            activity.SetTag(SpanAttrRequestModel, generation.Model.Name);
        }

        if (generation.MaxTokens.HasValue)
        {
            activity.SetTag(SpanAttrRequestMaxTokens, generation.MaxTokens.Value);
        }

        if (generation.Temperature.HasValue)
        {
            activity.SetTag(SpanAttrRequestTemperature, generation.Temperature.Value);
        }

        if (generation.TopP.HasValue)
        {
            activity.SetTag(SpanAttrRequestTopP, generation.TopP.Value);
        }

        if (!string.IsNullOrWhiteSpace(generation.ToolChoice))
        {
            activity.SetTag(SpanAttrRequestToolChoice, generation.ToolChoice);
        }

        if (generation.ThinkingEnabled.HasValue)
        {
            activity.SetTag(SpanAttrRequestThinkingEnabled, generation.ThinkingEnabled.Value);
        }
        if (TryGetThinkingBudgetFromMetadata(generation.Metadata, out var thinkingBudget))
        {
            activity.SetTag(SpanAttrRequestThinkingBudget, thinkingBudget);
        }

        if (!string.IsNullOrWhiteSpace(generation.ResponseId))
        {
            activity.SetTag(SpanAttrResponseId, generation.ResponseId);
        }

        if (!string.IsNullOrWhiteSpace(generation.ResponseModel))
        {
            activity.SetTag(SpanAttrResponseModel, generation.ResponseModel);
        }

        if (!string.IsNullOrWhiteSpace(generation.StopReason))
        {
            activity.SetTag(SpanAttrFinishReasons, new[] { generation.StopReason });
        }

        if (generation.Usage.InputTokens != 0)
        {
            activity.SetTag(SpanAttrInputTokens, generation.Usage.InputTokens);
        }

        if (generation.Usage.OutputTokens != 0)
        {
            activity.SetTag(SpanAttrOutputTokens, generation.Usage.OutputTokens);
        }

        if (generation.Usage.CacheReadInputTokens != 0)
        {
            activity.SetTag(SpanAttrCacheReadTokens, generation.Usage.CacheReadInputTokens);
        }

        if (generation.Usage.CacheWriteInputTokens != 0)
        {
            activity.SetTag(SpanAttrCacheWriteTokens, generation.Usage.CacheWriteInputTokens);
        }

        if (generation.Usage.CacheCreationInputTokens != 0)
        {
            activity.SetTag(SpanAttrCacheCreationTokens, generation.Usage.CacheCreationInputTokens);
        }

        if (generation.Usage.ReasoningTokens != 0)
        {
            activity.SetTag(SpanAttrReasoningTokens, generation.Usage.ReasoningTokens);
        }
    }

    internal static void ApplyToolSpanAttributes(Activity activity, ToolExecutionStart tool)
    {
        activity.SetTag(SpanAttrOperationName, "execute_tool");
        activity.SetTag(SpanAttrToolName, tool.ToolName);

        if (!string.IsNullOrWhiteSpace(tool.ToolCallId))
        {
            activity.SetTag(SpanAttrToolCallId, tool.ToolCallId);
        }

        if (!string.IsNullOrWhiteSpace(tool.ToolType))
        {
            activity.SetTag(SpanAttrToolType, tool.ToolType);
        }

        if (!string.IsNullOrWhiteSpace(tool.ToolDescription))
        {
            activity.SetTag(SpanAttrToolDescription, tool.ToolDescription);
        }

        if (!string.IsNullOrWhiteSpace(tool.ConversationId))
        {
            activity.SetTag(SpanAttrConversationId, tool.ConversationId);
        }

        if (!string.IsNullOrWhiteSpace(tool.AgentName))
        {
            activity.SetTag(SpanAttrAgentName, tool.AgentName);
        }

        if (!string.IsNullOrWhiteSpace(tool.AgentVersion))
        {
            activity.SetTag(SpanAttrAgentVersion, tool.AgentVersion);
        }
    }

    internal static string OperationName(Generation generation)
    {
        if (!string.IsNullOrWhiteSpace(generation.OperationName))
        {
            return generation.OperationName;
        }

        return DefaultOperationNameForMode(generation.Mode ?? GenerationMode.Sync);
    }

    internal void RecordGenerationMetrics(
        Generation generation,
        string errorType,
        string errorCategory,
        DateTimeOffset? firstTokenAt
    )
    {
        if (!generation.StartedAt.HasValue || !generation.CompletedAt.HasValue)
        {
            return;
        }

        var startedAt = generation.StartedAt.Value;
        var completedAt = generation.CompletedAt.Value;
        var durationSeconds = Math.Max(0d, (completedAt - startedAt).TotalSeconds);

        _operationDurationHistogram.Record(
            durationSeconds,
            new KeyValuePair<string, object?>[]
            {
                new(SpanAttrOperationName, OperationName(generation)),
                new(SpanAttrProviderName, generation.Model.Provider ?? string.Empty),
                new(SpanAttrRequestModel, generation.Model.Name ?? string.Empty),
                new(SpanAttrAgentName, generation.AgentName ?? string.Empty),
                new(SpanAttrErrorType, errorType ?? string.Empty),
                new(SpanAttrErrorCategory, errorCategory ?? string.Empty),
            });

        RecordTokenUsage(generation, MetricTokenTypeInput, generation.Usage.InputTokens);
        RecordTokenUsage(generation, MetricTokenTypeOutput, generation.Usage.OutputTokens);
        RecordTokenUsage(generation, MetricTokenTypeCacheRead, generation.Usage.CacheReadInputTokens);
        RecordTokenUsage(generation, MetricTokenTypeCacheWrite, generation.Usage.CacheWriteInputTokens);
        RecordTokenUsage(generation, MetricTokenTypeCacheCreation, generation.Usage.CacheCreationInputTokens);
        RecordTokenUsage(generation, MetricTokenTypeReasoning, generation.Usage.ReasoningTokens);

        _toolCallsHistogram.Record(
            CountToolCallParts(generation.Output),
            new KeyValuePair<string, object?>[]
            {
                new(SpanAttrProviderName, generation.Model.Provider ?? string.Empty),
                new(SpanAttrRequestModel, generation.Model.Name ?? string.Empty),
                new(SpanAttrAgentName, generation.AgentName ?? string.Empty),
            });

        if (string.Equals(OperationName(generation), DefaultOperationNameStream, StringComparison.Ordinal)
            && firstTokenAt.HasValue)
        {
            var ttftSeconds = (firstTokenAt.Value - startedAt).TotalSeconds;
            if (ttftSeconds >= 0d)
            {
                _ttftHistogram.Record(
                    ttftSeconds,
                    new KeyValuePair<string, object?>[]
                    {
                        new(SpanAttrProviderName, generation.Model.Provider ?? string.Empty),
                        new(SpanAttrRequestModel, generation.Model.Name ?? string.Empty),
                        new(SpanAttrAgentName, generation.AgentName ?? string.Empty),
                    });
            }
        }
    }

    internal void RecordToolExecutionMetrics(
        ToolExecutionStart seed,
        DateTimeOffset startedAt,
        DateTimeOffset completedAt,
        Exception? finalError
    )
    {
        var durationSeconds = Math.Max(0d, (completedAt - startedAt).TotalSeconds);
        var errorType = finalError == null ? string.Empty : "tool_execution_error";
        var errorCategory = finalError == null ? string.Empty : ErrorCategoryFromException(finalError, true);

        _operationDurationHistogram.Record(
            durationSeconds,
            new KeyValuePair<string, object?>[]
            {
                new(SpanAttrOperationName, "execute_tool"),
                new(SpanAttrProviderName, string.Empty),
                new(SpanAttrRequestModel, seed.ToolName ?? string.Empty),
                new(SpanAttrAgentName, seed.AgentName ?? string.Empty),
                new(SpanAttrErrorType, errorType),
                new(SpanAttrErrorCategory, errorCategory),
            });
    }

    internal static string ErrorCategoryFromException(Exception? error, bool fallbackSdk)
    {
        if (error == null)
        {
            return fallbackSdk ? "sdk_error" : string.Empty;
        }

        if (error is TimeoutException or OperationCanceledException)
        {
            return "timeout";
        }

        var message = error.Message ?? string.Empty;
        if (message.IndexOf("timeout", StringComparison.OrdinalIgnoreCase) >= 0
            || message.IndexOf("deadline exceeded", StringComparison.OrdinalIgnoreCase) >= 0)
        {
            return "timeout";
        }

        var statusCode = ExtractStatusCode(error);
        if (statusCode == 429)
        {
            return "rate_limit";
        }

        if (statusCode is 401 or 403)
        {
            return "auth_error";
        }

        if (statusCode == 408)
        {
            return "timeout";
        }

        if (statusCode.HasValue && statusCode.Value >= 500 && statusCode.Value <= 599)
        {
            return "server_error";
        }

        if (statusCode.HasValue && statusCode.Value >= 400 && statusCode.Value <= 499)
        {
            return "client_error";
        }

        return fallbackSdk ? "sdk_error" : string.Empty;
    }

    private void RecordTokenUsage(Generation generation, string tokenType, long value)
    {
        if (value == 0L)
        {
            return;
        }

        _tokenUsageHistogram.Record(
            value,
            new KeyValuePair<string, object?>[]
            {
                new(SpanAttrProviderName, generation.Model.Provider ?? string.Empty),
                new(SpanAttrRequestModel, generation.Model.Name ?? string.Empty),
                new(SpanAttrAgentName, generation.AgentName ?? string.Empty),
                new(MetricAttrTokenType, tokenType),
            });
    }

    private static long CountToolCallParts(IReadOnlyList<Message> messages)
    {
        long total = 0;
        foreach (var message in messages)
        {
            foreach (var part in message.Parts)
            {
                if (part.Kind == PartKind.ToolCall)
                {
                    total++;
                }
            }
        }

        return total;
    }

    private static int? ExtractStatusCode(Exception error)
    {
        var direct = ReadStatusCodeValue(error);
        if (direct.HasValue)
        {
            return direct;
        }

        foreach (var propertyName in new[] { "Response", "Error" })
        {
            var property = error.GetType().GetProperty(propertyName, BindingFlags.Public | BindingFlags.NonPublic | BindingFlags.Instance);
            var nested = property?.GetValue(error);
            if (nested != null)
            {
                var nestedValue = ReadStatusCodeValue(nested);
                if (nestedValue.HasValue)
                {
                    return nestedValue;
                }
            }
        }

        var matches = StatusCodeRegex.Matches(error.Message ?? string.Empty);
        foreach (Match match in matches)
        {
            if (int.TryParse(match.Value, NumberStyles.Integer, CultureInfo.InvariantCulture, out var parsed)
                && parsed is >= 100 and <= 599)
            {
                return parsed;
            }
        }

        return null;
    }

    private static int? ReadStatusCodeValue(object value)
    {
        foreach (var memberName in new[] { "StatusCode", "Status", "statusCode", "status" })
        {
            var property = value.GetType().GetProperty(memberName, BindingFlags.Public | BindingFlags.NonPublic | BindingFlags.Instance);
            if (property != null)
            {
                var parsed = ConvertToStatusCode(property.GetValue(value));
                if (parsed.HasValue)
                {
                    return parsed;
                }
            }

            var field = value.GetType().GetField(memberName, BindingFlags.Public | BindingFlags.NonPublic | BindingFlags.Instance);
            if (field != null)
            {
                var parsed = ConvertToStatusCode(field.GetValue(value));
                if (parsed.HasValue)
                {
                    return parsed;
                }
            }
        }

        return null;
    }

    private static int? ConvertToStatusCode(object? value)
    {
        if (value == null)
        {
            return null;
        }

        if (value is int statusCode)
        {
            return statusCode is >= 100 and <= 599 ? statusCode : null;
        }

        if (value is long longStatus && longStatus is >= 100 and <= 599)
        {
            return (int)longStatus;
        }

        if (value is string text
            && int.TryParse(text, NumberStyles.Integer, CultureInfo.InvariantCulture, out var parsed)
            && parsed is >= 100 and <= 599)
        {
            return parsed;
        }

        return null;
    }

    private static bool TryGetThinkingBudgetFromMetadata(
        IReadOnlyDictionary<string, object?> metadata,
        out long thinkingBudget
    )
    {
        thinkingBudget = 0;
        if (!metadata.TryGetValue(SpanAttrRequestThinkingBudget, out var raw) || raw == null)
        {
            return false;
        }

        switch (raw)
        {
            case long value:
                thinkingBudget = value;
                return true;
            case int value:
                thinkingBudget = value;
                return true;
            case short value:
                thinkingBudget = value;
                return true;
            case byte value:
                thinkingBudget = value;
                return true;
            case ulong value when value <= long.MaxValue:
                thinkingBudget = (long)value;
                return true;
            case uint value:
                thinkingBudget = value;
                return true;
            case ushort value:
                thinkingBudget = value;
                return true;
            case sbyte value:
                thinkingBudget = value;
                return true;
            case double value when value % 1 == 0 && value >= long.MinValue && value <= long.MaxValue:
                thinkingBudget = (long)value;
                return true;
            case float value when value % 1 == 0 && value >= long.MinValue && value <= long.MaxValue:
                thinkingBudget = (long)value;
                return true;
            case decimal value when decimal.Truncate(value) == value && value >= long.MinValue && value <= long.MaxValue:
                thinkingBudget = (long)value;
                return true;
            case JsonElement json:
                if (json.ValueKind == JsonValueKind.Number && json.TryGetInt64(out var jsonInt))
                {
                    thinkingBudget = jsonInt;
                    return true;
                }
                if (json.ValueKind == JsonValueKind.String
                    && long.TryParse(json.GetString(), NumberStyles.Integer, CultureInfo.InvariantCulture, out var jsonParsed))
                {
                    thinkingBudget = jsonParsed;
                    return true;
                }
                return false;
            case string text:
                return long.TryParse(text, NumberStyles.Integer, CultureInfo.InvariantCulture, out thinkingBudget);
            default:
                return false;
        }
    }

    internal static void RecordException(Activity activity, Exception error)
    {
        if (activity == null || error == null)
        {
            return;
        }

        activity.SetTag("exception.type", error.GetType().FullName);
        activity.SetTag("exception.message", error.Message);
        activity.SetTag("exception.stacktrace", error.ToString());
    }

    private static Task<string> ReadResponseBodyAsync(HttpContent content, CancellationToken cancellationToken)
    {
#if NETSTANDARD2_0
        return content.ReadAsStringAsync();
#else
        return content.ReadAsStringAsync(cancellationToken);
#endif
    }
}

public sealed class GenerationRecorder
{
    internal static readonly GenerationRecorder Noop = new(null, new GenerationStart(), DateTimeOffset.UtcNow, null, true);

    private readonly SigilClient? _client;
    private readonly GenerationStart _seed;
    private readonly DateTimeOffset _startedAt;
    private readonly Activity? _activity;
    private readonly bool _noop;

    private readonly object _gate = new();
    private bool _ended;
    private Exception? _callError;
    private Exception? _mappingError;
    private Generation? _result;
    private DateTimeOffset? _firstTokenAt;

    public Generation? LastGeneration { get; private set; }

    public Exception? Error { get; private set; }

    internal GenerationRecorder(
        SigilClient? client,
        GenerationStart seed,
        DateTimeOffset startedAt,
        Activity? activity,
        bool noop = false
    )
    {
        _client = client;
        _seed = seed;
        _startedAt = startedAt;
        _activity = activity;
        _noop = noop;
    }

    public void SetCallError(Exception error)
    {
        if (_noop || error == null)
        {
            return;
        }

        lock (_gate)
        {
            _callError = error;
        }
    }

    public void SetResult(Generation generation, Exception? mappingError = null)
    {
        if (_noop)
        {
            return;
        }

        lock (_gate)
        {
            _result = InternalUtils.DeepClone(generation);
            _mappingError = mappingError;
        }
    }

    public void SetFirstTokenAt(DateTimeOffset firstTokenAt)
    {
        if (_noop)
        {
            return;
        }

        lock (_gate)
        {
            _firstTokenAt = InternalUtils.Utc(firstTokenAt);
        }
    }

    public void End()
    {
        if (_noop)
        {
            return;
        }

        Exception? callError;
        Exception? mappingError;
        Generation result;
        DateTimeOffset? firstTokenAt;

        lock (_gate)
        {
            if (_ended)
            {
                return;
            }

            _ended = true;
            callError = _callError;
            mappingError = _mappingError;
            result = _result != null ? InternalUtils.DeepClone(_result) : new Generation();
            firstTokenAt = _firstTokenAt;
        }

        var completedAt = _client!._config.UtcNow!();
        var generation = NormalizeGeneration(result, completedAt, callError);

        if (_activity != null)
        {
            generation.TraceId = _activity.TraceId.ToHexString();
            generation.SpanId = _activity.SpanId.ToHexString();

            _activity.DisplayName = SigilClient.GenerationSpanName(generation.OperationName, generation.Model.Name);
            SigilClient.ApplyGenerationSpanAttributes(_activity, generation);

            if (callError != null)
            {
                SigilClient.RecordException(_activity, callError);
            }

            if (mappingError != null)
            {
                SigilClient.RecordException(_activity, mappingError);
            }
        }

        Exception? localError = null;
        try
        {
            _client.PersistGeneration(generation);
        }
        catch (ValidationException ex)
        {
            localError = ex;
        }
        catch (EnqueueException ex)
        {
            localError = ex;
        }
        catch (Exception ex)
        {
            localError = new EnqueueException($"sigil: generation enqueue failed: {ex.Message}", ex);
        }

        var errorType = string.Empty;
        var errorCategory = string.Empty;
        if (callError != null)
        {
            errorType = "provider_call_error";
            errorCategory = SigilClient.ErrorCategoryFromException(callError, true);
        }
        else if (mappingError != null)
        {
            errorType = "mapping_error";
            errorCategory = "sdk_error";
        }
        else if (localError != null)
        {
            errorType = localError is ValidationException ? "validation_error" : "enqueue_error";
            errorCategory = "sdk_error";
        }

        if (_activity != null)
        {
            if (localError != null)
            {
                SigilClient.RecordException(_activity, localError);
            }

            if (errorType.Length > 0)
            {
                _activity.SetTag(SigilClient.SpanAttrErrorType, errorType);
                _activity.SetTag(SigilClient.SpanAttrErrorCategory, errorCategory);
                _activity.SetStatus(ActivityStatusCode.Error, (callError ?? mappingError ?? localError)?.Message);
            }
            else
            {
                _activity.SetStatus(ActivityStatusCode.Ok);
            }

            _activity.Stop();
        }

        _client.RecordGenerationMetrics(generation, errorType, errorCategory, firstTokenAt);

        LastGeneration = InternalUtils.DeepClone(generation);
        Error = localError;
    }

    private Generation NormalizeGeneration(Generation raw, DateTimeOffset completedAt, Exception? callError)
    {
        var generation = InternalUtils.DeepClone(raw);

        generation.Id = FirstNonEmpty(generation.Id, _seed.Id, InternalUtils.NewRandomId("gen"));
        generation.ConversationId = FirstNonEmpty(generation.ConversationId, _seed.ConversationId);
        generation.AgentName = FirstNonEmpty(generation.AgentName, _seed.AgentName);
        generation.AgentVersion = FirstNonEmpty(generation.AgentVersion, _seed.AgentVersion);
        generation.Mode ??= _seed.Mode ?? GenerationMode.Sync;
        generation.OperationName = FirstNonEmpty(
            generation.OperationName,
            _seed.OperationName,
            SigilClient.DefaultOperationNameForMode(generation.Mode.Value)
        );

        generation.Model.Provider = FirstNonEmpty(generation.Model.Provider, _seed.Model.Provider);
        generation.Model.Name = FirstNonEmpty(generation.Model.Name, _seed.Model.Name);
        generation.SystemPrompt = FirstNonEmpty(generation.SystemPrompt, _seed.SystemPrompt);
        generation.MaxTokens ??= _seed.MaxTokens;
        generation.Temperature ??= _seed.Temperature;
        generation.TopP ??= _seed.TopP;
        generation.ToolChoice = FirstNonEmpty(generation.ToolChoice ?? string.Empty, _seed.ToolChoice ?? string.Empty);
        generation.ThinkingEnabled ??= _seed.ThinkingEnabled;

        if (generation.Tools.Count == 0)
        {
            generation.Tools = InternalUtils.DeepClone(_seed.Tools);
        }

        generation.Tags = Merge(_seed.Tags, generation.Tags);
        generation.Metadata = Merge(_seed.Metadata, generation.Metadata);

        generation.StartedAt = generation.StartedAt.HasValue
            ? InternalUtils.Utc(generation.StartedAt.Value)
            : _startedAt;
        generation.CompletedAt = generation.CompletedAt.HasValue
            ? InternalUtils.Utc(generation.CompletedAt.Value)
            : completedAt;

        if (callError != null)
        {
            if (string.IsNullOrWhiteSpace(generation.CallError))
            {
                generation.CallError = callError.Message;
            }

            generation.Metadata["call_error"] = callError.Message;
        }

        generation.Usage = generation.Usage.Normalize();
        return generation;
    }

    private static string FirstNonEmpty(params string[] values)
    {
        foreach (var value in values)
        {
            if (!string.IsNullOrWhiteSpace(value))
            {
                return value;
            }
        }

        return string.Empty;
    }

    private static Dictionary<TKey, TValue> Merge<TKey, TValue>(
        IReadOnlyDictionary<TKey, TValue> left,
        IReadOnlyDictionary<TKey, TValue> right
    )
        where TKey : notnull
    {
        var merged = new Dictionary<TKey, TValue>();
        foreach (var pair in left)
        {
            merged[pair.Key] = pair.Value;
        }

        foreach (var pair in right)
        {
            merged[pair.Key] = pair.Value;
        }

        return merged;
    }
}

public sealed class ToolExecutionRecorder
{
    internal static readonly ToolExecutionRecorder Noop = new(null, new ToolExecutionStart(), DateTimeOffset.UtcNow, false, null, true);

    private readonly SigilClient? _client;
    private readonly ToolExecutionStart _seed;
    private readonly DateTimeOffset _startedAt;
    private readonly bool _includeContent;
    private readonly Activity? _activity;
    private readonly bool _noop;

    private readonly object _gate = new();
    private bool _ended;
    private Exception? _executionError;
    private ToolExecutionEnd _result = new();

    public Exception? Error { get; private set; }

    internal ToolExecutionRecorder(
        SigilClient? client,
        ToolExecutionStart seed,
        DateTimeOffset startedAt,
        bool includeContent,
        Activity? activity,
        bool noop = false
    )
    {
        _client = client;
        _seed = seed;
        _startedAt = startedAt;
        _includeContent = includeContent;
        _activity = activity;
        _noop = noop;
    }

    public void SetExecutionError(Exception error)
    {
        if (_noop || error == null)
        {
            return;
        }

        lock (_gate)
        {
            _executionError = error;
        }
    }

    public void SetResult(ToolExecutionEnd result)
    {
        if (_noop)
        {
            return;
        }

        lock (_gate)
        {
            _result = InternalUtils.DeepClone(result);
        }
    }

    public void End()
    {
        if (_noop)
        {
            return;
        }

        Exception? executionError;
        ToolExecutionEnd result;

        lock (_gate)
        {
            if (_ended)
            {
                return;
            }

            _ended = true;
            executionError = _executionError;
            result = InternalUtils.DeepClone(_result);
        }

        var finalError = executionError;
        var completedAt = result.CompletedAt.HasValue
            ? InternalUtils.Utc(result.CompletedAt.Value)
            : _client!._config.UtcNow!();

        if (_activity != null)
        {
            _activity.DisplayName = SigilClient.ToolSpanName(_seed.ToolName);
            SigilClient.ApplyToolSpanAttributes(_activity, _seed);

            if (_includeContent)
            {
                try
                {
                    var arguments = InternalUtils.SerializeJson(result.Arguments);
                    if (!string.IsNullOrWhiteSpace(arguments))
                    {
                        _activity.SetTag(SigilClient.SpanAttrToolCallArguments, arguments);
                    }

                    var resultJson = InternalUtils.SerializeJson(result.Result);
                    if (!string.IsNullOrWhiteSpace(resultJson))
                    {
                        _activity.SetTag(SigilClient.SpanAttrToolCallResult, resultJson);
                    }
                }
                catch (Exception ex)
                {
                    finalError = finalError != null ? new AggregateException(finalError, ex) : ex;
                }
            }

            if (finalError != null)
            {
                SigilClient.RecordException(_activity, finalError);
                _activity.SetTag(SigilClient.SpanAttrErrorType, "tool_execution_error");
                _activity.SetTag(SigilClient.SpanAttrErrorCategory, SigilClient.ErrorCategoryFromException(finalError, true));
                _activity.SetStatus(ActivityStatusCode.Error, finalError.Message);
            }
            else
            {
                _activity.SetStatus(ActivityStatusCode.Ok);
            }

            _activity.SetEndTime(completedAt.UtcDateTime);
            _activity.Stop();
        }

        _client!.RecordToolExecutionMetrics(_seed, _startedAt, completedAt, finalError);
        Error = finalError;
    }
}
