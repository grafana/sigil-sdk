import { beforeEach, describe, expect, it, vi } from "vitest";

const { createSigilClientMock, createTelemetryProvidersMock } = vi.hoisted(
  () => ({
    createSigilClientMock: vi.fn(),
    createTelemetryProvidersMock: vi.fn(),
  }),
);

vi.mock("./client.js", () => ({ createSigilClient: createSigilClientMock }));
vi.mock("./telemetry.js", () => ({
  createTelemetryProviders: createTelemetryProvidersMock,
}));

import type { SigilOpencodeConfig } from "./config.js";
import { createSigilHooks } from "./hooks.js";
import { stableOpencodeGenerationId } from "./lineage.js";

type CapturedGeneration = {
  seed: any;
  firstTokenAt: Date | undefined;
  result: unknown;
  callError: unknown;
};

function makeSigilMock() {
  const generations: CapturedGeneration[] = [];
  const startStreamingGeneration = vi.fn(async (seed: any, run: any) => {
    const entry: CapturedGeneration = {
      seed,
      firstTokenAt: undefined,
      result: undefined,
      callError: undefined,
    };
    generations.push(entry);
    await run({
      setResult: (r: unknown) => {
        entry.result = r;
      },
      setCallError: (e: unknown) => {
        entry.callError = e;
      },
      setFirstTokenAt: (d: Date) => {
        entry.firstTokenAt = d;
      },
      setCacheDiagnostics: vi.fn(),
      end: vi.fn(),
      getError: () => undefined,
    });
  });
  const startGeneration = vi.fn();
  const sigil = {
    startStreamingGeneration,
    startGeneration,
    startToolExecution: vi.fn(() => ({
      setResult: vi.fn(),
      setCallError: vi.fn(),
      end: vi.fn(),
      getError: vi.fn(),
    })),
    flush: vi.fn(async () => {}),
    shutdown: vi.fn(async () => {}),
  };
  return { sigil, generations, startStreamingGeneration, startGeneration };
}

function makeOpencodeClient(parts: any[] = []) {
  return {
    session: { message: vi.fn(async () => ({ data: { parts } })) },
  } as any;
}

function baseConfig(
  overrides: Partial<SigilOpencodeConfig> = {},
): SigilOpencodeConfig {
  return {
    endpoint: "http://127.0.0.1:1/api/v1/generations:export",
    auth: { mode: "none" },
    agentName: "opencode",
    agentVersion: "test-version",
    contentCapture: "full",
    debug: false,
    ...overrides,
  };
}

function assistantMessage(sessionID: string, messageID: string) {
  return {
    id: messageID,
    sessionID,
    role: "assistant",
    time: { created: 1_700_000_001_000, completed: 1_700_000_002_500 },
    parentID: "user-1",
    modelID: "claude-sonnet-4",
    providerID: "anthropic",
    mode: "build",
    path: { cwd: "/repo", root: "/repo" },
    cost: 0.001,
    tokens: {
      input: 10,
      output: 5,
      reasoning: 0,
      cache: { read: 0, write: 0 },
    },
    finish: "end_turn",
  } as const;
}

function textPart(
  sessionID: string,
  messageID: string,
  start: number,
): unknown {
  return {
    id: "p-1",
    sessionID,
    messageID,
    type: "text",
    text: "hello",
    time: { start },
  };
}

async function emitMessageUpdated(
  hooks: NonNullable<Awaited<ReturnType<typeof createSigilHooks>>>,
  msg: unknown,
): Promise<void> {
  await hooks.event({
    event: { type: "message.updated", properties: { info: msg } },
  });
}

async function emitPartUpdated(
  hooks: NonNullable<Awaited<ReturnType<typeof createSigilHooks>>>,
  part: unknown,
): Promise<void> {
  await hooks.event({
    event: { type: "message.part.updated", properties: { part } },
  });
}

async function emitSessionDeleted(
  hooks: NonNullable<Awaited<ReturnType<typeof createSigilHooks>>>,
  sessionID: string,
): Promise<void> {
  await hooks.event({
    event: { type: "session.deleted", properties: { info: { id: sessionID } } },
  });
}

async function emitSessionCreated(
  hooks: NonNullable<Awaited<ReturnType<typeof createSigilHooks>>>,
  id: string,
  parentID?: string,
): Promise<void> {
  await hooks.event({
    event: { type: "session.created", properties: { info: { id, parentID } } },
  });
}

describe("opencode generation lineage and streaming", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("assigns a deterministic opencode- generation id from session and message id", async () => {
    const { sigil, generations } = makeSigilMock();
    createSigilClientMock.mockReturnValue(sigil);
    const hooks = await createSigilHooks(baseConfig(), makeOpencodeClient());
    if (!hooks) throw new Error("expected hooks");

    await emitMessageUpdated(hooks, assistantMessage("sess-det", "msg-1"));

    expect(generations).toHaveLength(1);
    expect(generations[0]!.seed.id).toBe(
      stableOpencodeGenerationId("sess-det", "msg-1"),
    );
  });

  it("exports through startStreamingGeneration, not startGeneration", async () => {
    const { sigil, startStreamingGeneration, startGeneration } =
      makeSigilMock();
    createSigilClientMock.mockReturnValue(sigil);
    const hooks = await createSigilHooks(baseConfig(), makeOpencodeClient());
    if (!hooks) throw new Error("expected hooks");

    await emitMessageUpdated(hooks, assistantMessage("sess-stream", "msg-1"));

    expect(startStreamingGeneration).toHaveBeenCalledTimes(1);
    expect(startGeneration).not.toHaveBeenCalled();
  });

  it("chains two sequential assistant generations via parentGenerationIds", async () => {
    const { sigil, generations } = makeSigilMock();
    createSigilClientMock.mockReturnValue(sigil);
    const hooks = await createSigilHooks(baseConfig(), makeOpencodeClient());
    if (!hooks) throw new Error("expected hooks");

    await emitMessageUpdated(hooks, assistantMessage("sess-chain", "msg-1"));
    await emitMessageUpdated(hooks, assistantMessage("sess-chain", "msg-2"));

    const idA = stableOpencodeGenerationId("sess-chain", "msg-1");
    const idB = stableOpencodeGenerationId("sess-chain", "msg-2");
    expect(generations).toHaveLength(2);
    expect(generations[0]!.seed.id).toBe(idA);
    expect(generations[0]!.seed.parentGenerationIds).toBeUndefined();
    expect(generations[1]!.seed.id).toBe(idB);
    expect(generations[1]!.seed.parentGenerationIds).toEqual([idA]);
  });

  it("re-exporting the same message after a restart keeps the same id and no parent", async () => {
    const { sigil, generations } = makeSigilMock();
    createSigilClientMock.mockReturnValue(sigil);
    const hooks = await createSigilHooks(baseConfig(), makeOpencodeClient());
    if (!hooks) throw new Error("expected hooks");

    await emitMessageUpdated(hooks, assistantMessage("sess-restart", "msg-1"));
    // session.deleted clears the in-process dedup and parent chain, the same
    // way a process restart would, so the next record is a "first" turn.
    await emitSessionDeleted(hooks, "sess-restart");
    await emitMessageUpdated(hooks, assistantMessage("sess-restart", "msg-1"));

    const id = stableOpencodeGenerationId("sess-restart", "msg-1");
    expect(generations).toHaveLength(2);
    expect(generations[0]!.seed.id).toBe(id);
    expect(generations[1]!.seed.id).toBe(id);
    expect(generations[1]!.seed.parentGenerationIds).toBeUndefined();
  });

  it("links a subagent child session's first generation to the parent session's latest generation", async () => {
    const { sigil, generations } = makeSigilMock();
    createSigilClientMock.mockReturnValue(sigil);
    const hooks = await createSigilHooks(baseConfig(), makeOpencodeClient());
    if (!hooks) throw new Error("expected hooks");

    // Parent session runs a turn, then spawns a subagent (child session).
    await emitMessageUpdated(hooks, assistantMessage("sess-parent", "msg-1"));
    await emitSessionCreated(hooks, "sess-child", "sess-parent");
    await emitMessageUpdated(hooks, assistantMessage("sess-child", "msg-c1"));

    const parentId = stableOpencodeGenerationId("sess-parent", "msg-1");
    const childId = stableOpencodeGenerationId("sess-child", "msg-c1");
    expect(generations).toHaveLength(2);
    expect(generations[0]!.seed.id).toBe(parentId);
    expect(generations[1]!.seed.id).toBe(childId);
    // Child's first generation parents onto the spawning session's latest gen.
    expect(generations[1]!.seed.parentGenerationIds).toEqual([parentId]);
  });

  it("keeps intra-session chaining for a child session's later generations", async () => {
    const { sigil, generations } = makeSigilMock();
    createSigilClientMock.mockReturnValue(sigil);
    const hooks = await createSigilHooks(baseConfig(), makeOpencodeClient());
    if (!hooks) throw new Error("expected hooks");

    await emitMessageUpdated(hooks, assistantMessage("sess-parent-2", "msg-1"));
    await emitSessionCreated(hooks, "sess-child-2", "sess-parent-2");
    await emitMessageUpdated(hooks, assistantMessage("sess-child-2", "msg-c1"));
    await emitMessageUpdated(hooks, assistantMessage("sess-child-2", "msg-c2"));

    const childId1 = stableOpencodeGenerationId("sess-child-2", "msg-c1");
    const childId2 = stableOpencodeGenerationId("sess-child-2", "msg-c2");
    // The child's second turn chains to its own first turn, not the parent.
    expect(generations[2]!.seed.id).toBe(childId2);
    expect(generations[2]!.seed.parentGenerationIds).toEqual([childId1]);
  });

  it("freezes the parent link at session.created, not at child-record time", async () => {
    const { sigil, generations } = makeSigilMock();
    createSigilClientMock.mockReturnValue(sigil);
    const hooks = await createSigilHooks(baseConfig(), makeOpencodeClient());
    if (!hooks) throw new Error("expected hooks");

    // Parent turn 1 is recorded, then the subagent is spawned. The parent then
    // records a LATER turn 2 *before* the child's turn is recorded. The child
    // must link to turn 1 — the turn frozen at session.created, the one it was
    // spawned from — not the parent's latest turn 2. Recording turn 2 before
    // the child record is what distinguishes freeze-at-creation from a lazy
    // resolver that would read the parent's current-latest gen (turn 2) at
    // child-record time.
    await emitMessageUpdated(hooks, assistantMessage("sess-parent-3", "msg-1"));
    await emitSessionCreated(hooks, "sess-child-3", "sess-parent-3");
    await emitMessageUpdated(hooks, assistantMessage("sess-parent-3", "msg-2"));
    await emitMessageUpdated(hooks, assistantMessage("sess-child-3", "msg-c1"));

    const parentTurn1 = stableOpencodeGenerationId("sess-parent-3", "msg-1");
    const parentTurn2 = stableOpencodeGenerationId("sess-parent-3", "msg-2");
    const childId = stableOpencodeGenerationId("sess-child-3", "msg-c1");
    const child = generations.find((g) => g.seed.id === childId);
    expect(child?.seed.parentGenerationIds).toEqual([parentTurn1]);
    // Explicitly assert it did NOT pick the parent's later turn.
    expect(child?.seed.parentGenerationIds).not.toEqual([parentTurn2]);
  });

  it("does not link a subagent when the parent has no recorded generation yet", async () => {
    const { sigil, generations } = makeSigilMock();
    createSigilClientMock.mockReturnValue(sigil);
    const hooks = await createSigilHooks(baseConfig(), makeOpencodeClient());
    if (!hooks) throw new Error("expected hooks");

    // session.created arrives before the parent has recorded any generation.
    // No parent generation exists to freeze, so the child records unlinked
    // (fails safe) rather than guessing.
    await emitSessionCreated(hooks, "sess-child-4", "sess-parent-4");
    await emitMessageUpdated(hooks, assistantMessage("sess-child-4", "msg-c1"));

    expect(generations).toHaveLength(1);
    expect(generations[0]!.seed.parentGenerationIds).toBeUndefined();
  });

  it("does not link a root session with no parentID", async () => {
    const { sigil, generations } = makeSigilMock();
    createSigilClientMock.mockReturnValue(sigil);
    const hooks = await createSigilHooks(baseConfig(), makeOpencodeClient());
    if (!hooks) throw new Error("expected hooks");

    await emitSessionCreated(hooks, "sess-root");
    await emitMessageUpdated(hooks, assistantMessage("sess-root", "msg-1"));

    expect(generations).toHaveLength(1);
    expect(generations[0]!.seed.parentGenerationIds).toBeUndefined();
  });

  it("records first-token time from the first streamed part", async () => {
    const { sigil, generations } = makeSigilMock();
    createSigilClientMock.mockReturnValue(sigil);
    const hooks = await createSigilHooks(baseConfig(), makeOpencodeClient());
    if (!hooks) throw new Error("expected hooks");

    await emitPartUpdated(
      hooks,
      textPart("sess-ttft", "msg-1", 1_700_000_001_200),
    );
    // A later part for the same message must not overwrite the first.
    await emitPartUpdated(
      hooks,
      textPart("sess-ttft", "msg-1", 1_700_000_001_900),
    );
    await emitMessageUpdated(hooks, assistantMessage("sess-ttft", "msg-1"));

    expect(generations).toHaveLength(1);
    expect(generations[0]!.firstTokenAt).toEqual(new Date(1_700_000_001_200));
  });

  it("records TTFT in metadata_only without fetching the message body", async () => {
    const { sigil, generations } = makeSigilMock();
    createSigilClientMock.mockReturnValue(sigil);
    const client = makeOpencodeClient();
    const hooks = await createSigilHooks(
      baseConfig({ contentCapture: "metadata_only" }),
      client,
    );
    if (!hooks) throw new Error("expected hooks");

    await emitPartUpdated(
      hooks,
      textPart("sess-ttft-meta", "msg-1", 1_700_000_001_200),
    );
    await emitMessageUpdated(
      hooks,
      assistantMessage("sess-ttft-meta", "msg-1"),
    );

    expect(generations).toHaveLength(1);
    expect(generations[0]!.firstTokenAt).toEqual(new Date(1_700_000_001_200));
    expect(client.session.message).not.toHaveBeenCalled();
  });
});
