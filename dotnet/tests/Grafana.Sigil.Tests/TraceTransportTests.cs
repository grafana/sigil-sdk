using System.Collections.Concurrent;
using System.Net;
using System.Text;
using Google.Protobuf;
using Grpc.Core;
using Microsoft.AspNetCore.Builder;
using Microsoft.AspNetCore.Hosting;
using Microsoft.AspNetCore.Server.Kestrel.Core;
using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.Hosting;
using OpenTelemetry.Proto.Collector.Trace.V1;
using OpenTelemetry.Proto.Trace.V1;
using Xunit;

namespace Grafana.Sigil.Tests;

public sealed class TraceTransportTests
{
    [Fact]
    public async Task TraceExportOverHttp_ContainsRequiredAttributesAndTraceLinkage()
    {
        using var collector = new HttpTraceCollector();

        var exporter = new CapturingGenerationExporter();
        var config = TestHelpers.TestConfig(exporter);
        config.Trace = new TraceConfig
        {
            Protocol = TraceProtocol.Http,
            Endpoint = collector.Endpoint,
            Insecure = true,
        };

        await using var client = new SigilClient(config);
        var recorder = client.StartGeneration(TestHelpers.CreateSeedStart("gen-trace-http"));
        recorder.SetResult(TestHelpers.CreateSeedResult("gen-trace-http"));
        recorder.End();

        await client.ShutdownAsync();

        var request = await collector.WaitSingleRequestAsync();
        AssertSpanMatchesGeneration(request, recorder.LastGeneration!);
    }

    [Fact]
    public async Task TraceExportOverGrpc_ContainsRequiredAttributesAndAuthMetadata()
    {
        using var collector = new GrpcTraceCollector();

        var exporter = new CapturingGenerationExporter();
        var config = TestHelpers.TestConfig(exporter);
        config.Trace = new TraceConfig
        {
            Protocol = TraceProtocol.Grpc,
            Endpoint = $"127.0.0.1:{collector.Port}",
            Insecure = true,
            Auth = new AuthConfig
            {
                Mode = ExportAuthMode.Tenant,
                TenantId = "tenant-trace",
            },
        };

        await using var client = new SigilClient(config);
        var recorder = client.StartStreamingGeneration(TestHelpers.CreateSeedStart("gen-trace-grpc"));
        var result = TestHelpers.CreateSeedResult("gen-trace-grpc");
        result.Mode = GenerationMode.Stream;
        result.OperationName = "streamText";
        recorder.SetResult(result);
        recorder.End();

        await client.ShutdownAsync();

        var (request, metadata) = await collector.WaitSingleRequestAsync();
        AssertSpanMatchesGeneration(request, recorder.LastGeneration!);
        Assert.Equal("tenant-trace", metadata["x-scope-orgid"]);
    }

    [Fact]
    public async Task TraceHttpTransport_ExplicitHeadersOverrideAuthInjection()
    {
        using var collector = new HttpTraceCollector();

        var exporter = new CapturingGenerationExporter();
        var config = TestHelpers.TestConfig(exporter);
        config.Trace = new TraceConfig
        {
            Protocol = TraceProtocol.Http,
            Endpoint = collector.Endpoint,
            Insecure = true,
            Headers = new Dictionary<string, string>(StringComparer.OrdinalIgnoreCase)
            {
                ["authorization"] = "Bearer override-trace-token",
            },
            Auth = new AuthConfig
            {
                Mode = ExportAuthMode.Bearer,
                BearerToken = "trace-token",
            },
        };

        await using var client = new SigilClient(config);
        var recorder = client.StartGeneration(TestHelpers.CreateSeedStart("gen-trace-http-auth"));
        recorder.SetResult(TestHelpers.CreateSeedResult("gen-trace-http-auth"));
        recorder.End();

        await client.ShutdownAsync();

        var headers = await collector.WaitSingleHeaderAsync();
        Assert.Equal("Bearer override-trace-token", headers["authorization"]);
    }

    private static void AssertSpanMatchesGeneration(ExportTraceServiceRequest request, Generation generation)
    {
        var spans = request.ResourceSpans
            .SelectMany(resource => resource.ScopeSpans)
            .SelectMany(scope => scope.Spans)
            .ToList();
        Assert.NotEmpty(spans);

        var span = spans
            .Select(item => new
            {
                Span = item,
                Attributes = TraceAssertions.AttributeStringMap(item.Attributes),
            })
            .FirstOrDefault(item =>
                item.Attributes.TryGetValue("sigil.generation.id", out var generationId)
                && string.Equals(generationId, generation.Id, StringComparison.Ordinal)
            );

        Assert.NotNull(span);
        var attrs = span!.Attributes;

        Assert.Equal(generation.Id, attrs["sigil.generation.id"]);
        Assert.Equal(generation.ConversationId, attrs["gen_ai.conversation.id"]);
        Assert.Equal(generation.AgentName, attrs["gen_ai.agent.name"]);
        Assert.Equal(generation.AgentVersion, attrs["gen_ai.agent.version"]);
        Assert.Equal(generation.Model.Provider, attrs["gen_ai.provider.name"]);
        Assert.Equal(generation.Model.Name, attrs["gen_ai.request.model"]);
        Assert.Equal(generation.OperationName, attrs["gen_ai.operation.name"]);

        Assert.DoesNotContain("sigil.generation.mode", attrs.Keys);

        var exportedTraceId = Convert.ToHexString(span.Span.TraceId.ToByteArray()).ToLowerInvariant();
        var exportedSpanId = Convert.ToHexString(span.Span.SpanId.ToByteArray()).ToLowerInvariant();
        Assert.Equal(generation.TraceId, exportedTraceId);
        Assert.Equal(generation.SpanId, exportedSpanId);
    }

    private sealed class HttpTraceCollector : IDisposable
    {
        private readonly HttpListener _listener;
        private readonly CancellationTokenSource _cts = new();
        private readonly Task _loop;

        private readonly ConcurrentQueue<ExportTraceServiceRequest> _requests = new();
        private readonly ConcurrentQueue<Dictionary<string, string>> _headers = new();

        public string Endpoint { get; }

        public HttpTraceCollector()
        {
            var port = ReservePort();
            Endpoint = $"http://127.0.0.1:{port}/v1/traces";

            _listener = new HttpListener();
            _listener.Prefixes.Add($"http://127.0.0.1:{port}/");
            _listener.Start();

            _loop = Task.Run(async () =>
            {
                while (!_cts.IsCancellationRequested)
                {
                    HttpListenerContext context;
                    try
                    {
                        context = await _listener.GetContextAsync().ConfigureAwait(false);
                    }
                    catch
                    {
                        if (_cts.IsCancellationRequested)
                        {
                            break;
                        }

                        throw;
                    }

                    try
                    {
                        var headers = new Dictionary<string, string>(StringComparer.OrdinalIgnoreCase);
                        foreach (var key in context.Request.Headers.AllKeys)
                        {
                            if (key != null)
                            {
                                headers[key] = context.Request.Headers[key] ?? string.Empty;
                            }
                        }

                        _headers.Enqueue(headers);

                        using var stream = new MemoryStream();
                        await context.Request.InputStream.CopyToAsync(stream).ConfigureAwait(false);
                        var request = ExportTraceServiceRequest.Parser.ParseFrom(stream.ToArray());
                        _requests.Enqueue(request);

                        var response = new ExportTraceServiceResponse();
                        var payload = response.ToByteArray();

                        context.Response.StatusCode = 200;
                        context.Response.ContentType = "application/x-protobuf";
                        await context.Response.OutputStream.WriteAsync(payload).ConfigureAwait(false);
                    }
                    finally
                    {
                        context.Response.Close();
                    }
                }
            });
        }

        public async Task<ExportTraceServiceRequest> WaitSingleRequestAsync()
        {
            await TestHelpers.WaitForAsync(
                () => _requests.Count >= 1,
                TimeSpan.FromSeconds(5),
                "did not receive trace request"
            );

            Assert.True(_requests.TryDequeue(out var request));
            return request;
        }

        public async Task<Dictionary<string, string>> WaitSingleHeaderAsync()
        {
            await TestHelpers.WaitForAsync(
                () => _headers.Count >= 1,
                TimeSpan.FromSeconds(5),
                "did not receive trace request headers"
            );

            Assert.True(_headers.TryDequeue(out var headers));
            return headers;
        }

        public void Dispose()
        {
            _cts.Cancel();
            _listener.Close();
            try
            {
                _loop.GetAwaiter().GetResult();
            }
            catch
            {
                // Ignore shutdown exceptions.
            }
        }

        private static int ReservePort()
        {
            var listener = new System.Net.Sockets.TcpListener(System.Net.IPAddress.Loopback, 0);
            listener.Start();
            var port = ((System.Net.IPEndPoint)listener.LocalEndpoint).Port;
            listener.Stop();
            return port;
        }
    }

    private sealed class GrpcTraceCollector : IDisposable
    {
        private readonly IHost _host;
        private readonly RequestStore _store = new();

        public int Port { get; }

        public GrpcTraceCollector()
        {
            Port = ReservePort();
            _host = Host.CreateDefaultBuilder()
                .ConfigureWebHostDefaults(webBuilder =>
                {
                    webBuilder.UseKestrel(options =>
                    {
                        options.ListenLocalhost(Port, listen => listen.Protocols = HttpProtocols.Http2);
                    });
                    webBuilder.ConfigureServices(services =>
                    {
                        services.AddSingleton(_store);
                        services.AddGrpc();
                    });
                    webBuilder.Configure(app =>
                    {
                        app.UseRouting();
                        app.UseEndpoints(endpoints =>
                        {
                            endpoints.MapGrpcService<TraceCollectorService>();
                        });
                    });
                })
                .Build();

            _host.Start();
        }

        public async Task<(ExportTraceServiceRequest Request, Dictionary<string, string> Metadata)> WaitSingleRequestAsync()
        {
            await TestHelpers.WaitForAsync(
                () => _store.Count >= 1,
                TimeSpan.FromSeconds(5),
                "did not receive grpc trace request"
            );

            var entry = _store.Single();
            var metadata = entry.Metadata.ToDictionary(item => item.Key, item => item.Value, StringComparer.OrdinalIgnoreCase);
            return (entry.Request, metadata);
        }

        public void Dispose()
        {
            _host.StopAsync().GetAwaiter().GetResult();
            _host.Dispose();
        }

        private sealed class RequestStore
        {
            private readonly object _gate = new();
            private readonly List<(ExportTraceServiceRequest Request, Metadata Metadata)> _requests = new();

            public int Count
            {
                get
                {
                    lock (_gate)
                    {
                        return _requests.Count;
                    }
                }
            }

            public void Add(ExportTraceServiceRequest request, Metadata metadata)
            {
                lock (_gate)
                {
                    _requests.Add((request, metadata));
                }
            }

            public (ExportTraceServiceRequest Request, Metadata Metadata) Single()
            {
                lock (_gate)
                {
                    Assert.Single(_requests);
                    return _requests[0];
                }
            }
        }

        private sealed class TraceCollectorService : TraceService.TraceServiceBase
        {
            private readonly RequestStore _store;

            public TraceCollectorService(RequestStore store)
            {
                _store = store;
            }

            public override Task<ExportTraceServiceResponse> Export(ExportTraceServiceRequest request, ServerCallContext context)
            {
                _store.Add(request, context.RequestHeaders);
                return Task.FromResult(new ExportTraceServiceResponse());
            }
        }

        private static int ReservePort()
        {
            var listener = new System.Net.Sockets.TcpListener(System.Net.IPAddress.Loopback, 0);
            listener.Start();
            var port = ((System.Net.IPEndPoint)listener.LocalEndpoint).Port;
            listener.Stop();
            return port;
        }
    }
}
