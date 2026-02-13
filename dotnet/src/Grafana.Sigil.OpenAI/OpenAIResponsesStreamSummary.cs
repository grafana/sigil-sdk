using OpenAI.Responses;

namespace Grafana.Sigil.OpenAI;

public sealed class OpenAIResponsesStreamSummary
{
    public List<StreamingResponseUpdate> Events { get; } = new();

    public OpenAIResponse? FinalResponse { get; set; }
}
