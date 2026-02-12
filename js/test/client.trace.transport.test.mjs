import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';
import * as grpc from '@grpc/grpc-js';
import * as protoLoader from '@grpc/proto-loader';
import { defaultConfig, SigilClient } from '../.test-dist/index.js';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);
const traceProtoPath = join(__dirname, '../proto/opentelemetry/proto/collector/trace/v1/trace_service.proto');
const traceProtoLoadOptions = {
  keepCase: false,
  longs: String,
  enums: String,
  defaults: false,
  oneofs: true,
  includeDirs: [join(__dirname, '../proto')],
};

test('trace export over HTTP includes generation span attributes', async () => {
  const receivedRequests = [];

  const server = createServer(async (request, response) => {
    const chunks = [];
    for await (const chunk of request) {
      chunks.push(chunk);
    }

    const payload = JSON.parse(Buffer.concat(chunks).toString('utf8'));
    receivedRequests.push(payload);

    response.writeHead(200, { 'content-type': 'application/json' });
    response.end('{}');
  });

  await listen(server);
  const address = server.address();
  if (address === null || typeof address === 'string') {
    throw new Error('failed to resolve trace test http server address');
  }

  const client = newClient({
    protocol: 'http',
    endpoint: `http://127.0.0.1:${address.port}/v1/traces`,
    insecure: true,
  });

  try {
    const recorder = client.startGeneration({
      id: 'gen-trace-http',
      conversationId: 'conv-trace-http',
      agentName: 'trace-agent-http',
      agentVersion: 'trace-v-http',
      model: {
        provider: 'openai',
        name: 'gpt-5',
      },
    });
    recorder.setResult({
      input: [{ role: 'user', content: 'hello' }],
      output: [{ role: 'assistant', content: 'hi' }],
    });
    recorder.end();
    assert.equal(recorder.getError(), undefined);

    const generation = singleGeneration(client);
    await client.shutdown();

    await waitFor(() => receivedRequests.length >= 1, 2_000);
    const span = findSpanByName(receivedRequests[0], 'generateText gpt-5');
    assertSpanForGeneration(span, generation);
  } finally {
    await close(server);
  }
});

test('trace export over gRPC includes generation span attributes', async () => {
  const receivedRequests = [];
  const grpcServer = await startTraceGRPCServer((request) => {
    receivedRequests.push(request);
  });

  const client = newClient({
    protocol: 'grpc',
    endpoint: `127.0.0.1:${grpcServer.port}`,
    insecure: true,
  });

  try {
    const recorder = client.startStreamingGeneration({
      id: 'gen-trace-grpc',
      conversationId: 'conv-trace-grpc',
      agentName: 'trace-agent-grpc',
      agentVersion: 'trace-v-grpc',
      model: {
        provider: 'anthropic',
        name: 'claude-sonnet-4-5',
      },
    });
    recorder.setResult({
      input: [{ role: 'user', content: 'hello' }],
      output: [{ role: 'assistant', content: 'hi' }],
    });
    recorder.end();
    assert.equal(recorder.getError(), undefined);

    const generation = singleGeneration(client);
    await client.shutdown();

    await waitFor(() => receivedRequests.length >= 1, 2_000);
    const span = findSpanByName(receivedRequests[0], 'streamText claude-sonnet-4-5');
    assertSpanForGeneration(span, generation);
  } finally {
    await stopGRPCServer(grpcServer.server);
  }
});

test('trace export over HTTP applies bearer auth header', async () => {
  const receivedHeaders = [];

  const server = createServer(async (request, response) => {
    receivedHeaders.push(Object.fromEntries(Object.entries(request.headers)));
    const chunks = [];
    for await (const chunk of request) {
      chunks.push(chunk);
    }
    const _payload = JSON.parse(Buffer.concat(chunks).toString('utf8'));

    response.writeHead(200, { 'content-type': 'application/json' });
    response.end('{}');
  });

  await listen(server);
  const address = server.address();
  if (address === null || typeof address === 'string') {
    throw new Error('failed to resolve trace test http server address');
  }

  const client = newClient({
    protocol: 'http',
    endpoint: `http://127.0.0.1:${address.port}/v1/traces`,
    insecure: true,
    auth: {
      mode: 'bearer',
      bearerToken: 'trace-secret',
    },
  });

  try {
    const recorder = client.startGeneration({
      id: 'gen-trace-http-auth',
      conversationId: 'conv-trace-http-auth',
      model: { provider: 'openai', name: 'gpt-5' },
    });
    recorder.setResult({
      output: [{ role: 'assistant', content: 'hi' }],
    });
    recorder.end();
    assert.equal(recorder.getError(), undefined);
    await client.shutdown();

    assert.equal(receivedHeaders.length, 1);
    assert.equal(receivedHeaders[0].authorization, 'Bearer trace-secret');
  } finally {
    await close(server);
  }
});

test('trace export over gRPC applies tenant metadata with explicit header override', async () => {
  const receivedMetadata = [];
  const grpcServer = await startTraceGRPCServer((_request, metadata) => {
    receivedMetadata.push(metadata);
  });

  const client = newClient({
    protocol: 'grpc',
    endpoint: `127.0.0.1:${grpcServer.port}`,
    insecure: true,
    headers: {
      'x-scope-orgid': 'override-tenant',
    },
    auth: {
      mode: 'tenant',
      tenantId: 'tenant-a',
    },
  });

  try {
    const recorder = client.startGeneration({
      id: 'gen-trace-grpc-auth',
      conversationId: 'conv-trace-grpc-auth',
      model: { provider: 'anthropic', name: 'claude-sonnet-4-5' },
    });
    recorder.setResult({
      output: [{ role: 'assistant', content: 'hi' }],
    });
    recorder.end();
    assert.equal(recorder.getError(), undefined);
    await client.shutdown();

    assert.equal(receivedMetadata.length, 1);
    assert.equal(receivedMetadata[0]['x-scope-orgid'], 'override-tenant');
  } finally {
    await stopGRPCServer(grpcServer.server);
  }
});

function singleGeneration(client) {
  const snapshot = client.debugSnapshot();
  assert.equal(snapshot.generations.length, 1);
  return snapshot.generations[0];
}

function assertSpanForGeneration(span, generation) {
  const attrs = attributeStringMap(span.attributes ?? []);

  assert.equal(attrs['sigil.generation.id'], generation.id);
  assert.equal(attrs['gen_ai.conversation.id'], generation.conversationId);
  assert.equal(attrs['gen_ai.agent.name'], generation.agentName);
  assert.equal(attrs['gen_ai.agent.version'], generation.agentVersion);
  assert.equal(attrs['gen_ai.provider.name'], generation.model.provider);
  assert.equal(attrs['gen_ai.request.model'], generation.model.name);
  assert.equal(attrs['gen_ai.operation.name'], generation.operationName);

  assert.equal(normalizeHexID(span.traceId, 32), generation.traceId);
  assert.equal(normalizeHexID(span.spanId, 16), generation.spanId);
}

function findSpanByName(request, expectedName) {
  for (const resourceSpans of request.resourceSpans ?? []) {
    for (const scopeSpans of resourceSpans.scopeSpans ?? []) {
      for (const span of scopeSpans.spans ?? []) {
        if (span.name === expectedName) {
          return span;
        }
      }
    }
  }
  throw new Error(`span ${expectedName} not found in trace export`);
}

function attributeStringMap(attributes) {
  const output = {};
  for (const attr of attributes) {
    if (!isRecord(attr) || typeof attr.key !== 'string' || !isRecord(attr.value)) {
      continue;
    }

    const value = attr.value;
    if (typeof value.stringValue === 'string') {
      output[attr.key] = value.stringValue;
    }
  }
  return output;
}

function normalizeHexID(id, expectedLength) {
  if (Buffer.isBuffer(id)) {
    const decoded = id.toString('hex').toLowerCase();
    if (decoded.length === expectedLength) {
      return decoded;
    }
    return '';
  }
  if (isRecord(id) && id.type === 'Buffer' && Array.isArray(id.data)) {
    const decoded = Buffer.from(id.data).toString('hex').toLowerCase();
    if (decoded.length === expectedLength) {
      return decoded;
    }
    return '';
  }
  if (typeof id !== 'string' || id.length === 0) {
    return '';
  }
  const lower = id.toLowerCase();
  if (/^[0-9a-f]+$/.test(lower)) {
    return lower;
  }
  const decoded = Buffer.from(id, 'base64').toString('hex').toLowerCase();
  if (decoded.length === expectedLength) {
    return decoded;
  }
  return lower;
}

function isRecord(value) {
  return typeof value === 'object' && value !== null;
}

function newClient(traceOverrides) {
  const defaults = defaultConfig();
  return new SigilClient({
    trace: {
      ...defaults.trace,
      ...traceOverrides,
    },
    generationExport: {
      ...defaults.generationExport,
      batchSize: 1,
      flushIntervalMs: 10,
      maxRetries: 1,
      initialBackoffMs: 1,
      maxBackoffMs: 1,
    },
    generationExporter: {
      async exportGenerations(request) {
        return {
          results: request.generations.map((generation) => ({
            generationId: generation.id,
            accepted: true,
          })),
        };
      },
    },
  });
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

async function startTraceGRPCServer(onRequest) {
  const packageDefinition = await protoLoader.load(traceProtoPath, traceProtoLoadOptions);
  const loaded = grpc.loadPackageDefinition(packageDefinition);
  const service = loaded.opentelemetry.proto.collector.trace.v1.TraceService;

  const server = new grpc.Server();
  server.addService(service.service, {
    Export(call, callback) {
      onRequest(call.request, call.metadata.getMap());
      callback(null, {});
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
