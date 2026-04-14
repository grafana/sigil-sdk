import assert from 'node:assert/strict';
import test from 'node:test';
import { SpanStatusCode } from '@opentelemetry/api';
import { BasicTracerProvider, InMemorySpanExporter, SimpleSpanProcessor } from '@opentelemetry/sdk-trace-base';
import { defaultConfig, SigilClient } from '../.test-dist/index.js';

class CapturingExporter {
  requests = [];

  async exportGenerations(request) {
    this.requests.push(structuredClone(request));
    return {
      results: request.generations.map((generation) => ({
        generationId: generation.id,
        accepted: true,
      })),
    };
  }
}

// ---------------------------------------------------------------------------
// stripContent — generation-level content stripping
// ---------------------------------------------------------------------------

test('metadata_only strips sensitive content from generation', async () => {
  const harness = newHarness({ contentCapture: 'metadata_only' });

  try {
    const recorder = harness.client.startGeneration({
      model: { provider: 'anthropic', name: 'claude-sonnet-4-5' },
      systemPrompt: 'You are helpful.',
      conversationTitle: 'Weather chat with user',
    });
    recorder.setResult({
      input: [
        { role: 'user', parts: [{ type: 'text', text: 'What is the weather?' }] },
        {
          role: 'tool',
          parts: [
            {
              type: 'tool_result',
              toolResult: { toolCallId: 'call_1', name: 'weather', content: 'sunny 18C', contentJSON: '{"temp":18}' },
            },
          ],
        },
      ],
      output: [
        {
          role: 'assistant',
          parts: [
            { type: 'thinking', thinking: 'let me think about weather' },
            {
              type: 'tool_call',
              toolCall: { id: 'call_1', name: 'weather', inputJSON: '{"city":"Paris"}' },
            },
            { type: 'text', text: "It's 18C and sunny in Paris." },
          ],
        },
      ],
      tools: [
        { name: 'weather', description: 'Get weather info', type: 'function', inputSchemaJSON: '{"type":"object"}' },
      ],
      usage: { inputTokens: 120, outputTokens: 42 },
      stopReason: 'end_turn',
      artifacts: [{ type: 'request', payload: 'raw' }],
    });
    recorder.end();
    assert.equal(recorder.getError(), undefined);

    const gen = singleGeneration(harness.client);

    // Sensitive content stripped
    assert.equal(gen.systemPrompt, '');
    assert.equal(gen.conversationTitle, '');
    assert.equal(gen.metadata['sigil.conversation.title'], undefined);
    assert.equal(gen.artifacts, null);
    assert.equal(gen.input[0].parts[0].text, '');
    assert.equal(gen.output[0].parts[0].thinking, '');
    assert.equal(gen.output[0].parts[1].toolCall.inputJSON, '');
    assert.equal(gen.output[0].parts[2].text, '');
    assert.equal(gen.input[1].parts[0].toolResult.content, '');
    assert.equal(gen.input[1].parts[0].toolResult.contentJSON, '');
    assert.equal(gen.tools[0].description, '');
    assert.equal(gen.tools[0].inputSchemaJSON, '');

    // Structure preserved
    assert.equal(gen.input.length, 2);
    assert.equal(gen.output.length, 1);
    assert.equal(gen.output[0].parts.length, 3);
    assert.equal(gen.input[0].role, 'user');
    assert.equal(gen.output[0].parts[0].type, 'thinking');
    assert.equal(gen.output[0].parts[1].toolCall.name, 'weather');
    assert.equal(gen.output[0].parts[1].toolCall.id, 'call_1');
    assert.equal(gen.input[1].parts[0].toolResult.toolCallId, 'call_1');
    assert.equal(gen.input[1].parts[0].toolResult.name, 'weather');

    // Operational metadata preserved
    assert.equal(gen.tools[0].name, 'weather');
    assert.equal(gen.usage.inputTokens, 120);
    assert.equal(gen.usage.outputTokens, 42);
    assert.equal(gen.stopReason, 'end_turn');
    assert.equal(gen.model.name, 'claude-sonnet-4-5');
    assert.equal(gen.metadata['sigil.sdk.name'], 'sdk-js');
  } finally {
    await shutdownHarness(harness);
  }
});

test('metadata_only replaces callError with category', async () => {
  const harness = newHarness({ contentCapture: 'metadata_only' });

  try {
    const recorder = harness.client.startGeneration({
      model: { provider: 'openai', name: 'gpt-5' },
    });
    recorder.setCallError(new Error('rate limit exceeded: 429 Too Many Requests'));
    recorder.setResult({
      input: [{ role: 'user', parts: [{ type: 'text', text: 'hello' }] }],
      output: [{ role: 'assistant', parts: [{ type: 'text', text: 'world' }] }],
    });
    recorder.end();

    const gen = singleGeneration(harness.client);
    assert.equal(gen.callError, 'rate_limit');
    assert.equal(gen.metadata.call_error, undefined);
  } finally {
    await shutdownHarness(harness);
  }
});

test('metadata_only falls back to sdk_error without category', async () => {
  const harness = newHarness({ contentCapture: 'metadata_only' });

  try {
    const recorder = harness.client.startGeneration({
      model: { provider: 'openai', name: 'gpt-5' },
    });
    recorder.setCallError(new Error('something went wrong'));
    recorder.setResult({
      input: [{ role: 'user', parts: [{ type: 'text', text: 'hello' }] }],
      output: [{ role: 'assistant', parts: [{ type: 'text', text: 'world' }] }],
    });
    recorder.end();

    const gen = singleGeneration(harness.client);
    assert.equal(gen.callError, 'sdk_error');
  } finally {
    await shutdownHarness(harness);
  }
});

test('metadata_only does not leak raw callError into OTel span', async () => {
  const harness = newHarness({ contentCapture: 'metadata_only' });

  try {
    const rawError = 'rate limit exceeded: 429 Too Many Requests';
    const recorder = harness.client.startGeneration({
      model: { provider: 'openai', name: 'gpt-5' },
    });
    recorder.setCallError(new Error(rawError));
    recorder.setResult({
      input: [{ role: 'user', parts: [{ type: 'text', text: 'hello' }] }],
      output: [{ role: 'assistant', parts: [{ type: 'text', text: 'world' }] }],
    });
    recorder.end();

    const span = singleGenerationSpan(harness.spanExporter);
    assert.equal(span.status.code, SpanStatusCode.ERROR);
    assert.equal(span.status.message, 'rate_limit');
    assert.notEqual(span.status.message, rawError);
    assert.equal(span.attributes['error.category'], 'rate_limit', 'error.category attribute must be rate_limit');

    for (const event of span.events) {
      if (event.name === 'exception') {
        const msg = event.attributes?.['exception.message'];
        assert.notEqual(msg, rawError, 'span exception event must not contain raw error');
        assert.equal(msg, 'rate_limit');
      }
    }
  } finally {
    await shutdownHarness(harness);
  }
});

test('metadata_only span uses sdk_error for uncategorized callError', async () => {
  const harness = newHarness({ contentCapture: 'metadata_only' });

  try {
    const rawError = 'something completely unexpected happened';
    const recorder = harness.client.startGeneration({
      model: { provider: 'openai', name: 'gpt-5' },
    });
    recorder.setCallError(new Error(rawError));
    recorder.setResult({
      input: [{ role: 'user', parts: [{ type: 'text', text: 'hello' }] }],
      output: [{ role: 'assistant', parts: [{ type: 'text', text: 'world' }] }],
    });
    recorder.end();

    const span = singleGenerationSpan(harness.spanExporter);
    assert.equal(span.status.code, SpanStatusCode.ERROR);
    assert.equal(span.status.message, 'sdk_error');
    assert.notEqual(span.status.message, rawError);
  } finally {
    await shutdownHarness(harness);
  }
});

// ---------------------------------------------------------------------------
// Content capture mode stamping in generation metadata
// ---------------------------------------------------------------------------

test('content capture mode is stamped in generation metadata', async () => {
  const cases = [
    { clientMode: 'default', genMode: undefined, wantMarker: 'no_tool_content', wantStripped: false },
    { clientMode: 'metadata_only', genMode: undefined, wantMarker: 'metadata_only', wantStripped: true },
    { clientMode: 'full', genMode: 'metadata_only', wantMarker: 'metadata_only', wantStripped: true },
    { clientMode: 'metadata_only', genMode: 'full', wantMarker: 'full', wantStripped: false },
    { clientMode: 'full', genMode: undefined, wantMarker: 'full', wantStripped: false },
  ];

  for (const tc of cases) {
    const harness = newHarness({ contentCapture: tc.clientMode });

    try {
      const recorder = harness.client.startGeneration({
        model: { provider: 'anthropic', name: 'claude-sonnet-4-5' },
        contentCapture: tc.genMode,
      });
      recorder.setResult({
        systemPrompt: 'You are helpful.',
        input: [{ role: 'user', parts: [{ type: 'text', text: 'Hello' }] }],
        output: [{ role: 'assistant', parts: [{ type: 'text', text: 'Hi there' }] }],
        usage: { inputTokens: 10, outputTokens: 5 },
      });
      recorder.end();
      assert.equal(recorder.getError(), undefined, `case ${tc.clientMode}/${tc.genMode}: unexpected error`);

      const gen = singleGeneration(harness.client);
      assert.equal(
        gen.metadata['sigil.sdk.content_capture_mode'],
        tc.wantMarker,
        `case ${tc.clientMode}/${tc.genMode}: marker`,
      );

      const stripped = gen.input[0].parts[0].text === '';
      assert.equal(stripped, tc.wantStripped, `case ${tc.clientMode}/${tc.genMode}: stripped`);

      // Structure always preserved
      assert.equal(gen.input.length, 1);
      assert.equal(gen.input[0].role, 'user');
      assert.equal(gen.usage.inputTokens, 10);
    } finally {
      await shutdownHarness(harness);
    }
  }
});

// ---------------------------------------------------------------------------
// Content capture with resolver
// ---------------------------------------------------------------------------

test('contentCaptureResolver overrides client mode', async () => {
  const harness = newHarness({
    contentCapture: 'full',
    contentCaptureResolver: () => 'metadata_only',
  });

  try {
    const recorder = harness.client.startGeneration({
      model: { provider: 'test', name: 'test-model' },
    });
    recorder.setResult({
      input: [{ role: 'user', parts: [{ type: 'text', text: 'hello' }] }],
      output: [{ role: 'assistant', parts: [{ type: 'text', text: 'world' }] }],
    });
    recorder.end();
    assert.equal(recorder.getError(), undefined);

    const gen = singleGeneration(harness.client);
    assert.equal(gen.metadata['sigil.sdk.content_capture_mode'], 'metadata_only');
    assert.equal(gen.input[0].parts[0].text, '');
  } finally {
    await shutdownHarness(harness);
  }
});

test('per-generation contentCapture overrides resolver', async () => {
  const harness = newHarness({
    contentCapture: 'default',
    contentCaptureResolver: () => 'metadata_only',
  });

  try {
    const recorder = harness.client.startGeneration({
      model: { provider: 'test', name: 'test-model' },
      contentCapture: 'full',
    });
    recorder.setResult({
      input: [{ role: 'user', parts: [{ type: 'text', text: 'hello' }] }],
      output: [{ role: 'assistant', parts: [{ type: 'text', text: 'world' }] }],
    });
    recorder.end();
    assert.equal(recorder.getError(), undefined);

    const gen = singleGeneration(harness.client);
    assert.equal(gen.metadata['sigil.sdk.content_capture_mode'], 'full');
    assert.equal(gen.input[0].parts[0].text, 'hello');
  } finally {
    await shutdownHarness(harness);
  }
});

test('resolver returning default defers to client mode', async () => {
  const harness = newHarness({
    contentCapture: 'metadata_only',
    contentCaptureResolver: () => 'default',
  });

  try {
    const recorder = harness.client.startGeneration({
      model: { provider: 'test', name: 'test-model' },
    });
    recorder.setResult({
      input: [{ role: 'user', parts: [{ type: 'text', text: 'hello' }] }],
      output: [{ role: 'assistant', parts: [{ type: 'text', text: 'world' }] }],
    });
    recorder.end();
    assert.equal(recorder.getError(), undefined);

    const gen = singleGeneration(harness.client);
    assert.equal(gen.metadata['sigil.sdk.content_capture_mode'], 'metadata_only');
    assert.equal(gen.input[0].parts[0].text, '');
  } finally {
    await shutdownHarness(harness);
  }
});

test('throwing resolver fails closed to metadata_only', async () => {
  const harness = newHarness({
    contentCapture: 'full',
    contentCaptureResolver: () => {
      throw new Error('resolver bug');
    },
  });

  try {
    const recorder = harness.client.startGeneration({
      model: { provider: 'test', name: 'test-model' },
    });
    recorder.setResult({
      input: [{ role: 'user', parts: [{ type: 'text', text: 'hello' }] }],
      output: [{ role: 'assistant', parts: [{ type: 'text', text: 'world' }] }],
    });
    recorder.end();
    assert.equal(recorder.getError(), undefined);

    const gen = singleGeneration(harness.client);
    assert.equal(gen.metadata['sigil.sdk.content_capture_mode'], 'metadata_only');
    assert.equal(gen.input[0].parts[0].text, '');
  } finally {
    await shutdownHarness(harness);
  }
});

// ---------------------------------------------------------------------------
// Tool execution content capture
// ---------------------------------------------------------------------------

test('tool execution content capture modes', async () => {
  const cases = [
    // client Default (→ NoToolContent), legacy controls.
    {
      name: 'client default, legacy false — suppressed',
      clientMode: 'default',
      toolMode: undefined,
      legacy: false,
      wantContent: false,
    },
    {
      name: 'client default, legacy true — included',
      clientMode: 'default',
      toolMode: undefined,
      legacy: true,
      wantContent: true,
    },
    // Explicit Full client — legacy irrelevant.
    {
      name: 'client full, legacy false — included',
      clientMode: 'full',
      toolMode: undefined,
      legacy: false,
      wantContent: true,
    },
    {
      name: 'client full, legacy true — included',
      clientMode: 'full',
      toolMode: undefined,
      legacy: true,
      wantContent: true,
    },
    // Client MetadataOnly — always suppressed.
    {
      name: 'client metadata_only, legacy true — suppressed',
      clientMode: 'metadata_only',
      toolMode: undefined,
      legacy: true,
      wantContent: false,
    },
    // Per-tool overrides.
    {
      name: 'tool full overrides client metadata_only',
      clientMode: 'metadata_only',
      toolMode: 'full',
      legacy: false,
      wantContent: true,
    },
    {
      name: 'tool metadata_only overrides client full',
      clientMode: 'full',
      toolMode: 'metadata_only',
      legacy: true,
      wantContent: false,
    },
  ];

  for (const tc of cases) {
    const harness = newHarness({ contentCapture: tc.clientMode });

    try {
      const recorder = harness.client.startToolExecution({
        toolName: 'test_tool',
        contentCapture: tc.toolMode,
        includeContent: tc.legacy,
      });
      recorder.setResult({
        arguments: { city: 'Paris' },
        result: { temp: 18 },
      });
      recorder.end();
      assert.equal(recorder.getError(), undefined, `case "${tc.name}": unexpected error`);

      const span = singleToolSpan(harness.spanExporter);
      const hasArgs = 'gen_ai.tool.call.arguments' in span.attributes;
      assert.equal(
        hasArgs,
        tc.wantContent,
        `case "${tc.name}": tool arguments present=${hasArgs}, want=${tc.wantContent}`,
      );

      assert.ok('gen_ai.tool.name' in span.attributes, `case "${tc.name}": tool name always present`);
    } finally {
      await shutdownHarness(harness);
    }
  }
});

test('tool execution with resolver overriding client mode', async () => {
  const harness = newHarness({
    contentCapture: 'full',
    contentCaptureResolver: () => 'metadata_only',
  });

  try {
    const recorder = harness.client.startToolExecution({
      toolName: 'test_tool',
      includeContent: true,
    });
    recorder.setResult({
      arguments: 'args',
      result: 'result',
    });
    recorder.end();
    assert.equal(recorder.getError(), undefined);

    const span = singleToolSpan(harness.spanExporter);
    assert.equal(
      'gen_ai.tool.call.arguments' in span.attributes,
      false,
      'resolver metadata_only should suppress tool content',
    );
  } finally {
    await shutdownHarness(harness);
  }
});

test('per-tool contentCapture full overrides resolver metadata_only', async () => {
  const harness = newHarness({
    contentCapture: 'default',
    contentCaptureResolver: () => 'metadata_only',
  });

  try {
    const recorder = harness.client.startToolExecution({
      toolName: 'test_tool',
      contentCapture: 'full',
      includeContent: false,
    });
    recorder.setResult({
      arguments: 'args',
      result: 'result',
    });
    recorder.end();
    assert.equal(recorder.getError(), undefined);

    const span = singleToolSpan(harness.spanExporter);
    assert.equal('gen_ai.tool.call.arguments' in span.attributes, true, 'per-tool full should override resolver');
  } finally {
    await shutdownHarness(harness);
  }
});

// ---------------------------------------------------------------------------
// full mode preserves all content
// ---------------------------------------------------------------------------

test('full mode preserves all generation content', async () => {
  const harness = newHarness({ contentCapture: 'full' });

  try {
    const recorder = harness.client.startGeneration({
      model: { provider: 'anthropic', name: 'claude-sonnet-4-5' },
      systemPrompt: 'You are helpful.',
      conversationTitle: 'Math question',
    });
    recorder.setResult({
      input: [{ role: 'user', parts: [{ type: 'text', text: 'What is 2+2?' }] }],
      output: [{ role: 'assistant', parts: [{ type: 'text', text: '4' }] }],
      usage: { inputTokens: 10, outputTokens: 1 },
      artifacts: [{ type: 'request', payload: '{}' }],
    });
    recorder.end();
    assert.equal(recorder.getError(), undefined);

    const gen = singleGeneration(harness.client);
    assert.equal(gen.metadata['sigil.sdk.content_capture_mode'], 'full');
    assert.equal(gen.systemPrompt, 'You are helpful.');
    assert.equal(gen.conversationTitle, 'Math question');
    assert.equal(gen.input[0].parts[0].text, 'What is 2+2?');
    assert.equal(gen.output[0].parts[0].text, '4');
    assert.equal(gen.artifacts.length, 1);
  } finally {
    await shutdownHarness(harness);
  }
});

// ---------------------------------------------------------------------------
// no_tool_content mode preserves generation content
// ---------------------------------------------------------------------------

test('no_tool_content preserves generation content', async () => {
  const harness = newHarness({ contentCapture: 'no_tool_content' });

  try {
    const recorder = harness.client.startGeneration({
      model: { provider: 'openai', name: 'gpt-5' },
      systemPrompt: 'Be concise.',
    });
    recorder.setResult({
      input: [{ role: 'user', parts: [{ type: 'text', text: 'Hello' }] }],
      output: [{ role: 'assistant', parts: [{ type: 'text', text: 'Hi!' }] }],
    });
    recorder.end();
    assert.equal(recorder.getError(), undefined);

    const gen = singleGeneration(harness.client);
    assert.equal(gen.metadata['sigil.sdk.content_capture_mode'], 'no_tool_content');
    assert.equal(gen.systemPrompt, 'Be concise.');
    assert.equal(gen.input[0].parts[0].text, 'Hello');
    assert.equal(gen.output[0].parts[0].text, 'Hi!');
  } finally {
    await shutdownHarness(harness);
  }
});

// ---------------------------------------------------------------------------
// exported generation payload has content capture mode stamped
// ---------------------------------------------------------------------------

test('exported generation includes content capture mode metadata', async () => {
  const harness = newHarness({ contentCapture: 'full' });

  try {
    const recorder = harness.client.startGeneration({
      model: { provider: 'openai', name: 'gpt-5' },
    });
    recorder.setResult({
      output: [{ role: 'assistant', content: 'ok' }],
    });
    recorder.end();
    assert.equal(recorder.getError(), undefined);

    await harness.client.flush();
    assert.equal(harness.generationExporter.requests.length, 1);
    const exported = harness.generationExporter.requests[0].generations[0];
    assert.equal(exported.metadata['sigil.sdk.content_capture_mode'], 'full');
  } finally {
    await shutdownHarness(harness);
  }
});

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function newHarness(overrides = {}) {
  const spanExporter = new InMemorySpanExporter();
  const traceProvider = new BasicTracerProvider({
    spanProcessors: [new SimpleSpanProcessor(spanExporter)],
  });
  const tracer = traceProvider.getTracer('sigil-sdk-js-test');
  const generationExporter = new CapturingExporter();
  const defaults = defaultConfig();

  const { contentCapture, contentCaptureResolver, ...exportOverrides } = overrides;

  const client = new SigilClient({
    tracer,
    generationExport: {
      ...defaults.generationExport,
      batchSize: 100,
      flushIntervalMs: 60_000,
      maxRetries: 1,
      initialBackoffMs: 1,
      maxBackoffMs: 1,
      ...exportOverrides,
    },
    contentCapture,
    contentCaptureResolver,
    generationExporter,
  });

  return {
    client,
    spanExporter,
    traceProvider,
    generationExporter,
  };
}

async function shutdownHarness(harness) {
  await harness.client.shutdown();
  await harness.traceProvider.shutdown();
}

function singleGeneration(client) {
  const snapshot = client.debugSnapshot();
  assert.equal(snapshot.generations.length, 1);
  return snapshot.generations[0];
}

function generationSpans(spanExporter) {
  return spanExporter.getFinishedSpans().filter((span) => {
    const operation = span.attributes['gen_ai.operation.name'];
    return operation !== 'execute_tool' && operation !== 'embeddings';
  });
}

function singleGenerationSpan(spanExporter) {
  const spans = generationSpans(spanExporter);
  assert.equal(spans.length, 1);
  return spans[0];
}

function toolSpans(spanExporter) {
  return spanExporter.getFinishedSpans().filter((span) => span.attributes['gen_ai.operation.name'] === 'execute_tool');
}

function singleToolSpan(spanExporter) {
  const spans = toolSpans(spanExporter);
  assert.equal(spans.length, 1);
  return spans[0];
}
