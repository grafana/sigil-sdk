import { beforeEach, describe, expect, it, vi } from "vitest";

const { loadConfigMock, createSigilClientMock } = vi.hoisted(() => ({
  loadConfigMock: vi.fn(),
  createSigilClientMock: vi.fn(),
}));

vi.mock("./config.js", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./config.js")>();
  return {
    ...actual,
    loadConfig: loadConfigMock,
  };
});

vi.mock("./client.js", () => ({
  createSigilClient: createSigilClientMock,
}));

import type { SigilClient } from "@grafana/sigil-sdk-js";
import registerExtension, { emitToolSpans } from "./index.js";
import type {
  PiAssistantMessage,
  PiToolResult,
  ToolTiming,
} from "./mappers.js";

interface RecorderLike {
  setResult: (value: unknown) => void;
  setCallError: (error: Error) => void;
}

interface ToolRecorderLike {
  setResult: ReturnType<typeof vi.fn>;
  setCallError: ReturnType<typeof vi.fn>;
  end: ReturnType<typeof vi.fn>;
  getError: ReturnType<typeof vi.fn>;
}

interface SigilLike {
  startGeneration: (
    seed: unknown,
    run: (recorder: RecorderLike) => Promise<void>,
  ) => Promise<void>;
  startToolExecution: ReturnType<typeof vi.fn>;
  shutdown: () => Promise<void>;
}

class FakePi {
  handlers = new Map<string, (event: any, ctx: any) => Promise<void> | void>();

  on(event: string, handler: (event: any, ctx: any) => Promise<void> | void) {
    this.handlers.set(event, handler);
  }

  async emit(event: string, payload: any = {}, ctx: any = defaultCtx) {
    const handler = this.handlers.get(event);
    if (!handler) return;
    await handler(payload, ctx);
  }
}

const defaultCtx = {
  sessionManager: {
    getSessionFile: () => "session-1",
    getSessionId: () => "sess-default-id",
  },
};

function makeCtx({
  sessionFile,
  sessionId,
}: {
  sessionFile?: string | (() => string | undefined);
  sessionId: string | (() => string);
}) {
  const fileFn =
    typeof sessionFile === "function"
      ? sessionFile
      : () => sessionFile ?? "session-1";
  const idFn = typeof sessionId === "function" ? sessionId : () => sessionId;
  return {
    sessionManager: {
      getSessionFile: fileFn,
      getSessionId: idFn,
    },
  };
}

function assistantMessage() {
  return {
    role: "assistant",
    content: [{ type: "text", text: "hello" }],
    provider: "anthropic",
    model: "claude-sonnet-4",
    usage: {
      input: 10,
      output: 20,
      cacheRead: 0,
      cacheWrite: 0,
      totalTokens: 30,
      cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 },
    },
    stopReason: "stop",
    timestamp: Date.now(),
  };
}

describe("extension lifecycle", () => {
  beforeEach(() => {
    loadConfigMock.mockReset();
    createSigilClientMock.mockReset();
  });

  it("handles the happy path and exports one generation with user input", async () => {
    const recorder = {
      setResult: vi.fn(),
      setCallError: vi.fn(),
    };

    const sigil: SigilLike = {
      startGeneration: vi.fn(async (_seed, run) => {
        await run(recorder);
      }),
      startToolExecution: vi.fn(() => ({
        setResult: vi.fn(),
        setCallError: vi.fn(),
        end: vi.fn(),
        getError: vi.fn(),
      })),
      shutdown: vi.fn(async () => {}),
    };

    loadConfigMock.mockResolvedValue({
      endpoint: "http://localhost:8080/api/v1/generations:export",
      auth: { mode: "none" },
      agentName: "pi",
      contentCapture: "full",
    });
    createSigilClientMock.mockReturnValue(sigil);

    const pi = new FakePi();
    registerExtension(pi as any);

    await pi.emit("session_start");
    await pi.emit("turn_start");
    await pi.emit("message_end", {
      message: { role: "user", content: "hey", timestamp: Date.now() },
    });
    await pi.emit("tool_execution_start", {
      toolCallId: "c1",
      toolName: "read",
    });
    await pi.emit("tool_execution_end", { toolCallId: "c1", isError: false });
    await pi.emit("turn_end", { message: assistantMessage(), toolResults: [] });

    expect(sigil.startGeneration).toHaveBeenCalledTimes(1);
    expect(recorder.setResult).toHaveBeenCalledTimes(1);
    expect(recorder.setCallError).not.toHaveBeenCalled();

    const result = recorder.setResult.mock.calls[0]![0] as {
      input?: Array<{
        role: string;
        parts?: Array<{ type: string; text?: string }>;
      }>;
    };
    expect(result.input).toBeDefined();
    expect(result.input).toHaveLength(1);
    expect(result.input?.[0]?.role).toBe("user");
    expect(result.input?.[0]?.parts?.[0]).toEqual({
      type: "text",
      text: "hey",
    });
  });

  it("leaves input empty on a tool-loop continuation with no user message_end", async () => {
    const recorder = {
      setResult: vi.fn(),
      setCallError: vi.fn(),
    };

    const sigil: SigilLike = {
      startGeneration: vi.fn(async (_seed, run) => {
        await run(recorder);
      }),
      startToolExecution: vi.fn(() => ({
        setResult: vi.fn(),
        setCallError: vi.fn(),
        end: vi.fn(),
        getError: vi.fn(),
      })),
      shutdown: vi.fn(async () => {}),
    };

    loadConfigMock.mockResolvedValue({
      endpoint: "http://localhost:8080/api/v1/generations:export",
      auth: { mode: "none" },
      agentName: "pi",
      contentCapture: "full",
    });
    createSigilClientMock.mockReturnValue(sigil);

    const pi = new FakePi();
    registerExtension(pi as any);

    await pi.emit("session_start");

    // Turn 1: user types, assistant calls a tool.
    await pi.emit("turn_start");
    await pi.emit("message_end", {
      message: { role: "user", content: "hey", timestamp: Date.now() },
    });
    await pi.emit("turn_end", { message: assistantMessage(), toolResults: [] });

    // Turn 2: agent loop continues with no new user input.
    await pi.emit("turn_start");
    await pi.emit("turn_end", { message: assistantMessage(), toolResults: [] });

    expect(sigil.startGeneration).toHaveBeenCalledTimes(2);
    expect(recorder.setResult).toHaveBeenCalledTimes(2);

    const turn1 = recorder.setResult.mock.calls[0]![0] as { input?: unknown[] };
    const turn2 = recorder.setResult.mock.calls[1]![0] as { input?: unknown[] };
    expect(turn1.input).toBeDefined();
    expect(turn1.input).toHaveLength(1);
    expect(turn2.input).toBeUndefined();
  });

  it("clamps startedAt when msg.timestamp precedes turnStartTime", async () => {
    let capturedSeed: any;
    const recorder = {
      setResult: vi.fn(),
      setCallError: vi.fn(),
    };

    const sigil: SigilLike = {
      startGeneration: vi.fn(async (seed, run) => {
        capturedSeed = seed;
        await run(recorder);
      }),
      startToolExecution: vi.fn(() => ({
        setResult: vi.fn(),
        setCallError: vi.fn(),
        end: vi.fn(),
        getError: vi.fn(),
      })),
      shutdown: vi.fn(async () => {}),
    };

    loadConfigMock.mockResolvedValue({
      endpoint: "http://localhost:8080/api/v1/generations:export",
      auth: { mode: "none" },
      agentName: "pi",
      contentCapture: "metadata_only",
    });
    createSigilClientMock.mockReturnValue(sigil);

    const pi = new FakePi();
    registerExtension(pi as any);

    await pi.emit("session_start");
    await pi.emit("turn_start");

    // Simulate msg.timestamp that is earlier than turnStartTime
    // (can happen with clock adjustments)
    const msg = assistantMessage();
    msg.timestamp = Date.now() - 5000;

    await pi.emit("turn_end", { message: msg, toolResults: [] });

    expect(sigil.startGeneration).toHaveBeenCalledTimes(1);
    // startedAt must be <= completedAt
    const startedAt = capturedSeed.startedAt.getTime();
    const completedAt = msg.timestamp;
    expect(startedAt).toBeLessThanOrEqual(completedAt);
  });

  it("emits tool execution spans on turn_end", async () => {
    const toolRecorders: ToolRecorderLike[] = [];
    const recorder = {
      setResult: vi.fn(),
      setCallError: vi.fn(),
    };

    const sigil: SigilLike = {
      startGeneration: vi.fn(async (_seed, run) => {
        await run(recorder);
      }),
      startToolExecution: vi.fn(() => {
        const tr: ToolRecorderLike = {
          setResult: vi.fn(),
          setCallError: vi.fn(),
          end: vi.fn(),
          getError: vi.fn(),
        };
        toolRecorders.push(tr);
        return tr;
      }),
      shutdown: vi.fn(async () => {}),
    };

    loadConfigMock.mockResolvedValue({
      endpoint: "http://localhost:8080/api/v1/generations:export",
      auth: { mode: "none" },
      agentName: "pi",
      contentCapture: "metadata_only",
    });
    createSigilClientMock.mockReturnValue(sigil);

    const pi = new FakePi();
    registerExtension(pi as any);

    await pi.emit("session_start");
    await pi.emit("turn_start");
    await pi.emit("tool_execution_start", {
      toolCallId: "c1",
      toolName: "read",
    });
    await pi.emit("tool_execution_end", { toolCallId: "c1", isError: false });
    await pi.emit("tool_execution_start", {
      toolCallId: "c2",
      toolName: "write",
    });
    await pi.emit("tool_execution_end", { toolCallId: "c2", isError: true });

    const msg = assistantMessage();
    (msg as any).content = [
      { type: "toolCall", id: "c1", name: "read", arguments: { path: "a.go" } },
      {
        type: "toolCall",
        id: "c2",
        name: "write",
        arguments: { path: "b.go" },
      },
    ];

    await pi.emit("turn_end", { message: msg, toolResults: [] });

    expect(sigil.startToolExecution).toHaveBeenCalledTimes(2);
    expect(toolRecorders).toHaveLength(2);
    expect(toolRecorders[0]!.end).toHaveBeenCalled();
    expect(toolRecorders[1]!.end).toHaveBeenCalled();
    expect(toolRecorders[1]!.setCallError).toHaveBeenCalled();
  });

  it("swallows sigil failures instead of throwing", async () => {
    const sigil = {
      startGeneration: vi.fn(async () => {
        throw new Error("transport down");
      }),
      shutdown: vi.fn(async () => {}),
    };

    loadConfigMock.mockResolvedValue({
      endpoint: "http://localhost:8080/api/v1/generations:export",
      auth: { mode: "none" },
      agentName: "pi",
      contentCapture: "metadata_only",
    });
    createSigilClientMock.mockReturnValue(sigil);

    const pi = new FakePi();
    registerExtension(pi as any);

    await pi.emit("session_start");
    await pi.emit("turn_start");

    await expect(
      pi.emit("turn_end", { message: assistantMessage(), toolResults: [] }),
    ).resolves.toBeUndefined();
  });

  it("warns and skips when assistant message shape is invalid", async () => {
    const recorder = {
      setResult: vi.fn(),
      setCallError: vi.fn(),
    };
    const sigil: SigilLike = {
      startGeneration: vi.fn(async (_seed, run) => {
        await run(recorder);
      }),
      startToolExecution: vi.fn(),
      shutdown: vi.fn(async () => {}),
    };

    loadConfigMock.mockResolvedValue({
      endpoint: "http://localhost:8080/api/v1/generations:export",
      auth: { mode: "none" },
      agentName: "pi",
      contentCapture: "metadata_only",
    });
    createSigilClientMock.mockReturnValue(sigil);

    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const pi = new FakePi();
    registerExtension(pi as any);

    await pi.emit("session_start");
    await pi.emit("turn_start");
    // Missing required fields (e.g. usage, content) — should fail validation.
    await pi.emit("turn_end", {
      message: { role: "assistant" },
      toolResults: [],
    });

    expect(sigil.startGeneration).not.toHaveBeenCalled();
    expect(warn).toHaveBeenCalledWith(
      expect.stringContaining("did not validate"),
    );
    warn.mockRestore();
  });

  it("uses sessionId, not file basename, as conversationId", async () => {
    // Two distinct pi sessions whose session files share a basename
    // (e.g. extensions that spawn child sessions under <root>/run-N/session.jsonl)
    // must emit distinct conversationIds.
    const seeds: Array<{ conversationId?: string }> = [];
    const recorder = { setResult: vi.fn(), setCallError: vi.fn() };
    const sigil: SigilLike = {
      startGeneration: vi.fn(async (seed, run) => {
        seeds.push(seed as { conversationId?: string });
        await run(recorder);
      }),
      startToolExecution: vi.fn(() => ({
        setResult: vi.fn(),
        setCallError: vi.fn(),
        end: vi.fn(),
        getError: vi.fn(),
      })),
      shutdown: vi.fn(async () => {}),
    };

    loadConfigMock.mockResolvedValue({
      endpoint: "http://localhost:8080/api/v1/generations:export",
      auth: { mode: "none" },
      agentName: "pi",
      contentCapture: "metadata_only",
    });
    createSigilClientMock.mockReturnValue(sigil);

    const pi = new FakePi();
    registerExtension(pi as any);

    // Session 1: filename basename === "session.jsonl", uuid AAA
    const ctxA = makeCtx({
      sessionFile: "/tmp/runs/run-0/session.jsonl",
      sessionId: "019dd89e-ffad-76ae-9f80-454acd646039",
    });
    await pi.emit("session_start", {}, ctxA);
    await pi.emit("turn_start", {}, ctxA);
    await pi.emit(
      "turn_end",
      {
        message: assistantMessage(),
        toolResults: [],
      },
      ctxA,
    );
    await pi.emit("session_shutdown", {}, ctxA);

    // Session 2: same basename, different uuid BBB
    const ctxB = makeCtx({
      sessionFile: "/tmp/runs/run-1/session.jsonl",
      sessionId: "019de579-98b4-7619-9157-8a6a4f61d487",
    });
    await pi.emit("session_start", {}, ctxB);
    await pi.emit("turn_start", {}, ctxB);
    await pi.emit(
      "turn_end",
      {
        message: assistantMessage(),
        toolResults: [],
      },
      ctxB,
    );

    expect(seeds).toHaveLength(2);
    expect(seeds[0]!.conversationId).toBe(
      "019dd89e-ffad-76ae-9f80-454acd646039",
    );
    expect(seeds[1]!.conversationId).toBe(
      "019de579-98b4-7619-9157-8a6a4f61d487",
    );
    expect(seeds[0]!.conversationId).not.toBe(seeds[1]!.conversationId);
  });

  it("refreshes conversationId per turn when sessionId changes mid-life", async () => {
    // SessionManager reassigns this.sessionId on fork/branch
    // (session-manager.js:927,961). The plugin must observe the current
    // sessionId at every turn_end, not just at session_start.
    const seeds: Array<{ conversationId?: string }> = [];
    const recorder = { setResult: vi.fn(), setCallError: vi.fn() };
    const sigil: SigilLike = {
      startGeneration: vi.fn(async (seed, run) => {
        seeds.push(seed as { conversationId?: string });
        await run(recorder);
      }),
      startToolExecution: vi.fn(() => ({
        setResult: vi.fn(),
        setCallError: vi.fn(),
        end: vi.fn(),
        getError: vi.fn(),
      })),
      shutdown: vi.fn(async () => {}),
    };

    loadConfigMock.mockResolvedValue({
      endpoint: "http://localhost:8080/api/v1/generations:export",
      auth: { mode: "none" },
      agentName: "pi",
      contentCapture: "metadata_only",
    });
    createSigilClientMock.mockReturnValue(sigil);

    const pi = new FakePi();
    registerExtension(pi as any);

    let currentId = "id-before-fork";
    const ctx = makeCtx({
      sessionFile: "/tmp/sess/session.jsonl",
      sessionId: () => currentId,
    });

    await pi.emit("session_start", {}, ctx);
    await pi.emit("turn_start", {}, ctx);
    await pi.emit(
      "turn_end",
      {
        message: assistantMessage(),
        toolResults: [],
      },
      ctx,
    );

    // Simulate fork/branch: sessionManager swaps sessionId.
    currentId = "id-after-fork";

    await pi.emit("turn_start", {}, ctx);
    await pi.emit(
      "turn_end",
      {
        message: assistantMessage(),
        toolResults: [],
      },
      ctx,
    );

    expect(seeds).toHaveLength(2);
    expect(seeds[0]!.conversationId).toBe("id-before-fork");
    expect(seeds[1]!.conversationId).toBe("id-after-fork");
  });

  it("yields undefined conversationId when sessionId is empty (no-session mode)", async () => {
    // session-manager.js:430 initializes sessionId to "" before newSession()
    // runs, and --no-session never assigns one. We must not emit a literal
    // empty string as the conversationId.
    let capturedSeed: { conversationId?: string } | undefined;
    const recorder = { setResult: vi.fn(), setCallError: vi.fn() };
    const sigil: SigilLike = {
      startGeneration: vi.fn(async (seed, run) => {
        capturedSeed = seed as { conversationId?: string };
        await run(recorder);
      }),
      startToolExecution: vi.fn(() => ({
        setResult: vi.fn(),
        setCallError: vi.fn(),
        end: vi.fn(),
        getError: vi.fn(),
      })),
      shutdown: vi.fn(async () => {}),
    };

    loadConfigMock.mockResolvedValue({
      endpoint: "http://localhost:8080/api/v1/generations:export",
      auth: { mode: "none" },
      agentName: "pi",
      contentCapture: "metadata_only",
    });
    createSigilClientMock.mockReturnValue(sigil);

    const pi = new FakePi();
    registerExtension(pi as any);

    const ctx = makeCtx({ sessionFile: undefined, sessionId: "" });

    await pi.emit("session_start", {}, ctx);
    await pi.emit("turn_start", {}, ctx);
    await pi.emit(
      "turn_end",
      {
        message: assistantMessage(),
        toolResults: [],
      },
      ctx,
    );

    expect(capturedSeed).toBeDefined();
    expect(capturedSeed!.conversationId).toBeUndefined();
  });
});

// --- Unit tests for emitToolSpans ---

function makePiMsg(
  overrides?: Partial<PiAssistantMessage>,
): PiAssistantMessage {
  return {
    role: "assistant",
    content: [{ type: "text", text: "Hello" }],
    provider: "anthropic",
    model: "claude-sonnet-4-20250514",
    usage: {
      input: 100,
      output: 50,
      cacheRead: 0,
      cacheWrite: 0,
      totalTokens: 150,
      cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 },
    },
    stopReason: "toolUse",
    timestamp: 1700000001000,
    ...overrides,
  };
}

function makePiTiming(overrides?: Partial<ToolTiming>): ToolTiming {
  return {
    toolCallId: "call-1",
    toolName: "bash",
    startedAt: 1700000000500,
    completedAt: 1700000001500,
    isError: false,
    ...overrides,
  };
}

function makePiToolResult(overrides?: Partial<PiToolResult>): PiToolResult {
  return {
    role: "toolResult",
    toolCallId: "call-1",
    toolName: "bash",
    content: [{ type: "text", text: "output" }],
    isError: false,
    timestamp: 1700000002000,
    ...overrides,
  };
}

function mockSigilClient() {
  const recorders: Array<{
    start: Record<string, unknown>;
    result: Record<string, unknown> | undefined;
    callError: unknown;
    ended: boolean;
  }> = [];

  const client = {
    startToolExecution: vi.fn((start: Record<string, unknown>) => {
      const rec = {
        start,
        result: undefined as Record<string, unknown> | undefined,
        callError: undefined as unknown,
        ended: false,
      };
      recorders.push(rec);
      return {
        setResult: vi.fn((r: Record<string, unknown>) => {
          rec.result = r;
        }),
        setCallError: vi.fn((e: unknown) => {
          rec.callError = e;
        }),
        end: vi.fn(() => {
          rec.ended = true;
        }),
        getError: vi.fn(() => undefined),
      };
    }),
  } as unknown as SigilClient;

  return { client, recorders };
}

describe("emitToolSpans", () => {
  it("does nothing when no timings", () => {
    const { client, recorders } = mockSigilClient();
    emitToolSpans(client, makePiMsg(), [], [], {
      agentName: "pi",
      contentCapture: "metadata_only",
    });
    expect(recorders).toHaveLength(0);
  });

  it("creates a span per tool timing", () => {
    const { client, recorders } = mockSigilClient();
    const msg = makePiMsg({
      content: [
        { type: "toolCall", id: "c1", name: "bash", arguments: { cmd: "ls" } },
        {
          type: "toolCall",
          id: "c2",
          name: "read",
          arguments: { path: "a.go" },
        },
      ],
    });

    emitToolSpans(
      client,
      msg,
      [],
      [
        makePiTiming({ toolCallId: "c1", toolName: "bash" }),
        makePiTiming({ toolCallId: "c2", toolName: "read" }),
      ],
      { agentName: "pi", contentCapture: "metadata_only" },
    );

    expect(recorders).toHaveLength(2);
    expect(recorders[0]!.start).toMatchObject({
      toolName: "bash",
      toolCallId: "c1",
      toolType: "function",
    });
    expect(recorders[1]!.start).toMatchObject({
      toolName: "read",
      toolCallId: "c2",
      toolType: "function",
    });
    expect(recorders.every((r) => r.ended)).toBe(true);
  });

  it("passes model and agent context", () => {
    const { client, recorders } = mockSigilClient();
    emitToolSpans(
      client,
      makePiMsg(),
      [],
      [makePiTiming({ toolCallId: "c1" })],
      {
        conversationId: "conv-42",
        agentName: "pi",
        agentVersion: "2.0.0",
        contentCapture: "metadata_only",
      },
    );

    expect(recorders[0]!.start).toMatchObject({
      conversationId: "conv-42",
      agentName: "pi",
      agentVersion: "2.0.0",
      requestModel: "claude-sonnet-4-20250514",
      requestProvider: "anthropic",
    });
  });

  it("includes arguments and results with content capture", () => {
    const { client, recorders } = mockSigilClient();
    const msg = makePiMsg({
      content: [
        { type: "toolCall", id: "c1", name: "bash", arguments: { cmd: "ls" } },
      ],
    });
    const toolResults = [
      makePiToolResult({
        toolCallId: "c1",
        content: [{ type: "text", text: "file.txt" }],
      }),
    ];

    emitToolSpans(
      client,
      msg,
      toolResults,
      [makePiTiming({ toolCallId: "c1" })],
      {
        agentName: "pi",
        contentCapture: "full",
      },
    );

    expect(recorders[0]!.result?.arguments).toBe('{"cmd":"ls"}');
    expect(recorders[0]!.result?.result).toBe("file.txt");
  });

  it("omits content when contentCapture is off", () => {
    const { client, recorders } = mockSigilClient();
    const msg = makePiMsg({
      content: [
        { type: "toolCall", id: "c1", name: "bash", arguments: { cmd: "ls" } },
      ],
    });

    emitToolSpans(
      client,
      msg,
      [makePiToolResult({ toolCallId: "c1" })],
      [makePiTiming({ toolCallId: "c1" })],
      {
        agentName: "pi",
        contentCapture: "metadata_only",
      },
    );

    expect(recorders[0]!.result?.arguments).toBeUndefined();
    expect(recorders[0]!.result?.result).toBeUndefined();
  });

  it("marks error tool executions", () => {
    const { client, recorders } = mockSigilClient();
    emitToolSpans(
      client,
      makePiMsg(),
      [],
      [makePiTiming({ toolCallId: "c1", isError: true })],
      { agentName: "pi", contentCapture: "metadata_only" },
    );

    expect(recorders[0]!.callError).toBeInstanceOf(Error);
  });

  it("uses real start/end times", () => {
    const { client, recorders } = mockSigilClient();
    emitToolSpans(
      client,
      makePiMsg(),
      [],
      [makePiTiming({ startedAt: 1000, completedAt: 5000 })],
      { agentName: "pi", contentCapture: "metadata_only" },
    );

    expect(recorders[0]!.start).toMatchObject({ startedAt: new Date(1000) });
    expect(recorders[0]!.result?.completedAt).toEqual(new Date(5000));
  });
});
