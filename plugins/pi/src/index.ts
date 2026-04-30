import { readFileSync } from "node:fs";
import { basename, dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import type {
  ContentCaptureMode,
  Message,
  SigilClient,
} from "@grafana/sigil-sdk-js";
import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";
import { createSigilClient } from "./client.js";
import type { SigilPiConfig } from "./config.js";
import { loadConfig } from "./config.js";
import {
  mapGenerationResult,
  mapGenerationStart,
  mapToolNames,
  mapUserMessage,
  type PiAssistantMessage,
  type PiToolResult,
  type PiUserMessage,
  type ToolTiming,
} from "./mappers.js";
import {
  createTelemetryProviders,
  type TelemetryProviders,
} from "./telemetry.js";

function detectPiVersion(): string | undefined {
  try {
    // Resolve an exported subpath via ESM resolution, then walk up to package.json.
    // createRequire won't work here: pi's package.json uses "import"-only exports.
    const resolved = import.meta.resolve("@mariozechner/pi-coding-agent/hooks");
    // import.meta.resolve is sync on Node ≥20.6; older versions return a
    // Promise. Bail out in that case rather than passing a Promise downstream.
    if (typeof resolved !== "string") return undefined;
    let dir = dirname(fileURLToPath(resolved));
    for (let i = 0; i < 5; i++) {
      try {
        const pkgPath = join(dir, "package.json");
        const pkg = JSON.parse(readFileSync(pkgPath, "utf-8")) as {
          name?: string;
          version?: string;
        };
        if (pkg.name === "@mariozechner/pi-coding-agent") return pkg.version;
      } catch {
        // no package.json at this level, keep walking
      }
      dir = dirname(dir);
    }
    return undefined;
  } catch {
    return undefined;
  }
}

export default function (pi: ExtensionAPI) {
  let sigil: SigilClient | null = null;
  let config: SigilPiConfig | null = null;
  let telemetry: TelemetryProviders | null = null;

  let turnStartTime = 0;
  let conversationId: string | undefined;

  function debugLog(msg: string) {
    if (config?.debug) console.error(`[sigil-pi] ${msg}`);
  }

  // Tool execution timing: toolCallId → start timestamp
  const toolStarts = new Map<string, { toolName: string; startedAt: number }>();
  const turnToolTimings: ToolTiming[] = [];

  // User messages observed since the previous turn_end. Consumed at the next
  // turn_end and attached to GenerationResult.input. Filled by the
  // `message_end` handler (user role only). Per pi's agent loop, `turn_start`
  // fires BEFORE the user `message_end` for fresh prompts, so this buffer
  // must NOT be cleared at turn_start — only after consume and on session
  // boundaries.
  const pendingInputMessages: Message[] = [];

  function resetTurnState() {
    turnStartTime = 0;
    toolStarts.clear();
    turnToolTimings.length = 0;
  }

  async function resetSessionState() {
    config = null;
    conversationId = undefined;
    if (telemetry) {
      try {
        await telemetry.shutdown();
      } catch (err) {
        console.warn("[sigil-pi] telemetry shutdown failed:", err);
      }
      telemetry = null;
    }
    resetTurnState();
    pendingInputMessages.length = 0;
  }

  pi.on("session_start", async (_event, ctx) => {
    try {
      if (sigil) {
        try {
          await sigil.shutdown();
        } catch (err) {
          console.warn("[sigil-pi] stale client shutdown failed:", err);
        }
      }

      sigil = null;
      await resetSessionState();

      config = await loadConfig();
      if (!config) return;

      if (!config.agentVersion) {
        config = { ...config, agentVersion: detectPiVersion() };
      }
      const sessionFile = ctx.sessionManager.getSessionFile();
      conversationId = sessionFile ? basename(sessionFile) : undefined;

      // Set up OTel providers if OTLP is configured
      if (config.otlp) {
        try {
          telemetry = createTelemetryProviders(config.otlp);
        } catch (err) {
          console.warn("[sigil-pi] failed to create OTel providers:", err);
        }
      }

      sigil = createSigilClient(config, {
        tracer: telemetry?.tracer,
        meter: telemetry?.meter,
      });
      if (!sigil) {
        await resetSessionState();
        return;
      }

      debugLog(`enabled, endpoint=${config.endpoint} auth=${config.auth.mode}`);
    } catch (err) {
      console.warn("[sigil-pi] session_start failed:", err);
      sigil = null;
      await resetSessionState();
    }
  });

  pi.on("turn_start", async (_event, _ctx) => {
    resetTurnState();
    if (!sigil) return;
    turnStartTime = Date.now();
  });

  pi.on("message_end", async (event, _ctx) => {
    if (!sigil || !config) return;
    try {
      const message = (event as { message?: unknown }).message;
      if (!isUserMessage(message)) return;
      const mapped = mapUserMessage(message, config.contentCapture);
      if (mapped) pendingInputMessages.push(mapped);
    } catch (err) {
      console.warn("[sigil-pi] message_end failed:", err);
    }
  });

  pi.on("tool_execution_start", async (event, _ctx) => {
    if (!sigil) return;

    try {
      toolStarts.set(event.toolCallId, {
        toolName: event.toolName,
        startedAt: Date.now(),
      });
    } catch (err) {
      console.warn("[sigil-pi] tool_execution_start failed:", err);
    }
  });

  pi.on("tool_execution_end", async (event, _ctx) => {
    if (!sigil) return;

    try {
      const start = toolStarts.get(event.toolCallId);
      if (!start) return;
      toolStarts.delete(event.toolCallId);

      turnToolTimings.push({
        toolCallId: event.toolCallId,
        toolName: start.toolName,
        startedAt: start.startedAt,
        completedAt: Date.now(),
        isError: event.isError,
      });
    } catch (err) {
      console.warn("[sigil-pi] tool_execution_end failed:", err);
    }
  });

  pi.on("turn_end", async (event, _ctx) => {
    if (!sigil || !config) return;

    try {
      if (!isAssistantMessage(event.message)) {
        console.warn(
          "[sigil-pi] turn_end: assistant message shape did not validate, skipping",
        );
        return;
      }

      const msg = event.message;
      const contentCapture = config.contentCapture;
      const toolDefs = mapToolNames(turnToolTimings);

      // Ensure startedAt <= completedAt (msg.timestamp). The provider sets
      // msg.timestamp via Date.now() when creating the message object, which
      // is normally after turn_start, but clock adjustments can invert them.
      const completedAtMs = msg.timestamp;
      const startedAtMs = Math.min(
        turnStartTime || completedAtMs,
        completedAtMs,
      );

      const seed = mapGenerationStart(
        msg,
        conversationId,
        config.agentName,
        config.agentVersion,
        startedAtMs,
        toolDefs.length > 0 ? toolDefs : undefined,
      );

      const toolResults = (event.toolResults ?? []) as PiToolResult[];
      // Snapshot the buffer; the finally below clears it in place and
      // GenerationResult.input would otherwise alias the cleared array.
      const result = mapGenerationResult(
        msg,
        toolResults,
        contentCapture,
        pendingInputMessages.slice(),
      );

      await sigil.startGeneration(seed, async (recorder) => {
        recorder.setResult(result);
        if (msg.errorMessage) {
          recorder.setCallError(new Error(msg.errorMessage));
        }

        // sigil and config are guaranteed non-null by the guard at the top of this handler.
        emitToolSpans(sigil as SigilClient, msg, toolResults, turnToolTimings, {
          conversationId,
          agentName: (config as SigilPiConfig).agentName,
          agentVersion: (config as SigilPiConfig).agentVersion,
          contentCapture,
        });
      });
      debugLog(
        `generation queued, model=${msg.model} tokens=${msg.usage.totalTokens}`,
      );
    } catch (err) {
      console.warn("[sigil-pi] turn_end failed:", err);
    } finally {
      toolStarts.clear();
      turnToolTimings.length = 0;
      pendingInputMessages.length = 0;
    }
  });

  pi.on("session_shutdown", async (_event, _ctx) => {
    if (sigil) {
      try {
        await sigil.shutdown();
      } catch (err) {
        console.warn("[sigil-pi] session shutdown failed:", err);
      }
    }

    sigil = null;
    await resetSessionState();
  });
}

/** @internal Exported for testing. */
export function emitToolSpans(
  client: SigilClient,
  msg: PiAssistantMessage,
  toolResults: PiToolResult[],
  timings: ToolTiming[],
  opts: {
    conversationId?: string;
    agentName: string;
    agentVersion?: string;
    contentCapture: ContentCaptureMode;
  },
): void {
  if (timings.length === 0) return;

  const includeContent = opts.contentCapture === "full";

  const argsMap = new Map<string, Record<string, unknown>>();
  const resultMap = new Map<string, string>();
  if (includeContent) {
    for (const block of msg.content) {
      if (block.type === "toolCall") {
        argsMap.set(block.id, block.arguments);
      }
    }
    for (const tr of toolResults) {
      const text = tr.content
        .filter(
          (c): c is { type: "text"; text: string } =>
            c.type === "text" && !!c.text,
        )
        .map((c) => c.text)
        .join("\n");
      resultMap.set(tr.toolCallId, text);
    }
  }

  for (const timing of timings) {
    try {
      const toolRec = client.startToolExecution({
        toolName: timing.toolName,
        toolCallId: timing.toolCallId,
        toolType: "function",
        conversationId: opts.conversationId,
        agentName: opts.agentName,
        agentVersion: opts.agentVersion,
        requestModel: msg.model,
        requestProvider: msg.provider,
        startedAt: new Date(timing.startedAt),
        contentCapture: opts.contentCapture,
      });

      const end: {
        arguments?: unknown;
        result?: unknown;
        completedAt: Date;
      } = {
        completedAt: new Date(timing.completedAt),
      };

      if (includeContent) {
        const args = argsMap.get(timing.toolCallId);
        if (args) {
          end.arguments = JSON.stringify(args);
        }
        const trContent = resultMap.get(timing.toolCallId);
        if (trContent !== undefined) {
          end.result = trContent;
        }
      }

      if (timing.isError) {
        toolRec.setCallError(new Error("tool returned error"));
      }

      toolRec.setResult(end);
      toolRec.end();
    } catch (err) {
      console.warn(
        `[sigil-pi] failed to emit tool span for ${timing.toolName}:`,
        err,
      );
    }
  }
}

function isAssistantMessage(message: unknown): message is PiAssistantMessage {
  if (!message || typeof message !== "object") return false;
  const candidate = message as Partial<PiAssistantMessage>;

  return (
    candidate.role === "assistant" &&
    typeof candidate.provider === "string" &&
    typeof candidate.model === "string" &&
    typeof candidate.timestamp === "number" &&
    !!candidate.usage &&
    Array.isArray(candidate.content) &&
    typeof candidate.stopReason === "string"
  );
}

function isUserMessage(message: unknown): message is PiUserMessage {
  if (!message || typeof message !== "object") return false;
  const candidate = message as Partial<PiUserMessage>;
  return (
    candidate.role === "user" &&
    (typeof candidate.content === "string" ||
      Array.isArray(candidate.content)) &&
    typeof candidate.timestamp === "number"
  );
}
