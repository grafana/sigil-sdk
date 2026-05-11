import type {
  ContentCaptureMode,
  GenerationResult,
  GenerationStart,
  Message,
  ToolDefinition,
} from "@grafana/sigil-sdk-js";

/**
 * Pi's AssistantMessage shape from @mariozechner/pi-ai.
 * Declared here to avoid hard import (pi types are external at runtime).
 */
export interface PiAssistantMessage {
  role: "assistant";
  content: PiContentBlock[];
  provider: string;
  model: string;
  responseId?: string;
  usage: {
    input: number;
    output: number;
    cacheRead: number;
    cacheWrite: number;
    totalTokens: number;
    cost?: {
      input: number;
      output: number;
      cacheRead: number;
      cacheWrite: number;
      total: number;
    };
  };
  stopReason: string;
  errorMessage?: string;
  timestamp: number;
}

export type PiContentBlock =
  | { type: "text"; text: string }
  | { type: "thinking"; thinking: string; redacted?: boolean }
  | {
      type: "toolCall";
      id: string;
      name: string;
      arguments: Record<string, unknown>;
    };

/** Pi's TextContent / ImageContent / UserMessage shapes from @mariozechner/pi-ai. */
export interface PiTextContent {
  type: "text";
  text: string;
}

export interface PiImageContent {
  type: "image";
  data: string;
  mimeType: string;
}

export interface PiUserMessage {
  role: "user";
  content: string | (PiTextContent | PiImageContent)[];
  timestamp: number;
}

export interface PiToolResult {
  role: "toolResult";
  toolCallId: string;
  toolName: string;
  content: Array<{ type: string; text?: string }>;
  details?: unknown;
  isError: boolean;
  timestamp: number;
}

export interface ToolTiming {
  toolCallId: string;
  toolName: string;
  startedAt: number;
  completedAt: number;
  isError: boolean;
}

/** Build the GenerationStart seed from an assistant message and context. */
export function mapGenerationStart(
  msg: PiAssistantMessage,
  conversationId: string | undefined,
  agentName: string,
  agentVersion: string | undefined,
  turnStartTime: number,
  tools: ToolDefinition[] | undefined,
): GenerationStart {
  const start: GenerationStart = {
    conversationId,
    agentName,
    agentVersion,
    effectiveVersion: agentVersion,
    model: { provider: msg.provider, name: msg.model },
    startedAt: new Date(turnStartTime),
    ...(tools && tools.length > 0 ? { tools } : {}),
  };
  if (msg.content.some((b) => b.type === "thinking")) {
    start.thinkingEnabled = true;
  }
  return start;
}

/**
 * Build the GenerationResult from an assistant message.
 *
 * `completedAtMs` should be the time the provider stream finished
 * (assistant `message_end`). `msg.timestamp` is unreliable as a completion
 * marker: pi providers set it via `Date.now()` when constructing the
 * assistant message object — i.e. *before* the API request is sent — so
 * it is closer to a start timestamp than an end timestamp. Falls back to
 * `msg.timestamp` only when no end timestamp was observed.
 */
export function mapGenerationResult(
  msg: PiAssistantMessage,
  toolResults: PiToolResult[],
  contentCapture: ContentCaptureMode,
  input?: Message[],
  completedAtMs?: number,
): GenerationResult {
  const result: GenerationResult = {
    responseId: msg.responseId,
    responseModel: msg.model,
    usage: {
      inputTokens: msg.usage.input,
      outputTokens: msg.usage.output,
      totalTokens: msg.usage.totalTokens,
      cacheReadInputTokens: msg.usage.cacheRead,
      cacheCreationInputTokens: msg.usage.cacheWrite,
    },
    stopReason: mapStopReason(msg.stopReason),
    completedAt: new Date(completedAtMs ?? msg.timestamp),
    metadata:
      msg.usage.cost !== undefined ? { cost_usd: msg.usage.cost.total } : {},
  };

  if (input && input.length > 0) {
    result.input = input;
  }

  // Always emit structural tool_call / tool_result parts so the SDK can count
  // them for the `gen_ai.client.tool_calls_per_operation` histogram. Body
  // content (assistant text, tool args, tool results) is included per
  // contentCapture; in `metadata_only` the SDK strips content before export.
  const output: Message[] = [
    ...mapAssistantOutput(msg, contentCapture),
    ...mapToolResultsOutput(toolResults, contentCapture),
  ];
  if (output.length > 0) {
    result.output = output;
  }

  return result;
}

/**
 * Map a pi user message to a Sigil input Message. Returns null in
 * `metadata_only` (mirrors how assistant text/thinking is dropped) and for
 * empty/whitespace-only content. Image parts are skipped because Sigil's
 * `MessagePart` union has no image type; multiple text parts are joined with
 * a newline.
 */
export function mapUserMessage(
  msg: PiUserMessage,
  contentCapture: ContentCaptureMode,
): Message | null {
  if (contentCapture === "metadata_only") return null;

  let text: string;
  if (typeof msg.content === "string") {
    text = msg.content;
  } else {
    text = msg.content
      .filter((c): c is PiTextContent => c.type === "text")
      .map((c) => c.text)
      .join("\n");
  }

  if (text.trim().length === 0) return null;

  return {
    role: "user",
    parts: [{ type: "text", text }],
  };
}

/** Map tool names used in this turn to ToolDefinition[]. */
export function mapToolNames(toolTimings: ToolTiming[]): ToolDefinition[] {
  const seen = new Set<string>();
  const defs: ToolDefinition[] = [];
  for (const t of toolTimings) {
    if (!seen.has(t.toolName)) {
      seen.add(t.toolName);
      defs.push({ name: t.toolName });
    }
  }
  return defs;
}

/**
 * Map assistant message content blocks to Sigil output messages.
 * - text/thinking parts: only when contentCapture allows body content.
 * - tool_call parts: always emitted (structure needed for the SDK's
 *   tool_calls_per_operation metric); inputJSON is only filled in `full` mode.
 */
function mapAssistantOutput(
  msg: PiAssistantMessage,
  contentCapture: ContentCaptureMode,
): Message[] {
  const messages: Message[] = [];
  const includeBodies = contentCapture !== "metadata_only";

  for (const block of msg.content) {
    switch (block.type) {
      case "text": {
        if (includeBodies && block.text.trim().length > 0) {
          messages.push({
            role: "assistant",
            parts: [{ type: "text", text: block.text }],
          });
        }
        break;
      }
      case "thinking": {
        if (block.redacted) break;
        if (includeBodies && block.thinking.trim().length > 0) {
          messages.push({
            role: "assistant",
            parts: [{ type: "thinking", thinking: block.thinking }],
          });
        }
        break;
      }
      case "toolCall": {
        messages.push({
          role: "assistant",
          parts: [
            {
              type: "tool_call",
              toolCall: {
                id: block.id,
                name: block.name,
                inputJSON:
                  contentCapture === "full"
                    ? JSON.stringify(block.arguments)
                    : "",
              },
            },
          ],
        });
        break;
      }
    }
  }

  return messages;
}

/**
 * Map pi tool results to Sigil tool result messages. Always emits the
 * structural part; body content is included only in `full` mode.
 */
function mapToolResultsOutput(
  toolResults: PiToolResult[],
  contentCapture: ContentCaptureMode,
): Message[] {
  const messages: Message[] = [];
  const includeBody = contentCapture === "full";

  for (const tr of toolResults) {
    let content = "";
    if (includeBody) {
      content = tr.content
        .filter(
          (c): c is { type: "text"; text: string } =>
            c.type === "text" && !!c.text,
        )
        .map((c) => c.text)
        .join("\n");
    }

    messages.push({
      role: "tool",
      parts: [
        {
          type: "tool_result",
          toolResult: {
            toolCallId: tr.toolCallId,
            name: tr.toolName,
            content,
            isError: tr.isError,
          },
        },
      ],
    });
  }

  return messages;
}

function mapStopReason(reason: string): string {
  switch (reason) {
    case "stop":
      return "end_turn";
    case "length":
      return "max_tokens";
    case "toolUse":
      return "tool_use";
    case "error":
      return "error";
    case "aborted":
      return "aborted";
    default:
      return reason;
  }
}
