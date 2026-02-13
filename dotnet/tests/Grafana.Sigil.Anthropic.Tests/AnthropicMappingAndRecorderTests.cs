using System.Text.Json;
using Anthropic.Models.Messages;
using Xunit;
using AnthropicMessage = Anthropic.Models.Messages.Message;

namespace Grafana.Sigil.Anthropic.Tests;

public sealed class AnthropicMappingAndRecorderTests
{
    [Fact]
    public void FromRequestResponse_MapsSyncModeAndDefaultsRawArtifactsOff()
    {
        var request = CreateRequest();
        var response = CreateResponse();

        var generation = AnthropicGenerationMapper.FromRequestResponse(
            request,
            response,
            new AnthropicSigilOptions
            {
                ConversationId = "conv-1",
                AgentName = "agent-anthropic",
                AgentVersion = "v-anthropic",
            }
        );

        Assert.Equal(GenerationMode.Sync, generation.Mode);
        Assert.Equal("conv-1", generation.ConversationId);
        Assert.Equal("Be precise.", generation.SystemPrompt);
        Assert.Equal("msg_1", generation.ResponseId);
        Assert.Equal("end_turn", generation.StopReason);
        Assert.Equal(162, generation.Usage.TotalTokens);
        Assert.Equal(30, generation.Usage.CacheReadInputTokens);
        Assert.Equal(10, generation.Usage.CacheCreationInputTokens);
        Assert.Empty(generation.Artifacts);
    }

    [Fact]
    public void FromStream_MapsStreamMode_AndRawArtifactsOptIn()
    {
        var request = CreateRequest();
        var summary = new AnthropicStreamSummary();
        summary.Events.Add(CreateMessageStartEvent("msg_stream_1", "stream output"));
        summary.Events.Add(CreateMessageDeltaEvent(80, 25, 8, 4));

        var generation = AnthropicGenerationMapper.FromStream(request, summary, new AnthropicSigilOptions().WithRawArtifacts());

        Assert.Equal(GenerationMode.Stream, generation.Mode);
        Assert.Equal("msg_stream_1", generation.ResponseId);
        Assert.Equal("end_turn", generation.StopReason);
        Assert.Equal(105, generation.Usage.TotalTokens);
        Assert.Contains(generation.Artifacts, artifact => artifact.Kind == ArtifactKind.ProviderEvent);
    }

    [Fact]
    public async Task Recorder_SyncAndStreamModes_AreRecordedWithProviderErrorPropagation()
    {
        var exporter = new CapturingExporter();
        var client = new SigilClient(new SigilClientConfig
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
        });

        var request = CreateRequest();

        await Assert.ThrowsAsync<InvalidOperationException>(() => AnthropicRecorder.MessageAsync(
            client,
            request,
            (_, _) => throw new InvalidOperationException("provider failed"),
            new AnthropicSigilOptions
            {
                ModelName = "claude-sonnet-4-5",
            }
        ));

        var streamSummary = await AnthropicRecorder.MessageStreamAsync(
            client,
            request,
            (_, _) => StreamEvents(),
            new AnthropicSigilOptions
            {
                ModelName = "claude-sonnet-4-5",
            }
        );

        Assert.NotEmpty(streamSummary.Events);

        await client.FlushAsync();
        await client.ShutdownAsync();

        var generations = exporter.Requests.SelectMany(request => request.Generations).ToList();
        Assert.True(generations.Count >= 2);
        Assert.Contains(generations, generation => generation.Mode == GenerationMode.Sync && generation.CallError.Contains("provider failed", StringComparison.Ordinal));
        Assert.Contains(generations, generation => generation.Mode == GenerationMode.Stream);
    }

    private static MessageCreateParams CreateRequest()
    {
        return new MessageCreateParams
        {
            MaxTokens = 512,
            Model = Model.ClaudeSonnet4_5,
            System = "Be precise.",
            Messages = new List<MessageParam>
            {
                new MessageParam
                {
                    Role = Role.User,
                    Content = "What's the weather in Paris?",
                },
            },
        };
    }

    private static AnthropicMessage CreateResponse()
    {
        return new AnthropicMessage
        {
            ID = "msg_1",
            Content = new List<ContentBlock>
            {
                new TextBlock
                {
                    Text = "It's 18C and sunny.",
                    Citations = null,
                    Type = JsonSerializer.SerializeToElement("text"),
                },
                new ThinkingBlock
                {
                    Signature = "sig_1",
                    Thinking = "done",
                    Type = JsonSerializer.SerializeToElement("thinking"),
                },
            },
            Model = Model.ClaudeSonnet4_5,
            StopReason = StopReason.EndTurn,
            StopSequence = null,
            Usage = new Usage
            {
                InputTokens = 120,
                OutputTokens = 42,
                CacheReadInputTokens = 30,
                CacheCreationInputTokens = 10,
                CacheCreation = null,
                ServerToolUse = null,
                ServiceTier = null,
            },
        };
    }

    private static async IAsyncEnumerable<RawMessageStreamEvent> StreamEvents()
    {
        yield return CreateMessageStartEvent("msg_stream_recorder", "hello");
        yield return CreateMessageDeltaEvent(2, 1, null, null);
        await Task.CompletedTask;
    }

    private static RawMessageStreamEvent CreateMessageStartEvent(string id, string text)
    {
        return new RawMessageStreamEvent(new RawMessageStartEvent
        {
            Type = JsonSerializer.SerializeToElement("message_start"),
            Message = new AnthropicMessage
            {
                ID = id,
                Model = Model.ClaudeSonnet4_5,
                Content = new List<ContentBlock>
                {
                    new TextBlock
                    {
                        Type = JsonSerializer.SerializeToElement("text"),
                        Text = text,
                        Citations = null,
                    },
                },
                StopReason = null,
                StopSequence = null,
                Usage = new Usage
                {
                    InputTokens = 0,
                    OutputTokens = 0,
                    CacheCreation = null,
                    CacheCreationInputTokens = null,
                    CacheReadInputTokens = null,
                    ServerToolUse = null,
                    ServiceTier = null,
                },
            },
        });
    }

    private static RawMessageStreamEvent CreateMessageDeltaEvent(
        long inputTokens,
        long outputTokens,
        long? cacheReadTokens,
        long? cacheCreationTokens
    )
    {
        return new RawMessageStreamEvent(new RawMessageDeltaEvent
        {
            Type = JsonSerializer.SerializeToElement("message_delta"),
            Delta = new Delta
            {
                StopReason = StopReason.EndTurn,
                StopSequence = null,
            },
            Usage = new MessageDeltaUsage
            {
                InputTokens = inputTokens,
                OutputTokens = outputTokens,
                CacheReadInputTokens = cacheReadTokens,
                CacheCreationInputTokens = cacheCreationTokens,
                ServerToolUse = null,
            },
        });
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
