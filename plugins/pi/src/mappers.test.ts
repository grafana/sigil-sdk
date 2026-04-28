import { describe, expect, it } from "vitest";
import {
  mapGenerationResult,
  mapGenerationStart,
  mapToolNames,
  type PiAssistantMessage,
  type PiToolResult,
  type ToolTiming,
} from "./mappers.js";

function makeMsg(overrides?: Partial<PiAssistantMessage>): PiAssistantMessage {
  return {
    role: "assistant",
    content: [{ type: "text", text: "Hello world" }],
    provider: "anthropic",
    model: "claude-sonnet-4-20250514",
    responseId: "resp-1",
    usage: {
      input: 100,
      output: 50,
      cacheRead: 10,
      cacheWrite: 5,
      totalTokens: 165,
      cost: {
        input: 0.003,
        output: 0.006,
        cacheRead: 0.001,
        cacheWrite: 0.002,
        total: 0.012,
      },
    },
    stopReason: "stop",
    timestamp: 1700000001000,
    ...overrides,
  };
}

function makeToolResult(overrides?: Partial<PiToolResult>): PiToolResult {
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

function makeTiming(overrides?: Partial<ToolTiming>): ToolTiming {
  return {
    toolCallId: "call-1",
    toolName: "bash",
    startedAt: 1700000000500,
    completedAt: 1700000001500,
    isError: false,
    ...overrides,
  };
}

describe("mapGenerationStart", () => {
  it("maps model, conversation, agent info", () => {
    const msg = makeMsg();
    const start = mapGenerationStart(
      msg,
      "session-abc",
      "pi",
      "1.0.0",
      1700000000000,
      undefined,
    );
    expect(start.model).toEqual({
      provider: "anthropic",
      name: "claude-sonnet-4-20250514",
    });
    expect(start.conversationId).toBe("session-abc");
    expect(start.agentName).toBe("pi");
    expect(start.agentVersion).toBe("1.0.0");
    expect(start.startedAt).toEqual(new Date(1700000000000));
  });

  it("includes tools whenever provided", () => {
    const msg = makeMsg();
    const tools = [{ name: "bash" }, { name: "read" }];

    const result = mapGenerationStart(msg, "s", "pi", undefined, 0, tools);
    expect(result.tools).toEqual(tools);
  });

  it("sets thinkingEnabled when message has a thinking block", () => {
    const msg = makeMsg({
      content: [
        { type: "thinking", thinking: "let me think..." },
        { type: "text", text: "answer" },
      ],
    });
    const start = mapGenerationStart(
      msg,
      undefined,
      "pi",
      undefined,
      0,
      undefined,
    );
    expect(start.thinkingEnabled).toBe(true);
  });

  it("omits thinkingEnabled when no thinking blocks present", () => {
    const start = mapGenerationStart(
      makeMsg(),
      undefined,
      "pi",
      undefined,
      0,
      undefined,
    );
    expect(start.thinkingEnabled).toBeUndefined();
  });
});

describe("mapGenerationResult", () => {
  it("maps usage and metadata (no content)", () => {
    const msg = makeMsg();
    const result = mapGenerationResult(msg, [], "metadata_only");

    expect(result.usage).toEqual({
      inputTokens: 100,
      outputTokens: 50,
      totalTokens: 165,
      cacheReadInputTokens: 10,
      cacheCreationInputTokens: 5,
    });
    expect(result.responseModel).toBe("claude-sonnet-4-20250514");
    expect(result.stopReason).toBe("end_turn");
    expect(result.completedAt).toEqual(new Date(1700000001000));
    expect(result.metadata?.cost_usd).toBe(0.012);
  });

  it("uses provider-reported totalTokens (includes cache)", () => {
    const msg = makeMsg({
      usage: {
        input: 100,
        output: 50,
        cacheRead: 200,
        cacheWrite: 30,
        totalTokens: 380,
        cost: {
          input: 0,
          output: 0,
          cacheRead: 0,
          cacheWrite: 0,
          total: 0,
        },
      },
    });
    const result = mapGenerationResult(msg, [], "metadata_only");
    expect(result.usage?.totalTokens).toBe(380);
  });

  it("metadata_only produces no output", () => {
    const result = mapGenerationResult(
      makeMsg(),
      [makeToolResult()],
      "metadata_only",
    );
    expect(result.output).toBeUndefined();
  });

  it("no_tool_content emits assistant text/thinking but skips tool blocks", () => {
    const msg = makeMsg({
      content: [
        { type: "text", text: "I'll run that command" },
        { type: "thinking", thinking: "deciding" },
        {
          type: "toolCall",
          id: "c1",
          name: "bash",
          arguments: { command: "ls" },
        },
      ],
    });
    const toolResults = [
      makeToolResult({
        toolCallId: "c1",
        content: [{ type: "text", text: "file.txt" }],
      }),
    ];
    const result = mapGenerationResult(msg, toolResults, "no_tool_content");

    // Only assistant text + thinking (2 messages), no tool_call or tool_result.
    expect(result.output).toHaveLength(2);
    const partTypes = result.output?.flatMap((m) =>
      m.parts?.map((p) => p.type),
    );
    expect(partTypes).toContain("text");
    expect(partTypes).toContain("thinking");
    expect(partTypes).not.toContain("tool_call");
    expect(result.output?.some((m) => m.role === "tool")).toBe(false);
  });

  it("full mode emits assistant text, tool_call, and tool_result", () => {
    const msg = makeMsg({
      content: [
        { type: "text", text: "I'll run that command" },
        {
          type: "toolCall",
          id: "c1",
          name: "bash",
          arguments: { command: "ls" },
        },
      ],
    });
    const toolResults = [
      makeToolResult({
        toolCallId: "c1",
        content: [{ type: "text", text: "file.txt" }],
      }),
    ];
    const result = mapGenerationResult(msg, toolResults, "full");

    expect(result.output).toHaveLength(3);
    const roles = result.output?.map((m) => m.role);
    expect(roles).toContain("assistant");
    expect(roles).toContain("tool");
  });

  it("skips redacted thinking blocks", () => {
    const msg = makeMsg({
      content: [
        { type: "thinking", thinking: "encrypted-blob", redacted: true },
        { type: "text", text: "result" },
      ],
    });
    const result = mapGenerationResult(msg, [], "full");
    const allText = result.output
      ?.map(
        (m) =>
          m.parts
            ?.map((p) => {
              if (p.type === "text") return p.text;
              if (p.type === "thinking") return p.thinking;
              return "";
            })
            .join("") ?? "",
      )
      .join("");
    expect(allText).not.toContain("encrypted-blob");
    expect(allText).toContain("result");
  });

  it("maps stop reasons", () => {
    expect(
      mapGenerationResult(makeMsg({ stopReason: "stop" }), [], "metadata_only")
        .stopReason,
    ).toBe("end_turn");
    expect(
      mapGenerationResult(
        makeMsg({ stopReason: "length" }),
        [],
        "metadata_only",
      ).stopReason,
    ).toBe("max_tokens");
    expect(
      mapGenerationResult(
        makeMsg({ stopReason: "toolUse" }),
        [],
        "metadata_only",
      ).stopReason,
    ).toBe("tool_use");
    expect(
      mapGenerationResult(makeMsg({ stopReason: "error" }), [], "metadata_only")
        .stopReason,
    ).toBe("error");
  });
});

describe("mapToolNames", () => {
  it("deduplicates tool names", () => {
    const timings = [
      makeTiming({ toolName: "bash" }),
      makeTiming({ toolName: "read" }),
      makeTiming({ toolName: "bash" }),
    ];
    const defs = mapToolNames(timings);
    expect(defs).toEqual([{ name: "bash" }, { name: "read" }]);
  });

  it("returns empty for no timings", () => {
    expect(mapToolNames([])).toEqual([]);
  });
});
