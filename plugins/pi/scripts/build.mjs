import { spawnSync } from "node:child_process";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { build } from "esbuild";

const packageDir = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const tsc = process.platform === "win32" ? "tsc.cmd" : "tsc";

const typecheck = spawnSync(tsc, ["--noEmit"], {
  cwd: packageDir,
  stdio: "inherit",
});
if (typecheck.error) {
  console.error(typecheck.error);
  process.exit(1);
}
if (typecheck.status !== 0) {
  process.exit(typecheck.status ?? 1);
}

await build({
  absWorkingDir: packageDir,
  entryPoints: ["src/index.ts"],
  bundle: true,
  format: "esm",
  platform: "node",
  target: "es2022",
  banner: {
    js: "import { createRequire as __cr } from 'node:module'; const require = __cr(import.meta.url);",
  },
  outfile: "dist/index.js",
  external: [
    "@mariozechner/pi-coding-agent",
    "@mariozechner/pi-ai",
    "@sinclair/typebox",
    "@grpc/grpc-js",
    "@grpc/proto-loader",
  ],
  logLevel: "info",
});
