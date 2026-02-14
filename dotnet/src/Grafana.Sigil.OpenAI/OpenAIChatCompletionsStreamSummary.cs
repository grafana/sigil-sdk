using OpenAI.Chat;

namespace Grafana.Sigil.OpenAI;

public sealed class OpenAIChatCompletionsStreamSummary
{
    public List<StreamingChatCompletionUpdate> Updates { get; } = new();

    public ChatCompletion? FinalResponse { get; set; }

    public DateTimeOffset? FirstChunkAt { get; set; }
}
