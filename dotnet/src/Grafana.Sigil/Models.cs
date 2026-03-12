using System.Text.Json;

namespace Grafana.Sigil;

public enum GenerationMode
{
    Sync,
    Stream
}

public enum MessageRole
{
    User,
    Assistant,
    Tool
}

public enum PartKind
{
    Text,
    Thinking,
    ToolCall,
    ToolResult
}

public enum ArtifactKind
{
    Request,
    Response,
    Tools,
    ProviderEvent
}

public enum ConversationRatingValue
{
    Good,
    Bad
}

public sealed class ModelRef
{
    public string Provider { get; set; } = string.Empty;
    public string Name { get; set; } = string.Empty;
}

public sealed class ToolDefinition
{
    public string Name { get; set; } = string.Empty;
    public string Description { get; set; } = string.Empty;
    public string Type { get; set; } = string.Empty;
    public byte[] InputSchemaJson { get; set; } = Array.Empty<byte>();
}

public sealed class TokenUsage
{
    public long InputTokens { get; set; }
    public long OutputTokens { get; set; }
    public long TotalTokens { get; set; }
    public long CacheReadInputTokens { get; set; }
    public long CacheWriteInputTokens { get; set; }
    public long ReasoningTokens { get; set; }
    public long CacheCreationInputTokens { get; set; }

    public TokenUsage Normalize()
    {
        if (TotalTokens != 0)
        {
            return Clone();
        }

        var clone = Clone();
        clone.TotalTokens = clone.InputTokens + clone.OutputTokens;
        return clone;
    }

    public TokenUsage Clone()
    {
        return new TokenUsage
        {
            InputTokens = InputTokens,
            OutputTokens = OutputTokens,
            TotalTokens = TotalTokens,
            CacheReadInputTokens = CacheReadInputTokens,
            CacheWriteInputTokens = CacheWriteInputTokens,
            ReasoningTokens = ReasoningTokens,
            CacheCreationInputTokens = CacheCreationInputTokens,
        };
    }
}

public sealed class PartMetadata
{
    public string ProviderType { get; set; } = string.Empty;
}

public sealed class ToolCall
{
    public string Id { get; set; } = string.Empty;
    public string Name { get; set; } = string.Empty;
    public byte[] InputJson { get; set; } = Array.Empty<byte>();
}

public sealed class ToolResult
{
    public string ToolCallId { get; set; } = string.Empty;
    public string Name { get; set; } = string.Empty;
    public string Content { get; set; } = string.Empty;
    public byte[] ContentJson { get; set; } = Array.Empty<byte>();
    public bool IsError { get; set; }
}

public sealed class Part
{
    public PartKind Kind { get; set; }
    public string Text { get; set; } = string.Empty;
    public string Thinking { get; set; } = string.Empty;
    public ToolCall? ToolCall { get; set; }
    public ToolResult? ToolResult { get; set; }
    public PartMetadata Metadata { get; set; } = new();

    public static Part TextPart(string text)
    {
        return new Part { Kind = PartKind.Text, Text = text };
    }

    public static Part ThinkingPart(string thinking)
    {
        return new Part { Kind = PartKind.Thinking, Thinking = thinking };
    }

    public static Part ToolCallPart(ToolCall toolCall)
    {
        return new Part { Kind = PartKind.ToolCall, ToolCall = toolCall };
    }

    public static Part ToolResultPart(ToolResult toolResult)
    {
        return new Part { Kind = PartKind.ToolResult, ToolResult = toolResult };
    }
}

public sealed class Message
{
    public MessageRole Role { get; set; }
    public string Name { get; set; } = string.Empty;
    public List<Part> Parts { get; set; } = new();

    public static Message UserTextMessage(string text)
    {
        return new Message { Role = MessageRole.User, Parts = new List<Part> { Part.TextPart(text) } };
    }

    public static Message AssistantTextMessage(string text)
    {
        return new Message { Role = MessageRole.Assistant, Parts = new List<Part> { Part.TextPart(text) } };
    }

    public static Message ToolResultMessage(string toolCallId, object? content)
    {
        byte[] payload = Array.Empty<byte>();
        if (content != null)
        {
            payload = JsonSerializer.SerializeToUtf8Bytes(content);
        }

        return new Message
        {
            Role = MessageRole.Tool,
            Parts = new List<Part>
            {
                Part.ToolResultPart(new ToolResult
                {
                    ToolCallId = toolCallId,
                    ContentJson = payload,
                }),
            },
        };
    }
}

public sealed class Artifact
{
    public ArtifactKind Kind { get; set; }
    public string Name { get; set; } = string.Empty;
    public string ContentType { get; set; } = string.Empty;
    public byte[] Payload { get; set; } = Array.Empty<byte>();
    public string RecordId { get; set; } = string.Empty;
    public string Uri { get; set; } = string.Empty;

    public static Artifact JsonArtifact(ArtifactKind kind, string name, object value)
    {
        return new Artifact
        {
            Kind = kind,
            Name = name,
            ContentType = "application/json",
            Payload = JsonSerializer.SerializeToUtf8Bytes(value),
        };
    }
}

public sealed class GenerationStart
{
    public string Id { get; set; } = string.Empty;
    public string ConversationId { get; set; } = string.Empty;
    public string ConversationTitle { get; set; } = string.Empty;
    public string UserId { get; set; } = string.Empty;
    public string AgentName { get; set; } = string.Empty;
    public string AgentVersion { get; set; } = string.Empty;
    public GenerationMode? Mode { get; set; }
    public string OperationName { get; set; } = string.Empty;
    public ModelRef Model { get; set; } = new();
    public string SystemPrompt { get; set; } = string.Empty;
    public long? MaxTokens { get; set; }
    public double? Temperature { get; set; }
    public double? TopP { get; set; }
    public string? ToolChoice { get; set; }
    public bool? ThinkingEnabled { get; set; }
    public List<ToolDefinition> Tools { get; set; } = new();
    public Dictionary<string, string> Tags { get; set; } = new(StringComparer.Ordinal);
    public Dictionary<string, object?> Metadata { get; set; } = new(StringComparer.Ordinal);
    public DateTimeOffset? StartedAt { get; set; }
}

public sealed class EmbeddingStart
{
    public ModelRef Model { get; set; } = new();
    public string AgentName { get; set; } = string.Empty;
    public string AgentVersion { get; set; } = string.Empty;
    public long? Dimensions { get; set; }
    public string EncodingFormat { get; set; } = string.Empty;
    public Dictionary<string, string> Tags { get; set; } = new(StringComparer.Ordinal);
    public Dictionary<string, object?> Metadata { get; set; } = new(StringComparer.Ordinal);
    public DateTimeOffset? StartedAt { get; set; }
}

public sealed class EmbeddingResult
{
    public int InputCount { get; set; }
    public long InputTokens { get; set; }
    public List<string> InputTexts { get; set; } = new();
    public string ResponseModel { get; set; } = string.Empty;
    public long? Dimensions { get; set; }
}

public sealed class Generation
{
    public string Id { get; set; } = string.Empty;
    public string ConversationId { get; set; } = string.Empty;
    public string ConversationTitle { get; set; } = string.Empty;
    public string UserId { get; set; } = string.Empty;
    public string AgentName { get; set; } = string.Empty;
    public string AgentVersion { get; set; } = string.Empty;
    public GenerationMode? Mode { get; set; }
    public string OperationName { get; set; } = string.Empty;
    public string TraceId { get; set; } = string.Empty;
    public string SpanId { get; set; } = string.Empty;
    public ModelRef Model { get; set; } = new();
    public string ResponseId { get; set; } = string.Empty;
    public string ResponseModel { get; set; } = string.Empty;
    public string SystemPrompt { get; set; } = string.Empty;
    public long? MaxTokens { get; set; }
    public double? Temperature { get; set; }
    public double? TopP { get; set; }
    public string? ToolChoice { get; set; }
    public bool? ThinkingEnabled { get; set; }
    public List<Message> Input { get; set; } = new();
    public List<Message> Output { get; set; } = new();
    public List<ToolDefinition> Tools { get; set; } = new();
    public TokenUsage Usage { get; set; } = new();
    public string StopReason { get; set; } = string.Empty;
    public DateTimeOffset? StartedAt { get; set; }
    public DateTimeOffset? CompletedAt { get; set; }
    public Dictionary<string, string> Tags { get; set; } = new(StringComparer.Ordinal);
    public Dictionary<string, object?> Metadata { get; set; } = new(StringComparer.Ordinal);
    public List<Artifact> Artifacts { get; set; } = new();
    public string CallError { get; set; } = string.Empty;
}

public sealed class ToolExecutionStart
{
    public string ToolName { get; set; } = string.Empty;
    public string ToolCallId { get; set; } = string.Empty;
    public string ToolType { get; set; } = string.Empty;
    public string ToolDescription { get; set; } = string.Empty;
    public string ConversationId { get; set; } = string.Empty;
    public string ConversationTitle { get; set; } = string.Empty;
    public string AgentName { get; set; } = string.Empty;
    public string AgentVersion { get; set; } = string.Empty;
    /// <summary>The model that requested the tool call (e.g. "gpt-5").</summary>
    public string RequestModel { get; set; } = string.Empty;
    /// <summary>The provider that served the model (e.g. "openai").</summary>
    public string RequestProvider { get; set; } = string.Empty;
    public bool IncludeContent { get; set; }
    public DateTimeOffset? StartedAt { get; set; }
}

public sealed class ToolExecutionEnd
{
    public object? Arguments { get; set; }
    public object? Result { get; set; }
    public DateTimeOffset? CompletedAt { get; set; }
}

public sealed class ExportGenerationResult
{
    public string GenerationId { get; set; } = string.Empty;
    public bool Accepted { get; set; }
    public string Error { get; set; } = string.Empty;
}

public sealed class ExportGenerationsRequest
{
    public List<Generation> Generations { get; set; } = new();
}

public sealed class ExportGenerationsResponse
{
    public List<ExportGenerationResult> Results { get; set; } = new();
}

public sealed class SubmitConversationRatingRequest
{
    public string RatingId { get; set; } = string.Empty;
    public ConversationRatingValue Rating { get; set; } = ConversationRatingValue.Good;
    public string Comment { get; set; } = string.Empty;
    public Dictionary<string, object?> Metadata { get; set; } = new(StringComparer.Ordinal);
    public string GenerationId { get; set; } = string.Empty;
    public string RaterId { get; set; } = string.Empty;
    public string Source { get; set; } = string.Empty;
}

public sealed class ConversationRating
{
    public string RatingId { get; set; } = string.Empty;
    public string ConversationId { get; set; } = string.Empty;
    public ConversationRatingValue Rating { get; set; } = ConversationRatingValue.Good;
    public string Comment { get; set; } = string.Empty;
    public Dictionary<string, object?> Metadata { get; set; } = new(StringComparer.Ordinal);
    public string GenerationId { get; set; } = string.Empty;
    public string RaterId { get; set; } = string.Empty;
    public string Source { get; set; } = string.Empty;
    public DateTimeOffset CreatedAt { get; set; }
}

public sealed class ConversationRatingSummary
{
    public int TotalCount { get; set; }
    public int GoodCount { get; set; }
    public int BadCount { get; set; }
    public ConversationRatingValue? LatestRating { get; set; }
    public DateTimeOffset LatestRatedAt { get; set; }
    public DateTimeOffset? LatestBadAt { get; set; }
    public bool HasBadRating { get; set; }
}

public sealed class SubmitConversationRatingResponse
{
    public ConversationRating Rating { get; set; } = new();
    public ConversationRatingSummary Summary { get; set; } = new();
}
