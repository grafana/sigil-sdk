using System.Threading;

namespace Grafana.Sigil;

public static class SigilContext
{
    private static readonly AsyncLocal<string?> ConversationIdSlot = new();
    private static readonly AsyncLocal<string?> AgentNameSlot = new();
    private static readonly AsyncLocal<string?> AgentVersionSlot = new();

    public static IDisposable WithConversationId(string conversationId)
    {
        return new AsyncLocalScope(ConversationIdSlot, conversationId);
    }

    public static IDisposable WithAgentName(string agentName)
    {
        return new AsyncLocalScope(AgentNameSlot, agentName);
    }

    public static IDisposable WithAgentVersion(string agentVersion)
    {
        return new AsyncLocalScope(AgentVersionSlot, agentVersion);
    }

    public static string? ConversationIdFromContext()
    {
        return ConversationIdSlot.Value;
    }

    public static string? AgentNameFromContext()
    {
        return AgentNameSlot.Value;
    }

    public static string? AgentVersionFromContext()
    {
        return AgentVersionSlot.Value;
    }

    private sealed class AsyncLocalScope : IDisposable
    {
        private readonly AsyncLocal<string?> _slot;
        private readonly string? _previousValue;
        private bool _disposed;

        public AsyncLocalScope(AsyncLocal<string?> slot, string value)
        {
            _slot = slot;
            _previousValue = slot.Value;
            _slot.Value = value;
        }

        public void Dispose()
        {
            if (_disposed)
            {
                return;
            }

            _slot.Value = _previousValue;
            _disposed = true;
        }
    }
}
