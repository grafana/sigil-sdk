using System.ClientModel;
using System.Text;
using OpenAI.Chat;
using Xunit;

namespace Grafana.Sigil.OpenAI.Tests;

public sealed class OpenAIMappingAndRecorderTests
{
    [Fact]
    public void FromRequestResponse_MapsSyncModeAndDefaultsRawArtifactsOff()
    {
        var messages = new List<ChatMessage>
        {
            new SystemChatMessage("You are concise."),
            new UserChatMessage("What is the weather in Paris?"),
            new ToolChatMessage("call_weather", "{\"temp_c\":18}"),
        };

        var options = new ChatCompletionOptions();
        options.MaxOutputTokenCount = 512;
        options.ToolChoice = ChatToolChoice.CreateFunctionChoice("weather");
        options.ReasoningEffortLevel = ChatReasoningEffortLevel.High;
        options.Tools.Add(ChatTool.CreateFunctionTool(
            "weather",
            "Get weather",
            BinaryData.FromString("{\"type\":\"object\",\"properties\":{\"city\":{\"type\":\"string\"}}}")
        ));

        var response = OpenAIChatModelFactory.ChatCompletion(
            id: "chatcmpl_1",
            finishReason: ChatFinishReason.ToolCalls,
            content: new ChatMessageContent("calling weather"),
            toolCalls: new[]
            {
                ChatToolCall.CreateFunctionToolCall(
                    "call_weather",
                    "weather",
                    BinaryData.FromString("{\"city\":\"Paris\"}")
                ),
            },
            role: ChatMessageRole.Assistant,
            model: "gpt-5",
            usage: OpenAIChatModelFactory.ChatTokenUsage(
                outputTokenCount: 42,
                inputTokenCount: 120,
                totalTokenCount: 162,
                outputTokenDetails: OpenAIChatModelFactory.ChatOutputTokenUsageDetails(reasoningTokenCount: 5),
                inputTokenDetails: OpenAIChatModelFactory.ChatInputTokenUsageDetails(cachedTokenCount: 8)
            )
        );

        var generation = OpenAIGenerationMapper.FromRequestResponse(
            "gpt-5",
            messages,
            options,
            response,
            new OpenAISigilOptions
            {
                ConversationId = "conv-1",
                AgentName = "agent-openai",
                AgentVersion = "v-openai",
            }
        );

        Assert.Equal(GenerationMode.Sync, generation.Mode);
        Assert.Equal("You are concise.", generation.SystemPrompt);
        Assert.Equal("chatcmpl_1", generation.ResponseId);
        Assert.Equal("tool_calls", generation.StopReason);
        Assert.Equal(512, generation.MaxTokens);
        Assert.Contains("weather", generation.ToolChoice ?? string.Empty, StringComparison.OrdinalIgnoreCase);
        Assert.True(generation.ThinkingEnabled);
        Assert.Equal(162, generation.Usage.TotalTokens);
        Assert.Equal(8, generation.Usage.CacheReadInputTokens);
        Assert.Equal(5, generation.Usage.ReasoningTokens);
        Assert.Empty(generation.Artifacts);
        Assert.Contains(generation.Input, message => message.Role == MessageRole.Tool);
    }

    [Fact]
    public void FromRequestResponse_WithRawArtifactsOptIn_IncludesRequestResponseAndToolsArtifacts()
    {
        var messages = new List<ChatMessage>
        {
            new SystemChatMessage("You are concise."),
            new UserChatMessage("hello"),
        };

        var options = new ChatCompletionOptions();
        options.MaxOutputTokenCount = 256;
        options.ToolChoice = ChatToolChoice.CreateRequiredChoice();
        options.ReasoningEffortLevel = ChatReasoningEffortLevel.Medium;
        options.Tools.Add(ChatTool.CreateFunctionTool("weather"));

        var response = OpenAIChatModelFactory.ChatCompletion(
            id: "chatcmpl_1",
            finishReason: ChatFinishReason.Stop,
            content: new ChatMessageContent("hi"),
            role: ChatMessageRole.Assistant,
            model: "gpt-5"
        );

        var generation = OpenAIGenerationMapper.FromRequestResponse(
            "gpt-5",
            messages,
            options,
            response,
            new OpenAISigilOptions().WithRawArtifacts()
        );

        Assert.Equal(3, generation.Artifacts.Count);
    }

    [Fact]
    public void FromStream_MapsStreamMode()
    {
        var messages = new List<ChatMessage>
        {
            new SystemChatMessage("You are concise."),
            new UserChatMessage("What is the weather in Paris?"),
        };

        var options = new ChatCompletionOptions();
        options.MaxOutputTokenCount = 256;
        options.ToolChoice = ChatToolChoice.CreateRequiredChoice();
        options.ReasoningEffortLevel = ChatReasoningEffortLevel.Medium;
        options.Tools.Add(ChatTool.CreateFunctionTool("weather"));

        var summary = new OpenAIStreamSummary();
        summary.Updates.Add(OpenAIChatModelFactory.StreamingChatCompletionUpdate(
            completionId: "chatcmpl_stream_1",
            contentUpdate: new ChatMessageContent("Calling tool"),
            toolCallUpdates: new[]
            {
                OpenAIChatModelFactory.StreamingChatToolCallUpdate(
                    index: 0,
                    toolCallId: "call_weather",
                    kind: ChatToolCallKind.Function,
                    functionName: "weather",
                    functionArgumentsUpdate: BinaryData.FromString("{\"city\":\"Pa")
                ),
            },
            model: "gpt-5"
        ));
        summary.Updates.Add(OpenAIChatModelFactory.StreamingChatCompletionUpdate(
            toolCallUpdates: new[]
            {
                OpenAIChatModelFactory.StreamingChatToolCallUpdate(
                    index: 0,
                    kind: ChatToolCallKind.Function,
                    functionArgumentsUpdate: BinaryData.FromString("ris\"}")
                ),
            },
            finishReason: ChatFinishReason.ToolCalls,
            usage: OpenAIChatModelFactory.ChatTokenUsage(outputTokenCount: 5, inputTokenCount: 20, totalTokenCount: 25)
        ));

        var generation = OpenAIGenerationMapper.FromStream("gpt-5", messages, options, summary);

        Assert.Equal(GenerationMode.Stream, generation.Mode);
        Assert.Equal("chatcmpl_stream_1", generation.ResponseId);
        Assert.Equal("tool_calls", generation.StopReason);
        Assert.Equal(256, generation.MaxTokens);
        Assert.Contains("required", generation.ToolChoice ?? string.Empty, StringComparison.OrdinalIgnoreCase);
        Assert.True(generation.ThinkingEnabled);
        Assert.Equal(25, generation.Usage.TotalTokens);
    }

    [Fact]
    public async Task Recorder_SyncAndStreamModes_AreRecordedWithProviderErrorPropagation()
    {
        var exporter = new CapturingExporter();
        var config = new SigilClientConfig
        {
            Trace = new TraceConfig
            {
                Endpoint = string.Empty,
            },
            GenerationExporter = exporter,
            GenerationExport = new GenerationExportConfig
            {
                BatchSize = 1,
                QueueSize = 10,
                FlushInterval = TimeSpan.FromHours(1),
            },
        };

        await using var client = new SigilClient(config);

        var syncMessages = new List<ChatMessage>
        {
            new UserChatMessage("hello"),
        };

        await Assert.ThrowsAsync<InvalidOperationException>(() => OpenAIRecorder.ChatCompletionAsync(
            client,
            syncMessages,
            (_, _, _) => throw new InvalidOperationException("provider failed"),
            requestOptions: null,
            options: new OpenAISigilOptions
            {
                ModelName = "gpt-5",
            }
        ));

        var streamSummary = await OpenAIRecorder.ChatCompletionStreamAsync(
            client,
            syncMessages,
            (_, _, _) => StreamUpdates(),
            requestOptions: null,
            options: new OpenAISigilOptions
            {
                ModelName = "gpt-5",
            }
        );

        Assert.NotEmpty(streamSummary.Updates);

        await client.FlushAsync();
        await client.ShutdownAsync();

        var generations = exporter.Requests.SelectMany(request => request.Generations).ToList();

        Assert.Contains(generations, generation => generation.Mode == GenerationMode.Sync && generation.CallError.Contains("provider failed", StringComparison.Ordinal));
        Assert.Contains(generations, generation => generation.Mode == GenerationMode.Stream);
    }

    private static async IAsyncEnumerable<StreamingChatCompletionUpdate> StreamUpdates()
    {
        yield return OpenAIChatModelFactory.StreamingChatCompletionUpdate(
            completionId: "chatcmpl_stream_recorder",
            contentUpdate: new ChatMessageContent("hello"),
            finishReason: ChatFinishReason.Stop,
            usage: OpenAIChatModelFactory.ChatTokenUsage(outputTokenCount: 1, inputTokenCount: 1, totalTokenCount: 2),
            model: "gpt-5"
        );

        await Task.CompletedTask;
    }

    private sealed class CapturingExporter : IGenerationExporter
    {
        public List<ExportGenerationsRequest> Requests { get; } = new();

        public Task<ExportGenerationsResponse> ExportGenerationsAsync(ExportGenerationsRequest request, CancellationToken cancellationToken)
        {
            Requests.Add(request);
            return Task.FromResult(new ExportGenerationsResponse
            {
                Results = request.Generations.Select(generation => new ExportGenerationResult
                {
                    GenerationId = generation.Id,
                    Accepted = true,
                }).ToList(),
            });
        }

        public Task ShutdownAsync(CancellationToken cancellationToken)
        {
            return Task.CompletedTask;
        }
    }
}
