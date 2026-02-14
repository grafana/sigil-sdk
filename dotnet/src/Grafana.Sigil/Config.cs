namespace Grafana.Sigil;

public enum TraceProtocol
{
    Grpc,
    Http
}

public enum GenerationExportProtocol
{
    Grpc,
    Http,
    None
}

public enum ExportAuthMode
{
    None,
    Tenant,
    Bearer
}

public sealed class AuthConfig
{
    public ExportAuthMode Mode { get; set; } = ExportAuthMode.None;
    public string TenantId { get; set; } = string.Empty;
    public string BearerToken { get; set; } = string.Empty;
}

public sealed class TraceConfig
{
    public TraceProtocol Protocol { get; set; } = TraceProtocol.Http;
    public string Endpoint { get; set; } = "http://localhost:4318/v1/traces";
    public Dictionary<string, string> Headers { get; set; } = new(StringComparer.OrdinalIgnoreCase);
    public AuthConfig Auth { get; set; } = new();
    public bool Insecure { get; set; } = true;
    public bool EnableMetrics { get; set; } = true;
}

public sealed class GenerationExportConfig
{
    public GenerationExportProtocol Protocol { get; set; } = GenerationExportProtocol.Grpc;
    public string Endpoint { get; set; } = "localhost:4317";
    public Dictionary<string, string> Headers { get; set; } = new(StringComparer.OrdinalIgnoreCase);
    public AuthConfig Auth { get; set; } = new();
    public bool Insecure { get; set; } = true;
    public int BatchSize { get; set; } = 100;
    public TimeSpan FlushInterval { get; set; } = TimeSpan.FromSeconds(1);
    public int QueueSize { get; set; } = 2000;
    public int MaxRetries { get; set; } = 5;
    public TimeSpan InitialBackoff { get; set; } = TimeSpan.FromMilliseconds(100);
    public TimeSpan MaxBackoff { get; set; } = TimeSpan.FromSeconds(5);
    public int PayloadMaxBytes { get; set; } = 4 << 20;
}

public sealed class SigilClientConfig
{
    public TraceConfig Trace { get; set; } = new();
    public GenerationExportConfig GenerationExport { get; set; } = new();
    public Action<string>? Logger { get; set; }
    public Func<DateTimeOffset>? UtcNow { get; set; }
    public Func<TimeSpan, CancellationToken, Task>? SleepAsync { get; set; }
    public IGenerationExporter? GenerationExporter { get; set; }
}

internal static class ConfigResolver
{
    internal const string TenantHeaderName = "X-Scope-OrgID";
    internal const string AuthorizationHeaderName = "Authorization";

    public static SigilClientConfig Resolve(SigilClientConfig? config)
    {
        var resolved = config ?? new SigilClientConfig();

        if (resolved.Logger == null)
        {
            resolved.Logger = _ => { };
        }

        if (resolved.UtcNow == null)
        {
            resolved.UtcNow = () => DateTimeOffset.UtcNow;
        }

        if (resolved.SleepAsync == null)
        {
            resolved.SleepAsync = static (delay, ct) => Task.Delay(delay, ct);
        }

        resolved.Trace.Headers = ResolveHeadersWithAuth(resolved.Trace.Headers, resolved.Trace.Auth, "trace");
        resolved.GenerationExport.Headers = ResolveHeadersWithAuth(
            resolved.GenerationExport.Headers,
            resolved.GenerationExport.Auth,
            "generation"
        );

        if (resolved.GenerationExport.BatchSize <= 0)
        {
            resolved.GenerationExport.BatchSize = 1;
        }

        if (resolved.GenerationExport.QueueSize <= 0)
        {
            resolved.GenerationExport.QueueSize = 1;
        }

        if (resolved.GenerationExport.FlushInterval <= TimeSpan.Zero)
        {
            resolved.GenerationExport.FlushInterval = TimeSpan.FromMilliseconds(1);
        }

        if (resolved.GenerationExport.MaxRetries < 0)
        {
            resolved.GenerationExport.MaxRetries = 0;
        }

        if (resolved.GenerationExport.InitialBackoff <= TimeSpan.Zero)
        {
            resolved.GenerationExport.InitialBackoff = TimeSpan.FromMilliseconds(100);
        }

        if (resolved.GenerationExport.MaxBackoff <= TimeSpan.Zero)
        {
            resolved.GenerationExport.MaxBackoff = TimeSpan.FromMilliseconds(100);
        }

        return resolved;
    }

    public static Dictionary<string, string> ResolveHeadersWithAuth(
        Dictionary<string, string> headers,
        AuthConfig auth,
        string label
    )
    {
        var resolved = new Dictionary<string, string>(headers, StringComparer.OrdinalIgnoreCase);

        var tenantId = auth.TenantId?.Trim() ?? string.Empty;
        var bearerToken = auth.BearerToken?.Trim() ?? string.Empty;

        switch (auth.Mode)
        {
            case ExportAuthMode.None:
                if (tenantId.Length > 0 || bearerToken.Length > 0)
                {
                    throw new ArgumentException($"{label} auth mode 'none' does not allow tenant_id or bearer_token");
                }
                return resolved;
            case ExportAuthMode.Tenant:
                if (tenantId.Length == 0)
                {
                    throw new ArgumentException($"{label} auth mode 'tenant' requires tenant_id");
                }

                if (bearerToken.Length > 0)
                {
                    throw new ArgumentException($"{label} auth mode 'tenant' does not allow bearer_token");
                }

                if (!resolved.ContainsKey(TenantHeaderName))
                {
                    resolved[TenantHeaderName] = tenantId;
                }

                return resolved;
            case ExportAuthMode.Bearer:
                if (bearerToken.Length == 0)
                {
                    throw new ArgumentException($"{label} auth mode 'bearer' requires bearer_token");
                }

                if (tenantId.Length > 0)
                {
                    throw new ArgumentException($"{label} auth mode 'bearer' does not allow tenant_id");
                }

                if (!resolved.ContainsKey(AuthorizationHeaderName))
                {
                    resolved[AuthorizationHeaderName] = FormatBearerTokenValue(bearerToken);
                }

                return resolved;
            default:
                throw new ArgumentException($"unsupported {label} auth mode '{auth.Mode}'");
        }
    }

    private static string FormatBearerTokenValue(string token)
    {
        var value = token.Trim();
        if (value.StartsWith("Bearer ", StringComparison.OrdinalIgnoreCase))
        {
            value = value.Substring("Bearer ".Length).Trim();
        }

        return $"Bearer {value}";
    }
}
