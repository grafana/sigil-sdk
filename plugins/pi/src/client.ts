import type {
  GenerationExportConfig,
  SigilLogger,
} from "@grafana/sigil-sdk-js";
import {
  createSecretRedactionSanitizer,
  SigilClient,
} from "@grafana/sigil-sdk-js";
import type { Meter, Tracer } from "@opentelemetry/api";
import type { SigilAuthConfig, SigilPiConfig } from "./config.js";

export interface SigilClientOptions {
  tracer?: Tracer;
  meter?: Meter;
}

function createSdkLogger(debug: boolean): SigilLogger {
  const debugLog = (message: string, ...args: unknown[]) => {
    if (debug) console.error(`[sigil-pi] ${message}`, ...args);
  };

  return {
    debug: debugLog,
    warn: (message: string, ...args: unknown[]) => {
      if (isBestEffortExportLog(message)) {
        debugLog(message, ...args);
        return;
      }
      console.warn(`[sigil-pi] ${message}`, ...args);
    },
    error: (message: string, ...args: unknown[]) => {
      console.error(`[sigil-pi] ${message}`, ...args);
    },
  };
}

function isBestEffortExportLog(message: string): boolean {
  return (
    message.startsWith("sigil generation export failed") ||
    message.startsWith("sigil generation rejected")
  );
}

export function createSigilClient(
  config: SigilPiConfig,
  options?: SigilClientOptions,
): SigilClient | null {
  try {
    return new SigilClient({
      generationExport: {
        protocol: "http",
        endpoint: config.endpoint,
        auth: mapAuth(config.auth),
      },
      contentCapture: config.contentCapture,
      ...(options?.tracer ? { tracer: options.tracer } : {}),
      ...(options?.meter ? { meter: options.meter } : {}),
      logger: createSdkLogger(config.debug),
      generationSanitizer: config.redaction.enabled
        ? createSecretRedactionSanitizer({
            redactInputMessages: config.redaction.redactInputMessages,
            redactEmailAddresses: config.redaction.redactEmailAddresses,
          })
        : undefined,
    });
  } catch (err) {
    console.warn("[sigil-pi] failed to create SigilClient:", err);
    return null;
  }
}

function mapAuth(auth: SigilAuthConfig): GenerationExportConfig["auth"] {
  switch (auth.mode) {
    case "basic":
      return {
        mode: "basic",
        basicUser: auth.user,
        basicPassword: auth.password,
        tenantId: auth.tenantId,
      };
    default:
      return auth;
  }
}
