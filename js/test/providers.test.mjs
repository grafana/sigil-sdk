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
          maxCompletionTokens: suite.name === 'openai' ? 320 : undefined,
          maxTokens: suite.name === 'anthropic' ? 320 : undefined,
          maxOutputTokens: suite.name === 'gemini' ? 320 : undefined,
          temperature: 0.2,
          topP: 0.85,
          toolChoice: suite.name === 'openai' ? { type: 'function', name: 'weather' } : undefined,
          functionCallingMode: suite.name === 'gemini' ? { mode: 'ANY' } : undefined,
          thinking: suite.name === 'anthropic' ? { type: 'adaptive', budget_tokens: 2048 } : undefined,
          thinkingConfig: suite.name === 'gemini' ? { includeThoughts: true, thinkingBudget: 1536 } : undefined,
          reasoning: suite.name === 'openai' ? { effort: 'high', max_output_tokens: 1024 } : undefined,
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
    assert.equal(generation.temperature, 0.2);
    assert.equal(generation.topP, 0.85);
    assert.equal(generation.maxTokens, 320);
    assert.equal(
      generation.metadata['sigil.gen_ai.request.thinking.budget_tokens'],
      suite.name === 'openai' ? 1024 : suite.name === 'anthropic' ? 2048 : 1536
    );
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
          maxCompletionTokens: suite.name === 'openai' ? 400 : undefined,
          maxTokens: suite.name === 'anthropic' ? 400 : undefined,
          maxOutputTokens: suite.name === 'gemini' ? 400 : undefined,
          temperature: 0.1,
          topP: 0.9,
          toolChoice: suite.name === 'openai' ? { type: 'function', name: 'weather' } : undefined,
          functionCallingMode: suite.name === 'gemini' ? { mode: 'ANY' } : undefined,
          thinking: suite.name === 'anthropic' ? { type: 'adaptive', budget_tokens: 2048 } : undefined,
          thinkingConfig: suite.name === 'gemini' ? { includeThoughts: true, thinkingBudget: 1536 } : undefined,
          reasoning: suite.name === 'openai' ? { effort: 'high', max_output_tokens: 1024 } : undefined,
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
    assert.equal(generation.maxTokens, 400);
    assert.equal(generation.temperature, 0.1);
    assert.equal(generation.topP, 0.9);
    assert.equal(
      generation.metadata['sigil.gen_ai.request.thinking.budget_tokens'],
      suite.name === 'openai' ? 1024 : suite.name === 'anthropic' ? 2048 : 1536
    );
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
    maxCompletionTokens: 256,
    maxTokens: 999,
    temperature: 0.3,
    topP: 0.8,
    toolChoice: { type: 'function', name: 'weather' },
    reasoning: { effort: 'high', max_output_tokens: 1024 },
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
  assert.equal(mappedDefault.maxTokens, 256);
  assert.equal(mappedDefault.temperature, 0.3);
  assert.equal(mappedDefault.topP, 0.8);
  assert.equal(mappedDefault.toolChoice, '{"name":"weather","type":"function"}');
  assert.equal(mappedDefault.thinkingEnabled, true);
  assert.equal(mappedDefault.metadata['sigil.gen_ai.request.thinking.budget_tokens'], 1024);
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
      maxCompletionTokens: suite.name === 'openai' ? 123 : undefined,
      maxTokens: suite.name === 'anthropic' ? 123 : undefined,
      maxOutputTokens: suite.name === 'gemini' ? 123 : undefined,
      temperature: 0.4,
      topP: 0.7,
      toolChoice: suite.name === 'openai' ? { type: 'function', name: 'weather' } : undefined,
      functionCallingMode: suite.name === 'gemini' ? { mode: 'ANY' } : undefined,
      thinking: suite.name === 'anthropic' ? { type: 'adaptive', budget_tokens: 2048 } : undefined,
      thinkingConfig: suite.name === 'gemini' ? { includeThoughts: true, thinkingBudget: 1536 } : undefined,
      reasoning: suite.name === 'openai' ? { effort: 'high', max_output_tokens: 1024 } : undefined,
      messages: [{ role: 'user', content: 'hello' }],
    };
    const summary =
      suite.streamEventsKey === 'chunks'
        ? { outputText: `stream-output-${suite.name}`, chunks: [{ token: 'x' }] }
        : { outputText: `stream-output-${suite.name}`, events: [{ type: 'delta' }] };

    const mapped = suite.sdk.fromStream(request, summary, { rawArtifacts: true });
    assert.equal(mapped.responseModel, `${suite.name}-model`);
    assert.equal(mapped.maxTokens, 123);
    assert.equal(mapped.temperature, 0.4);
    assert.equal(mapped.topP, 0.7);
    assert.equal(
      mapped.metadata['sigil.gen_ai.request.thinking.budget_tokens'],
      suite.name === 'openai' ? 1024 : suite.name === 'anthropic' ? 2048 : 1536
    );
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

test('provider mappers expose thinking disabled when explicitly configured', () => {
  const anthropicMapped = anthropic.fromRequestResponse(
    {
      model: 'claude-sonnet',
      thinking: 'disabled',
      messages: [{ role: 'user', content: 'hi' }],
    },
    { outputText: 'ok' }
  );
  assert.equal(anthropicMapped.thinkingEnabled, false);

  const geminiMapped = gemini.fromRequestResponse(
    {
      model: 'gemini-pro',
      thinkingConfig: { includeThoughts: false },
      messages: [{ role: 'user', content: 'hi' }],
    },
    { outputText: 'ok' }
  );
  assert.equal(geminiMapped.thinkingEnabled, false);
});

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
