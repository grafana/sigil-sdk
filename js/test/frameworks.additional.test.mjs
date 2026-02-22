import assert from 'node:assert/strict';
import test from 'node:test';

import { defaultConfig, SigilClient } from '../.test-dist/index.js';
import {
  SigilLangChainHandler,
  withSigilLangChainCallbacks,
} from '../.test-dist/frameworks/langchain/index.js';
import {
  SigilLangGraphHandler,
  withSigilLangGraphCallbacks,
} from '../.test-dist/frameworks/langgraph/index.js';
import {
  SigilOpenAIAgentsHandler,
  withSigilOpenAIAgentsHooks,
} from '../.test-dist/frameworks/openai-agents/index.js';
import {
  SigilLlamaIndexHandler,
  attachSigilLlamaIndexCallbacks,
  withSigilLlamaIndexCallbacks,
} from '../.test-dist/frameworks/llamaindex/index.js';
import {
  SigilGoogleAdkHandler,
  createSigilGoogleAdkPlugin,
  withSigilGoogleAdkPlugins,
} from '../.test-dist/frameworks/google-adk/index.js';

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

const frameworks = [
  {
    name: 'openai-agents',
    handlerCtor: SigilOpenAIAgentsHandler,
  },
  {
    name: 'llamaindex',
    handlerCtor: SigilLlamaIndexHandler,
  },
  {
    name: 'google-adk',
    handlerCtor: SigilGoogleAdkHandler,
  },
];

for (const framework of frameworks) {
  test(`${framework.name} handler maps conversation id first and preserves lineage metadata`, async () => {
    const generation = await captureSingleGeneration(async (client) => {
      const handler = new framework.handlerCtor(client);

      await handler.handleChatModelStart(
        { name: 'ChatModel' },
        [[{ type: 'human', content: 'hello' }]],
        'run-sync',
        'parent-run-sync',
        {
          invocation_params: {
            model: 'gpt-5',
            retry_attempt: 1,
            session_id: 'session-from-invocation',
          },
        },
        ['prod'],
        {
          conversation_id: 'framework-conversation-42',
          thread_id: 'framework-thread-42',
          event_id: 'framework-event-42',
        }
      );
      await handler.handleLLMEnd(
        {
          generations: [[{ text: 'world' }]],
          llm_output: {
            model_name: 'gpt-5',
            finish_reason: 'stop',
          },
        },
        'run-sync'
      );
    });

    assert.equal(generation.tags['sigil.framework.name'], framework.name);
    assert.equal(generation.tags['sigil.framework.source'], 'handler');
    assert.equal(generation.tags['sigil.framework.language'], 'javascript');
    assert.equal(generation.conversationId, 'framework-conversation-42');
    assert.equal(generation.metadata['sigil.framework.run_id'], 'run-sync');
    assert.equal(generation.metadata['sigil.framework.parent_run_id'], 'parent-run-sync');
    assert.equal(generation.metadata['sigil.framework.thread_id'], 'framework-thread-42');
    assert.equal(generation.metadata['sigil.framework.event_id'], 'framework-event-42');
    assert.equal(generation.metadata['sigil.framework.run_type'], 'chat');
    assert.equal(generation.metadata['sigil.framework.retry_attempt'], 1);
  });

  test(`${framework.name} handler keeps thread metadata when conversation and thread ids are split across payloads`, async () => {
    const generation = await captureSingleGeneration(async (client) => {
      const handler = new framework.handlerCtor(client);

      await handler.handleChatModelStart(
        { name: 'ChatModel' },
        [[{ type: 'human', content: 'hello' }]],
        'run-split',
        'parent-run-split',
        {
          invocation_params: {
            model: 'gpt-5',
          },
          thread_id: 'framework-thread-split-42',
        },
        ['prod'],
        {
          conversation_id: 'framework-conversation-split-42',
          event_id: 'framework-event-split-42',
        }
      );
      await handler.handleLLMEnd(
        {
          generations: [[{ text: 'world' }]],
          llm_output: {
            model_name: 'gpt-5',
            finish_reason: 'stop',
          },
        },
        'run-split'
      );
    });

    assert.equal(generation.conversationId, 'framework-conversation-split-42');
    assert.equal(generation.metadata['sigil.framework.thread_id'], 'framework-thread-split-42');
    assert.equal(generation.metadata['sigil.framework.event_id'], 'framework-event-split-42');
  });

  test(`${framework.name} handler uses deterministic conversation fallback when framework id is missing`, async () => {
    const generation = await captureSingleGeneration(async (client) => {
      const handler = new framework.handlerCtor(client);

      await handler.handleLLMStart(
        { kwargs: { model: 'gpt-5' } },
        ['hello'],
        'run-fallback',
        undefined,
        { invocation_params: { model: 'gpt-5' } }
      );
      await handler.handleLLMEnd(
        {
          generations: [[{ text: 'ok' }]],
          llm_output: { model_name: 'gpt-5' },
        },
        'run-fallback'
      );
    });

    assert.equal(generation.conversationId, `sigil:framework:${framework.name}:run-fallback`);
  });

  test(`${framework.name} handler normalizes extra metadata to JSON-safe values`, async () => {
    const generation = await captureSingleGeneration(async (client) => {
      const handler = new framework.handlerCtor(client, {
        extraMetadata: {
          timestamp: new Date('2026-02-20T00:00:00.000Z'),
          list: ['a', { nested: true }, () => 'skip'],
          object: { child: { depth: 'ok' } },
          callable: () => 'skip',
        },
      });

      await handler.handleLLMStart(
        { kwargs: { model: 'gpt-5' } },
        ['hello'],
        'run-metadata',
        undefined,
        { invocation_params: { model: 'gpt-5' } }
      );
      await handler.handleLLMEnd(
        {
          generations: [[{ text: 'ok' }]],
          llm_output: { model_name: 'gpt-5' },
        },
        'run-metadata'
      );
    });

    assert.equal(generation.metadata.timestamp, '2026-02-20T00:00:00.000Z');
    assert.deepEqual(generation.metadata.list, ['a', { nested: true }]);
    assert.deepEqual(generation.metadata.object, { child: { depth: 'ok' } });
    assert.equal(Object.prototype.hasOwnProperty.call(generation.metadata, 'callable'), false);
  });

  test(`${framework.name} handler drops invalid date metadata values`, async () => {
    const generation = await captureSingleGeneration(async (client) => {
      const handler = new framework.handlerCtor(client, {
        extraMetadata: {
          validDate: new Date('2026-02-20T00:00:00.000Z'),
          invalidDate: new Date('not-a-real-date'),
        },
      });

      await handler.handleLLMStart(
        { kwargs: { model: 'gpt-5' } },
        ['hello'],
        'run-invalid-date',
        undefined,
        { invocation_params: { model: 'gpt-5' } }
      );
      await handler.handleLLMEnd(
        {
          generations: [[{ text: 'ok' }]],
          llm_output: { model_name: 'gpt-5' },
        },
        'run-invalid-date'
      );
    });

    assert.equal(generation.metadata.validDate, '2026-02-20T00:00:00.000Z');
    assert.equal(Object.prototype.hasOwnProperty.call(generation.metadata, 'invalidDate'), false);
  });

  test(`${framework.name} handler does not mark repeated non-cyclic metadata objects as circular`, async () => {
    const shared = { nested: { ok: true } };
    const generation = await captureSingleGeneration(async (client) => {
      const handler = new framework.handlerCtor(client, {
        extraMetadata: {
          first: shared,
          second: shared,
        },
      });

      await handler.handleLLMStart(
        { kwargs: { model: 'gpt-5' } },
        ['hello'],
        'run-shared-metadata',
        undefined,
        { invocation_params: { model: 'gpt-5' } }
      );
      await handler.handleLLMEnd(
        {
          generations: [[{ text: 'ok' }]],
          llm_output: { model_name: 'gpt-5' },
        },
        'run-shared-metadata'
      );
    });

    assert.deepEqual(generation.metadata.first, { nested: { ok: true } });
    assert.deepEqual(generation.metadata.second, { nested: { ok: true } });
  });
}

async function captureSingleGeneration(run) {
  const generations = [];
  await captureGenerations(run, (generation) => generations.push(generation));
  assert.equal(generations.length, 1);
  return generations[0];
}

test('withSigilLangChainCallbacks preserves existing callbacks and appends sigil handler', () => {
  const client = new SigilClient(defaultConfig());
  try {
    const existing = { name: 'existing' };
    const config = withSigilLangChainCallbacks({ callbacks: [existing], retry: 1 }, client);
    assert.equal(config.retry, 1);
    assert.equal(Array.isArray(config.callbacks), true);
    assert.equal(config.callbacks.length, 2);
    assert.equal(config.callbacks[0], existing);
    assert.equal(config.callbacks[1] instanceof SigilLangChainHandler, true);
  } finally {
    void client.shutdown();
  }
});

test('withSigilLangChainCallbacks does not duplicate existing sigil handler', () => {
  const client = new SigilClient(defaultConfig());
  try {
    const existingSigil = new SigilLangChainHandler(client, { providerResolver: 'auto' });
    const config = withSigilLangChainCallbacks({ callbacks: [existingSigil] }, client);
    assert.equal(config.callbacks.length, 1);
    assert.equal(config.callbacks[0], existingSigil);
  } finally {
    void client.shutdown();
  }
});

test('withSigilLangGraphCallbacks creates callback list when config is empty', () => {
  const client = new SigilClient(defaultConfig());
  try {
    const config = withSigilLangGraphCallbacks(undefined, client);
    assert.equal(Array.isArray(config.callbacks), true);
    assert.equal(config.callbacks.length, 1);
    assert.equal(config.callbacks[0] instanceof SigilLangGraphHandler, true);
  } finally {
    void client.shutdown();
  }
});

test('withSigilOpenAIAgentsHooks wires to hook emitter lifecycle', async () => {
  const generation = await captureSingleGeneration(async (client) => {
    const hooks = new FakeHookEmitter();
    const registration = withSigilOpenAIAgentsHooks(hooks, client);

    const context = {
      context: {
        conversationId: 'oa-conversation-42',
      },
      usage: {
        input_tokens: 11,
        output_tokens: 7,
        total_tokens: 18,
      },
    };
    const agent = {
      name: 'triage',
      model: 'gpt-5',
    };

    hooks.emit('agent_start', context, agent, [{ role: 'user', content: 'hello' }]);
    hooks.emit(
      'agent_tool_start',
      context,
      agent,
      { name: 'weather_tool' },
      { toolCall: { callId: 'call-1', name: 'weather_tool', arguments: '{"city":"Paris"}' } }
    );
    hooks.emit(
      'agent_tool_end',
      context,
      agent,
      { name: 'weather_tool' },
      '{"temp_c":18}',
      { toolCall: { callId: 'call-1', name: 'weather_tool' } }
    );
    hooks.emit('agent_end', context, agent, 'world');

    registration.detach();
    hooks.emit('agent_start', context, agent, [{ role: 'user', content: 'ignored' }]);
    hooks.emit('agent_end', context, agent, 'ignored');
  });

  assert.equal(generation.tags['sigil.framework.name'], 'openai-agents');
  assert.equal(generation.conversationId, 'oa-conversation-42');
  assert.equal(generation.metadata['sigil.framework.run_type'], 'chat');
  assert.equal(generation.usage.inputTokens, 11);
  assert.equal(generation.usage.outputTokens, 7);
  assert.equal(generation.usage.totalTokens, 18);
});

test('withSigilOpenAIAgentsHooks reads usage from output payload when context usage is missing', async () => {
  const generation = await captureSingleGeneration(async (client) => {
    const hooks = new FakeHookEmitter();
    const registration = withSigilOpenAIAgentsHooks(hooks, client);

    const context = {
      context: {
        conversationId: 'oa-output-usage-42',
      },
    };
    const agent = {
      name: 'triage',
      model: 'gpt-5',
    };
    const output = {
      output: [{ role: 'assistant', content: 'world' }],
      usage: {
        input_tokens: 5,
        output_tokens: 3,
        total_tokens: 8,
      },
    };

    hooks.emit('agent_start', context, agent, [{ role: 'user', content: 'hello' }]);
    hooks.emit('agent_end', context, agent, output);
    registration.detach();
  });

  assert.equal(generation.conversationId, 'oa-output-usage-42');
  assert.equal(generation.usage.inputTokens, 5);
  assert.equal(generation.usage.outputTokens, 3);
  assert.equal(generation.usage.totalTokens, 8);
});

test('withSigilOpenAIAgentsHooks handles agent_error and clears stack state', async () => {
  const generations = [];
  await captureGenerations(async (client) => {
    const hooks = new FakeHookEmitter();
    const registration = withSigilOpenAIAgentsHooks(hooks, client);

    const context = {
      context: {
        conversationId: 'oa-conversation-error',
      },
    };
    const agent = {
      name: 'triage',
      model: 'gpt-5',
    };

    hooks.emit('agent_start', context, agent, [{ role: 'user', content: 'first' }]);
    hooks.emit('agent_error', context, agent, new Error('boom'));
    hooks.emit('agent_start', context, agent, [{ role: 'user', content: 'second' }]);
    hooks.emit('agent_end', context, agent, 'ok');
    registration.detach();
  }, (generation) => generations.push(generation));

  const secondGeneration = generations.find((generation) => extractFirstText(generation.output) === 'ok');
  assert.ok(secondGeneration);
  assert.equal(secondGeneration.metadata['sigil.framework.parent_run_id'], undefined);
});

test('withSigilOpenAIAgentsHooks closes tool runs when tool call id is missing', async () => {
  await captureGenerations(async (client) => {
    const hooks = new FakeHookEmitter();
    const registration = withSigilOpenAIAgentsHooks(hooks, client);

    const startedRunIds = [];
    const endedRunIds = [];
    const originalToolStart = registration.handler.handleToolStart.bind(registration.handler);
    const originalToolEnd = registration.handler.handleToolEnd.bind(registration.handler);
    registration.handler.handleToolStart = async (...args) => {
      startedRunIds.push(args[2]);
      await originalToolStart(...args);
    };
    registration.handler.handleToolEnd = async (...args) => {
      endedRunIds.push(args[1]);
      await originalToolEnd(...args);
    };

    const context = { context: { conversationId: 'oa-no-call-id' } };
    const agent = { name: 'triage', model: 'gpt-5' };
    hooks.emit('agent_start', context, agent, [{ role: 'user', content: 'hello' }]);
    hooks.emit(
      'agent_tool_start',
      context,
      agent,
      { name: 'weather_tool' },
      { toolCall: { name: 'weather_tool', arguments: '{"city":"Paris"}' } }
    );
    hooks.emit(
      'agent_tool_end',
      context,
      agent,
      { name: 'weather_tool' },
      '{"temp_c":18}',
      { toolCall: { name: 'weather_tool' } }
    );
    hooks.emit('agent_end', context, agent, 'world');

    await new Promise((resolve) => setTimeout(resolve, 0));

    assert.equal(startedRunIds.length, 1);
    assert.equal(endedRunIds.length, 1);
    assert.equal(endedRunIds[0], startedRunIds[0]);

    registration.detach();
  }, () => undefined);
});

test('withSigilLlamaIndexCallbacks registers through callback manager API', async () => {
  const generation = await captureSingleGeneration(async (client) => {
    const callbackManager = new FakeCallbackManager();
    const config = withSigilLlamaIndexCallbacks({ callbackManager, retry: 2 }, client);
    assert.equal(config.retry, 2);
    assert.equal(config.callbackManager, callbackManager);

    callbackManager.emit('llm-start', {
      detail: {
        id: 'llm-run-1',
        conversation_id: 'llama-conversation-9',
        stream: true,
        messages: [{ role: 'user', content: 'hello' }],
      },
    });
    callbackManager.emit('llm-stream', {
      detail: {
        id: 'llm-run-1',
        chunk: { delta: 'wor' },
      },
    });
    callbackManager.emit('llm-end', {
      detail: {
        id: 'llm-run-1',
        response: {
          message: { role: 'assistant', content: 'world' },
          raw: {
            usage: {
              prompt_tokens: 3,
              completion_tokens: 4,
              total_tokens: 7,
            },
            finish_reason: 'stop',
          },
        },
      },
    });
  });

  assert.equal(generation.tags['sigil.framework.name'], 'llamaindex');
  assert.equal(generation.conversationId, 'llama-conversation-9');
  assert.equal(generation.usage.inputTokens, 3);
  assert.equal(generation.usage.outputTokens, 4);
  assert.equal(generation.usage.totalTokens, 7);
  assert.equal(generation.stopReason, 'stop');
  assert.equal(generation.mode, 'STREAM');
});

test('withSigilLlamaIndexCallbacks defaults non-streaming runs to sync mode', async () => {
  const generation = await captureSingleGeneration(async (client) => {
    const callbackManager = new FakeCallbackManager();
    withSigilLlamaIndexCallbacks({ callbackManager }, client);

    callbackManager.emit('llm-start', {
      detail: {
        id: 'llm-sync-1',
        conversation_id: 'llama-conversation-sync',
        messages: [{ role: 'user', content: 'hello' }],
      },
    });
    callbackManager.emit('llm-end', {
      detail: {
        id: 'llm-sync-1',
        response: {
          message: { role: 'assistant', content: 'world' },
        },
      },
    });
  });

  assert.equal(generation.mode, 'SYNC');
});

test('withSigilLlamaIndexCallbacks closes id-less llm runs', async () => {
  const generation = await captureSingleGeneration(async (client) => {
    const callbackManager = new FakeCallbackManager();
    withSigilLlamaIndexCallbacks({ callbackManager }, client);

    callbackManager.emit('llm-start', {
      detail: {
        conversation_id: 'llama-conversation-no-id',
        stream: true,
        messages: [{ role: 'user', content: 'hello' }],
      },
    });
    callbackManager.emit('llm-stream', {
      detail: {
        chunk: { delta: 'wor' },
      },
    });
    callbackManager.emit('llm-end', {
      detail: {
        response: {
          message: { role: 'assistant', content: 'world' },
          raw: {
            usage: {
              prompt_tokens: 2,
              completion_tokens: 1,
              total_tokens: 3,
            },
          },
        },
      },
    });
  });

  assert.equal(generation.conversationId, 'llama-conversation-no-id');
  assert.equal(generation.usage.inputTokens, 2);
  assert.equal(generation.usage.outputTokens, 1);
  assert.equal(generation.usage.totalTokens, 3);
  assert.equal(generation.mode, 'STREAM');
  assert.equal(extractFirstText(generation.output), 'world');
});

test('withSigilLlamaIndexCallbacks handles llm-error and clears run state', async () => {
  const generations = [];
  await captureGenerations(async (client) => {
    const callbackManager = new FakeCallbackManager();
    withSigilLlamaIndexCallbacks({ callbackManager }, client);

    callbackManager.emit('llm-start', {
      detail: {
        id: 'llm-error-1',
        conversation_id: 'llama-conversation-error',
        messages: [{ role: 'user', content: 'first' }],
      },
    });
    callbackManager.emit('llm-error', {
      detail: {
        id: 'llm-error-1',
        error: new Error('boom'),
      },
    });
    callbackManager.emit('llm-start', {
      detail: {
        id: 'llm-after-error-2',
        conversation_id: 'llama-conversation-error',
        messages: [{ role: 'user', content: 'second' }],
      },
    });
    callbackManager.emit('llm-end', {
      detail: {
        id: 'llm-after-error-2',
        response: {
          message: { role: 'assistant', content: 'ok' },
        },
      },
    });
  }, (generation) => generations.push(generation));

  const errored = generations.find((generation) => generation.callError === 'boom');
  assert.ok(errored);
  const secondGeneration = generations.find((generation) => extractFirstText(generation.output) === 'ok');
  assert.ok(secondGeneration);
  assert.equal(secondGeneration.metadata['sigil.framework.parent_run_id'], undefined);
});

test('withSigilLlamaIndexCallbacks closes id-less tool runs', async () => {
  await captureGenerations(async (client) => {
    const callbackManager = new FakeCallbackManager();
    const registration = attachSigilLlamaIndexCallbacks(callbackManager, client);

    const startedRunIds = [];
    const endedRunIds = [];
    const originalToolStart = registration.handler.handleToolStart.bind(registration.handler);
    const originalToolEnd = registration.handler.handleToolEnd.bind(registration.handler);
    registration.handler.handleToolStart = async (...args) => {
      startedRunIds.push(args[2]);
      await originalToolStart(...args);
    };
    registration.handler.handleToolEnd = async (...args) => {
      endedRunIds.push(args[1]);
      await originalToolEnd(...args);
    };

    callbackManager.emit('llm-tool-call', {
      detail: {
        toolCall: {
          name: 'weather_tool',
          input: { city: 'Paris' },
        },
      },
    });
    callbackManager.emit('llm-tool-result', {
      detail: {
        toolCall: {
          name: 'weather_tool',
        },
        toolResult: { temp_c: 18 },
      },
    });

    await new Promise((resolve) => setTimeout(resolve, 0));

    assert.equal(startedRunIds.length, 1);
    assert.equal(endedRunIds.length, 1);
    assert.equal(endedRunIds[0], startedRunIds[0]);
    registration.detach();
  }, () => undefined);
});

test('attachSigilLlamaIndexCallbacks reuses existing registration for same manager', () => {
  const client = new SigilClient(defaultConfig());
  try {
    const callbackManager = new FakeCallbackManager();
    const first = attachSigilLlamaIndexCallbacks(callbackManager, client);
    const second = attachSigilLlamaIndexCallbacks(callbackManager, client);
    assert.equal(first, second);
    first.detach();
  } finally {
    void client.shutdown();
  }
});

test('withSigilGoogleAdkPlugins appends an ADK plugin callback implementation', async () => {
  const generation = await captureSingleGeneration(async (client) => {
    const existing = { name: 'existing-plugin' };
    const config = withSigilGoogleAdkPlugins({ plugins: [existing] }, client);
    assert.equal(Array.isArray(config.plugins), true);
    assert.equal(config.plugins.length, 2);
    assert.equal(config.plugins[0], existing);

    const plugin = config.plugins[1];
    assert.equal(typeof plugin.beforeRunCallback, 'function');
    assert.equal(typeof plugin.onEventCallback, 'function');
    assert.equal(typeof plugin.afterRunCallback, 'function');
    assert.equal(typeof plugin.beforeModelCallback, 'function');
    assert.equal(typeof plugin.afterModelCallback, 'function');
    assert.equal(typeof plugin.beforeToolCallback, 'function');
    assert.equal(typeof plugin.afterToolCallback, 'function');

    const invocationContext = {
      invocationId: 'inv-42',
      appName: 'demo-app',
      userId: 'user-1',
      branch: 'thread-42',
      session: { id: 'conversation-42' },
      agent: { name: 'root-agent' },
    };
    const callbackContext = {
      invocationId: 'inv-42',
      agentName: 'root-agent',
      invocationContext,
    };

    await plugin.beforeRunCallback({ invocationContext });
    await plugin.beforeModelCallback({
      callbackContext,
      llmRequest: {
        model: 'gemini-2.5-pro',
        contents: [{ role: 'user', parts: [{ text: 'hello' }] }],
      },
    });
    await plugin.afterModelCallback({
      callbackContext,
      llmResponse: {
        content: { parts: [{ text: 'world' }] },
        usageMetadata: {
          promptTokenCount: 3,
          candidatesTokenCount: 2,
          totalTokenCount: 5,
        },
      },
    });
    await plugin.afterRunCallback({ invocationContext });
  });

  assert.equal(generation.tags['sigil.framework.name'], 'google-adk');
  assert.equal(generation.conversationId, 'conversation-42');
  assert.equal(generation.metadata['sigil.framework.thread_id'], 'thread-42');
  assert.equal(generation.mode, 'SYNC');
});

test('google adk plugin closes tool runs when functionCallId is missing', async () => {
  await captureGenerations(async (client) => {
    const plugin = createSigilGoogleAdkPlugin(client);

    const startedRunIds = [];
    const endedRunIds = [];
    const originalToolStart = plugin.handler.handleToolStart.bind(plugin.handler);
    const originalToolEnd = plugin.handler.handleToolEnd.bind(plugin.handler);
    plugin.handler.handleToolStart = async (...args) => {
      startedRunIds.push(args[2]);
      await originalToolStart(...args);
    };
    plugin.handler.handleToolEnd = async (...args) => {
      endedRunIds.push(args[1]);
      await originalToolEnd(...args);
    };

    const invocationContext = {
      invocationId: 'inv-tool-no-call-id',
      session: { id: 'conversation-tool-no-call-id' },
      agent: { name: 'tool-agent' },
    };

    await plugin.beforeToolCallback({
      tool: { name: 'weather_tool' },
      toolArgs: { city: 'Paris' },
      toolContext: { invocationContext },
    });
    await plugin.afterToolCallback({
      toolContext: { invocationContext },
      result: { temp_c: 18 },
    });

    assert.equal(startedRunIds.length, 1);
    assert.equal(endedRunIds.length, 1);
    assert.equal(endedRunIds[0], startedRunIds[0]);
  }, () => undefined);
});

test('google adk plugin uses onUserMessageCallback when llmRequest has no contents', async () => {
  const generation = await captureSingleGeneration(async (client) => {
    const config = withSigilGoogleAdkPlugins({}, client);
    const plugin = config.plugins[0];

    const invocationContext = {
      invocationId: 'inv-user-42',
      session: { id: 'conversation-user-42' },
      agent: { name: 'root-agent' },
    };
    const callbackContext = {
      invocationId: 'inv-user-42',
      agentName: 'root-agent',
      invocationContext,
    };

    await plugin.onUserMessageCallback({
      invocationContext,
      userMessage: {
        role: 'user',
        parts: [{ text: 'hello from onUserMessage' }],
      },
    });
    await plugin.beforeRunCallback({ invocationContext });
    await plugin.beforeModelCallback({
      callbackContext,
      llmRequest: {
        model: 'gemini-2.5-pro',
        contents: [],
      },
    });
    await plugin.afterModelCallback({
      callbackContext,
      llmResponse: {
        content: { parts: [{ text: 'world' }] },
      },
    });
    await plugin.afterRunCallback({ invocationContext });
  });

  assert.equal(extractFirstText(generation.input), 'hello from onUserMessage');
});

test('google adk plugin records partial tokens from onEventCallback and marks stream mode', async () => {
  const generation = await captureSingleGeneration(async (client) => {
    const config = withSigilGoogleAdkPlugins({}, client);
    const plugin = config.plugins[0];

    const invocationContext = {
      invocationId: 'inv-stream-42',
      session: { id: 'conversation-stream-42' },
      agent: { name: 'root-agent' },
    };
    const callbackContext = {
      invocationId: 'inv-stream-42',
      agentName: 'root-agent',
      invocationContext,
    };

    await plugin.beforeRunCallback({ invocationContext });
    await plugin.beforeModelCallback({
      callbackContext,
      llmRequest: {
        model: 'gemini-2.5-pro',
        config: { stream: true },
        contents: [{ role: 'user', parts: [{ text: 'hello' }] }],
      },
    });
    await plugin.onEventCallback({
      invocationContext,
      event: {
        invocationId: 'inv-stream-42',
        partial: true,
        content: { parts: [{ text: 'wor' }] },
      },
    });
    await plugin.afterModelCallback({
      callbackContext,
      llmResponse: {
        content: { parts: [] },
      },
    });
    await plugin.afterRunCallback({ invocationContext });
  });

  assert.equal(generation.mode, 'STREAM');
  assert.equal(extractFirstText(generation.output), 'wor');
});

test('google adk plugin emits agent chain spans that parent llm runs', async () => {
  const generation = await captureSingleGeneration(async (client) => {
    const config = withSigilGoogleAdkPlugins({}, client);
    const plugin = config.plugins[0];

    const invocationContext = {
      invocationId: 'inv-agent-42',
      session: { id: 'conversation-agent-42' },
      agent: { name: 'root-agent' },
    };
    const callbackContext = {
      invocationId: 'inv-agent-42',
      agentName: 'planner-agent',
      invocationContext,
    };

    await plugin.beforeRunCallback({ invocationContext });
    await plugin.beforeAgentCallback({ callbackContext });
    await plugin.beforeModelCallback({
      callbackContext,
      llmRequest: {
        model: 'gemini-2.5-pro',
        contents: [{ role: 'user', parts: [{ text: 'hello' }] }],
      },
    });
    await plugin.afterModelCallback({
      callbackContext,
      llmResponse: {
        content: { parts: [{ text: 'world' }] },
      },
    });
    await plugin.afterAgentCallback({ callbackContext });
    await plugin.afterRunCallback({ invocationContext });
  });

  const parentRunId = generation.metadata['sigil.framework.parent_run_id'];
  assert.equal(typeof parentRunId, 'string');
  assert.equal(parentRunId.startsWith('adk_agent:'), true);
});

test('google adk plugin keeps fallback invocation ids stable without invocationId', async () => {
  const client = new SigilClient(defaultConfig());
  try {
    const plugin = createSigilGoogleAdkPlugin(client);
    const invocationContext = {
      session: { id: 'conversation-no-invocation-id' },
      agent: { name: 'root-agent' },
    };

    await plugin.beforeRunCallback({ invocationContext });
    await plugin.afterRunCallback({ invocationContext });

    assert.equal(plugin.invocationRunIds.size, 0);
  } finally {
    await client.shutdown();
  }
});

test('createSigilGoogleAdkPlugin exposes the ADK callback surface', () => {
  const client = new SigilClient(defaultConfig());
  try {
    const plugin = createSigilGoogleAdkPlugin(client);
    assert.equal(typeof plugin.beforeRunCallback, 'function');
    assert.equal(typeof plugin.onEventCallback, 'function');
    assert.equal(typeof plugin.afterRunCallback, 'function');
    assert.equal(typeof plugin.beforeModelCallback, 'function');
    assert.equal(typeof plugin.afterModelCallback, 'function');
    assert.equal(typeof plugin.beforeToolCallback, 'function');
    assert.equal(typeof plugin.afterToolCallback, 'function');
    assert.equal(plugin.name, 'sigil_google_adk_plugin');
  } finally {
    void client.shutdown();
  }
});

class FakeHookEmitter {
  listeners = new Map();

  on(event, listener) {
    const existing = this.listeners.get(event) ?? [];
    existing.push(listener);
    this.listeners.set(event, existing);
    return this;
  }

  off(event, listener) {
    const existing = this.listeners.get(event) ?? [];
    this.listeners.set(
      event,
      existing.filter((entry) => entry !== listener)
    );
    return this;
  }

  emit(event, ...args) {
    const existing = this.listeners.get(event) ?? [];
    for (const listener of existing) {
      listener(...args);
    }
  }
}

class FakeCallbackManager {
  listeners = new Map();

  on(event, listener) {
    const existing = this.listeners.get(event) ?? [];
    existing.push(listener);
    this.listeners.set(event, existing);
    return this;
  }

  off(event, listener) {
    const existing = this.listeners.get(event) ?? [];
    this.listeners.set(
      event,
      existing.filter((entry) => entry !== listener)
    );
    return this;
  }

  emit(event, payload) {
    const existing = this.listeners.get(event) ?? [];
    for (const listener of existing) {
      listener(payload);
    }
  }
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

function extractFirstText(messages = []) {
  if (!Array.isArray(messages) || messages.length === 0) {
    return '';
  }
  const first = messages[0];
  if (typeof first?.content === 'string') {
    return first.content;
  }
  const parts = first?.parts;
  if (!Array.isArray(parts) || parts.length === 0) {
    return '';
  }
  return parts[0]?.text ?? '';
}
