#!/usr/bin/env node
import { copyFile, mkdir, readdir } from 'node:fs/promises';
import { dirname, isAbsolute, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(__dirname, '..', '..');
const src = join(repoRoot, 'proto', 'agento11y', 'v1');

// Optional first argument: alternate destination root. When set, files are
// written to `${arg}/agento11y/v1/*.proto` so callers (like `mise run check:proto`)
// can compare against the committed `js/proto/agento11y/` tree.
const destArg = process.argv[2];
const destRoot = destArg
  ? isAbsolute(destArg)
    ? destArg
    : resolve(process.cwd(), destArg)
  : join(__dirname, '..', 'proto');
const dst = join(destRoot, 'agento11y', 'v1');

await mkdir(dst, { recursive: true });
const entries = await readdir(src);
for (const name of entries) {
  if (!name.endsWith('.proto')) continue;
  await copyFile(join(src, name), join(dst, name));
}
