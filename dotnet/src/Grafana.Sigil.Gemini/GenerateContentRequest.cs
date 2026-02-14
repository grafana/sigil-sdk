using Google.GenAI.Types;

namespace Grafana.Sigil.Gemini;

internal sealed record GenerateContentRequest
{
    public string Model { get; init; } = string.Empty;

    public List<Content> Contents { get; init; } = new();

    public GenerateContentConfig? Config { get; init; }
}
