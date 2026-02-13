using System.Diagnostics;
using System.Text.Json;
using System.Threading;

namespace Grafana.Sigil;

public sealed class SigilClient : IAsyncDisposable
{
    internal const string DefaultOperationNameSync = "generateText";
    internal const string DefaultOperationNameStream = "streamText";

    internal const string SpanAttrGenerationId = "sigil.generation.id";
    internal const string SpanAttrConversationId = "gen_ai.conversation.id";
    internal const string SpanAttrAgentName = "gen_ai.agent.name";
    internal const string SpanAttrAgentVersion = "gen_ai.agent.version";
    internal const string SpanAttrErrorType = "error.type";
    internal const string SpanAttrOperationName = "gen_ai.operation.name";
    internal const string SpanAttrProviderName = "gen_ai.provider.name";
    internal const string SpanAttrRequestModel = "gen_ai.request.model";
    internal const string SpanAttrResponseId = "gen_ai.response.id";
    internal const string SpanAttrResponseModel = "gen_ai.response.model";
    internal const string SpanAttrFinishReasons = "gen_ai.response.finish_reasons";
    internal const string SpanAttrInputTokens = "gen_ai.usage.input_tokens";
    internal const string SpanAttrOutputTokens = "gen_ai.usage.output_tokens";
    internal const string SpanAttrCacheReadTokens = "gen_ai.usage.cache_read_input_tokens";
    internal const string SpanAttrCacheWriteTokens = "gen_ai.usage.cache_write_input_tokens";
    internal const string SpanAttrToolName = "gen_ai.tool.name";
    internal const string SpanAttrToolCallId = "gen_ai.tool.call.id";
    internal const string SpanAttrToolType = "gen_ai.tool.type";
    internal const string SpanAttrToolDescription = "gen_ai.tool.description";
    internal const string SpanAttrToolCallArguments = "gen_ai.tool.call.arguments";
    internal const string SpanAttrToolCallResult = "gen_ai.tool.call.result";

    internal readonly SigilClientConfig _config;
    private readonly IGenerationExporter _generationExporter;
    private readonly TraceRuntime _traceRuntime;
    private readonly Action<string> _log;

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
            ?? (_config.GenerationExport.Protocol == GenerationExportProtocol.Http
                ? new HttpGenerationExporter(_config.GenerationExport.Endpoint, _config.GenerationExport.Headers)
                : new GrpcGenerationExporter(
                    _config.GenerationExport.Endpoint,
                    _config.GenerationExport.Insecure,
                    _config.GenerationExport.Headers
                ));

        _traceRuntime = TraceRuntime.Create(_config.Trace, _log);

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

        var activity = _traceRuntime.Source.StartActivity(
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

        try
        {
            _traceRuntime.Flush();
        }
        catch (Exception ex)
        {
            _log($"sigil trace flush failed: {ex}");
        }

        _traceRuntime.Dispose();
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

        var activity = _traceRuntime.Source.StartActivity(
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
            activity.SetTag(SpanAttrFinishReasons, "[\"" + generation.StopReason + "\"]");
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

    public void End()
    {
        if (_noop)
        {
            return;
        }

        Exception? callError;
        Exception? mappingError;
        Generation result;

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

        if (_activity != null)
        {
            if (localError != null)
            {
                SigilClient.RecordException(_activity, localError);
            }

            if (callError != null)
            {
                _activity.SetTag(SigilClient.SpanAttrErrorType, "provider_call_error");
                _activity.SetStatus(ActivityStatusCode.Error, callError.Message);
            }
            else if (mappingError != null)
            {
                _activity.SetTag(SigilClient.SpanAttrErrorType, "mapping_error");
                _activity.SetStatus(ActivityStatusCode.Error, mappingError.Message);
            }
            else if (localError != null)
            {
                _activity.SetTag(
                    SigilClient.SpanAttrErrorType,
                    localError is ValidationException ? "validation_error" : "enqueue_error"
                );
                _activity.SetStatus(ActivityStatusCode.Error, localError.Message);
            }
            else
            {
                _activity.SetStatus(ActivityStatusCode.Ok);
            }

            _activity.Stop();
        }

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
                _activity.SetStatus(ActivityStatusCode.Error, finalError.Message);
            }
            else
            {
                _activity.SetStatus(ActivityStatusCode.Ok);
            }

            _activity.Stop();
        }

        Error = finalError;
    }
}
