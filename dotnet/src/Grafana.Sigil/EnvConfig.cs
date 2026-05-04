namespace Grafana.Sigil;

/// <summary>
/// Reads canonical <c>SIGIL_*</c> environment variables and layers them under
/// caller-supplied <see cref="SigilClientConfig"/> values.
/// </summary>
/// <remarks>
/// <para>Resolution order (highest precedence first):</para>
/// <list type="number">
///   <item><description>Caller-supplied <see cref="SigilClientConfig"/> field
///       (when not at its default/unset state).</description></item>
///   <item><description>Canonical <c>SIGIL_*</c> env var.</description></item>
///   <item><description>SDK schema default.</description></item>
/// </list>
/// <para>Mirrors the Go reference implementation in
/// <c>go/sigil/env.go</c>. Invalid env values are skipped with a warning so
/// a single typo does not discard the rest of the env layer.</para>
/// </remarks>
public static class EnvConfig
{
    public const string EnvEndpoint = "SIGIL_ENDPOINT";
    public const string EnvProtocol = "SIGIL_PROTOCOL";
    public const string EnvInsecure = "SIGIL_INSECURE";
    public const string EnvHeaders = "SIGIL_HEADERS";
    public const string EnvAuthMode = "SIGIL_AUTH_MODE";
    public const string EnvAuthTenantId = "SIGIL_AUTH_TENANT_ID";
    public const string EnvAuthToken = "SIGIL_AUTH_TOKEN";
    public const string EnvAgentName = "SIGIL_AGENT_NAME";
    public const string EnvAgentVersion = "SIGIL_AGENT_VERSION";
    public const string EnvUserId = "SIGIL_USER_ID";
    public const string EnvTags = "SIGIL_TAGS";
    public const string EnvContentCaptureMode = "SIGIL_CONTENT_CAPTURE_MODE";
    public const string EnvDebug = "SIGIL_DEBUG";

    internal const string DefaultEndpoint = "localhost:4317";

    /// <summary>
    /// Returns a config built from process env vars layered onto a fresh
    /// <see cref="SigilClientConfig"/>. Convenience helper; most callers should
    /// let <see cref="SigilClient"/> construction perform the same resolution
    /// internally. Warnings for invalid env values are written to stderr.
    /// </summary>
    public static SigilClientConfig FromEnv()
    {
        var (cfg, warnings) = ResolveFromEnv(Environment.GetEnvironmentVariable, new SigilClientConfig());
        LogWarnings(Console.Error.WriteLine, warnings);
        return cfg;
    }

    /// <summary>
    /// Applies canonical <c>SIGIL_*</c> env values onto <paramref name="base"/>,
    /// preserving caller-supplied fields. Mutates <paramref name="base"/> in
    /// place to match the existing <c>ConfigResolver.Resolve</c> semantics so
    /// callers can read back resolved auth headers and other normalised fields
    /// from the config object they supplied.
    /// </summary>
    /// <returns>
    /// Tuple of the resolved config (same reference as <paramref name="base"/>)
    /// and a list of warnings emitted for invalid env values (e.g. unknown
    /// <c>SIGIL_AUTH_MODE</c>).
    /// </returns>
    internal static (SigilClientConfig config, IReadOnlyList<string> warnings) ResolveFromEnv(
        Func<string, string?> lookup,
        SigilClientConfig @base
    )
    {
        var src = lookup ?? Environment.GetEnvironmentVariable;
        var cfg = @base ?? new SigilClientConfig();
        EnsureNonNullNested(cfg);
        var warnings = new List<string>();

        var export = cfg.GenerationExport;

        var endpoint = EnvTrimmed(src, EnvEndpoint);
        if (endpoint != null && string.IsNullOrEmpty(export.Endpoint))
        {
            export.Endpoint = endpoint;
        }

        var protocol = EnvTrimmed(src, EnvProtocol);
        if (protocol != null && export.Protocol == null)
        {
            var parsed = ParseProtocol(protocol);
            if (parsed.HasValue)
            {
                export.Protocol = parsed.Value;
            }
            else
            {
                warnings.Add($"sigil: ignoring invalid {EnvProtocol} {protocol}");
            }
        }

        var insecureRaw = EnvTrimmed(src, EnvInsecure);
        if (insecureRaw != null && export.Insecure == null)
        {
            export.Insecure = ParseBool(insecureRaw);
        }

        var headersRaw = EnvTrimmed(src, EnvHeaders);
        if (headersRaw != null && (export.Headers == null || export.Headers.Count == 0))
        {
            // Headers are HTTP-style and case-insensitive at the consumer; the
            // env-derived map mirrors that. Built element-by-element so case-mixed
            // duplicates (x-a=1,X-A=2) collapse to last-wins instead of throwing.
            var headers = new Dictionary<string, string>(StringComparer.OrdinalIgnoreCase);
            foreach (var kv in ParseCsvKv(headersRaw))
            {
                headers[kv.Key] = kv.Value;
            }
            export.Headers = headers;
        }

        var auth = export.Auth ??= new AuthConfig();
        var authModeRaw = EnvTrimmed(src, EnvAuthMode);
        if (authModeRaw != null && auth.Mode == null)
        {
            var parsed = ParseAuthMode(authModeRaw);
            if (parsed.HasValue)
            {
                auth.Mode = parsed.Value;
            }
            else
            {
                warnings.Add($"sigil: ignoring invalid {EnvAuthMode} {authModeRaw}");
            }
        }

        var tenantId = EnvTrimmed(src, EnvAuthTenantId);
        if (tenantId != null && string.IsNullOrEmpty(auth.TenantId))
        {
            auth.TenantId = tenantId;
        }

        var token = EnvTrimmed(src, EnvAuthToken);
        if (token != null)
        {
            // Set both fields when empty; ResolveHeadersWithAuth uses only the
            // one matching the final mode. Lets env's token populate a
            // caller-set mode without env declaring SIGIL_AUTH_MODE.
            if (string.IsNullOrEmpty(auth.BearerToken))
            {
                auth.BearerToken = token;
            }
            if (string.IsNullOrEmpty(auth.BasicPassword))
            {
                auth.BasicPassword = token;
            }
        }
        if (auth.Mode == ExportAuthMode.Basic
            && string.IsNullOrEmpty(auth.BasicUser)
            && !string.IsNullOrEmpty(auth.TenantId))
        {
            auth.BasicUser = auth.TenantId;
        }

        // Finalize tri-state defaults after env layering: null/empty means
        // "no caller value and no env value", so apply the schema default.
        export.Protocol ??= GenerationExportProtocol.Grpc;
        if (string.IsNullOrEmpty(export.Endpoint))
        {
            export.Endpoint = DefaultEndpoint;
        }
        auth.Mode ??= ExportAuthMode.None;

        var agentName = EnvTrimmed(src, EnvAgentName);
        if (agentName != null && string.IsNullOrEmpty(cfg.AgentName))
        {
            cfg.AgentName = agentName;
        }
        var agentVersion = EnvTrimmed(src, EnvAgentVersion);
        if (agentVersion != null && string.IsNullOrEmpty(cfg.AgentVersion))
        {
            cfg.AgentVersion = agentVersion;
        }
        var userId = EnvTrimmed(src, EnvUserId);
        if (userId != null && string.IsNullOrEmpty(cfg.UserId))
        {
            cfg.UserId = userId;
        }

        var tagsRaw = EnvTrimmed(src, EnvTags);
        if (tagsRaw != null)
        {
            var envTags = ParseCsvKv(tagsRaw);
            // Env tags act as a base layer; caller tags win on collision.
            var merged = new Dictionary<string, string>(envTags);
            if (cfg.Tags != null)
            {
                foreach (var kv in cfg.Tags)
                {
                    merged[kv.Key] = kv.Value;
                }
            }
            cfg.Tags = merged;
        }

        var ccmRaw = EnvTrimmed(src, EnvContentCaptureMode);
        if (ccmRaw != null && cfg.ContentCapture == ContentCaptureMode.Default)
        {
            var parsed = ParseContentCaptureMode(ccmRaw);
            if (parsed.HasValue)
            {
                cfg.ContentCapture = parsed.Value;
            }
            else
            {
                warnings.Add($"sigil: ignoring invalid {EnvContentCaptureMode} {ccmRaw}");
            }
        }

        var debugRaw = EnvTrimmed(src, EnvDebug);
        if (debugRaw != null && cfg.Debug == null)
        {
            cfg.Debug = ParseBool(debugRaw);
        }

        return (cfg, warnings);
    }

    private static void EnsureNonNullNested(SigilClientConfig cfg)
    {
        cfg.Api ??= new ApiConfig();
        cfg.GenerationExport ??= new GenerationExportConfig();
        cfg.GenerationExport.Auth ??= new AuthConfig();
        cfg.GenerationExport.Headers ??= new Dictionary<string, string>(StringComparer.OrdinalIgnoreCase);
        cfg.Tags ??= new Dictionary<string, string>();
    }

    internal static string? EnvTrimmed(Func<string, string?> lookup, string key)
    {
        string? raw;
        try
        {
            raw = lookup(key);
        }
        catch (System.Security.SecurityException)
        {
            return null;
        }
        if (raw == null)
        {
            return null;
        }
        var v = raw.Trim();
        return v.Length == 0 ? null : v;
    }

    internal static bool ParseBool(string? raw)
    {
        if (raw == null)
        {
            return false;
        }
        return raw.Trim().ToLowerInvariant() switch
        {
            "1" or "true" or "yes" or "on" => true,
            _ => false,
        };
    }

    internal static Dictionary<string, string> ParseCsvKv(string? raw)
    {
        // Case-sensitive to match Go/Java: SIGIL_TAGS=env=prod,Env=staging
        // yields two distinct keys. Headers consumers wrap the result in an
        // OrdinalIgnoreCase dictionary at the use site.
        var out_ = new Dictionary<string, string>();
        if (raw == null)
        {
            return out_;
        }
        foreach (var part in raw.Split(','))
        {
            var trimmed = part.Trim();
            if (trimmed.Length == 0)
            {
                continue;
            }
            var idx = trimmed.IndexOf('=');
            if (idx <= 0)
            {
                continue;
            }
            var key = trimmed.Substring(0, idx).Trim();
            var value = trimmed.Substring(idx + 1).Trim();
            if (key.Length > 0)
            {
                out_[key] = value;
            }
        }
        return out_;
    }

    internal static ContentCaptureMode? ParseContentCaptureMode(string? raw)
    {
        if (raw == null)
        {
            return null;
        }
        return raw.Trim().ToLowerInvariant() switch
        {
            "full" => ContentCaptureMode.Full,
            "no_tool_content" => ContentCaptureMode.NoToolContent,
            "metadata_only" => ContentCaptureMode.MetadataOnly,
            _ => null,
        };
    }

    internal static ExportAuthMode? ParseAuthMode(string? raw)
    {
        if (raw == null)
        {
            return null;
        }
        return raw.Trim().ToLowerInvariant() switch
        {
            "none" => ExportAuthMode.None,
            "tenant" => ExportAuthMode.Tenant,
            "bearer" => ExportAuthMode.Bearer,
            "basic" => ExportAuthMode.Basic,
            _ => null,
        };
    }

    internal static GenerationExportProtocol? ParseProtocol(string? raw)
    {
        if (raw == null)
        {
            return null;
        }
        return raw.Trim().ToLowerInvariant() switch
        {
            "grpc" => GenerationExportProtocol.Grpc,
            "http" => GenerationExportProtocol.Http,
            "none" => GenerationExportProtocol.None,
            _ => null,
        };
    }

    internal static void LogWarnings(Action<string>? log, IReadOnlyList<string> warnings)
    {
        if (log == null || warnings == null)
        {
            return;
        }
        foreach (var w in warnings)
        {
            log(w);
        }
    }
}
