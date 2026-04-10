import assert from 'node:assert/strict';
import test from 'node:test';
import { context, SpanStatusCode, trace } from '@opentelemetry/api';
import { BasicTracerProvider, InMemorySpanExporter, SimpleSpanProcessor } from '@opentelemetry/sdk-trace-base';
import { SigilLangChainHandler } from '../.test-dist/frameworks/langchain/index.js';
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

test('langchain handler records sync lifecycle with framework tags', async () => {
  const generation = await captureSingleGeneration(async (client) => {
    const handler = new SigilLangChainHandler(client, {
      agentName: 'agent-langchain',
      agentVersion: 'v1',
      extraTags: { env: 'test', 'sigil.framework.name': 'override' },
      extraMetadata: {
        seed: 7,
        'sigil.framework.run_id': 'override-run',
        'sigil.framework.thread_id': 'override-thread',
      },
    });

    await handler.handleChatModelStart(
      { name: 'ChatOpenAI' },
      [[{ type: 'human', content: 'hello' }]],
      'run-sync',
      'parent-run-sync',
      { invocation_params: { model: 'gpt-5', retry_attempt: 2 } },
      ['prod', 'blue'],
      { thread_id: 'chain-thread-42' },
    );
    await handler.handleLLMEnd(
      {
        generations: [[{ text: 'world' }]],
        llm_output: {
          model_name: 'gpt-5',
          finish_reason: 'stop',
          token_usage: {
            prompt_tokens: 10,
            completion_tokens: 5,
            total_tokens: 15,
          },
        },
      },
      'run-sync',
    );
  });

  assert.equal(generation.mode, 'SYNC');
  assert.equal(generation.model.provider, 'openai');
  assert.equal(generation.model.name, 'gpt-5');
  assert.equal(generation.tags['sigil.framework.name'], 'langchain');
  assert.equal(generation.tags['sigil.framework.source'], 'handler');
  assert.equal(generation.tags['sigil.framework.language'], 'javascript');
  assert.equal(generation.tags.env, 'test');
  assert.equal(generation.conversationId, 'chain-thread-42');
  assert.equal(generation.metadata['sigil.framework.run_id'], 'run-sync');
  assert.equal(generation.metadata['sigil.framework.thread_id'], 'chain-thread-42');
  assert.equal(generation.metadata['sigil.framework.parent_run_id'], 'parent-run-sync');
  assert.equal(generation.metadata['sigil.framework.component_name'], 'ChatOpenAI');
  assert.equal(generation.metadata['sigil.framework.run_type'], 'chat');
  assert.equal(generation.metadata['sigil.framework.retry_attempt'], 2);
  assert.deepEqual(generation.metadata['sigil.framework.tags'], ['prod', 'blue']);
  assert.equal(generation.metadata.seed, 7);
  assert.equal(generation.usage.inputTokens, 10);
  assert.equal(generation.usage.outputTokens, 5);
  assert.equal(generation.usage.totalTokens, 15);
  assert.equal(generation.stopReason, 'stop');
  assert.equal(generation.output[0].content, 'world');
});

test('langchain handler records stream mode and token fallback output', async () => {
  const generation = await captureSingleGeneration(async (client) => {
    const handler = new SigilLangChainHandler(client);

    await handler.handleLLMStart({ kwargs: { model: 'claude-sonnet-4-5' } }, ['stream this'], 'run-stream', undefined, {
      invocation_params: { model: 'claude-sonnet-4-5', stream: true },
    });
    await handler.handleLLMNewToken('hello', undefined, 'run-stream');
    await handler.handleLLMNewToken(' world', undefined, 'run-stream');
    await handler.handleLLMEnd({ llm_output: { model_name: 'claude-sonnet-4-5' } }, 'run-stream');
  });

  assert.equal(generation.mode, 'STREAM');
  assert.equal(generation.model.provider, 'anthropic');
  assert.equal(generation.output[0].content, 'hello world');
});

test('langchain handler records first token timestamp once per run', async () => {
  const defaults = defaultConfig();
  const client = new SigilClient({
    generationExport: {
      ...defaults.generationExport,
      batchSize: 10,
      flushIntervalMs: 60_000,
    },
    generationExporter: new CapturingExporter(),
  });

  try {
    const handler = new SigilLangChainHandler(client);
    await handler.handleLLMStart({ kwargs: { model: 'gpt-5' } }, ['stream this'], 'run-ttft', undefined, {
      invocation_params: { model: 'gpt-5', stream: true },
    });

    const runState = handler.runs.get('run-ttft');
    assert.ok(runState);

    let firstTokenCalls = 0;
    const originalSetFirstTokenAt = runState.recorder.setFirstTokenAt.bind(runState.recorder);
    runState.recorder.setFirstTokenAt = (timestamp) => {
      firstTokenCalls += 1;
      originalSetFirstTokenAt(timestamp);
    };

    await handler.handleLLMNewToken('hello', undefined, 'run-ttft');
    await handler.handleLLMNewToken(' world', undefined, 'run-ttft');
    await handler.handleLLMEnd({ llm_output: { model_name: 'gpt-5' } }, 'run-ttft');

    assert.equal(firstTokenCalls, 1);
  } finally {
    await client.shutdown();
  }
});

test('langchain generation span tracks active parent span and preserves export lineage', async () => {
  const spanExporter = new InMemorySpanExporter();
  const tracerProvider = new BasicTracerProvider({
    spanProcessors: [new SimpleSpanProcessor(spanExporter)],
  });
  const baseTracer = tracerProvider.getTracer('sigil-framework-test');
  let parentContext;
  const tracer = {
    startSpan(name, options, contextArg) {
      return baseTracer.startSpan(name, options, contextArg ?? parentContext);
    },
    startActiveSpan(...args) {
      return baseTracer.startActiveSpan(...args);
    },
  };
  const defaults = defaultConfig();
  const exporter = new CapturingExporter();
  const client = new SigilClient({
    generationExport: {
      ...defaults.generationExport,
      batchSize: 10,
      flushIntervalMs: 60_000,
    },
    generationExporter: exporter,
    tracer,
  });

  try {
    const handler = new SigilLangChainHandler(client);
    const parentSpan = baseTracer.startSpan('framework.request');
    parentContext = trace.setSpan(context.active(), parentSpan);
    await handler.handleChatModelStart(
      { name: 'ChatOpenAI' },
      [[{ type: 'human', content: 'hello' }]],
      'run-lineage',
      'parent-run-lineage',
      { invocation_params: { model: 'gpt-5' } },
      ['prod'],
      { thread_id: 'chain-thread-lineage-42' },
    );
    await handler.handleLLMEnd(
      {
        generations: [[{ text: 'world' }]],
        llm_output: { model_name: 'gpt-5', finish_reason: 'stop' },
      },
      'run-lineage',
    );
    parentSpan.end();

    await client.flush();
    const generation = exporter.requests[0].generations[0];
    const generationSpan = spanExporter
      .getFinishedSpans()
      .find((span) => span.attributes['gen_ai.operation.name'] === 'generateText');

    assert.ok(generationSpan);
    assert.equal(generationSpan.parentSpanContext?.spanId, parentSpan.spanContext().spanId);
    assert.equal(generationSpan.spanContext().traceId, parentSpan.spanContext().traceId);
    assert.equal(generation.traceId, generationSpan.spanContext().traceId);
    assert.equal(generation.spanId, generationSpan.spanContext().spanId);
  } finally {
    await client.shutdown();
    await tracerProvider.shutdown();
  }
});

test('langchain provider mapping covers openai anthopic gemini and fallback', async () => {
  const providers = [];

  await captureGenerations(
    async (client) => {
      const handler = new SigilLangChainHandler(client);

      await handler.handleLLMStart({}, ['x'], 'run-openai', undefined, { invocation_params: { model: 'gpt-5' } });
      await handler.handleLLMEnd({ generations: [[{ text: 'ok' }]] }, 'run-openai');

      await handler.handleLLMStart({}, ['x'], 'run-anthropic', undefined, {
        invocation_params: { model: 'claude-sonnet-4-5' },
      });
      await handler.handleLLMEnd({ generations: [[{ text: 'ok' }]] }, 'run-anthropic');

      await handler.handleLLMStart({}, ['x'], 'run-gemini', undefined, {
        invocation_params: { model: 'gemini-2.5-pro' },
      });
      await handler.handleLLMEnd({ generations: [[{ text: 'ok' }]] }, 'run-gemini');

      await handler.handleLLMStart({}, ['x'], 'run-custom', undefined, {
        invocation_params: { model: 'mistral-large' },
      });
      await handler.handleLLMEnd({ generations: [[{ text: 'ok' }]] }, 'run-custom');
    },
    (generation) => providers.push(generation.model.provider),
  );

  assert.deepEqual(providers, ['openai', 'anthropic', 'gemini', 'custom']);
});

test('langchain handler sets call_error on llm error', async () => {
  const generation = await captureSingleGeneration(async (client) => {
    const handler = new SigilLangChainHandler(client);

    await handler.handleLLMStart({}, ['x'], 'run-error', undefined, { invocation_params: { model: 'gpt-5' } });
    await handler.handleLLMError(new Error('provider unavailable'), 'run-error');
  });

  assert.match(generation.callError ?? '', /provider unavailable/);
  assert.equal(generation.tags['sigil.framework.name'], 'langchain');
});

test('langchain handler explicitly has no embedding lifecycle', async () => {
  const client = new SigilClient(defaultConfig());
  try {
    const handler = new SigilLangChainHandler(client);
    assert.equal(typeof handler.handleEmbeddingStart, 'undefined');
    assert.equal(typeof handler.handleEmbeddingEnd, 'undefined');
    assert.equal(typeof handler.handleEmbeddingError, 'undefined');
  } finally {
    await client.shutdown();
  }
});

test('langchain handler maps tool callbacks and emits chain/retriever spans', async () => {
  const spanExporter = new InMemorySpanExporter();
  const tracerProvider = new BasicTracerProvider({
    spanProcessors: [new SimpleSpanProcessor(spanExporter)],
  });
  const tracer = tracerProvider.getTracer('sigil-framework-test');

  const defaults = defaultConfig();
  const client = new SigilClient({
    generationExport: {
      ...defaults.generationExport,
      batchSize: 10,
      flushIntervalMs: 60_000,
    },
    generationExporter: new CapturingExporter(),
    tracer,
  });

  try {
    const handler = new SigilLangChainHandler(client);
    await handler.handleToolStart(
      { name: 'weather', description: 'Get weather' },
      '{"city":"Paris"}',
      'tool-run',
      'parent-run',
      ['tools'],
      { thread_id: 'chain-thread-42' },
    );
    await handler.handleToolEnd({ temp_c: 18 }, 'tool-run');

    await handler.handleChainStart(
      { name: 'PlanChain' },
      {},
      'chain-run',
      'parent-run',
      ['workflow'],
      { thread_id: 'chain-thread-42' },
      'chain',
    );
    await handler.handleChainEnd({}, 'chain-run');

    await handler.handleRetrieverStart(
      { name: 'VectorRetriever' },
      'where is my data',
      'retriever-run',
      'parent-run',
      ['retriever'],
      { thread_id: 'chain-thread-42' },
    );
    await handler.handleRetrieverError(new Error('retriever failed'), 'retriever-run');

    const spans = spanExporter.getFinishedSpans();
    const toolSpan = spans.find((span) => span.attributes['gen_ai.operation.name'] === 'execute_tool');
    const chainSpan = spans.find((span) => span.attributes['gen_ai.operation.name'] === 'framework_chain');
    const retrieverSpan = spans.find((span) => span.attributes['gen_ai.operation.name'] === 'framework_retriever');

    assert.ok(toolSpan);
    assert.equal(toolSpan.attributes['gen_ai.tool.name'], 'weather');
    assert.equal(toolSpan.attributes['gen_ai.conversation.id'], 'chain-thread-42');

    assert.ok(chainSpan);
    assert.equal(chainSpan.attributes['sigil.framework.run_type'], 'chain');
    assert.equal(chainSpan.attributes['sigil.framework.component_name'], 'PlanChain');
    assert.equal(chainSpan.attributes['sigil.framework.parent_run_id'], 'parent-run');
    assert.equal(chainSpan.status.code, SpanStatusCode.OK);

    assert.ok(retrieverSpan);
    assert.equal(retrieverSpan.attributes['sigil.framework.run_type'], 'retriever');
    assert.equal(retrieverSpan.attributes['sigil.framework.component_name'], 'VectorRetriever');
    assert.equal(retrieverSpan.attributes['error.type'], 'framework_error');
    assert.equal(retrieverSpan.status.code, SpanStatusCode.ERROR);
  } finally {
    await client.shutdown();
    await tracerProvider.shutdown();
  }
});

async function captureSingleGeneration(run) {
  const generations = [];
  await captureGenerations(run, (generation) => generations.push(generation));
  assert.equal(generations.length, 1);
  return generations[0];
}

async function captureGenerations(run, onGeneration) {
  const exporter = new CapturingExporter();
  const defaults = defaultConfig();
  const client = new SigilClient({
    generationExport: {
      ...defaults.generationExport,
      batchSize: 10,
      flushIntervalMs: 60_000,
    },
    generationExporter: exporter,
  });

  try {
    await run(client);
    await client.flush();
    for (const request of exporter.requests) {
      for (const generation of request.generations) {
        onGeneration(generation);
      }
    }
  } finally {
    await client.shutdown();
  }
}
