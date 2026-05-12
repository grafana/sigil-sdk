import { execFileSync } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { resolveGitBranch } from "./git.js";

function git(cwd: string, args: string[]): string {
  return execFileSync("git", args, {
    cwd,
    stdio: ["ignore", "pipe", "pipe"],
    encoding: "utf-8",
  }).trim();
}

function initRepo(cwd: string, branch = "main"): void {
  // Use a fixed initial branch so the test doesn't depend on the host's
  // init.defaultBranch (which differs between machines and CI images).
  git(cwd, ["init", "-q", "-b", branch]);
  // user.* config is required for `git commit` to succeed in CI sandboxes.
  git(cwd, ["config", "user.email", "test@example.com"]);
  git(cwd, ["config", "user.name", "test"]);
  git(cwd, ["commit", "-q", "--allow-empty", "-m", "init"]);
}

describe("resolveGitBranch", () => {
  let dir: string;

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "sigil-pi-git-"));
  });

  afterEach(() => {
    rmSync(dir, { recursive: true, force: true });
  });

  it("returns the branch name on a normal checkout", () => {
    initRepo(dir, "feature-x");
    expect(resolveGitBranch(dir)).toBe("feature-x");
  });

  it("returns a short sha on detached HEAD", () => {
    initRepo(dir, "main");
    const sha = git(dir, ["rev-parse", "HEAD"]);
    git(dir, ["checkout", "-q", "--detach", sha]);

    const out = resolveGitBranch(dir);
    expect(out).toBeDefined();
    expect(out).not.toBe("HEAD");
    expect(out).toBe(sha.slice(0, 12));
  });

  it("returns undefined outside a git repo", () => {
    // mkdtemp roots have no `.git` ancestor on macOS/Linux.
    expect(resolveGitBranch(dir)).toBeUndefined();
  });

  it("returns undefined for empty cwd", () => {
    expect(resolveGitBranch("")).toBeUndefined();
  });
});
