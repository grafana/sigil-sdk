# Grafana Sigil Go SDK (Core)

If you already use OpenTelemetry, Sigil is a thin extension plus sugar for AI observability.

The Go SDK is the current production-ready baseline for normalized generation recording with OTEL traces and generation-first export.

Cross-language parity tracks are available for:

- Python: `sdks/python`
- TypeScript/JavaScript: `sdks/js`
- .NET/C#: `sdks/dotnet`

## Core model

- `Generation` is the canonical entity.
- `Generation.Mode` is explicit: `SYNC` or `STREAM`.
- `OperationName` defaults are mode-aware:
  - `SYNC` -> `generateText`
  - `STREAM` -> `streamText`
- `ModelRef` bundles `provider + model`.
- `AgentName` and `AgentVersion` are optional generation/tool identity fields.
- `SystemPrompt` is separate from messages.
- Request controls are optional first-class fields:
  - `MaxTokens`
  - `Temperature`
  - `TopP`
  - `ToolChoice`
  - `ThinkingEnabled`
- `Message` contains typed parts: `text`, `thinking`, `tool_call`, `tool_result`.
- `TokenUsage` includes token/cache/reasoning fields.
- Raw provider `Artifacts` are optional debug payloads.

## Recording API (explicit, OTel-like)

- `StartGeneration(ctx, start)` -> `(ctx, *GenerationRecorder)`
- `StartStreamingGeneration(ctx, start)` -> `(ctx, *GenerationRecorder)`
- `StartToolExecution(ctx, start)` -> `(ctx, *ToolExecutionRecorder)`
- `rec.SetResult(...)` / `rec.SetCallError(...)`
- `rec.End()` is defer-safe and idempotent.
- `rec.Err()` reports local validation/enqueue failures only.
- Background export failures are retried and logged.
- Generation spans emit request controls using GenAI keys where standardized:
  - `gen_ai.request.max_tokens`
  - `gen_ai.request.temperature`
  - `gen_ai.request.top_p`
  - `sigil.gen_ai.request.tool_choice`
  - `sigil.gen_ai.request.thinking.enabled`
  - `sigil.gen_ai.request.thinking.budget_tokens` (provider-specific)
  - `gen_ai.response.finish_reasons` is emitted as a string array.
- Context helpers are available for defaults:
  - `WithConversationID(ctx, id)`
  - `WithAgentName(ctx, name)`
  - `WithAgentVersion(ctx, version)`

## Configuration

```go
cfg := sigil.DefaultConfig()

// Optional: inject tracer/meter explicitly.
// If unset, the SDK uses otel.Tracer(...) and otel.Meter(...).
// cfg.Tracer = myTracer
// cfg.Meter = myMeter

// Generation export (custom ingest)
cfg.GenerationExport.Protocol = sigil.GenerationExportProtocolGRPC // default; or sigil.GenerationExportProtocolHTTP / sigil.GenerationExportProtocolNone
cfg.GenerationExport.Endpoint = "localhost:4317"                  // HTTP parity: "http://localhost:8080/api/v1/generations:export"
cfg.GenerationExport.Auth = sigil.AuthConfig{
	Mode:     sigil.ExportAuthModeTenant,
	TenantID: "dev-tenant",
}
cfg.GenerationExport.BatchSize = 100
cfg.GenerationExport.FlushInterval = time.Second
cfg.GenerationExport.QueueSize = 2000
cfg.GenerationExport.MaxRetries = 5
cfg.GenerationExport.InitialBackoff = 100 * time.Millisecond
cfg.GenerationExport.MaxBackoff = 5 * time.Second

// Sigil API base used by helpers like SubmitConversationRating.
cfg.API.Endpoint = "http://localhost:8080"

client := sigil.NewClient(cfg)
defer func() {
	_ = client.Shutdown(context.Background())
}()
```

Configure OTEL exporters (traces/metrics) in your application OTEL SDK setup.

### Instrumentation-only mode (no generation send)

Use `GenerationExportProtocolNone` to keep generation and tool instrumentation active while disabling generation transport:

```go
cfg := sigil.DefaultConfig()
cfg.GenerationExport.Protocol = sigil.GenerationExportProtocolNone

client := sigil.NewClient(cfg)
defer func() { _ = client.Shutdown(context.Background()) }()
```

## Generation export auth modes

Auth is configured for generation export.

- `none`
- `tenant` (requires `TenantID`, injects `X-Scope-OrgID`)
- `bearer` (requires `BearerToken`, injects `Authorization: Bearer <token>`)

Invalid combinations fail fast during `NewClient(...)`.

```go
cfg.GenerationExport.Auth = sigil.AuthConfig{
	Mode:        sigil.ExportAuthModeBearer,
	BearerToken: "token-from-secret-manager",
}
```

Explicit transport headers remain the highest-precedence escape hatch. If `Headers` already contains `Authorization` or `X-Scope-OrgID`, the SDK does not overwrite them.

## Env-secret wiring example

The SDK does not auto-load env vars. Read env values in your app and assign config explicitly.

```go
genToken := strings.TrimSpace(os.Getenv("SIGIL_GEN_BEARER_TOKEN"))
if genToken != "" {
	cfg.GenerationExport.Auth = sigil.AuthConfig{
		Mode:        sigil.ExportAuthModeBearer,
		BearerToken: genToken,
	}
}
```

Common topology:

- Generations direct to Sigil: generation `tenant` mode.
- Traces/metrics via OTEL Collector/Alloy: configure exporters in your app OTEL SDK setup.
- Enterprise proxy: generation `bearer` mode to proxy; proxy authenticates and forwards tenant header upstream.

## Conversation Ratings

Use the SDK helper to submit user-facing ratings:

```go
rating, err := client.SubmitConversationRating(ctx, "conv-123", sigil.ConversationRatingInput{
	RatingID: "rat-123",
	Rating:   sigil.ConversationRatingValueBad,
	Comment:  "Answer ignored user context",
	Metadata: map[string]any{
		"channel": "assistant-ui",
	},
	Source: "sdk-go",
})
if err != nil {
	panic(err)
}

fmt.Printf("rating=%s has_bad=%v\n", rating.Rating.Rating, rating.Summary.HasBadRating)
```

`SubmitConversationRating` sends requests to `cfg.API.Endpoint` (default `http://localhost:8080`) and uses the same generation-export auth headers (`tenant` or `bearer`) that your client config already resolves.

## Lifecycle requirement

- Always call `client.Shutdown(ctx)` before process exit.
- `Shutdown` flushes pending generation batches and closes generation exporters.
- Optional `client.Flush(ctx)` is available for explicit flush points.

## SDK metrics

The SDK emits four OTel histograms automatically through your configured OTel meter provider:

- `gen_ai.client.operation.duration`
- `gen_ai.client.token.usage`
- `gen_ai.client.time_to_first_token`
- `gen_ai.client.tool_calls_per_operation`

## Explicit flow example

```go
ctx, rec := client.StartGeneration(ctx, sigil.GenerationStart{
	ConversationID: "conv-9b2f",
	AgentName:      "assistant-core",
	AgentVersion:   "1.0.0",
	Model:          sigil.ModelRef{Provider: "anthropic", Name: "claude-sonnet-4-5"},
})
defer rec.End()

resp, err := provider.Call(ctx, req)
if err != nil {
	rec.SetCallError(err)
	return err
}

rec.SetResult(sigil.Generation{
	Input:  []sigil.Message{sigil.UserTextMessage("Hello")},
	Output: []sigil.Message{sigil.AssistantTextMessage(resp.Text)},
	Usage:  sigil.TokenUsage{InputTokens: 120, OutputTokens: 42},
}, nil)
```

## Streaming example

```go
ctx, rec := client.StartStreamingGeneration(ctx, sigil.GenerationStart{
	ConversationID: "conv-stream",
	AgentName:      "assistant-core",
	AgentVersion:   "1.0.0",
	Model:          sigil.ModelRef{Provider: "openai", Name: "gpt-5"},
})
defer rec.End()

// accumulate stream output...
rec.SetResult(sigil.Generation{
	Input:  []sigil.Message{sigil.UserTextMessage("Say hello")},
	Output: []sigil.Message{sigil.AssistantTextMessage(stitchedOutput)},
}, nil)
```

## Provider wrappers

Provider modules are documented wrapper-first for ergonomics and include explicit-flow alternatives.

Current Go provider helpers:

- `sdks/go-providers/openai` (OpenAI Chat Completions + Responses wrappers and mappers)
- `sdks/go-providers/anthropic`
- `sdks/go-providers/gemini`

## Raw artifact policy

- Default: raw artifacts OFF in provider wrappers.
- Opt-in only for debug workflows (`WithRawArtifacts()` in provider helper packages).
- Normalized generation fields remain always on.
