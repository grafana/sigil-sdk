# Grafana Sigil Python SDK

If you already use OpenTelemetry, Sigil is a thin extension plus sugar for AI observability.

## Core API direction (explicit, primary)

Core SDK docs are explicit API first:

- `start_generation(...)`
- `start_streaming_generation(...)`
- `start_tool_execution(...)`
- recorder methods: `set_result(...)`, `set_call_error(...)`, `end()`
- lifecycle: `flush()`, `shutdown()`

### Primary usage style: context manager

```python
client = sigil.Client(config)

with client.start_generation(
    conversation_id="conv-1",
    model={"provider": "openai", "name": "gpt-5"},
) as rec:
    resp = openai_client.responses.create(**req)
    rec.set_result(sigil_openai.from_response(req, resp))

client.shutdown()
```

Streaming variant:

```python
with client.start_streaming_generation(
    conversation_id="conv-1",
    model={"provider": "anthropic", "name": "claude-sonnet-4-5"},
) as rec:
    summary = collect_stream(...)
    rec.set_result(sigil_anthropic.from_stream(req, summary))
```

## Provider docs direction (wrapper-first)

Provider package docs are wrapper-first and include explicit flow as secondary guidance.

Parity target:

- OpenAI
- Anthropic
- Gemini

## Runtime behavior contract

- generation mode is explicit (`SYNC` / `STREAM`)
- exports are async with bounded queue + retry/backoff
- `shutdown()` flushes pending batches and trace provider state
- local recorder errors are surfaced separately from background export retries

## Raw artifact policy

- default OFF
- explicit debug opt-in only
