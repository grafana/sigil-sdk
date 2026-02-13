using Google.GenAI.Types;
using Xunit;
using GPart = Google.GenAI.Types.Part;

namespace Grafana.Sigil.Gemini.Tests;

public sealed class GeminiMappingAndRecorderTests
{
    [Fact]
    public void FromRequestResponse_MapsSyncModeAndDefaultsRawArtifactsOff()
    {
        var request = CreateRequest();
        var response = CreateResponse();

        var generation = GeminiGenerationMapper.FromRequestResponse(
            request,
            response,
            new GeminiSigilOptions
            {
                ConversationId = "conv-1",
                AgentName = "agent-gemini",
                AgentVersion = "v-gemini",
            }
        );

        Assert.Equal(GenerationMode.Sync, generation.Mode);
        Assert.Equal("conv-1", generation.ConversationId);
        Assert.Equal("resp_1", generation.ResponseId);
        Assert.Equal("gemini-2.5-pro-001", generation.ResponseModel);
        Assert.Equal("STOP", generation.StopReason);
        Assert.Equal(444, generation.MaxTokens);
        Assert.InRange(generation.Temperature ?? 0, 0.149, 0.151);
        Assert.InRange(generation.TopP ?? 0, 0.799, 0.801);
        Assert.Contains("any", generation.ToolChoice ?? string.Empty, StringComparison.OrdinalIgnoreCase);
        Assert.True(generation.ThinkingEnabled);
        Assert.Equal(1536L, ReadThinkingBudget(generation));
        Assert.Equal(170, generation.Usage.TotalTokens);
        Assert.Equal(12, generation.Usage.CacheReadInputTokens);
        Assert.Equal(10, generation.Usage.ReasoningTokens);
        Assert.Empty(generation.Artifacts);
        Assert.Contains(generation.Input, message => message.Role == MessageRole.Tool);
    }

    [Fact]
    public void FromStream_MapsStreamMode_AndRawArtifactsOptIn()
    {
        var request = CreateRequest();
        var summary = new GeminiStreamSummary();
        summary.Responses.Add(new GenerateContentResponse
        {
            ResponseId = "resp_stream_1",
            ModelVersion = "gemini-2.5-pro-001",
            Candidates = new List<Candidate>
            {
                new Candidate
                {
                    Content = new Content
                    {
                        Role = "model",
                        Parts = new List<GPart>
                        {
                            new GPart
                            {
                                FunctionCall = new FunctionCall
                                {
                                    Id = "call_weather",
                                    Name = "weather",
                                    Args = new Dictionary<string, object>
                                    {
                                        ["city"] = "Paris",
                                    },
                                },
                            },
                        },
                    },
                },
            },
        });
        summary.Responses.Add(new GenerateContentResponse
        {
            ResponseId = "resp_stream_2",
            ModelVersion = "gemini-2.5-pro-001",
            Candidates = new List<Candidate>
            {
                new Candidate
                {
                    FinishReason = FinishReason.Stop,
                    Content = new Content
                    {
                        Role = "model",
                        Parts = new List<GPart>
                        {
                            new GPart
                            {
                                Text = "It is sunny.",
                            },
                        },
                    },
                },
            },
            UsageMetadata = new GenerateContentResponseUsageMetadata
            {
                PromptTokenCount = 20,
                CandidatesTokenCount = 6,
                TotalTokenCount = 26,
            },
        });

        var generation = GeminiGenerationMapper.FromStream(request, summary, new GeminiSigilOptions().WithRawArtifacts());

        Assert.Equal(GenerationMode.Stream, generation.Mode);
        Assert.Equal("resp_stream_2", generation.ResponseId);
        Assert.Equal("STOP", generation.StopReason);
        Assert.Equal(444, generation.MaxTokens);
        Assert.InRange(generation.Temperature ?? 0, 0.149, 0.151);
        Assert.InRange(generation.TopP ?? 0, 0.799, 0.801);
        Assert.Contains("any", generation.ToolChoice ?? string.Empty, StringComparison.OrdinalIgnoreCase);
        Assert.True(generation.ThinkingEnabled);
        Assert.Equal(1536L, ReadThinkingBudget(generation));
        Assert.Equal(26, generation.Usage.TotalTokens);
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

        await Assert.ThrowsAsync<InvalidOperationException>(() => GeminiRecorder.GenerateContentAsync(
            client,
            request,
            (_, _) => throw new InvalidOperationException("provider failed"),
            new GeminiSigilOptions
            {
                ModelName = "gemini-2.5-pro",
            }
        ));

        var streamSummary = await GeminiRecorder.GenerateContentStreamAsync(
            client,
            request,
            (_, _) => StreamResponses(),
            new GeminiSigilOptions
            {
                ModelName = "gemini-2.5-pro",
            }
        );

        Assert.NotEmpty(streamSummary.Responses);

        await client.FlushAsync();
        await client.ShutdownAsync();

        var generations = exporter.Requests.SelectMany(request => request.Generations).ToList();
        Assert.True(generations.Count >= 2);
        Assert.Contains(generations, generation => generation.Mode == GenerationMode.Sync && generation.CallError.Contains("provider failed", StringComparison.Ordinal));
        Assert.Contains(generations, generation => generation.Mode == GenerationMode.Stream);
    }

    [Fact]
    public void FromRequestResponse_MapsThinkingDisabled()
    {
        var request = CreateRequest();
        request.Config!.ThinkingConfig = new ThinkingConfig
        {
            IncludeThoughts = false,
        };

        var generation = GeminiGenerationMapper.FromRequestResponse(request, CreateResponse());
        Assert.False(generation.ThinkingEnabled);
    }

    private static GenerateContentRequest CreateRequest()
    {
        return new GenerateContentRequest
        {
            Model = "gemini-2.5-pro",
            Contents = new List<Content>
            {
                new Content
                {
                    Role = "user",
                    Parts = new List<GPart>
                    {
                        new GPart
                        {
                            Text = "What is the weather in Paris?",
                        },
                    },
                },
                new Content
                {
                    Role = "user",
                    Parts = new List<GPart>
                    {
                        new GPart
                        {
                            FunctionResponse = new FunctionResponse
                            {
                                Id = "call_weather",
                                Name = "weather",
                                Response = new Dictionary<string, object>
                                {
                                    ["output"] = "18C and sunny",
                                },
                            },
                        },
                    },
                },
            },
            Config = new GenerateContentConfig
            {
                MaxOutputTokens = 444,
                Temperature = 0.15f,
                TopP = 0.8f,
                ToolConfig = new ToolConfig
                {
                    FunctionCallingConfig = new FunctionCallingConfig
                    {
                        Mode = FunctionCallingConfigMode.Any,
                    },
                },
                ThinkingConfig = new ThinkingConfig
                {
                    IncludeThoughts = true,
                    ThinkingBudget = 1536,
                },
                SystemInstruction = new Content
                {
                    Role = "user",
                    Parts = new List<GPart>
                    {
                        new GPart
                        {
                            Text = "Be concise.",
                        },
                    },
                },
                Tools = new List<Tool>
                {
                    new Tool
                    {
                        FunctionDeclarations = new List<FunctionDeclaration>
                        {
                            new FunctionDeclaration
                            {
                                Name = "weather",
                                Description = "Get weather",
                                ParametersJsonSchema = new Dictionary<string, object>
                                {
                                    ["type"] = "object",
                                },
                            },
                        },
                    },
                },
            },
        };
    }

    private static GenerateContentResponse CreateResponse()
    {
        return new GenerateContentResponse
        {
            ResponseId = "resp_1",
            ModelVersion = "gemini-2.5-pro-001",
            Candidates = new List<Candidate>
            {
                new Candidate
                {
                    FinishReason = FinishReason.Stop,
                    Content = new Content
                    {
                        Role = "model",
                        Parts = new List<GPart>
                        {
                            new GPart
                            {
                                FunctionCall = new FunctionCall
                                {
                                    Id = "call_weather",
                                    Name = "weather",
                                    Args = new Dictionary<string, object>
                                    {
                                        ["city"] = "Paris",
                                    },
                                },
                            },
                            new GPart
                            {
                                Text = "It is 18C and sunny.",
                            },
                        },
                    },
                },
            },
            UsageMetadata = new GenerateContentResponseUsageMetadata
            {
                PromptTokenCount = 120,
                CandidatesTokenCount = 40,
                TotalTokenCount = 170,
                CachedContentTokenCount = 12,
                ThoughtsTokenCount = 10,
            },
        };
    }

    private static long ReadThinkingBudget(Generation generation)
    {
        var raw = generation.Metadata["sigil.gen_ai.request.thinking.budget_tokens"];
        return raw switch
        {
            System.Text.Json.JsonElement json
                when json.ValueKind == System.Text.Json.JsonValueKind.Number && json.TryGetInt64(out var parsed) => parsed,
            IConvertible convertible => Convert.ToInt64(convertible),
            _ => throw new InvalidOperationException("unexpected thinking budget metadata type"),
        };
    }

    private static async IAsyncEnumerable<GenerateContentResponse> StreamResponses()
    {
        yield return new GenerateContentResponse
        {
            ResponseId = "resp_stream_recorder",
            ModelVersion = "gemini-2.5-pro-001",
            Candidates = new List<Candidate>
            {
                new Candidate
                {
                    FinishReason = FinishReason.Stop,
                    Content = new Content
                    {
                        Role = "model",
                        Parts = new List<GPart>
                        {
                            new GPart
                            {
                                Text = "hello",
                            },
                        },
                    },
                },
            },
            UsageMetadata = new GenerateContentResponseUsageMetadata
            {
                PromptTokenCount = 1,
                CandidatesTokenCount = 1,
                TotalTokenCount = 2,
            },
        };

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
