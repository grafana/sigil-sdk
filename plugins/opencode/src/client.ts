import { SigilClient } from "@grafana/sigil-sdk-js";
import type { SigilConfig, SigilAuthConfig } from "./config.js";

// Matches ExportAuthConfig from @grafana/sigil-sdk-js (not re-exported from package index)
type ResolvedAuth = {
  mode: "none" | "tenant" | "bearer";
  tenantId?: string;
  bearerToken?: string;
};

export function resolveEnvVars(value: string): string {
  return value.replace(/\$\{(\w+)\}/g, (_match, name) => {
    return process.env[name] ?? "";
  });
}

type ResolvedTransport = {
  auth: ResolvedAuth;
  headers?: Record<string, string>;
};

function resolveAuth(auth: SigilAuthConfig): ResolvedTransport {
  switch (auth.mode) {
    case "bearer":
      return { auth: { mode: "bearer", bearerToken: resolveEnvVars(auth.bearerToken) } };
    case "tenant":
      return { auth: { mode: "tenant", tenantId: resolveEnvVars(auth.tenantId) } };
    case "basic": {
      // JS SDK doesn't support Basic auth natively — use
      // mode "none" and inject the Authorization header manually.
      const user = resolveEnvVars(auth.tenantId);
      const pass = resolveEnvVars(auth.token);
      const encoded = Buffer.from(`${user}:${pass}`).toString("base64");
      return {
        auth: { mode: "none" },
        headers: { Authorization: `Basic ${encoded}` },
      };
    }
    case "none":
      return { auth: { mode: "none" } };
  }
}

const GENERATION_EXPORT_PATH = "/api/v1/generations:export";

export function createSigilClient(config: SigilConfig): SigilClient | null {
  try {
    if (!config.endpoint.includes(GENERATION_EXPORT_PATH)) {
      console.warn(
        `[sigil] endpoint "${config.endpoint}" does not include "${GENERATION_EXPORT_PATH}" -- ` +
        `the JS SDK requires the full export URL (e.g. "http://localhost:8080${GENERATION_EXPORT_PATH}")`,
      );
    }
    const transport = resolveAuth(config.auth);
    return new SigilClient({
      generationExport: {
        protocol: "http",
        endpoint: config.endpoint,
        auth: transport.auth,
        ...(transport.headers && { headers: transport.headers }),
      },
    });
  } catch {
    console.warn("[sigil] failed to create SigilClient");
    return null;
  }
}
