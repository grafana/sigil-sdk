import assert from 'node:assert/strict';
import test from 'node:test';
import { trace } from '@opentelemetry/api';
import { anthropic, defaultConfig, gemini, openai, SigilClient } from '../.test-dist/index.js';

const providerSuites = [
  {
    name: 'openai',
    provider: 'openai',
    sdk: openai,
    syncMethod: 'chatCompletion',
    streamMethod: 'chatCompletionStream',
    streamEventsKey: 'chunks',
  },
  {
    name: 'anthropic',
    provider: 'anthropic',
    sdk: anthropic,
    syncMethod: 'completion',
    streamMethod: 'completionStream',
    streamEventsKey: 'events',
  },
  {
    name: 'gemini',
    provider: 'gemini',
    sdk: gemini,
    syncMethod: 'completion',
    streamMethod: 'completionStream',
    streamEventsKey: 'events',
  },
];

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

test('provider sync wrappers set SYNC mode, provider name, and default raw artifacts OFF', async () => {
  for (const suite of providerSuites) {
    const generation = await captureSingleGeneration(async (client) => {
      await suite.sdk[suite.syncMethod](
        client,
        {
          model: `${suite.name}-model`,
          systemPrompt: `${suite.name}-system`,
          messages: [
            { role: 'system', content: 'system-message' },
            { role: 'user', content: `hello-${suite.name}` },
          ],
        },
        async () => ({
          id: `resp-${suite.name}`,
          outputText: `output-${suite.name}`,
        })
      );
    });

    assert.equal(generation.mode, 'SYNC');
    assert.equal(generation.model.provider, suite.provider);
    assert.equal(generation.model.name, `${suite.name}-model`);
    assert.equal(generation.artifacts, undefined);
  }
});

test('provider stream wrappers set STREAM mode and include raw artifacts only on opt-in', async () => {
  for (const suite of providerSuites) {
    const generation = await captureSingleGeneration(async (client) => {
      await suite.sdk[suite.streamMethod](
        client,
        {
          model: `${suite.name}-model`,
          messages: [{ role: 'user', content: `stream-${suite.name}` }],
        },
        async () => ({
          outputText: `stream-output-${suite.name}`,
          [suite.streamEventsKey]: [{ index: 1 }],
        }),
        { rawArtifacts: true }
      );
    });

    assert.equal(generation.mode, 'STREAM');
    assert.equal(generation.model.provider, suite.provider);
    assert.ok(Array.isArray(generation.artifacts));
    assert.deepEqual(
      generation.artifacts.map((artifact) => artifact.type),
      ['request', 'provider_event']
    );
  }
});

test('provider wrappers propagate provider errors and persist callError', async () => {
  for (const suite of providerSuites) {
    const exporter = new CapturingExporter();
    const client = newClient(exporter);

    try {
      await assert.rejects(
        suite.sdk[suite.syncMethod](
          client,
          {
            model: `${suite.name}-model`,
            messages: [{ role: 'user', content: 'hello' }],
          },
          async () => {
            throw new Error(`provider failure ${suite.name}`);
          }
        ),
        new RegExp(`provider failure ${suite.name}`)
      );

      await client.flush();
      const generation = firstGeneration(exporter);
      assert.equal(generation.model.provider, suite.provider);
      assert.equal(generation.callError, `provider failure ${suite.name}`);
      assert.equal(generation.output, undefined);
    } finally {
      await client.shutdown();
    }
  }
});

test('openai mapper filters system messages, preserves tool role, and respects raw artifact policy', () => {
  const request = {
    model: 'gpt-5',
    systemPrompt: 'You are concise',
    messages: [
      { role: 'system', content: 'system-message' },
      { role: 'user', content: 'hello' },
      { role: 'tool', content: '{"ok":true}', name: 'tool-weather' },
    ],
    tools: [
      {
        name: 'weather',
        description: 'lookup weather',
        type: 'function',
      },
    ],
  };
  const response = {
    id: 'resp-openai',
    outputText: 'world',
    stopReason: 'stop',
  };

  const mappedDefault = openai.fromRequestResponse(request, response);
  assert.equal(mappedDefault.responseModel, 'gpt-5');
  assert.equal(mappedDefault.input.length, 2);
  assert.equal(mappedDefault.input[0].role, 'user');
  assert.equal(mappedDefault.input[1].role, 'tool');
  assert.equal(mappedDefault.artifacts, undefined);

  const mappedWithArtifacts = openai.fromRequestResponse(request, response, { rawArtifacts: true });
  assert.equal(mappedWithArtifacts.artifacts.length, 2);
  assert.deepEqual(
    mappedWithArtifacts.artifacts.map((artifact) => artifact.type),
    ['request', 'response']
  );
});

test('provider stream mappers fall back to request model and map provider_event artifacts', () => {
  for (const suite of providerSuites) {
    const request = {
      model: `${suite.name}-model`,
      messages: [{ role: 'user', content: 'hello' }],
    };
    const summary =
      suite.streamEventsKey === 'chunks'
        ? { outputText: `stream-output-${suite.name}`, chunks: [{ token: 'x' }] }
        : { outputText: `stream-output-${suite.name}`, events: [{ type: 'delta' }] };

    const mapped = suite.sdk.fromStream(request, summary, { rawArtifacts: true });
    assert.equal(mapped.responseModel, `${suite.name}-model`);
    assert.equal(mapped.output.length, 1);
    assert.equal(mapped.output[0].content, `stream-output-${suite.name}`);
    assert.deepEqual(
      mapped.artifacts.map((artifact) => artifact.type),
      ['request', 'provider_event']
    );
  }
});

async function captureSingleGeneration(run) {
  const exporter = new CapturingExporter();
  const client = newClient(exporter);

  try {
    await run(client);
    await client.flush();
    return firstGeneration(exporter);
  } finally {
    await client.shutdown();
  }
}

function firstGeneration(exporter) {
  assert.equal(exporter.requests.length, 1);
  assert.equal(exporter.requests[0].generations.length, 1);
  return exporter.requests[0].generations[0];
}

function newClient(generationExporter) {
  const defaults = defaultConfig();
  return new SigilClient({
    tracer: trace.getTracer('sigil-sdk-js-test'),
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
}
