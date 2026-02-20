import assert from 'node:assert/strict';
import test from 'node:test';
import { defaultConfig, SigilClient } from '../.test-dist/index.js';
import { SigilLangChainHandler } from '../.test-dist/frameworks/langchain/index.js';

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
      undefined,
      { invocation_params: { model: 'gpt-5' } },
      undefined,
      { thread_id: 'chain-thread-42' }
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
      'run-sync'
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

    await handler.handleLLMStart(
      { kwargs: { model: 'claude-sonnet-4-5' } },
      ['stream this'],
      'run-stream',
      undefined,
      { invocation_params: { model: 'claude-sonnet-4-5', stream: true } }
    );
    await handler.handleLLMNewToken('hello', undefined, 'run-stream');
    await handler.handleLLMNewToken(' world', undefined, 'run-stream');
    await handler.handleLLMEnd({ llm_output: { model_name: 'claude-sonnet-4-5' } }, 'run-stream');
  });

  assert.equal(generation.mode, 'STREAM');
  assert.equal(generation.model.provider, 'anthropic');
  assert.equal(generation.output[0].content, 'hello world');
});

test('langchain provider mapping covers openai anthopic gemini and fallback', async () => {
  const providers = [];

  await captureGenerations(async (client) => {
    const handler = new SigilLangChainHandler(client);

    await handler.handleLLMStart({}, ['x'], 'run-openai', undefined, { invocation_params: { model: 'gpt-5' } });
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

test('langchain handler sets call_error on llm error', async () => {
  const generation = await captureSingleGeneration(async (client) => {
    const handler = new SigilLangChainHandler(client);

    await handler.handleLLMStart({}, ['x'], 'run-error', undefined, { invocation_params: { model: 'gpt-5' } });
    await handler.handleLLMError(new Error('provider unavailable'), 'run-error');
  });

  assert.match(generation.callError ?? '', /provider unavailable/);
  assert.equal(generation.tags['sigil.framework.name'], 'langchain');
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
