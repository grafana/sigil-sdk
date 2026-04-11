# Sigil SDK: Genkit (Go)

[Genkit](https://github.com/genkit-ai/genkit) plugin for Grafana Sigil that captures LLM generation telemetry via Genkit's ModelMiddleware.

Each generation produces:
- **OTel traces** with GenAI semantic attributes
- **OTel metrics** (`gen_ai.client.operation.duration`, `gen_ai.client.token.usage`, `gen_ai.client.time_to_first_token`)
- **Generation payloads** exported to Sigil for analytics and conversation replay

## Install

```bash
go get github.com/grafana/sigil-sdk/go-frameworks/genkit
```

## Quickstart

```go
// 1. Configure OTel (traces go through the standard Go OTel pipeline)
tp := sdktrace.NewTracerProvider(
	sdktrace.WithBatcher(otlptracegrpc.NewUnstarted()),
)
otel.SetTracerProvider(tp)

// 2. Configure the Sigil client (generation payloads)
cfg := sigil.DefaultConfig()
cfg.GenerationExport.Protocol = sigil.GenerationExportProtocolGRPC
cfg.GenerationExport.Endpoint = "sigil.example.com:4317"

client := sigil.NewClient(cfg)
defer client.Shutdown(context.Background())

// 3. Create the Genkit plugin and apply middleware
plugin := genkit.New(client, genkit.Options{
	AgentName:    "planner",
	AgentVersion: "1.0.0",
})
mw := plugin.Middleware(model)
```

See the [core Go SDK](https://github.com/grafana/sigil-sdk/tree/main/go) for full `Config` reference.

## Features

- SYNC and STREAM generation support with TTFT capture
- Tool definition and tool call/result recording
- Content capture controlled by the SDK's `ContentCaptureMode`
- Custom tags and metadata per plugin instance

## Content capture

Content capture is controlled by the SDK's `ContentCaptureMode`. The plugin
passes the mode through to `GenerationStart.ContentCapture`; the SDK handles
stripping at export time.

```go
plugin := genkit.New(client, genkit.Options{
	AgentName:      "private-agent",
	ContentCapture: sigil.ContentCaptureModeMetadataOnly,
})
```

When unset (default), the plugin inherits the client-level mode.

## Extra tags and metadata

```go
plugin := genkit.New(client, genkit.Options{
	AgentName: "my-agent",
	ExtraTags: map[string]string{
		"deployment.environment": "production",
	},
	ExtraMetadata: map[string]any{
		"team": "infra",
	},
})
```

Framework tags (`sigil.framework.name`, `sigil.framework.source`, `sigil.framework.language`) are added automatically.
