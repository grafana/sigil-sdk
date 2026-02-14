# Grafana.Sigil

Core runtime for normalized generation export and OTLP tracing.

## Install

```bash
dotnet add package Grafana.Sigil
```

## Configure `SigilClient`

```csharp
using Grafana.Sigil;

var client = new SigilClient(new SigilClientConfig
{
    Trace = new TraceConfig
    {
        Protocol = TraceProtocol.Http,
        Endpoint = "http://localhost:4318/v1/traces",
        Auth = new AuthConfig
        {
            Mode = ExportAuthMode.None,
        },
    },
    GenerationExport = new GenerationExportConfig
    {
        Protocol = GenerationExportProtocol.Grpc,
        Endpoint = "localhost:4317",
        Auth = new AuthConfig
        {
            Mode = ExportAuthMode.Tenant,
            TenantId = "dev-tenant",
        },
        BatchSize = 100,
        FlushInterval = TimeSpan.FromSeconds(1),
        QueueSize = 2000,
        MaxRetries = 5,
        InitialBackoff = TimeSpan.FromMilliseconds(100),
        MaxBackoff = TimeSpan.FromSeconds(5),
    },
});
```

Trace and generation export are configured independently, including protocol and auth mode.

Generation export transport protocols:

- `GenerationExportProtocol.Grpc`
- `GenerationExportProtocol.Http`
- `GenerationExportProtocol.None` (instrumentation-only; no generation transport)

## Instrumentation-only mode (no generation send)

```csharp
var client = new SigilClient(new SigilClientConfig
{
    GenerationExport = new GenerationExportConfig
    {
        Protocol = GenerationExportProtocol.None,
    },
    Trace = new TraceConfig
    {
        Protocol = TraceProtocol.Http,
        Endpoint = "http://localhost:4318/v1/traces",
    },
});
```

## Manual generation instrumentation (sync)

Use this API for unsupported providers or custom pipelines.

```csharp
var recorder = client.StartGeneration(new GenerationStart
{
    ConversationId = "conv-9b2f",
    AgentName = "assistant-core",
    AgentVersion = "1.0.0",
    Model = new ModelRef
    {
        Provider = "openai",
        Name = "gpt-5",
    },
});

try
{
    var providerResponseText = await Task.FromResult("Paris is 18C and sunny.");

    recorder.SetResult(new Generation
    {
        Input = new List<Message>
        {
            Message.UserTextMessage("Give me a short weather summary for Paris."),
        },
        Output = new List<Message>
        {
            Message.AssistantTextMessage(providerResponseText),
        },
        Usage = new TokenUsage
        {
            InputTokens = 120,
            OutputTokens = 42,
            TotalTokens = 162,
        },
        StopReason = "stop",
    });
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
```

## Manual generation instrumentation (stream)

```csharp
var recorder = client.StartStreamingGeneration(new GenerationStart
{
    ConversationId = "conv-stream",
    AgentName = "assistant-core",
    AgentVersion = "1.0.0",
    Model = new ModelRef
    {
        Provider = "openai",
        Name = "gpt-5",
    },
});

try
{
    var chunks = new List<string>();
    await foreach (var chunk in StreamAsync())
    {
        chunks.Add(chunk);
    }

    recorder.SetResult(new Generation
    {
        Input = new List<Message>
        {
            Message.UserTextMessage("Stream a short weather summary for Paris."),
        },
        Output = new List<Message>
        {
            Message.AssistantTextMessage(string.Concat(chunks)),
        },
    });
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

static async IAsyncEnumerable<string> StreamAsync()
{
    yield return "Paris is ";
    yield return "18C and sunny.";
    await Task.CompletedTask;
}
```

## Tool execution instrumentation

```csharp
var tool = client.StartToolExecution(new ToolExecutionStart
{
    ToolName = "weather",
    ToolCallId = "call_weather_1",
    ToolType = "function",
    ToolDescription = "Get weather by city",
    ConversationId = "conv-9b2f",
    AgentName = "assistant-core",
    AgentVersion = "1.0.0",
    IncludeContent = true,
});

try
{
    var args = new { city = "Paris" };
    var result = await Task.FromResult<object>(new
    {
        city = "Paris",
        tempC = 18,
        condition = "sunny",
    });

    tool.SetResult(new ToolExecutionEnd
    {
        Arguments = args,
        Result = result,
    });
}
catch (Exception ex)
{
    tool.SetExecutionError(ex);
    throw;
}
finally
{
    tool.End();
}
```

## Context defaults with async-local scopes

```csharp
using var _conversation = SigilContext.WithConversationId("conv-default");
using var _agentName = SigilContext.WithAgentName("assistant-core");
using var _agentVersion = SigilContext.WithAgentVersion("1.0.0");

var recorder = client.StartGeneration(new GenerationStart
{
    Model = new ModelRef
    {
        Provider = "openai",
        Name = "gpt-5",
    },
});
```

If `ConversationId`, `AgentName`, or `AgentVersion` are omitted in `GenerationStart` or `ToolExecutionStart`, the current `SigilContext` values are applied.

## Error semantics

- `SetCallError(...)` records provider-call failures as generation `CallError` and span status.
- `SetResult(..., mappingError)` lets you keep generation data while marking mapper failures.
- `GenerationRecorder.Error` and `ToolExecutionRecorder.Error` represent local SDK failures only (validation/enqueue/shutdown path), not provider-call failures.

## Auth modes and header precedence

Per export path, supported auth modes are:

- `ExportAuthMode.None`
- `ExportAuthMode.Tenant` (`X-Scope-OrgID`)
- `ExportAuthMode.Bearer` (`Authorization: Bearer <token>`)

Explicit transport headers take precedence over auth-derived headers (`Authorization`, `X-Scope-OrgID`, case-insensitive).

## Lifecycle and performance guidance

- Reuse one `SigilClient` for the process lifetime.
- Call `ShutdownAsync(...)` on graceful shutdown to flush pending generation batches and trace state.
- Use `FlushAsync(...)` before short-lived jobs exit if needed.
- Keep raw artifacts disabled by default in production.
