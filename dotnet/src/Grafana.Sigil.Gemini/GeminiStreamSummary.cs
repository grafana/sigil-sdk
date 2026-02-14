using Google.GenAI.Types;

namespace Grafana.Sigil.Gemini;

public sealed class GeminiStreamSummary
{
    public List<GenerateContentResponse> Responses { get; } = new();

    public DateTimeOffset? FirstChunkAt { get; set; }
}
