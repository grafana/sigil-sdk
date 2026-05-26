import { mkdtemp, rm } from "fs/promises";
import { tmpdir } from "os";
import { join } from "path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AssistantMessage, Part, UserMessage } from "@opencode-ai/sdk";
import { createSigilHooks } from "./hooks.js";
import type { SigilConfig } from "./config.js";

const mocks = vi.hoisted(() => {
  const records: { seed: any; recorder: any }[] = [];
  return {
    records,
    sigil: {
      startGeneration: vi.fn(async (seed: any, callback: (recorder: any) => Promise<void>) => {
        const recorder = {
          setResult: vi.fn(),
          setCallError: vi.fn(),
        };
        records.push({ seed, recorder });
        await callback(recorder);
      }),
      flush: vi.fn(),
      shutdown: vi.fn(),
    },
  };
});

vi.mock("./client.js", () => ({
  createSigilClient: vi.fn(() => mocks.sigil),
}));

const config: SigilConfig = {
  enabled: true,
  endpoint: "http://localhost:8080/api/v1/generations:export",
  auth: { mode: "none" },
  agentName: "opencode",
  contentCapture: true,
};

function makeClient() {
  return {
    session: {
      message: vi.fn(async () => ({
        data: {
          parts: [
            { id: "part-assistant", sessionID: "ses-1", messageID: "assistant", type: "text", text: "done" },
          ],
        },
      })),
    },
  } as any;
}

function makeUserMessage(id: string, sessionID: string, system?: string): UserMessage {
  return {
    id,
    sessionID,
    role: "user",
    time: { created: 1 },
    agent: "build",
    model: { providerID: "openai", modelID: "gpt-5.4" },
    ...(system && { system }),
    tools: { bash: true },
  } as UserMessage;
}

function makeAssistantMessage(
  id: string,
  sessionID: string,
  parentID: string,
  finish: string,
): AssistantMessage {
  return {
    id,
    sessionID,
    role: "assistant",
    parentID,
    modelID: "gpt-5.4",
    providerID: "openai",
    mode: "build",
    path: { cwd: "/tmp", root: "/tmp" },
    cost: 0.01,
    tokens: { input: 100, output: 50, reasoning: 0, cache: { read: 0, write: 0 } },
    time: { created: 10, completed: 20 },
    finish,
  } as AssistantMessage;
}

describe("createSigilHooks", () => {
  let testDedupDir: string;

  beforeEach(async () => {
    testDedupDir = await mkdtemp(join(tmpdir(), "sigil-opencode-test-"));
    process.env.SIGIL_OPENCODE_DEDUP_DIR = testDedupDir;
    mocks.records.length = 0;
    vi.clearAllMocks();
  });

  afterEach(async () => {
    delete process.env.SIGIL_OPENCODE_DEDUP_DIR;
    await rm(testDedupDir, { recursive: true, force: true });
  });

  it("records the effective system prompt from opencode system transform output", async () => {
    const hooks = await createSigilHooks(config, makeClient());
    if (!hooks) throw new Error("expected hooks");

    const parts = [
      { id: "part-user", sessionID: "ses-1", messageID: "user-1", type: "text", text: "hello" },
    ] as Part[];
    hooks.chatMessage({ sessionID: "ses-1" }, { message: makeUserMessage("user-1", "ses-1"), parts });
    hooks.systemTransform(
      { sessionID: "ses-1" },
      { system: ["base opencode prompt", "agent-specific instructions"] },
    );

    await hooks.event({
      event: {
        type: "message.updated",
        properties: { info: makeAssistantMessage("assistant-1", "ses-1", "user-1", "stop") },
      },
    });

    expect(mocks.records[0].seed.systemPrompt).toBe(
      "base opencode prompt\n\nagent-specific instructions",
    );
  });

  it("keeps pending prompt context across intermediate tool-call assistant messages", async () => {
    const hooks = await createSigilHooks(config, makeClient());
    if (!hooks) throw new Error("expected hooks");

    hooks.chatMessage({
      sessionID: "ses-2",
    }, {
      message: makeUserMessage("user-2", "ses-2", "message system prompt"),
      parts: [],
    });

    await hooks.event({
      event: {
        type: "message.updated",
        properties: { info: makeAssistantMessage("assistant-2a", "ses-2", "user-2", "tool-calls") },
      },
    });
    await hooks.event({
      event: {
        type: "message.updated",
        properties: { info: makeAssistantMessage("assistant-2b", "ses-2", "user-2", "stop") },
      },
    });

    expect(mocks.records).toHaveLength(2);
    expect(mocks.records[0].seed.systemPrompt).toBe("message system prompt");
    expect(mocks.records[1].seed.systemPrompt).toBe("message system prompt");
  });

  it("deduplicates repeated terminal events for the same assistant message", async () => {
    const hooks = await createSigilHooks(config, makeClient());
    if (!hooks) throw new Error("expected hooks");

    hooks.chatMessage({
      sessionID: "ses-3",
    }, {
      message: makeUserMessage("user-3", "ses-3", "message system prompt"),
      parts: [],
    });

    const event = {
      event: {
        type: "message.updated",
        properties: { info: makeAssistantMessage("assistant-3", "ses-3", "user-3", "stop") },
      },
    };
    await hooks.event(event);
    await hooks.event(event);

    expect(mocks.records).toHaveLength(1);
    expect(mocks.records[0].seed.id).toBe("assistant-3");
  });
});
