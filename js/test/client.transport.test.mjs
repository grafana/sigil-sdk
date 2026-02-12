import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';
import * as grpc from '@grpc/grpc-js';
import * as protoLoader from '@grpc/proto-loader';
import { trace } from '@opentelemetry/api';
import { defaultConfig, SigilClient } from '../.test-dist/index.js';

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

test('HTTP transport roundtrip preserves full generation payload shape', async () => {
  const receivedGenerations = [];

  const server = createServer(async (request, response) => {
    const chunks = [];
    for await (const chunk of request) {
      chunks.push(chunk);
    }

    const payload = JSON.parse(Buffer.concat(chunks).toString('utf8'));
    for (const generation of payload.generations ?? []) {
      receivedGenerations.push(generation);
    }

    response.writeHead(202, { 'content-type': 'application/json' });
    response.end(
      JSON.stringify({
        results: (payload.generations ?? []).map((generation) => ({
          generationId: generation.id,
          accepted: true,
        })),
      })
    );
  });

  await listen(server);
  const address = server.address();
  if (address === null || typeof address === 'string') {
    throw new Error('failed to resolve transport test server address');
  }

  const defaults = defaultConfig();
  const client = new SigilClient({
    tracer: trace.getTracer('sigil-sdk-js-test'),
    generationExport: {
      ...defaults.generationExport,
      protocol: 'http',
      endpoint: `http://127.0.0.1:${address.port}/api/v1/generations:export`,
      batchSize: 1,
      flushIntervalMs: 60_000,
      maxRetries: 1,
      initialBackoffMs: 1,
      maxBackoffMs: 1,
    },
  });

  const totalSeeds = 20;
  try {
    for (let seed = 1; seed <= totalSeeds; seed++) {
      const { start, result } = payloadFromSeed(seed);
      const recorder = start.mode === 'STREAM' ? client.startStreamingGeneration(start) : client.startGeneration(start);
      recorder.setResult(result);
      if (seed % 3 === 0) {
        recorder.setCallError(new Error(`provider_error_${seed}`));
      }
      recorder.end();

      assert.equal(recorder.getError(), undefined);
    }

    await waitFor(() => receivedGenerations.length === totalSeeds, 2_000);

    const expectedGenerations = client.debugSnapshot().generations.map((generation) =>
      JSON.parse(JSON.stringify(generation))
    );
    assert.deepEqual(receivedGenerations, expectedGenerations);
  } finally {
    await client.shutdown();
    await close(server);
  }
});

test('gRPC transport roundtrip preserves full generation payload shape', async () => {
  const receivedGenerations = [];
  const grpcServer = await startGRPCServer((request) => {
    for (const generation of request.generations ?? []) {
      receivedGenerations.push(generation);
    }
  });

  const defaults = defaultConfig();
  const client = new SigilClient({
    tracer: trace.getTracer('sigil-sdk-js-test'),
    generationExport: {
      ...defaults.generationExport,
      protocol: 'grpc',
      endpoint: `127.0.0.1:${grpcServer.port}`,
      insecure: true,
      batchSize: 1,
      flushIntervalMs: 60_000,
      maxRetries: 1,
      initialBackoffMs: 1,
      maxBackoffMs: 1,
    },
  });

  try {
    const seedStart = 11;
    const seedEnd = 30;
    for (let seed = seedStart; seed <= seedEnd; seed++) {
      const { start, result } = payloadFromSeed(seed);
      const recorder = start.mode === 'STREAM' ? client.startStreamingGeneration(start) : client.startGeneration(start);
      recorder.setResult(result);
      if (seed % 3 === 0) {
        recorder.setCallError(new Error(`provider_error_${seed}`));
      }
      recorder.end();
      assert.equal(recorder.getError(), undefined);
    }

    const totalSeeds = seedEnd - seedStart + 1;
    await waitFor(() => receivedGenerations.length === totalSeeds, 2_000);

    const expectedGenerations = client.debugSnapshot().generations.map(canonicalizeSDKGeneration);
    const actualGenerations = receivedGenerations.map(canonicalizeProtoGeneration);
    assert.deepEqual(actualGenerations, expectedGenerations);
  } finally {
    await client.shutdown();
    await stopGRPCServer(grpcServer.server);
  }
});

test('gRPC transport maps typed message parts to proto payloads', async () => {
  const receivedGenerations = [];
  const grpcServer = await startGRPCServer((request) => {
    for (const generation of request.generations ?? []) {
      receivedGenerations.push(generation);
    }
  });

  const defaults = defaultConfig();
  const client = new SigilClient({
    tracer: trace.getTracer('sigil-sdk-js-test'),
    generationExport: {
      ...defaults.generationExport,
      protocol: 'grpc',
      endpoint: `127.0.0.1:${grpcServer.port}`,
      insecure: true,
      batchSize: 1,
      flushIntervalMs: 60_000,
      maxRetries: 1,
      initialBackoffMs: 1,
      maxBackoffMs: 1,
    },
  });

  try {
    const recorder = client.startGeneration({
      id: 'gen-parts',
      model: { provider: 'openai', name: 'gpt-5' },
    });
    recorder.setResult({
      input: [
        {
          role: 'assistant',
          parts: [
            {
              type: 'thinking',
              thinking: 'deliberation',
              metadata: { providerType: 'reasoning' },
            },
            {
              type: 'tool_call',
              toolCall: {
                id: 'tool-call-1',
                name: 'weather',
                inputJSON: '{"city":"paris"}',
              },
              metadata: { providerType: 'tool_call' },
            },
          ],
        },
      ],
      output: [
        {
          role: 'tool',
          parts: [
            {
              type: 'tool_result',
              toolResult: {
                toolCallId: 'tool-call-1',
                name: 'weather',
                content: 'sunny',
                contentJSON: '{"temp_c":22}',
                isError: false,
              },
              metadata: { providerType: 'tool_result' },
            },
          ],
        },
      ],
    });
    recorder.end();
    assert.equal(recorder.getError(), undefined);

    await waitFor(() => receivedGenerations.length === 1, 2_000);
    const generation = receivedGenerations[0];
    const inputParts = generation.input[0].parts;
    const outputParts = generation.output[0].parts;

    assert.equal(inputParts[0].thinking, 'deliberation');
    assert.equal(inputParts[0].metadata.providerType, 'reasoning');

    assert.equal(inputParts[1].toolCall.id, 'tool-call-1');
    assert.equal(inputParts[1].toolCall.name, 'weather');
    assert.equal(asUTF8String(inputParts[1].toolCall.inputJson), '{"city":"paris"}');
    assert.equal(inputParts[1].metadata.providerType, 'tool_call');

    assert.equal(outputParts[0].toolResult.toolCallId, 'tool-call-1');
    assert.equal(outputParts[0].toolResult.name, 'weather');
    assert.equal(outputParts[0].toolResult.content, 'sunny');
    assert.equal(asUTF8String(outputParts[0].toolResult.contentJson), '{"temp_c":22}');
    assert.equal(outputParts[0].toolResult.isError, false);
    assert.equal(outputParts[0].metadata.providerType, 'tool_result');
  } finally {
    await client.shutdown();
    await stopGRPCServer(grpcServer.server);
  }
});

function payloadFromSeed(seed) {
  const startedAt = new Date(Date.UTC(2026, 1, 12, 10, seed, 0));
  const completedAt = new Date(startedAt.getTime() + 250);
  const mode = seed % 2 === 0 ? 'STREAM' : 'SYNC';

  return {
    start: {
      id: `gen-${seed}`,
      conversationId: `conv-${seed}`,
      agentName: `agent-${seed}`,
      agentVersion: `v-${seed}`,
      mode,
      operationName: mode === 'STREAM' ? 'streamText' : 'generateText',
      model: {
        provider: 'openai',
        name: `gpt-5-${seed}`,
      },
      systemPrompt: `system-${seed}`,
      tools: [
        {
          name: `tool-${seed}`,
          description: `description-${seed}`,
          type: 'function',
          inputSchemaJSON: JSON.stringify({
            type: 'object',
            properties: {
              seed: { type: 'number' },
            },
          }),
        },
      ],
      tags: {
        env: 'test',
        seed: String(seed),
      },
      metadata: {
        seed,
        nested: {
          seedSquared: seed * seed,
        },
      },
      startedAt,
    },
    result: {
      responseId: `resp-${seed}`,
      responseModel: `gpt-5-${seed}`,
      input: [
        {
          role: 'user',
          content: `hello-${seed}`,
          name: 'user',
        },
      ],
      output: [
        {
          role: 'assistant',
          content: `world-${seed}`,
          name: 'assistant',
        },
      ],
      tools: [
        {
          name: `tool-${seed}`,
          description: `description-${seed}`,
          type: 'function',
          inputSchemaJSON: JSON.stringify({
            type: 'object',
            properties: {
              seed: { type: 'number' },
            },
          }),
        },
      ],
      usage: {
        inputTokens: seed * 10,
        outputTokens: seed * 20,
        totalTokens: seed * 30,
        cacheReadInputTokens: seed,
        cacheWriteInputTokens: seed + 1,
        reasoningTokens: seed + 2,
      },
      stopReason: 'stop',
      completedAt,
      tags: {
        stage: 'transport',
        seed: String(seed),
      },
      metadata: {
        source: 'transport-test',
        seed,
        nested: {
          seedPlusOne: seed + 1,
        },
      },
      artifacts: [
        {
          type: 'request',
          name: 'provider.request',
          payload: `payload-${seed}`,
          mimeType: 'application/json',
          recordId: `record-${seed}`,
          uri: `sigil://artifact/${seed}`,
        },
      ],
    },
  };
}

function canonicalizeSDKGeneration(generation) {
  const usage = generation.usage ?? {};
  const inputTokens = asNumber(usage.inputTokens);
  const outputTokens = asNumber(usage.outputTokens);
  return {
    id: generation.id,
    conversationId: generation.conversationId ?? '',
    agentName: generation.agentName ?? '',
    agentVersion: generation.agentVersion ?? '',
    mode: generation.mode,
    operationName: generation.operationName,
    traceId: generation.traceId ?? '',
    spanId: generation.spanId ?? '',
    model: {
      provider: generation.model.provider,
      name: generation.model.name,
    },
    responseId: generation.responseId ?? '',
    responseModel: generation.responseModel ?? '',
    systemPrompt: generation.systemPrompt ?? '',
    input: (generation.input ?? []).map(canonicalizeSDKMessage),
    output: (generation.output ?? []).map(canonicalizeSDKMessage),
    tools: (generation.tools ?? []).map((tool) => ({
      name: tool.name,
      description: tool.description ?? '',
      type: tool.type ?? '',
      inputSchemaJSON: tool.inputSchemaJSON ?? '',
    })),
    usage: {
      inputTokens,
      outputTokens,
      totalTokens: usage.totalTokens !== undefined ? asNumber(usage.totalTokens) : inputTokens + outputTokens,
      cacheReadInputTokens: asNumber(usage.cacheReadInputTokens),
      cacheWriteInputTokens: asNumber(usage.cacheWriteInputTokens),
      reasoningTokens: asNumber(usage.reasoningTokens),
    },
    stopReason: generation.stopReason ?? '',
    startedAt: new Date(generation.startedAt).toISOString(),
    completedAt: new Date(generation.completedAt).toISOString(),
    tags: generation.tags ?? {},
    metadata: generation.metadata ?? {},
    artifacts: (generation.artifacts ?? []).map((artifact) => ({
      type: artifact.type,
      name: artifact.name ?? artifact.type,
      payload: artifact.payload ?? '',
      mimeType: artifact.mimeType ?? 'application/json',
      recordId: artifact.recordId ?? '',
      uri: artifact.uri ?? '',
    })),
    callError: generation.callError ?? '',
  };
}

function canonicalizeSDKMessage(message) {
  return {
    role: normalizeSDKRole(message.role),
    name: message.name ?? '',
    content: message.content,
  };
}

function canonicalizeProtoGeneration(generation) {
  const usage = generation.usage ?? {};
  return {
    id: generation.id ?? '',
    conversationId: generation.conversationId ?? '',
    agentName: generation.agentName ?? '',
    agentVersion: generation.agentVersion ?? '',
    mode: fromProtoGenerationMode(generation.mode),
    operationName: generation.operationName ?? '',
    traceId: generation.traceId ?? '',
    spanId: generation.spanId ?? '',
    model: {
      provider: generation.model?.provider ?? '',
      name: generation.model?.name ?? '',
    },
    responseId: generation.responseId ?? '',
    responseModel: generation.responseModel ?? '',
    systemPrompt: generation.systemPrompt ?? '',
    input: (generation.input ?? []).map(canonicalizeProtoMessage),
    output: (generation.output ?? []).map(canonicalizeProtoMessage),
    tools: (generation.tools ?? []).map((tool) => ({
      name: tool.name ?? '',
      description: tool.description ?? '',
      type: tool.type ?? '',
      inputSchemaJSON: asUTF8String(tool.inputSchemaJson),
    })),
    usage: {
      inputTokens: asNumber(usage.inputTokens),
      outputTokens: asNumber(usage.outputTokens),
      totalTokens: asNumber(usage.totalTokens),
      cacheReadInputTokens: asNumber(usage.cacheReadInputTokens),
      cacheWriteInputTokens: asNumber(usage.cacheWriteInputTokens),
      reasoningTokens: asNumber(usage.reasoningTokens),
    },
    stopReason: generation.stopReason ?? '',
    startedAt: timestampToISO(generation.startedAt),
    completedAt: timestampToISO(generation.completedAt),
    tags: generation.tags ?? {},
    metadata: normalizeProtoMetadata(generation.metadata),
    artifacts: (generation.rawArtifacts ?? []).map((artifact) => ({
      type: fromProtoArtifactKind(artifact.kind),
      name: artifact.name ?? '',
      payload: asUTF8String(artifact.payload),
      mimeType: artifact.contentType ?? 'application/json',
      recordId: artifact.recordId ?? '',
      uri: artifact.uri ?? '',
    })),
    callError: generation.callError ?? '',
  };
}

function canonicalizeProtoMessage(message) {
  return {
    role: fromProtoMessageRole(message.role),
    name: message.name ?? '',
    content: (message.parts ?? [])
      .map((part) => (typeof part.text === 'string' ? part.text : ''))
      .join(''),
  };
}

function normalizeSDKRole(role) {
  const normalized = String(role ?? '').trim().toLowerCase();
  if (normalized === 'assistant' || normalized === 'tool') {
    return normalized;
  }
  return 'user';
}

function fromProtoGenerationMode(mode) {
  if (mode === 'GENERATION_MODE_STREAM') {
    return 'STREAM';
  }
  return 'SYNC';
}

function fromProtoMessageRole(role) {
  switch (role) {
    case 'MESSAGE_ROLE_ASSISTANT':
      return 'assistant';
    case 'MESSAGE_ROLE_TOOL':
      return 'tool';
    case 'MESSAGE_ROLE_USER':
    default:
      return 'user';
  }
}

function fromProtoArtifactKind(kind) {
  switch (kind) {
    case 'ARTIFACT_KIND_REQUEST':
      return 'request';
    case 'ARTIFACT_KIND_RESPONSE':
      return 'response';
    case 'ARTIFACT_KIND_TOOLS':
      return 'tools';
    case 'ARTIFACT_KIND_PROVIDER_EVENT':
      return 'provider_event';
    default:
      return 'unknown';
  }
}

function normalizeProtoMetadata(metadata) {
  if (metadata === undefined || metadata === null) {
    return {};
  }

  if (isRecord(metadata) && isRecord(metadata.fields)) {
    return decodeStructFields(metadata.fields);
  }
  if (isRecord(metadata)) {
    return metadata;
  }
  return {};
}

function decodeStructFields(fields) {
  const decoded = {};
  for (const [key, value] of Object.entries(fields)) {
    decoded[key] = decodeStructValue(value);
  }
  return decoded;
}

function decodeStructValue(value) {
  if (!isRecord(value)) {
    return null;
  }
  if ('stringValue' in value) {
    return value.stringValue;
  }
  if ('numberValue' in value) {
    return asNumber(value.numberValue);
  }
  if ('boolValue' in value) {
    return Boolean(value.boolValue);
  }
  if ('nullValue' in value) {
    return null;
  }
  if ('structValue' in value && isRecord(value.structValue) && isRecord(value.structValue.fields)) {
    return decodeStructFields(value.structValue.fields);
  }
  if ('listValue' in value && isRecord(value.listValue) && Array.isArray(value.listValue.values)) {
    return value.listValue.values.map((entry) => decodeStructValue(entry));
  }
  return null;
}

function timestampToISO(value) {
  if (!isRecord(value)) {
    return new Date(0).toISOString();
  }
  const seconds = asNumber(value.seconds);
  const nanos = asNumber(value.nanos);
  const milliseconds = seconds * 1_000 + Math.floor(nanos / 1_000_000);
  return new Date(milliseconds).toISOString();
}

function asUTF8String(value) {
  if (Buffer.isBuffer(value)) {
    return value.toString('utf8');
  }
  if (isRecord(value) && value.type === 'Buffer' && Array.isArray(value.data)) {
    return Buffer.from(value.data).toString('utf8');
  }
  if (typeof value === 'string') {
    return value;
  }
  return '';
}

function asNumber(value) {
  if (typeof value === 'number') {
    return Number.isFinite(value) ? value : 0;
  }
  if (typeof value === 'string') {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : 0;
  }
  return 0;
}

function isRecord(value) {
  return typeof value === 'object' && value !== null;
}

function waitFor(condition, timeoutMs = 1_000) {
  const deadline = Date.now() + timeoutMs;
  return new Promise((resolve, reject) => {
    const tick = () => {
      if (condition()) {
        resolve();
        return;
      }
      if (Date.now() >= deadline) {
        reject(new Error('timed out waiting for condition'));
        return;
      }
      setTimeout(tick, 5);
    };
    tick();
  });
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
      onRequest(call.request);
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

  return { server, port };
}

function stopGRPCServer(server) {
  return new Promise((resolve) => {
    server.tryShutdown(() => {
      resolve();
    });
  });
}
