import type { AssistantMessage, Part } from "@opencode-ai/sdk";
import type {
  GenerationResult,
  Message,
  ToolDefinition,
} from "@grafana/sigil-sdk-js";
import type { Redactor } from "./redact.js";

export type { GenerationResult };

/**
 * Map user-side parts to Sigil input messages. No redaction applied — user text is the
 * user's own data and Sigil needs it verbatim for prompt analysis. Tier 1 patterns in
 * user text (e.g., pasted connection strings) are a known accepted gap; apply redaction
 * here if this becomes a problem.
 */
export function mapInputMessages(parts: Part[]): Message[] {
  const messages: Message[] = [];
  for (const part of parts) {
    if (part.type === "text" && part.text.trim().length > 0) {
      messages.push({
        role: "user",
        parts: [{ type: "text", text: part.text }],
      });
    }
  }
  return messages;
}

/** Map assistant-side parts to Sigil output messages with redaction. */
export function mapOutputMessages(parts: Part[], redactor: Redactor): Message[] {
  const messages: Message[] = [];
  for (const part of parts) {
    switch (part.type) {
      case "text": {
        const text = redactor.redactLightweight(part.text);
        if (text.trim().length > 0) {
          messages.push({
            role: "assistant",
            parts: [{ type: "text", text }],
          });
        }
        break;
      }
      case "reasoning": {
        const thinking = redactor.redactLightweight(part.text);
        if (thinking.trim().length > 0) {
          messages.push({
            role: "assistant",
            parts: [{ type: "thinking", thinking }],
          });
        }
        break;
      }
      case "tool": {
        const { state } = part;
        if (state.status === "completed") {
          messages.push({
            role: "assistant",
            parts: [{
              type: "tool_call",
              toolCall: {
                id: part.callID,
                name: part.tool,
                inputJSON: redactor.redact(JSON.stringify(state.input ?? {})),
              },
            }],
          });
          messages.push({
            role: "tool",
            parts: [{
              type: "tool_result",
              toolResult: {
                toolCallId: part.callID,
                name: part.tool,
                content: redactor.redact(state.output ?? ""),
              },
            }],
          });
        } else if (state.status === "error") {
          messages.push({
            role: "assistant",
            parts: [{
              type: "tool_call",
              toolCall: {
                id: part.callID,
                name: part.tool,
                inputJSON: redactor.redact(JSON.stringify(state.input ?? {})),
              },
            }],
          });
          messages.push({
            role: "tool",
            parts: [{
              type: "tool_result",
              toolResult: {
                toolCallId: part.callID,
                name: part.tool,
                content: redactor.redact(state.error ?? "unknown error"),
                isError: true,
              },
            }],
          });
        }
        break;
      }
    }
  }
  return messages;
}

/** Convert opencode tool name map to Sigil ToolDefinition array. Only includes enabled tools. */
export function mapToolDefinitions(
  tools: Record<string, boolean> | undefined,
): ToolDefinition[] {
  if (!tools) return [];
  return Object.entries(tools)
    .filter(([, enabled]) => enabled)
    .map(([name]) => ({ name }));
}

/** Map an AssistantMessage + parts to a Sigil GenerationResult with content. */
export function mapGeneration(
  msg: AssistantMessage,
  userParts: Part[],
  assistantParts: Part[],
  redactor: Redactor,
): GenerationResult {
  return {
    input: mapInputMessages(userParts),
    output: mapOutputMessages(assistantParts, redactor),
    usage: {
      inputTokens: msg.tokens.input,
      outputTokens: msg.tokens.output,
      reasoningTokens: msg.tokens.reasoning,
      cacheReadInputTokens: msg.tokens.cache.read,
      cacheCreationInputTokens: msg.tokens.cache.write,
    },
    responseModel: msg.modelID,
    stopReason: msg.finish,
    completedAt: msg.time.completed ? new Date(msg.time.completed) : undefined,
    metadata: {
      cost: msg.cost,
    },
  };
}

export function mapError(
  error: NonNullable<AssistantMessage["error"]>,
): Error {
  switch (error.name) {
    case "ProviderAuthError":
      return new Error("provider_auth");
    case "APIError":
      return new Error(`api_error: ${error.data.statusCode ?? "unknown"}`);
    case "MessageOutputLengthError":
      return new Error("output_length_exceeded");
    case "MessageAbortedError":
      return new Error("aborted");
    case "UnknownError":
      return new Error("unknown_error");
    default: {
      const _exhaustive: never = error;
      return new Error("unknown_error");
    }
  }
}
