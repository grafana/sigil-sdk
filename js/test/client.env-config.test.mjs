import assert from 'node:assert/strict';
import test from 'node:test';

import { configFromEnv, mergeConfig, SigilClient } from '../.test-dist/index.js';

// configFromEnv table: env-only resolution with no caller-supplied config.
const configFromEnvCases = [
  {
    name: 'no env returns defaults',
    env: {},
    check: (cfg) => {
      assert.equal(cfg.generationExport.protocol, 'http');
      assert.equal(cfg.contentCapture, 'default');
      assert.equal(cfg.agentName, undefined);
      assert.equal(cfg.userId, undefined);
      assert.equal(cfg.debug, undefined);
    },
  },
  {
    name: 'transport from env',
    env: { SIGIL_ENDPOINT: 'https://env:4318', SIGIL_PROTOCOL: 'grpc' },
    check: (cfg) => {
      assert.equal(cfg.generationExport.endpoint, 'https://env:4318');
      assert.equal(cfg.generationExport.protocol, 'grpc');
    },
  },
  {
    name: 'basic auth from env',
    env: {
      SIGIL_AUTH_MODE: 'basic',
      SIGIL_AUTH_TENANT_ID: '42',
      SIGIL_AUTH_TOKEN: 'glc_xxx',
    },
    check: (cfg) => {
      assert.equal(cfg.generationExport.auth.mode, 'basic');
      assert.equal(cfg.generationExport.auth.tenantId, '42');
      assert.equal(cfg.generationExport.auth.basicUser, '42');
      assert.equal(cfg.generationExport.auth.basicPassword, 'glc_xxx');
    },
  },
  {
    name: 'bearer auth from env',
    env: { SIGIL_AUTH_MODE: 'bearer', SIGIL_AUTH_TOKEN: 'tok' },
    check: (cfg) => {
      assert.equal(cfg.generationExport.auth.mode, 'bearer');
      assert.equal(cfg.generationExport.auth.bearerToken, 'tok');
    },
  },
  {
    name: 'agent / user / tags / debug from env',
    env: {
      SIGIL_AGENT_NAME: 'planner',
      SIGIL_AGENT_VERSION: '1.2.3',
      SIGIL_USER_ID: 'alice',
      SIGIL_TAGS: 'service=orchestrator,env=prod',
      SIGIL_DEBUG: 'true',
    },
    check: (cfg) => {
      assert.equal(cfg.agentName, 'planner');
      assert.equal(cfg.agentVersion, '1.2.3');
      assert.equal(cfg.userId, 'alice');
      assert.deepEqual(cfg.tags, { service: 'orchestrator', env: 'prod' });
      assert.equal(cfg.debug, true);
    },
  },
  {
    name: 'insecure default is false',
    env: {},
    check: (cfg) => {
      assert.equal(cfg.generationExport.insecure, false);
    },
  },
  {
    name: 'invalid content_capture_mode falls back to default',
    env: { SIGIL_CONTENT_CAPTURE_MODE: 'bogus' },
    check: (cfg) => {
      assert.equal(cfg.contentCapture, 'default');
    },
  },
  {
    name: 'legacy "default" content_capture_mode falls back to default',
    env: { SIGIL_CONTENT_CAPTURE_MODE: 'default' },
    check: (cfg) => {
      assert.equal(cfg.contentCapture, 'default');
    },
  },
  {
    name: 'invalid auth mode preserves valid env siblings',
    env: {
      SIGIL_AUTH_MODE: 'Bearrer',
      SIGIL_ENDPOINT: 'valid.example:4318',
      SIGIL_AGENT_NAME: 'valid-agent',
    },
    check: (cfg) => {
      assert.equal(cfg.generationExport.auth.mode, 'none');
      assert.equal(cfg.generationExport.endpoint, 'valid.example:4318');
      assert.equal(cfg.agentName, 'valid-agent');
    },
  },
  {
    // configFromEnv must use the supplied env, not process.env. Poisoning
    // process.env here would corrupt the result if the supplied env were ignored.
    name: 'uses supplied env, ignores process.env',
    env: { SIGIL_ENDPOINT: 'supplied.example:4318' },
    setupAmbient: { SIGIL_ENDPOINT: 'ambient.example:4318', SIGIL_AGENT_NAME: 'ambient-agent' },
    check: (cfg) => {
      assert.equal(cfg.generationExport.endpoint, 'supplied.example:4318');
      // Ambient values must not leak through.
      assert.equal(cfg.agentName, undefined);
    },
  },
];

for (const tc of configFromEnvCases) {
  test(`configFromEnv: ${tc.name}`, () => {
    const original = process.env;
    if (tc.setupAmbient) {
      process.env = { ...original, ...tc.setupAmbient };
    }
    try {
      const cfg = configFromEnv(tc.env);
      tc.check(cfg);
    } finally {
      if (tc.setupAmbient) {
        process.env = original;
      }
    }
  });
}

// mergeConfig table: caller config layered over env, with explicit precedence.
const mergeConfigCases = [
  {
    name: 'explicit caller wins over env',
    config: { generationExport: { endpoint: 'https://explicit:4318' } },
    env: { SIGIL_ENDPOINT: 'https://env:4318', SIGIL_AGENT_NAME: 'planner' },
    check: (cfg) => {
      assert.equal(cfg.generationExport.endpoint, 'https://explicit:4318');
      // Unset fields still come from env.
      assert.equal(cfg.agentName, 'planner');
    },
  },
  {
    name: 'env SIGIL_AUTH_TOKEN fills caller-supplied bearer mode',
    config: { generationExport: { auth: { mode: 'bearer' } } },
    env: { SIGIL_AUTH_TOKEN: 'env-tok' },
    check: (cfg) => {
      assert.equal(cfg.generationExport.auth.mode, 'bearer');
      assert.equal(cfg.generationExport.auth.bearerToken, 'env-tok');
    },
  },
  {
    // Caller mode wins; env's mode-incompatible credentials are silently
    // ignored by resolveHeadersWithAuth.
    name: 'caller bearer mode wins over env basic mode',
    config: {
      generationExport: { auth: { mode: 'bearer', bearerToken: 'callertok' } },
    },
    env: {
      SIGIL_AUTH_MODE: 'basic',
      SIGIL_AUTH_TENANT_ID: '42',
      SIGIL_AUTH_TOKEN: 'envpass',
    },
    check: (cfg) => {
      assert.equal(cfg.generationExport.auth.mode, 'bearer');
      assert.equal(cfg.generationExport.headers?.Authorization, 'Bearer callertok');
    },
  },
  {
    name: 'stray SIGIL_AUTH_TENANT_ID does not throw',
    config: {},
    env: { SIGIL_AUTH_TENANT_ID: '42' },
    check: (cfg) => {
      assert.equal(cfg.generationExport.auth.mode, 'none');
    },
  },
  {
    // Caller tags merge with env tags as a base layer; caller wins on key
    // collision. Matches Go and Python SDK behavior.
    name: 'caller tags merge with env tags',
    config: { tags: { team: 'ai', env: 'staging' } },
    env: { SIGIL_TAGS: 'service=orch,env=prod' },
    check: (cfg) => {
      assert.deepEqual(cfg.tags, { service: 'orch', team: 'ai', env: 'staging' });
    },
  },
];

for (const tc of mergeConfigCases) {
  test(`mergeConfig: ${tc.name}`, () => {
    const cfg = mergeConfig(tc.config, tc.env);
    tc.check(cfg);
  });
}

// SigilClient integration: env-driven defaults applied to generation seeds.
test('SigilClient applies default agent / user / tags from config', async () => {
  const original = process.env;
  try {
    process.env = {
      ...original,
      SIGIL_PROTOCOL: 'none',
      SIGIL_AGENT_NAME: 'planner',
      SIGIL_AGENT_VERSION: '1.0',
      SIGIL_USER_ID: 'alice',
      SIGIL_TAGS: 'service=orch',
    };
    const client = new SigilClient();
    try {
      const rec = client.startGeneration({
        model: { provider: 'openai', name: 'gpt-5' },
        conversationId: 'conv-1',
      });
      assert.equal(rec.seed.agentName, 'planner');
      assert.equal(rec.seed.agentVersion, '1.0');
      assert.equal(rec.seed.userId, 'alice');
      assert.deepEqual(rec.seed.tags, { service: 'orch' });
    } finally {
      await client.shutdown();
    }
  } finally {
    process.env = original;
  }
});

test('SigilClient: per-call args override env defaults', async () => {
  const original = process.env;
  try {
    process.env = {
      ...original,
      SIGIL_PROTOCOL: 'none',
      SIGIL_AGENT_NAME: 'planner',
      SIGIL_TAGS: 'env=prod',
    };
    const client = new SigilClient();
    try {
      const rec = client.startGeneration({
        model: { provider: 'openai', name: 'gpt-5' },
        agentName: 'reviewer',
        tags: { env: 'staging', task: 'summarize' },
      });
      assert.equal(rec.seed.agentName, 'reviewer');
      assert.deepEqual(rec.seed.tags, { env: 'staging', task: 'summarize' });
    } finally {
      await client.shutdown();
    }
  } finally {
    process.env = original;
  }
});

test('SigilClient applies default agent identity to tool execution recorders', async () => {
  const original = process.env;
  try {
    process.env = {
      ...original,
      SIGIL_PROTOCOL: 'none',
      SIGIL_AGENT_NAME: 'planner',
      SIGIL_AGENT_VERSION: '1.0',
    };
    const client = new SigilClient();
    try {
      const rec = client.startToolExecution({ toolName: 'lookup' });
      assert.equal(rec.seed.agentName, 'planner');
      assert.equal(rec.seed.agentVersion, '1.0');
    } finally {
      await client.shutdown();
    }
  } finally {
    process.env = original;
  }
});
