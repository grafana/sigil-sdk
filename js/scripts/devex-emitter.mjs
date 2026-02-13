import { setTimeout as sleep } from 'node:timers/promises';
import { pathToFileURL } from 'node:url';

const LANGUAGE = 'javascript';
const SOURCES = ['openai', 'anthropic', 'gemini', 'mistral'];
const PERSONAS = ['planner', 'retriever', 'executor'];
let sdkModulePromise;

async function loadSdkModule() {
  if (sdkModulePromise === undefined) {
    sdkModulePromise = import('../dist/index.js');
  }
  return sdkModulePromise;
}

export function intFromEnv(key, defaultValue) {
  const raw = process.env[key];
  if (raw === undefined || raw.trim().length === 0) {
    return defaultValue;
  }
  const value = Number.parseInt(raw, 10);
  if (!Number.isFinite(value) || value <= 0) {
    return defaultValue;
  }
  return value;
}

export function stringFromEnv(key, defaultValue) {
  const raw = process.env[key];
  if (raw === undefined) {
    return defaultValue;
  }
  const value = raw.trim();
  return value.length === 0 ? defaultValue : value;
}

export function loadConfig() {
  return {
    intervalMs: intFromEnv('SIGIL_TRAFFIC_INTERVAL_MS', 2000),
    streamPercent: intFromEnv('SIGIL_TRAFFIC_STREAM_PERCENT', 30),
    conversations: intFromEnv('SIGIL_TRAFFIC_CONVERSATIONS', 3),
    rotateTurns: intFromEnv('SIGIL_TRAFFIC_ROTATE_TURNS', 24),
    customProvider: stringFromEnv('SIGIL_TRAFFIC_CUSTOM_PROVIDER', 'mistral'),
    genHttpEndpoint: stringFromEnv('SIGIL_TRAFFIC_GEN_HTTP_ENDPOINT', 'http://sigil:8080/api/v1/generations:export'),
    traceHttpEndpoint: stringFromEnv('SIGIL_TRAFFIC_TRACE_HTTP_ENDPOINT', 'http://sigil:4318/v1/traces'),
    maxCycles: intFromEnv('SIGIL_TRAFFIC_MAX_CYCLES', 0),
  };
}

export function sourceTagFor(source) {
  return source === 'mistral' ? 'core_custom' : 'provider_wrapper';
}

export function providerShapeFor(source) {
  switch (source) {
    case 'openai':
      return 'chat_completion';
    case 'anthropic':
      return 'messages';
    case 'gemini':
      return 'generate_content';
    default:
      return 'core_generation';
  }
}

export function scenarioFor(source, turn) {
  const even = turn % 2 === 0;
  if (source === 'openai') {
    return even ? 'openai_briefing' : 'openai_live_status';
  }
  if (source === 'anthropic') {
    return even ? 'anthropic_reasoning' : 'anthropic_delta';
  }
  if (source === 'gemini') {
    return even ? 'gemini_tool_shape' : 'gemini_stream_story';
  }
  return even ? 'custom_mistral_sync' : 'custom_mistral_stream';
}

export function personaForTurn(turn) {
  return PERSONAS[turn % PERSONAS.length];
}

export function newConversationId(source, slot) {
  return `devex-${LANGUAGE}-${source}-${slot}-${Date.now()}`;
}

export function chooseMode(randomValue, streamPercent) {
  return randomValue < streamPercent ? 'STREAM' : 'SYNC';
}

export function createSourceState(conversations) {
  return {
    cursor: 0,
    slots: Array.from({ length: conversations }, () => ({
      conversationId: '',
      turn: 0,
    })),
  };
}

export function resolveThread(state, rotateTurns, source, slot) {
  const thread = state.slots[slot];
  if (thread.conversationId.length === 0 || thread.turn >= rotateTurns) {
    thread.conversationId = newConversationId(source, slot);
    thread.turn = 0;
  }
  return thread;
}

export function buildTagsAndMetadata(source, mode, turn, slot) {
  const agentPersona = personaForTurn(turn);
  return {
    agentPersona,
    tags: {
      'sigil.devex.language': LANGUAGE,
      'sigil.devex.provider': source,
      'sigil.devex.source': sourceTagFor(source),
      'sigil.devex.scenario': scenarioFor(source, turn),
      'sigil.devex.mode': mode,
    },
    metadata: {
      turn_index: turn,
      conversation_slot: slot,
      agent_persona: agentPersona,
      emitter: 'sdk-traffic',
      provider_shape: providerShapeFor(source),
    },
  };
}

async function emitOpenAISync(sdk, client, context) {
  const request = {
    model: 'gpt-5',
    systemPrompt: 'Return compact project-planning bullets.',
    messages: [
      { role: 'user', content: `Draft release checkpoint plan #${context.turn}.` },
    ],
  };

  await sdk.openai.chatCompletion(
    client,
    request,
    async () => ({
      id: `js-openai-sync-${context.turn}`,
      model: 'gpt-5',
      outputText: `Plan ${context.turn}: validate rollout, assign owner, publish timeline.`,
      stopReason: 'stop',
      usage: {
        inputTokens: 88 + (context.turn % 9),
        outputTokens: 26 + (context.turn % 7),
        totalTokens: 114 + (context.turn % 13),
      },
      raw: {
        shape: 'openai.sync',
      },
    }),
    {
      conversationId: context.conversationId,
      agentName: context.agentName,
      agentVersion: context.agentVersion,
      tags: context.tags,
      metadata: context.metadata,
    }
  );
}

async function emitOpenAIStream(sdk, client, context) {
  const request = {
    model: 'gpt-5',
    systemPrompt: 'Stream incident status updates in short clauses.',
    messages: [
      { role: 'user', content: `Stream checkpoint status for ticket ${context.turn}.` },
    ],
  };

  await sdk.openai.chatCompletionStream(
    client,
    request,
    async () => ({
      outputText: `Ticket ${context.turn}: canary healthy; promote gate passed.`,
      finalResponse: {
        id: `js-openai-stream-${context.turn}`,
        model: 'gpt-5',
        outputText: `Ticket ${context.turn}: canary healthy; promote gate passed.`,
        stopReason: 'stop',
        usage: {
          inputTokens: 51 + (context.turn % 5),
          outputTokens: 17 + (context.turn % 4),
          totalTokens: 68 + (context.turn % 7),
        },
      },
      chunks: [
        { delta: 'Ticket update: canary healthy' },
        { delta: '; promote gate passed.' },
      ],
    }),
    {
      conversationId: context.conversationId,
      agentName: context.agentName,
      agentVersion: context.agentVersion,
      tags: context.tags,
      metadata: context.metadata,
    }
  );
}

async function emitAnthropicSync(sdk, client, context) {
  const request = {
    model: 'claude-sonnet-4-5',
    systemPrompt: 'Reason in two phases: diagnosis then recommendation.',
    messages: [
      { role: 'user', content: `Summarize reliability drift set ${context.turn}.` },
    ],
  };

  await sdk.anthropic.completion(
    client,
    request,
    async () => ({
      id: `js-anthropic-sync-${context.turn}`,
      model: 'claude-sonnet-4-5',
      outputText: `Diagnosis ${context.turn}: latency drift in eu-west. Recommendation: rebalance workers.`,
      stopReason: 'end_turn',
      usage: {
        inputTokens: 73 + (context.turn % 8),
        outputTokens: 31 + (context.turn % 5),
        totalTokens: 104 + (context.turn % 11),
        cacheReadInputTokens: 12,
      },
      raw: {
        shape: 'anthropic.sync',
      },
    }),
    {
      conversationId: context.conversationId,
      agentName: context.agentName,
      agentVersion: context.agentVersion,
      tags: context.tags,
      metadata: context.metadata,
    }
  );
}

async function emitAnthropicStream(sdk, client, context) {
  const request = {
    model: 'claude-sonnet-4-5',
    systemPrompt: 'Use concise streaming deltas for operational narration.',
    messages: [
      { role: 'user', content: `Stream mitigation deltas for change ${context.turn}.` },
    ],
  };

  await sdk.anthropic.completionStream(
    client,
    request,
    async () => ({
      outputText: `Change ${context.turn}: rollback guard armed; verification complete.`,
      finalResponse: {
        id: `js-anthropic-stream-${context.turn}`,
        model: 'claude-sonnet-4-5',
        outputText: `Change ${context.turn}: rollback guard armed; verification complete.`,
        stopReason: 'end_turn',
        usage: {
          inputTokens: 46 + (context.turn % 6),
          outputTokens: 18 + (context.turn % 4),
          totalTokens: 64 + (context.turn % 8),
        },
      },
      events: [
        { type: 'message_start' },
        { type: 'delta', text: 'rollback guard armed' },
        { type: 'message_delta', stop_reason: 'end_turn' },
      ],
    }),
    {
      conversationId: context.conversationId,
      agentName: context.agentName,
      agentVersion: context.agentVersion,
      tags: context.tags,
      metadata: context.metadata,
    }
  );
}

async function emitGeminiSync(sdk, client, context) {
  const request = {
    model: 'gemini-2.5-pro',
    systemPrompt: 'Write release notes with explicit structured tool language.',
    messages: [
      { role: 'user', content: `Generate launch note ${context.turn} using function-style tone.` },
      { role: 'tool', content: '{"tool":"release_metrics","status":"green"}', name: 'release_metrics' },
    ],
  };

  await sdk.gemini.completion(
    client,
    request,
    async () => ({
      id: `js-gemini-sync-${context.turn}`,
      model: 'gemini-2.5-pro-001',
      outputText: `Launch ${context.turn}: all quality gates green; release metrics consistent.`,
      stopReason: 'STOP',
      usage: {
        inputTokens: 62 + (context.turn % 7),
        outputTokens: 20 + (context.turn % 5),
        totalTokens: 82 + (context.turn % 9),
        reasoningTokens: 6,
      },
      raw: {
        shape: 'gemini.sync',
      },
    }),
    {
      conversationId: context.conversationId,
      agentName: context.agentName,
      agentVersion: context.agentVersion,
      tags: context.tags,
      metadata: context.metadata,
    }
  );
}

async function emitGeminiStream(sdk, client, context) {
  const request = {
    model: 'gemini-2.5-pro',
    systemPrompt: 'Emit stream checkpoints as staged migration updates.',
    messages: [
      { role: 'user', content: `Stream migration sequence ${context.turn} for canary rollout.` },
    ],
  };

  await sdk.gemini.completionStream(
    client,
    request,
    async () => ({
      outputText: `Wave ${context.turn}: shard sync complete; traffic shift finalized.`,
      finalResponse: {
        id: `js-gemini-stream-${context.turn}`,
        model: 'gemini-2.5-pro-001',
        outputText: `Wave ${context.turn}: shard sync complete; traffic shift finalized.`,
        stopReason: 'STOP',
        usage: {
          inputTokens: 47 + (context.turn % 5),
          outputTokens: 16 + (context.turn % 4),
          totalTokens: 63 + (context.turn % 7),
        },
      },
      events: [
        { response_id: `js-gemini-stream-${context.turn}`, delta: 'shard sync complete' },
        { delta: '; traffic shift finalized.' },
      ],
    }),
    {
      conversationId: context.conversationId,
      agentName: context.agentName,
      agentVersion: context.agentVersion,
      tags: context.tags,
      metadata: context.metadata,
    }
  );
}

async function emitCustomSync(client, cfg, context) {
  await client.startGeneration(
    {
      conversationId: context.conversationId,
      agentName: context.agentName,
      agentVersion: context.agentVersion,
      model: {
        provider: cfg.customProvider,
        name: 'mistral-large-devex',
      },
      tags: context.tags,
      metadata: context.metadata,
    },
    async (recorder) => {
      recorder.setResult({
        input: [{ role: 'user', content: `Draft custom provider checkpoint ${context.turn}.` }],
        output: [{ role: 'assistant', content: `Custom provider sync ${context.turn}: all guardrails satisfied.` }],
        usage: {
          inputTokens: 29 + (context.turn % 6),
          outputTokens: 15 + (context.turn % 5),
          totalTokens: 44 + (context.turn % 8),
        },
        stopReason: 'stop',
      });
    }
  );
}

async function emitCustomStream(client, cfg, context) {
  await client.startStreamingGeneration(
    {
      conversationId: context.conversationId,
      agentName: context.agentName,
      agentVersion: context.agentVersion,
      model: {
        provider: cfg.customProvider,
        name: 'mistral-large-devex',
      },
      tags: context.tags,
      metadata: context.metadata,
    },
    async (recorder) => {
      recorder.setResult({
        input: [{ role: 'user', content: `Stream custom remediation report ${context.turn}.` }],
        output: [
          {
            role: 'assistant',
            parts: [
              { type: 'thinking', thinking: 'assembling synthetic stream chunks' },
              { type: 'text', text: `Custom stream ${context.turn}: segment A complete; segment B complete.` },
            ],
          },
        ],
        usage: {
          inputTokens: 24 + (context.turn % 5),
          outputTokens: 17 + (context.turn % 4),
          totalTokens: 41 + (context.turn % 7),
        },
        stopReason: 'end_turn',
      });
    }
  );
}

export async function emitSource(sdk, client, cfg, source, mode, context) {
  if (source === 'openai') {
    if (mode === 'STREAM') {
      await emitOpenAIStream(sdk, client, context);
      return;
    }
    await emitOpenAISync(sdk, client, context);
    return;
  }

  if (source === 'anthropic') {
    if (mode === 'STREAM') {
      await emitAnthropicStream(sdk, client, context);
      return;
    }
    await emitAnthropicSync(sdk, client, context);
    return;
  }

  if (source === 'gemini') {
    if (mode === 'STREAM') {
      await emitGeminiStream(sdk, client, context);
      return;
    }
    await emitGeminiSync(sdk, client, context);
    return;
  }

  if (mode === 'STREAM') {
    await emitCustomStream(client, cfg, context);
    return;
  }
  await emitCustomSync(client, cfg, context);
}

export async function runEmitter(config = loadConfig()) {
  const sdk = await loadSdkModule();
  const client = new sdk.SigilClient({
    generationExport: {
      protocol: 'http',
      endpoint: config.genHttpEndpoint,
      auth: { mode: 'none' },
      insecure: true,
    },
    trace: {
      protocol: 'http',
      endpoint: config.traceHttpEndpoint,
      auth: { mode: 'none' },
      insecure: true,
    },
  });

  const sourceState = new Map(SOURCES.map((source) => [source, createSourceState(config.conversations)]));
  let cycles = 0;
  let stopping = false;

  const stop = () => {
    stopping = true;
  };

  process.once('SIGINT', stop);
  process.once('SIGTERM', stop);

  console.log(
    `[js-emitter] started interval_ms=${config.intervalMs} stream_percent=${config.streamPercent} conversations=${config.conversations} rotate_turns=${config.rotateTurns} custom_provider=${config.customProvider}`
  );

  try {
    while (!stopping) {
      for (const source of SOURCES) {
        const state = sourceState.get(source);
        const slot = state.cursor % config.conversations;
        state.cursor += 1;

        const thread = resolveThread(state, config.rotateTurns, source, slot);
        const mode = chooseMode(Math.floor(Math.random() * 100), config.streamPercent);
        const context = buildTagsAndMetadata(source, mode, thread.turn, slot);

        const agentName = `devex-${LANGUAGE}-${source}-${context.agentPersona}`;
        const agentVersion = 'devex-1';

        await emitSource(sdk, client, config, source, mode, {
          ...context,
          conversationId: thread.conversationId,
          turn: thread.turn,
          slot,
          agentName,
          agentVersion,
        });

        thread.turn += 1;
      }

      cycles += 1;
      if (config.maxCycles > 0 && cycles >= config.maxCycles) {
        break;
      }

      const jitterMs = Math.floor(Math.random() * 401) - 200;
      const sleepMs = Math.max(200, config.intervalMs + jitterMs);
      await sleep(sleepMs);
    }
  } finally {
    await client.shutdown();
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  runEmitter().catch((error) => {
    console.error('[js-emitter] fatal error', error);
    process.exit(1);
  });
}
