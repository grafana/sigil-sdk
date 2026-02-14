using System.Text;
using Google.Protobuf;
using Xunit;
using SigilProto = Sigil.V1;

namespace Grafana.Sigil.Tests;

public sealed class GenerationTransportTests
{
    [Fact]
    public async Task ExportsGenerationOverHttp_AllPropertiesRoundTrip()
    {
        using var server = new HttpCaptureServer((_, body) =>
        {
            var request = Google.Protobuf.JsonParser.Default.Parse<SigilProto.ExportGenerationsRequest>(
                Encoding.UTF8.GetString(body)
            );

            var response = new SigilProto.ExportGenerationsResponse();
            foreach (var generation in request.Generations)
            {
                response.Results.Add(new SigilProto.ExportGenerationResult
                {
                    GenerationId = generation.Id,
                    Accepted = true,
                });
            }

            return Encoding.UTF8.GetBytes(JsonFormatter.Default.Format(response));
        });

        var config = new SigilClientConfig
        {
            Trace = new TraceConfig
            {
                Endpoint = string.Empty,
            },
            GenerationExport = new GenerationExportConfig
            {
                Protocol = GenerationExportProtocol.Http,
                Endpoint = $"http://127.0.0.1:{server.Port}",
                BatchSize = 1,
                QueueSize = 10,
                FlushInterval = TimeSpan.FromSeconds(1),
                MaxRetries = 1,
                InitialBackoff = TimeSpan.FromMilliseconds(1),
                MaxBackoff = TimeSpan.FromMilliseconds(2),
            },
        };

        await using var client = new SigilClient(config);
        var recorder = client.StartGeneration(TestHelpers.CreateSeedStart("gen-http"));
        recorder.SetResult(TestHelpers.CreateSeedResult("gen-http"));
        recorder.End();

        await client.ShutdownAsync();

        Assert.True(server.Requests.TryDequeue(out var captured));
        var request = Google.Protobuf.JsonParser.Default.Parse<SigilProto.ExportGenerationsRequest>(
            Encoding.UTF8.GetString(captured.Body)
        );

        Assert.Single(request.Generations);
        GenerationAssertions.AssertEquivalent(recorder.LastGeneration!, request.Generations[0]);
    }

    [Fact]
    public async Task ExportsGenerationOverGrpc_AllPropertiesRoundTripAndTypedParts()
    {
        using var server = new GrpcIngestServer();

        var config = new SigilClientConfig
        {
            Trace = new TraceConfig
            {
                Endpoint = string.Empty,
            },
            GenerationExport = new GenerationExportConfig
            {
                Protocol = GenerationExportProtocol.Grpc,
                Endpoint = $"127.0.0.1:{server.Port}",
                Insecure = true,
                BatchSize = 1,
                QueueSize = 10,
                FlushInterval = TimeSpan.FromSeconds(1),
                MaxRetries = 1,
                InitialBackoff = TimeSpan.FromMilliseconds(1),
                MaxBackoff = TimeSpan.FromMilliseconds(2),
            },
        };

        await using var client = new SigilClient(config);
        var recorder = client.StartGeneration(TestHelpers.CreateSeedStart("gen-grpc"));
        recorder.SetResult(TestHelpers.CreateSeedResult("gen-grpc"));
        recorder.End();

        await client.ShutdownAsync();

        Assert.Single(server.Requests);
        var request = server.Requests[0].Request;
        Assert.Single(request.Generations);

        var exported = request.Generations[0];
        GenerationAssertions.AssertEquivalent(recorder.LastGeneration!, exported);

        Assert.Contains(
            exported.Output.SelectMany(message => message.Parts),
            part => part.PayloadCase == SigilProto.Part.PayloadOneofCase.ToolCall && part.ToolCall.Name == "weather"
        );
        Assert.Contains(
            exported.Output.SelectMany(message => message.Parts),
            part => part.PayloadCase == SigilProto.Part.PayloadOneofCase.ToolResult && part.ToolResult.Name == "weather"
        );
    }

    [Fact]
    public async Task GenerationHttpTransport_AppliesTenantAuthHeader()
    {
        using var server = new HttpCaptureServer((_, body) =>
        {
            var request = Google.Protobuf.JsonParser.Default.Parse<SigilProto.ExportGenerationsRequest>(
                Encoding.UTF8.GetString(body)
            );

            var response = new SigilProto.ExportGenerationsResponse();
            foreach (var generation in request.Generations)
            {
                response.Results.Add(new SigilProto.ExportGenerationResult
                {
                    GenerationId = generation.Id,
                    Accepted = true,
                });
            }

            return Encoding.UTF8.GetBytes(JsonFormatter.Default.Format(response));
        });

        var config = new SigilClientConfig
        {
            Trace = new TraceConfig
            {
                Endpoint = string.Empty,
            },
            GenerationExport = new GenerationExportConfig
            {
                Protocol = GenerationExportProtocol.Http,
                Endpoint = $"http://127.0.0.1:{server.Port}/api/v1/generations:export",
                Auth = new AuthConfig
                {
                    Mode = ExportAuthMode.Tenant,
                    TenantId = "tenant-a",
                },
                BatchSize = 1,
                QueueSize = 10,
                FlushInterval = TimeSpan.FromSeconds(1),
            },
        };

        await using var client = new SigilClient(config);
        var recorder = client.StartGeneration(TestHelpers.CreateSeedStart("gen-http-auth"));
        recorder.SetResult(TestHelpers.CreateSeedResult("gen-http-auth"));
        recorder.End();
        await client.ShutdownAsync();

        Assert.True(server.Requests.TryDequeue(out var captured));
        Assert.Equal("tenant-a", captured.Headers["X-Scope-OrgID"]);
    }

    [Fact]
    public async Task GenerationHttpTransport_ExplicitHeadersOverrideAuthInjection()
    {
        using var server = new HttpCaptureServer((_, body) =>
        {
            var request = Google.Protobuf.JsonParser.Default.Parse<SigilProto.ExportGenerationsRequest>(
                Encoding.UTF8.GetString(body)
            );

            var response = new SigilProto.ExportGenerationsResponse();
            foreach (var generation in request.Generations)
            {
                response.Results.Add(new SigilProto.ExportGenerationResult
                {
                    GenerationId = generation.Id,
                    Accepted = true,
                });
            }

            return Encoding.UTF8.GetBytes(JsonFormatter.Default.Format(response));
        });

        var config = new SigilClientConfig
        {
            Trace = new TraceConfig
            {
                Endpoint = string.Empty,
            },
            GenerationExport = new GenerationExportConfig
            {
                Protocol = GenerationExportProtocol.Http,
                Endpoint = $"http://127.0.0.1:{server.Port}/api/v1/generations:export",
                Headers = new Dictionary<string, string>(StringComparer.OrdinalIgnoreCase)
                {
                    ["x-scope-orgid"] = "tenant-override",
                    ["authorization"] = "Bearer override-token",
                },
                Auth = new AuthConfig
                {
                    Mode = ExportAuthMode.Bearer,
                    BearerToken = "token-from-auth",
                },
                BatchSize = 1,
                QueueSize = 10,
                FlushInterval = TimeSpan.FromSeconds(1),
            },
        };

        await using var client = new SigilClient(config);
        var recorder = client.StartGeneration(TestHelpers.CreateSeedStart("gen-http-override"));
        recorder.SetResult(TestHelpers.CreateSeedResult("gen-http-override"));
        recorder.End();
        await client.ShutdownAsync();

        Assert.True(server.Requests.TryDequeue(out var captured));
        Assert.Equal("tenant-override", captured.Headers["x-scope-orgid"]);
        Assert.Equal("Bearer override-token", captured.Headers["authorization"]);
    }

    [Fact]
    public async Task GenerationGrpcTransport_AppliesAndOverridesAuthMetadata()
    {
        using var server = new GrpcIngestServer();

        var config = new SigilClientConfig
        {
            Trace = new TraceConfig
            {
                Endpoint = string.Empty,
            },
            GenerationExport = new GenerationExportConfig
            {
                Protocol = GenerationExportProtocol.Grpc,
                Endpoint = $"127.0.0.1:{server.Port}",
                Insecure = true,
                Headers = new Dictionary<string, string>(StringComparer.OrdinalIgnoreCase)
                {
                    ["authorization"] = "Bearer override-token",
                },
                Auth = new AuthConfig
                {
                    Mode = ExportAuthMode.Bearer,
                    BearerToken = "token-from-auth",
                },
                BatchSize = 1,
                QueueSize = 10,
                FlushInterval = TimeSpan.FromSeconds(1),
            },
        };

        await using var client = new SigilClient(config);
        var recorder = client.StartGeneration(TestHelpers.CreateSeedStart("gen-grpc-auth"));
        recorder.SetResult(TestHelpers.CreateSeedResult("gen-grpc-auth"));
        recorder.End();
        await client.ShutdownAsync();

        Assert.Single(server.Requests);
        var metadata = server.Requests[0].Headers.ToDictionary(entry => entry.Key, entry => entry.Value, StringComparer.OrdinalIgnoreCase);

        Assert.Equal("Bearer override-token", metadata["authorization"]);
    }

    [Fact]
    public async Task GenerationTransport_NoneProtocol_RecordsWithoutSending()
    {
        var config = new SigilClientConfig
        {
            Trace = new TraceConfig
            {
                Endpoint = string.Empty,
            },
            GenerationExport = new GenerationExportConfig
            {
                Protocol = GenerationExportProtocol.None,
                Endpoint = "http://127.0.0.1:1",
                BatchSize = 1,
                QueueSize = 10,
                FlushInterval = TimeSpan.FromSeconds(1),
                MaxRetries = 1,
                InitialBackoff = TimeSpan.FromMilliseconds(1),
                MaxBackoff = TimeSpan.FromMilliseconds(2),
            },
        };

        await using var client = new SigilClient(config);
        var recorder = client.StartGeneration(TestHelpers.CreateSeedStart("gen-none"));
        recorder.SetResult(TestHelpers.CreateSeedResult("gen-none"));
        recorder.End();

        await client.FlushAsync();
        await client.ShutdownAsync();

        Assert.Null(recorder.Error);
        Assert.NotNull(recorder.LastGeneration);
    }
}
