using System.Text;
using System.Text.Json;
using Grafana.Sigil;
using OpenAI.Chat;

namespace Grafana.Sigil.OpenAI;

public static class OpenAIGenerationMapper
{
    private const string ThinkingBudgetMetadataKey = "sigil.gen_ai.request.thinking.budget_tokens";

    public static Generation FromRequestResponse(
        string modelName,
        IReadOnlyList<ChatMessage> messages,
        ChatCompletionOptions? requestOptions,
        ChatCompletion response,
        OpenAISigilOptions? options = null
    )
    {
        if (response == null)
        {
            throw new ArgumentNullException(nameof(response));
        }

        var effective = options ?? new OpenAISigilOptions();
        var requestMessages = messages ?? Array.Empty<ChatMessage>();

        var (input, systemPrompt) = MapRequestMessages(requestMessages);
        var output = MapResponseMessages(response);
        var tools = MapTools(requestOptions);
        var thinkingBudget = ResolveThinkingBudget(requestOptions);

        var responseModel = string.IsNullOrWhiteSpace(response.Model) ? modelName : response.Model;

        var generation = new Generation
        {
            ConversationId = effective.ConversationId,
            AgentName = effective.AgentName,
            AgentVersion = effective.AgentVersion,
            Model = new ModelRef
            {
                Provider = effective.ProviderName,
                Name = modelName,
            },
            ResponseId = response.Id,
            ResponseModel = responseModel,
            SystemPrompt = systemPrompt,
            MaxTokens = ResolveRequestMaxTokens(requestOptions),
            Temperature = ReadNullableDoubleProperty(requestOptions, "Temperature"),
            TopP = ReadNullableDoubleProperty(requestOptions, "TopP"),
            ToolChoice = CanonicalToolChoice(ReadProperty(requestOptions, "ToolChoice")),
            ThinkingEnabled = ResolveThinkingEnabled(requestOptions),
            Input = input,
            Output = output,
            Tools = tools,
            Usage = MapUsage(response.Usage),
            StopReason = OpenAIJsonHelpers.NormalizeStopReason(response.FinishReason.ToString()),
            Tags = new Dictionary<string, string>(effective.Tags, StringComparer.Ordinal),
            Metadata = MetadataWithThinkingBudget(effective.Metadata, thinkingBudget),
            Artifacts = BuildArtifactsForRequestResponse(
                effective,
                modelName,
                systemPrompt,
                input,
                output,
                tools,
                response
            ),
            Mode = GenerationMode.Sync,
        };

        GenerationValidator.Validate(generation);
        return generation;
    }

    public static Generation FromStream(
        string modelName,
        IReadOnlyList<ChatMessage> messages,
        ChatCompletionOptions? requestOptions,
        OpenAIStreamSummary summary,
        OpenAISigilOptions? options = null
    )
    {
        if (summary == null)
        {
            throw new ArgumentNullException(nameof(summary));
        }

        if (summary.FinalResponse != null)
        {
            var finalGeneration = FromRequestResponse(modelName, messages, requestOptions, summary.FinalResponse, options);
            finalGeneration.Mode = GenerationMode.Stream;
            return AppendStreamEventsArtifact(finalGeneration, summary, options);
        }

        if (summary.Updates.Count == 0)
        {
            throw new ArgumentException("stream summary must contain updates or a final response", nameof(summary));
        }

        var effective = options ?? new OpenAISigilOptions();
        var requestMessages = messages ?? Array.Empty<ChatMessage>();
        var (input, systemPrompt) = MapRequestMessages(requestMessages);

        var responseId = string.Empty;
        var responseModel = modelName;
        var stopReason = string.Empty;
        var usage = new TokenUsage();
        var textBuilder = new StringBuilder();

        var toolCalls = new Dictionary<int, StreamToolCall>();
        var toolOrder = new List<int>();

        foreach (var update in summary.Updates)
        {
            if (!string.IsNullOrWhiteSpace(update.CompletionId))
            {
                responseId = update.CompletionId;
            }

            if (!string.IsNullOrWhiteSpace(update.Model))
            {
                responseModel = update.Model;
            }

            if (update.Usage != null)
            {
                usage = MapUsage(update.Usage);
            }

            if (update.FinishReason.HasValue)
            {
                stopReason = OpenAIJsonHelpers.NormalizeStopReason(update.FinishReason.Value.ToString());
            }

            foreach (var part in update.ContentUpdate)
            {
                if (part.Kind == ChatMessageContentPartKind.Text && !string.IsNullOrWhiteSpace(part.Text))
                {
                    textBuilder.Append(part.Text);
                }

                if (part.Kind == ChatMessageContentPartKind.Refusal && !string.IsNullOrWhiteSpace(part.Refusal))
                {
                    textBuilder.Append(part.Refusal);
                }
            }

            foreach (var toolCallUpdate in update.ToolCallUpdates)
            {
                if (!toolCalls.TryGetValue(toolCallUpdate.Index, out var call))
                {
                    call = new StreamToolCall();
                    toolCalls[toolCallUpdate.Index] = call;
                    toolOrder.Add(toolCallUpdate.Index);
                }

                if (!string.IsNullOrWhiteSpace(toolCallUpdate.ToolCallId))
                {
                    call.Id = toolCallUpdate.ToolCallId;
                }

                if (!string.IsNullOrWhiteSpace(toolCallUpdate.FunctionName))
                {
                    call.Name = toolCallUpdate.FunctionName;
                }

                var chunk = toolCallUpdate.FunctionArgumentsUpdate?.ToString() ?? string.Empty;
                if (!string.IsNullOrWhiteSpace(chunk))
                {
                    call.Arguments.Append(chunk);
                }
            }
        }

        var assistantParts = new List<Part>(Math.Max(1, toolOrder.Count + 1));
        var generated = textBuilder.ToString().Trim();
        if (generated.Length > 0)
        {
            assistantParts.Add(Part.TextPart(generated));
        }

        foreach (var index in toolOrder)
        {
            if (!toolCalls.TryGetValue(index, out var call) || string.IsNullOrWhiteSpace(call.Name))
            {
                continue;
            }

            var part = Part.ToolCallPart(new ToolCall
            {
                Id = call.Id,
                Name = call.Name,
                InputJson = OpenAIJsonHelpers.ParseJsonOrString(call.Arguments.ToString()),
            });
            part.Metadata.ProviderType = "tool_call";
            assistantParts.Add(part);
        }

        var output = new List<Message>();
        if (assistantParts.Count > 0)
        {
            output.Add(new Message
            {
                Role = MessageRole.Assistant,
                Parts = assistantParts,
            });
        }

        var tools = MapTools(requestOptions);
        var thinkingBudget = ResolveThinkingBudget(requestOptions);

        var generation = new Generation
        {
            ConversationId = effective.ConversationId,
            AgentName = effective.AgentName,
            AgentVersion = effective.AgentVersion,
            Model = new ModelRef
            {
                Provider = effective.ProviderName,
                Name = modelName,
            },
            ResponseId = responseId,
            ResponseModel = responseModel,
            SystemPrompt = systemPrompt,
            MaxTokens = ResolveRequestMaxTokens(requestOptions),
            Temperature = ReadNullableDoubleProperty(requestOptions, "Temperature"),
            TopP = ReadNullableDoubleProperty(requestOptions, "TopP"),
            ToolChoice = CanonicalToolChoice(ReadProperty(requestOptions, "ToolChoice")),
            ThinkingEnabled = ResolveThinkingEnabled(requestOptions),
            Input = input,
            Output = output,
            Tools = tools,
            Usage = usage,
            StopReason = stopReason,
            Tags = new Dictionary<string, string>(effective.Tags, StringComparer.Ordinal),
            Metadata = MetadataWithThinkingBudget(effective.Metadata, thinkingBudget),
            Artifacts = BuildArtifactsForStream(
                effective,
                modelName,
                systemPrompt,
                input,
                output,
                tools,
                summary
            ),
            Mode = GenerationMode.Stream,
        };

        GenerationValidator.Validate(generation);
        return generation;
    }

    private static (List<Message> input, string systemPrompt) MapRequestMessages(IReadOnlyList<ChatMessage> messages)
    {
        var input = new List<Message>(messages.Count);
        var systemChunks = new List<string>();

        foreach (var message in messages)
        {
            switch (message)
            {
                case SystemChatMessage:
                    systemChunks.Add(ExtractMessageText(message.Content));
                    continue;
                case ToolChatMessage toolMessage:
                    {
                        var content = ExtractMessageText(toolMessage.Content);
                        if (content.Length == 0)
                        {
                            continue;
                        }

                        var part = Part.ToolResultPart(new ToolResult
                        {
                            ToolCallId = toolMessage.ToolCallId,
                            Content = content,
                            ContentJson = OpenAIJsonHelpers.ParseJsonOrString(content),
                        });
                        part.Metadata.ProviderType = "tool_result";

                        input.Add(new Message
                        {
                            Role = MessageRole.Tool,
                            Parts = new List<Part> { part },
                        });
                        continue;
                    }
                case AssistantChatMessage assistantMessage:
                    {
                        var parts = new List<Part>();
                        parts.AddRange(MapContentParts(assistantMessage.Content));

                        foreach (var call in assistantMessage.ToolCalls)
                        {
                            var part = Part.ToolCallPart(new ToolCall
                            {
                                Id = call.Id,
                                Name = call.FunctionName,
                                InputJson = OpenAIJsonHelpers.ToBytes(call.FunctionArguments),
                            });
                            part.Metadata.ProviderType = "tool_call";
                            parts.Add(part);
                        }

                        if (parts.Count > 0)
                        {
                            input.Add(new Message
                            {
                                Role = MessageRole.Assistant,
                                Parts = parts,
                            });
                        }

                        continue;
                    }
                case UserChatMessage:
                    {
                        var parts = MapContentParts(message.Content);
                        if (parts.Count > 0)
                        {
                            input.Add(new Message
                            {
                                Role = MessageRole.User,
                                Parts = parts,
                            });
                        }

                        continue;
                    }
                default:
                    {
                        if (message.GetType().Name == "DeveloperChatMessage")
                        {
                            systemChunks.Add(ExtractMessageText(message.Content));
                            continue;
                        }

                        var parts = MapContentParts(message.Content);
                        if (parts.Count > 0)
                        {
                            input.Add(new Message
                            {
                                Role = MessageRole.User,
                                Parts = parts,
                            });
                        }

                        continue;
                    }
            }
        }

        return (input, OpenAIJsonHelpers.MergeSystemPrompt(systemChunks));
    }

    private static List<Message> MapResponseMessages(ChatCompletion response)
    {
        var parts = new List<Part>();

        parts.AddRange(MapContentParts(response.Content));

        if (!string.IsNullOrWhiteSpace(response.Refusal))
        {
            parts.Add(Part.TextPart(response.Refusal));
        }

        foreach (var toolCall in response.ToolCalls)
        {
            var part = Part.ToolCallPart(new ToolCall
            {
                Id = toolCall.Id,
                Name = toolCall.FunctionName,
                InputJson = OpenAIJsonHelpers.ToBytes(toolCall.FunctionArguments),
            });
            part.Metadata.ProviderType = "tool_call";
            parts.Add(part);
        }

        if (parts.Count == 0)
        {
            return new List<Message>();
        }

        return new List<Message>
        {
            new()
            {
                Role = MessageRole.Assistant,
                Parts = parts,
            },
        };
    }

    private static List<Part> MapContentParts(ChatMessageContent? content)
    {
        var parts = new List<Part>();
        if (content == null)
        {
            return parts;
        }

        foreach (var item in content)
        {
            if (item.Kind == ChatMessageContentPartKind.Text && !string.IsNullOrWhiteSpace(item.Text))
            {
                parts.Add(Part.TextPart(item.Text));
                continue;
            }

            if (item.Kind == ChatMessageContentPartKind.Refusal && !string.IsNullOrWhiteSpace(item.Refusal))
            {
                parts.Add(Part.TextPart(item.Refusal));
            }
        }

        return parts;
    }

    private static string ExtractMessageText(ChatMessageContent? content)
    {
        if (content == null)
        {
            return string.Empty;
        }

        var chunks = new List<string>(content.Count);
        foreach (var item in content)
        {
            if (item.Kind == ChatMessageContentPartKind.Text && !string.IsNullOrWhiteSpace(item.Text))
            {
                chunks.Add(item.Text);
                continue;
            }

            if (item.Kind == ChatMessageContentPartKind.Refusal && !string.IsNullOrWhiteSpace(item.Refusal))
            {
                chunks.Add(item.Refusal);
            }
        }

        return string.Join("\n", chunks);
    }

    private static List<ToolDefinition> MapTools(ChatCompletionOptions? requestOptions)
    {
        var mapped = new List<ToolDefinition>();
        if (requestOptions == null)
        {
            return mapped;
        }

        foreach (var tool in requestOptions.Tools)
        {
            if (string.IsNullOrWhiteSpace(tool.FunctionName))
            {
                continue;
            }

            mapped.Add(new ToolDefinition
            {
                Name = tool.FunctionName,
                Description = tool.FunctionDescription ?? string.Empty,
                Type = tool.Kind.ToString().ToLowerInvariant(),
                InputSchemaJson = OpenAIJsonHelpers.ToBytes(tool.FunctionParameters),
            });
        }

        return mapped;
    }

    private static TokenUsage MapUsage(ChatTokenUsage? usage)
    {
        if (usage == null)
        {
            return new TokenUsage();
        }

        var mapped = new TokenUsage
        {
            InputTokens = usage.InputTokenCount,
            OutputTokens = usage.OutputTokenCount,
            TotalTokens = usage.TotalTokenCount,
            CacheReadInputTokens = usage.InputTokenDetails?.CachedTokenCount ?? 0,
            ReasoningTokens = usage.OutputTokenDetails?.ReasoningTokenCount ?? 0,
        };

        if (mapped.TotalTokens == 0)
        {
            mapped.TotalTokens = mapped.InputTokens + mapped.OutputTokens;
        }

        return mapped;
    }

    private static List<Artifact> BuildArtifactsForRequestResponse(
        OpenAISigilOptions options,
        string modelName,
        string systemPrompt,
        IReadOnlyList<Message> input,
        IReadOnlyList<Message> output,
        IReadOnlyList<ToolDefinition> tools,
        ChatCompletion response
    )
    {
        var artifacts = new List<Artifact>(3);

        if (options.IncludeRequestArtifact)
        {
            artifacts.Add(Artifact.JsonArtifact(ArtifactKind.Request, "openai.chat.request", new
            {
                model = modelName,
                system_prompt = systemPrompt,
                input,
            }));
        }

        if (options.IncludeResponseArtifact)
        {
            artifacts.Add(Artifact.JsonArtifact(ArtifactKind.Response, "openai.chat.response", new
            {
                id = response.Id,
                model = response.Model,
                finish_reason = OpenAIJsonHelpers.NormalizeStopReason(response.FinishReason.ToString()),
                output,
                usage = MapUsage(response.Usage),
            }));
        }

        if (options.IncludeToolsArtifact && tools.Count > 0)
        {
            artifacts.Add(Artifact.JsonArtifact(ArtifactKind.Tools, "openai.chat.tools", tools));
        }

        return artifacts;
    }

    private static List<Artifact> BuildArtifactsForStream(
        OpenAISigilOptions options,
        string modelName,
        string systemPrompt,
        IReadOnlyList<Message> input,
        IReadOnlyList<Message> output,
        IReadOnlyList<ToolDefinition> tools,
        OpenAIStreamSummary summary
    )
    {
        var artifacts = new List<Artifact>(4);

        if (options.IncludeRequestArtifact)
        {
            artifacts.Add(Artifact.JsonArtifact(ArtifactKind.Request, "openai.chat.request", new
            {
                model = modelName,
                system_prompt = systemPrompt,
                input,
            }));
        }

        if (options.IncludeToolsArtifact && tools.Count > 0)
        {
            artifacts.Add(Artifact.JsonArtifact(ArtifactKind.Tools, "openai.chat.tools", tools));
        }

        if (options.IncludeEventsArtifact)
        {
            artifacts.Add(Artifact.JsonArtifact(ArtifactKind.ProviderEvent, "openai.chat.stream_events", summary.Updates));
        }

        if (options.IncludeResponseArtifact && summary.FinalResponse != null)
        {
            artifacts.Add(Artifact.JsonArtifact(ArtifactKind.Response, "openai.chat.response", summary.FinalResponse));
        }

        return artifacts;
    }

    private static Generation AppendStreamEventsArtifact(Generation generation, OpenAIStreamSummary summary, OpenAISigilOptions? options)
    {
        var effective = options ?? new OpenAISigilOptions();
        if (!effective.IncludeEventsArtifact || summary.Updates.Count == 0)
        {
            return generation;
        }

        generation.Artifacts.Add(Artifact.JsonArtifact(
            ArtifactKind.ProviderEvent,
            "openai.chat.stream_events",
            summary.Updates
        ));
        return generation;
    }

    private sealed class StreamToolCall
    {
        public string Id { get; set; } = string.Empty;

        public string Name { get; set; } = string.Empty;

        public StringBuilder Arguments { get; } = new();
    }

    private static long? ResolveRequestMaxTokens(ChatCompletionOptions? requestOptions)
    {
        return ReadNullableLongProperty(requestOptions, "MaxCompletionTokens", "MaxTokens", "MaxOutputTokenCount");
    }

    private static bool? ResolveThinkingEnabled(ChatCompletionOptions? requestOptions)
    {
        if (requestOptions == null)
        {
            return null;
        }

        var reasoning = ReadProperty(requestOptions, "Reasoning")
            ?? ReadProperty(requestOptions, "ReasoningEffortLevel")
            ?? ReadProperty(requestOptions, "ReasoningOptions");
        return reasoning == null ? null : true;
    }

    private static long? ResolveThinkingBudget(ChatCompletionOptions? requestOptions)
    {
        if (requestOptions == null)
        {
            return null;
        }

        var reasoning = ReadProperty(requestOptions, "Reasoning")
            ?? ReadProperty(requestOptions, "ReasoningOptions");
        if (reasoning == null)
        {
            return null;
        }

        return ReadNullableLongProperty(
            reasoning,
            "BudgetTokens",
            "ThinkingBudget",
            "MaxOutputTokens",
            "MaxCompletionTokens",
            "budget_tokens",
            "thinking_budget",
            "max_output_tokens"
        );
    }

    private static object? ReadProperty(object? instance, params string[] names)
    {
        if (instance == null)
        {
            return null;
        }

        if (instance is IReadOnlyDictionary<string, object?> readOnlyMap)
        {
            foreach (var name in names)
            {
                if (readOnlyMap.TryGetValue(name, out var mappedValue) && mappedValue != null)
                {
                    return mappedValue;
                }
            }
        }

        if (instance is IDictionary<string, object?> map)
        {
            foreach (var name in names)
            {
                if (map.TryGetValue(name, out var mappedValue) && mappedValue != null)
                {
                    return mappedValue;
                }
            }
        }

        if (instance is JsonElement json && json.ValueKind == JsonValueKind.Object)
        {
            foreach (var name in names)
            {
                if (json.TryGetProperty(name, out var value))
                {
                    return value;
                }
            }
        }

        var flags = System.Reflection.BindingFlags.Instance
            | System.Reflection.BindingFlags.Public
            | System.Reflection.BindingFlags.NonPublic
            | System.Reflection.BindingFlags.IgnoreCase;

        foreach (var name in names)
        {
            var property = instance.GetType().GetProperty(name, flags);
            if (property == null)
            {
                var field = instance.GetType().GetField(name, flags);
                if (field == null)
                {
                    continue;
                }

                var fieldValue = field.GetValue(instance);
                if (fieldValue != null)
                {
                    return fieldValue;
                }

                continue;
            }

            var value = property.GetValue(instance);
            if (value != null)
            {
                return value;
            }
        }

        return null;
    }

    private static Dictionary<string, object?> MetadataWithThinkingBudget(
        IReadOnlyDictionary<string, object?> metadata,
        long? thinkingBudget
    )
    {
        var outMetadata = new Dictionary<string, object?>(metadata, StringComparer.Ordinal);
        if (thinkingBudget.HasValue)
        {
            outMetadata[ThinkingBudgetMetadataKey] = thinkingBudget.Value;
        }
        return outMetadata;
    }

    private static long? ReadNullableLongProperty(object? instance, params string[] names)
    {
        var value = ReadProperty(instance, names);
        if (value == null)
        {
            return null;
        }

        return value switch
        {
            long v => v,
            int v => v,
            short v => v,
            uint v => v,
            ulong v => (long)v,
            float v => (long)v,
            double v => (long)v,
            decimal v => (long)v,
            _ when long.TryParse(value.ToString(), out var parsed) => parsed,
            _ => null,
        };
    }

    private static double? ReadNullableDoubleProperty(object? instance, params string[] names)
    {
        var value = ReadProperty(instance, names);
        if (value == null)
        {
            return null;
        }

        return value switch
        {
            double v => v,
            float v => v,
            decimal v => (double)v,
            long v => v,
            int v => v,
            _ when double.TryParse(value.ToString(), out var parsed) => parsed,
            _ => null,
        };
    }

    private static string? CanonicalToolChoice(object? value)
    {
        if (value == null)
        {
            return null;
        }

        if (value is string text)
        {
            var normalized = text.Trim().ToLowerInvariant();
            return normalized.Length == 0 ? null : normalized;
        }

        if (value is Enum enumValue)
        {
            var normalized = enumValue.ToString().Trim().ToLowerInvariant();
            return normalized.Length == 0 ? null : normalized;
        }

        var structured = TryMapStructuredToolChoice(value);
        if (!string.IsNullOrWhiteSpace(structured))
        {
            return structured;
        }

        try
        {
            var element = JsonSerializer.SerializeToElement(value);
            var canonical = CanonicalJson(element);
            if (canonical == "{}")
            {
                var normalized = value.ToString()?.Trim().ToLowerInvariant();
                return string.IsNullOrWhiteSpace(normalized) ? null : normalized;
            }

            return canonical;
        }
        catch
        {
            var normalized = value.ToString()?.Trim().ToLowerInvariant();
            return string.IsNullOrWhiteSpace(normalized) ? null : normalized;
        }
    }

    private static string? TryMapStructuredToolChoice(object value)
    {
        var kind = ReadProperty(value, "Kind", "Type", "Mode", "_type", "_predefinedValue")?.ToString()?.Trim();
        var functionName = ReadProperty(value, "FunctionName", "Name")?.ToString()?.Trim();

        if (string.IsNullOrWhiteSpace(functionName))
        {
            var function = ReadProperty(value, "Function", "_function");
            functionName = ReadProperty(function, "Name", "<Name>k__BackingField")?.ToString()?.Trim();
        }

        var normalizedKind = kind?.ToLowerInvariant();
        if (string.IsNullOrWhiteSpace(functionName)
            && (normalizedKind == "none" || normalizedKind == "auto" || normalizedKind == "required"))
        {
            return normalizedKind;
        }

        if (string.IsNullOrWhiteSpace(kind) && string.IsNullOrWhiteSpace(functionName))
        {
            return null;
        }

        var toolChoice = new SortedDictionary<string, object?>(StringComparer.Ordinal);
        if (!string.IsNullOrWhiteSpace(kind))
        {
            toolChoice["type"] = normalizedKind;
        }

        if (!string.IsNullOrWhiteSpace(functionName))
        {
            toolChoice["function"] = new SortedDictionary<string, object?>(StringComparer.Ordinal)
            {
                ["name"] = functionName,
            };
        }

        return JsonSerializer.Serialize(toolChoice);
    }

    private static string CanonicalJson(JsonElement element)
    {
        using var stream = new MemoryStream();
        using var writer = new Utf8JsonWriter(stream);
        WriteCanonicalElement(writer, element);
        writer.Flush();
        return Encoding.UTF8.GetString(stream.ToArray());
    }

    private static void WriteCanonicalElement(Utf8JsonWriter writer, JsonElement element)
    {
        switch (element.ValueKind)
        {
            case JsonValueKind.Object:
                writer.WriteStartObject();
                foreach (var property in element.EnumerateObject().OrderBy(property => property.Name, StringComparer.Ordinal))
                {
                    writer.WritePropertyName(property.Name);
                    WriteCanonicalElement(writer, property.Value);
                }

                writer.WriteEndObject();
                break;
            case JsonValueKind.Array:
                writer.WriteStartArray();
                foreach (var item in element.EnumerateArray())
                {
                    WriteCanonicalElement(writer, item);
                }

                writer.WriteEndArray();
                break;
            default:
                element.WriteTo(writer);
                break;
        }
    }
}
