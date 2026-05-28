import { createServer, type Server } from "node:http";
import type { SigilClient } from "@grafana/sigil-sdk-js";
import type { Part } from "@opencode-ai/sdk";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { SigilOpencodeConfig } from "./config.js";
import {
  _peekToolExecutionState,
  _resetToolExecutionState,
  createSigilHooks,
  emitToolSpans,
  mergeToolSpanRecords,
  type ToolExecutionRecord,
  toolSpansFromParts,
} from "./hooks.js";
import { Redactor } from "./redact.js";

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

function makeRecord(
  overrides?: Partial<ToolExecutionRecord>,
): ToolExecutionRecord {
  return {
    sessionID: "sess-1",
    toolName: "Bash",
    toolCallId: "call-1",
    startedAt: 1_700_000_001_000,
    completedAt: 1_700_000_002_000,
    input: { command: "ls" },
    output: "ok",
    ...overrides,
  };
}

const defaultOpts = () => ({
  conversationId: "sess-1",
  agentName: "opencode:build",
  agentVersion: "test-version",
  requestProvider: "anthropic",
  requestModel: "claude-sonnet-4-opencode",
  redactor: new Redactor(),
  debugLog: () => {},
});

describe("emitToolSpans", () => {
  it("does nothing when no records", () => {
    const { client, recorders } = mockSigilClient();
    emitToolSpans(client, [], {
      ...defaultOpts(),
      contentCapture: "metadata_only",
    });
    expect(recorders).toHaveLength(0);
  });

  it("creates a span per completed record with full context", () => {
    const { client, recorders } = mockSigilClient();
    emitToolSpans(
      client,
      [
        makeRecord({ toolCallId: "c1", toolName: "Bash" }),
        makeRecord({ toolCallId: "c2", toolName: "Read" }),
      ],
      { ...defaultOpts(), contentCapture: "metadata_only" },
    );

    expect(recorders).toHaveLength(2);
    expect(recorders[0]!.start).toMatchObject({
      toolName: "Bash",
      toolCallId: "c1",
      toolType: "function",
      conversationId: "sess-1",
      agentName: "opencode:build",
      agentVersion: "test-version",
      requestProvider: "anthropic",
      requestModel: "claude-sonnet-4-opencode",
    });
    expect(recorders[1]!.start).toMatchObject({
      toolName: "Read",
      toolCallId: "c2",
    });
    expect(recorders.every((r) => r.ended)).toBe(true);
  });

  it("uses real start/end times", () => {
    const { client, recorders } = mockSigilClient();
    emitToolSpans(
      client,
      [makeRecord({ startedAt: 1000, completedAt: 5000 })],
      { ...defaultOpts(), contentCapture: "metadata_only" },
    );

    expect(recorders[0]!.start).toMatchObject({ startedAt: new Date(1000) });
    expect(recorders[0]!.result?.completedAt).toEqual(new Date(5000));
  });

  it("omits arguments and result in metadata_only", () => {
    const { client, recorders } = mockSigilClient();
    emitToolSpans(client, [makeRecord()], {
      ...defaultOpts(),
      contentCapture: "metadata_only",
    });

    expect(recorders[0]!.result?.arguments).toBeUndefined();
    expect(recorders[0]!.result?.result).toBeUndefined();
  });

  it("omits arguments and result in no_tool_content", () => {
    const { client, recorders } = mockSigilClient();
    emitToolSpans(client, [makeRecord()], {
      ...defaultOpts(),
      contentCapture: "no_tool_content",
    });

    expect(recorders[0]!.result?.arguments).toBeUndefined();
    expect(recorders[0]!.result?.result).toBeUndefined();
  });

  it("includes redacted arguments and result in full", () => {
    const { client, recorders } = mockSigilClient();
    emitToolSpans(
      client,
      [
        makeRecord({
          input: { command: "echo glc_abcdefghijklmnopqrstuv" },
          output: "leaked glc_abcdefghijklmnopqrstuv done",
        }),
      ],
      { ...defaultOpts(), contentCapture: "full" },
    );

    expect(recorders[0]!.result?.arguments).toBe(
      '{"command":"echo [REDACTED:grafana-cloud-token]"}',
    );
    expect(recorders[0]!.result?.result).toBe(
      "leaked [REDACTED:grafana-cloud-token] done",
    );
  });

  it("marks error records with setCallError", () => {
    const { client, recorders } = mockSigilClient();
    emitToolSpans(client, [makeRecord({ isError: true, error: "boom" })], {
      ...defaultOpts(),
      contentCapture: "metadata_only",
    });

    expect(recorders[0]!.callError).toBeInstanceOf(Error);
    expect((recorders[0]!.callError as Error).message).toBe("boom");
  });

  it("swallows SDK errors so a span failure does not break the plugin", () => {
    const failing = {
      startToolExecution: () => {
        throw new Error("nope");
      },
    } as unknown as SigilClient;
    expect(() =>
      emitToolSpans(failing, [makeRecord()], {
        ...defaultOpts(),
        contentCapture: "metadata_only",
      }),
    ).not.toThrow();
  });
});

describe("toolSpansFromParts", () => {
  it("returns completed and error records with terminal timing", () => {
    const parts: Part[] = [
      {
        id: "p1",
        sessionID: "sess-1",
        messageID: "m1",
        type: "tool",
        callID: "c1",
        tool: "Bash",
        state: {
          status: "completed",
          input: { command: "ls" },
          output: "main.go",
          title: "ls",
          metadata: {},
          time: { start: 100, end: 200 },
        },
      } as Part,
      {
        id: "p2",
        sessionID: "sess-1",
        messageID: "m1",
        type: "tool",
        callID: "c2",
        tool: "Read",
        state: {
          status: "error",
          input: { path: "/missing" },
          error: "ENOENT",
          time: { start: 300, end: 400 },
        },
      } as Part,
      {
        id: "p3",
        sessionID: "sess-1",
        messageID: "m1",
        type: "tool",
        callID: "c3",
        tool: "Pending",
        state: { status: "pending", input: {}, raw: "" },
      } as Part,
      {
        id: "p4",
        sessionID: "sess-1",
        messageID: "m1",
        type: "text",
        text: "hello",
      } as Part,
    ];

    const records = toolSpansFromParts("sess-1", parts);
    expect(records).toEqual([
      {
        sessionID: "sess-1",
        toolName: "Bash",
        toolCallId: "c1",
        startedAt: 100,
        completedAt: 200,
        input: { command: "ls" },
        output: "main.go",
      },
      {
        sessionID: "sess-1",
        toolName: "Read",
        toolCallId: "c2",
        startedAt: 300,
        completedAt: 400,
        input: { path: "/missing" },
        isError: true,
        error: "ENOENT",
      },
    ]);
  });
});

describe("mergeToolSpanRecords", () => {
  it("prefers terminal records, keeps unique hook records", () => {
    const term = [
      makeRecord({ toolCallId: "c1", startedAt: 1, completedAt: 2 }),
    ];
    const hook = [
      makeRecord({ toolCallId: "c1", startedAt: 10, completedAt: 20 }),
      makeRecord({ toolCallId: "c2", startedAt: 30, completedAt: 40 }),
    ];

    const merged = mergeToolSpanRecords(term, hook);
    expect(merged).toHaveLength(2);
    expect(merged[0]).toMatchObject({
      toolCallId: "c1",
      startedAt: 1,
      completedAt: 2,
    });
    expect(merged[1]).toMatchObject({
      toolCallId: "c2",
      startedAt: 30,
      completedAt: 40,
    });
  });

  it("scopes the dedup key by session", () => {
    const term = [makeRecord({ sessionID: "sess-a", toolCallId: "shared" })];
    const hook = [
      makeRecord({ sessionID: "sess-b", toolCallId: "shared", startedAt: 99 }),
    ];

    const merged = mergeToolSpanRecords(term, hook);
    expect(merged).toHaveLength(2);
  });
});

function startResponseServer(
  response: Record<string, unknown>,
): Promise<{ server: Server; baseUrl: string }> {
  return new Promise((resolve) => {
    const server = createServer((_req, res) => {
      res.setHeader("Content-Type", "application/json");
      res.end(JSON.stringify(response));
    });
    server.listen(0, "127.0.0.1", () => {
      const addr = server.address();
      if (!addr || typeof addr === "string") {
        throw new Error("expected AddressInfo from server.address()");
      }
      resolve({ server, baseUrl: `http://127.0.0.1:${addr.port}` });
    });
  });
}

function closeServer(server: Server): Promise<void> {
  return new Promise((resolve, reject) => {
    server.close((err) => (err ? reject(err) : resolve()));
  });
}

describe("hook lifecycle records and guard denial", () => {
  const servers: Server[] = [];

  beforeEach(() => {
    _resetToolExecutionState();
  });

  afterEach(async () => {
    await Promise.all(servers.splice(0).map(closeServer));
    _resetToolExecutionState();
  });

  it("toolExecuteBefore/After move an active record into completed", async () => {
    const config: SigilOpencodeConfig = {
      endpoint: "http://127.0.0.1:1",
      auth: { mode: "none" },
      agentName: "opencode",
      agentVersion: "test-version",
      contentCapture: "full",
      debug: false,
      // guards disabled, so the before hook just records timing.
    };

    const hooks = await createSigilHooks(config, {
      session: { message: async () => ({ data: { parts: [] } }) },
    } as never);
    if (!hooks) throw new Error("expected hooks");

    await hooks.toolExecuteBefore(
      { sessionID: "sess-1", callID: "call-1", tool: "Bash" },
      { args: { command: "ls" } },
    );

    expect(_peekToolExecutionState().active).toHaveLength(1);

    hooks.toolExecuteAfter(
      {
        sessionID: "sess-1",
        callID: "call-1",
        tool: "Bash",
        args: { command: "ls" },
      },
      { title: "ls", output: "main.go", metadata: {} },
    );

    const peeked = _peekToolExecutionState();
    expect(peeked.active).toHaveLength(0);
    expect(peeked.completed).toHaveLength(1);
    expect(peeked.completed[0]).toMatchObject({
      sessionID: "sess-1",
      toolCallId: "call-1",
      toolName: "Bash",
      input: { command: "ls" },
      output: "main.go",
    });
    expect(peeked.completed[0]!.completedAt).toBeGreaterThan(0);
  });

  it("clears the active record when the guard denies the tool", async () => {
    const guardSrv = await startResponseServer({
      action: "deny",
      reason: "blocked",
      evaluations: [],
    });
    servers.push(guardSrv.server);

    const config: SigilOpencodeConfig = {
      endpoint: guardSrv.baseUrl,
      auth: { mode: "none" },
      agentName: "opencode",
      agentVersion: "test-version",
      contentCapture: "full",
      debug: false,
      guards: { enabled: true, timeoutMs: 1500, failOpen: false },
    };

    const hooks = await createSigilHooks(config, {
      session: { message: async () => ({ data: { parts: [] } }) },
    } as never);
    if (!hooks) throw new Error("expected hooks");

    await expect(
      hooks.toolExecuteBefore(
        { sessionID: "sess-1", callID: "call-1", tool: "Bash" },
        { args: { command: "ls" } },
      ),
    ).rejects.toThrow("blocked");

    expect(_peekToolExecutionState().active).toHaveLength(0);

    // The after hook for a denied call should be a no-op since the active
    // record was already cleared.
    hooks.toolExecuteAfter(
      {
        sessionID: "sess-1",
        callID: "call-1",
        tool: "Bash",
        args: { command: "ls" },
      },
      { title: "ls", output: "ignored", metadata: {} },
    );
    expect(_peekToolExecutionState().completed).toHaveLength(0);

    await hooks.event({ event: { type: "global.disposed", properties: {} } });
  });

  it("clears active and completed records on session.deleted", async () => {
    const config: SigilOpencodeConfig = {
      endpoint: "http://127.0.0.1:1",
      auth: { mode: "none" },
      agentName: "opencode",
      agentVersion: "test-version",
      contentCapture: "full",
      debug: false,
    };

    const hooks = await createSigilHooks(config, {
      session: { message: async () => ({ data: { parts: [] } }) },
    } as never);
    if (!hooks) throw new Error("expected hooks");

    // Active record for sess-1.
    await hooks.toolExecuteBefore(
      { sessionID: "sess-1", callID: "call-1", tool: "Bash" },
      { args: { command: "ls" } },
    );
    // Completed record for sess-1.
    await hooks.toolExecuteBefore(
      { sessionID: "sess-1", callID: "call-2", tool: "Read" },
      { args: { path: "/x" } },
    );
    hooks.toolExecuteAfter(
      {
        sessionID: "sess-1",
        callID: "call-2",
        tool: "Read",
        args: { path: "/x" },
      },
      { title: "/x", output: "ok", metadata: {} },
    );
    // Active record for an unrelated session that must survive cleanup.
    await hooks.toolExecuteBefore(
      { sessionID: "sess-2", callID: "call-3", tool: "Bash" },
      { args: { command: "pwd" } },
    );

    expect(_peekToolExecutionState().active).toHaveLength(2);
    expect(_peekToolExecutionState().completed).toHaveLength(1);

    await hooks.event({
      event: {
        type: "session.deleted",
        properties: { info: { id: "sess-1" } },
      },
    });

    const peeked = _peekToolExecutionState();
    expect(peeked.completed).toHaveLength(0);
    expect(peeked.active).toHaveLength(1);
    expect(peeked.active[0]).toMatchObject({
      sessionID: "sess-2",
      toolCallId: "call-3",
    });
  });
});
