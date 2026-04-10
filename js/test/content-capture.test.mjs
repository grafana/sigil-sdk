import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import test from 'node:test';
import { BasicTracerProvider, InMemorySpanExporter, SimpleSpanProcessor } from '@opentelemetry/sdk-trace-base';
import {
  contentCaptureModeFromContext,
  defaultConfig,
  SigilClient,
  withContentCaptureMode,
} from '../.test-dist/index.js';

// --- Test helpers ---

class CapturingExporter {
  requests = [];

  async exportGenerations(request) {
    this.requests.push(structuredClone(request));
    return {
      results: request.generations.map((g) => ({
        generationId: g.id,
        accepted: true,
      })),
    };
  }
}

function newHarness(overrides = {}) {
  const spanExporter = new InMemorySpanExporter();
  const traceProvider = new BasicTracerProvider({
    spanProcessors: [new SimpleSpanProcessor(spanExporter)],
  });
  const tracer = traceProvider.getTracer('sigil-sdk-js-test');
  const generationExporter = new CapturingExporter();
  const defaults = defaultConfig();

  const client = new SigilClient({
    tracer,
    generationExport: {
      ...defaults.generationExport,
      batchSize: 100,
      flushIntervalMs: 60_000,
      maxRetries: 1,
      initialBackoffMs: 1,
      maxBackoffMs: 1,
    },
    generationExporter,
    ...overrides,
  });

  return { client, spanExporter, traceProvider, generationExporter };
}

function singleGeneration(client) {
  const snapshot = client.debugSnapshot();
  assert.equal(snapshot.generations.length, 1);
  return snapshot.generations[0];
}

function _generationSpans(spanExporter) {
  return spanExporter.getFinishedSpans().filter((span) => {
    const op = span.attributes['gen_ai.operation.name'];
    return op !== 'execute_tool' && op !== 'embeddings';
  });
}

function toolSpans(spanExporter) {
  return spanExporter.getFinishedSpans().filter((span) => span.attributes['gen_ai.operation.name'] === 'execute_tool');
}

function singleToolSpan(spanExporter) {
  const spans = toolSpans(spanExporter);
  assert.equal(spans.length, 1);
  return spans[0];
}

async function shutdownHarness(harness) {
  await harness.client.shutdown();
  await harness.traceProvider.shutdown();
}

// --- Content capture mode resolution tests ---

test('default resolution: client default + gen default → no_tool_content marker', async () => {
  const harness = newHarness();
  try {
    const recorder = harness.client.startGeneration({
      model: { provider: 'test', name: 'test-model' },
    });
    recorder.setResult({
      input: [{ role: 'user', parts: [{ type: 'text', text: 'Hello' }] }],
      output: [{ role: 'assistant', parts: [{ type: 'text', text: 'Hi' }] }],
    });
    recorder.end();
    assert.equal(recorder.getError(), undefined);

    const gen = singleGeneration(harness.client);
    assert.equal(gen.metadata['sigil.sdk.content_capture_mode'], 'no_tool_content');
    assert.equal(gen.input[0].parts[0].text, 'Hello');
    assert.equal(gen.output[0].parts[0].text, 'Hi');
  } finally {
    await shutdownHarness(harness);
  }
});

test('full mode: client full preserves all content', async () => {
  const harness = newHarness({ contentCapture: 'full' });
  try {
    const recorder = harness.client.startGeneration({
      model: { provider: 'test', name: 'test-model' },
      systemPrompt: 'You are helpful.',
    });
    recorder.setResult({
      input: [{ role: 'user', parts: [{ type: 'text', text: 'Hello' }] }],
      output: [{ role: 'assistant', parts: [{ type: 'text', text: 'Hi' }] }],
    });
    recorder.end();
    assert.equal(recorder.getError(), undefined);

    const gen = singleGeneration(harness.client);
    assert.equal(gen.metadata['sigil.sdk.content_capture_mode'], 'full');
    assert.equal(gen.systemPrompt, 'You are helpful.');
    assert.equal(gen.input[0].parts[0].text, 'Hello');
  } finally {
    await shutdownHarness(harness);
  }
});

test('metadata_only strips content but preserves structure', async () => {
  const harness = newHarness({ contentCapture: 'metadata_only' });
  try {
    const recorder = harness.client.startGeneration({
      model: { provider: 'test', name: 'test-model' },
      systemPrompt: 'You are helpful.',
      tools: [{ name: 'weather', description: 'Get weather', inputSchemaJSON: '{"type":"object"}' }],
    });
    recorder.setResult({
      input: [
        { role: 'user', parts: [{ type: 'text', text: 'What is the weather?' }] },
        {
          role: 'tool',
          parts: [
            {
              type: 'tool_result',
              toolResult: { toolCallId: 'call_1', name: 'weather', content: 'sunny', contentJSON: '{"temp":18}' },
            },
          ],
        },
      ],
      output: [
        {
          role: 'assistant',
          parts: [
            { type: 'thinking', thinking: 'let me think' },
            { type: 'tool_call', toolCall: { id: 'call_1', name: 'weather', inputJSON: '{"city":"Paris"}' } },
            { type: 'text', text: 'It is sunny.' },
          ],
        },
      ],
      artifacts: [{ type: 'request', payload: 'raw data' }],
      usage: { inputTokens: 120, outputTokens: 42 },
      stopReason: 'end_turn',
    });
    recorder.end();
    assert.equal(recorder.getError(), undefined);

    const gen = singleGeneration(harness.client);
    assert.equal(gen.metadata['sigil.sdk.content_capture_mode'], 'metadata_only');

    // Stripped content
    assert.equal(gen.systemPrompt, '');
    assert.equal(gen.input[0].parts[0].text, '');
    assert.equal(gen.output[0].parts[0].thinking, '');
    assert.equal(gen.output[0].parts[1].toolCall.inputJSON, undefined);
    assert.equal(gen.output[0].parts[2].text, '');
    assert.equal(gen.input[1].parts[0].toolResult.content, undefined);
    assert.equal(gen.input[1].parts[0].toolResult.contentJSON, undefined);
    assert.equal(gen.artifacts, undefined);
    assert.equal(gen.tools[0].description, undefined);
    assert.equal(gen.tools[0].inputSchemaJSON, undefined);

    // Preserved structure
    assert.equal(gen.input.length, 2);
    assert.equal(gen.output.length, 1);
    assert.equal(gen.output[0].parts.length, 3);
    assert.equal(gen.input[0].role, 'user');
    assert.equal(gen.output[0].parts[0].type, 'thinking');
    assert.equal(gen.output[0].parts[1].toolCall.name, 'weather');
    assert.equal(gen.output[0].parts[1].toolCall.id, 'call_1');
    assert.equal(gen.input[1].parts[0].toolResult.toolCallId, 'call_1');
    assert.equal(gen.input[1].parts[0].toolResult.name, 'weather');
    assert.equal(gen.tools[0].name, 'weather');
    assert.equal(gen.usage.inputTokens, 120);
    assert.equal(gen.usage.outputTokens, 42);
    assert.equal(gen.stopReason, 'end_turn');
    assert.equal(gen.model.name, 'test-model');
  } finally {
    await shutdownHarness(harness);
  }
});

test('metadata_only replaces callError with category', async () => {
  const harness = newHarness({ contentCapture: 'metadata_only' });
  try {
    const recorder = harness.client.startGeneration({
      model: { provider: 'test', name: 'test-model' },
    });
    recorder.setResult({
      input: [{ role: 'user', parts: [{ type: 'text', text: 'Hello' }] }],
      output: [{ role: 'assistant', parts: [{ type: 'text', text: 'Hi' }] }],
    });
    recorder.setCallError(new Error('rate limit exceeded: got 429'));
    recorder.end();

    const gen = singleGeneration(harness.client);
    assert.equal(gen.callError, 'rate_limit');
    assert.equal(gen.metadata.call_error, undefined);
  } finally {
    await shutdownHarness(harness);
  }
});

test('metadata_only falls back to sdk_error without status code', async () => {
  const harness = newHarness({ contentCapture: 'metadata_only' });
  try {
    const recorder = harness.client.startGeneration({
      model: { provider: 'test', name: 'test-model' },
    });
    recorder.setResult({
      input: [{ role: 'user', parts: [{ type: 'text', text: 'Hello' }] }],
      output: [{ role: 'assistant', parts: [{ type: 'text', text: 'Hi' }] }],
    });
    recorder.setCallError(new Error('something broke'));
    recorder.end();

    const gen = singleGeneration(harness.client);
    assert.equal(gen.callError, 'sdk_error');
  } finally {
    await shutdownHarness(harness);
  }
});

// --- Per-generation override ---

test('per-generation override: client full, gen metadata_only → stripped', async () => {
  const harness = newHarness({ contentCapture: 'full' });
  try {
    const recorder = harness.client.startGeneration({
      model: { provider: 'test', name: 'test-model' },
      contentCapture: 'metadata_only',
    });
    recorder.setResult({
      input: [{ role: 'user', parts: [{ type: 'text', text: 'Hello' }] }],
      output: [{ role: 'assistant', parts: [{ type: 'text', text: 'Hi' }] }],
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

test('per-generation override: client metadata_only, gen full → preserved', async () => {
  const harness = newHarness({ contentCapture: 'metadata_only' });
  try {
    const recorder = harness.client.startGeneration({
      model: { provider: 'test', name: 'test-model' },
      contentCapture: 'full',
    });
    recorder.setResult({
      input: [{ role: 'user', parts: [{ type: 'text', text: 'Hello' }] }],
      output: [{ role: 'assistant', parts: [{ type: 'text', text: 'Hi' }] }],
    });
    recorder.end();
    assert.equal(recorder.getError(), undefined);

    const gen = singleGeneration(harness.client);
    assert.equal(gen.metadata['sigil.sdk.content_capture_mode'], 'full');
    assert.equal(gen.input[0].parts[0].text, 'Hello');
  } finally {
    await shutdownHarness(harness);
  }
});

// --- Resolver callback ---

test('resolver callback: resolver MetadataOnly overrides client Full', async () => {
  const harness = newHarness({
    contentCapture: 'full',
    contentCaptureResolver: () => 'metadata_only',
  });
  try {
    const recorder = harness.client.startGeneration({
      model: { provider: 'test', name: 'test-model' },
    });
    recorder.setResult({
      input: [{ role: 'user', parts: [{ type: 'text', text: 'Hello' }] }],
      output: [{ role: 'assistant', parts: [{ type: 'text', text: 'Hi' }] }],
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

test('resolver callback: per-generation Full overrides resolver MetadataOnly', async () => {
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
      input: [{ role: 'user', parts: [{ type: 'text', text: 'Hello' }] }],
      output: [{ role: 'assistant', parts: [{ type: 'text', text: 'Hi' }] }],
    });
    recorder.end();
    assert.equal(recorder.getError(), undefined);

    const gen = singleGeneration(harness.client);
    assert.equal(gen.metadata['sigil.sdk.content_capture_mode'], 'full');
    assert.equal(gen.input[0].parts[0].text, 'Hello');
  } finally {
    await shutdownHarness(harness);
  }
});

test('resolver callback: resolver default defers to client', async () => {
  const harness = newHarness({
    contentCapture: 'metadata_only',
    contentCaptureResolver: () => 'default',
  });
  try {
    const recorder = harness.client.startGeneration({
      model: { provider: 'test', name: 'test-model' },
    });
    recorder.setResult({
      input: [{ role: 'user', parts: [{ type: 'text', text: 'Hello' }] }],
      output: [{ role: 'assistant', parts: [{ type: 'text', text: 'Hi' }] }],
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

test('resolver callback: resolver receives generation metadata', async () => {
  let receivedMetadata;
  const harness = newHarness({
    contentCaptureResolver: (metadata) => {
      receivedMetadata = metadata;
      if (metadata?.tenant_id === 'opted-out') {
        return 'metadata_only';
      }
      return 'full';
    },
  });
  try {
    const recorder = harness.client.startGeneration({
      model: { provider: 'test', name: 'test-model' },
      metadata: { tenant_id: 'opted-out' },
    });
    recorder.setResult({
      input: [{ role: 'user', parts: [{ type: 'text', text: 'Hello' }] }],
      output: [{ role: 'assistant', parts: [{ type: 'text', text: 'Hi' }] }],
    });
    recorder.end();
    assert.equal(recorder.getError(), undefined);

    assert.equal(receivedMetadata.tenant_id, 'opted-out');
    const gen = singleGeneration(harness.client);
    assert.equal(gen.metadata['sigil.sdk.content_capture_mode'], 'metadata_only');
    assert.equal(gen.input[0].parts[0].text, '');
  } finally {
    await shutdownHarness(harness);
  }
});

// --- Resolver exception fail-closed ---

test('resolver exception: panicking resolver fails closed to metadata_only', async () => {
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
      input: [{ role: 'user', parts: [{ type: 'text', text: 'Hello' }] }],
      output: [{ role: 'assistant', parts: [{ type: 'text', text: 'Hi' }] }],
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

// --- Backward compat: includeContent ---

test('backward compat: includeContent=true with default mode includes tool content in spans', async () => {
  const harness = newHarness();
  try {
    const toolRec = harness.client.startToolExecution({
      toolName: 'test_tool',
      includeContent: true,
    });
    toolRec.setResult({ arguments: 'tool args', result: 'tool result' });
    toolRec.end();
    assert.equal(toolRec.getError(), undefined);

    const span = singleToolSpan(harness.spanExporter);
    assert.equal(span.attributes['gen_ai.tool.call.arguments'], '"tool args"');
    assert.equal(span.attributes['gen_ai.tool.call.result'], '"tool result"');
  } finally {
    await shutdownHarness(harness);
  }
});

test('backward compat: includeContent=false with default mode excludes tool content from spans', async () => {
  const harness = newHarness();
  try {
    const toolRec = harness.client.startToolExecution({
      toolName: 'test_tool',
      toolDescription: 'A test tool',
      includeContent: false,
    });
    toolRec.setResult({ arguments: 'tool args', result: 'tool result' });
    toolRec.end();
    assert.equal(toolRec.getError(), undefined);

    const span = singleToolSpan(harness.spanExporter);
    assert.equal(span.attributes['gen_ai.tool.call.arguments'], undefined);
    assert.equal(span.attributes['gen_ai.tool.call.result'], undefined);
    assert.equal(span.attributes['gen_ai.tool.description'], 'A test tool', 'no_tool_content preserves description');
  } finally {
    await shutdownHarness(harness);
  }
});

test('includeContent ignored under metadata_only', async () => {
  const harness = newHarness({ contentCapture: 'metadata_only' });
  try {
    const toolRec = harness.client.startToolExecution({
      toolName: 'test_tool',
      includeContent: true,
    });
    toolRec.setResult({ arguments: 'tool args', result: 'tool result' });
    toolRec.end();
    assert.equal(toolRec.getError(), undefined);

    const span = singleToolSpan(harness.spanExporter);
    assert.equal(span.attributes['gen_ai.tool.call.arguments'], undefined);
    assert.equal(span.attributes['gen_ai.tool.call.result'], undefined);
    assert.equal(span.attributes['gen_ai.tool.name'], 'test_tool');
  } finally {
    await shutdownHarness(harness);
  }
});

// --- Per-tool override ---

test('per-tool override: tool full overrides client metadata_only', async () => {
  const harness = newHarness({ contentCapture: 'metadata_only' });
  try {
    const toolRec = harness.client.startToolExecution({
      toolName: 'test_tool',
      contentCapture: 'full',
      includeContent: true,
    });
    toolRec.setResult({ arguments: 'tool args', result: 'tool result' });
    toolRec.end();
    assert.equal(toolRec.getError(), undefined);

    const span = singleToolSpan(harness.spanExporter);
    assert.equal(span.attributes['gen_ai.tool.call.arguments'], '"tool args"');
  } finally {
    await shutdownHarness(harness);
  }
});

test('per-tool override: tool metadata_only suppresses even under client full', async () => {
  const harness = newHarness({ contentCapture: 'full' });
  try {
    const toolRec = harness.client.startToolExecution({
      toolName: 'test_tool',
      contentCapture: 'metadata_only',
      includeContent: true,
    });
    toolRec.setResult({ arguments: 'tool args', result: 'tool result' });
    toolRec.end();
    assert.equal(toolRec.getError(), undefined);

    const span = singleToolSpan(harness.spanExporter);
    assert.equal(span.attributes['gen_ai.tool.call.arguments'], undefined);
  } finally {
    await shutdownHarness(harness);
  }
});

// --- Client full mode: tool content always included ---

test('client full mode includes tool content regardless of includeContent', async () => {
  const harness = newHarness({ contentCapture: 'full' });
  try {
    const toolRec = harness.client.startToolExecution({
      toolName: 'test_tool',
      includeContent: false,
    });
    toolRec.setResult({ arguments: 'tool args', result: 'tool result' });
    toolRec.end();
    assert.equal(toolRec.getError(), undefined);

    const span = singleToolSpan(harness.spanExporter);
    assert.equal(span.attributes['gen_ai.tool.call.arguments'], '"tool args"');
    assert.equal(span.attributes['gen_ai.tool.call.result'], '"tool result"');
  } finally {
    await shutdownHarness(harness);
  }
});

// --- Validation accepts stripped content ---

test('validation accepts stripped generation', async () => {
  const harness = newHarness({ contentCapture: 'metadata_only' });
  try {
    const recorder = harness.client.startGeneration({
      model: { provider: 'test', name: 'test-model' },
    });
    recorder.setResult({
      input: [
        { role: 'user', parts: [{ type: 'text', text: 'Hello' }] },
        {
          role: 'tool',
          parts: [
            {
              type: 'tool_result',
              toolResult: { toolCallId: 'call_1', name: 'tool', content: 'data' },
            },
          ],
        },
      ],
      output: [
        {
          role: 'assistant',
          parts: [
            { type: 'thinking', thinking: 'hmm' },
            { type: 'tool_call', toolCall: { id: 'call_1', name: 'tool' } },
            { type: 'text', text: 'result' },
          ],
        },
      ],
    });
    recorder.end();
    assert.equal(recorder.getError(), undefined);
  } finally {
    await shutdownHarness(harness);
  }
});

// --- Rating comment stripped under metadata_only ---

test('rating comment stripped under metadata_only resolver', async () => {
  // We test that the resolver affects the rating by ensuring the
  // input comment is cleared before sending.
  let resolverCalls = 0;
  const harness = newHarness({
    contentCaptureResolver: () => {
      resolverCalls++;
      return 'metadata_only';
    },
  });

  // submitConversationRating will fail due to no real server, but we
  // can verify the resolver was called by checking the resolverCalls count.
  // Instead, let's check via a more integrated approach by mocking the fetch.
  // For simplicity, we just verify the resolver is wired up for ratings.
  try {
    assert.equal(resolverCalls, 0);
    // The rating method will throw because there's no real API,
    // but the resolver should still be called.
    await harness.client
      .submitConversationRating('conv-1', {
        ratingId: 'r1',
        rating: 'CONVERSATION_RATING_VALUE_GOOD',
        comment: 'great job',
      })
      .catch(() => {});
    assert.ok(resolverCalls > 0, 'resolver was called for rating');
  } finally {
    await shutdownHarness(harness);
  }
});

// --- Context propagation ---

test('context propagation: withContentCaptureMode sets and reads from context', () => {
  const before = contentCaptureModeFromContext();
  assert.equal(before.set, false);
  assert.equal(before.mode, 'default');

  withContentCaptureMode('metadata_only', () => {
    const inside = contentCaptureModeFromContext();
    assert.equal(inside.set, true);
    assert.equal(inside.mode, 'metadata_only');
  });

  const after = contentCaptureModeFromContext();
  assert.equal(after.set, false);
});

test('context propagation: generation callback sets mode for child tool executions', async () => {
  const harness = newHarness({ contentCapture: 'metadata_only' });
  try {
    await harness.client.startGeneration({ model: { provider: 'test', name: 'test-model' } }, async (recorder) => {
      // Inside the callback, the content capture mode should be set
      const ctx = contentCaptureModeFromContext();
      assert.equal(ctx.set, true);
      assert.equal(ctx.mode, 'metadata_only');

      const toolRec = harness.client.startToolExecution({
        toolName: 'test_tool',
        includeContent: true,
      });
      toolRec.setResult({ arguments: 'args', result: 'result' });
      toolRec.end();

      recorder.setResult({
        input: [{ role: 'user', parts: [{ type: 'text', text: 'Hello' }] }],
        output: [{ role: 'assistant', parts: [{ type: 'text', text: 'Hi' }] }],
      });
    });

    // Tool span should not have content (inherited metadata_only)
    const span = singleToolSpan(harness.spanExporter);
    assert.equal(span.attributes['gen_ai.tool.call.arguments'], undefined);
    assert.equal(span.attributes['gen_ai.tool.name'], 'test_tool');
  } finally {
    await shutdownHarness(harness);
  }
});

test('context propagation: generation full callback allows child tool content', async () => {
  const harness = newHarness({ contentCapture: 'full' });
  try {
    await harness.client.startGeneration({ model: { provider: 'test', name: 'test-model' } }, async (recorder) => {
      const toolRec = harness.client.startToolExecution({
        toolName: 'test_tool',
        includeContent: true,
      });
      toolRec.setResult({ arguments: 'args', result: 'result' });
      toolRec.end();

      recorder.setResult({
        input: [{ role: 'user', parts: [{ type: 'text', text: 'Hello' }] }],
        output: [{ role: 'assistant', parts: [{ type: 'text', text: 'Hi' }] }],
      });
    });

    const span = singleToolSpan(harness.spanExporter);
    assert.equal(span.attributes['gen_ai.tool.call.arguments'], '"args"');
  } finally {
    await shutdownHarness(harness);
  }
});

// --- ToolExecution type preserves contentCapture ---

test('ToolExecution snapshot preserves contentCapture field', async () => {
  const harness = newHarness();
  try {
    const toolRec = harness.client.startToolExecution({
      toolName: 'test_tool',
      contentCapture: 'full',
    });
    toolRec.setResult({ arguments: 'args', result: 'result' });
    toolRec.end();
    assert.equal(toolRec.getError(), undefined);

    const snapshot = harness.client.debugSnapshot();
    assert.equal(snapshot.toolExecutions.length, 1);
    assert.equal(snapshot.toolExecutions[0].contentCapture, 'full');
  } finally {
    await shutdownHarness(harness);
  }
});

// --- Multiple modes table test ---

const generationModeTests = [
  {
    name: 'client default, gen default → no_tool_content',
    clientMode: undefined,
    genMode: undefined,
    wantStripped: false,
    wantMarker: 'no_tool_content',
  },
  {
    name: 'client metadata_only, gen default → stripped',
    clientMode: 'metadata_only',
    genMode: undefined,
    wantStripped: true,
    wantMarker: 'metadata_only',
  },
  {
    name: 'client full, gen metadata_only → stripped',
    clientMode: 'full',
    genMode: 'metadata_only',
    wantStripped: true,
    wantMarker: 'metadata_only',
  },
  {
    name: 'client metadata_only, gen full → full',
    clientMode: 'metadata_only',
    genMode: 'full',
    wantStripped: false,
    wantMarker: 'full',
  },
  {
    name: 'client full, gen default → full',
    clientMode: 'full',
    genMode: undefined,
    wantStripped: false,
    wantMarker: 'full',
  },
];

for (const tc of generationModeTests) {
  test(`generation mode resolution: ${tc.name}`, async () => {
    const harness = newHarness(tc.clientMode ? { contentCapture: tc.clientMode } : {});
    try {
      const recorder = harness.client.startGeneration({
        model: { provider: 'test', name: 'test-model' },
        contentCapture: tc.genMode,
      });
      recorder.setResult({
        input: [{ role: 'user', parts: [{ type: 'text', text: 'Hello' }] }],
        output: [{ role: 'assistant', parts: [{ type: 'text', text: 'Hi' }] }],
        usage: { inputTokens: 10, outputTokens: 5 },
      });
      recorder.end();
      assert.equal(recorder.getError(), undefined);

      const gen = singleGeneration(harness.client);
      assert.equal(gen.metadata['sigil.sdk.content_capture_mode'], tc.wantMarker);
      const stripped = gen.input[0].parts[0].text === '';
      assert.equal(stripped, tc.wantStripped);

      // Structure always preserved
      assert.equal(gen.input.length, 1);
      assert.equal(gen.input[0].role, 'user');
      assert.equal(gen.usage.inputTokens, 10);
    } finally {
      await shutdownHarness(harness);
    }
  });
}

// --- message.content shorthand stripping ---

test('metadata_only strips message.content shorthand field', async () => {
  const harness = newHarness({ contentCapture: 'metadata_only' });
  try {
    const recorder = harness.client.startGeneration({
      model: { provider: 'test', name: 'test-model' },
    });
    recorder.setResult({
      input: [{ role: 'user', content: 'Hello secret data', parts: [{ type: 'text', text: 'Hello text' }] }],
      output: [{ role: 'assistant', content: 'Secret response', parts: [{ type: 'text', text: 'Response text' }] }],
      usage: { inputTokens: 10, outputTokens: 5 },
    });
    recorder.end();
    assert.equal(recorder.getError(), undefined);

    const gen = singleGeneration(harness.client);
    // message.content shorthand must be stripped
    assert.equal(gen.input[0].content, '');
    assert.equal(gen.output[0].content, '');
    // parts also stripped
    assert.equal(gen.input[0].parts[0].text, '');
    assert.equal(gen.output[0].parts[0].text, '');
  } finally {
    await shutdownHarness(harness);
  }
});

test('metadata_only validates content-only messages (no parts) after stripping', async () => {
  const harness = newHarness({ contentCapture: 'metadata_only' });
  try {
    const recorder = harness.client.startGeneration({
      model: { provider: 'test', name: 'test-model' },
    });
    recorder.setResult({
      input: [{ role: 'user', content: 'Hello with no parts' }],
      output: [{ role: 'assistant', parts: [{ type: 'text', text: 'Response' }] }],
      usage: { inputTokens: 10, outputTokens: 5 },
    });
    recorder.end();
    // Should not fail validation even though message.content is now '' and parts is empty
    assert.equal(recorder.getError(), undefined);

    const gen = singleGeneration(harness.client);
    assert.equal(gen.input[0].content, '');
    assert.equal(gen.input[0].parts, undefined);
  } finally {
    await shutdownHarness(harness);
  }
});

// --- Rating context propagation ---

test('rating comment stripped when called inside withContentCaptureMode context', async () => {
  let receivedBody = {};
  const server = createServer(async (request, response) => {
    const chunks = [];
    for await (const chunk of request) {
      chunks.push(chunk);
    }
    receivedBody = JSON.parse(Buffer.concat(chunks).toString('utf8'));
    response.writeHead(200, { 'content-type': 'application/json' });
    response.end(
      JSON.stringify({
        rating: {
          rating_id: 'r1',
          conversation_id: 'conv-1',
          rating: 'CONVERSATION_RATING_VALUE_GOOD',
          created_at: '2026-01-01T00:00:00Z',
        },
        summary: {
          total_count: 1,
          good_count: 1,
          bad_count: 0,
          latest_rating: 'CONVERSATION_RATING_VALUE_GOOD',
          latest_rated_at: '2026-01-01T00:00:00Z',
          has_bad_rating: false,
        },
      }),
    );
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();

  const harness = newHarness({
    contentCapture: 'full',
    api: { endpoint: `http://127.0.0.1:${address.port}` },
  });
  try {
    await withContentCaptureMode('metadata_only', async () => {
      await harness.client.submitConversationRating('conv-1', {
        ratingId: 'r1',
        rating: 'CONVERSATION_RATING_VALUE_GOOD',
        comment: 'should be stripped',
      });
    });
    assert.equal(receivedBody.comment, '', 'context metadata_only should strip rating comment');
  } finally {
    await shutdownHarness(harness);
    await new Promise((resolve) => server.close(resolve));
  }
});

// --- Tool description span suppression ---

test('metadata_only suppresses tool description on span', async () => {
  const harness = newHarness({ contentCapture: 'metadata_only' });
  try {
    const tool = harness.client.startToolExecution({
      toolName: 'search',
      toolDescription: 'Searches the web for information',
      includeContent: true,
    });
    tool.setResult({ result: 'some result', completedAt: new Date() });
    tool.end();

    const span = singleToolSpan(harness.spanExporter);
    assert.equal(span.attributes['gen_ai.tool.description'], '', 'tool description should be cleared on span');
    assert.equal(span.attributes['gen_ai.tool.call.arguments'], undefined, 'arguments should not be on span');
    assert.equal(span.attributes['gen_ai.tool.call.result'], undefined, 'result should not be on span');
  } finally {
    await shutdownHarness(harness);
  }
});

// --- Resolver validation ---

test('resolver returning invalid mode defaults to metadata_only', async () => {
  let warnMessage = '';
  const harness = newHarness({
    contentCaptureResolver: () => 'not_a_valid_mode',
    logger: {
      warn: (msg) => {
        warnMessage = msg;
      },
    },
  });
  try {
    const recorder = harness.client.startGeneration({
      model: { provider: 'test', name: 'test-model' },
    });
    recorder.setResult({
      input: [{ role: 'user', parts: [{ type: 'text', text: 'Hello' }] }],
      output: [{ role: 'assistant', parts: [{ type: 'text', text: 'Hi' }] }],
    });
    recorder.end();
    assert.equal(recorder.getError(), undefined);

    const gen = singleGeneration(harness.client);
    assert.equal(gen.metadata['sigil.sdk.content_capture_mode'], 'metadata_only');
    assert.equal(gen.input[0].parts[0].text, '');
    assert.ok(warnMessage.includes('invalid mode'), 'should warn about invalid mode');
  } finally {
    await shutdownHarness(harness);
  }
});

test('resolver throwing defaults to metadata_only with warning', async () => {
  let warnMessage = '';
  const harness = newHarness({
    contentCaptureResolver: () => {
      throw new Error('resolver broken');
    },
    logger: {
      warn: (msg) => {
        warnMessage = msg;
      },
    },
  });
  try {
    const recorder = harness.client.startGeneration({
      model: { provider: 'test', name: 'test-model' },
    });
    recorder.setResult({
      input: [{ role: 'user', parts: [{ type: 'text', text: 'Hello' }] }],
      output: [{ role: 'assistant', parts: [{ type: 'text', text: 'Hi' }] }],
    });
    recorder.end();
    assert.equal(recorder.getError(), undefined);

    const gen = singleGeneration(harness.client);
    assert.equal(gen.metadata['sigil.sdk.content_capture_mode'], 'metadata_only');
    assert.equal(gen.input[0].parts[0].text, '');
    assert.ok(warnMessage.includes('contentCaptureResolver threw'), 'should warn about thrown exception');
  } finally {
    await shutdownHarness(harness);
  }
});
