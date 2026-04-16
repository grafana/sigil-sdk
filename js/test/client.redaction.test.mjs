import assert from 'node:assert/strict';
import test from 'node:test';
import { trace } from '@opentelemetry/api';
import { createSecretRedactionSanitizer, defaultConfig, SigilClient } from '../.test-dist/index.js';

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

test('secret redaction sanitizer redacts assistant and tool content but leaves user input by default', async () => {
  const exporter = new CapturingExporter();
  const defaults = defaultConfig();
  const client = new SigilClient({
    tracer: trace.getTracer('sigil-sdk-js-test'),
    generationExport: {
      ...defaults.generationExport,
      batchSize: 1,
      flushIntervalMs: 60_000,
    },
    generationExporter: exporter,
    generationSanitizer: createSecretRedactionSanitizer(),
  });

  try {
    const secretToken = 'glc_abcdefghijklmnopqrstuvwxyz1234';
    const envSecret = 'DATABASE_PASSWORD=hunter2secret123';

    const recorder = client.startGeneration({
      model: { provider: 'openai', name: 'gpt-5' },
    });
    recorder.setResult({
      input: [{ role: 'user', parts: [{ type: 'text', text: `user pasted ${secretToken}` }] }],
      output: [
        {
          role: 'assistant',
          parts: [
            { type: 'text', text: `assistant saw ${secretToken}` },
            { type: 'thinking', thinking: `thinking about ${secretToken}` },
            {
              type: 'tool_call',
              toolCall: {
                id: 'call-1',
                name: 'bash',
                inputJSON: JSON.stringify({ header: `Bearer ${'a'.repeat(30)}`, env: envSecret }),
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
                name: 'bash',
                content: `output ${envSecret}`,
              },
            },
          ],
        },
      ],
    });
    recorder.end();

    await client.flush();

    const generation = exporter.requests[0].generations[0];
    assert.match(generation.input[0].parts[0].text, /glc_/);
    assert.doesNotMatch(generation.output[0].parts[0].text, /glc_/);
    assert.match(generation.output[0].parts[0].text, /\[REDACTED:grafana-cloud-token\]/);
    assert.doesNotMatch(generation.output[0].parts[1].thinking, /glc_/);
    assert.doesNotMatch(generation.output[0].parts[2].toolCall.inputJSON, /hunter2secret123|Bearer /);
    assert.match(generation.output[0].parts[2].toolCall.inputJSON, /\[REDACTED:/);
    assert.doesNotMatch(generation.output[1].parts[0].toolResult.content, /hunter2secret123/);
    assert.match(generation.output[1].parts[0].toolResult.content, /\[REDACTED:env-secret-value\]/);
  } finally {
    await client.shutdown();
  }
});

test('secret redaction sanitizer can redact user input when enabled', async () => {
  const sanitizer = createSecretRedactionSanitizer({ redactInputMessages: true });
  const sanitized = sanitizer({
    id: 'gen-1',
    mode: 'SYNC',
    operationName: 'generateText',
    model: { provider: 'openai', name: 'gpt-5' },
    startedAt: new Date('2026-01-01T00:00:00Z'),
    completedAt: new Date('2026-01-01T00:00:01Z'),
    input: [{ role: 'user', parts: [{ type: 'text', text: 'key sk-proj-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' }] }],
  });

  assert.doesNotMatch(sanitized.input[0].parts[0].text, /sk-proj-/);
  assert.match(sanitized.input[0].parts[0].text, /\[REDACTED:openai-project-key\]/);
});

test('generation sanitizer failure falls back to metadata_only stripping', async () => {
  const exporter = new CapturingExporter();
  const defaults = defaultConfig();
  const warnings = [];
  const client = new SigilClient({
    tracer: trace.getTracer('sigil-sdk-js-test'),
    generationExport: {
      ...defaults.generationExport,
      batchSize: 1,
      flushIntervalMs: 60_000,
    },
    generationExporter: exporter,
    generationSanitizer: () => {
      throw new Error('boom');
    },
    logger: {
      warn(message, ...args) {
        warnings.push([message, ...args]);
      },
    },
  });

  try {
    const recorder = client.startGeneration({
      model: { provider: 'openai', name: 'gpt-5' },
      conversationTitle: 'Top secret title',
      systemPrompt: 'system secret',
    });
    recorder.setResult({
      input: [{ role: 'user', parts: [{ type: 'text', text: 'hello' }] }],
      output: [{ role: 'assistant', parts: [{ type: 'text', text: 'world' }] }],
    });
    recorder.end();

    await client.flush();

    const generation = exporter.requests[0].generations[0];
    assert.equal(generation.metadata['sigil.sdk.content_capture_mode'], 'metadata_only');
    assert.equal(generation.conversationTitle, '');
    assert.equal(generation.systemPrompt, '');
    assert.equal(generation.input[0].parts[0].text, '');
    assert.equal(generation.output[0].parts[0].text, '');
    assert.equal(warnings.length, 1);
    assert.match(warnings[0][0], /generation sanitization failed/);
  } finally {
    await client.shutdown();
  }
});
