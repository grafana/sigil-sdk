import { describe, it, expect } from "vitest";
import { Redactor } from "./redact.js";

describe("Redactor", () => {
  const redactor = new Redactor();

  describe("redact (full — tier 1 + tier 2)", () => {
    it("redacts Grafana Cloud tokens", () => {
      const input = "token: glc_abcdefghijklmnopqrstuvwxyz1234";
      const result = redactor.redact(input);
      expect(result).not.toContain("glc_abcdefghijklmnopqrstuvwxyz1234");
      expect(result).toContain("[REDACTED:");
    });

    it("redacts Grafana service account tokens", () => {
      const input = "glsa_abcdefghijklmnopqrstuvwxyz1234";
      const result = redactor.redact(input);
      expect(result).not.toContain("glsa_");
      expect(result).toContain("[REDACTED:");
    });

    it("redacts AWS access keys", () => {
      const input = "aws_access_key_id = AKIAIOSFODNN7REALKEY";
      const result = redactor.redact(input);
      expect(result).not.toContain("AKIAIOSFODNN7REALKEY");
      expect(result).toContain("[REDACTED:");
    });

    it("redacts GitHub personal access tokens", () => {
      const input = "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij";
      const result = redactor.redact(input);
      expect(result).not.toContain("ghp_");
      expect(result).toContain("[REDACTED:");
    });

    it("redacts PEM private keys", () => {
      const input = `-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA0Z3VS5JJcds3xfn/ygWyF8PbnGy5AhEiS0C5
-----END RSA PRIVATE KEY-----`;
      const result = redactor.redact(input);
      expect(result).not.toContain("MIIEpAIBAAKCAQ");
      expect(result).toContain("[REDACTED:");
    });

    it("redacts connection strings with passwords", () => {
      const input = "postgres://admin:s3cretP4ss@db.example.com:5432/mydb";
      const result = redactor.redact(input);
      expect(result).not.toContain("s3cretP4ss");
      expect(result).toContain("[REDACTED:");
    });

    it("redacts Anthropic API keys", () => {
      const input = "sk-ant-api03-" + "a".repeat(93) + "AA";
      const result = redactor.redact(input);
      expect(result).not.toContain("sk-ant-api03-");
      expect(result).toContain("[REDACTED:");
    });

    it("redacts modern OpenAI project keys (sk-proj-)", () => {
      const input = "sk-proj-" + "a".repeat(50);
      const result = redactor.redact(input);
      expect(result).not.toContain("sk-proj-");
      expect(result).toContain("[REDACTED:");
    });

    it("redacts OpenAI service account keys (sk-svcacct-)", () => {
      const input = "sk-svcacct-" + "b".repeat(50);
      const result = redactor.redact(input);
      expect(result).not.toContain("sk-svcacct-");
      expect(result).toContain("[REDACTED:");
    });

    it("redacts env file secret values (tier 2)", () => {
      const input = "DATABASE_PASSWORD=hunter2secret123";
      const result = redactor.redact(input);
      expect(result).toContain("DATABASE_PASSWORD=");
      expect(result).not.toContain("hunter2secret123");
      expect(result).toContain("[REDACTED:");
    });

    it("redacts bearer tokens in headers", () => {
      const input = 'Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U';
      const result = redactor.redact(input);
      expect(result).toContain("[REDACTED:");
    });

    it("does NOT redact normal text", () => {
      const input = "The function returns a list of users from the database.";
      expect(redactor.redact(input)).toBe(input);
    });

    it("does NOT redact UUIDs", () => {
      const input = "session-id: 550e8400-e29b-41d4-a716-446655440000";
      expect(redactor.redact(input)).toBe(input);
    });

    it("handles empty string", () => {
      expect(redactor.redact("")).toBe("");
    });

    it("handles multiple secrets in one string", () => {
      const input = "key=AKIAIOSFODNN7REALKEY token=glc_abcdefghijklmnopqrstuvwxyz1234";
      const result = redactor.redact(input);
      expect(result).not.toContain("AKIAIOSFODNN7REALKEY");
      expect(result).not.toContain("glc_abcdefghijklmnopqrstuvwxyz1234");
    });
  });

  describe("redactLightweight (tier 1 only)", () => {
    it("redacts Grafana Cloud tokens", () => {
      const input = "I found the token: glc_abcdefghijklmnopqrstuvwxyz1234";
      const result = redactor.redactLightweight(input);
      expect(result).not.toContain("glc_abcdefghijklmnopqrstuvwxyz1234");
      expect(result).toContain("[REDACTED:");
    });

    it("does NOT redact env file patterns (tier 2 only)", () => {
      const input = "The file contains DATABASE_PASSWORD=hunter2secret123";
      const result = redactor.redactLightweight(input);
      expect(result).toContain("hunter2secret123");
    });

    it("does NOT redact normal text", () => {
      const input = "The API key configuration is stored in the settings panel.";
      expect(redactor.redactLightweight(input)).toBe(input);
    });
  });
});
