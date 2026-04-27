# Sigil SDK — Claude Code Context

You are running in Claude Code with repository files and shell access.
Prefer direct file edits over speculative refactors.
Before proposing broad changes, confirm impact scope with quick evidence.

## Repository structure

This is a multi-language SDK monorepo for [Grafana Sigil](https://grafana.com/docs/grafana-cloud/monitor-applications/ai-observability/) (AI observability).

| Directory | Language | Key entry points |
|---|---|---|
| `go/` | Go | `StartGeneration`, `StartStreamingGeneration`, `StartToolExecution`, `StartEmbedding` |
| `js/` | TypeScript | `startGeneration`, `startStreamingGeneration`, `startToolExecution`, `startEmbedding` |
| `python/` | Python | `start_generation`, `start_streaming_generation`, `start_tool_execution`, `start_embedding` |
| `java/` | Java | `startGeneration`, `startStreamingGeneration`, `withGeneration`, `withToolExecution` |
| `dotnet/` | C# | `StartGeneration`, `StartStreamingGeneration`, `StartToolExecution`, `StartEmbedding` |
| `go-providers/` | Go | OpenAI, Anthropic, Gemini provider wrappers |
| `python-providers/` | Python | Anthropic, Gemini provider wrappers |
| `python-frameworks/` | Python | LangChain, LangGraph, LlamaIndex, OpenAI Agents, Google ADK, LiteLLM |
| `go-frameworks/` | Go | Google ADK adapter |
| `java/providers/` | Java | Anthropic, Gemini provider wrappers |
| `java/frameworks/` | Java | Google ADK adapter |
| `dotnet/src/Grafana.Sigil.*` | C# | OpenAI, Anthropic, Gemini provider wrappers |
| `plugins/` | Go, TS | Claude Code stop hook, OpenCode plugin |
| `proto/` | Protobuf | Generation ingest service definition |

## Sigil architecture and ingest model

- Sigil uses generation-first ingest:
  - gRPC: `sigil.v1.GenerationIngestService.ExportGenerations`
  - HTTP parity: `POST /api/v1/generations:export`
- Traces/metrics go through OTEL collector/alloy, not through Sigil ingest.
- Required generation modes: `SYNC` (non-stream), `STREAM` (stream).
- Raw provider artifacts are default OFF.

## Telemetry fields to prioritize

On generation and tool spans, capture or preserve these when available:

- identity and routing: `gen_ai.operation.name`, `sigil.generation.id`, `gen_ai.conversation.id`, `gen_ai.agent.name`, `gen_ai.agent.version`, `sigil.generation.parent_generation_ids`, `sigil.sdk.name`
- model: `gen_ai.provider.name`, `gen_ai.request.model`, `gen_ai.response.model`
- request controls: `gen_ai.request.max_tokens`, `gen_ai.request.temperature`, `gen_ai.request.top_p`, `sigil.gen_ai.request.tool_choice`, `sigil.gen_ai.request.thinking.enabled`
- usage and outcomes: `gen_ai.usage.input_tokens`, `gen_ai.usage.output_tokens`, `gen_ai.usage.cache_read_input_tokens`, `gen_ai.usage.cache_creation_input_tokens`, `gen_ai.usage.reasoning_tokens`, `gen_ai.response.finish_reasons`

## Multi-agent dependency tracking

Set `parent_generation_ids` on the GenerationStart/seed with generation IDs of upstream agents whose output this generation consumes. Sigil uses these links to build a dependency DAG and propagate quality signals.

## Useful examples

- Go explicit generation: `go/sigil/example_test.go`
- Go provider wrappers: `go-providers/openai/sdk_example_test.go`, `go-providers/anthropic/sdk_example_test.go`
- .NET emitter: `dotnet/examples/Grafana.Sigil.DevExEmitter/Program.cs`
- JS frameworks: `js/test/frameworks.vercel-ai-sdk.test.mjs`
- Python frameworks: `python-frameworks/*/tests/*.py`

## Implementation rules

- Prefer small targeted patches over refactors.
- Use existing conventions in each language package.
- Keep raw artifacts disabled unless explicitly asked.
- Ensure non-stream wrappers set `SYNC`, stream wrappers set `STREAM`.
- Ensure lifecycle flush/shutdown semantics are preserved.
- Add or update tests for changed instrumentation behavior.
