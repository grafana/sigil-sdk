import { mkdir, open, unlink } from "fs/promises";
import { homedir } from "os";
import { join } from "path";
import type { SigilClient } from "@grafana/sigil-sdk-js";
import type { AssistantMessage, UserMessage, Part } from "@opencode-ai/sdk";
import type { PluginInput } from "@opencode-ai/plugin";
import type { SigilConfig } from "./config.js";
import { createSigilClient } from "./client.js";
import { Redactor } from "./redact.js";
import { mapGeneration, mapError, mapToolDefinitions } from "./mappers.js";

type OpencodeClient = PluginInput["client"];

// Track recorded messages per session for dedup and cleanup
const recordedMessages = new Map<string, Set<string>>();

// Pending generation store: user-side data captured before assistant responds
type PendingGeneration = {
  systemPrompt: string | undefined;
  userParts: Part[];
  tools: Record<string, boolean> | undefined;
};
const pendingGenerations = new Map<string, PendingGeneration>();
const sessionPendingMessages = new Map<string, Set<string>>();
const latestSystemPrompts = new Map<string, string>();

function dedupDir(): string {
  return process.env.SIGIL_OPENCODE_DEDUP_DIR ??
    join(homedir(), ".cache", "opencode", "sigil-recorded");
}

function buildAgentName(prefix: string | undefined, mode: string | undefined): string {
  const base = prefix || "opencode";
  return mode ? `${base}:${mode}` : base;
}

function safePathPart(value: string): string {
  return value.replace(/[^A-Za-z0-9_.-]/g, "_");
}

function dedupMarkerPath(sessionID: string, messageID: string): string {
  return join(dedupDir(), `${safePathPart(sessionID)}-${safePathPart(messageID)}.lock`);
}

async function claimRecordedMessage(sessionID: string, messageID: string): Promise<boolean> {
  const sessionSet = recordedMessages.get(sessionID) ?? new Set<string>();
  if (sessionSet.has(messageID)) return false;

  try {
    await mkdir(dedupDir(), { recursive: true });
    const marker = await open(dedupMarkerPath(sessionID, messageID), "wx");
    await marker.close();
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === "EEXIST") {
      return false;
    }
    // If filesystem dedup is unavailable, keep the existing in-process guard.
  }

  sessionSet.add(messageID);
  recordedMessages.set(sessionID, sessionSet);
  return true;
}

async function releaseRecordedMessage(sessionID: string, messageID: string): Promise<void> {
  recordedMessages.get(sessionID)?.delete(messageID);
  try {
    await unlink(dedupMarkerPath(sessionID, messageID));
  } catch {
    // Best effort cleanup; a stale marker is preferable to duplicate exports.
  }
}

function joinSystemPrompt(system: string[] | undefined): string | undefined {
  const prompt = system?.filter((part) => part.trim().length > 0).join("\n\n");
  return prompt && prompt.length > 0 ? prompt : undefined;
}

/**
 * Called from the chat.message hook. Stores user-side data for later use
 * when the assistant message completes.
 */
function handleChatMessage(
  input: { sessionID: string },
  output: { message: UserMessage; parts: Part[] },
): void {
  const messageID = output.message.id;
  pendingGenerations.set(messageID, {
    systemPrompt: output.message.system ?? latestSystemPrompts.get(input.sessionID),
    userParts: output.parts,
    tools: output.message.tools,
  });
  const sessionSet = sessionPendingMessages.get(input.sessionID) ?? new Set<string>();
  sessionSet.add(messageID);
  sessionPendingMessages.set(input.sessionID, sessionSet);
}

function handleSystemTransform(
  input: { sessionID?: string },
  output: { system: string[] },
): void {
  if (!input.sessionID) return;
  const systemPrompt = joinSystemPrompt(output.system);
  if (!systemPrompt) return;

  latestSystemPrompts.set(input.sessionID, systemPrompt);
  for (const messageID of sessionPendingMessages.get(input.sessionID) ?? []) {
    const pending = pendingGenerations.get(messageID);
    if (pending) {
      pending.systemPrompt = systemPrompt;
    }
  }
}

function shouldClearPendingGeneration(msg: AssistantMessage): boolean {
  if (msg.error) return true;
  return msg.finish !== "tool-calls";
}

function clearPendingGeneration(sessionID: string, messageID: string | undefined): void {
  if (!messageID) return;
  pendingGenerations.delete(messageID);
  const sessionSet = sessionPendingMessages.get(sessionID);
  if (!sessionSet) return;
  sessionSet.delete(messageID);
  if (sessionSet.size === 0) {
    sessionPendingMessages.delete(sessionID);
  }
}

async function handleEvent(
  sigil: SigilClient,
  config: SigilConfig,
  client: OpencodeClient,
  redactor: Redactor,
  event: { type: string; properties: unknown },
): Promise<void> {
  if (event.type !== "message.updated") return;

  const properties = event.properties as { info?: { role?: string } } | undefined;
  const msg = properties?.info;
  if (!msg || msg.role !== "assistant") return;

  const assistantMsg = msg as AssistantMessage;

  // Only record terminal messages
  const isTerminal = assistantMsg.finish || assistantMsg.error || assistantMsg.time.completed;
  if (!isTerminal) return;

  // Dedup across repeated events and multiple live opencode processes.
  if (!(await claimRecordedMessage(assistantMsg.sessionID, assistantMsg.id))) return;

  // Look up pending generation (user-side data)
  const pending = pendingGenerations.get(assistantMsg.parentID);

  // Fetch assistant parts via REST
  let assistantParts: Part[] = [];
  try {
    const response = await client.session.message({
      path: { id: assistantMsg.sessionID, messageID: assistantMsg.id },
    });
    assistantParts = response.data?.parts ?? [];
  } catch {
    // REST fetch failed — fall back to metadata-only
  }

  const contentCapture = config.contentCapture ?? true;

  const seed = {
    id: assistantMsg.id,
    conversationId: assistantMsg.sessionID,
    agentName: buildAgentName(config.agentName, assistantMsg.mode),
    agentVersion: config.agentVersion,
    effectiveVersion: config.agentVersion,
    model: { provider: assistantMsg.providerID, name: assistantMsg.modelID },
    startedAt: new Date(assistantMsg.time.created),
    ...(contentCapture && {
      systemPrompt: pending?.systemPrompt,
      tools: mapToolDefinitions(pending?.tools),
    }),
  };

  // When contentCapture is enabled, map full content with redaction;
  // otherwise fall back to metadata-only result (no message content).
  const result = contentCapture
    ? mapGeneration(assistantMsg, pending?.userParts ?? [], assistantParts, redactor)
    : mapGeneration(assistantMsg, [], [], redactor);

  try {
    if (assistantMsg.error) {
      await sigil.startGeneration(seed, async (recorder) => {
        recorder.setResult(result);
        recorder.setCallError(mapError(assistantMsg.error!));
      });
    } else {
      await sigil.startGeneration(seed, async (recorder) => {
        recorder.setResult(result);
      });
    }
  } catch {
    await releaseRecordedMessage(assistantMsg.sessionID, assistantMsg.id);
    // Sigil recording failure should never break the plugin
  }

  // Keep user-side context through intermediate tool-call assistant messages.
  if (shouldClearPendingGeneration(assistantMsg)) {
    clearPendingGeneration(assistantMsg.sessionID, assistantMsg.parentID);
  }
}

async function handleLifecycle(
  sigil: SigilClient,
  event: { type: string; properties: unknown },
): Promise<void> {
  const type = event.type as string;

  if (type === "session.idle") {
    try {
      await sigil.flush();
    } catch {
      // flush failure is non-fatal
    }
  }

  if (type === "session.deleted") {
    const properties = event.properties as { info?: { id?: string } } | undefined;
    const sessionId = properties?.info?.id;
    if (sessionId) {
      recordedMessages.delete(sessionId);
      for (const messageID of sessionPendingMessages.get(sessionId) ?? []) {
        pendingGenerations.delete(messageID);
      }
      sessionPendingMessages.delete(sessionId);
      latestSystemPrompts.delete(sessionId);
    }
  }

  if (type === "global.disposed") {
    try {
      await sigil.shutdown();
    } catch {
      // shutdown failure is non-fatal
    }
  }
}

export type SigilHooks = {
  event: (input: { event: { type: string; properties: unknown } }) => Promise<void>;
  chatMessage: (
    input: { sessionID: string },
    output: { message: UserMessage; parts: Part[] },
  ) => void;
  systemTransform: (
    input: { sessionID?: string },
    output: { system: string[] },
  ) => void;
};

export async function createSigilHooks(
  config: SigilConfig,
  client: OpencodeClient,
): Promise<SigilHooks | null> {
  if (!config.enabled) return null;

  if (!config.endpoint) {
    console.warn("[sigil] endpoint is required when enabled -- skipping Sigil initialization");
    return null;
  }

  const sigil = createSigilClient(config);
  if (!sigil) return null;

  const redactor = new Redactor();

  process.on("beforeExit", () => {
    sigil.shutdown().catch(() => {});
  });

  return {
    event: async (input) => {
      await handleEvent(sigil, config, client, redactor, input.event);
      await handleLifecycle(sigil, input.event);
    },
    chatMessage: (input, output) => {
      handleChatMessage(input, output);
    },
    systemTransform: (input, output) => {
      handleSystemTransform(input, output);
    },
  };
}
