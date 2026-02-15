# Grafana Sigil .NET SDKs

Sigil extends OpenTelemetry-style instrumentation with normalized AI generation records.
The .NET SDK follows the same generation-first contract and provider parity target as the Go SDK.

## Packages

- `Grafana.Sigil`: core runtime (`SigilClient`, generation/tool recorders, generation export)
- `Grafana.Sigil.OpenAI`: OpenAI Responses + Chat Completions wrappers and mappers
- `Grafana.Sigil.Anthropic`: Anthropic Messages wrappers and mappers
- `Grafana.Sigil.Gemini`: Gemini GenerateContent wrappers and mappers

Package docs:

- Core: [`src/Grafana.Sigil/README.md`](src/Grafana.Sigil/README.md)
- OpenAI: [`src/Grafana.Sigil.OpenAI/README.md`](src/Grafana.Sigil.OpenAI/README.md)
- Anthropic: [`src/Grafana.Sigil.Anthropic/README.md`](src/Grafana.Sigil.Anthropic/README.md)
- Gemini: [`src/Grafana.Sigil.Gemini/README.md`](src/Grafana.Sigil.Gemini/README.md)

## Target frameworks

- Core package: `net8.0`, `netstandard2.0`
- Provider packages: `net8.0`

## Install

```bash
dotnet add package Grafana.Sigil
dotnet add package Grafana.Sigil.OpenAI
# or: Grafana.Sigil.Anthropic / Grafana.Sigil.Gemini
```

## Quickstart (OpenAI Responses wrapper)

```csharp
using Grafana.Sigil;
using Grafana.Sigil.OpenAI;
using OpenAI.Responses;

var sigil = new SigilClient(new SigilClientConfig
{
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
    },
    Api = new ApiConfig
    {
        Endpoint = "http://localhost:8080",
    },
});

// Configure OTel exporters (traces/metrics) separately in your application's OTel setup.
using var tracerProvider = OpenTelemetry.Sdk.CreateTracerProviderBuilder()
    .AddSource("github.com/grafana/sigil/sdks/dotnet")
    .AddOtlpExporter()
    .Build();

using var meterProvider = OpenTelemetry.Sdk.CreateMeterProviderBuilder()
    .AddMeter("github.com/grafana/sigil/sdks/dotnet")
    .AddOtlpExporter()
    .Build();

var openAI = new OpenAIResponseClient(
    "gpt-5",
    Environment.GetEnvironmentVariable("OPENAI_API_KEY")!
);

var inputItems = new List<ResponseItem>
{
    ResponseItem.CreateUserMessageItem("Give me a short weather summary for Paris."),
};

var requestOptions = new ResponseCreationOptions
{
    Instructions = "You are concise.",
    MaxOutputTokenCount = 320,
};

var response = await OpenAIRecorder.CreateResponseAsync(
    sigil,
    openAI,
    inputItems,
    requestOptions: requestOptions,
    options: new OpenAISigilOptions
    {
        ConversationId = "conv-9b2f",
        AgentName = "assistant-core",
        AgentVersion = "1.0.0",
    },
    cancellationToken: CancellationToken.None
);

await sigil.ShutdownAsync(CancellationToken.None);
```

## Core API

- `StartGeneration(...)` for non-stream operations (`SYNC`)
- `StartStreamingGeneration(...)` for stream operations (`STREAM`)
- `StartToolExecution(...)` for tool spans/events
- `FlushAsync(...)` for explicit export flush points
- `ShutdownAsync(...)` for graceful shutdown

Recorder behavior is explicit and idempotent:

- `GenerationRecorder.SetResult(...)` and `SetCallError(...)`
- `ToolExecutionRecorder.SetResult(...)` and `SetExecutionError(...)`
- `End()` is safe to call once in `finally`
- recorder `Error` only reports local instrumentation/export-queue errors

Generation export transport protocols:

- `GenerationExportProtocol.Grpc`
- `GenerationExportProtocol.Http`
- `GenerationExportProtocol.None` (instrumentation-only; no generation transport)

## Context defaults

`SigilContext` uses async-local scopes:

- `SigilContext.WithConversationId(...)`
- `SigilContext.WithAgentName(...)`
- `SigilContext.WithAgentVersion(...)`

These defaults are used when a start payload omits those fields.

## Conversation Ratings

Use the SDK helper to submit user-facing ratings:

```csharp
var result = await client.SubmitConversationRatingAsync(
    "conv-123",
    new SubmitConversationRatingRequest
    {
        RatingId = "rat-123",
        Rating = ConversationRatingValue.Bad,
        Comment = "Answer ignored user context",
        Metadata = new Dictionary<string, object?>
        {
            ["channel"] = "assistant-ui",
        },
        Source = "sdk-dotnet",
    },
    CancellationToken.None
);

Console.WriteLine($"{result.Rating.Rating} hasBad={result.Summary.HasBadRating}");
```

`SubmitConversationRatingAsync(...)` sends requests to `SigilClientConfig.Api.Endpoint` (default `http://localhost:8080`) and uses the same generation-export auth headers (`tenant` or `bearer`) already configured on the SDK client.

## .NET best practices

- Create one long-lived `SigilClient` per process (for example as a singleton in DI).
- Always call `ShutdownAsync(...)` during process shutdown.
- Keep provider request/response payloads normalized; enable raw artifacts only for debug sessions.
- Use explicit generation export auth config instead of sharing ad-hoc headers.

## Instrumentation-only mode (no generation send)

```csharp
var sigil = new SigilClient(new SigilClientConfig
{
    GenerationExport = new GenerationExportConfig
    {
        Protocol = GenerationExportProtocol.None,
    },
});
```

## SDK metrics

The SDK emits these OTel histograms through your configured OTel meter provider:

- `gen_ai.client.operation.duration`
- `gen_ai.client.token.usage`
- `gen_ai.client.time_to_first_token`
- `gen_ai.client.tool_calls_per_operation`

## Local tasks

Run from repository root:

- `mise run test:cs:sdk-core`
- `mise run test:cs:sdk-openai`
- `mise run test:cs:sdk-anthropic`
- `mise run test:cs:sdk-gemini`
