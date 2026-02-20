import assert from 'node:assert/strict';
import test from 'node:test';
import { defaultConfig, SigilClient } from '../.test-dist/index.js';
import { SigilLangGraphHandler } from '../.test-dist/frameworks/langgraph/index.js';

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

test('langgraph handler records sync lifecycle with framework tags', async () => {
  const generation = await captureSingleGeneration(async (client) => {
    const handler = new SigilLangGraphHandler(client, {
      agentName: 'agent-langgraph',
      agentVersion: 'v1',
      extraTags: { env: 'test' },
      extraMetadata: { seed: 9 },
    });

    await handler.handleChatModelStart(
      { name: 'ChatOpenAI' },
      [[{ type: 'human', content: 'hello' }]],
      'run-sync',
      undefined,
      { invocation_params: { model: 'gpt-5' } }
    );
    await handler.handleLLMEnd(
      {
        generations: [[{ text: 'world' }]],
        llm_output: {
          model_name: 'gpt-5',
          finish_reason: 'stop',
          token_usage: {
            prompt_tokens: 11,
            completion_tokens: 6,
            total_tokens: 17,
          },
        },
      },
      'run-sync'
    );
  });

  assert.equal(generation.mode, 'SYNC');
  assert.equal(generation.model.provider, 'openai');
  assert.equal(generation.tags['sigil.framework.name'], 'langgraph');
  assert.equal(generation.tags['sigil.framework.source'], 'handler');
  assert.equal(generation.tags['sigil.framework.language'], 'javascript');
  assert.equal(generation.tags.env, 'test');
  assert.equal(generation.metadata['sigil.framework.run_id'], 'run-sync');
  assert.equal(generation.metadata.seed, 9);
});

test('langgraph handler records stream mode and token fallback output', async () => {
  const generation = await captureSingleGeneration(async (client) => {
    const handler = new SigilLangGraphHandler(client);

    await handler.handleLLMStart(
      { kwargs: { model: 'gemini-2.5-pro' } },
      ['stream this'],
      'run-stream',
      undefined,
      { invocation_params: { model: 'gemini-2.5-pro', streaming: true } }
    );
    await handler.handleLLMNewToken('hello', undefined, 'run-stream');
    await handler.handleLLMNewToken(' world', undefined, 'run-stream');
    await handler.handleLLMEnd({ llm_output: { model_name: 'gemini-2.5-pro' } }, 'run-stream');
  });

  assert.equal(generation.mode, 'STREAM');
  assert.equal(generation.model.provider, 'gemini');
  assert.equal(generation.output[0].content, 'hello world');
});

test('langgraph provider mapping covers openai anthopic gemini and fallback', async () => {
  const providers = [];

  await captureGenerations(async (client) => {
    const handler = new SigilLangGraphHandler(client);

    await handler.handleLLMStart({}, ['x'], 'run-openai', undefined, { invocation_params: { model: 'o3-mini' } });
    await handler.handleLLMEnd({ generations: [[{ text: 'ok' }]] }, 'run-openai');

    await handler.handleLLMStart({}, ['x'], 'run-anthropic', undefined, { invocation_params: { model: 'claude-sonnet-4-5' } });
    await handler.handleLLMEnd({ generations: [[{ text: 'ok' }]] }, 'run-anthropic');

    await handler.handleLLMStart({}, ['x'], 'run-gemini', undefined, { invocation_params: { model: 'gemini-2.5-pro' } });
    await handler.handleLLMEnd({ generations: [[{ text: 'ok' }]] }, 'run-gemini');

    await handler.handleLLMStart({}, ['x'], 'run-custom', undefined, { invocation_params: { model: 'mistral-large' } });
    await handler.handleLLMEnd({ generations: [[{ text: 'ok' }]] }, 'run-custom');
  }, (generation) => providers.push(generation.model.provider));

  assert.deepEqual(providers, ['openai', 'anthropic', 'gemini', 'custom']);
});

test('langgraph handler sets call_error on llm error', async () => {
  const generation = await captureSingleGeneration(async (client) => {
    const handler = new SigilLangGraphHandler(client);

    await handler.handleLLMStart({}, ['x'], 'run-error', undefined, { invocation_params: { model: 'gpt-5' } });
    await handler.handleLLMError(new Error('provider unavailable'), 'run-error');
  });

  assert.match(generation.callError ?? '', /provider unavailable/);
  assert.equal(generation.tags['sigil.framework.name'], 'langgraph');
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
