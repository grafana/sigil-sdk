namespace Grafana.Sigil;

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
    Bearer,
    Basic
}

public sealed class AuthConfig
{
    public ExportAuthMode Mode { get; set; } = ExportAuthMode.None;
    public string TenantId { get; set; } = string.Empty;
    public string BearerToken { get; set; } = string.Empty;
    /// <summary>Username for basic auth. When empty, TenantId is used.</summary>
    public string BasicUser { get; set; } = string.Empty;
    /// <summary>Password/token for basic auth.</summary>
    public string BasicPassword { get; set; } = string.Empty;
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

public sealed class ApiConfig
{
    public string Endpoint { get; set; } = "http://localhost:8080";
}

public sealed class SigilClientConfig
{
    public GenerationExportConfig GenerationExport { get; set; } = new();
    public ApiConfig Api { get; set; } = new();
    public EmbeddingCaptureConfig EmbeddingCapture { get; set; } = new();
    public ContentCaptureMode ContentCapture { get; set; } = ContentCaptureMode.Default;
    public Func<IReadOnlyDictionary<string, object?>?, ContentCaptureMode>? ContentCaptureResolver { get; set; }
    public Action<string>? Logger { get; set; }
    public Func<DateTimeOffset>? UtcNow { get; set; }
    public Func<TimeSpan, CancellationToken, Task>? SleepAsync { get; set; }
    public IGenerationExporter? GenerationExporter { get; set; }
}

public sealed class EmbeddingCaptureConfig
{
    public bool CaptureInput { get; set; }
    public int MaxInputItems { get; set; } = 20;
    public int MaxTextLength { get; set; } = 1024;
}

internal static class ConfigResolver
{
    internal const string TenantHeaderName = "X-Scope-OrgID";
    internal const string AuthorizationHeaderName = "Authorization";

    public static SigilClientConfig Resolve(SigilClientConfig? config)
    {
        var resolved = config ?? new SigilClientConfig();

        resolved.Logger ??= _ => { };

#if NET8_0_OR_GREATER
        resolved.UtcNow ??= TimeProvider.System.GetUtcNow;
#else
        resolved.UtcNow ??= () => DateTimeOffset.UtcNow;
#endif

        resolved.SleepAsync ??= static (delay, ct) => Task.Delay(delay, ct);

        resolved.GenerationExport.Headers = ResolveHeadersWithAuth(
            resolved.GenerationExport.Headers,
            resolved.GenerationExport.Auth,
            "generation"
        );
        if (string.IsNullOrWhiteSpace(resolved.Api.Endpoint))
        {
            resolved.Api.Endpoint = "http://localhost:8080";
        }

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

        resolved.EmbeddingCapture ??= new EmbeddingCaptureConfig();

        if (resolved.EmbeddingCapture.MaxInputItems <= 0)
        {
            resolved.EmbeddingCapture.MaxInputItems = 20;
        }
        if (resolved.EmbeddingCapture.MaxTextLength <= 0)
        {
            resolved.EmbeddingCapture.MaxTextLength = 1024;
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
                var noneBasicUser = auth.BasicUser?.Trim() ?? string.Empty;
                var noneBasicPassword = auth.BasicPassword?.Trim() ?? string.Empty;
                if (tenantId.Length > 0 || bearerToken.Length > 0 || noneBasicUser.Length > 0 || noneBasicPassword.Length > 0)
                {
                    throw new ArgumentException($"{label} auth mode 'none' does not allow credentials");
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
            case ExportAuthMode.Basic:
                var basicPassword = auth.BasicPassword?.Trim() ?? string.Empty;
                if (basicPassword.Length == 0)
                {
                    throw new ArgumentException($"{label} auth mode 'basic' requires basic_password");
                }

                var basicUser = auth.BasicUser?.Trim() ?? string.Empty;
                if (basicUser.Length == 0)
                {
                    basicUser = tenantId;
                }

                if (basicUser.Length == 0)
                {
                    throw new ArgumentException($"{label} auth mode 'basic' requires basic_user or tenant_id");
                }

                if (!resolved.ContainsKey(AuthorizationHeaderName))
                {
                    var encoded = Convert.ToBase64String(
                        System.Text.Encoding.UTF8.GetBytes($"{basicUser}:{basicPassword}"));
                    resolved[AuthorizationHeaderName] = $"Basic {encoded}";
                }

                if (tenantId.Length > 0 && !resolved.ContainsKey(TenantHeaderName))
                {
                    resolved[TenantHeaderName] = tenantId;
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
#if NET
            value = value["Bearer ".Length..].Trim();
#else
            value = value.Substring("Bearer ".Length).Trim();
#endif
        }

        return $"Bearer {value}";
    }
}
