# Grafana.Sigil.OpenAI

OpenAI instrumentation helpers for `Grafana.Sigil` with strict official OpenAI .NET SDK types for both:

- Chat Completions
- Responses

## Integration styles

- Strict wrappers: call OpenAI and record in one step.
- Manual instrumentation: call OpenAI directly, then map strict OpenAI request/response payloads with `OpenAIGenerationMapper`.

## Public API

- Wrappers:
  - `OpenAIRecorder.CompleteChatAsync(...)`
  - `OpenAIRecorder.CompleteChatStreamingAsync(...)`
  - `OpenAIRecorder.CreateResponseAsync(...)`
  - `OpenAIRecorder.CreateResponseStreamingAsync(...)`
- Mappers:
  - `OpenAIGenerationMapper.ChatCompletionsFromRequestResponse(...)`
  - `OpenAIGenerationMapper.ChatCompletionsFromStream(...)`
  - `OpenAIGenerationMapper.ResponsesFromRequestResponse(...)`
  - `OpenAIGenerationMapper.ResponsesFromStream(...)`

## Install

```bash
dotnet add package Grafana.Sigil
dotnet add package Grafana.Sigil.OpenAI
dotnet add package OpenAI
```

## Responses Wrapper (Sync)

```csharp
using Grafana.Sigil;
using Grafana.Sigil.OpenAI;
using OpenAI.Responses;

var sigil = new SigilClient(config);

var responsesClient = new OpenAIResponseClient(
    "gpt-5",
    Environment.GetEnvironmentVariable("OPENAI_API_KEY")!
);

var inputItems = new List<ResponseItem>
{
    ResponseItem.CreateUserMessageItem("What's the weather in Paris?"),
};

var requestOptions = new ResponseCreationOptions
{
    Instructions = "You are concise.",
    MaxOutputTokenCount = 320,
};

OpenAIResponse response = await OpenAIRecorder.CreateResponseAsync(
    sigil,
    responsesClient,
    inputItems,
    requestOptions: requestOptions,
    options: new OpenAISigilOptions
    {
        ConversationId = "conv-openai-responses-1",
        AgentName = "assistant-core",
        AgentVersion = "1.0.0",
    },
    cancellationToken: CancellationToken.None
);
```

## Responses Wrapper (Stream)

```csharp
OpenAIResponsesStreamSummary summary = await OpenAIRecorder.CreateResponseStreamingAsync(
    sigil,
    responsesClient,
    inputItems,
    requestOptions: requestOptions,
    options: new OpenAISigilOptions
    {
        ConversationId = "conv-openai-responses-stream-1",
        AgentName = "assistant-core",
        AgentVersion = "1.0.0",
    },
    cancellationToken: CancellationToken.None
);

foreach (var evt in summary.Events)
{
    // Inspect raw stream events if needed.
}
```

## Chat Completions Wrapper (Sync)

```csharp
using OpenAI.Chat;

var chatClient = new ChatClient(
    "gpt-5",
    Environment.GetEnvironmentVariable("OPENAI_API_KEY")!
);

var messages = new List<ChatMessage>
{
    new SystemChatMessage("You are concise."),
    new UserChatMessage("What's the weather in Paris?"),
};

ChatCompletion chat = await OpenAIRecorder.CompleteChatAsync(
    sigil,
    chatClient,
    messages,
    requestOptions: null,
    options: new OpenAISigilOptions
    {
        ConversationId = "conv-openai-chat-1",
        AgentName = "assistant-core",
        AgentVersion = "1.0.0",
    },
    cancellationToken: CancellationToken.None
);
```

## Chat Completions Wrapper (Stream)

```csharp
OpenAIChatCompletionsStreamSummary streamSummary = await OpenAIRecorder.CompleteChatStreamingAsync(
    sigil,
    chatClient,
    messages,
    requestOptions: null,
    options: new OpenAISigilOptions
    {
        ConversationId = "conv-openai-chat-stream-1",
        AgentName = "assistant-core",
        AgentVersion = "1.0.0",
    },
    cancellationToken: CancellationToken.None
);
```

## Manual instrumentation example (strict mapper)

```csharp
var sigilOptions = new OpenAISigilOptions
{
    ConversationId = "conv-openai-manual-1",
    AgentName = "assistant-core",
    AgentVersion = "1.0.0",
};

var recorder = sigil.StartGeneration(new GenerationStart
{
    ConversationId = sigilOptions.ConversationId,
    AgentName = sigilOptions.AgentName,
    AgentVersion = sigilOptions.AgentVersion,
    Model = new ModelRef { Provider = "openai", Name = "gpt-5" },
});

try
{
    OpenAIResponse response = await responsesClient.CreateResponseAsync(
        inputItems,
        requestOptions,
        CancellationToken.None
    );

    recorder.SetResult(OpenAIGenerationMapper.ResponsesFromRequestResponse(
        "gpt-5",
        inputItems,
        requestOptions,
        response,
        sigilOptions
    ));
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

## Raw Artifacts (Debug Opt-In)

Raw provider request/response/tools/stream-events artifacts are disabled by default.

```csharp
var sigilOptions = new OpenAISigilOptions
{
    ConversationId = "conv-openai-debug-1",
    AgentName = "assistant-core",
    AgentVersion = "1.0.0",
}.WithRawArtifacts();
```

## Delegate Overloads

All wrappers also provide delegate overloads so you can inject custom retry/transport/test-double behavior while keeping Sigil recording intact.
