using System.Diagnostics;
using System.Diagnostics.Metrics;
using OpenTelemetry;
using OpenTelemetry.Exporter;
using OpenTelemetry.Metrics;
using OpenTelemetry.Trace;

namespace Grafana.Sigil;

internal sealed class TraceRuntime : IDisposable
{
    public const string InstrumentationName = "github.com/grafana/sigil/sdks/dotnet";

    public ActivitySource Source { get; }

    public Meter Meter { get; }

    private readonly TracerProvider? _traceProvider;
    private readonly MeterProvider? _meterProvider;

    private TraceRuntime(ActivitySource source, Meter meter, TracerProvider? traceProvider, MeterProvider? meterProvider)
    {
        Source = source;
        Meter = meter;
        _traceProvider = traceProvider;
        _meterProvider = meterProvider;
    }

    public static TraceRuntime Create(TraceConfig config, Action<string> log)
    {
        var source = new ActivitySource(InstrumentationName);
        var meter = new Meter(InstrumentationName);

        try
        {
            var traceEndpoint = ResolveEndpoint(config);
            var traceProvider = Sdk.CreateTracerProviderBuilder()
                .AddSource(InstrumentationName)
                .AddOtlpExporter(options =>
                {
                    options.Endpoint = traceEndpoint;
                    options.Protocol = config.Protocol == TraceProtocol.Grpc
                        ? OtlpExportProtocol.Grpc
                        : OtlpExportProtocol.HttpProtobuf;
                    options.Headers = string.Join(",", config.Headers.Select(entry => $"{entry.Key}={entry.Value}"));
                    options.TimeoutMilliseconds = 10000;
                })
                .Build();

            MeterProvider? meterProvider = null;
            if (config.EnableMetrics)
            {
                var metricEndpoint = ResolveMetricEndpoint(config);
                meterProvider = Sdk.CreateMeterProviderBuilder()
                    .AddMeter(InstrumentationName)
                    .AddView(
                        "gen_ai.client.operation.duration",
                        new ExplicitBucketHistogramConfiguration
                        {
                            Boundaries = new double[] { 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120 },
                        })
                    .AddView(
                        "gen_ai.client.token.usage",
                        new ExplicitBucketHistogramConfiguration
                        {
                            Boundaries = new double[] { 1, 10, 50, 100, 250, 500, 1_000, 2_500, 5_000, 10_000, 50_000, 100_000 },
                        })
                    .AddView(
                        "gen_ai.client.time_to_first_token",
                        new ExplicitBucketHistogramConfiguration
                        {
                            Boundaries = new double[] { 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10 },
                        })
                    .AddView(
                        "gen_ai.client.tool_calls_per_operation",
                        new ExplicitBucketHistogramConfiguration
                        {
                            Boundaries = new double[] { 0, 1, 2, 3, 5, 10, 20, 50 },
                        })
                    .AddOtlpExporter(options =>
                    {
                        options.Endpoint = metricEndpoint;
                        options.Protocol = config.Protocol == TraceProtocol.Grpc
                            ? OtlpExportProtocol.Grpc
                            : OtlpExportProtocol.HttpProtobuf;
                        options.Headers = string.Join(",", config.Headers.Select(entry => $"{entry.Key}={entry.Value}"));
                        options.TimeoutMilliseconds = 10000;
                    })
                    .Build();
            }

            return new TraceRuntime(source, meter, traceProvider, meterProvider);
        }
        catch (Exception ex)
        {
            log($"sigil trace exporter init failed: {ex}");
            return new TraceRuntime(source, meter, null, null);
        }
    }

    public void Flush()
    {
        _traceProvider?.ForceFlush();
        _meterProvider?.ForceFlush();
    }

    public void Dispose()
    {
        _meterProvider?.Dispose();
        _traceProvider?.Dispose();
        Source.Dispose();
        Meter.Dispose();
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

    private static Uri ResolveMetricEndpoint(TraceConfig config)
    {
        var traceEndpoint = ResolveEndpoint(config);
        if (config.Protocol == TraceProtocol.Grpc)
        {
            return traceEndpoint;
        }

        var path = traceEndpoint.AbsolutePath.Trim();
        if (path.Length == 0 || path == "/" || string.Equals(path, "/v1/traces", StringComparison.OrdinalIgnoreCase))
        {
            var builder = new UriBuilder(traceEndpoint)
            {
                Path = "/v1/metrics",
            };
            return builder.Uri;
        }

        return traceEndpoint;
    }
}
