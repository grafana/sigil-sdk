import { readFile } from "fs/promises";
import { join } from "path";
import { homedir } from "os";

export type SigilAuthConfig =
  | { mode: "bearer"; bearerToken: string }
  | { mode: "tenant"; tenantId: string }
  | { mode: "basic"; tenantId: string; token: string }
  | { mode: "none" };

export type SigilConfig = {
  enabled: boolean;
  endpoint: string;
  auth: SigilAuthConfig;
  agentName?: string;
  agentVersion?: string;
  contentCapture?: boolean;
};

const CONFIG_PATH = join(homedir(), ".config", "opencode", "opencode-sigil.json");

const DISABLED: SigilConfig = {
  enabled: false,
  endpoint: "",
  auth: { mode: "none" },
};

export async function loadSigilConfig(): Promise<SigilConfig> {
  try {
    const raw = await readFile(CONFIG_PATH, "utf-8");
    const parsed = JSON.parse(raw);
    return parseSigilConfig(parsed) ?? DISABLED;
  } catch {
    return DISABLED;
  }
}

export function parseSigilConfig(raw: unknown): SigilConfig | undefined {
  if (!raw || typeof raw !== "object") return undefined;
  const obj = raw as Record<string, unknown>;
  if (obj.enabled !== true) return undefined;
  if (typeof obj.endpoint !== "string" || !obj.endpoint) {
    console.warn("[sigil] enabled but endpoint is missing -- disabling");
    return undefined;
  }
  if (!obj.auth || typeof obj.auth !== "object") {
    console.warn("[sigil] enabled but auth config is missing -- disabling");
    return undefined;
  }
  return raw as SigilConfig;
}
