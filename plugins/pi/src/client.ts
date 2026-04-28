import type { GenerationExportConfig } from "@grafana/sigil-sdk-js";
import { SigilClient } from "@grafana/sigil-sdk-js";
import type { Meter, Tracer } from "@opentelemetry/api";
import type { SigilAuthConfig, SigilPiConfig } from "./config.js";

export interface SigilClientOptions {
  tracer?: Tracer;
  meter?: Meter;
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
