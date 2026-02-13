using System.Diagnostics;
using OpenTelemetry;
using OpenTelemetry.Exporter;
using OpenTelemetry.Trace;

namespace Grafana.Sigil;

internal sealed class TraceRuntime : IDisposable
{
    public const string InstrumentationName = "github.com/grafana/sigil/sdks/dotnet";

    public ActivitySource Source { get; }

    private readonly TracerProvider? _provider;

    private TraceRuntime(ActivitySource source, TracerProvider? provider)
    {
        Source = source;
        _provider = provider;
    }

    public static TraceRuntime Create(TraceConfig config, Action<string> log)
    {
        var source = new ActivitySource(InstrumentationName);

        try
        {
            var endpoint = ResolveEndpoint(config);
            var provider = Sdk.CreateTracerProviderBuilder()
                .AddSource(InstrumentationName)
                .AddOtlpExporter(options =>
                {
                    options.Endpoint = endpoint;
                    options.Protocol = config.Protocol == TraceProtocol.Grpc
                        ? OtlpExportProtocol.Grpc
                        : OtlpExportProtocol.HttpProtobuf;
                    options.Headers = string.Join(",", config.Headers.Select(entry => $"{entry.Key}={entry.Value}"));
                    options.TimeoutMilliseconds = 10000;
                })
                .Build();

            return new TraceRuntime(source, provider);
        }
        catch (Exception ex)
        {
            log($"sigil trace exporter init failed: {ex}");
            return new TraceRuntime(source, null);
        }
    }

    public void Flush()
    {
        _provider?.ForceFlush();
    }

    public void Dispose()
    {
        _provider?.Dispose();
        Source.Dispose();
    }

    private static Uri ResolveEndpoint(TraceConfig config)
    {
        var endpoint = config.Endpoint?.Trim() ?? string.Empty;
        if (endpoint.Length == 0)
        {
            throw new ArgumentException("trace endpoint is required");
        }

        if (endpoint.IndexOf("://", StringComparison.Ordinal) < 0)
        {
            endpoint = (config.Insecure ? "http://" : "https://") + endpoint;
        }

        var uri = new Uri(endpoint, UriKind.Absolute);

        if (config.Protocol == TraceProtocol.Http && string.IsNullOrEmpty(uri.AbsolutePath.Trim('/')))
        {
            var builder = new UriBuilder(uri)
            {
                Path = "/v1/traces",
            };
            return builder.Uri;
        }

        return uri;
    }
}
