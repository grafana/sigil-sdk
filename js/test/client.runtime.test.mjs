import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import test from 'node:test';
import { trace } from '@opentelemetry/api';
import { defaultConfig, SigilClient } from '../.test-dist/index.js';

class MockGenerationExporter {
  requests = [];
  attempts = 0;
  shutdownCalls = 0;

  constructor(failuresBeforeSuccess = 0) {
    this.failuresBeforeSuccess = failuresBeforeSuccess;
  }

  async exportGenerations(request) {
    this.attempts++;
    this.requests.push(structuredClone(request));

    if (this.failuresBeforeSuccess > 0) {
      this.failuresBeforeSuccess--;
      throw new Error('forced export failure');
    }

    return {
      results: request.generations.map((generation) => ({
        generationId: generation.id,
        accepted: true,
      })),
    };
  }

  async shutdown() {
    this.shutdownCalls++;
  }
}

test('flushes generation exports by batch size', async () => {
  const exporter = new MockGenerationExporter();
  const client = newClient(exporter, {
    batchSize: 2,
    flushIntervalMs: 60_000,
  });

  try {
    endWithSuccess(client.startGeneration(seedGeneration(1)), 1);
    endWithSuccess(client.startGeneration(seedGeneration(2)), 2);

    await waitFor(() => exporter.requests.length === 1);
    assert.equal(exporter.requests[0].generations.length, 2);
  } finally {
    await client.shutdown();
  }
});

test('flushes generation exports by interval', async () => {
  const exporter = new MockGenerationExporter();
  const client = newClient(exporter, {
    batchSize: 10,
    flushIntervalMs: 20,
  });

  try {
    endWithSuccess(client.startGeneration(seedGeneration(3)), 3);

    await waitFor(() => exporter.requests.length === 1);
    assert.equal(exporter.requests[0].generations.length, 1);
  } finally {
    await client.shutdown();
  }
});

test('flush retries failed exports with backoff and succeeds', async () => {
  const exporter = new MockGenerationExporter(2);
  const client = newClient(exporter, {
    batchSize: 10,
    flushIntervalMs: 60_000,
    maxRetries: 2,
    initialBackoffMs: 1,
    maxBackoffMs: 1,
  });

  try {
    endWithSuccess(client.startGeneration(seedGeneration(4)), 4);

    await client.flush();
    assert.equal(exporter.attempts, 3);
    assert.equal(exporter.requests.length, 3);
  } finally {
    await client.shutdown();
  }
});

test('shutdown flushes pending generation batch', async () => {
  const exporter = new MockGenerationExporter();
  const client = newClient(exporter, {
    batchSize: 10,
    flushIntervalMs: 60_000,
  });

  endWithSuccess(client.startGeneration(seedGeneration(5)), 5);

  await client.shutdown();

  assert.equal(exporter.requests.length, 1);
  assert.equal(exporter.requests[0].generations.length, 1);
  assert.equal(exporter.shutdownCalls, 1);
});

test('queue-full recorder local error is exposed and callback style throws', async () => {
  const exporter = new MockGenerationExporter();
  const client = newClient(exporter, {
    batchSize: 10,
    queueSize: 1,
    flushIntervalMs: 60_000,
  });

  try {
    endWithSuccess(client.startGeneration(seedGeneration(6)), 6);

    const recorder = client.startGeneration(seedGeneration(7));
    recorder.setResult({ output: [{ role: 'assistant', content: 'full' }] });
    recorder.end();

    assert.match(recorder.getError()?.message ?? '', /queue is full/);

    await assert.rejects(
      client.startGeneration(seedGeneration(8), async (callbackRecorder) => {
        callbackRecorder.setResult({ output: [{ role: 'assistant', content: 'callback' }] });
      }),
      /queue is full/
    );
  } finally {
    await client.shutdown();
  }
});

test('built-in HTTP exporter posts generation batches to configured endpoint', async () => {
  const receivedRequests = [];
  const server = createServer(async (request, response) => {
    const chunks = [];
    for await (const chunk of request) {
      chunks.push(chunk);
    }

    const payload = JSON.parse(Buffer.concat(chunks).toString('utf8'));
    receivedRequests.push(payload);

    response.writeHead(202, { 'content-type': 'application/json' });
    response.end(
      JSON.stringify({
        results: payload.generations.map((generation) => ({
          generationId: generation.id,
          accepted: true,
        })),
      })
    );
  });

  await listen(server);
  const address = server.address();
  if (address === null || typeof address === 'string') {
    throw new Error('failed to resolve test server address');
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

  try {
    endWithSuccess(client.startGeneration(seedGeneration(9)), 9);

    await waitFor(() => receivedRequests.length === 1);
    assert.equal(receivedRequests[0].generations.length, 1);
    assert.equal(receivedRequests[0].generations[0].mode, 'GENERATION_MODE_SYNC');
  } finally {
    await client.shutdown();
    await close(server);
  }
});

test('built-in none exporter records generations without sending', async () => {
  const defaults = defaultConfig();
  const client = new SigilClient({
    tracer: trace.getTracer('sigil-sdk-js-test'),
    generationExport: {
      ...defaults.generationExport,
      protocol: 'none',
      endpoint: 'http://127.0.0.1:1',
      batchSize: 1,
      flushIntervalMs: 60_000,
    },
  });

  try {
    const recorder = client.startGeneration(seedGeneration(10));
    recorder.setResult({
      output: [{ role: 'assistant', content: 'ok-10' }],
    });
    recorder.end();
    assert.equal(recorder.getError(), undefined);

    await client.flush();

    const snapshot = client.debugSnapshot();
    assert.equal(snapshot.generations.length, 1);
  } finally {
    await client.shutdown();
  }
});

function newClient(generationExporter, overrides) {
  const defaults = defaultConfig();
  return new SigilClient({
    tracer: trace.getTracer('sigil-sdk-js-test'),
    generationExport: {
      ...defaults.generationExport,
      ...overrides,
    },
    generationExporter,
  });
}

function seedGeneration(seed) {
  return {
    conversationId: `conv-${seed}`,
    model: {
      provider: 'openai',
      name: 'gpt-5',
    },
  };
}

function endWithSuccess(recorder, seed) {
  recorder.setResult({
    output: [{ role: 'assistant', content: `ok-${seed}` }],
  });
  recorder.end();
  assert.equal(recorder.getError(), undefined);
}

async function waitFor(condition, timeoutMs = 750) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (condition()) {
      return;
    }
    await sleep(5);
  }
  throw new Error('timed out waiting for condition');
}

function sleep(durationMs) {
  return new Promise((resolve) => {
    setTimeout(resolve, durationMs);
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
