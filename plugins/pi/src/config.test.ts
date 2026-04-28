import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { resolveConfig, resolveEnvVars } from "./config.js";

function clearEnv() {
  for (const key of Object.keys(process.env)) {
    if (key.startsWith("SIGIL_PI_")) {
      delete process.env[key];
    }
  }
}

describe("resolveConfig", () => {
  beforeEach(clearEnv);
  afterEach(clearEnv);

  it("returns null when endpoint is missing", () => {
    expect(resolveConfig({})).toBeNull();
  });

  it("returns null when endpoint is whitespace", () => {
    expect(resolveConfig({ endpoint: "   " })).toBeNull();
  });

  it("parses full config from file", () => {
    const cfg = resolveConfig({
      endpoint: "http://localhost:8080/api/v1/generations:export",
      auth: { mode: "tenant", tenantId: "my-tenant" },
      agentName: "pi-custom",
      agentVersion: "1.0.0",
      contentCapture: true,
    });
    expect(cfg).not.toBeNull();
    expect(cfg?.endpoint).toBe(
      "http://localhost:8080/api/v1/generations:export",
    );
    expect(cfg?.auth).toEqual({ mode: "tenant", tenantId: "my-tenant" });
    expect(cfg?.agentName).toBe("pi-custom");
    expect(cfg?.agentVersion).toBe("1.0.0");
    expect(cfg?.contentCapture).toBe("full");
  });

  it("auto-appends export path to endpoint", () => {
    const cfg = resolveConfig({ endpoint: "http://localhost:8080" });
    expect(cfg?.endpoint).toBe(
      "http://localhost:8080/api/v1/generations:export",
    );
  });

  it("auto-appends export path and strips trailing slash", () => {
    const cfg = resolveConfig({ endpoint: "http://localhost:8080/" });
    expect(cfg?.endpoint).toBe(
      "http://localhost:8080/api/v1/generations:export",
    );
  });

  it("does not double-append export path", () => {
    const cfg = resolveConfig({
      endpoint: "http://localhost:8080/api/v1/generations:export",
    });
    expect(cfg?.endpoint).toBe(
      "http://localhost:8080/api/v1/generations:export",
    );
  });

  it("defaults contentCapture to metadata_only", () => {
    const cfg = resolveConfig({
      endpoint: "http://localhost:8080/api/v1/generations:export",
    });
    expect(cfg?.contentCapture).toBe("metadata_only");
  });

  it("maps boolean true to full", () => {
    const cfg = resolveConfig({
      endpoint: "http://localhost:8080/api/v1/generations:export",
      contentCapture: true,
    });
    expect(cfg?.contentCapture).toBe("full");
  });

  it("maps boolean false to metadata_only", () => {
    const cfg = resolveConfig({
      endpoint: "http://localhost:8080/api/v1/generations:export",
      contentCapture: false,
    });
    expect(cfg?.contentCapture).toBe("metadata_only");
  });

  it("accepts mode string no_tool_content", () => {
    const cfg = resolveConfig({
      endpoint: "http://localhost:8080/api/v1/generations:export",
      contentCapture: "no_tool_content",
    });
    expect(cfg?.contentCapture).toBe("no_tool_content");
  });

  it("warns and falls back on unknown mode string", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const cfg = resolveConfig({
      endpoint: "http://localhost:8080/api/v1/generations:export",
      contentCapture: "yolo",
    });
    expect(cfg?.contentCapture).toBe("metadata_only");
    expect(warn).toHaveBeenCalledWith(
      expect.stringContaining("unsupported contentCapture"),
    );
    warn.mockRestore();
  });

  it("defaults agentName to 'pi'", () => {
    const cfg = resolveConfig({
      endpoint: "http://localhost:8080/api/v1/generations:export",
      agentName: "   ",
    });
    expect(cfg?.agentName).toBe("pi");
  });

  it("env vars override file values", () => {
    process.env.SIGIL_PI_ENDPOINT = "http://env:9090/api/v1/generations:export";
    process.env.SIGIL_PI_AGENT_NAME = "pi-env";
    process.env.SIGIL_PI_CONTENT_CAPTURE = "true";
    process.env.SIGIL_PI_AUTH_MODE = "bearer";
    process.env.SIGIL_PI_BEARER_TOKEN = "tok-123";

    const cfg = resolveConfig({
      endpoint: "http://file:8080",
      agentName: "pi-file",
      contentCapture: false,
      auth: { mode: "none" },
    });

    expect(cfg?.endpoint).toBe("http://env:9090/api/v1/generations:export");
    expect(cfg?.agentName).toBe("pi-env");
    expect(cfg?.contentCapture).toBe("full");
    expect(cfg?.auth).toEqual({ mode: "bearer", bearerToken: "tok-123" });
  });

  it("env bool parsing is case-insensitive", () => {
    process.env.SIGIL_PI_CONTENT_CAPTURE = "On";
    process.env.SIGIL_PI_ENDPOINT =
      "http://localhost:8080/api/v1/generations:export";

    const cfg = resolveConfig({ auth: { mode: "none" } });

    expect(cfg?.contentCapture).toBe("full");
  });

  it("parses bearer auth from file", () => {
    const cfg = resolveConfig({
      endpoint: "http://localhost:8080/api/v1/generations:export",
      auth: { mode: "bearer", bearerToken: "my-token" },
    });
    expect(cfg?.auth).toEqual({ mode: "bearer", bearerToken: "my-token" });
  });

  it("returns null on bearer auth with missing token", () => {
    const cfg = resolveConfig({
      endpoint: "http://localhost:8080/api/v1/generations:export",
      auth: { mode: "bearer", bearerToken: "" },
    });
    expect(cfg).toBeNull();
  });

  it("parses basic auth with tenantId defaulting to user", () => {
    const cfg = resolveConfig({
      endpoint: "http://localhost:8080/api/v1/generations:export",
      auth: { mode: "basic", user: "12345", password: "pass" },
    });
    expect(cfg?.auth).toEqual({
      mode: "basic",
      user: "12345",
      password: "pass",
      tenantId: "12345",
    });
  });

  it("parses basic auth with explicit tenantId", () => {
    const cfg = resolveConfig({
      endpoint: "http://localhost:8080/api/v1/generations:export",
      auth: {
        mode: "basic",
        user: "12345",
        password: "pass",
        tenantId: "my-org",
      },
    });
    expect(cfg?.auth).toEqual({
      mode: "basic",
      user: "12345",
      password: "pass",
      tenantId: "my-org",
    });
  });

  it("returns null on basic auth with missing fields", () => {
    const cfg = resolveConfig({
      endpoint: "http://localhost:8080/api/v1/generations:export",
      auth: { mode: "basic", user: "user", password: "" },
    });
    expect(cfg).toBeNull();
  });

  it("defaults auth to none", () => {
    const cfg = resolveConfig({
      endpoint: "http://localhost:8080/api/v1/generations:export",
    });
    expect(cfg?.auth).toEqual({ mode: "none" });
  });

  it("returns null on unsupported auth mode", () => {
    const cfg = resolveConfig({
      endpoint: "http://localhost:8080/api/v1/generations:export",
      auth: { mode: "berer" },
    });
    expect(cfg).toBeNull();
  });
});

describe("resolveConfig otlp", () => {
  beforeEach(clearEnv);
  afterEach(clearEnv);

  it("returns no otlp when not configured", () => {
    const cfg = resolveConfig({
      endpoint: "http://localhost:8080/api/v1/generations:export",
    });
    expect(cfg?.otlp).toBeUndefined();
  });

  it("parses otlp with basic auth", () => {
    const cfg = resolveConfig({
      endpoint: "http://localhost:8080/api/v1/generations:export",
      otlp: {
        endpoint: "https://otlp-gw.grafana.net/otlp",
        basicUser: "123456",
        basicPassword: "glc_token",
      },
    });
    expect(cfg?.otlp).toBeDefined();
    expect(cfg?.otlp?.endpoint).toBe("https://otlp-gw.grafana.net/otlp");
    expect(cfg?.otlp?.headers.Authorization).toMatch(/^Basic /);
    const decoded = Buffer.from(
      cfg!.otlp!.headers.Authorization!.replace("Basic ", ""),
      "base64",
    ).toString();
    expect(decoded).toBe("123456:glc_token");
  });

  it("parses otlp with bearer token", () => {
    const cfg = resolveConfig({
      endpoint: "http://localhost:8080/api/v1/generations:export",
      otlp: {
        endpoint: "https://otlp.example.com",
        bearerToken: "my-token",
      },
    });
    expect(cfg?.otlp?.headers.Authorization).toBe("Bearer my-token");
  });

  it("parses otlp with custom headers", () => {
    const cfg = resolveConfig({
      endpoint: "http://localhost:8080/api/v1/generations:export",
      otlp: {
        endpoint: "https://otlp.example.com",
        headers: { "X-Custom": "value" },
      },
    });
    expect(cfg?.otlp?.headers["X-Custom"]).toBe("value");
  });

  it("env vars override otlp config", () => {
    process.env.SIGIL_PI_OTLP_ENDPOINT = "https://env-otlp.example.com";
    process.env.SIGIL_PI_OTLP_BASIC_USER = "env-user";
    process.env.SIGIL_PI_OTLP_BASIC_PASSWORD = "env-pass";

    const cfg = resolveConfig({
      endpoint: "http://localhost:8080/api/v1/generations:export",
      otlp: {
        endpoint: "https://file-otlp.example.com",
        basicUser: "file-user",
        basicPassword: "file-pass",
      },
    });
    expect(cfg?.otlp?.endpoint).toBe("https://env-otlp.example.com");
    const decoded = Buffer.from(
      cfg!.otlp!.headers.Authorization!.replace("Basic ", ""),
      "base64",
    ).toString();
    expect(decoded).toBe("env-user:env-pass");
  });

  it("basic auth takes precedence over bearer", () => {
    const cfg = resolveConfig({
      endpoint: "http://localhost:8080/api/v1/generations:export",
      otlp: {
        endpoint: "https://otlp.example.com",
        basicUser: "user",
        basicPassword: "pass",
        bearerToken: "token",
      },
    });
    expect(cfg?.otlp?.headers.Authorization).toMatch(/^Basic /);
  });
});

describe("resolveEnvVars", () => {
  beforeEach(clearEnv);
  afterEach(clearEnv);

  it("replaces ${VAR} with env value", () => {
    process.env.MY_TOKEN = "secret-123";
    expect(resolveEnvVars("Bearer ${MY_TOKEN}")).toBe("Bearer secret-123");
  });

  it("replaces missing vars with empty string", () => {
    expect(resolveEnvVars("${NONEXISTENT}")).toBe("");
  });

  it("leaves strings without vars unchanged", () => {
    expect(resolveEnvVars("plain-value")).toBe("plain-value");
  });
});
