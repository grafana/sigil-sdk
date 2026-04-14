import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { dirname, join } from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';
import * as grpc from '@grpc/grpc-js';
import * as protoLoader from '@grpc/proto-loader';
import {
  AggregationTemporality,
  InMemoryMetricExporter,
  MeterProvider,
  PeriodicExportingMetricReader,
} from '@opentelemetry/sdk-metrics';
import { BasicTracerProvider, InMemorySpanExporter, SimpleSpanProcessor } from '@opentelemetry/sdk-trace-base';
import {
  defaultConfig,
  SigilClient,
  withAgentName,
  withAgentVersion,
  withConversationTitle,
  withUserId,
} from '../.test-dist/index.js';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);
const protoPath = join(__dirname, '../proto/sigil/v1/generation_ingest.proto');
const protoLoadOptions = {
  keepCase: false,
  longs: String,
  enums: String,
  defaults: false,
  oneofs: true,
};

test('conformance sync roundtrip semantics', async () => {
  const env = await createConformanceEnv();

  try {
    const recorder = env.client.startGeneration({
      id: 'gen-roundtrip',
      conversationId: 'conv-roundtrip',
      conversationTitle: 'Roundtrip conversation',
      userId: 'user-roundtrip',
      agentName: 'agent-roundtrip',
      agentVersion: 'v-roundtrip',
      model: { provider: 'openai', name: 'gpt-5' },
      maxTokens: 256,
      temperature: 0.2,
      topP: 0.9,
      toolChoice: 'required',
      thinkingEnabled: false,
      tools: [{ name: 'weather', description: 'Get weather', type: 'function' }],
      tags: { tenant: 'dev' },
      metadata: { trace: 'roundtrip' },
    });
    recorder.setResult({
      responseId: 'resp-roundtrip',
      responseModel: 'gpt-5-2026',
      input: [{ role: 'user', parts: [{ type: 'text', text: 'hello' }] }],
      output: [
        {
          role: 'assistant',
          parts: [
            { type: 'thinking', thinking: 'reasoning' },
            {
              type: 'tool_call',
              toolCall: {
                id: 'call-1',
                name: 'weather',
                inputJSON: '{"city":"Paris"}',
              },
            },
          ],
        },
        {
          role: 'tool',
          parts: [
            {
              type: 'tool_result',
              toolResult: {
                toolCallId: 'call-1',
                name: 'weather',
                content: 'sunny',
                contentJSON: '{"temp_c":18}',
              },
            },
          ],
        },
      ],
      usage: {
        inputTokens: 12,
        outputTokens: 7,
        totalTokens: 19,
        cacheReadInputTokens: 2,
        cacheWriteInputTokens: 1,
        cacheCreationInputTokens: 3,
        reasoningTokens: 4,
      },
      stopReason: 'stop',
      tags: { region: 'eu' },
      metadata: { result: 'ok' },
      artifacts: [
        { type: 'request', name: 'request', mimeType: 'application/json', payload: '{"prompt":"hello"}' },
        { type: 'response', name: 'response', mimeType: 'application/json', payload: '{"text":"sunny"}' },
      ],
    });
    recorder.end();
    assert.equal(recorder.getError(), undefined);

    await env.client.shutdown();
    const generation = env.singleGeneration();
    const span = env.latestGenerationSpan();
    const metricNames = await env.metricNames();

    assert.equal(generation.mode, 'GENERATION_MODE_SYNC');
    assert.equal(generation.operationName, 'generateText');
    assert.equal(generation.conversationId, 'conv-roundtrip');
    assert.equal(generation.agentName, 'agent-roundtrip');
    assert.equal(generation.agentVersion, 'v-roundtrip');
    assert.equal(generation.traceId, span.spanContext().traceId);
    assert.equal(generation.spanId, span.spanContext().spanId);
    assert.equal(generation.metadata?.fields?.['sigil.conversation.title']?.stringValue, 'Roundtrip conversation');
    assert.equal(generation.metadata?.fields?.['sigil.user.id']?.stringValue, 'user-roundtrip');
    assert.equal(generation.input?.[0]?.parts?.[0]?.text, 'hello');
    assert.equal(generation.output?.[0]?.parts?.[0]?.thinking, 'reasoning');
    assert.equal(generation.output?.[0]?.parts?.[1]?.toolCall?.name, 'weather');
    assert.equal(generation.output?.[1]?.parts?.[0]?.toolResult?.content, 'sunny');
    assert.equal(Number(generation.maxTokens), 256);
    assert.equal(generation.temperature, 0.2);
    assert.equal(generation.topP, 0.9);
    assert.equal(generation.toolChoice, 'required');
    assert.equal(generation.thinkingEnabled, false);
    assert.equal(Number(generation.usage?.inputTokens ?? 0), 12);
    assert.equal(Number(generation.usage?.outputTokens ?? 0), 7);
    assert.equal(Number(generation.usage?.totalTokens ?? 0), 19);
    assert.equal(Number(generation.usage?.cacheReadInputTokens ?? 0), 2);
    assert.equal(Number(generation.usage?.cacheWriteInputTokens ?? 0), 1);
    assert.equal(Number(generation.usage?.reasoningTokens ?? 0), 4);
    assert.equal(generation.stopReason, 'stop');
    assert.equal(generation.tags?.tenant, 'dev');
    assert.equal(generation.tags?.region, 'eu');
    assert.equal((generation.rawArtifacts ?? []).length, 2);
    assert.equal(span.attributes['gen_ai.operation.name'], 'generateText');
    assert.equal(span.attributes['sigil.conversation.title'], 'Roundtrip conversation');
    assert.equal(span.attributes['user.id'], 'user-roundtrip');
    assert.equal(span.attributes['sigil.gen_ai.tool_call_count'], 1);
    assert.ok(metricNames.includes('gen_ai.client.operation.duration'));
    assert.ok(metricNames.includes('gen_ai.client.token.usage'));
    assert.ok(!metricNames.includes('gen_ai.client.time_to_first_token'));
  } finally {
    await env.close();
  }
});

for (const testCase of [
  {
    name: 'explicit wins',
    startTitle: 'Explicit',
    contextTitle: 'Context',
    metadataTitle: 'Meta',
    expected: 'Explicit',
  },
  { name: 'context fallback', startTitle: '', contextTitle: 'Context', metadataTitle: '', expected: 'Context' },
  { name: 'metadata fallback', startTitle: '', contextTitle: '', metadataTitle: 'Meta', expected: 'Meta' },
  { name: 'whitespace trimmed', startTitle: '  Padded  ', contextTitle: '', metadataTitle: '', expected: 'Padded' },
  { name: 'whitespace omitted', startTitle: '   ', contextTitle: '', metadataTitle: '', expected: '' },
]) {
  test(`conformance conversation title semantics: ${testCase.name}`, async () => {
    const env = await createConformanceEnv();

    try {
      await runWithMaybeContext(testCase.contextTitle, withConversationTitle, async () => {
        const recorder = env.client.startGeneration({
          model: { provider: 'openai', name: 'gpt-5' },
          conversationTitle: testCase.startTitle,
          metadata:
            testCase.metadataTitle.length > 0 ? { 'sigil.conversation.title': testCase.metadataTitle } : undefined,
        });
        recorder.setResult({});
        recorder.end();
        assert.equal(recorder.getError(), undefined);
      });

      await env.client.shutdown();
      const generation = env.singleGeneration();
      const span = env.latestGenerationSpan();
      if (testCase.expected.length === 0) {
        assert.equal(generation.metadata?.fields?.['sigil.conversation.title'], undefined);
        assert.equal(span.attributes['sigil.conversation.title'], undefined);
        return;
      }

      assert.equal(generation.metadata?.fields?.['sigil.conversation.title']?.stringValue, testCase.expected);
      assert.equal(span.attributes['sigil.conversation.title'], testCase.expected);
    } finally {
      await env.close();
    }
  });
}

for (const testCase of [
  {
    name: 'explicit wins',
    startUserId: 'explicit',
    contextUserId: 'ctx',
    canonicalUserId: 'canonical',
    legacyUserId: 'legacy',
    expected: 'explicit',
  },
  {
    name: 'context fallback',
    startUserId: '',
    contextUserId: 'ctx',
    canonicalUserId: '',
    legacyUserId: '',
    expected: 'ctx',
  },
  {
    name: 'canonical metadata',
    startUserId: '',
    contextUserId: '',
    canonicalUserId: 'canonical',
    legacyUserId: '',
    expected: 'canonical',
  },
  {
    name: 'legacy metadata',
    startUserId: '',
    contextUserId: '',
    canonicalUserId: '',
    legacyUserId: 'legacy',
    expected: 'legacy',
  },
  {
    name: 'canonical beats legacy',
    startUserId: '',
    contextUserId: '',
    canonicalUserId: 'canonical',
    legacyUserId: 'legacy',
    expected: 'canonical',
  },
  {
    name: 'whitespace trimmed',
    startUserId: '  padded  ',
    contextUserId: '',
    canonicalUserId: '',
    legacyUserId: '',
    expected: 'padded',
  },
]) {
  test(`conformance user id semantics: ${testCase.name}`, async () => {
    const env = await createConformanceEnv();

    try {
      await runWithMaybeContext(testCase.contextUserId, withUserId, async () => {
        const metadata = {};
        if (testCase.canonicalUserId.length > 0) {
          metadata['sigil.user.id'] = testCase.canonicalUserId;
        }
        if (testCase.legacyUserId.length > 0) {
          metadata['user.id'] = testCase.legacyUserId;
        }

        const recorder = env.client.startGeneration({
          model: { provider: 'openai', name: 'gpt-5' },
          userId: testCase.startUserId,
          metadata,
        });
        recorder.setResult({});
        recorder.end();
        assert.equal(recorder.getError(), undefined);
      });

      await env.client.shutdown();
      const generation = env.singleGeneration();
      const span = env.latestGenerationSpan();
      assert.equal(generation.metadata?.fields?.['sigil.user.id']?.stringValue, testCase.expected);
      assert.equal(span.attributes['user.id'], testCase.expected);
    } finally {
      await env.close();
    }
  });
}

for (const testCase of [
  {
    name: 'explicit fields',
    startName: 'agent-explicit',
    startVersion: 'v1.2.3',
    contextName: '',
    contextVersion: '',
    resultName: '',
    resultVersion: '',
    expectedName: 'agent-explicit',
    expectedVersion: 'v1.2.3',
  },
  {
    name: 'context fallback',
    startName: '',
    startVersion: '',
    contextName: 'agent-context',
    contextVersion: 'v-context',
    resultName: '',
    resultVersion: '',
    expectedName: 'agent-context',
    expectedVersion: 'v-context',
  },
  {
    name: 'result override',
    startName: 'agent-seed',
    startVersion: 'v-seed',
    contextName: '',
    contextVersion: '',
    resultName: 'agent-result',
    resultVersion: 'v-result',
    expectedName: 'agent-result',
    expectedVersion: 'v-result',
  },
  {
    name: 'empty omission',
    startName: '',
    startVersion: '',
    contextName: '',
    contextVersion: '',
    resultName: '',
    resultVersion: '',
    expectedName: '',
    expectedVersion: '',
  },
]) {
  test(`conformance agent identity semantics: ${testCase.name}`, async () => {
    const env = await createConformanceEnv();

    try {
      await runWithMaybeContext(testCase.contextName, withAgentName, async () => {
        await runWithMaybeContext(testCase.contextVersion, withAgentVersion, async () => {
          const recorder = env.client.startGeneration({
            model: { provider: 'openai', name: 'gpt-5' },
            agentName: testCase.startName,
            agentVersion: testCase.startVersion,
          });
          recorder.setResult({
            agentName: testCase.resultName,
            agentVersion: testCase.resultVersion,
          });
          recorder.end();
          assert.equal(recorder.getError(), undefined);
        });
      });

      await env.client.shutdown();
      const generation = env.singleGeneration();
      const span = env.latestGenerationSpan();
      assert.equal(generation.agentName ?? '', testCase.expectedName);
      assert.equal(generation.agentVersion ?? '', testCase.expectedVersion);
      assert.equal(span.attributes['gen_ai.agent.name'], testCase.expectedName || undefined);
      assert.equal(span.attributes['gen_ai.agent.version'], testCase.expectedVersion || undefined);
    } finally {
      await env.close();
    }
  });
}

test('conformance streaming telemetry semantics', async () => {
  const env = await createConformanceEnv();

  try {
    const startedAt = new Date('2026-03-12T09:00:00Z');
    const recorder = env.client.startStreamingGeneration({
      model: { provider: 'openai', name: 'gpt-5' },
      startedAt,
    });
    recorder.setFirstTokenAt(new Date('2026-03-12T09:00:00.250Z'));
    recorder.setResult({
      output: [{ role: 'assistant', parts: [{ type: 'text', text: 'Hello world' }] }],
      usage: { inputTokens: 4, outputTokens: 3, totalTokens: 7 },
      startedAt,
      completedAt: new Date('2026-03-12T09:00:01Z'),
    });
    recorder.end();
    assert.equal(recorder.getError(), undefined);

    await env.client.shutdown();
    const generation = env.singleGeneration();
    const span = env.latestGenerationSpan();
    const metricNames = await env.metricNames();

    assert.equal(generation.mode, 'GENERATION_MODE_STREAM');
    assert.equal(generation.operationName, 'streamText');
    assert.equal(generation.output?.[0]?.parts?.[0]?.text, 'Hello world');
    assert.equal(span.name, 'streamText gpt-5');
    assert.ok(metricNames.includes('gen_ai.client.operation.duration'));
    assert.ok(metricNames.includes('gen_ai.client.time_to_first_token'));
  } finally {
    await env.close();
  }
});

test('conformance tool execution semantics', async () => {
  const env = await createConformanceEnv();

  try {
    await runWithMaybeContext('Context title', withConversationTitle, async () => {
      await runWithMaybeContext('agent-context', withAgentName, async () => {
        await runWithMaybeContext('v-context', withAgentVersion, async () => {
          const recorder = env.client.startToolExecution({
            toolName: 'weather',
            toolCallId: 'call-weather-1',
            toolType: 'function',
            includeContent: true,
          });
          recorder.setResult({
            arguments: { city: 'Paris' },
            result: { forecast: 'sunny' },
          });
          recorder.end();
          assert.equal(recorder.getError(), undefined);
        });
      });
    });

    await env.client.shutdown();
    const span = env.latestSpanByOperation('execute_tool');
    const metricNames = await env.metricNames();

    assert.equal(env.receivedRequests.length, 0);
    assert.equal(span.name, 'execute_tool weather');
    assert.equal(span.attributes['gen_ai.operation.name'], 'execute_tool');
    assert.equal(span.attributes['gen_ai.tool.name'], 'weather');
    assert.equal(span.attributes['gen_ai.tool.call.id'], 'call-weather-1');
    assert.equal(span.attributes['gen_ai.tool.type'], 'function');
    assert.match(String(span.attributes['gen_ai.tool.call.arguments'] ?? ''), /Paris/);
    assert.match(String(span.attributes['gen_ai.tool.call.result'] ?? ''), /sunny/);
    assert.equal(span.attributes['sigil.conversation.title'], 'Context title');
    assert.equal(span.attributes['gen_ai.agent.name'], 'agent-context');
    assert.equal(span.attributes['gen_ai.agent.version'], 'v-context');
    assert.ok(metricNames.includes('gen_ai.client.operation.duration'));
    assert.ok(!metricNames.includes('gen_ai.client.time_to_first_token'));
  } finally {
    await env.close();
  }
});

test('conformance embedding semantics', async () => {
  const env = await createConformanceEnv();

  try {
    await runWithMaybeContext('agent-context', withAgentName, async () => {
      await runWithMaybeContext('v-context', withAgentVersion, async () => {
        const recorder = env.client.startEmbedding({
          model: { provider: 'openai', name: 'text-embedding-3-small' },
          dimensions: 512,
        });
        recorder.setResult({
          inputCount: 2,
          inputTokens: 8,
          inputTexts: ['hello', 'world'],
          responseModel: 'text-embedding-3-small',
          dimensions: 512,
        });
        recorder.end();
        assert.equal(recorder.getError(), undefined);
      });
    });

    await env.client.shutdown();
    const span = env.latestSpanByOperation('embeddings');
    const metricNames = await env.metricNames();

    assert.equal(env.receivedRequests.length, 0);
    assert.equal(span.name, 'embeddings text-embedding-3-small');
    assert.equal(span.attributes['gen_ai.operation.name'], 'embeddings');
    assert.equal(span.attributes['gen_ai.agent.name'], 'agent-context');
    assert.equal(span.attributes['gen_ai.agent.version'], 'v-context');
    assert.equal(span.attributes['gen_ai.embeddings.input_count'], 2);
    assert.equal(span.attributes['gen_ai.embeddings.dimension.count'], 512);
    assert.equal(span.attributes['gen_ai.response.model'], 'text-embedding-3-small');
    assert.ok(metricNames.includes('gen_ai.client.operation.duration'));
    assert.ok(metricNames.includes('gen_ai.client.token.usage'));
    assert.ok(!metricNames.includes('gen_ai.client.time_to_first_token'));
    assert.ok(!metricNames.includes('gen_ai.client.tool_calls_per_operation'));
  } finally {
    await env.close();
  }
});

test('conformance validation and provider call error semantics', async () => {
  const env = await createConformanceEnv();

  try {
    const invalid = env.client.startGeneration({
      model: { provider: 'anthropic', name: 'claude-sonnet-4-5' },
    });
    invalid.setResult({
      input: [
        {
          role: 'user',
          parts: [{ type: 'tool_call', toolCall: { name: 'weather' } }],
        },
      ],
    });
    invalid.end();

    assert.match(invalid.getError()?.message ?? '', /tool_call only allowed for assistant role/);
    assert.equal(env.receivedRequests.length, 0);
    assert.equal(env.latestGenerationSpan().attributes['error.type'], 'validation_error');

    const callError = env.client.startGeneration({
      model: { provider: 'openai', name: 'gpt-5' },
    });
    callError.setCallError(new Error('provider unavailable'));
    callError.setResult({});
    callError.end();
    assert.equal(callError.getError(), undefined);

    await env.client.shutdown();
    const generation = env.singleGeneration();
    const span = env.latestGenerationSpan();
    assert.equal(generation.callError, 'provider unavailable');
    assert.equal(generation.metadata?.fields?.call_error?.stringValue, 'provider unavailable');
    assert.equal(span.attributes['error.type'], 'provider_call_error');
  } finally {
    await env.close();
  }
});

test('conformance rating submission semantics', async () => {
  const env = await createConformanceEnv();

  try {
    const response = await env.client.submitConversationRating('conv-rating', {
      ratingId: 'rat-1',
      rating: 'CONVERSATION_RATING_VALUE_BAD',
      comment: 'wrong answer',
      metadata: { channel: 'assistant' },
    });

    assert.equal(env.ratingPath, '/api/v1/conversations/conv-rating/ratings');
    assert.deepEqual(env.ratingPayload, {
      rating_id: 'rat-1',
      rating: 'CONVERSATION_RATING_VALUE_BAD',
      comment: 'wrong answer',
      metadata: { channel: 'assistant' },
    });
    assert.equal(response.rating.conversationId, 'conv-rating');
    assert.equal(response.summary.badCount, 1);
  } finally {
    await env.close();
  }
});

test('conformance shutdown flush semantics', async () => {
  const env = await createConformanceEnv({ batchSize: 10 });

  try {
    const recorder = env.client.startGeneration({
      conversationId: 'conv-shutdown',
      agentName: 'agent-shutdown',
      agentVersion: 'v-shutdown',
      model: { provider: 'openai', name: 'gpt-5' },
    });
    recorder.setResult({});
    recorder.end();
    assert.equal(recorder.getError(), undefined);
    assert.equal(env.receivedRequests.length, 0);

    await env.client.shutdown();
    const generation = env.singleGeneration();
    assert.equal(generation.conversationId, 'conv-shutdown');
    assert.equal(generation.agentName, 'agent-shutdown');
    assert.equal(generation.agentVersion, 'v-shutdown');
  } finally {
    await env.close();
  }
});

async function createConformanceEnv(options = {}) {
  const receivedRequests = [];
  const grpcServer = await startGRPCServer((request) => {
    receivedRequests.push(request);
  });

  let ratingPath = '';
  let ratingPayload;
  const ratingServer = createServer(async (request, response) => {
    ratingPath = request.url ?? '';
    const chunks = [];
    for await (const chunk of request) {
      chunks.push(chunk);
    }
    ratingPayload = JSON.parse(Buffer.concat(chunks).toString('utf8'));
    response.writeHead(200, { 'content-type': 'application/json' });
    response.end(
      JSON.stringify({
        rating: {
          rating_id: 'rat-1',
          conversation_id: 'conv-rating',
          rating: 'CONVERSATION_RATING_VALUE_BAD',
          created_at: '2026-03-12T09:00:00Z',
        },
        summary: {
          total_count: 1,
          good_count: 0,
          bad_count: 1,
          latest_rating: 'CONVERSATION_RATING_VALUE_BAD',
          latest_rated_at: '2026-03-12T09:00:00Z',
          has_bad_rating: true,
        },
      }),
    );
  });
  await listen(ratingServer);
  const ratingAddress = ratingServer.address();
  if (ratingAddress === null || typeof ratingAddress === 'string') {
    throw new Error('failed to resolve rating server address');
  }

  const spanExporter = new InMemorySpanExporter();
  const tracerProvider = new BasicTracerProvider({
    spanProcessors: [new SimpleSpanProcessor(spanExporter)],
  });
  const metricExporter = new InMemoryMetricExporter(AggregationTemporality.CUMULATIVE);
  const metricReader = new PeriodicExportingMetricReader({
    exporter: metricExporter,
    exportIntervalMillis: 60_000,
  });
  const meterProvider = new MeterProvider({
    readers: [metricReader],
  });

  const defaults = defaultConfig();
  const client = new SigilClient({
    tracer: tracerProvider.getTracer('sigil-conformance-test'),
    meter: meterProvider.getMeter('sigil-conformance-test'),
    generationExport: {
      ...defaults.generationExport,
      protocol: 'grpc',
      endpoint: `127.0.0.1:${grpcServer.port}`,
      insecure: true,
      batchSize: options.batchSize ?? 1,
      queueSize: 10,
      flushIntervalMs: 60 * 60 * 1_000,
      maxRetries: 1,
      initialBackoffMs: 1,
      maxBackoffMs: 2,
    },
    api: {
      endpoint: `http://127.0.0.1:${ratingAddress.port}`,
    },
  });

  let closed = false;
  return {
    client,
    receivedRequests,
    get ratingPath() {
      return ratingPath;
    },
    get ratingPayload() {
      return ratingPayload;
    },
    singleGeneration() {
      assert.equal(receivedRequests.length, 1);
      assert.equal(receivedRequests[0].generations?.length, 1);
      return receivedRequests[0].generations[0];
    },
    latestGenerationSpan() {
      const spans = spanExporter.getFinishedSpans().filter((span) => {
        const operation = span.attributes['gen_ai.operation.name'];
        return operation === 'generateText' || operation === 'streamText';
      });
      assert.ok(spans.length > 0);
      return spans.at(-1);
    },
    latestSpanByOperation(operationName) {
      const spans = spanExporter
        .getFinishedSpans()
        .filter((span) => span.attributes['gen_ai.operation.name'] === operationName);
      assert.ok(spans.length > 0);
      return spans.at(-1);
    },
    async metricNames() {
      await meterProvider.forceFlush();
      return metricExporter
        .getMetrics()
        .flatMap((resourceMetrics) => resourceMetrics.scopeMetrics)
        .flatMap((scopeMetrics) => scopeMetrics.metrics)
        .map((metric) => metric.descriptor.name);
    },
    async close() {
      if (closed) {
        return;
      }
      closed = true;
      await client.shutdown();
      await meterProvider.shutdown();
      await tracerProvider.shutdown();
      await close(ratingServer);
      await stopGRPCServer(grpcServer.server);
    },
  };
}

async function runWithMaybeContext(value, wrapper, callback) {
  if (typeof value === 'string' && value.trim().length > 0) {
    return await wrapper(value, callback);
  }
  return await callback();
}

function listen(server) {
  return new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', () => {
      server.off('error', reject);
      resolve();
    });
  });
}

function close(server) {
  return new Promise((resolve, reject) => {
    server.close((error) => {
      if (error) {
        reject(error);
        return;
      }
      resolve();
    });
  });
}

async function startGRPCServer(onRequest) {
  const packageDefinition = await protoLoader.load(protoPath, protoLoadOptions);
  const loaded = grpc.loadPackageDefinition(packageDefinition);
  const service = loaded.sigil.v1.GenerationIngestService;

  const server = new grpc.Server();
  server.addService(service.service, {
    ExportGenerations(call, callback) {
      onRequest(call.request, call.metadata.getMap());
      callback(null, {
        results: (call.request.generations ?? []).map((generation) => ({
          generationId: generation.id,
          accepted: true,
        })),
      });
    },
  });

  const port = await new Promise((resolve, reject) => {
    server.bindAsync('127.0.0.1:0', grpc.ServerCredentials.createInsecure(), (error, boundPort) => {
      if (error) {
        reject(error);
        return;
      }
      resolve(boundPort);
    });
  });

  server.start();
  return { server, port };
}

function stopGRPCServer(server) {
  return new Promise((resolve) => {
    server.tryShutdown(() => {
      resolve();
    });
  });
}
