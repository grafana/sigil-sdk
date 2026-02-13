# Grafana.Sigil.OpenAI

OpenAI Chat Completions instrumentation helpers for `Grafana.Sigil`.

## Install

```bash
dotnet add package Grafana.Sigil
dotnet add package Grafana.Sigil.OpenAI
dotnet add package OpenAI
```

## Sync wrapper (`ChatCompletionAsync`)

```csharp
using Grafana.Sigil;
using Grafana.Sigil.OpenAI;
using OpenAI.Chat;

var sigil = new SigilClient(config);

var openAI = new ChatClient(
    "gpt-5",
    Environment.GetEnvironmentVariable("OPENAI_API_KEY")!
);

var messages = new List<ChatMessage>
{
    new SystemChatMessage("You are concise."),
    new UserChatMessage("What's the weather in Paris?"),
};

var requestOptions = new ChatCompletionOptions();
requestOptions.Tools.Add(ChatTool.CreateFunctionTool(
    "weather",
    "Get weather by city",
    BinaryData.FromString("{\"type\":\"object\",\"properties\":{\"city\":{\"type\":\"string\"}}}")
));

ChatCompletion response = await OpenAIRecorder.ChatCompletionAsync(
    sigil,
    openAI,
    messages,
    requestOptions: requestOptions,
    options: new OpenAISigilOptions
    {
        ConversationId = "conv-openai-1",
        AgentName = "assistant-core",
        AgentVersion = "1.0.0",
    },
    cancellationToken: CancellationToken.None
);
```

## Stream wrapper (`ChatCompletionStreamAsync`)

```csharp
OpenAIStreamSummary summary = await OpenAIRecorder.ChatCompletionStreamAsync(
    sigil,
    openAI,
    messages,
    requestOptions: requestOptions,
    options: new OpenAISigilOptions
    {
        ConversationId = "conv-openai-stream-1",
        AgentName = "assistant-core",
        AgentVersion = "1.0.0",
    },
    cancellationToken: CancellationToken.None
);

foreach (var update in summary.Updates)
{
    // Use streamed token/tool-call updates in real time if needed.
}
```

The wrapper records mode as `STREAM` and aggregates a normalized generation from stream updates.

## Raw artifacts (debug opt-in)

Raw provider request/response/event artifacts are disabled by default.

```csharp
var sigilOptions = new OpenAISigilOptions
{
    ConversationId = "conv-openai-debug-1",
    AgentName = "assistant-core",
    AgentVersion = "1.0.0",
}.WithRawArtifacts();
```

Use this only for diagnostics because artifacts can be large.

## Delegate overload for custom call pipelines

If you need custom retries, alternate transports, or test doubles:

```csharp
var response = await OpenAIRecorder.ChatCompletionAsync(
    sigil,
    messages,
    async (requestMessages, opts, ct) =>
    {
        var result = await openAI.CompleteChatAsync(requestMessages, opts, ct);
        return result.Value;
    },
    requestOptions: requestOptions,
    options: new OpenAISigilOptions { ModelName = "gpt-5" },
    cancellationToken: CancellationToken.None
);
```

## Behavior notes

- Wrapper sets generation mode automatically (`SYNC` for non-stream, `STREAM` for stream).
- This package targets OpenAI Chat Completions in this parity pass.
- System messages are normalized into `SystemPrompt` and not duplicated in generation input messages.
- Provider exceptions are captured as generation `CallError` and then rethrown.
- Recorder `Error` covers local SDK failures only.
- Call `SigilClient.ShutdownAsync(...)` during application shutdown to flush pending exports.
