# Grafana.Sigil.Gemini

Google Gemini GenerateContent instrumentation helpers for `Grafana.Sigil`.

## Install

```bash
dotnet add package Grafana.Sigil
dotnet add package Grafana.Sigil.Gemini
dotnet add package Google.GenAI
```

## Sync wrapper (`GenerateContentAsync`)

```csharp
using Google.GenAI;
using Google.GenAI.Types;
using Grafana.Sigil;
using Grafana.Sigil.Gemini;
using GPart = Google.GenAI.Types.Part;

var sigil = new SigilClient(config);

var gemini = new Client(apiKey: Environment.GetEnvironmentVariable("GEMINI_API_KEY")!);

var request = new GenerateContentRequest
{
    Model = "gemini-2.5-pro",
    Contents = new List<Content>
    {
        new Content
        {
            Role = "user",
            Parts = new List<GPart>
            {
                new GPart { Text = "What's the weather in Paris?" },
            },
        },
    },
    Config = new GenerateContentConfig
    {
        SystemInstruction = new Content
        {
            Role = "user",
            Parts = new List<GPart> { new GPart { Text = "Be concise." } },
        },
    },
};

GenerateContentResponse response = await GeminiRecorder.GenerateContentAsync(
    sigil,
    gemini,
    request,
    options: new GeminiSigilOptions
    {
        ConversationId = "conv-gemini-1",
        AgentName = "assistant-core",
        AgentVersion = "1.0.0",
    },
    cancellationToken: CancellationToken.None
);
```

## Stream wrapper (`GenerateContentStreamAsync`)

```csharp
GeminiStreamSummary summary = await GeminiRecorder.GenerateContentStreamAsync(
    sigil,
    gemini,
    request,
    options: new GeminiSigilOptions
    {
        ConversationId = "conv-gemini-stream-1",
        AgentName = "assistant-core",
        AgentVersion = "1.0.0",
    },
    cancellationToken: CancellationToken.None
);

foreach (var update in summary.Responses)
{
    // Consume incremental GenerateContentResponse payloads.
}
```

The wrapper records mode as `STREAM` and aggregates the normalized generation from collected responses.

## Raw artifacts (debug opt-in)

```csharp
var sigilOptions = new GeminiSigilOptions
{
    ConversationId = "conv-gemini-debug-1",
    AgentName = "assistant-core",
    AgentVersion = "1.0.0",
}.WithRawArtifacts();
```

Raw artifacts are off by default and should be enabled only for diagnostics.

## Delegate overload for custom call pipelines

```csharp
var response = await GeminiRecorder.GenerateContentAsync(
    sigil,
    request,
    (payload, ct) => gemini.Models.GenerateContentAsync(payload.Model, payload.Contents, payload.Config, ct),
    options: new GeminiSigilOptions { ModelName = "gemini-2.5-pro" },
    cancellationToken: CancellationToken.None
);
```

## Behavior notes

- Wrapper sets generation mode automatically (`SYNC` or `STREAM`).
- Candidate text, tool calls, and function responses map to normalized Sigil message parts.
- Stop reason and usage fields are normalized from Gemini responses.
- Provider exceptions are captured as generation `CallError` and rethrown.
- Call `SigilClient.ShutdownAsync(...)` during application shutdown to flush pending exports.
