# Grafana.Sigil.Anthropic

Anthropic Messages API instrumentation helpers for `Grafana.Sigil`.

## Install

```bash
dotnet add package Grafana.Sigil
dotnet add package Grafana.Sigil.Anthropic
dotnet add package Anthropic
```

## Sync wrapper (`MessageAsync`)

```csharp
using Anthropic;
using Anthropic.Models.Messages;
using Grafana.Sigil;
using Grafana.Sigil.Anthropic;

var sigil = new SigilClient(config);

var anthropic = new AnthropicClient(new Anthropic.Core.ClientOptions
{
    APIKey = Environment.GetEnvironmentVariable("ANTHROPIC_API_KEY")!,
});

var request = new MessageCreateParams
{
    Model = Model.ClaudeSonnet4_5,
    MaxTokens = 512,
    System = "Be concise.",
    Messages = new List<MessageParam>
    {
        new MessageParam
        {
            Role = Role.User,
            Content = "What's the weather in Paris?",
        },
    },
};

var response = await AnthropicRecorder.MessageAsync(
    sigil,
    anthropic,
    request,
    options: new AnthropicSigilOptions
    {
        ConversationId = "conv-anthropic-1",
        AgentName = "assistant-core",
        AgentVersion = "1.0.0",
    },
    cancellationToken: CancellationToken.None
);
```

## Stream wrapper (`MessageStreamAsync`)

```csharp
AnthropicStreamSummary summary = await AnthropicRecorder.MessageStreamAsync(
    sigil,
    anthropic,
    request,
    options: new AnthropicSigilOptions
    {
        ConversationId = "conv-anthropic-stream-1",
        AgentName = "assistant-core",
        AgentVersion = "1.0.0",
    },
    cancellationToken: CancellationToken.None
);

foreach (var streamEvent in summary.Events)
{
    // Inspect raw stream events if needed.
}
```

The wrapper records mode as `STREAM` and aggregates final usage/stop-reason/output fields.

## Raw artifacts (debug opt-in)

```csharp
var sigilOptions = new AnthropicSigilOptions
{
    ConversationId = "conv-anthropic-debug-1",
    AgentName = "assistant-core",
    AgentVersion = "1.0.0",
}.WithRawArtifacts();
```

Raw artifacts are off by default and should be enabled only for troubleshooting.

## Delegate overload for custom call pipelines

```csharp
var response = await AnthropicRecorder.MessageAsync(
    sigil,
    request,
    (payload, ct) => anthropic.Messages.Create(payload, ct),
    options: new AnthropicSigilOptions { ModelName = "claude-sonnet-4-5" },
    cancellationToken: CancellationToken.None
);
```

## Behavior notes

- Wrapper sets generation mode automatically (`SYNC` or `STREAM`).
- `System` prompt is normalized into `Generation.SystemPrompt`.
- Thinking blocks, tool-use blocks, and tool-result blocks map to typed Sigil parts.
- Provider exceptions are captured as generation `CallError` and rethrown.
- Call `SigilClient.ShutdownAsync(...)` during application shutdown to flush pending exports.
