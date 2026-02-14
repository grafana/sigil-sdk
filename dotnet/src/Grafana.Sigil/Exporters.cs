using Google.Protobuf;
using Grpc.Core;
using Grpc.Net.Client;
using Proto = Sigil.V1;

namespace Grafana.Sigil;

public interface IGenerationExporter
{
    Task<ExportGenerationsResponse> ExportGenerationsAsync(ExportGenerationsRequest request, CancellationToken cancellationToken);

    Task ShutdownAsync(CancellationToken cancellationToken);
}

internal sealed class NoopGenerationExporter : IGenerationExporter
{
    public Task<ExportGenerationsResponse> ExportGenerationsAsync(
        ExportGenerationsRequest request,
        CancellationToken cancellationToken
    )
    {
        cancellationToken.ThrowIfCancellationRequested();
        return Task.FromResult(new ExportGenerationsResponse
        {
            Results = request.Generations.Select(generation => new ExportGenerationResult
            {
                GenerationId = generation.Id,
                Accepted = true,
            }).ToList(),
        });
    }

    public Task ShutdownAsync(CancellationToken cancellationToken)
    {
        return Task.CompletedTask;
    }
}

internal sealed class HttpGenerationExporter : IGenerationExporter
{
    private readonly HttpClient _httpClient;
    private readonly Uri _endpoint;
    private readonly IReadOnlyDictionary<string, string> _headers;

    public HttpGenerationExporter(string endpoint, IReadOnlyDictionary<string, string> headers)
    {
        if (string.IsNullOrWhiteSpace(endpoint))
        {
            throw new ArgumentException("endpoint is required");
        }

        var normalized = endpoint.Trim();
        if (!normalized.StartsWith("http://", StringComparison.OrdinalIgnoreCase)
            && !normalized.StartsWith("https://", StringComparison.OrdinalIgnoreCase))
        {
            normalized = "http://" + normalized;
        }

        var uri = new Uri(normalized, UriKind.Absolute);
        if (string.IsNullOrWhiteSpace(uri.AbsolutePath) || uri.AbsolutePath == "/")
        {
            uri = new UriBuilder(uri)
            {
                Path = "/api/v1/generations:export",
            }.Uri;
        }

        _endpoint = uri;
        var normalizedHeaders = new Dictionary<string, string>(StringComparer.OrdinalIgnoreCase);
        foreach (var pair in headers)
        {
            normalizedHeaders[pair.Key] = pair.Value;
        }
        _headers = normalizedHeaders;
        _httpClient = new HttpClient
        {
            Timeout = TimeSpan.FromSeconds(10),
        };
    }

    public async Task<ExportGenerationsResponse> ExportGenerationsAsync(
        ExportGenerationsRequest request,
        CancellationToken cancellationToken
    )
    {
        var protoRequest = new Proto.ExportGenerationsRequest();
        protoRequest.Generations.AddRange(request.Generations.Select(ProtoMapping.ToProto));

        var payload = JsonFormatter.Default.Format(protoRequest);
        using var httpRequest = new HttpRequestMessage(HttpMethod.Post, _endpoint)
        {
            Content = new StringContent(payload, System.Text.Encoding.UTF8, "application/json"),
        };

        foreach (var header in _headers)
        {
            httpRequest.Headers.TryAddWithoutValidation(header.Key, header.Value);
        }

        using var response = await _httpClient.SendAsync(httpRequest, cancellationToken).ConfigureAwait(false);
        var body = await response.Content.ReadAsStringAsync().ConfigureAwait(false);
        if (!response.IsSuccessStatusCode)
        {
            throw new InvalidOperationException($"http generation export status {(int)response.StatusCode}: {body.Trim()}");
        }

        if (string.IsNullOrWhiteSpace(body))
        {
            return new ExportGenerationsResponse();
        }

        Proto.ExportGenerationsResponse parsed;
        try
        {
            parsed = Google.Protobuf.JsonParser.Default.Parse<Proto.ExportGenerationsResponse>(body);
        }
        catch (Exception ex)
        {
            throw new InvalidOperationException($"unmarshal generation response: {ex.Message}", ex);
        }

        var results = parsed.Results.Select(r => new ExportGenerationResult
        {
            GenerationId = r.GenerationId,
            Accepted = r.Accepted,
            Error = r.Error,
        }).ToList();

        return new ExportGenerationsResponse
        {
            Results = results,
        };
    }

    public Task ShutdownAsync(CancellationToken cancellationToken)
    {
        _httpClient.Dispose();
        return Task.CompletedTask;
    }
}

internal sealed class GrpcGenerationExporter : IGenerationExporter
{
    private readonly GrpcChannel _channel;
    private readonly Proto.GenerationIngestService.GenerationIngestServiceClient _client;
    private readonly IReadOnlyDictionary<string, string> _headers;

    public GrpcGenerationExporter(string endpoint, bool insecure, IReadOnlyDictionary<string, string> headers)
    {
        if (string.IsNullOrWhiteSpace(endpoint))
        {
            throw new ArgumentException("endpoint is required");
        }

        var uri = endpoint.StartsWith("http://", StringComparison.OrdinalIgnoreCase)
            || endpoint.StartsWith("https://", StringComparison.OrdinalIgnoreCase)
            ? new Uri(endpoint, UriKind.Absolute)
            : new Uri((insecure ? "http://" : "https://") + endpoint, UriKind.Absolute);

        var handler = new HttpClientHandler();
        if (!insecure)
        {
            handler.ServerCertificateCustomValidationCallback = null;
        }
        else
        {
            handler.ServerCertificateCustomValidationCallback = static (_, _, _, _) => true;
        }

        _channel = GrpcChannel.ForAddress(uri, new GrpcChannelOptions
        {
            HttpHandler = handler,
            DisposeHttpClient = true,
        });
        _client = new Proto.GenerationIngestService.GenerationIngestServiceClient(_channel);
        var normalizedHeaders = new Dictionary<string, string>(StringComparer.OrdinalIgnoreCase);
        foreach (var pair in headers)
        {
            normalizedHeaders[pair.Key] = pair.Value;
        }
        _headers = normalizedHeaders;
    }

    public async Task<ExportGenerationsResponse> ExportGenerationsAsync(
        ExportGenerationsRequest request,
        CancellationToken cancellationToken
    )
    {
        var protoRequest = new Proto.ExportGenerationsRequest();
        protoRequest.Generations.AddRange(request.Generations.Select(ProtoMapping.ToProto));

        var metadata = new Metadata();
        foreach (var header in _headers)
        {
            metadata.Add(header.Key, header.Value);
        }

        var response = await _client.ExportGenerationsAsync(protoRequest, metadata, cancellationToken: cancellationToken)
            .ResponseAsync
            .ConfigureAwait(false);

        return new ExportGenerationsResponse
        {
            Results = response.Results.Select(r => new ExportGenerationResult
            {
                GenerationId = r.GenerationId,
                Accepted = r.Accepted,
                Error = r.Error,
            }).ToList(),
        };
    }

    public Task ShutdownAsync(CancellationToken cancellationToken)
    {
        _channel.Dispose();
        return Task.CompletedTask;
    }
}
