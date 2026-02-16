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

test('generation result fields override seed and update span operation name', async () => {
  const harness = newHarness();

  try {
    const recorder = harness.client.startGeneration({
      conversationId: 'conv-seed',
      agentName: 'agent-seed',
      agentVersion: 'v-seed',
      maxTokens: 512,
      temperature: 0.7,
      topP: 0.9,
      toolChoice: 'auto',
      thinkingEnabled: true,
      model: { provider: 'openai', name: 'gpt-5' },
    });
    recorder.setResult({
      conversationId: 'conv-result',
      agentName: 'agent-result',
      agentVersion: 'v-result',
      operationName: 'text_completion',
      maxTokens: 256,
      temperature: 0.2,
      topP: 0.85,
      toolChoice: 'required',
      thinkingEnabled: false,
      metadata: {
        'sigil.gen_ai.request.thinking.budget_tokens': 4096,
        'sigil.sdk.name': 'user-value',
      },
      stopReason: 'end_turn',
      output: [{ role: 'assistant', content: 'ok' }],
    });
    recorder.end();
    assert.equal(recorder.getError(), undefined);

    const generation = singleGeneration(harness.client);
    assert.equal(generation.conversationId, 'conv-result');
    assert.equal(generation.agentName, 'agent-result');
    assert.equal(generation.agentVersion, 'v-result');
    assert.equal(generation.operationName, 'text_completion');
    assert.equal(generation.maxTokens, 256);
    assert.equal(generation.temperature, 0.2);
    assert.equal(generation.topP, 0.85);
    assert.equal(generation.toolChoice, 'required');
    assert.equal(generation.thinkingEnabled, false);
    assert.equal(generation.metadata?.['sigil.sdk.name'], 'sdk-js');

    const span = singleGenerationSpan(harness.spanExporter);
    assert.equal(span.name, 'text_completion gpt-5');
    assert.equal(span.attributes['gen_ai.operation.name'], 'text_completion');
    assert.equal(span.attributes['gen_ai.conversation.id'], 'conv-result');
    assert.equal(span.attributes['gen_ai.agent.name'], 'agent-result');
    assert.equal(span.attributes['gen_ai.agent.version'], 'v-result');
    assert.equal(span.attributes['gen_ai.request.max_tokens'], 256);
    assert.equal(span.attributes['gen_ai.request.temperature'], 0.2);
    assert.equal(span.attributes['gen_ai.request.top_p'], 0.85);
    assert.equal(span.attributes['sigil.gen_ai.request.tool_choice'], 'required');
    assert.equal(span.attributes['sigil.gen_ai.request.thinking.enabled'], false);
    assert.equal(span.attributes['sigil.gen_ai.request.thinking.budget_tokens'], 4096);
    assert.equal(span.attributes['sigil.sdk.name'], 'sdk-js');
    assert.deepEqual(span.attributes['gen_ai.response.finish_reasons'], ['end_turn']);
  } finally {
    await shutdownHarness(harness);
  }
});

test('generation callError sets metadata and provider_call_error span status', async () => {
  const harness = newHarness();

  try {
    const recorder = harness.client.startGeneration({
      model: { provider: 'anthropic', name: 'claude-sonnet-4-5' },
    });
    recorder.setCallError(new Error('provider unavailable'));
    recorder.end();
    assert.equal(recorder.getError(), undefined);

    const generation = singleGeneration(harness.client);
    assert.equal(generation.callError, 'provider unavailable');
    assert.equal(generation.metadata?.call_error, 'provider unavailable');
    assert.equal(generation.metadata?.['sigil.sdk.name'], 'sdk-js');

    const span = singleGenerationSpan(harness.spanExporter);
    assert.equal(span.status.code, SpanStatusCode.ERROR);
    assert.equal(span.attributes['error.type'], 'provider_call_error');
    assert.equal(span.attributes['sigil.sdk.name'], 'sdk-js');
  } finally {
    await shutdownHarness(harness);
  }
});

test('tool execution includeContent controls argument/result attributes', async () => {
  const harness = newHarness();

  try {
    const withContent = harness.client.startToolExecution({
      toolName: 'weather',
      includeContent: true,
      conversationId: 'conv-tool',
      agentName: 'agent-tool',
      agentVersion: 'v-tool',
    });
    withContent.setResult({
      arguments: { city: 'Paris' },
      result: { temp_c: 18 },
    });
    withContent.end();
    assert.equal(withContent.getError(), undefined);

    const withoutContent = harness.client.startToolExecution({
      toolName: 'weather',
    });
    withoutContent.setResult({
      arguments: { city: 'Paris' },
      result: { temp_c: 18 },
    });
    withoutContent.end();
    assert.equal(withoutContent.getError(), undefined);

    const spans = toolSpans(harness.spanExporter);
    assert.equal(spans.length, 2);

    const contentSpan = spans.find((span) => span.attributes['gen_ai.tool.call.arguments'] !== undefined);
    const noContentSpan = spans.find((span) => span.attributes['gen_ai.tool.call.arguments'] === undefined);

    assert.ok(contentSpan);
    assert.ok(noContentSpan);
    assert.equal(contentSpan.attributes['gen_ai.operation.name'], 'execute_tool');
    assert.equal(contentSpan.attributes['gen_ai.tool.name'], 'weather');
    assert.equal(contentSpan.attributes['gen_ai.conversation.id'], 'conv-tool');
    assert.equal(contentSpan.attributes['gen_ai.agent.name'], 'agent-tool');
    assert.equal(contentSpan.attributes['gen_ai.agent.version'], 'v-tool');
    assert.equal(contentSpan.attributes['sigil.sdk.name'], 'sdk-js');
    assert.equal(noContentSpan.attributes['gen_ai.tool.call.arguments'], undefined);
    assert.equal(noContentSpan.attributes['gen_ai.tool.call.result'], undefined);
    assert.equal(noContentSpan.attributes['sigil.sdk.name'], 'sdk-js');
  } finally {
    await shutdownHarness(harness);
  }
});

test('tool execution callError is surfaced locally and marks error span', async () => {
  const harness = newHarness();

  try {
    const recorder = harness.client.startToolExecution({
      toolName: 'weather',
    });
    recorder.setCallError(new Error('tool failed'));
    recorder.end();

    assert.equal(recorder.getError()?.message, 'tool failed');
    const span = singleToolSpan(harness.spanExporter);
    assert.equal(span.status.code, SpanStatusCode.ERROR);
    assert.equal(span.attributes['error.type'], 'tool_execution_error');
  } finally {
    await shutdownHarness(harness);
  }
});

test('generation and tool recorders are idempotent on duplicate end()', async () => {
  const harness = newHarness();

  try {
    const generationRecorder = harness.client.startGeneration({
      model: { provider: 'openai', name: 'gpt-5' },
    });
    generationRecorder.end();
    generationRecorder.end();
    assert.equal(generationRecorder.getError(), undefined);

    const toolRecorder = harness.client.startToolExecution({
      toolName: 'weather',
    });
    toolRecorder.end();
    toolRecorder.end();
    assert.equal(toolRecorder.getError(), undefined);

    assert.equal(generationSpans(harness.spanExporter).length, 1);
    assert.equal(toolSpans(harness.spanExporter).length, 1);
  } finally {
    await shutdownHarness(harness);
  }
});

test('empty tool name returns no-op tool recorder', async () => {
  const harness = newHarness();

  try {
    const recorder = harness.client.startToolExecution({
      toolName: '',
    });
    recorder.setResult({
      arguments: { city: 'Paris' },
      result: { temp_c: 18 },
    });
    recorder.setCallError(new Error('ignored'));
    recorder.end();

    assert.equal(recorder.getError(), undefined);
    assert.equal(toolSpans(harness.spanExporter).length, 0);
  } finally {
    await shutdownHarness(harness);
  }
});

function newHarness() {
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
  return spanExporter.getFinishedSpans().filter((span) => span.attributes['gen_ai.operation.name'] !== 'execute_tool');
}

function toolSpans(spanExporter) {
  return spanExporter.getFinishedSpans().filter((span) => span.attributes['gen_ai.operation.name'] === 'execute_tool');
}

function singleGenerationSpan(spanExporter) {
  const spans = generationSpans(spanExporter);
  assert.equal(spans.length, 1);
  return spans[0];
}

function singleToolSpan(spanExporter) {
  const spans = toolSpans(spanExporter);
  assert.equal(spans.length, 1);
  return spans[0];
}
