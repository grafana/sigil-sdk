import assert from 'node:assert/strict';
import test from 'node:test';

import {
  buildTagsAndMetadata,
  chooseMode,
  createSourceState,
  resolveThread,
  sourceTagFor,
} from '../scripts/devex-emitter.mjs';

test('devex emitter tags include required provider and language fields', () => {
  const payload = buildTagsAndMetadata('openai', 'SYNC', 2, 1);
  assert.equal(payload.tags['sigil.devex.language'], 'javascript');
  assert.equal(payload.tags['sigil.devex.provider'], 'openai');
  assert.equal(payload.tags['sigil.devex.source'], 'provider_wrapper');
  assert.equal(payload.tags['sigil.devex.mode'], 'SYNC');
  assert.equal(payload.metadata.turn_index, 2);
  assert.equal(payload.metadata.conversation_slot, 1);
  assert.equal(typeof payload.metadata.agent_persona, 'string');
  assert.equal(payload.metadata.emitter, 'sdk-traffic');
  assert.equal(typeof payload.metadata.provider_shape, 'string');
});

test('custom provider source is marked as core_custom', () => {
  assert.equal(sourceTagFor('mistral'), 'core_custom');
  assert.equal(sourceTagFor('gemini'), 'provider_wrapper');
});

test('mode chooser returns both SYNC and STREAM based on threshold', () => {
  assert.equal(chooseMode(0, 30), 'STREAM');
  assert.equal(chooseMode(29, 30), 'STREAM');
  assert.equal(chooseMode(30, 30), 'SYNC');
  assert.equal(chooseMode(99, 30), 'SYNC');
});

test('thread rotation resets turn and assigns a new conversation id', async () => {
  const state = createSourceState(1);

  let thread = resolveThread(state, 3, 'openai', 0);
  assert.equal(thread.turn, 0);
  const firstConversationId = thread.conversationId;
  assert.ok(firstConversationId.length > 0);

  thread.turn = 3;
  await new Promise((resolve) => setTimeout(resolve, 2));
  thread = resolveThread(state, 3, 'openai', 0);
  assert.equal(thread.turn, 0);
  assert.notEqual(thread.conversationId, firstConversationId);
});
