using Anthropic.Models.Messages;
using AnthropicMessage = Anthropic.Models.Messages.Message;

namespace Grafana.Sigil.Anthropic;

public sealed class AnthropicStreamSummary
{
    public List<RawMessageStreamEvent> Events { get; } = new();

    public AnthropicMessage? FinalMessage { get; set; }
}
